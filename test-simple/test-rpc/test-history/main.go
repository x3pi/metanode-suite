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

type AccountStateResult struct {
	Address        string `json:"address"`
	Balance        string `json:"balance"`
	PendingBalance string `json:"pendingBalance"`
	LastHash       string `json:"lastHash"`
	DeviceKey      string `json:"deviceKey"`
	Nonce          uint64 `json:"nonce"`
	PublicKeyBls   string `json:"publicKeyBls"`
	AccountType    int32  `json:"accountType"`
}

func callContextWithRetry(ctx context.Context, rpcClient *rpc.Client, result interface{}, method string, args ...interface{}) error {
	var err error
	for i := 0; i < 15; i++ {
		err = rpcClient.CallContext(ctx, result, method, args...)
		if err == nil {
			return nil
		}
		errStr := err.Error()
		if strings.Contains(errStr, "block not found") || strings.Contains(errStr, "not found") || strings.Contains(errStr, "database") {
			fmt.Printf("   ⚠️ Lần thử %d/15: Lỗi RPC %s (%v). Đang thử lại sau 500ms...\n", i+1, method, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		return err
	}
	return err
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
			errBlock := callContextWithRetry(ctx, rpcClient, &latestBlockHex, "eth_blockNumber")
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

		waitStartTime := time.Now()
		for {
			if time.Since(waitStartTime) > 30*time.Second {
				fmt.Printf("\n   🚨 Timeout khi chờ receipt cho Tx %s. Dùng lệnh curl sau để kiểm tra trên từng RPC:\n", signedTx.Hash().Hex())
				for _, url := range fc.urls {
					fmt.Printf("      curl -X POST -H \"Content-Type: application/json\" --data '{\"jsonrpc\":\"2.0\",\"method\":\"eth_getTransactionByHash\",\"params\":[\"%s\"],\"id\":1}' %s\n", signedTx.Hash().Hex(), url)
				}
				fmt.Println()
				return fmt.Errorf("timeout waiting for receipt after 30s")
			}
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

// runHistoryCheck kiểm tra tính toàn vẹn của lịch sử state.
//
// Tham số waitTxCount (flag -wait):
//   - 0 hoặc 1 → gửi 1 tx thêm, block receipt của tx đó = Block B
//   - N > 1    → gửi thêm N tx, block receipt của tx cuối cùng = Block B
//
// Block A = block của giao dịch đầu tiên (mốc lịch sử cần kiểm tra).
// Block B = block của giao dịch cuối cùng (mốc "hiện tại" để so sánh).
// Mỗi tx đều chờ receipt → checker tự kiểm soát block tăng,
// không bị ảnh hưởng bởi spam giao dịch từ bên ngoài.
func runHistoryCheck(fc *FailoverClient, fromAddress, toAddress common.Address, waitTxCount uint64) bool {
	var blockA, blockB uint64
	var savedBalanceA *big.Int
	var savedNonceA uint64

	// ── Tx 1: đánh dấu Block A ───────────────────────────────────────────────
	bA, err := sendTxAndWait(fc, fromAddress, toAddress)
	if err != nil {
		fmt.Printf("Gửi Tx 1 thất bại: %v\n", err)
		return false
	}
	blockA = bA
	fmt.Printf("✅ Giao dịch 1 đã được mine tại Block A: %d\n", blockA)

	// Lưu snapshot state tại Block A ngay sau khi có receipt
	blockAHex := hexutil.EncodeUint64(blockA)
	err = fc.execute(func(ethCli *ethclient.Client, rpcClient *rpc.Client) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var savedBalanceAHex string
		err := callContextWithRetry(ctx, rpcClient, &savedBalanceAHex, "eth_getBalance", fromAddress, blockAHex)
		if err != nil {
			return err
		}
		savedBalanceA, _ = hexutil.DecodeBig(savedBalanceAHex)

		var savedNonceAHex string
		err = callContextWithRetry(ctx, rpcClient, &savedNonceAHex, "eth_getTransactionCount", fromAddress, blockAHex)
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

	// ── Gửi thêm N giao dịch, block receipt của tx cuối = Block B ────────────
	// Tối thiểu 1 tx để đảm bảo Block B > Block A (hoặc ít nhất nonce khác).
	extraTxCount := waitTxCount
	if extraTxCount < 1 {
		extraTxCount = 1
	}
	fmt.Printf("\n⏳ Đang gửi thêm %d giao dịch để tạo mốc Block B...\n", extraTxCount)
	blockB = blockA
	for i := uint64(1); i <= extraTxCount; i++ {
		fmt.Printf("   📤 Giao dịch thêm %d/%d...\n", i, extraTxCount)
		bB, errTx := sendTxAndWait(fc, fromAddress, toAddress)
		if errTx != nil {
			fmt.Printf("   ⚠️ Giao dịch %d thất bại: %v\n", i, errTx)
			return false
		}
		blockB = bB
		fmt.Printf("   ✅ Giao dịch %d mined tại Block: %d\n", i, bB)
	}
	fmt.Printf("\n📍 Block A (mốc lịch sử): %d | Block B (mốc hiện tại): %d\n", blockA, blockB)

	fmt.Println("\n=====================================================")
	fmt.Println("BẮT ĐẦU KIỂM TRA LỊCH SỬ BẰNG RPC TẠI 2 MỐC BLOCK KHÁC NHAU")
	fmt.Println("=====================================================")

	blockBHex := hexutil.EncodeUint64(blockB)
	var balanceA *big.Int
	var balanceB *big.Int
	var nonceA, nonceB uint64
	var accountStateA AccountStateResult
	var accountStateB AccountStateResult

	err = fc.execute(func(ethCli *ethclient.Client, rpcClient *rpc.Client) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var balanceAHex string
		err := callContextWithRetry(ctx, rpcClient, &balanceAHex, "eth_getBalance", fromAddress, blockAHex)
		if err != nil {
			return err
		}
		balanceA, _ = hexutil.DecodeBig(balanceAHex)

		var balanceBHex string
		err = callContextWithRetry(ctx, rpcClient, &balanceBHex, "eth_getBalance", fromAddress, blockBHex)
		if err != nil {
			return err
		}
		balanceB, _ = hexutil.DecodeBig(balanceBHex)

		var nonceAHex string
		err = callContextWithRetry(ctx, rpcClient, &nonceAHex, "eth_getTransactionCount", fromAddress, blockAHex)
		if err != nil {
			return err
		}
		nonceA, _ = hexutil.DecodeUint64(nonceAHex)

		var nonceBHex string
		err = callContextWithRetry(ctx, rpcClient, &nonceBHex, "eth_getTransactionCount", fromAddress, blockBHex)
		if err != nil {
			return err
		}
		nonceB, _ = hexutil.DecodeUint64(nonceBHex)

		err = callContextWithRetry(ctx, rpcClient, &accountStateA, "mtn_getAccountState", fromAddress, blockAHex)
		if err != nil {
			return err
		}

		err = callContextWithRetry(ctx, rpcClient, &accountStateB, "mtn_getAccountState", fromAddress, blockBHex)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		fmt.Printf("Lấy dữ liệu so sánh thất bại: %v\n", err)
		return false
	}

	fmt.Printf("💰 eth_getBalance tại Block A (Lịch sử: %d): %v\n", blockA, balanceA)
	fmt.Printf("💰 eth_getBalance tại Block B (Hiện tại: %d): %v\n", blockB, balanceB)
	fmt.Printf("💰 mtn_getAccountState.balance tại Block A: %s\n", accountStateA.Balance)
	fmt.Printf("💰 mtn_getAccountState.balance tại Block B: %s\n", accountStateB.Balance)
	fmt.Printf("\n🔢 eth_getTransactionCount tại Block A (Lịch sử: %d): %d\n", blockA, nonceA)
	fmt.Printf("🔢 eth_getTransactionCount tại Block B (Hiện tại: %d): %d\n", blockB, nonceB)
	fmt.Printf("🔢 mtn_getAccountState.nonce tại Block A: %d\n", accountStateA.Nonce)
	fmt.Printf("🔢 mtn_getAccountState.nonce tại Block B: %d\n", accountStateB.Nonce)

	hasError := false
	var errDetails []string

	// 1. Số dư lịch sử get lại phải bằng số dư lưu trước
	if balanceA.Cmp(savedBalanceA) != 0 {
		diff := new(big.Int).Sub(balanceA, savedBalanceA)
		errDetails = append(errDetails, fmt.Sprintf("Số dư lịch sử get lại (%s) KHÁC với số dư thực tế lưu trữ trước đó (%s). Sai lệch: %s", balanceA.String(), savedBalanceA.String(), diff.String()))
		hasError = true
	}

	// 2. Số dư tại Block A và số dư hiện tại tại Block B không được giống nhau (chỉ khi số dư hiện tại thực sự đã thay đổi so với mốc lịch sử)
	if balanceB.Cmp(savedBalanceA) != 0 && balanceA.Cmp(balanceB) == 0 {
		errDetails = append(errDetails, fmt.Sprintf(
			"Số dư tại Block A (%s) và Block B (%s) GIỐNG HỆT NHAU. "+
				"Mong đợi: Số dư lịch sử Block A (Lần đầu) phải là %s và Số dư hiện tại Block B (Lần cuối) phải thay đổi (khác %s) do có giao dịch trong quá trình đó. "+
				"(Lỗi: Có thể do rò rỉ State mới nhất về Block A, hoặc giao dịch phát sinh không thành công/chưa sync)",
			balanceA.String(), balanceB.String(), savedBalanceA.String(), savedBalanceA.String(),
		))
		hasError = true
	}

	// 3. Nonce lịch sử get lại phải bằng nonce lưu trước
	if nonceA != savedNonceA {
		diff := int64(nonceA) - int64(savedNonceA)
		errDetails = append(errDetails, fmt.Sprintf("Nonce lịch sử get lại (%d) KHÁC với nonce thực tế lưu trữ trước đó (%d). Sai lệch: %+d", nonceA, savedNonceA, diff))
		hasError = true
	}

	// 4. Nonce lịch sử tại Block A và nonce hiện tại tại Block B không được giống nhau (chỉ khi nonce hiện tại thực sự đã thay đổi so với mốc lịch sử)
	if nonceB != savedNonceA && nonceA == nonceB {
		errDetails = append(errDetails, fmt.Sprintf(
			"Nonce tại Block A (%d) và Block B (%d) GIỐNG HỆT NHAU. "+
				"Mong đợi: Nonce lịch sử Block A (Lần đầu) phải là %d và Nonce hiện tại Block B (Lần cuối) phải lớn hơn %d do có giao dịch trong quá trình đó. "+
				"(Lỗi: Có thể do trôi State/chưa phân tách lịch sử, hoặc giao dịch phát sinh không thành công/chưa sync)",
			nonceA, nonceB, savedNonceA, savedNonceA,
		))
		hasError = true
	}

	// 5. Đối chiếu giữa mtn_getAccountState và eth_getBalance/eth_getTransactionCount
	// Tại Block A
	if accountStateA.Balance != balanceA.String() {
		errDetails = append(errDetails, fmt.Sprintf("Mâu thuẫn Block A: mtn_getAccountState.balance (%s) khác eth_getBalance (%s)", accountStateA.Balance, balanceA.String()))
		hasError = true
	}
	if accountStateA.Nonce != nonceA {
		errDetails = append(errDetails, fmt.Sprintf("Mâu thuẫn Block A: mtn_getAccountState.nonce (%d) khác eth_getTransactionCount (%d)", accountStateA.Nonce, nonceA))
		hasError = true
	}
	// Tại Block B
	if accountStateB.Balance != balanceB.String() {
		errDetails = append(errDetails, fmt.Sprintf("Mâu thuẫn Block B: mtn_getAccountState.balance (%s) khác eth_getBalance (%s)", accountStateB.Balance, balanceB.String()))
		hasError = true
	}
	if accountStateB.Nonce != nonceB {
		errDetails = append(errDetails, fmt.Sprintf("Mâu thuẫn Block B: mtn_getAccountState.nonce (%d) khác eth_getTransactionCount (%d)", accountStateB.Nonce, nonceB))
		hasError = true
	}

	if hasError {
		var sb strings.Builder
		sb.WriteString("==================================================\n")
		sb.WriteString("🚨 LỖI LỊCH SỬ STATE (CHẾ ĐỘ CHẠY LIÊN TỤC)\n")
		sb.WriteString(fmt.Sprintf("⏰ Thời gian: %s\n", time.Now().Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("📍 Mốc Block A: %d\n", blockA))
		sb.WriteString(fmt.Sprintf("📍 Mốc Block B: %d\n", blockB))
		sb.WriteString("==================================================\n")
		for _, det := range errDetails {
			sb.WriteString(fmt.Sprintf("- %s\n", det))
		}
		sb.WriteString(queryAllRPCsAndGenerateReport(fc.urls, fromAddress, blockA))
		sb.WriteString("==================================================\n")
		reason := sb.String()
		os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)

		// Ghi đè/thêm vào log file riêng của checker
		appendLocalErrorLog(reason)

		fmt.Printf("\n🚨 %s\n", reason)
	}

	fmt.Println("\n=====================================================")
	fmt.Println("🚀 BẮT ĐẦU XÁC MINH TRÊN CÁC NODE CÒN LẠI")
	fmt.Println("=====================================================")

	for _, u := range fc.urls {
		fmt.Printf("🔍 Đang kiểm tra node: %s\n", u)
		tempClient, errDial := rpc.Dial(u)
		if errDial != nil {
			fmt.Printf("   ⚠️ Node %s không thể kết nối (%v). Bỏ qua...\n", u, errDial)
			continue
		}

		// Đợi node đồng bộ tới blockB (tối đa 30s)
		synced := false
		waitStart := time.Now()
		isAlive := false
		for time.Since(waitStart) < 30*time.Second {
			ctxTemp, cancelTemp := context.WithTimeout(context.Background(), 2*time.Second)
			var latestBlockHex string
			errBlock := tempClient.CallContext(ctxTemp, &latestBlockHex, "eth_blockNumber")
			cancelTemp()

			if errBlock == nil {
				isAlive = true
				latestBlock, _ := hexutil.DecodeUint64(latestBlockHex)
				if latestBlock >= blockB {
					synced = true
					break
				}
			}
			time.Sleep(500 * time.Millisecond)
		}

		if !isAlive {
			fmt.Printf("   ⚠️ Node %s không phản hồi eth_blockNumber. Bỏ qua...\n", u)
			tempClient.Close()
			continue
		}

		if !synced {
			reason := fmt.Sprintf("🛑 LỖI: Node %s không đồng bộ tới block %d sau 30s", u, blockB)
			os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)
			appendLocalErrorLog(reason)
			fmt.Printf("   %s\n", reason)
			tempClient.Close()
			return false
		}

		// Xác minh dữ liệu lịch sử trên node này
		ctxTemp, cancelTemp := context.WithTimeout(context.Background(), 5*time.Second)
		var tBalAHex, tBalBHex, tNonceAHex, tNonceBHex string
		var tAsA, tAsB AccountStateResult
		
		err1 := tempClient.CallContext(ctxTemp, &tBalAHex, "eth_getBalance", fromAddress, blockAHex)
		err2 := tempClient.CallContext(ctxTemp, &tBalBHex, "eth_getBalance", fromAddress, blockBHex)
		err3 := tempClient.CallContext(ctxTemp, &tNonceAHex, "eth_getTransactionCount", fromAddress, blockAHex)
		err4 := tempClient.CallContext(ctxTemp, &tNonceBHex, "eth_getTransactionCount", fromAddress, blockBHex)
		err5 := tempClient.CallContext(ctxTemp, &tAsA, "mtn_getAccountState", fromAddress, blockAHex)
		err6 := tempClient.CallContext(ctxTemp, &tAsB, "mtn_getAccountState", fromAddress, blockBHex)
		cancelTemp()

		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil {
			fmt.Printf("   ⚠️ Node %s gặp lỗi RPC khi query data. Bỏ qua...\n", u)
			tempClient.Close()
			continue
		}

		tBalA, _ := hexutil.DecodeBig(tBalAHex)
		tBalB, _ := hexutil.DecodeBig(tBalBHex)
		tNonceA, _ := hexutil.DecodeUint64(tNonceAHex)
		tNonceB, _ := hexutil.DecodeUint64(tNonceBHex)

		nodeHasError := false
		var nodeErrDetails []string

		if tBalA.Cmp(balanceA) != 0 {
			nodeErrDetails = append(nodeErrDetails, fmt.Sprintf("- balanceA khác: Node=%v vs Chuẩn=%v", tBalA, balanceA))
			nodeHasError = true
		}
		if tBalB.Cmp(balanceB) != 0 {
			nodeErrDetails = append(nodeErrDetails, fmt.Sprintf("- balanceB khác: Node=%v vs Chuẩn=%v", tBalB, balanceB))
			nodeHasError = true
		}
		if tNonceA != nonceA {
			nodeErrDetails = append(nodeErrDetails, fmt.Sprintf("- nonceA khác: Node=%v vs Chuẩn=%v", tNonceA, nonceA))
			nodeHasError = true
		}
		if tNonceB != nonceB {
			nodeErrDetails = append(nodeErrDetails, fmt.Sprintf("- nonceB khác: Node=%v vs Chuẩn=%v", tNonceB, nonceB))
			nodeHasError = true
		}
		if tAsA.Balance != accountStateA.Balance || tAsA.Nonce != accountStateA.Nonce {
			nodeErrDetails = append(nodeErrDetails, fmt.Sprintf("- accountStateA khác: Node=%v/%v vs Chuẩn=%v/%v", tAsA.Balance, tAsA.Nonce, accountStateA.Balance, accountStateA.Nonce))
			nodeHasError = true
		}
		if tAsB.Balance != accountStateB.Balance || tAsB.Nonce != accountStateB.Nonce {
			nodeErrDetails = append(nodeErrDetails, fmt.Sprintf("- accountStateB khác: Node=%v/%v vs Chuẩn=%v/%v", tAsB.Balance, tAsB.Nonce, accountStateB.Balance, accountStateB.Nonce))
			nodeHasError = true
		}

		if nodeHasError {
			reason := fmt.Sprintf("🛑 LỖI LỊCH SỬ STATE TRÊN NODE %s:\n%s", u, strings.Join(nodeErrDetails, "\n"))
			os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)
			appendLocalErrorLog(reason)
			fmt.Printf("   %s\n", reason)
			tempClient.Close()
			return false
		}

		fmt.Printf("   ✅ Node %s đã xác minh dữ liệu lịch sử hoàn toàn khớp!\n", u)
		tempClient.Close()
	}

	return true
}

func queryAllRPCsAndGenerateReport(urls []string, fromAddr common.Address, blockA uint64) string {
	var sb strings.Builder
	blockAHex := hexutil.EncodeUint64(blockA)
	sb.WriteString("    🌐 Đối chiếu dữ liệu thực tế trên toàn bộ RPC Nodes:\n")
	sb.WriteString("       RPC URL                        | eth_getBalance / mtn_balance                      | eth_nonce / mtn_nonce\n")
	sb.WriteString("       ------------------------------------------------------------------------------------------------------\n")
	for _, u := range urls {
		tempClient, errDial := rpc.Dial(u)
		if errDial != nil {
			sb.WriteString(fmt.Sprintf("       %-30s | Lỗi kết nối: %v\n", u, errDial))
			continue
		}

		ctxTemp, cancelTemp := context.WithTimeout(context.Background(), 2*time.Second)
		var tempBalanceAHex string
		errBal := tempClient.CallContext(ctxTemp, &tempBalanceAHex, "eth_getBalance", fromAddr, blockAHex)

		var tempNonceAHex string
		errNon := tempClient.CallContext(ctxTemp, &tempNonceAHex, "eth_getTransactionCount", fromAddr, blockAHex)

		var tempAccountState AccountStateResult
		errAs := tempClient.CallContext(ctxTemp, &tempAccountState, "mtn_getAccountState", fromAddr, blockAHex)
		cancelTemp()
		tempClient.Close()

		if errBal != nil || errNon != nil || errAs != nil {
			sb.WriteString(fmt.Sprintf("       %-30s | Lỗi RPC: bal_err=%v, nonce_err=%v, as_err=%v\n", u, errBal, errNon, errAs))
			continue
		}

		tempBalance, _ := hexutil.DecodeBig(tempBalanceAHex)
		tempNonce, _ := hexutil.DecodeUint64(tempNonceAHex)
		sb.WriteString(fmt.Sprintf("       %-30s | eth_bal=%s / mtn_bal=%s | eth_nonce=%d / mtn_nonce=%d\n",
			u, tempBalance.String(), tempAccountState.Balance, tempNonce, tempAccountState.Nonce))
	}
	sb.WriteString("       ------------------------------------------------------------------------------------------------------\n")
	return sb.String()
}

func appendLocalErrorLog(reason string) {
	f, err := os.OpenFile("history_errors.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(reason + "\n")
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
	waitBlocksFlag := flag.Uint64("wait", 5, "Số giao dịch gửi thêm sau Tx đầu tiên trước khi kiểm tra lịch sử (mặc định 5). Block receipt của tx cuối = Block B. Tránh phụ thuộc vào spam ngoài để block tăng.")
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

	// Nếu người dùng truyền target-node, đưa nó lên vị trí ưu tiên số 1 trong danh sách kết nối
	if *targetNodeFlag != "" {
		targetURL, err := getTargetNodeURL(cfg, *targetNodeFlag)
		if err == nil {
			orderedUrls := []string{targetURL}
			for _, u := range urls {
				if u != targetURL {
					orderedUrls = append(orderedUrls, u)
				}
			}
			urls = orderedUrls
		}
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
			err := callContextWithRetry(ctx, rpcClient, &savedBalanceAHex, "eth_getBalance", fromAddress, blockAHex)
			if err != nil {
				return err
			}
			savedBalanceA, _ = hexutil.DecodeBig(savedBalanceAHex)

			var savedNonceAHex string
			err = callContextWithRetry(ctx, rpcClient, &savedNonceAHex, "eth_getTransactionCount", fromAddress, blockAHex)
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

		var checkpoints []SavedCheckpoint
		existingData, errRead := os.ReadFile(*fileFlag)
		if errRead == nil {
			errArr := json.Unmarshal(existingData, &checkpoints)
			if errArr != nil {
				var singleCheckpoint SavedCheckpoint
				if errSingle := json.Unmarshal(existingData, &singleCheckpoint); errSingle == nil {
					checkpoints = append(checkpoints, singleCheckpoint)
				}
			}
		}

		checkpoints = append(checkpoints, checkpoint)
		if len(checkpoints) > 1000 {
			checkpoints = checkpoints[len(checkpoints)-1000:]
		}

		data, err := json.MarshalIndent(checkpoints, "", "  ")
		if err != nil {
			log.Fatalf("Lỗi serialize checkpoints JSON: %v", err)
		}

		err = os.WriteFile(*fileFlag, data, 0644)
		if err != nil {
			log.Fatalf("Lỗi ghi file lưu trạng thái: %v", err)
		}

		fmt.Printf("🎉 Đã lưu thành công trạng thái vào file %s (Tổng số checkpoints: %d):\n", *fileFlag, len(checkpoints))
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

		var checkpoints []SavedCheckpoint
		if errArr := json.Unmarshal(data, &checkpoints); errArr != nil {
			var singleCheckpoint SavedCheckpoint
			if errSingle := json.Unmarshal(data, &singleCheckpoint); errSingle == nil {
				checkpoints = append(checkpoints, singleCheckpoint)
			} else {
				log.Fatalf("Lỗi parse file trạng thái: %v", errArr)
			}
		}

		if len(checkpoints) == 0 {
			fmt.Println("⚠️ Không có checkpoint nào cần xác minh.")
			os.Remove(*fileFlag)
			fmt.Println("🔄 Đã verify xong, tiếp tục chuyển sang chế độ chạy kiểm tra lịch sử...")
		} else {
			targetURL, err := getTargetNodeURL(cfg, *targetNodeFlag)
			if err != nil {
				log.Fatalf("Lỗi xác định target node: %v", err)
			}

			// Tìm BlockA lớn nhất
			var maxBlockA uint64
			for _, cp := range checkpoints {
				if cp.BlockA > maxBlockA {
					maxBlockA = cp.BlockA
				}
			}

			fmt.Printf("🔌 Đang kết nối trực tiếp (Strict) tới RPC Node: %s...\n", targetURL)

			// Đợi node online và đồng bộ vượt qua Block A lớn nhất
			var rpcClient *rpc.Client

			maxRetries := 300
			for r := 1; r <= maxRetries; r++ {
				rpcClient, err = rpc.Dial(targetURL)
				if err == nil {
					// Kiểm tra chiều cao block hiện tại của node
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					var latestBlockHex string
					errBlock := callContextWithRetry(ctx, rpcClient, &latestBlockHex, "eth_blockNumber")
					cancel()

					if errBlock == nil {
						latestBlock, _ := hexutil.DecodeUint64(latestBlockHex)
						if latestBlock > maxBlockA {
							fmt.Printf("✅ Node đã online và đồng bộ tới Block %d (vượt qua Block A lớn nhất %d)\n", latestBlock, maxBlockA)
							fmt.Printf("🔍 Bắt đầu kiểm tra trạng thái khả dụng của dữ liệu Block A (%d) qua RPC...\n", maxBlockA)

							// Thăm dò chủ động (active polling) mỗi 200ms thay vì sleep 2s cứng
							var ready bool
							for checkRetries := 0; checkRetries < 100; checkRetries++ {
								var checkBlock map[string]interface{}
								ctxBlock, cancelBlock := context.WithTimeout(context.Background(), 2*time.Second)
								errBlockCheck := rpcClient.CallContext(ctxBlock, &checkBlock, "eth_getBlockByNumber", hexutil.EncodeUint64(maxBlockA), false)
								cancelBlock()

								var checkNonceHex string
								ctxNonce, cancelNonce := context.WithTimeout(context.Background(), 2*time.Second)
								errNonceCheck := rpcClient.CallContext(ctxNonce, &checkNonceHex, "eth_getTransactionCount", common.HexToAddress(checkpoints[0].FromAddress), hexutil.EncodeUint64(maxBlockA))
								cancelNonce()

								if errBlockCheck == nil && checkBlock != nil && errNonceCheck == nil {
									fmt.Printf("   ✅ Trạng thái dữ liệu Block A (%d) đã sẵn sàng sau %d lần thử (khoảng %d ms)!\n", maxBlockA, checkRetries+1, (checkRetries+1)*200)
									ready = true
									break
								} else {
									fmt.Printf("   DEBUG (lần %d): errBlockCheck=%v, checkBlock==nil: %t, errNonceCheck=%v\n", checkRetries+1, errBlockCheck, checkBlock == nil, errNonceCheck)
								}
								time.Sleep(200 * time.Millisecond)
							}

							if ready {
								break
							} else {
								report := queryAllRPCsAndGenerateReport(urls, common.HexToAddress(checkpoints[0].FromAddress), maxBlockA)
								reason := fmt.Sprintf("🛑 LỖI: Dữ liệu mốc Block A (%d) không sẵn sàng trên RPC %s sau 20 giây thăm dò mặc dù chiều cao node đã đạt %d!\n%s", maxBlockA, targetURL, latestBlock, report)
								os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)
								appendLocalErrorLog(reason)
								log.Fatalf(reason)
							}
						} else {
							fmt.Printf("⏳ Node đã online nhưng chiều cao block (%d) chưa vượt qua Block A lớn nhất (%d). Đang đợi...\n", latestBlock, maxBlockA)
						}
					} else {
						fmt.Printf("⏳ Kết nối RPC thành công nhưng lỗi eth_blockNumber: %v. Đang thử lại...\n", errBlock)
					}
					rpcClient.Close()
				} else {
					fmt.Printf("⏳ Lần thử %d/%d: Node %s chưa online (Error: %v). Đang đợi...\n", r, maxRetries, targetURL, err)
				}
				time.Sleep(200 * time.Millisecond)
				if r == maxRetries {
					log.Fatalf("🛑 LỖI: Node %s không online hoặc không đồng bộ kịp sau %d giây!", targetURL, int(float64(maxRetries)*0.2))
				}
			}
			defer rpcClient.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Lấy block mới nhất (Block B) của node hiện tại
			var latestBlockHex string
			err = callContextWithRetry(ctx, rpcClient, &latestBlockHex, "eth_blockNumber")
			if err != nil {
				reason := fmt.Sprintf("🚨 LỖI SO SÁNH LỊCH SỬ STATE (VERIFY)\nLấy block mới nhất thất bại: %v\n", err)
				os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)
				appendLocalErrorLog(reason)
				fmt.Printf("\n🚨 %s\n", reason)
				os.Exit(1)
			}
			latestBlock, _ := hexutil.DecodeUint64(latestBlockHex)
			latestBlockHex = hexutil.EncodeUint64(latestBlock)

			hasGlobalError := false
			var allErrDetails []string

			for idx, cp := range checkpoints {
				fmt.Printf("\n🔍 Đang đối chiếu checkpoint %d/%d (Block A: %d, Address: %s)...\n", idx+1, len(checkpoints), cp.BlockA, cp.FromAddress)

				blockAHex := hexutil.EncodeUint64(cp.BlockA)
				fromAddr := common.HexToAddress(cp.FromAddress)

				var balanceAHex string
				err = callContextWithRetry(ctx, rpcClient, &balanceAHex, "eth_getBalance", fromAddr, blockAHex)
				if err != nil {
					reason := fmt.Sprintf("🚨 LỖI SO SÁNH LỊCH SỬ STATE (VERIFY)\nLấy Balance tại Block A (%d) thất bại: %v\n", cp.BlockA, err)
					os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)
					appendLocalErrorLog(reason)
					fmt.Printf("\n🚨 %s\n", reason)
					os.Exit(1)
				}
				balanceA, _ := hexutil.DecodeBig(balanceAHex)

				var nonceAHex string
				err = callContextWithRetry(ctx, rpcClient, &nonceAHex, "eth_getTransactionCount", fromAddr, blockAHex)
				if err != nil {
					reason := fmt.Sprintf("🚨 LỖI SO SÁNH LỊCH SỬ STATE (VERIFY)\nLấy Nonce tại Block A (%d) thất bại: %v\n", cp.BlockA, err)
					os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)
					appendLocalErrorLog(reason)
					fmt.Printf("\n🚨 %s\n", reason)
					os.Exit(1)
				}
				nonceA, _ := hexutil.DecodeUint64(nonceAHex)

				var balanceBHex string
				err = callContextWithRetry(ctx, rpcClient, &balanceBHex, "eth_getBalance", fromAddr, latestBlockHex)
				if err != nil {
					reason := fmt.Sprintf("🚨 LỖI SO SÁNH LỊCH SỬ STATE (VERIFY)\nLấy Balance hiện tại (%d) thất bại: %v\n", latestBlock, err)
					os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)
					appendLocalErrorLog(reason)
					fmt.Printf("\n🚨 %s\n", reason)
					os.Exit(1)
				}
				balanceB, _ := hexutil.DecodeBig(balanceBHex)

				var nonceBHex string
				err = callContextWithRetry(ctx, rpcClient, &nonceBHex, "eth_getTransactionCount", fromAddr, latestBlockHex)
				if err != nil {
					reason := fmt.Sprintf("🚨 LỖI SO SÁNH LỊCH SỬ STATE (VERIFY)\nLấy Nonce hiện tại (%d) thất bại: %v\n", latestBlock, err)
					os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)
					appendLocalErrorLog(reason)
					fmt.Printf("\n🚨 %s\n", reason)
					os.Exit(1)
				}
				nonceB, _ := hexutil.DecodeUint64(nonceBHex)

				var accountStateA AccountStateResult
				err = callContextWithRetry(ctx, rpcClient, &accountStateA, "mtn_getAccountState", fromAddr, blockAHex)
				if err != nil {
					reason := fmt.Sprintf("🚨 LỖI SO SÁNH LỊCH SỬ STATE (VERIFY)\nLấy AccountState tại Block A (%d) thất bại: %v\n", cp.BlockA, err)
					os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)
					appendLocalErrorLog(reason)
					fmt.Printf("\n🚨 %s\n", reason)
					os.Exit(1)
				}

				var accountStateB AccountStateResult
				err = callContextWithRetry(ctx, rpcClient, &accountStateB, "mtn_getAccountState", fromAddr, latestBlockHex)
				if err != nil {
					reason := fmt.Sprintf("🚨 LỖI SO SÁNH LỊCH SỬ STATE (VERIFY)\nLấy AccountState hiện tại (%d) thất bại: %v\n", latestBlock, err)
					os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)
					appendLocalErrorLog(reason)
					fmt.Printf("\n🚨 %s\n", reason)
					os.Exit(1)
				}

				fmt.Printf("Mốc lịch sử Block A (%d):\n", cp.BlockA)
				fmt.Printf("   - Số dư lưu trước: %s | Thực tế get lại: %v | mtn_balance: %s\n", cp.SavedBalanceA, balanceA, accountStateA.Balance)
				fmt.Printf("   - Nonce lưu trước: %d | Thực tế get lại: %d | mtn_nonce: %d\n", cp.SavedNonceA, nonceA, accountStateA.Nonce)
				fmt.Printf("Mốc hiện tại Block B (%d):\n", latestBlock)
				fmt.Printf("   - Số dư hiện tại: %v | mtn_balance: %s\n", balanceB, accountStateB.Balance)
				fmt.Printf("   - Nonce hiện tại: %d | mtn_nonce: %d\n", nonceB, accountStateB.Nonce)

				hasError := false
				var errDetails []string

				// 5. Đối chiếu giữa mtn_getAccountState và eth_getBalance/eth_getTransactionCount
				// Tại Block A
				if accountStateA.Balance != balanceA.String() {
					errDetails = append(errDetails, fmt.Sprintf("Mâu thuẫn Block A: mtn_getAccountState.balance (%s) khác eth_getBalance (%s)", accountStateA.Balance, balanceA.String()))
					hasError = true
				}
				if accountStateA.Nonce != nonceA {
					errDetails = append(errDetails, fmt.Sprintf("Mâu thuẫn Block A: mtn_getAccountState.nonce (%d) khác eth_getTransactionCount (%d)", accountStateA.Nonce, nonceA))
					hasError = true
				}
				// Tại Block B
				if accountStateB.Balance != balanceB.String() {
					errDetails = append(errDetails, fmt.Sprintf("Mâu thuẫn Block B: mtn_getAccountState.balance (%s) khác eth_getBalance (%s)", accountStateB.Balance, balanceB.String()))
					hasError = true
				}
				if accountStateB.Nonce != nonceB {
					errDetails = append(errDetails, fmt.Sprintf("Mâu thuẫn Block B: mtn_getAccountState.nonce (%d) khác eth_getTransactionCount (%d)", accountStateB.Nonce, nonceB))
					hasError = true
				}

				// 1. Số dư lịch sử get lại phải bằng số dư lưu trước
				savedBalBig, _ := new(big.Int).SetString(cp.SavedBalanceA, 10)
				if balanceA.Cmp(savedBalBig) != 0 {
					diff := new(big.Int).Sub(balanceA, savedBalBig)
					errDetails = append(errDetails, fmt.Sprintf("Số dư lịch sử get lại (%s) KHÁC với số dư thực tế lưu trữ trước đó (%s). Sai lệch: %s", balanceA.String(), savedBalBig.String(), diff.String()))
					hasError = true
				}

				// 2. Số dư lịch sử phải khác số dư hiện tại (chỉ khi số dư hiện tại thực sự đã thay đổi so với mốc lịch sử)
				if balanceB.Cmp(savedBalBig) != 0 && balanceA.Cmp(balanceB) == 0 {
					// Tính delta: từ savedBalance → balanceB để biết mong đợi balanceA phải là gì
					deltaBalance := new(big.Int).Sub(balanceB, savedBalBig)
					errDetails = append(errDetails, fmt.Sprintf(
						"[ROI RI STATE] Số dư lịch sử tại Block A (%d) = %s, trùng với số dư hiện tại Block B (%d) = %s. "+
							"Số dư gốc lưu lúc save = %s, Số dư hiện tại đã thay đổi %s (delta) so với lúc save. "+
							"→ Mong đợi: eth_getBalance(Block A) phải trả về đúng %s (giá trị lúc Block A), KHÔNG phải %s (giá trị hiện tại Block B)",
						cp.BlockA, balanceA.String(), latestBlock, balanceB.String(),
						cp.SavedBalanceA, deltaBalance.String(),
						cp.SavedBalanceA, balanceB.String(),
					))
					hasError = true
				}

				// 3. Nonce lịch sử get lại phải bằng nonce lưu trước
				if nonceA != cp.SavedNonceA {
					diff := int64(nonceA) - int64(cp.SavedNonceA)
					errDetails = append(errDetails, fmt.Sprintf("Nonce lịch sử get lại (%d) KHÁC với nonce thực tế lưu trữ trước đó (%d). Sai lệch: %+d", nonceA, cp.SavedNonceA, diff))
					hasError = true
				}

				// 4. Nonce lịch sử phải khác nonce hiện tại (chỉ khi nonce hiện tại thực sự đã thay đổi so với mốc lịch sử)
				if nonceB != cp.SavedNonceA && nonceA == nonceB {
					txCount := int64(nonceB) - int64(cp.SavedNonceA)
					errDetails = append(errDetails, fmt.Sprintf(
						"[TROI STATE] Nonce lịch sử tại Block A (%d) = %d, trùng với nonce hiện tại Block B (%d) = %d. "+
							"Nonce gốc lưu lúc save = %d, hiện tại đã tăng thêm %d Tx (từ %d lên %d). "+
							"→ Mong đợi: eth_getTransactionCount(Block A) phải trả về đúng %d (nonce lúc Block A), KHÔNG phải %d (nonce hiện tại Block B)",
						cp.BlockA, nonceA, latestBlock, nonceB,
						cp.SavedNonceA, txCount, cp.SavedNonceA, nonceB,
						cp.SavedNonceA, nonceB,
					))
					hasError = true
				}

				if hasError {
					var errStr strings.Builder
					errStr.WriteString(fmt.Sprintf("Checkpoint Block %d (Wallet: %s):\n", cp.BlockA, cp.FromAddress))
					for _, det := range errDetails {
						errStr.WriteString(fmt.Sprintf("    * %s\n", det))
					}
					errStr.WriteString(queryAllRPCsAndGenerateReport(urls, fromAddr, cp.BlockA))
					allErrDetails = append(allErrDetails, errStr.String())
					hasGlobalError = true
				}
			}

			if hasGlobalError {
				var sb strings.Builder
				sb.WriteString("==================================================\n")
				sb.WriteString(fmt.Sprintf("🚨 LỖI LỊCH SỬ STATE TRÊN NODE %s (XÁC MINH SNAPSHOT/RECOVERY)\n", *targetNodeFlag))
				sb.WriteString(fmt.Sprintf("⏰ Thời gian: %s\n", time.Now().Format(time.RFC3339)))
				sb.WriteString("==================================================\n")
				for _, errDetail := range allErrDetails {
					sb.WriteString(errDetail)
				}
				sb.WriteString("==================================================\n")
				reason := sb.String()
				os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)

				// Ghi đè/thêm vào log file riêng của checker
				appendLocalErrorLog(reason)

				fmt.Printf("\n🚨 %s\n", reason)
				os.Exit(1)
			}

			fmt.Printf("🎉 XÁC MINH THÀNH CÔNG: Node %s trả về toàn bộ dữ liệu lịch sử của %d checkpoints hoàn toàn chính xác!\n", *targetNodeFlag, len(checkpoints))
			os.Remove(*fileFlag)
			fmt.Println("🔄 Đã verify xong, tiếp tục chuyển sang chế độ chạy kiểm tra lịch sử...")
		}
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
