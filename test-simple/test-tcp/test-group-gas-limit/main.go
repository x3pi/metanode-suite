package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	client_tcp "tool-test/pkg/client-tcp"
	tcp_config "tool-test/pkg/client-tcp/config"
	tx_models "tool-test/pkg/client-tcp/models"
	tx_helper "tool-test/pkg/client-tcp/utils/tx_helper"
	"tool-test/pkg/logger"
)

func main() {
	logger.SetConfig(&logger.LoggerConfig{
		Flag:    logger.FLAG_INFO,
		Outputs: []*os.File{os.Stdout},
	})

	fmt.Println("==================================================")
	fmt.Println("🚀 START TEST: GROUP GAS LIMIT EXCEED")
	fmt.Println("==================================================")

	// 1. Load config
	cfgRaw, err := tcp_config.LoadConfig("config-local.json")
	if err != nil {
		log.Fatalf("❌ Lỗi đọc file config: %v", err)
	}
	cfg := cfgRaw.(*tcp_config.ClientConfig)

	// 2. Connect to TCP
	fmt.Printf("Connecting to TCP: %s\n", cfg.ConnectionAddress())
	tcpClient, err := client_tcp.NewClient(cfg)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối TCP: %v", err)
	}
	time.Sleep(1 * time.Second)

	// Đọc thêm 2 ví wallet_1 và wallet_2 từ file json
	fileBytes, _ := os.ReadFile("config-local.json")
	var rawCfg map[string]interface{}
	json.Unmarshal(fileBytes, &rawCfg)

	fromAddresses := []common.Address{
		common.HexToAddress(cfg.ParentAddress),
		common.HexToAddress(rawCfg["wallet_1"].(string)),
		common.HexToAddress(rawCfg["wallet_2"].(string)),
	}

	// Đích đến chung của cả 3 giao dịch là một địa chỉ ngẫu nhiên.
	// Tuyệt đối không dùng fromAddresses[0] để tránh lỗi Self-Transfer (SetAccountType)
	toAddress := common.HexToAddress("0x1111111111111111111111111111111111111111")

	amount := big.NewInt(1) // Gửi 1 wei

	// MAX_GROUP_GAS trên chain của bạn là 9999999999999999999 (gần 10^19).
	// Ta gửi 3 giao dịch, mỗi giao dịch set MaxGas = 6000000000000000000 (6 * 10^18).
	// -> Tx 1 (6*10^18) <= MAX_GROUP_GAS (Vào nhóm).
	// -> Tx 2 (12*10^18) > MAX_GROUP_GAS (Bị văng ra excludedItems!).
	// -> Tx 3 (18*10^18) > MAX_GROUP_GAS (Bị văng ra excludedItems!).

	var wg sync.WaitGroup

	var hashes []string
	var mu sync.Mutex

	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func(txIndex int) {
			defer wg.Done()
			
			fromAddr := fromAddresses[txIndex-1]

			// Tạo Payload rỗng (Transfer thuần)
			var payloadData []byte

			options := &tx_models.TxOptions{
				Amount: amount,
				MaxGas: 45,
			}

			fmt.Printf("▶️  [Tx %d] Đang gửi giao dịch (MaxGas = 45)...\n", txIndex)
			receipt, err := tx_helper.SendTransaction(
				fmt.Sprintf("Transfer_%d", txIndex),
				tcpClient,
				cfg,
				toAddress,
				fromAddr,
				payloadData,
				options,
			)

			if err != nil {
				fmt.Printf("❌ [Tx %d] Gửi lỗi: %v\n", txIndex, err)
			} else if receipt != nil {
				hash := receipt.TransactionHash().Hex()
				fmt.Printf("✅ [Tx %d] THÀNH CÔNG! Gas used: %d, Hash: %s\n", txIndex, receipt.GasUsed(), hash)
				
				mu.Lock()
				hashes = append(hashes, hash)
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	fmt.Println("==================================================")
	fmt.Println("🎉 HOÀN THẤT GỬI 3 GIAO DỊCH!")
	
	fmt.Println("⏳ Đợi 3 giây để Node đóng Block...")
	time.Sleep(3 * time.Second)
	
	fmt.Println("🔍 KIỂM TRA BLOCK CỦA TỪNG GIAO DỊCH (QUA RPC):")
	
	rpcClient, err := ethclient.Dial("http://192.168.1.234:8545")
	if err != nil {
		fmt.Printf("❌ Lỗi kết nối RPC: %v\n", err)
	} else {
		for _, hashStr := range hashes {
			hash := common.HexToHash(hashStr)
			receipt, err := rpcClient.TransactionReceipt(context.Background(), hash)
			if err != nil {
				fmt.Printf("⚠️ [Tx %s] Lỗi lấy receipt qua RPC: %v (có thể đang ở excludedItems)\n", hashStr, err)
			} else if receipt != nil {
				fmt.Printf("✅ [Tx %s] nằm trong Block: %s (Block Hash: %s)\n", hashStr, receipt.BlockNumber.String(), receipt.BlockHash.Hex())
			}
		}
	}
	fmt.Println("==================================================")
}
