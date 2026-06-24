package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"crypto/rand"

	client_tcp "tool-test/pkg/client-tcp"
	tcp_config "tool-test/pkg/client-tcp/config"
	tx_models "tool-test/pkg/client-tcp/models"
	"tool-test/pkg/client-tcp/utils/tx_helper"
	"tool-test/pkg/logger"
	pb "tool-test/pkg/proto"
	"tool-test/test_tps/tps_blast_cc/rpc"

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

func main() {
	countFlag := flag.Int("count", 1, "Số lượng ví cần tạo")
	skipFundFlag := flag.Bool("skip_fund", false, "Bỏ qua bước chuyển native coin vào ví mới")
	singleNodeFlag := flag.Bool("single", false, "Chỉ gửi đến node đầu tiên (m0) thay vì tất cả các node")
	traceFlag := flag.Bool("trace", false, "Hiển thị chi tiết trace performance của block")
	flag.Parse()

	logger.SetConfig(&logger.LoggerConfig{
		Flag:    logger.FLAG_INFO,
		Outputs: []*os.File{os.Stdout},
	})

	configPath := "config.json"
	fmt.Println("==================================================")
	fmt.Println("🚀 START REGISTER BLS & FUND WALLETS VIA TCP")
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

	// Chuẩn bị payload setBlsPublicKey được xử lý bên trong RegisterBlsForAccount

	var rpcHost string
	if len(appCfg.RpcEndpoints) > 0 && appCfg.RpcEndpoints[0] != "" {
		rpcHost = appCfg.RpcEndpoints[0]
		rpcHost = strings.ReplaceAll(rpcHost, " ", "") // remove accidental spaces
		if !strings.HasPrefix(rpcHost, "http://") && !strings.HasPrefix(rpcHost, "https://") {
			rpcHost = "http://" + rpcHost
		}
	} else {
		tcpHost := appCfg.ParentConnectionAddress[0]
		rpcHost = "http://" + strings.Split(tcpHost, ":")[0] + ":8757"
	}
	rpcClient := rpc.NewRPCClient(rpcHost)

	// Lấy block bắt đầu gửi để tí nữa chỉ check từ block này trở đi
	startBlockNum, err := rpcClient.GetBlockNumber()
	if err != nil {
		fmt.Printf("⚠️ Không thể lấy block hiện tại từ RPC: %v\n", err)
	} else {
		fmt.Printf("📌 Block hiện tại trước khi gửi: %d\n", startBlockNum)
	}

	// =====================================================================
	// BƯỚC 2: GỌI ĐĂNG KÝ BLS CHO TẤT CẢ VÍ TRƯỚC (Concurrently)
	// =====================================================================
	fmt.Printf("\n[2] Đang đăng ký BLS cho %d ví mới (Async)...\n", numWallets)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 500) // Giới hạn 500 goroutines đồng thời
	successCount := 0
	var mu sync.Mutex

	expectedTxHashes := make(map[string]bool)

	sendStartTime := time.Now()

	for i, key := range generatedKeys {
		wg.Add(1)
		sem <- struct{}{}

		go func(k GeneratedKey, idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			client := clientPool[idx%len(clientPool)]
			// Sử dụng hàm RegisterBlsForAccountAsync để gửi tx đi không chờ receipt
			chainIdStr := appCfg.ChainId

			txHash, err := client.RegisterBlsForAccountAsync(k.PrivateKey, appCfg.PublicKeyBLS, chainIdStr)
			if err != nil {
				fmt.Printf("❌ Ví %d: Lỗi gửi tx setBlsPublicKey: %v\n", k.Index, err)
			} else {
				mu.Lock()
				successCount++
				expectedTxHashes[strings.ToLower(txHash.Hex())] = true
				if successCount%1000 == 0 || successCount == numWallets {
					fmt.Printf("   ✅ Đã gửi tx đăng ký BLS %d/%d ví (Last Tx: %s)\n", successCount, numWallets, txHash.Hex())
				}
				mu.Unlock()
			}
		}(key, i)
	}
	wg.Wait()
	
	sendDuration := time.Since(sendStartTime)
	injectionTps := float64(successCount) / sendDuration.Seconds()
	fmt.Printf("✅ Đã GỬI xong tx đăng ký BLS (%d/%d thành công). Thời gian: %v (%.2f tx/s). Bắt đầu chờ block...\n", successCount, numWallets, sendDuration, injectionTps)

	if len(expectedTxHashes) > 0 {
		fmt.Printf("📡 Đang kết nối RPC %s để kiểm tra %d giao dịch...\n", rpcHost, len(expectedTxHashes))
		lastBlockNum := startBlockNum
		totalConfirmed := 0
		maxWait := 500 * time.Second
		startTime := time.Now()
		
		var firstTxBlockTime time.Time
		var lastTxBlockTime time.Time
		firstBlockNum := uint64(0)
		lastConfirmedBlockNum := uint64(0)

		for len(expectedTxHashes) > 0 {
			if time.Since(startTime) > maxWait {
				fmt.Printf("\n❌ Hết thời gian chờ (%s)! Còn %d giao dịch chưa được confirm.\n", maxWait, len(expectedTxHashes))
				break
			}

			time.Sleep(50 * time.Millisecond)
			currentBlockNum, err := rpcClient.GetBlockNumber()
			if err != nil {
				fmt.Printf("\r❌ Lỗi kết nối RPC (%s): %v          ", rpcHost, err)
				continue
			}

			if currentBlockNum <= lastBlockNum {
				// fmt.Printf("\r   ⏳ Đang chờ block mới (hiện tại: %d, time: %v)...  ", lastBlockNum, time.Since(startTime).Round(time.Second))
				continue
			}

			newConfirms := 0
			for bn := lastBlockNum + 1; bn <= currentBlockNum; bn++ {
				blk, err := rpcClient.GetBlockByNumber(bn)
				if err == nil && blk != nil {
					blockHasOurTx := false
					for _, hash := range blk.Transactions {
						hashLower := strings.ToLower(hash)
						if expectedTxHashes[hashLower] {
							delete(expectedTxHashes, hashLower)
							newConfirms++
							blockHasOurTx = true
						}
					}
					if blockHasOurTx {
						if firstTxBlockTime.IsZero() {
							firstTxBlockTime = time.UnixMilli(int64(blk.Timestamp))
							firstBlockNum = bn
						}
						lastTxBlockTime = time.UnixMilli(int64(blk.Timestamp))
						lastConfirmedBlockNum = bn
					}
				}
			}

			if newConfirms > 0 {
				totalConfirmed += newConfirms
				fmt.Printf("\r   📡 Block %d: Đã confirm %d/%d giao dịch BLS...   \n", currentBlockNum, totalConfirmed, successCount)
			} else {
				fmt.Printf("\r   📡 Block %d: Đã check (chưa thấy tx BLS nào)...   ", currentBlockNum)
			}
			lastBlockNum = currentBlockNum
		}
		
		e2eDuration := time.Since(startTime)
		e2eTps := float64(totalConfirmed) / e2eDuration.Seconds()
		
		var onChainTps float64
		var onChainDuration time.Duration
		if !firstTxBlockTime.IsZero() && !lastTxBlockTime.IsZero() {
			onChainDuration = lastTxBlockTime.Sub(firstTxBlockTime)
			if onChainDuration > 0 {
				onChainTps = float64(totalConfirmed) / onChainDuration.Seconds()
			}
		}

		fmt.Printf("\n═══════════════════════════════════════════════════\n")
		fmt.Printf("  📊 KẾT QUẢ ĐO TPS (BLS REGISTRATION)\n")
		fmt.Printf("═══════════════════════════════════════════════════\n")
		fmt.Printf("  🧱 Start Block:          %d\n", firstBlockNum)
		fmt.Printf("  🧱 End Block:            %d\n", lastConfirmedBlockNum)
		fmt.Printf("  📤 Total TXs sent:       %d\n", successCount)
		fmt.Printf("  🚀 Injection TPS:        %.0f tx/s\n", injectionTps)
		fmt.Printf("  ⏱️  Injection time:       %s\n", sendDuration.Round(time.Millisecond))
		fmt.Printf("  ─────────────────────────────────────────────────\n")
		fmt.Printf("  📥 TX in blocks:         %d\n", totalConfirmed)
		fmt.Printf("  📊 End-to-End TPS:       ~%.0f tx/s\n", e2eTps)
		fmt.Printf("  ⏱️  End-to-End time:      %s\n", e2eDuration.Round(time.Millisecond))
		if onChainDuration > 0 {
			fmt.Printf("  📊 On-Chain Engine TPS:  ~%.0f tx/s (First ➡️ Last block commit)\n", onChainTps)
			fmt.Printf("  ⏱️  On-Chain Commit time: %s\n", onChainDuration.Round(time.Millisecond))
		} else {
			fmt.Printf("  📊 On-Chain Engine TPS:  N/A (All TXs confirmed in a single block)\n")
		}
		fmt.Printf("═══════════════════════════════════════════════════\n")

		if *traceFlag {
			traceStart := firstBlockNum
			if lastConfirmedBlockNum >= traceStart && traceStart > 0 {
				fmt.Printf("\n  📝 BLOCK PERFORMANCE TRACES (Blocks %d to %d)\n", traceStart, lastConfirmedBlockNum)
				fmt.Printf("  %-8s | %-6s | %-10s | %-10s | %-10s | %-8s | %-11s | %-10s | %-10s | %-10s | %-10s | %-10s | %-10s | %-10s | %-10s\n",
					"Block", "TXs", "WaitGo", "WaitRust", "Consensus", "RustFFI", "ClientBatch", "ProcessTX", "CalcRoots", "BlockData", "Mapping", "CommitMem", "SaveDB", "Total", "GCPause")
				fmt.Printf("  %s\n", strings.Repeat("-", 190))

				traces, err := rpcClient.GetBlockTraces(traceStart, lastConfirmedBlockNum)
				if err != nil {
					fmt.Printf("  ❌ Could not fetch block traces: %v\n", err)
				} else {
					for _, t := range traces {
						realTotalUs := float64(t.WaitGoUs) +
							float64(t.WaitRustUs) +
							float64(t.ConsensusDurationUs) +
							float64(t.ClientBatchProcessingUs) +
							float64(t.ProcessTxsDurationUs) +
							float64(t.TotalBlockDurationUs)

						fmt.Printf("  %-8d | %-6d | %-8.1fms | %-8.1fms | %-8.1fms | %-6.1fms | %-9.1fms | %-8.1fms | %-8.1fms | %-8.2fms | %-8.2fms | %-8.1fms | %-8.1fms | %-8.1fms | %-8.1fms\n",
							t.BlockNumber, t.TxCount,
							float64(t.WaitGoUs)/1000.0,
							float64(t.WaitRustUs)/1000.0,
							float64(t.ConsensusDurationUs)/1000.0,
							float64(t.RustDeliveryFFIDurationUs)/1000.0,
							float64(t.ClientBatchProcessingUs)/1000.0,
							float64(t.ProcessTxsDurationUs)/1000.0,
							float64(t.Phase1TotalDurationUs)/1000.0,
							float64(t.BlockDataDurationUs)/1000.0,
							float64(t.MappingDurationUs)/1000.0,
							float64(t.CommitMemoryDurationUs)/1000.0,
							float64(t.SaveDBDurationUs)/1000.0,
							realTotalUs/1000.0,
							float64(t.GCPauseUs)/1000.0)
					}
				}
				fmt.Printf("═══════════════════════════════════════════════════\n")
			}
		}
	}

	// =====================================================================
	// BƯỚC 3: CHUYỂN TIỀN VÀO CÁC VÍ VỪA TẠO (Sequentially)
	// =====================================================================
	if *skipFundFlag {
		fmt.Println("\n==================================================")
		fmt.Println("⏭️  BỎ QUA BƯỚC CHUYỂN TIỀN (skip_fund=true)")
		fmt.Println("==================================================")
		fmt.Println("🎉 TẤT CẢ QUÁ TRÌNH ĐÃ HOÀN TẤT!")
		return
	}

	fmt.Printf("\n[3] Đang tiến hành chuyển tiền vào %d ví mới...\n", numWallets)
	fundSuccess := 0
	var fundMu sync.Mutex
	var fundWg sync.WaitGroup

	// Tạo channel chứa các ví cần nhận tiền
	jobs := make(chan GeneratedKey, len(generatedKeys))
	for _, key := range generatedKeys {
		jobs <- key
	}
	close(jobs)

	// Danh sách các ví sẽ làm nhiệm vụ đi gửi tiền
	fundingWallets := []string{cfg.Address().Hex()}
	if len(appCfg.WalletPool) > 0 {
		fundingWallets = appCfg.WalletPool
	}

	// Mở mỗi ví gửi tiền thành 1 worker (như vậy 1 ví không bao giờ bị dùng song song -> tránh lỗi nonce)
	// Các worker sẽ tranh nhau lấy job (ví nhận) từ trong channel để xử lý
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
					fundSuccess++
					if fundSuccess%100 == 0 || fundSuccess == numWallets {
						fmt.Printf("   ✅ Đã chuyển tiền thành công %d/%d ví (Tx: %s)\n", fundSuccess, numWallets, receipt.TransactionHash().Hex())
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
	fmt.Printf("✅ Hoàn thành chuyển tiền (%d/%d thành công).\n", fundSuccess, numWallets)
	fmt.Println("\n==================================================")
	fmt.Println("🎉 TẤT CẢ QUÁ TRÌNH ĐÃ HOÀN TẤT!")
}
