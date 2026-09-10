/*
 * BÀI TEST: 33-state-history
 * MÔ TẢ   : Kiểm tra tính toàn vẹn của State History (lịch sử trạng thái tài khoản)
 *           trên các Node RPC (Archive/State History Nodes) được cấu hình trong rpc_nodes.
 * GỌI     : Gửi Tx tại Block A, lưu snapshot state (Balance, Nonce, AccountState).
 *           Gửi tiếp N giao dịch để đẩy chain sang Block B (Block B > Block A).
 *           Truy vấn lại dữ liệu tại mốc lịch sử Block A và mốc hiện tại Block B
 *           trên TẤT CẢ các RPC node được bật lưu lịch sử (rpc_nodes).
 * KỲ VỌNG :
 *   1. Tại Block A: eth_getBalance, eth_getTransactionCount, mtn_getAccountState
 *      phải trả về ĐÚNG giá trị lịch sử lúc Block A, KHÔNG bị trôi/rò rỉ state của Block B.
 *   2. Tại Block B: trả về giá trị mới nhất tương ứng với sự thay đổi của các giao dịch.
 *   3. Tất cả các Node RPC được bật đều phải phản hồi nhất quán và chính xác.
 */
package main

import (
	"context"
	"crypto/ecdsa"
	"flag"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"tool-test/test-simple/test-rpc/test-chain/config"
)

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

func dialRPCWithTimeout(urlStr string) (*rpc.Client, error) {
	if strings.HasPrefix(urlStr, "http://") || strings.HasPrefix(urlStr, "https://") {
		httpClient := &http.Client{
			Timeout: 30 * time.Second,
		}
		return rpc.DialHTTPWithClient(urlStr, httpClient)
	}
	return rpc.Dial(urlStr)
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
			time.Sleep(500 * time.Millisecond)
			continue
		}
		return err
	}
	return err
}

func sendTxAndWait(ethCli *ethclient.Client, privateKey *ecdsa.PrivateKey, chainID int64, fromAddress, toAddress common.Address) (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	nonce, err := ethCli.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		return 0, fmt.Errorf("lỗi lấy pending nonce: %w", err)
	}

	gasPrice, err := ethCli.SuggestGasPrice(ctx)
	if err != nil {
		return 0, fmt.Errorf("lỗi suggest gas price: %w", err)
	}

	tx := types.NewTransaction(nonce, toAddress, big.NewInt(1), 21000, gasPrice, nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), privateKey)
	if err != nil {
		return 0, fmt.Errorf("lỗi ký transaction: %w", err)
	}

	err = ethCli.SendTransaction(ctx, signedTx)
	if err != nil {
		return 0, fmt.Errorf("lỗi gửi transaction %s: %w", signedTx.Hash().Hex(), err)
	}
	fmt.Printf("   🚀 Đã gửi Tx: %s (Nonce: %d)\n", signedTx.Hash().Hex(), nonce)

	waitStartTime := time.Now()
	for {
		if time.Since(waitStartTime) > 60*time.Second {
			return 0, fmt.Errorf("timeout chờ receipt cho tx %s", signedTx.Hash().Hex())
		}
		rCtx, rCancel := context.WithTimeout(context.Background(), 5*time.Second)
		receipt, errR := ethCli.TransactionReceipt(rCtx, signedTx.Hash())
		rCancel()
		if errR == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			return receipt.BlockNumber.Uint64(), nil
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// collectRPCURLs retrieves all active RPC URLs enabled for history testing.
// Priorities:
// 1. cfg.RPCNodes (if non-empty, use all nodes in the map)
// 2. cfg.RPCUrl (if RPCNodes is empty)
func collectRPCURLs(cfg *config.Config) (map[string]string, []string) {
	nodeMap := make(map[string]string)
	var urls []string

	targetSource := cfg.StateHistoryNodes
	if len(targetSource) == 0 {
		targetSource = cfg.RPCNodes
	}

	if len(targetSource) > 0 {
		var keys []string
		for k := range targetSource {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			u := targetSource[k]
			if u != "" {
				nodeMap[k] = u
				urls = append(urls, u)
			}
		}
	} else if cfg.RPCUrl != "" {
		nodeMap["primary"] = cfg.RPCUrl
		urls = append(urls, cfg.RPCUrl)
	}

	return nodeMap, urls
}

func RunTest(configPath string) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 33-state-history (KIỂM TRA LỊCH SỬ STATE TRÊN CÁC RPC NODES)")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Kiểm tra khả năng lưu trữ và truy vấn State History:")
	fmt.Println("             1. Gửi Tx mốc Block A, lưu lại số dư & nonce tại Block A.")
	fmt.Println("             2. Gửi thêm các Tx tiếp theo để nâng chain lên Block B (Block B > Block A).")
	fmt.Println("             3. Truy vấn lại eth_getBalance, eth_getTransactionCount,")
	fmt.Println("                mtn_getAccountState tại cả Block A và Block B.")
	fmt.Println("🎯 KỲ VỌNG : State tại Block A giữ nguyên chính xác lịch sử lúc Block A,")
	fmt.Println("             không bị trôi state hay rò rỉ dữ liệu mới nhất của Block B.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("không thể tải cấu hình từ %s: %w", configPath, err)
	}

	// Xác định Private Key
	privKeyHex := cfg.PrivateKey
	if privKeyHex == "" && len(cfg.PrivateKeys) > 0 {
		privKeyHex = cfg.PrivateKeys[0]
	}
	if privKeyHex == "" {
		return fmt.Errorf("không tìm thấy private key trong cấu hình")
	}

	privateKey, err := crypto.HexToECDSA(privKeyHex)
	if err != nil {
		return fmt.Errorf("lỗi parse private key: %w", err)
	}
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	toAddress := common.HexToAddress("0x7c6f5e38E6d4457cDdE34134B15FCC04F64bf6bd")

	// Lấy danh sách RPC Nodes được bật rpc_nodes
	nodeMap, urls := collectRPCURLs(cfg)
	if len(urls) == 0 {
		return fmt.Errorf("không tìm thấy RPC URL nào trong cấu hình")
	}

	fmt.Printf("📍 Địa chỉ ví kiểm tra: %s\n", fromAddress.Hex())
	fmt.Printf("🌐 Danh sách RPC Nodes bật lưu lịch sử (%d nodes):\n", len(nodeMap))
	for name, u := range nodeMap {
		fmt.Printf("   • %-10s : %s\n", name, u)
	}

	// Kết nối tới một node RPC khả dụng trong danh sách để gửi giao dịch
	var primaryURL string
	var primaryEthCli *ethclient.Client
	var primaryRpcCli *rpc.Client
	var dialErr error

	for _, u := range urls {
		eCli, errE := ethclient.Dial(u)
		if errE != nil {
			continue
		}
		rCli, errR := dialRPCWithTimeout(u)
		if errR != nil {
			eCli.Close()
			continue
		}
		primaryURL = u
		primaryEthCli = eCli
		primaryRpcCli = rCli
		break
	}

	if primaryEthCli == nil {
		return fmt.Errorf("không thể kết nối tới bất kỳ RPC node nào trong danh sách: %v", dialErr)
	}
	defer primaryEthCli.Close()
	defer primaryRpcCli.Close()
	fmt.Printf("🔌 Sử dụng RPC Node '%s' làm gateway gửi giao dịch\n", primaryURL)

	chainID := cfg.ChainID
	if chainID == 0 {
		cid, err := primaryEthCli.ChainID(context.Background())
		if err != nil {
			return fmt.Errorf("không thể lấy chain ID: %w", err)
		}
		chainID = cid.Int64()
	}

	// ── BƯỚC 1: Gửi Tx 1 để tạo mốc Block A ─────────────────────────────────
	fmt.Println("\n▶️  BƯỚC 1: Gửi giao dịch 1 để xác lập mốc Block A...")
	blockA, err := sendTxAndWait(primaryEthCli, privateKey, chainID, fromAddress, toAddress)
	if err != nil {
		return fmt.Errorf("gửi Tx 1 thất bại: %w", err)
	}
	fmt.Printf("✅ Giao dịch 1 đã mined tại Block A: %d\n", blockA)

	// Chờ một chút để trie commit changelog trên Block A
	time.Sleep(500 * time.Millisecond)

	blockAHex := hexutil.EncodeUint64(blockA)
	ctxA, cancelA := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelA()

	var savedBalanceAHex string
	if err := callContextWithRetry(ctxA, primaryRpcCli, &savedBalanceAHex, "eth_getBalance", fromAddress, blockAHex); err != nil {
		return fmt.Errorf("lỗi lấy eth_getBalance tại Block A từ %s: %w", primaryURL, err)
	}
	savedBalanceA, _ := hexutil.DecodeBig(savedBalanceAHex)

	var savedNonceAHex string
	if err := callContextWithRetry(ctxA, primaryRpcCli, &savedNonceAHex, "eth_getTransactionCount", fromAddress, blockAHex); err != nil {
		return fmt.Errorf("lỗi lấy eth_getTransactionCount tại Block A từ %s: %w", primaryURL, err)
	}
	savedNonceA, _ := hexutil.DecodeUint64(savedNonceAHex)

	fmt.Printf("   [Snapshot Block A (%d)] Balance: %v | Nonce: %d\n", blockA, savedBalanceA, savedNonceA)

	// ── BƯỚC 2: Gửi thêm giao dịch để tạo mốc Block B ───────────────────────
	extraTxs := 2
	fmt.Printf("\n▶️  BƯỚC 2: Gửi thêm %d giao dịch để đẩy chain lên mốc Block B...\n", extraTxs)
	var blockB uint64 = blockA
	for i := 1; i <= extraTxs; i++ {
		b, err := sendTxAndWait(primaryEthCli, privateKey, chainID, fromAddress, toAddress)
		if err != nil {
			return fmt.Errorf("gửi giao dịch phụ %d/%d thất bại: %w", i, extraTxs, err)
		}
		blockB = b
		fmt.Printf("   ✅ Tx phụ %d/%d mined tại Block: %d\n", i, extraTxs, b)
	}

	if blockB <= blockA {
		fmt.Printf("⚠️  Cảnh báo: Block B (%d) không lớn hơn Block A (%d), gửi thêm 1 Tx nữa...\n", blockB, blockA)
		b, err := sendTxAndWait(primaryEthCli, privateKey, chainID, fromAddress, toAddress)
		if err != nil {
			return fmt.Errorf("gửi tx bổ sung thất bại: %w", err)
		}
		blockB = b
	}

	fmt.Printf("\n📍 Đã chốt 2 mốc kiểm tra: Block A (Lịch sử) = %d | Block B (Hiện tại) = %d\n", blockA, blockB)
	blockBHex := hexutil.EncodeUint64(blockB)

	// Chờ các node RPC đồng bộ đầy đủ tới ít nhất Block B
	time.Sleep(1 * time.Second)

	// ── BƯỚC 3: Đối chiếu lịch sử trên TẤT CẢ các RPC node được bật ──────────
	fmt.Println("\n==========================================================")
	fmt.Println("BƯỚC 3: XÁC MINH TRẠNG THÁI LỊCH SỬ TRÊN TOÀN BỘ CÁC RPC NODES")
	fmt.Println("==========================================================")

	var allErrors []string

	for name, rpcURL := range nodeMap {
		fmt.Printf("\n🔍 Đang kiểm tra Node '%s' (%s)...\n", name, rpcURL)

		client, errDial := dialRPCWithTimeout(rpcURL)
		if errDial != nil {
			allErrors = append(allErrors, fmt.Sprintf("Node %s (%s): Không thể kết nối: %v", name, rpcURL, errDial))
			continue
		}

		// Đợi node này sync tới ít nhất Block B (tối đa 20s)
		waitStart := time.Now()
		synced := false
		for time.Since(waitStart) < 20*time.Second {
			cCtx, cCancel := context.WithTimeout(context.Background(), 5*time.Second)
			var curBlockHex string
			errCB := client.CallContext(cCtx, &curBlockHex, "eth_blockNumber")
			cCancel()
			if errCB == nil {
				curBlock, _ := hexutil.DecodeUint64(curBlockHex)
				if curBlock >= blockB {
					synced = true
					break
				}
			}
			time.Sleep(500 * time.Millisecond)
		}

		if !synced {
			allErrors = append(allErrors, fmt.Sprintf("Node %s (%s): Chưa đồng bộ tới Block B (%d) sau 20s", name, rpcURL, blockB))
			client.Close()
			continue
		}

		// Query tại Block A
		var balanceAHex string
		var nonceAHex string
		var stateA AccountStateResult

		qCtxA, qCancelA := context.WithTimeout(context.Background(), 15*time.Second)
		errBalA := callContextWithRetry(qCtxA, client, &balanceAHex, "eth_getBalance", fromAddress, blockAHex)
		errNonA := callContextWithRetry(qCtxA, client, &nonceAHex, "eth_getTransactionCount", fromAddress, blockAHex)
		errStA := callContextWithRetry(qCtxA, client, &stateA, "mtn_getAccountState", fromAddress, blockAHex)
		qCancelA()

		if errBalA != nil || errNonA != nil || errStA != nil {
			allErrors = append(allErrors, fmt.Sprintf("Node %s: Lỗi query Block A (bal_err: %v, nonce_err: %v, state_err: %v)", name, errBalA, errNonA, errStA))
			client.Close()
			continue
		}

		balanceA, _ := hexutil.DecodeBig(balanceAHex)
		nonceA, _ := hexutil.DecodeUint64(nonceAHex)

		// Query tại Block B
		var balanceBHex string
		var nonceBHex string
		var stateB AccountStateResult

		qCtxB, qCancelB := context.WithTimeout(context.Background(), 15*time.Second)
		errBalB := callContextWithRetry(qCtxB, client, &balanceBHex, "eth_getBalance", fromAddress, blockBHex)
		errNonB := callContextWithRetry(qCtxB, client, &nonceBHex, "eth_getTransactionCount", fromAddress, blockBHex)
		errStB := callContextWithRetry(qCtxB, client, &stateB, "mtn_getAccountState", fromAddress, blockBHex)
		qCancelB()
		client.Close()

		if errBalB != nil || errNonB != nil || errStB != nil {
			allErrors = append(allErrors, fmt.Sprintf("Node %s: Lỗi query Block B (bal_err: %v, nonce_err: %v, state_err: %v)", name, errBalB, errNonB, errStB))
			continue
		}

		balanceB, _ := hexutil.DecodeBig(balanceBHex)
		nonceB, _ := hexutil.DecodeUint64(nonceBHex)

		fmt.Printf("   💰 Block A (Lịch sử %d) : eth_bal=%s | mtn_bal=%s | nonce=%d | mtn_nonce=%d\n",
			blockA, balanceA.String(), stateA.Balance, nonceA, stateA.Nonce)
		fmt.Printf("   💰 Block B (Hiện tại %d) : eth_bal=%s | mtn_bal=%s | nonce=%d | mtn_nonce=%d\n",
			blockB, balanceB.String(), stateB.Balance, nonceB, stateB.Nonce)

		// ── Kiểm tra tính đúng đắn của State History ──
		nodeErrors := []string{}

		// 1. Số dư lịch sử tại Block A get lại phải bằng savedBalanceA
		if balanceA.Cmp(savedBalanceA) != 0 {
			nodeErrors = append(nodeErrors, fmt.Sprintf("eth_getBalance(Block A) = %s, khác với số dư lưu trước %s", balanceA.String(), savedBalanceA.String()))
		}
		if stateA.Balance != savedBalanceA.String() {
			nodeErrors = append(nodeErrors, fmt.Sprintf("mtn_getAccountState.balance(Block A) = %s, khác với số dư lưu trước %s", stateA.Balance, savedBalanceA.String()))
		}

		// 2. Nonce lịch sử tại Block A phải bằng savedNonceA
		if nonceA != savedNonceA {
			nodeErrors = append(nodeErrors, fmt.Sprintf("eth_getTransactionCount(Block A) = %d, khác với nonce lưu trước %d", nonceA, savedNonceA))
		}
		if stateA.Nonce != savedNonceA {
			nodeErrors = append(nodeErrors, fmt.Sprintf("mtn_getAccountState.nonce(Block A) = %d, khác với nonce lưu trước %d", stateA.Nonce, savedNonceA))
		}

		// 3. Số dư & Nonce tại Block B phải phản ánh đúng các giao dịch đã thực hiện
		if nonceB <= savedNonceA {
			nodeErrors = append(nodeErrors, fmt.Sprintf("Nonce tại Block B (%d) không lớn hơn Nonce lúc Block A (%d)", nonceB, savedNonceA))
		}
		if nonceA == nonceB && blockB > blockA {
			nodeErrors = append(nodeErrors, fmt.Sprintf("Nonce Block A và Block B bằng nhau (%d) dù đã gửi thêm giao dịch", nonceA))
		}

		// 4. Đồng bộ giữa mtn_getAccountState và eth_* endpoints
		if stateA.Balance != balanceA.String() {
			nodeErrors = append(nodeErrors, fmt.Sprintf("Tại Block A: mtn_getAccountState.balance (%s) khác eth_getBalance (%s)", stateA.Balance, balanceA.String()))
		}
		if stateB.Balance != balanceB.String() {
			nodeErrors = append(nodeErrors, fmt.Sprintf("Tại Block B: mtn_getAccountState.balance (%s) khác eth_getBalance (%s)", stateB.Balance, balanceB.String()))
		}

		if len(nodeErrors) > 0 {
			fmt.Printf("   ❌ Node %s phát hiện sai lệch lịch sử state:\n", name)
			for _, ne := range nodeErrors {
				fmt.Printf("      - %s\n", ne)
				allErrors = append(allErrors, fmt.Sprintf("Node %s: %s", name, ne))
			}
		} else {
			fmt.Printf("   ✅ Node %s: Dữ liệu lịch sử Block A và hiện tại Block B HOÀN TOÀN CHÍNH XÁC!\n", name)
		}
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("phát hiện %d lỗi trong quá trình kiểm tra State History:\n   • %s", len(allErrors), strings.Join(allErrors, "\n   • "))
	}

	fmt.Println("\n🎉 THÀNH CÔNG: Toàn bộ RPC Nodes cấu hình rpc_nodes đều trả về State History chính xác tuyệt đối!")
	return nil
}

func main() {
	configPath := flag.String("config", "../config.json", "Đường dẫn file cấu hình config.json")
	flag.Parse()

	cfgPath := *configPath
	if len(flag.Args()) > 0 {
		cfgPath = flag.Args()[0]
	}

	if err := RunTest(cfgPath); err != nil {
		fmt.Printf("\n❌ TEST FAILED: %v\n", err)
		os.Exit(1)
	}
}
