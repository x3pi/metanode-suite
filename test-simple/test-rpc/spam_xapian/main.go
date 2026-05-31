package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Config struct {
	RPCUrl  string   `json:"rpc_url"`
	RPCUrls []string `json:"rpc_urls"`
	ChainID int64    `json:"chain_id"`
}

type KeyItem struct {
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
}

func waitReceipt(client *ethclient.Client, rpcUrl string, txHash common.Hash, startEpoch uint64) (*types.Receipt, error) {
	timeout := time.After(1800 * time.Second)
	for {
		select {
		case <-timeout:
			if startEpoch != 0 {
				currentEpoch, errEpoch := getLatestEpoch(rpcUrl)
				if errEpoch == nil && currentEpoch == startEpoch {
					msg := fmt.Sprintf("🚨 Giao dịch %s bị Timeout (chờ 1800s) nhưng KHÔNG có chuyển đổi epoch! (Epoch: %d)", txHash.Hex(), startEpoch)
					sendTelegramAlert(msg, "SPAM XAPIAN")
				}
			}
			curlCmd := fmt.Sprintf(`curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"eth_getTransactionReceipt","params":["%s"],"id":1}' %s`, txHash.Hex(), rpcUrl)
			return nil, fmt.Errorf("timeout 1800s waiting for receipt.\n   Manual check: %s", curlCmd)
		default:
			receipt, err := client.TransactionReceipt(context.Background(), txHash)
			if err == nil {
				return receipt, nil
			}
			if err != ethereum.NotFound {
				return nil, err
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func fetchNonceWithRetry(client *ethclient.Client, addr common.Address, expectedMin uint64) (uint64, error) {
	var n uint64
	var err error
	for i := 1; i <= 5; i++ {
		n, err = client.PendingNonceAt(context.Background(), addr)
		if err != nil {
			return 0, fmt.Errorf("lỗi RPC khi lấy nonce: %v", err)
		}
		if n >= expectedMin {
			return n, nil
		}
		fmt.Printf("⚠️ Nonce lấy về (%d) nhỏ hơn mong đợi (%d) cho ví %s (lần %d/5). Thử lại sau 500ms...\n", n, expectedMin, addr.Hex(), i)
		time.Sleep(500 * time.Millisecond)
	}
	return 0, fmt.Errorf("không thể lấy nonce hợp lệ (>= %d) sau 5 lần thử (nonce hiện tại: %d)", expectedMin, n)
}

type Wallet struct {
	Address common.Address
	PrivKey *ecdsa.PrivateKey
	Nonce   uint64
}

func checkNodesHealth(rpcUrls []string) error {
	for _, url := range rpcUrls {
		payload := strings.NewReader(`{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req, err := http.NewRequestWithContext(ctx, "POST", url, payload)
		if err != nil {
			cancel()
			return fmt.Errorf("không thể tạo request cho node %s: %v", url, err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			return fmt.Errorf("node %s KHÔNG HOẠT ĐỘNG (Lỗi kết nối hoặc timeout: %v)", url, err)
		}
		resp.Body.Close()
		cancel()
		if resp.StatusCode != 200 {
			return fmt.Errorf("node %s KHÔNG HOẠT ĐỘNG (HTTP code: %d)", url, resp.StatusCode)
		}
	}
	return nil
}

func getLatestEpoch(rpcUrl string) (uint64, error) {
	payload := strings.NewReader(`{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest", false],"id":1}`)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", rpcUrl, payload)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var result struct {
		Result struct {
			Epoch *hexutil.Uint64 `json:"epoch"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	if result.Result.Epoch == nil {
		return 0, fmt.Errorf("không tìm thấy trường epoch trong block")
	}
	return uint64(*result.Result.Epoch), nil
}

func getSystemIPInfo() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "Unknown"
	}

	var localIPs []string
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					localIPs = append(localIPs, ipnet.IP.String())
				}
			}
		}
	}
	localIPStr := "Unknown"
	if len(localIPs) > 0 {
		localIPStr = strings.Join(localIPs, ", ")
	}

	publicIP := "Unknown"
	client := http.Client{
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get("https://api.ipify.org")
	if err == nil {
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			publicIP = strings.TrimSpace(string(body))
		}
	}

	if publicIP != "Unknown" {
		return fmt.Sprintf("%s (Static/Private IP: %s, Public IP: %s)", hostname, localIPStr, publicIP)
	}
	return fmt.Sprintf("%s (Static/Private IP: %s)", hostname, localIPStr)
}

func sendTelegramAlert(message string, testName string) {
	if os.Getenv("MTN_TELE_ALERT") != "true" {
		return
	}
	token := "8230176859:AAGoZ_78xzb1q4rgJJ5SYLxRhZBYBTSz_xo"
	chatID := "-1003867050625"
	ipInfo := getSystemIPInfo()

	fullMessage := fmt.Sprintf("❌ *[%s]* CẢNH BÁO LỖI!\n\n*Server:* `%s`\n\n%s", testName, ipInfo, message)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]string{
		"chat_id": chatID,
		"text":    fullMessage,
		"parse_mode": "Markdown",
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		fmt.Printf("⚠️ Lỗi gửi Telegram alert: %v\n", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Printf("⚠️ Lỗi gửi Telegram alert, status code: %d\n", resp.StatusCode)
	}
}

func main() {
	configPath := flag.String("config", "../config-local.json", "Path to config JSON")
	keysPath := flag.String("keys", "../../../test_tps/gen_spam_keys/generated_keys.json", "Path to generated keys JSON")
	abiPath := flag.String("abi", "../test_read_wire_xapian/abi/xapian.json", "Path to contract ABI JSON")
	contractAddrStr := flag.String("contract", "", "Contract address (REQUIRED if not deploying)")
	deployJsonPath := flag.String("deploy-json", "", "Path to JSON data file containing deploy action")
	methodName := flag.String("method", "runStep1_Setup", "Method name to call (e.g., runStep1_Setup, runStep3_UpdateDoc)")
	numWallets := flag.Int("wallets", 1000, "Number of wallets to use for spamming")
	maxRounds := flag.Int("rounds", 2000, "Maximum number of rounds to execute")

	flag.Parse()

	if *contractAddrStr == "" && *deployJsonPath == "" {
		log.Fatalf("❌ Vui lòng cung cấp -contract=<address> HOẶC -deploy-json=<path_to_json>")
	}

	rawCfg, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc config: %v", err)
	}
	var cfg Config
	json.Unmarshal(rawCfg, &cfg)

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC: %v", err)
	}
	fmt.Printf("🔗 Kết nối RPC: %s (ChainID: %d)\n", cfg.RPCUrl, cfg.ChainID)

	rawKeys, err := os.ReadFile(*keysPath)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc file keys: %v", err)
	}
	var allKeys []KeyItem
	if err := json.Unmarshal(rawKeys, &allKeys); err != nil {
		log.Fatalf("❌ Lỗi parse JSON keys: %v", err)
	}
	if len(allKeys) < *numWallets {
		log.Fatalf("❌ Số lượng ví trong file (%d) ít hơn số lượng yêu cầu (%d)", len(allKeys), *numWallets)
	}

	selectedKeys := allKeys[:*numWallets]
	fmt.Printf("🔑 Đang lấy nonce khởi tạo cho %d ví (có thể mất chút thời gian)...\n", len(selectedKeys))

	wallets := make([]*Wallet, len(selectedKeys))
	for i, k := range selectedKeys {
		pk, err := crypto.HexToECDSA(k.PrivateKey)
		if err != nil {
			log.Fatalf("❌ Lỗi parse private key ví thứ %d: %v", i, err)
		}
		addr := common.HexToAddress(k.Address)
		nonce, err := fetchNonceWithRetry(client, addr, 0)
		if err != nil {
			log.Fatalf("❌ Lỗi lấy nonce cho ví %s: %v", addr.Hex(), err)
		}
		wallets[i] = &Wallet{
			Address: addr,
			PrivKey: pk,
			Nonce:   nonce,
		}
	}
	fmt.Printf("✅ Đã nạp %d ví sẵn sàng spam\n", len(wallets))

	var contractAddr common.Address
	if *contractAddrStr != "" {
		contractAddr = common.HexToAddress(*contractAddrStr)
	} else {
		// Tự động deploy
		fmt.Printf("⏳ Chế độ Tự Deploy kích hoạt (File: %s)...\n", *deployJsonPath)
		rawJson, err := os.ReadFile(*deployJsonPath)
		if err != nil {
			log.Fatalf("❌ Lỗi đọc file json deploy: %v", err)
		}
		type TaskItem struct {
			Action    string `json:"action"`
			InputData string `json:"input_data"`
		}
		var tasks []TaskItem
		if err := json.Unmarshal(rawJson, &tasks); err != nil {
			log.Fatalf("❌ Lỗi parse json deploy: %v", err)
		}
		if len(tasks) == 0 || tasks[0].Action != "deploy" {
			log.Fatalf("❌ File JSON không có action 'deploy' ở phần tử đầu tiên")
		}
		hexStr := strings.TrimPrefix(tasks[0].InputData, "0x")
		bytecode, err := hex.DecodeString(hexStr)
		if err != nil {
			log.Fatalf("❌ Lỗi decode bytecode hex: %v", err)
		}

		deployer := wallets[0]
		gasPrice, _ := client.SuggestGasPrice(context.Background())
		gasLimit, err := client.EstimateGas(context.Background(), ethereum.CallMsg{
			From:     deployer.Address,
			GasPrice: gasPrice,
			Data:     bytecode,
		})
		if err != nil {
			gasLimit = 3000000
		} else {
			gasLimit += 50000
		}
		tx := types.NewContractCreation(deployer.Nonce, big.NewInt(0), gasLimit, gasPrice, bytecode)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), deployer.PrivKey)
		if err != nil {
			log.Fatalf("❌ Lỗi sign deploy tx: %v", err)
		}
		if err := client.SendTransaction(context.Background(), signedTx); err != nil {
			log.Fatalf("❌ Lỗi send deploy tx: %v", err)
		}
		fmt.Printf("🚀 Đã gửi tx Deploy (Hash: %s). Đang đợi receipt...\n", signedTx.Hash().Hex())
		receipt, err := waitReceipt(client, cfg.RPCUrl, signedTx.Hash(), 0)
		if err != nil {
			log.Fatalf("❌ Timeout khi đợi deploy receipt: %v", err)
		}
		if receipt.Status != 1 {
			log.Fatalf("❌ Transaction deploy bị REVERT!")
		}
		deployer.Nonce++
		contractAddr = receipt.ContractAddress
		fmt.Printf("✅ Đã deploy thành công! Mới tạo Contract: %s (Gas used: %d)\n", contractAddr.Hex(), receipt.GasUsed)
	}

	rawAbi, err := os.ReadFile(*abiPath)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc file ABI: %v", err)
	}
	parsedABI, err := abi.JSON(strings.NewReader(string(rawAbi)))
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	callData, err := parsedABI.Pack(*methodName)
	if err != nil {
		log.Fatalf("❌ Lỗi pack method %s: %v", *methodName, err)
	}
	fmt.Printf("📦 Đã pack xong data cho hàm: %s\n", *methodName)

	fmt.Println("\n🚀 BẮT ĐẦU SPAM XAPIAN LIÊN TỤC (Bấm Ctrl+C để dừng)")
	fmt.Printf("   Method: %s\n", *methodName)
	fmt.Printf("   Contract: %s\n", contractAddr.Hex())
	fmt.Printf("   Chế độ: Đợi Receipt xong là gửi round tiếp theo KHÔNG NGỦ\n")
	fmt.Println("--------------------------------------------------")

	var tpsHistory []float64

	for round := 1; round <= *maxRounds; round++ {

		if _, err := os.Stat("/tmp/MTN_CHAIN_ERROR_STOP"); err == nil {
			fmt.Println("\n🛑 PHÁT HIỆN CỜ LỖI (/tmp/MTN_CHAIN_ERROR_STOP) TỪ BLOCK CHECKER! DỪNG SPAM KHẨN CẤP!")
			os.Exit(1)
		}

		// Kiểm tra sức khỏe của 5 node trước khi bắt đầu round mới
		if len(cfg.RPCUrls) > 0 {
			if err := checkNodesHealth(cfg.RPCUrls); err != nil {
				errMsg := fmt.Sprintf("❌ PHÁT HIỆN NODE CHẾT TRONG QUÁ TRÌNH CHẠY SPAM XAPIAN!\n\nChi tiết: %v\n\n🚨 DỪNG CHƯƠNG TRÌNH KHẨN CẤP!", err)
				fmt.Println("\n" + errMsg)
				_ = os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(errMsg), 0644)
				os.Exit(1)
			}
		}

		startBlock, err := client.BlockNumber(context.Background())
		if err != nil {
			startBlock = 0
		}
		startEpoch, err := getLatestEpoch(cfg.RPCUrl)
		if err != nil {
			fmt.Printf("⚠️ Không thể lấy start epoch trước round %d: %v\n", round, err)
		}

		fmt.Printf("\n🔄 [ROUND %d/%d] Đang gửi %d transactions...\n", round, *maxRounds, len(wallets))

		var wg sync.WaitGroup
		var successCount uint32
		var failCount uint32
		var revertCount uint32

		gasPrice, err := client.SuggestGasPrice(context.Background())
		if err != nil {
			log.Printf("⚠️ Lỗi lấy gas price: %v. Dùng mặc định 0.", err)
			gasPrice = big.NewInt(0)
		}

		startTime := time.Now()

		for _, w := range wallets {
			wg.Add(1)
			go func(wallet *Wallet) {
				defer wg.Done()
				nonce := wallet.Nonce

				gasLimit, err := client.EstimateGas(context.Background(), ethereum.CallMsg{
					From:     wallet.Address,
					To:       &contractAddr,
					GasPrice: gasPrice,
					Data:     callData,
				})
				if err != nil {
					gasLimit = 200_000
				} else {
					gasLimit += 20_000
				}

				tx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), gasLimit, gasPrice, callData)
				signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), wallet.PrivKey)
				if err != nil {
					atomic.AddUint32(&failCount, 1)
					return
				}

				err = client.SendTransaction(context.Background(), signedTx)
				if err != nil {
					atomic.AddUint32(&failCount, 1)
					fmt.Printf("❌ Lỗi gửi Tx (Nonce %d): %v\n", nonce, err)

					if strings.Contains(strings.ToLower(err.Error()), "nonce") || strings.Contains(strings.ToLower(err.Error()), "replacement") {
						n, errFetch := fetchNonceWithRetry(client, wallet.Address, wallet.Nonce)
						if errFetch == nil {
							wallet.Nonce = n
						}
					}
					return
				}

				receipt, err := waitReceipt(client, cfg.RPCUrl, signedTx.Hash(), startEpoch)
				if err != nil {
					atomic.AddUint32(&failCount, 1)

					errMsg := fmt.Sprintf("❌ Tx %s Timeout/Lỗi: %v", signedTx.Hash().Hex(), err)
					fmt.Println(errMsg)

					// Write error to trigger CI monitor and stop execution immediately
					_ = os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(errMsg), 0644)
					os.Exit(1)
				}

				wallet.Nonce++

				if receipt.Status == 1 {
					atomic.AddUint32(&successCount, 1)
					fmt.Printf("✅ Tx %s THÀNH CÔNG (Gas: %d)\n", signedTx.Hash().Hex(), receipt.GasUsed)
				} else {
					atomic.AddUint32(&revertCount, 1)
					fmt.Printf("⚠️ Tx %s BỊ REVERT!\n", signedTx.Hash().Hex())
				}
			}(w)
		}

		wg.Wait()
		duration := time.Since(startTime)
		tps := float64(successCount) / duration.Seconds()
		fmt.Printf("✅ Đã kết thúc Round %d: %d thành công, %d Revert, %d thất bại (Mạng nghẽn/Lỗi Gửi) - Thời gian: %v - TPS: %.2f\n", round, successCount, revertCount, failCount, duration, tps)

		endBlock, err := client.BlockNumber(context.Background())
		if err != nil {
			endBlock = 0
		}
		endEpoch, err := getLatestEpoch(cfg.RPCUrl)
		if err != nil {
			fmt.Printf("⚠️ Không thể lấy end epoch sau round %d: %v\n", round, err)
		}

		if startEpoch != 0 && endEpoch != 0 && startEpoch != endEpoch {
			fmt.Printf("🔄 Phát hiện chuyển đổi Epoch (%d -> %d) trong Round %d. Giao dịch có thể bị chậm, bỏ qua cảnh báo TPS.\n", startEpoch, endEpoch, round)
		} else {
			if len(tpsHistory) >= 3 {
				var sum float64
				for _, v := range tpsHistory {
					sum += v
				}
				avgTps := sum / float64(len(tpsHistory))

				// Cảnh báo nếu TPS giảm hơn 40% so với trung bình các round trước
				if tps < avgTps*0.6 {
					dropPercent := (avgTps - tps) / avgTps * 100
					msg := fmt.Sprintf("🚨 [METANODE ALERT] Cảnh báo TPS giảm bất thường!\nRound: %d\nTPS hiện tại: %.2f\nTPS trung bình trước đó: %.2f\n📉 Mức giảm: %.2f%%\nThời gian round: %v\n📦 Block: %d ➡️ %d\n⏱️ Epoch: %d ➡️ %d", round, tps, avgTps, dropPercent, duration, startBlock, endBlock, startEpoch, endEpoch)
					fmt.Println(msg)
					sendTelegramAlert(msg, "SPAM XAPIAN")
				}
			}
			tpsHistory = append(tpsHistory, tps)
			if len(tpsHistory) > 10 {
				tpsHistory = tpsHistory[1:] // Giữ lịch sử 10 round gần nhất
			}
		}

		// Tự động ngắt khẩn cấp nếu 100% giao dịch thất bại (RPC sập, rớt mạng...)
		if failCount == uint32(len(wallets)) && len(wallets) > 0 {
			errMsg := fmt.Sprintf("❌ Đã kết thúc Round %d: %d thành công, %d Revert, %d thất bại (Mạng nghẽn/Lỗi Gửi) - Thời gian: %v\n\n🚨 TẤT CẢ GIAO DỊCH ĐỀU THẤT BẠI TRONG ROUND NÀY! Mạng RPC hoặc Node có thể đã sập. DỪNG KHẨN CẤP!", round, successCount, revertCount, failCount, duration)
			fmt.Println("\n" + errMsg)
			_ = os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(errMsg), 0644)
			os.Exit(1)
		}
	}
	fmt.Printf("\n🎉 HOÀN TẤT TỔNG CỘNG %d ROUNDS SPAM!\n", *maxRounds)
}
