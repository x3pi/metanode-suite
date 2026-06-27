package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"bufio"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	client_tcp "tool-test/pkg/client-tcp"
	tcp_config "tool-test/pkg/client-tcp/config"
	tx_models "tool-test/pkg/client-tcp/models"
	"tool-test/pkg/client-tcp/utils/tx_helper"
	"tool-test/pkg/logger"
	pb "tool-test/pkg/proto"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

type AppConfig struct {
	PublicKeyBLS            string   `json:"public_key_bls"`
	TransferAmount          string   `json:"transfer_amount"`
	ChainId                 string   `json:"chainId"`
	WalletPool              []string `json:"wallet_pool"`
	ParentConnectionAddress []string `json:"parent_connection_address"`
	RpcEndpoints            []string `json:"rpc_endpoints"`
}

type GeneratedKey struct {
	Index      int    `json:"index"`
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
}

func sendTelegramNotification(message string) {
	envPath := "/home/abc/nhat/con-chain-v2/metanode-suite/scripts/.env"
	file, err := os.Open(envPath)
	if err != nil {
		fmt.Println("⚠️ Không thể mở .env để gửi Telegram:", err)
		return
	}
	defer file.Close()

	var token, chatID string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "TELEGRAM_BOT_TOKEN=") {
			token = strings.TrimPrefix(line, "TELEGRAM_BOT_TOKEN=")
		} else if strings.HasPrefix(line, "TELEGRAM_CHAT_ID=") {
			chatID = strings.TrimPrefix(line, "TELEGRAM_CHAT_ID=")
		}
	}

	if token == "" || chatID == "" {
		fmt.Println("⚠️ Không tìm thấy token hoặc chatID trong .env")
		return
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]string{
		"chat_id": chatID,
		"text":    message,
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err == nil {
		resp.Body.Close()
	}
}

func main() {
	countFlag := flag.Int("count", 1, "Số lượng ví cần tạo")
	roundsFlag := flag.Int("rounds", 1, "Số vòng gửi tiền (mặc định 1)")
	skipFundFlag := flag.Bool("skip_fund", false, "Bỏ qua bước chuyển native coin vào ví mới")
	singleNodeFlag := flag.Bool("single", false, "Chỉ gửi đến node đầu tiên (m0) thay vì tất cả các node")
	flag.Parse()

	logger.SetConfig(&logger.LoggerConfig{
		Flag:    logger.FLAG_INFO,
		Outputs: []*os.File{os.Stdout},
	})

	configPath := "../config.json"
	fmt.Println("==================================================")
	fmt.Println("🚀 START GENERATE KEYS & FUND WALLETS VIA TCP")
	fmt.Println("==================================================")

	// 1. Tải cấu hình file JSON
	cfgRaw, err := tcp_config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc file config: %v", err)
	}
	cfg := cfgRaw.(*tcp_config.ClientConfig)

	// Parse Custom Config
	appCfgBytes, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc file config cho AppConfig: %v", err)
	}
	var appCfg AppConfig
	if err := json.Unmarshal(appCfgBytes, &appCfg); err != nil {
		log.Fatalf("❌ Lỗi parse AppConfig: %v", err)
	}

	if *singleNodeFlag {
		if len(appCfg.ParentConnectionAddress) > 0 {
			appCfg.ParentConnectionAddress = appCfg.ParentConnectionAddress[:1]
		}
		if len(appCfg.RpcEndpoints) > 0 {
			appCfg.RpcEndpoints = appCfg.RpcEndpoints[:1]
		}
		fmt.Println("⚠️ Chế độ SINGLE NODE: Chỉ kết nối đến node đầu tiên (m0).")
	}

	// Cập nhật cfg.ChainId từ AppConfig (config.json dùng "chainId" dạng string)
	if appCfg.ChainId != "" {
		if chainIdUint, ok := new(big.Int).SetString(appCfg.ChainId, 10); ok {
			cfg.ChainId = chainIdUint.Uint64()
		}
	}

	// if appCfg.PublicKeyBLS == "" {
	// 	log.Fatalf("❌ Không có public_key_bls nào để đăng ký")
	// }

	numWallets := *countFlag
	if numWallets <= 0 {
		log.Fatalf("❌ Số lượng ví (-count) phải lớn hơn 0")
	}

	transferAmtStr := appCfg.TransferAmount
	if transferAmtStr == "" {
		transferAmtStr = "1000000000000000000" // Mặc định 1 token
	}
	transferAmount, ok := new(big.Int).SetString(transferAmtStr, 10)
	if !ok {
		log.Fatalf("❌ transfer_amount không hợp lệ: %s", transferAmtStr)
	}

	// 2. Kết nối tới Chain qua TCP
	fmt.Printf("🔌 Connecting to TCP pool (Load balancing across %d nodes)\n", len(appCfg.ParentConnectionAddress))
	poolSize := 300
	var clientPool []*client_tcp.Client
	for i := 0; i < poolSize; i++ {
		cfgClone := *cfg

		// Generate a random 20-byte wallet address for this connection's identifier
		var randomAddrBytes [20]byte
		if _, err := rand.Read(randomAddrBytes[:]); err != nil {
			log.Fatalf("❌ Lỗi tạo random address cho connection %d: %v", i, err)
		}
		cfgClone.ParentAddress = common.BytesToAddress(randomAddrBytes[:]).Hex()

		if len(appCfg.ParentConnectionAddress) > 0 {
			cfgClone.ParentConnectionAddress = appCfg.ParentConnectionAddress[i%len(appCfg.ParentConnectionAddress)]
		}
		tcpClient, err := client_tcp.NewClient(&cfgClone)
		if err != nil {
			log.Fatalf("❌ Lỗi kết nối TCP: %v", err)
		}
		clientPool = append(clientPool, tcpClient)
	}
	// Không cần ChainID vì gửi qua TCP không dùng e_types.SignTx

	// =====================================================================
	// BƯỚC 1: TẠO VÍ MỚI & LƯU FILE
	// =====================================================================
	fmt.Printf("\n[1] Đang tạo %d ví mới...\n", numWallets)
	var generatedKeys []GeneratedKey

	for i := 0; i < numWallets; i++ {
		privKey, err := crypto.GenerateKey()
		if err != nil {
			log.Fatalf("❌ Lỗi tạo ví mới ở index %d: %v", i, err)
		}
		privKeyHex := hexutil.Encode(crypto.FromECDSA(privKey))
		addressHex := crypto.PubkeyToAddress(privKey.PublicKey).Hex()

		generatedKeys = append(generatedKeys, GeneratedKey{
			Index:      i,
			PrivateKey: strings.TrimPrefix(privKeyHex, "0x"),
			Address:    addressHex,
		})
	}

	outputPath := "../../test_tps/gen_spam_keys/generated_keys.json"
	os.MkdirAll(filepath.Dir(outputPath), 0755)

	fileBytes, err := json.MarshalIndent(generatedKeys, "", "  ")
	if err != nil {
		log.Fatalf("❌ Lỗi format JSON output: %v", err)
	}
	if err := os.WriteFile(outputPath, fileBytes, 0644); err != nil {
		log.Fatalf("❌ Lỗi ghi file output: %v", err)
	}
	fmt.Printf("✅ Đã lưu %d ví vào %s\n", numWallets, outputPath)

	// =====================================================================
	// BƯỚC 2: CHUYỂN TIỀN VÀO CÁC VÍ VỪA TẠO (Sequentially)
	// =====================================================================
	if *skipFundFlag {
		fmt.Println("\n==================================================")
		fmt.Println("⏭️  BỎ QUA BƯỚC CHUYỂN TIỀN (skip_fund=true)")
		fmt.Println("==================================================")
		fmt.Println("🎉 TẤT CẢ QUÁ TRÌNH ĐÃ HOÀN TẤT!")
		return
	}

	numRounds := *roundsFlag
	if numRounds <= 0 {
		numRounds = 1
	}

	totalTxs := numWallets * numRounds
	totalSuccess := 0
	var fundMu sync.Mutex

	fundingWallets := []string{cfg.Address().Hex()}
	if len(appCfg.WalletPool) > 0 {
		fundingWallets = appCfg.WalletPool
	}

	fmt.Printf("\n[2] Bắt đầu %d vòng chuyển tiền vào %d ví (Tổng %d txs)...\n", numRounds, numWallets, totalTxs)

	for round := 1; round <= numRounds; round++ {
		fmt.Printf("\n--- Bắt đầu VÒNG %d/%d ---\n", round, numRounds)
		
		var fundWg sync.WaitGroup
		jobs := make(chan GeneratedKey, len(generatedKeys))
		for _, key := range generatedKeys {
			jobs <- key
		}
		close(jobs)

		for workerID, walletAddrStr := range fundingWallets {
			fundWg.Add(1)
			go func(wID int, fromAddrStr string) {
				defer fundWg.Done()

				fromAddress := common.HexToAddress(fromAddrStr)
				client := clientPool[wID%len(clientPool)]

				for key := range jobs {
					toAddress := common.HexToAddress(key.Address)
					
					receipt, err := tx_helper.SendTransactionNoneKey(
						"NativeTransfer",
						client,
						cfg,
						toAddress,
						fromAddress,
						nil, // data
						&tx_models.TxOptions{Amount: transferAmount},
					)

					if err != nil {
						fmt.Printf("❌ Ví %d (%s): Lỗi chuyển tiền từ %s: %v\n", key.Index, toAddress.Hex(), fromAddress.Hex(), err)
						continue
					}

					if receipt != nil && (receipt.Status() == pb.RECEIPT_STATUS_RETURNED || receipt.Status() == pb.RECEIPT_STATUS_HALTED) {
						fundMu.Lock()
						totalSuccess++
						if totalSuccess%numWallets == 0 || totalSuccess == totalTxs {
							fmt.Printf("   ✅ Đã chuyển tiền thành công %d/%d txs (Tx: %s)\n", totalSuccess, totalTxs, receipt.TransactionHash().Hex())
						}
						fundMu.Unlock()
					} else {
						var status string
						if receipt != nil {
							status = receipt.Status().String()
						}
						fmt.Printf("❌ Ví %d: Chuyển tiền thất bại (Status: %s)\n", key.Index, status)
					}
				}
			}(workerID, walletAddrStr)
		}

		fundWg.Wait()
		fmt.Printf("--- Hoàn thành VÒNG %d ---\n", round)
	}

	fmt.Printf("\n✅ Hoàn thành tổng cộng (%d/%d thành công).\n", totalSuccess, totalTxs)
	
	if totalSuccess < totalTxs {
		errMsg := fmt.Sprintf("❌ [send_native] Lỗi chuyển tiền Native: Chỉ thành công %d/%d txs. Đã bị hụt %d txs!", totalSuccess, totalTxs, totalTxs-totalSuccess)
		fmt.Println(errMsg)
		sendTelegramNotification(errMsg)
	}

	fmt.Println("\n==================================================")
	fmt.Println("🎉 TẤT CẢ QUÁ TRÌNH ĐÃ HOÀN TẤT!")
}
