package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type Config struct {
	RPCUrl     string   `json:"rpc_url"`
	RPCUrls    []string `json:"rpc_urls"`
	PrivateKey string   `json:"private_key"`
	ChainID    int64    `json:"chain_id"`
}

type SavedCheckpoint struct {
	BlockA        uint64 `json:"block_a"`
	SavedBalanceA string `json:"saved_balance_a"`
	SavedNonceA   uint64 `json:"saved_nonce_a"`
	FromAddress   string `json:"from_address"`
	ToAddress     string `json:"to_address"`
	ChainID       int64  `json:"chain_id"`
}

type HistoryRecord struct {
	Timestamp       string `json:"timestamp"`
	BlockA          uint64 `json:"block_a"`
	SavedBalanceA   string `json:"saved_balance_a"`
	QueriedBalanceA string `json:"queried_balance_a"`
	SavedNonceA     uint64 `json:"saved_nonce_a"`
	QueriedNonceA   uint64 `json:"queried_nonce_a"`
	BlockB          uint64 `json:"block_b"`
	BalanceB        string `json:"balance_b"`
	NonceB          uint64 `json:"nonce_b"`
	IsValid         bool   `json:"is_valid"`
	ErrorDetails    string `json:"error_details,omitempty"`
}

type FailoverClient struct {
	urls        []string
	activeIdx   int
	rpcCli      *rpc.Client
	ethCli      *ethclient.Client
	privateKey  *ecdsa.PrivateKey
	chainId     int64
	flagExclude string
}

func NewFailoverClient(urls []string, privateKey *ecdsa.PrivateKey, chainId int64, flagExclude string) (*FailoverClient, error) {
	fc := &FailoverClient{
		urls:        urls,
		activeIdx:   0,
		privateKey:  privateKey,
		chainId:     chainId,
		flagExclude: flagExclude,
	}
	err := fc.reconnect()
	if err != nil {
		return nil, err
	}
	return fc, nil
}

func isExcluded(url string, excludeStr string) bool {
	if excludeStr == "" {
		return false
	}
	parts := strings.Split(excludeStr, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(url, p) {
			return true
		}
		nodePortMap := map[string]string{
			"0": "8545",
			"1": "8547",
			"2": "8548",
			"3": "8549",
			"4": "8550",
		}
		if port, ok := nodePortMap[p]; ok {
			if strings.Contains(url, port) {
				return true
			}
		}
	}
	return false
}

func getExcludedNodes(flagExclude string) string {
	data, err := os.ReadFile("/tmp/MTN_EXCLUDE_NODES")
	if err == nil {
		fileStr := strings.TrimSpace(string(data))
		if fileStr != "" {
			if flagExclude != "" {
				return flagExclude + "," + fileStr
			}
			return fileStr
		}
	}
	return flagExclude
}

func (fc *FailoverClient) reconnect() error {
	if fc.rpcCli != nil {
		fc.rpcCli.Close()
		fc.rpcCli = nil
		fc.ethCli = nil
	}

	excludeStr := getExcludedNodes(fc.flagExclude)

	startIdx := fc.activeIdx
	for {
		url := fc.urls[fc.activeIdx]

		if isExcluded(url, excludeStr) {
			fmt.Printf("🚫 RPC %s đang nằm trong danh sách loại trừ (%s). Bỏ qua...\n", url, excludeStr)
			fc.activeIdx = (fc.activeIdx + 1) % len(fc.urls)
			if fc.activeIdx == startIdx {
				return fmt.Errorf("tất cả các địa chỉ RPC đều bị loại trừ hoặc không kết nối được")
			}
			continue
		}

		fmt.Printf("🔌 Đang kết nối tới RPC: %s...\n", url)
		rpcClient, err := rpc.Dial(url)
		if err == nil {
			ethCli := ethclient.NewClient(rpcClient)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			var latestBlockHex string
			errBlock := rpcClient.CallContext(ctx, &latestBlockHex, "eth_blockNumber")
			cancel()
			if errBlock == nil {
				fc.rpcCli = rpcClient
				fc.ethCli = ethCli
				fmt.Printf("✅ Đã kết nối thành công tới RPC: %s\n", url)
				return nil
			}
			rpcClient.Close()
		}

		fmt.Printf("⚠️  Lỗi kết nối tới RPC %s. Đang chuyển sang RPC tiếp theo...\n", url)
		fc.activeIdx = (fc.activeIdx + 1) % len(fc.urls)
		if fc.activeIdx == startIdx {
			return fmt.Errorf("tất cả các địa chỉ RPC trong cấu hình đều không kết nối được")
		}
	}
}

func (fc *FailoverClient) execute(fn func(ethCli *ethclient.Client, rpcCli *rpc.Client) error) error {
	var err error
	for attempts := 0; attempts < len(fc.urls); attempts++ {
		excludeStr := getExcludedNodes(fc.flagExclude)
		activeUrl := fc.urls[fc.activeIdx]

		if isExcluded(activeUrl, excludeStr) {
			fmt.Printf("⚠️  RPC hiện tại (%s) bị đưa vào danh sách loại trừ (%s). Đang chuyển node...\n", activeUrl, excludeStr)
			fc.activeIdx = (fc.activeIdx + 1) % len(fc.urls)
			reconErr := fc.reconnect()
			if reconErr != nil {
				return reconErr
			}
			continue
		}

		err = fn(fc.ethCli, fc.rpcCli)
		if err == nil {
			return nil
		}

		fmt.Printf("⚠️  Lỗi khi gọi RPC (URL: %s): %v. Đang thực hiện kết nối lại / chuyển node...\n", fc.urls[fc.activeIdx], err)
		fc.activeIdx = (fc.activeIdx + 1) % len(fc.urls)
		reconErr := fc.reconnect()
		if reconErr != nil {
			return reconErr
		}
	}
	return err
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func saveRecord(record HistoryRecord) {
	filename := "history_records.json"
	var records []HistoryRecord

	data, err := os.ReadFile(filename)
	if err == nil {
		json.Unmarshal(data, &records)
	}

	records = append(records, record)

	if len(records) > 1000 {
		records = records[len(records)-1000:]
	}

	updatedData, err := json.MarshalIndent(records, "", "  ")
	if err == nil {
		os.WriteFile(filename, updatedData, 0644)
	}
}

func sendTxAndWait(fc *FailoverClient, fromAddress common.Address, toAddress common.Address) (uint64, error) {
	var blockNum uint64
	err := fc.execute(func(ethCli *ethclient.Client, rpcCli *rpc.Client) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		nonce, err := ethCli.PendingNonceAt(ctx, fromAddress)
		if err != nil {
			return err
		}
		gasPrice, err := ethCli.SuggestGasPrice(ctx)
		if err != nil {
			return err
		}

		tx := types.NewTransaction(nonce, toAddress, big.NewInt(1), 21000, gasPrice, nil)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(fc.chainId)), fc.privateKey)
		if err != nil {
			return err
		}

		err = ethCli.SendTransaction(ctx, signedTx)
		if err != nil {
			return err
		}
		fmt.Printf("   🚀 Đã gửi Tx. Hash: %s\n", signedTx.Hash().Hex())

		for {
			ctxReceipt, cancelReceipt := context.WithTimeout(context.Background(), 10*time.Second)
			receipt, errReceipt := ethCli.TransactionReceipt(ctxReceipt, signedTx.Hash())
			cancelReceipt()
			if errReceipt == nil {
				if receipt.Status == 1 {
					blockNum = receipt.BlockNumber.Uint64()
					return nil
				}
				return fmt.Errorf("Tx Reverted")
			} else if errReceipt != ethereum.NotFound {
				return errReceipt
			}
			time.Sleep(1 * time.Second)
		}
	})
	return blockNum, err
}

func runHistoryCheck(fc *FailoverClient, fromAddress, toAddress common.Address, waitBlocks uint64) bool {
	var blockA, blockB uint64
	var savedBalanceA *big.Int
	var savedNonceA uint64

	bA, err := sendTxAndWait(fc, fromAddress, toAddress)
	if err != nil {
		fmt.Printf("Gửi Tx thất bại: %v\n", err)
		return false
	}
	blockA = bA
	fmt.Printf("✅ Giao dịch đã được mine tại Block A: %d\n", blockA)

	blockAHex := hexutil.EncodeUint64(blockA)
	err = fc.execute(func(ethCli *ethclient.Client, rpcClient *rpc.Client) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var savedBalanceAHex string
		err := rpcClient.CallContext(ctx, &savedBalanceAHex, "eth_getBalance", fromAddress, blockAHex)
		if err != nil {
			return err
		}
		savedBalanceA, _ = hexutil.DecodeBig(savedBalanceAHex)

		var savedNonceAHex string
		err = rpcClient.CallContext(ctx, &savedNonceAHex, "eth_getTransactionCount", fromAddress, blockAHex)
		if err != nil {
			return err
		}
		savedNonceA, _ = hexutil.DecodeUint64(savedNonceAHex)
		return nil
	})
	if err != nil {
		fmt.Printf("Lấy saved state tại Block A thất bại: %v\n", err)
		return false
	}
	fmt.Printf("   [Saved] Tiền lúc Block A: %v | Nonce: %d\n", savedBalanceA, savedNonceA)

	if waitBlocks > 0 {
		blockB = blockA + waitBlocks
		fmt.Printf("\n⏳ Chế độ Test Pruning: Đang đợi mạng lưới chạy tới Block B (%d)...\n", blockB)
		for {
			var latestBlockHex string
			err = fc.execute(func(ethCli *ethclient.Client, rpcClient *rpc.Client) error {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				return rpcClient.CallContext(ctx, &latestBlockHex, "eth_blockNumber")
			})
			if err == nil {
				latestBlock, _ := hexutil.DecodeUint64(latestBlockHex)
				if latestBlock >= blockB {
					fmt.Printf("✅ Mạng lưới đã đạt Block %d!\n", latestBlock)
					break
				}
				fmt.Printf("   Đang ở block %d, cần đợi tới %d (Đang bắn Tx để kích block)...\n", latestBlock, blockB)

				_, errTx := sendTxAndWait(fc, fromAddress, toAddress)
				if errTx != nil {
					fmt.Printf("   ⚠️ Lỗi khi bắn Tx kích block: %v\n", errTx)
					time.Sleep(2 * time.Second)
				}
			} else {
				time.Sleep(2 * time.Second)
			}
		}
	} else {
		fmt.Println("\n2. Đang tạo giao dịch (Tx 2) để làm thay đổi số dư so với Block A...")
		bB, err := sendTxAndWait(fc, fromAddress, toAddress)
		if err != nil {
			fmt.Printf("Gửi Tx 2 thất bại: %v\n", err)
			return false
		}
		blockB = bB
		fmt.Printf("✅ Tx 2 đã được mine tại Block B: %d\n", blockB)
	}

	fmt.Println("\n=====================================================")
	fmt.Println("BẮT ĐẦU KIỂM TRA LỊCH SỬ BẰNG RPC TẠI 2 MỐC BLOCK KHÁC NHAU")
	fmt.Println("=====================================================")

	blockBHex := hexutil.EncodeUint64(blockB)
	var balanceA *big.Int
	var balanceB *big.Int
	var nonceA, nonceB uint64

	err = fc.execute(func(ethCli *ethclient.Client, rpcClient *rpc.Client) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var balanceAHex string
		err := rpcClient.CallContext(ctx, &balanceAHex, "eth_getBalance", fromAddress, blockAHex)
		if err != nil {
			return err
		}
		balanceA, _ = hexutil.DecodeBig(balanceAHex)

		var balanceBHex string
		err = rpcClient.CallContext(ctx, &balanceBHex, "eth_getBalance", fromAddress, blockBHex)
		if err != nil {
			return err
		}
		balanceB, _ = hexutil.DecodeBig(balanceBHex)

		var nonceAHex string
		err = rpcClient.CallContext(ctx, &nonceAHex, "eth_getTransactionCount", fromAddress, blockAHex)
		if err != nil {
			return err
		}
		nonceA, _ = hexutil.DecodeUint64(nonceAHex)

		var nonceBHex string
		err = rpcClient.CallContext(ctx, &nonceBHex, "eth_getTransactionCount", fromAddress, blockBHex)
		if err != nil {
			return err
		}
		nonceB, _ = hexutil.DecodeUint64(nonceBHex)

		return nil
	})
	if err != nil {
		fmt.Printf("Lấy dữ liệu so sánh thất bại: %v\n", err)
		return false
	}

	fmt.Printf("💰 eth_getBalance tại Block A (Lịch sử: %d): %v\n", blockA, balanceA)
	fmt.Printf("💰 eth_getBalance tại Block B (Hiện tại: %d): %v\n", blockB, balanceB)
	fmt.Printf("\n🔢 eth_getTransactionCount tại Block A (Lịch sử: %d): %d\n", blockA, nonceA)
	fmt.Printf("🔢 eth_getTransactionCount tại Block B (Hiện tại: %d): %d\n", blockB, nonceB)

	hasError := false
	var errDetails []string

	if balanceA.Cmp(balanceB) == 0 {
		errDetails = append(errDetails, "Số dư lịch sử (A) và hiện tại (B) GIỐNG HỆT NHAU")
		hasError = true
	}

	if balanceA.Cmp(savedBalanceA) != 0 {
		errDetails = append(errDetails, fmt.Sprintf("Số dư lịch sử get lại (%v) KHÁC với số dư thực tế lúc đó (%v)", balanceA, savedBalanceA))
		hasError = true
	}

	if nonceA == nonceB {
		errDetails = append(errDetails, "Nonce lịch sử và hiện tại GIỐNG HỆT NHAU")
		hasError = true
	}

	if nonceA != savedNonceA {
		errDetails = append(errDetails, fmt.Sprintf("Nonce lịch sử (%d) KHÁC với Nonce thực tế lúc đó (%d)", nonceA, savedNonceA))
		hasError = true
	}

	record := HistoryRecord{
		Timestamp:       time.Now().Format(time.RFC3339),
		BlockA:          blockA,
		SavedBalanceA:   savedBalanceA.String(),
		QueriedBalanceA: balanceA.String(),
		SavedNonceA:     savedNonceA,
		QueriedNonceA:   nonceA,
		BlockB:          blockB,
		BalanceB:        balanceB.String(),
		NonceB:          nonceB,
		IsValid:         !hasError,
	}

	if hasError {
		record.ErrorDetails = strings.Join(errDetails, "; ")
		reason := fmt.Sprintf("LỖI LỊCH SỬ STATE tại Block %d: %s", blockA, record.ErrorDetails)
		os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)
		fmt.Printf("\n🚨 %s\n", reason)
	}

	saveRecord(record)

	return !hasError
}

func getTargetNodeURL(cfg *Config, targetNode string) (string, error) {
	if strings.HasPrefix(targetNode, "http://") || strings.HasPrefix(targetNode, "https://") {
		return targetNode, nil
	}

	nodePortMap := map[string]string{
		"0": "8545",
		"1": "8547",
		"2": "8548",
		"3": "8549",
		"4": "8550",
	}

	if port, ok := nodePortMap[targetNode]; ok {
		for _, url := range cfg.RPCUrls {
			if strings.Contains(url, ":"+port) || strings.Contains(url, "/"+port) {
				return url, nil
			}
		}
		return fmt.Sprintf("http://127.0.0.1:%s", port), nil
	}

	return "", fmt.Errorf("không xác định được URL cho target-node: %s", targetNode)
}

func main() {
	configFlag := flag.String("config", "config-local.json", "Đường dẫn file cấu hình (ví dụ: config-server.json)")
	waitBlocksFlag := flag.Uint64("wait", 0, "Số block cần đợi mạng lưới sinh ra trước khi test mốc B (để test Pruning)")
	loopFlag := flag.Bool("loop", false, "Chạy lặp vô hạn để kiểm tra liên tục (chỉ chạy trong chế độ loop mặc định)")
	excludeFlag := flag.String("exclude", "", "Danh sách node ID hoặc cổng RPC cần loại trừ (ngăn cách bởi dấu phẩy, VD: 1,2 hoặc 8547)")
	
	// Các flag mới cho cơ chế Save/Verify lịch sử
	actionFlag := flag.String("action", "loop", "Hành động thực hiện: loop, save, verify")
	fileFlag := flag.String("file", "pending_check.json", "Đường dẫn file JSON lưu trạng thái check")
	targetNodeFlag := flag.String("target-node", "", "Node ID hoặc cổng RPC cần kiểm tra (chỉ dùng cho hành động verify)")
	flag.Parse()

	cfg, err := loadConfig(*configFlag)
	if err != nil {
		log.Fatalf("Failed to load config %s: %v", *configFlag, err)
	}

	// Chuẩn bị ví
	privateKey, err := crypto.HexToECDSA(cfg.PrivateKey)
	if err != nil {
		log.Fatalf("Lỗi parse private key: %v", err)
	}
	publicKey := privateKey.Public()
	fromAddress := crypto.PubkeyToAddress(*publicKey.(*ecdsa.PublicKey))
	toAddress := common.HexToAddress("0x7c6f5e38E6d4457cDdE34134B15FCC04F64bf6bd")

	// Lấy danh sách rpc_url
	var urls []string
	if len(cfg.RPCUrls) > 0 {
		urls = cfg.RPCUrls
	} else if cfg.RPCUrl != "" {
		urls = []string{cfg.RPCUrl}
	} else {
		log.Fatalf("Không tìm thấy địa chỉ RPC nào trong cấu hình")
	}

	// Xử lý action "save"
	if *actionFlag == "save" {
		fmt.Println("=====================================================")
		fmt.Println("📥 HÀNH ĐỘNG: LƯU TRẠNG THÁI LỊCH SỬ (SAVE)")
		fmt.Printf("Gửi Tx từ ví: %s\n", fromAddress.Hex())
		fmt.Println("=====================================================")

		fc, err := NewFailoverClient(urls, privateKey, cfg.ChainID, *excludeFlag)
		if err != nil {
			log.Fatalf("Lỗi khởi tạo failover client: %v", err)
		}

		var blockA uint64
		var savedBalanceA *big.Int
		var savedNonceA uint64

		bA, err := sendTxAndWait(fc, fromAddress, toAddress)
		if err != nil {
			log.Fatalf("Gửi Tx thất bại: %v", err)
		}
		blockA = bA
		fmt.Printf("✅ Giao dịch đã được mine tại Block A: %d\n", blockA)

		blockAHex := hexutil.EncodeUint64(blockA)
		err = fc.execute(func(ethCli *ethclient.Client, rpcClient *rpc.Client) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var savedBalanceAHex string
			err := rpcClient.CallContext(ctx, &savedBalanceAHex, "eth_getBalance", fromAddress, blockAHex)
			if err != nil {
				return err
			}
			savedBalanceA, _ = hexutil.DecodeBig(savedBalanceAHex)

			var savedNonceAHex string
			err = rpcClient.CallContext(ctx, &savedNonceAHex, "eth_getTransactionCount", fromAddress, blockAHex)
			if err != nil {
				return err
			}
			savedNonceA, _ = hexutil.DecodeUint64(savedNonceAHex)
			return nil
		})
		if err != nil {
			log.Fatalf("Lấy saved state tại Block A thất bại: %v", err)
		}

		checkpoint := SavedCheckpoint{
			BlockA:        blockA,
			SavedBalanceA: savedBalanceA.String(),
			SavedNonceA:   savedNonceA,
			FromAddress:   fromAddress.Hex(),
			ToAddress:     toAddress.Hex(),
			ChainID:       cfg.ChainID,
		}

		data, err := json.MarshalIndent(checkpoint, "", "  ")
		if err != nil {
			log.Fatalf("Lỗi serialize checkpoint JSON: %v", err)
		}

		err = os.WriteFile(*fileFlag, data, 0644)
		if err != nil {
			log.Fatalf("Lỗi ghi file lưu trạng thái: %v", err)
		}

		fmt.Printf("🎉 Đã lưu thành công trạng thái vào file %s:\n", *fileFlag)
		fmt.Printf("   - Block A: %d\n", checkpoint.BlockA)
		fmt.Printf("   - Balance A: %s\n", checkpoint.SavedBalanceA)
		fmt.Printf("   - Nonce A: %d\n", checkpoint.SavedNonceA)
		os.Exit(0)
	}

	// Xử lý action "verify"
	if *actionFlag == "verify" {
		fmt.Println("=====================================================")
		fmt.Println("📤 HÀNH ĐỘNG: XÁC MINH TRẠNG THÁI LỊCH SỬ (VERIFY)")
		fmt.Printf("Đọc file trạng thái: %s\n", *fileFlag)
		if *targetNodeFlag == "" {
			log.Fatalf("LỖI: verify yêu cầu truyền flag -target-node để biết node cần kiểm tra")
		}
		fmt.Printf("Target Node: %s\n", *targetNodeFlag)
		fmt.Println("=====================================================")

		// Đọc file checkpoint
		data, err := os.ReadFile(*fileFlag)
		if err != nil {
			log.Fatalf("Lỗi đọc file trạng thái %s: %v", *fileFlag, err)
		}

		var checkpoint SavedCheckpoint
		if err := json.Unmarshal(data, &checkpoint); err != nil {
			log.Fatalf("Lỗi parse file trạng thái: %v", err)
		}

		targetURL, err := getTargetNodeURL(cfg, *targetNodeFlag)
		if err != nil {
			log.Fatalf("Lỗi xác định target node: %v", err)
		}

		fmt.Printf("🔌 Đang kết nối trực tiếp (Strict) tới RPC Node: %s...\n", targetURL)
		
		// Đợi node online và đồng bộ vượt qua Block A
		var rpcClient *rpc.Client
		
		maxRetries := 30
		for r := 1; r <= maxRetries; r++ {
			rpcClient, err = rpc.Dial(targetURL)
			if err == nil {
				
				// Kiểm tra chiều cao block hiện tại của node
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				var latestBlockHex string
				errBlock := rpcClient.CallContext(ctx, &latestBlockHex, "eth_blockNumber")
				cancel()
				
				if errBlock == nil {
					latestBlock, _ := hexutil.DecodeUint64(latestBlockHex)
					if latestBlock > checkpoint.BlockA {
						fmt.Printf("✅ Node đã online và đồng bộ tới Block %d (vượt qua Block A %d)\n", latestBlock, checkpoint.BlockA)
						break
					} else {
						fmt.Printf("⏳ Node đã online nhưng chiều cao block (%d) chưa vượt qua Block A (%d). Đang đợi...\n", latestBlock, checkpoint.BlockA)
					}
				} else {
					fmt.Printf("⏳ Kết nối RPC thành công nhưng lỗi eth_blockNumber: %v. Đang thử lại...\n", errBlock)
				}
				rpcClient.Close()
			} else {
				fmt.Printf("⏳ Lần thử %d/%d: Node %s chưa online (Error: %v). Đang đợi...\n", r, maxRetries, targetURL, err)
			}
			time.Sleep(2 * time.Second)
			if r == maxRetries {
				log.Fatalf("🛑 LỖI: Node %s không online hoặc không đồng bộ kịp sau %d giây!", targetURL, maxRetries*2)
			}
		}
		defer rpcClient.Close()

		// Thực hiện truy vấn lịch sử tại Block A trên Node này
		blockAHex := hexutil.EncodeUint64(checkpoint.BlockA)
		fromAddr := common.HexToAddress(checkpoint.FromAddress)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var balanceAHex string
		err = rpcClient.CallContext(ctx, &balanceAHex, "eth_getBalance", fromAddr, blockAHex)
		if err != nil {
			log.Fatalf("Lỗi lấy Balance tại Block A: %v", err)
		}
		balanceA, _ := hexutil.DecodeBig(balanceAHex)

		var nonceAHex string
		err = rpcClient.CallContext(ctx, &nonceAHex, "eth_getTransactionCount", fromAddr, blockAHex)
		if err != nil {
			log.Fatalf("Lỗi lấy Nonce tại Block A: %v", err)
		}
		nonceA, _ := hexutil.DecodeUint64(nonceAHex)

		// Lấy block mới nhất (Block B) của node hiện tại
		var latestBlockHex string
		err = rpcClient.CallContext(ctx, &latestBlockHex, "eth_blockNumber")
		if err != nil {
			log.Fatalf("Lỗi lấy block mới nhất: %v", err)
		}
		latestBlock, _ := hexutil.DecodeUint64(latestBlockHex)
		latestBlockHex = hexutil.EncodeUint64(latestBlock)

		var balanceBHex string
		err = rpcClient.CallContext(ctx, &balanceBHex, "eth_getBalance", fromAddr, latestBlockHex)
		if err != nil {
			log.Fatalf("Lỗi lấy Balance hiện tại: %v", err)
		}
		balanceB, _ := hexutil.DecodeBig(balanceBHex)

		var nonceBHex string
		err = rpcClient.CallContext(ctx, &nonceBHex, "eth_getTransactionCount", fromAddr, latestBlockHex)
		if err != nil {
			log.Fatalf("Lỗi lấy Nonce hiện tại: %v", err)
		}
		nonceB, _ := hexutil.DecodeUint64(nonceBHex)

		fmt.Println("\n=====================================================")
		fmt.Printf("KẾT QUẢ ĐỐI CHIẾU LỊCH SỬ TRÊN NODE %s:\n", *targetNodeFlag)
		fmt.Printf("Mốc lịch sử Block A (%d):\n", checkpoint.BlockA)
		fmt.Printf("   - Số dư lưu trước: %s | Thực tế get lại: %v\n", checkpoint.SavedBalanceA, balanceA)
		fmt.Printf("   - Nonce lưu trước: %d | Thực tế get lại: %d\n", checkpoint.SavedNonceA, nonceA)
		fmt.Printf("Mốc hiện tại Block B (%d):\n", latestBlock)
		fmt.Printf("   - Số dư hiện tại: %v\n", balanceB)
		fmt.Printf("   - Nonce hiện tại: %d\n", nonceB)
		fmt.Println("=====================================================")

		hasError := false
		var errDetails []string

		// 1. Số dư lịch sử get lại phải bằng số dư lưu trước
		savedBalBig, _ := new(big.Int).SetString(checkpoint.SavedBalanceA, 10)
		if balanceA.Cmp(savedBalBig) != 0 {
			errDetails = append(errDetails, fmt.Sprintf("Số dư lịch sử get lại (%v) KHÁC với số dư thực tế lúc đó (%v)", balanceA, savedBalBig))
			hasError = true
		}

		// 2. Nonce lịch sử get lại phải bằng nonce lưu trước
		if nonceA != checkpoint.SavedNonceA {
			errDetails = append(errDetails, fmt.Sprintf("Nonce lịch sử (%d) KHÁC với Nonce thực tế lúc đó (%d)", nonceA, checkpoint.SavedNonceA))
			hasError = true
		}

		// 3. Số dư lịch sử phải khác số dư hiện tại (để chứng minh không rò rỉ state mới)
		if balanceA.Cmp(balanceB) == 0 {
			errDetails = append(errDetails, "Số dư lịch sử (A) và hiện tại (B) GIỐNG HỆT NHAU (Rò rỉ State mới!)")
			hasError = true
		}

		// 4. Nonce lịch sử phải khác nonce hiện tại
		if nonceA == nonceB {
			errDetails = append(errDetails, "Nonce lịch sử (A) và hiện tại (B) GIỐNG HỆT NHAU")
			hasError = true
		}

		if hasError {
			reason := fmt.Sprintf("LỖI LỊCH SỬ STATE TRÊN NODE %s tại Block %d: %s", *targetNodeFlag, checkpoint.BlockA, strings.Join(errDetails, "; "))
			os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)
			fmt.Printf("\n🚨 %s\n", reason)
			os.Exit(1)
		}

		fmt.Printf("🎉 XÁC MINH THÀNH CÔNG: Node %s trả về dữ liệu lịch sử hoàn toàn chính xác!\n", *targetNodeFlag)
		os.Remove(*fileFlag)
		os.Exit(0)
	}

	// Chế độ Loop mặc định (chạy liên tục gửi Tx và verify)
	fmt.Println("=====================================================")
	fmt.Printf("BẮT ĐẦU TEST LỊCH SỬ STATE (Từ ví: %s)\n", fromAddress.Hex())
	fmt.Printf("Sử dụng cấu hình: %s\n", *configFlag)
	fmt.Printf("Tổng số RPC Nodes cấu hình: %d\n", len(urls))
	fmt.Println("=====================================================")

	fc, err := NewFailoverClient(urls, privateKey, cfg.ChainID, *excludeFlag)
	if err != nil {
		log.Fatalf("Lỗi khởi tạo failover client: %v", err)
	}

	if *loopFlag {
		fmt.Println("🔄 CHẾ ĐỘ LẶP VÒNG LẶP ĐƯỢC BẬT (Nhấn Ctrl+C để dừng)")
		count := 1
		for {
			fmt.Printf("\n▶️  BẮT ĐẦU VÒNG LẶP KIỂM TRA THỨ %d\n", count)
			ok := runHistoryCheck(fc, fromAddress, toAddress, *waitBlocksFlag)
			if !ok {
				fmt.Println("❌ Phát hiện lỗi trong lịch sử state! Dừng chương trình lập tức.")
				os.Exit(1)
			} else {
				fmt.Println("✅ Kiểm tra lịch sử state thành công.")
			}
			count++
			fmt.Println("⏳ Đợi 5s trước khi bắt đầu vòng tiếp theo...")
			time.Sleep(5 * time.Second)
		}
	} else {
		fmt.Println("▶️  CHẾ ĐỘ CHẠY 1 LẦN")
		ok := runHistoryCheck(fc, fromAddress, toAddress, *waitBlocksFlag)
		if !ok {
			fmt.Println("\n❌ THẤT BẠI: Phát hiện có lỗi nghiêm trọng trong lịch sử state!")
			os.Exit(1)
		} else {
			fmt.Println("\n🎉 THÀNH CÔNG: Tất cả các kiểm tra lịch sử state đều vượt qua!")
		}
	}
}
