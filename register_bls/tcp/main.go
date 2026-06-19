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

	client_tcp "tool-test/pkg/client-tcp"
	tcp_config "tool-test/pkg/client-tcp/config"
	tx_models "tool-test/pkg/client-tcp/models"
	tx_helper "tool-test/pkg/client-tcp/utils/tx_helper"
	"tool-test/pkg/logger"
	pb "tool-test/pkg/proto"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	e_types "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type AppConfig struct {
	WalletPool     []string `json:"wallet_pool"`
	PublicKeyBLS   string   `json:"public_key_bls"`
	TransferAmount string   `json:"transfer_amount"`
}

type GeneratedKey struct {
	Index      int    `json:"index"`
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
}

func main() {
	countFlag := flag.Int("count", 1, "Số lượng ví cần tạo")
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

	if len(appCfg.WalletPool) == 0 {
		log.Fatalf("❌ wallet_pool trống, không có địa chỉ nào để chuyển tiền")
	}
	if appCfg.PublicKeyBLS == "" {
		log.Fatalf("❌ Không có public_key_bls nào để đăng ký")
	}

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
	fmt.Printf("🔌 Connecting to TCP: %s\n", cfg.ConnectionAddress())
	tcpClient, err := client_tcp.NewClient(cfg)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối TCP: %v", err)
	}
	time.Sleep(1 * time.Second)

	chainIDStr, err := tcpClient.RpcGetChainId()
	if err != nil {
		log.Fatalf("❌ Lỗi lấy ChainID: %v", err)
	}
	chainID := new(big.Int)
	chainID.SetString(strings.TrimPrefix(chainIDStr, "0x"), 16)
	fmt.Printf("✅ Connected to Chain ID: %s\n", chainID.String())

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

	outputPath := "/home/nhat/Workspace/go-project/metanode-suite/test_tps/gen_spam_keys/generated_keys.json"
	os.MkdirAll(filepath.Dir(outputPath), 0755)
	
	fileBytes, err := json.MarshalIndent(generatedKeys, "", "  ")
	if err != nil {
		log.Fatalf("❌ Lỗi format JSON output: %v", err)
	}
	if err := os.WriteFile(outputPath, fileBytes, 0644); err != nil {
		log.Fatalf("❌ Lỗi ghi file output: %v", err)
	}
	fmt.Printf("✅ Đã lưu %d ví vào %s\n", numWallets, outputPath)

	// Chuẩn bị payload setBlsPublicKey
	contractAddr := common.HexToAddress("0x00000000000000000000000000000000D844bb55")
	abiJSON := `[{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"setBlsPublicKey","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"}]`
	parsedABI, _ := abi.JSON(strings.NewReader(abiJSON))

	blsKeyBytes, err := hexutil.Decode("0x" + strings.TrimPrefix(appCfg.PublicKeyBLS, "0x"))
	if err != nil {
		log.Fatalf("❌ Lỗi decode BLS key: %v", err)
	}
	blsData, _ := parsedABI.Pack("setBlsPublicKey", blsKeyBytes)

	// =====================================================================
	// BƯỚC 2: GỌI ĐĂNG KÝ BLS CHO TẤT CẢ VÍ TRƯỚC (Concurrently)
	// =====================================================================
	fmt.Printf("\n[2] Đang đăng ký BLS cho %d ví mới...\n", numWallets)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 50) // Giới hạn 50 goroutines đồng thời
	successCount := 0
	var mu sync.Mutex

	for _, key := range generatedKeys {
		wg.Add(1)
		sem <- struct{}{}

		go func(k GeneratedKey) {
			defer wg.Done()
			defer func() { <-sem }()

			privKey, _ := crypto.HexToECDSA(k.PrivateKey)
			newNonce, _ := tcpClient.RpcGetPendingNonce(common.HexToAddress(k.Address))

			blsTx := e_types.NewTransaction(newNonce, contractAddr, big.NewInt(0), 100000, big.NewInt(2000000000), blsData)
			signedBlsTx, err := e_types.SignTx(blsTx, e_types.LatestSignerForChainID(chainID), privKey)
			if err != nil {
				fmt.Printf("❌ Ví %d: Lỗi ký tx setBlsPublicKey: %v\n", k.Index, err)
				return
			}

			blsTxBytes, _ := signedBlsTx.MarshalBinary()
			txHash, _, err := tcpClient.RpcSendRawTransactionWithReceipt(hexutil.Encode(blsTxBytes))
			if err != nil {
				fmt.Printf("❌ Ví %d: Lỗi gửi tx setBlsPublicKey: %v\n", k.Index, err)
			} else {
				mu.Lock()
				successCount++
				if successCount%100 == 0 || successCount == numWallets {
					fmt.Printf("   ✅ Đã đăng ký BLS thành công %d/%d ví (Last Tx: %s)\n", successCount, numWallets, txHash)
				}
				mu.Unlock()
			}
		}(key)
	}
	wg.Wait()
	fmt.Printf("✅ Đăng ký xong BLS (%d/%d thành công).\n", successCount, numWallets)

	// =====================================================================
	// BƯỚC 3: CHUYỂN TIỀN VÀO CÁC VÍ VỪA TẠO (Sequentially)
	// =====================================================================
	fmt.Printf("\n[3] Đang tiến hành chuyển tiền vào %d ví mới...\n", numWallets)
	
	funderIndex := 0
	fundSuccess := 0

	for _, key := range generatedKeys {
		funderAddrHex := appCfg.WalletPool[funderIndex%len(appCfg.WalletPool)]
		funderIndex++

		fromAddress := common.HexToAddress(funderAddrHex)
		toAddress := common.HexToAddress(key.Address)

		receipt, err := tx_helper.SendTransaction(
			"NativeTransfer",
			tcpClient,
			cfg,
			toAddress,
			fromAddress,
			nil, // data
			&tx_models.TxOptions{Amount: transferAmount},
		)

		if err != nil {
			fmt.Printf("❌ Ví %d (%s): Lỗi chuyển tiền: %v\n", key.Index, toAddress.Hex(), err)
			continue
		}

		if receipt != nil && (receipt.Status() == pb.RECEIPT_STATUS_RETURNED || receipt.Status() == pb.RECEIPT_STATUS_HALTED) {
			fundSuccess++
			if fundSuccess%100 == 0 || fundSuccess == numWallets {
				fmt.Printf("   ✅ Đã chuyển tiền thành công %d/%d ví (Tx: %s)\n", fundSuccess, numWallets, receipt.TransactionHash().Hex())
			}
		} else {
			fmt.Printf("❌ Ví %d: Chuyển tiền thất bại (Status: %s)\n", key.Index, receipt.Status().String())
		}
	}

	fmt.Printf("✅ Hoàn thành chuyển tiền (%d/%d thành công).\n", fundSuccess, numWallets)
	fmt.Println("\n==================================================")
	fmt.Println("🎉 TẤT CẢ QUÁ TRÌNH ĐÃ HOÀN TẤT!")
}
