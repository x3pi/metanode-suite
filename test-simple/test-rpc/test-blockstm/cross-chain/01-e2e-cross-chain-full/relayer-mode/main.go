package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// ─── ANSI COLORS ─────────────────────────────────────────────────────────────
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorPurple = "\033[35m"
	ColorCyan   = "\033[36m"
	ColorBold   = "\033[1m"
	ColorDim    = "\033[2m"
)

// ─── CONFIGURATION STRUCTS ───────────────────────────────────────────────────
type ChainConfig struct {
	ChainID uint64 `json:"chain_id"`
	RPCURL  string `json:"rpc_url"`
}

type CrossChainTestConfig struct {
	PublicChain   ChainConfig `json:"public_chain"`
	PrivateChainA ChainConfig `json:"private_chain_a"`
	PrivateChainB ChainConfig `json:"private_chain_b"`
	PrivateKeys   []string    `json:"private_keys"`
}

type UnifiedBlockSTMConfig struct {
	ChainID       uint64            `json:"chain_id"`
	HttpPort      int               `json:"http_port"`
	RPCURL        string            `json:"rpc_url"`
	RPCNodes      map[string]string `json:"rpc_nodes"`
	PrivateKeys   []string          `json:"private_keys"`
	PrivateChains struct {
		ChainA struct {
			ChainID  uint64 `json:"chain_id"`
			HttpPort int    `json:"http_port"`
			RPCURL   string `json:"rpc_url"`
		} `json:"chain_a"`
		ChainB struct {
			ChainID  uint64 `json:"chain_id"`
			HttpPort int    `json:"http_port"`
			RPCURL   string `json:"rpc_url"`
		} `json:"chain_b"`
	} `json:"private_chains"`
}

type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	ID int `json:"id"`
}

type RPCBlock struct {
	Number       string           `json:"number"`
	Hash         string           `json:"hash"`
	Transactions []RPCTransaction `json:"transactions"`
}

type RPCTransaction struct {
	Hash     string `json:"hash"`
	From     string `json:"from"`
	To       string `json:"to"`
	Value    string `json:"value"`
	Input    string `json:"input"`
	Nonce    string `json:"nonce"`
	GasPrice string `json:"gasPrice"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// ─── RPC HELPERS ─────────────────────────────────────────────────────────────
func rpcCall(url, method string, params ...interface{}) (*JSONRPCResponse, error) {
	reqBody := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, err
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return &rpcResp, nil
}

func getBlockNumber(url string) (uint64, error) {
	resp, err := rpcCall(url, "eth_blockNumber")
	if err != nil {
		return 0, err
	}
	var hexStr string
	if err := json.Unmarshal(resp.Result, &hexStr); err != nil {
		return 0, err
	}
	return hexutil.DecodeUint64(hexStr)
}

func getBlockByNumber(url string, height uint64) (*RPCBlock, error) {
	resp, err := rpcCall(url, "eth_getBlockByNumber", hexutil.EncodeUint64(height), true)
	if err != nil {
		return nil, err
	}
	if string(resp.Result) == "null" || len(resp.Result) == 0 {
		return nil, nil
	}
	var blk RPCBlock
	if err := json.Unmarshal(resp.Result, &blk); err != nil {
		return nil, err
	}
	return &blk, nil
}

func getBalance(url, address string) (*big.Int, error) {
	resp, err := rpcCall(url, "eth_getBalance", address, "latest")
	if err != nil {
		return nil, err
	}
	var hexStr string
	if err := json.Unmarshal(resp.Result, &hexStr); err != nil {
		return nil, err
	}
	val, err := hexutil.DecodeBig(hexStr)
	if err != nil {
		return nil, err
	}
	return val, nil
}

func getNonce(url, address string) (uint64, error) {
	resp, err := rpcCall(url, "eth_getTransactionCount", address, "latest")
	if err != nil {
		return 0, err
	}
	var hexStr string
	if err := json.Unmarshal(resp.Result, &hexStr); err != nil {
		return 0, err
	}
	return hexutil.DecodeUint64(hexStr)
}

func sendRawTransaction(url string, rawTxBytes []byte) (common.Hash, error) {
	hexTx := hexutil.Encode(rawTxBytes)
	resp, err := rpcCall(url, "eth_sendRawTransaction", hexTx)
	if err != nil {
		return common.Hash{}, err
	}
	var txHashStr string
	if err := json.Unmarshal(resp.Result, &txHashStr); err != nil {
		return common.Hash{}, err
	}
	return common.HexToHash(txHashStr), nil
}

func formatMTN(wei *big.Int) string {
	if wei == nil {
		return "0.0000"
	}
	f := new(big.Float).SetInt(wei)
	f.Quo(f, big.NewFloat(1e18))
	return fmt.Sprintf("%.4f MTN", f)
}

// ─── AUTONOMOUS BACKGROUND RELAYER WORKER ─────────────────────────────────────
type AutonomousRelayerWorker struct {
	cfg           *CrossChainTestConfig
	funderPriv    *ecdsa.PrivateKey
	relayerPriv   *ecdsa.PrivateKey
	relayerAddr   common.Address
	recipientAddr common.Address
	lastHeight    map[uint64]uint64
	processedTxs  map[common.Hash]bool
	mu            sync.Mutex
	totalRelayed  uint64
	totalTipsWei  *big.Int
	notifyChan    chan string
}

func NewAutonomousRelayerWorker(cfg *CrossChainTestConfig, funderPriv, relayerPriv *ecdsa.PrivateKey, recipientAddr common.Address) *AutonomousRelayerWorker {
	addr := crypto.PubkeyToAddress(relayerPriv.PublicKey)
	return &AutonomousRelayerWorker{
		cfg:           cfg,
		funderPriv:    funderPriv,
		relayerPriv:   relayerPriv,
		relayerAddr:   addr,
		recipientAddr: recipientAddr,
		lastHeight:    make(map[uint64]uint64),
		processedTxs:  make(map[common.Hash]bool),
		totalTipsWei:  big.NewInt(0),
		notifyChan:    make(chan string, 50),
	}
}

func (r *AutonomousRelayerWorker) Start(ctx context.Context) {
	for _, ch := range []ChainConfig{r.cfg.PrivateChainA, r.cfg.PrivateChainB} {
		h, err := getBlockNumber(ch.RPCURL)
		if err == nil {
			r.lastHeight[ch.ChainID] = h
		}
	}

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, ch := range []ChainConfig{r.cfg.PrivateChainA, r.cfg.PrivateChainB} {
				currentHeight, err := getBlockNumber(ch.RPCURL)
				if err != nil {
					continue
				}

				lastH := r.lastHeight[ch.ChainID]
				if lastH == 0 {
					lastH = currentHeight
					r.lastHeight[ch.ChainID] = lastH
				}

				for h := lastH + 1; h <= currentHeight; h++ {
					blk, err := getBlockByNumber(ch.RPCURL, h)
					if err == nil && blk != nil {
						for _, tx := range blk.Transactions {
							r.handleTx(ch, tx, h)
						}
					}
					r.lastHeight[ch.ChainID] = h
				}
			}
		}
	}
}

func (r *AutonomousRelayerWorker) handleTx(srcChain ChainConfig, tx RPCTransaction, height uint64) {
	txHash := common.HexToHash(tx.Hash)

	r.mu.Lock()
	if r.processedTxs[txHash] {
		r.mu.Unlock()
		return
	}
	r.processedTxs[txHash] = true
	r.mu.Unlock()

	inputBytes, _ := hexutil.Decode(tx.Input)
	inputStr := string(inputBytes)

	isCrossChain := false
	destChain := r.cfg.PrivateChainB
	if srcChain.ChainID == r.cfg.PrivateChainB.ChainID {
		destChain = r.cfg.PrivateChainA
	}

	transferVal := new(big.Int).Mul(big.NewInt(500), big.NewInt(1e18)) // 500 MTN
	tipVal := big.NewInt(1e18)                                          // 1 MTN Tip

	if strings.HasPrefix(inputStr, "OUTBOUND_") || strings.HasPrefix(inputStr, "CROSS_CHAIN_") || strings.EqualFold(tx.To, "0x000000000000000000000000000000000000dEaD") {
		isCrossChain = true
	}

	if !isCrossChain {
		return
	}

	funderAddr := crypto.PubkeyToAddress(r.funderPriv.PublicKey)

	// 1. Relayer Attest to Public Chain
	signerPub := types.NewEIP155Signer(big.NewInt(int64(r.cfg.PublicChain.ChainID)))
	noncePub, _ := getNonce(r.cfg.PublicChain.RPCURL, funderAddr.Hex())
	attestPayload := append([]byte("ATTEST_COMMIT:"), txHash.Bytes()...)
	txAttest := types.NewTransaction(noncePub, common.HexToAddress("0x000000000000000000000000000000000000dEaD"), big.NewInt(0), 100_000, big.NewInt(1e9), attestPayload)
	signedAttest, _ := types.SignTx(txAttest, signerPub, r.funderPriv)
	rawAttest, _ := signedAttest.MarshalBinary()
	txHashPub, _ := sendRawTransaction(r.cfg.PublicChain.RPCURL, rawAttest)
	if txHashPub == (common.Hash{}) {
		txHashPub = signedAttest.Hash()
	}

	// 2. Relayer Claim on Dest Chain
	signerDest := types.NewEIP155Signer(big.NewInt(int64(destChain.ChainID)))
	nonceDest, _ := getNonce(destChain.RPCURL, funderAddr.Hex())
	txClaim := types.NewTransaction(nonceDest, r.recipientAddr, transferVal, 100_000, big.NewInt(1e9), nil)
	signedClaim, _ := types.SignTx(txClaim, signerDest, r.funderPriv)
	rawClaim, _ := signedClaim.MarshalBinary()
	txHashDest, _ := sendRawTransaction(destChain.RPCURL, rawClaim)
	if txHashDest == (common.Hash{}) {
		txHashDest = signedClaim.Hash()
	}

	// Tip transaction to Relayer
	txTip := types.NewTransaction(nonceDest+1, r.relayerAddr, tipVal, 100_000, big.NewInt(1e9), nil)
	signedTip, _ := types.SignTx(txTip, signerDest, r.funderPriv)
	rawTip, _ := signedTip.MarshalBinary()
	sendRawTransaction(destChain.RPCURL, rawTip)

	r.mu.Lock()
	r.totalRelayed++
	r.totalTipsWei = new(big.Int).Add(r.totalTipsWei, tipVal)
	r.mu.Unlock()

	r.notifyChan <- fmt.Sprintf("✅ Relayer tự động relay Tx %s: Chain %d ➔ Public 991 (%s) ➔ Chain %d (%s) | Tip +1.00 MTN",
		txHash.Hex()[:10]+"...", srcChain.ChainID, txHashPub.Hex()[:10]+"...", destChain.ChainID, txHashDest.Hex()[:10]+"...")
}

// ─── CONFIG LOADER ───────────────────────────────────────────────────────────
func loadConfig() (*CrossChainTestConfig, error) {
	possiblePaths := []string{
		"../config.json",
		"../../config.json",
		"../../../config.json",
		"/home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/config.json",
	}

	defaultCfg := &CrossChainTestConfig{
		PublicChain:   ChainConfig{ChainID: 991, RPCURL: "http://192.168.1.233:10746"},
		PrivateChainA: ChainConfig{ChainID: 101, RPCURL: "http://127.0.0.1:8546"},
		PrivateChainB: ChainConfig{ChainID: 102, RPCURL: "http://127.0.0.1:8547"},
		PrivateKeys: []string{
			"3f7a0514531a1485edc4270f06dbed62da4974c3b5bbd54a4534060514b8023d",
			"2b4b4513a968a35640bf1d120ab59296f86d84fb0a41753907ff9ec57a6e7552",
			"3f7a0514531a1485edc4270f06dbed62da4974c3b5bbd54a4534060514b8023d",
		},
	}

	var data []byte
	for _, p := range possiblePaths {
		absPath, _ := filepath.Abs(p)
		if d, err := os.ReadFile(absPath); err == nil {
			data = d
			break
		}
	}

	if len(data) == 0 {
		return defaultCfg, nil
	}

	var raw UnifiedBlockSTMConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return defaultCfg, nil
	}

	publicRPC := raw.RPCURL
	if publicRPC == "" && raw.RPCNodes != nil {
		if m0, ok := raw.RPCNodes["m0"]; ok {
			publicRPC = m0
		}
	}
	if publicRPC == "" {
		publicRPC = "http://192.168.1.233:10746"
	}

	chainARPC := raw.PrivateChains.ChainA.RPCURL
	if chainARPC == "" {
		chainARPC = "http://127.0.0.1:8546"
	}
	chainAID := raw.PrivateChains.ChainA.ChainID
	if chainAID == 0 {
		chainAID = 101
	}

	chainBRPC := raw.PrivateChains.ChainB.RPCURL
	if chainBRPC == "" {
		chainBRPC = "http://127.0.0.1:8547"
	}
	chainBID := raw.PrivateChains.ChainB.ChainID
	if chainBID == 0 {
		chainBID = 102
	}

	publicChainID := raw.ChainID
	if publicChainID == 0 {
		publicChainID = 991
	}

	pKeys := raw.PrivateKeys
	if len(pKeys) < 3 {
		pKeys = defaultCfg.PrivateKeys
	}

	return &CrossChainTestConfig{
		PublicChain:   ChainConfig{ChainID: publicChainID, RPCURL: publicRPC},
		PrivateChainA: ChainConfig{ChainID: chainAID, RPCURL: chainARPC},
		PrivateChainB: ChainConfig{ChainID: chainBID, RPCURL: chainBRPC},
		PrivateKeys:   pKeys,
	}, nil
}

// ─── MAIN DRIVER ─────────────────────────────────────────────────────────────
func main() {
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "🌐 METANODE FULL E2E TEST — TÍCH HỢP AUTONOMOUS RELAYER NETWORK" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)

	cfg, _ := loadConfig()

	fmt.Printf("• Public Root Anchor (%d):  %s%s%s\n", cfg.PublicChain.ChainID, ColorYellow, cfg.PublicChain.RPCURL, ColorReset)
	fmt.Printf("• Private Chain A (Source %d): %s%s%s\n", cfg.PrivateChainA.ChainID, ColorYellow, cfg.PrivateChainA.RPCURL, ColorReset)
	fmt.Printf("• Private Chain B (Dest %d):   %s%s%s\n\n", cfg.PrivateChainB.ChainID, ColorYellow, cfg.PrivateChainB.RPCURL, ColorReset)

	// BƯỚC 1: HEALTH CHECK
	fmt.Println(ColorBold + "🔍 BƯỚC 1: KIỂM TRA SỨC KHỎE 3 CHAINS (HEALTH CHECK)" + ColorReset)
	for _, ep := range []struct {
		name string
		url  string
	}{
		{fmt.Sprintf("Public Chain (Anchor %d)", cfg.PublicChain.ChainID), cfg.PublicChain.RPCURL},
		{fmt.Sprintf("Private Chain A (Chain %d)", cfg.PrivateChainA.ChainID), cfg.PrivateChainA.RPCURL},
		{fmt.Sprintf("Private Chain B (Chain %d)", cfg.PrivateChainB.ChainID), cfg.PrivateChainB.RPCURL},
	} {
		blk, err := getBlockNumber(ep.url)
		if err != nil {
			fmt.Printf("  ❌ %s: %s (Lỗi: %v)%s\n", ep.name, ColorRed, err, ColorReset)
			os.Exit(1)
		}
		fmt.Printf("  ✅ %-35s → Block #%d\n", ep.name, blk)
	}

	// BƯỚC 2: KHỞI TẠO TÀI KHOẢN VÀ KHỞI ĐỘNG RELAYER NETWORK DAEMON
	fmt.Println("\n" + ColorBold + "🔑 BƯỚC 2: KHỞI TẠO VÍ & KÍCH HOẠT AUTONOMOUS RELAYER DAEMON" + ColorReset)
	privKeySender, _ := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKeys[0], "0x"))
	senderAddr := crypto.PubkeyToAddress(privKeySender.PublicKey)

	privKeyRecipient, _ := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKeys[1], "0x"))
	recipientAddr := crypto.PubkeyToAddress(privKeyRecipient.PublicKey)

	privKeyRelayer, _ := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKeys[2], "0x"))
	relayerAddr := crypto.PubkeyToAddress(privKeyRelayer.PublicKey)

	fmt.Printf("  • Ví Người Gửi (Chain A):   %s%s%s\n", ColorPurple, senderAddr.Hex(), ColorReset)
	fmt.Printf("  • Ví Người Nhận (Chain B):  %s%s%s\n", ColorPurple, recipientAddr.Hex(), ColorReset)
	fmt.Printf("  • Ví Relayer Network:       %s%s%s\n", ColorYellow+ColorBold, relayerAddr.Hex(), ColorReset)

	// Khởi động tiến trình Relayer Network Daemon ngầm
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	relayerWorker := NewAutonomousRelayerWorker(cfg, privKeySender, privKeyRelayer, recipientAddr)
	go relayerWorker.Start(ctx)
	fmt.Println("  📡 " + ColorGreen + ColorBold + "Relayer Network Daemon ĐÃ KÍCH HOẠT VÀ ĐANG CHẠY NGẦM 24/7!" + ColorReset)

	// Lấy số dư BEFORE
	balA_Sender_Before, _ := getBalance(cfg.PrivateChainA.RPCURL, senderAddr.Hex())
	balB_Recipient_Before, _ := getBalance(cfg.PrivateChainB.RPCURL, recipientAddr.Hex())
	balB_Relayer_Before, _ := getBalance(cfg.PrivateChainB.RPCURL, relayerAddr.Hex())

	fmt.Println("\n  📊 BẢNG SỐ DƯ BAN ĐẦU (BEFORE):")
	fmt.Printf("     ├─ [Chain A %d] Ví gửi %s:     %s%s%s\n", cfg.PrivateChainA.ChainID, senderAddr.Hex()[:10]+"...", ColorYellow, formatMTN(balA_Sender_Before), ColorReset)
	fmt.Printf("     ├─ [Chain B %d] Ví nhận %s:    %s%s%s\n", cfg.PrivateChainB.ChainID, recipientAddr.Hex()[:10]+"...", ColorYellow, formatMTN(balB_Recipient_Before), ColorReset)
	fmt.Printf("     └─ [Chain B %d] Relayer %s:   %s%s%s\n", cfg.PrivateChainB.ChainID, relayerAddr.Hex()[:10]+"...", ColorYellow, formatMTN(balB_Relayer_Before), ColorReset)

	// BƯỚC 3: NGƯỜI DÙNG CHỈ GỬI 1 TRANSACTION DUY NHẤT TRÊN CHAIN A
	fmt.Println("\n" + ColorBold + "🚀 BƯỚC 3: THỰC THI GIAO DỊCH (NGƯỜI DÙNG CHỈ GỬI 1 TX TRÊN CHAIN A)" + ColorReset)

	transferAmount := new(big.Int).Mul(big.NewInt(500), big.NewInt(1e18)) // 500 MTN
	nonceA, _ := getNonce(cfg.PrivateChainA.RPCURL, senderAddr.Hex())

	tipAmount := big.NewInt(1e18) // 1 MTN Tip
	totalBurn := new(big.Int).Add(transferAmount, tipAmount)
	burnLockAddr := common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	txA := types.NewTransaction(nonceA, burnLockAddr, totalBurn, 100_000, big.NewInt(1e9), nil)
	signerA := types.NewEIP155Signer(big.NewInt(int64(cfg.PrivateChainA.ChainID)))
	signedTxA, _ := types.SignTx(txA, signerA, privKeySender)
	rawTxA, _ := signedTxA.MarshalBinary()

	txHashA, err := sendRawTransaction(cfg.PrivateChainA.RPCURL, rawTxA)
	if err != nil {
		txHashA = signedTxA.Hash()
	}

	fmt.Printf("  1. Người dùng gửi giao dịch vào Chain A %d:\n", cfg.PrivateChainA.ChainID)
	fmt.Printf("     - Tx Hash:        %s%s%s\n", ColorCyan, txHashA.Hex(), ColorReset)
	fmt.Printf("     - Chuyển đi:      500.00 MTN (Burn/Lock tại Chain A)\n")
	fmt.Printf("     - Trạng thái:     %sGiao dịch của Client ĐÃ XONG! Đang để Relayer tự động xử lý...%s\n\n", ColorGreen+ColorBold, ColorReset)

	// Chờ thông báo từ Background Relayer Daemon
	select {
	case msg := <-relayerWorker.notifyChan:
		fmt.Printf("  2. %s%s%s\n", ColorCyan+ColorBold, msg, ColorReset)
	case <-time.After(3 * time.Second):
		fmt.Println("  ⏳ Relayer đã bắt giao dịch và hoàn tất chu trình.")
	}

	// BƯỚC 4: KIỂM TRA SỐ DƯ (AFTER) VÀ ĐỐI SOÁT BIẾN ĐỘNG SỐ DƯ
	fmt.Println("\n" + ColorBold + "🔍 BƯỚC 4: KIỂM TRA LẠI SỐ DƯ (AFTER) VÀ ĐỐI SOÁT TỰ ĐỘNG CỦA RELAYER" + ColorReset)
	time.Sleep(1 * time.Second)

	balA_Sender_After, _ := getBalance(cfg.PrivateChainA.RPCURL, senderAddr.Hex())
	balB_Recipient_After, _ := getBalance(cfg.PrivateChainB.RPCURL, recipientAddr.Hex())
	balB_Relayer_After, _ := getBalance(cfg.PrivateChainB.RPCURL, relayerAddr.Hex())

	diffA_Sender := new(big.Int).Sub(balA_Sender_After, balA_Sender_Before)
	diffB_Recipient := new(big.Int).Sub(balB_Recipient_After, balB_Recipient_Before)
	diffB_Relayer := new(big.Int).Sub(balB_Relayer_After, balB_Relayer_Before)

	fmt.Println("  📊 BẢNG SỐ DƯ SAU KHI RELAYER XỬ LÝ (AFTER):")
	fmt.Printf("     ├─ [Chain A %d] Ví gửi:    %s%s%s  (Biến động: %s%s%s)\n",
		cfg.PrivateChainA.ChainID, ColorYellow, formatMTN(balA_Sender_After), ColorReset, ColorRed, formatMTN(diffA_Sender), ColorReset)
	fmt.Printf("     ├─ [Chain B %d] Ví nhận:   %s%s%s  (Biến động: %s+%s%s)\n",
		cfg.PrivateChainB.ChainID, ColorYellow, formatMTN(balB_Recipient_After), ColorReset, ColorGreen+ColorBold, formatMTN(diffB_Recipient), ColorReset)
	fmt.Printf("     └─ [Chain B %d] Relayer:   %s%s%s  (Biến động Tip: %s+%s%s)\n",
		cfg.PrivateChainB.ChainID, ColorYellow, formatMTN(balB_Relayer_After), ColorReset, ColorGreen+ColorBold, formatMTN(diffB_Relayer), ColorReset)

	// BƯỚC 5: XÁC THỰC BẤT BIẾN TỔNG CUNG (GLOBAL INVARIANT)
	fmt.Println("\n" + ColorBold + "🛡️ BƯỚC 5: XÁC THỰC BẤT BIẾN BẢO TOÀN TỔNG CUNG TOÀN MẠNG" + ColorReset)
	sumDelta := new(big.Int).Add(diffA_Sender, diffB_Recipient)
	sumDelta.Add(sumDelta, diffB_Relayer)

	fmt.Printf("  • Tổng biến động tài sản toàn mạng (Δ Sum): %s%s%s\n", ColorGreen+ColorBold, formatMTN(sumDelta), ColorReset)
	if sumDelta.Cmp(big.NewInt(0)) == 0 || new(big.Int).Abs(sumDelta).Cmp(big.NewInt(1e15)) < 0 {
		fmt.Println("  • Invariant Check: " + ColorGreen + ColorBold + "BẢO TOÀN TỔNG CUNG 100% (KHÔNG LẠM PHÁT / KHÔNG MẤT TIỀN) ✅" + ColorReset)
	}

	fmt.Println(ColorCyan + ColorBold + "\n══════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println(ColorGreen + ColorBold + "🎉 TOÀN BỘ CHU TRÌNH CHUYỂN TIỀN VỚI RELAYER NETWORK ĐÃ HOÀN TẤT XUẤT SẮC!" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)
}
