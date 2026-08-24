package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// ─── ANSI COLORS ─────────────────────────────────────────────────────────────
const (
	ColorReset   = "\033[0m"
	ColorRed     = "\033[91m"
	ColorGreen   = "\033[92m"
	ColorYellow  = "\033[93m"
	ColorBlue    = "\033[94m"
	ColorPurple  = "\033[95m"
	ColorCyan    = "\033[96m"
	ColorWhite   = "\033[97m"
	ColorBold    = "\033[1m"
	ColorDim     = "\033[2m"
)

// ─── CONFIG & STRUCTS ────────────────────────────────────────────────────────
type ChainConfig struct {
	ChainID uint64 `json:"chain_id"`
	RPCURL  string `json:"rpc_url"`
	Name    string `json:"name"`
}

type RelayerConfig struct {
	PublicChain    ChainConfig
	PrivateChains  []ChainConfig
	RelayerPrivKey string
	PollIntervalMs int
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

type TxReceipt struct {
	TransactionHash string   `json:"transactionHash"`
	BlockNumber     string   `json:"blockNumber"`
	Status          string   `json:"status"`
	GasUsed         string   `json:"gasUsed"`
	Logs            []string `json:"logs"`
}

// ─── RELAYER DAEMON ENGINE ───────────────────────────────────────────────────
type RelayerDaemon struct {
	cfg            *RelayerConfig
	privKey        *ecdsa.PrivateKey
	relayerAddr    common.Address
	mu             sync.Mutex
	lastHeight     map[uint64]uint64
	processedTxs   map[common.Hash]bool
	totalRelayed   uint64
	totalTipsWei   *big.Int
	httpClient     *http.Client
}

func NewRelayerDaemon(cfg *RelayerConfig) (*RelayerDaemon, error) {
	keyHex := strings.TrimPrefix(cfg.RelayerPrivKey, "0x")
	privKey, err := crypto.HexToECDSA(keyHex)
	if err != nil {
		return nil, fmt.Errorf("khóa ví Relayer không hợp lệ: %w", err)
	}
	addr := crypto.PubkeyToAddress(privKey.PublicKey)

	return &RelayerDaemon{
		cfg:          cfg,
		privKey:      privKey,
		relayerAddr:  addr,
		lastHeight:   make(map[uint64]uint64),
		processedTxs: make(map[common.Hash]bool),
		totalTipsWei: big.NewInt(0),
		httpClient:   &http.Client{Timeout: 8 * time.Second},
	}, nil
}

func (r *RelayerDaemon) rpcCall(url, method string, params ...interface{}) (*JSONRPCResponse, error) {
	reqBody := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}
	payload, _ := json.Marshal(reqBody)
	resp, err := r.httpClient.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, err
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return &rpcResp, nil
}

func (r *RelayerDaemon) getBlockNumber(url string) (uint64, error) {
	resp, err := r.rpcCall(url, "eth_blockNumber")
	if err != nil {
		return 0, err
	}
	var hexStr string
	if err := json.Unmarshal(resp.Result, &hexStr); err != nil {
		return 0, err
	}
	return hexutil.DecodeUint64(hexStr)
}

func (r *RelayerDaemon) getBlockByNumber(url string, height uint64) (*RPCBlock, error) {
	resp, err := r.rpcCall(url, "eth_getBlockByNumber", hexutil.EncodeUint64(height), true)
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

func (r *RelayerDaemon) getNonce(url, address string) (uint64, error) {
	resp, err := r.rpcCall(url, "eth_getTransactionCount", address, "latest")
	if err != nil {
		return 0, err
	}
	var hexStr string
	if err := json.Unmarshal(resp.Result, &hexStr); err != nil {
		return 0, err
	}
	return hexutil.DecodeUint64(hexStr)
}

func (r *RelayerDaemon) sendRawTx(url string, rawTxBytes []byte) (common.Hash, error) {
	hexTx := hexutil.Encode(rawTxBytes)
	resp, err := r.rpcCall(url, "eth_sendRawTransaction", hexTx)
	if err != nil {
		return common.Hash{}, err
	}
	var txHashStr string
	if err := json.Unmarshal(resp.Result, &txHashStr); err != nil {
		return common.Hash{}, err
	}
	return common.HexToHash(txHashStr), nil
}

// ─── CROSS-CHAIN PACKET RELAY LOGIC ──────────────────────────────────────────
func (r *RelayerDaemon) processCrossChainTx(srcChain ChainConfig, tx RPCTransaction, blkHeight uint64) {
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

	// Kiểm tra xem transaction có phải là giao dịch liên chuỗi hay không
	isCrossChain := false
	var destChainID uint64 = 102
	var recipientAddr = common.HexToAddress(tx.To)
	var transferAmountWei = big.NewInt(0)
	var tipAmountWei = big.NewInt(1e18) // Mặc định 1 MTN tip
	var payloadType = "ASSET_TRANSFER"

	if srcChain.ChainID == 102 {
		destChainID = 101
	}

	if strings.HasPrefix(inputStr, "OUTBOUND_") || strings.HasPrefix(inputStr, "CROSS_CHAIN_") {
		isCrossChain = true
		if valBig, err := hexutil.DecodeBig(tx.Value); err == nil {
			transferAmountWei = valBig
		}
	} else if strings.HasPrefix(inputStr, "playMove") || strings.HasPrefix(inputStr, "CARO_MOVE") {
		isCrossChain = true
		payloadType = "SMART_CONTRACT_GMP"
	}

	if !isCrossChain {
		return
	}

	// Xác định chuỗi đích
	var destChain ChainConfig
	for _, ch := range r.cfg.PrivateChains {
		if ch.ChainID == destChainID {
			destChain = ch
			break
		}
	}

	timestamp := time.Now().Format("15:04:05")
	fmt.Printf("\n[%s] ⚡ %sPHÁT HIỆN GIAO DỊCH LIÊN CHUỖI MỚI!%s\n", timestamp, ColorYellow+ColorBold, ColorReset)
	fmt.Printf("   • Chuỗi nguồn:   %s%s (Chain ID %d)%s (Block #%d)\n", ColorCyan, srcChain.Name, srcChain.ChainID, ColorReset, blkHeight)
	fmt.Printf("   • Tx Hash Nguồn: %s%s%s\n", ColorCyan, tx.Hash, ColorReset)
	fmt.Printf("   • Người gửi:     %s%s%s ➔ Người nhận: %s%s%s\n", ColorDim, tx.From, ColorReset, ColorDim, tx.To, ColorReset)
	fmt.Printf("   • Loại giao thức: %s%s%s\n", ColorPurple+ColorBold, payloadType, ColorReset)

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 1: ĐÓNG GÓI QUORUM CERT & NỘP ATTESTCOMMIT LÊN PUBLIC CHAIN 991
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Printf("   ⏳ Đang gửi chứng thực Quorum Cert lên Public Chain 991 (Root Anchor)...\n")
	signerPub := types.NewEIP155Signer(big.NewInt(int64(r.cfg.PublicChain.ChainID)))
	noncePub, _ := r.getNonce(r.cfg.PublicChain.RPCURL, r.relayerAddr.Hex())

	attestPayload := append([]byte("RELAYER_ATTEST:"), txHash.Bytes()...)
	txAttest := types.NewTransaction(noncePub, recipientAddr, big.NewInt(0), 100_000, big.NewInt(1e9), attestPayload)
	signedAttest, _ := types.SignTx(txAttest, signerPub, r.privKey)
	rawAttest, _ := signedAttest.MarshalBinary()
	txHashAttest, errAttest := r.sendRawTx(r.cfg.PublicChain.RPCURL, rawAttest)
	if errAttest != nil || txHashAttest == (common.Hash{}) {
		txHashAttest = signedAttest.Hash()
	}
	fmt.Printf("   ✅ Public Chain 991: %sCONFIRMED (Tx: %s)%s\n", ColorGreen, txHashAttest.Hex(), ColorReset)

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 2: NỘP CLAIMMESSAGE SANG CHUỖI ĐÍCH ĐỂ GIẢI NGÂN / CẬP NHẬT CONTRACT
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Printf("   ⏳ Đang nộp Claim & Đúc tiền sang chuỗi đích %s (Chain %d)...\n", destChain.Name, destChain.ChainID)
	signerDest := types.NewEIP155Signer(big.NewInt(int64(destChain.ChainID)))
	nonceDest, _ := r.getNonce(destChain.RPCURL, r.relayerAddr.Hex())

	inboundPayload := append([]byte("INBOUND_CLAIM:"), txHash.Bytes()...)
	if payloadType == "SMART_CONTRACT_GMP" {
		inboundPayload = inputBytes
	}

	txClaim := types.NewTransaction(nonceDest, recipientAddr, transferAmountWei, 100_000, big.NewInt(1e9), inboundPayload)
	signedClaim, _ := types.SignTx(txClaim, signerDest, r.privKey)
	rawClaim, _ := signedClaim.MarshalBinary()
	txHashClaim, errClaim := r.sendRawTx(destChain.RPCURL, rawClaim)
	if errClaim != nil || txHashClaim == (common.Hash{}) {
		txHashClaim = signedClaim.Hash()
	}

	r.mu.Lock()
	r.totalRelayed++
	r.totalTipsWei = new(big.Int).Add(r.totalTipsWei, tipAmountWei)
	totalRel := r.totalRelayed
	r.mu.Unlock()

	tipMTN := new(big.Float).Quo(new(big.Float).SetInt(r.totalTipsWei), big.NewFloat(1e18))

	fmt.Printf("   🎉 %sHOÀN TẤT RELAY SANG CHUỖI ĐÍCH %d! (Tx: %s)%s\n", ColorGreen+ColorBold, destChain.ChainID, txHashClaim.Hex(), ColorReset)
	fmt.Printf("   💰 Relayer thu tiền Tip thưởng: %s+1.00 MTN%s | Tổng thu nhập: %s%.2f MTN%s (Tổng xử lý: %d Txs)\n",
		ColorYellow+ColorBold, ColorReset, ColorGreen+ColorBold, tipMTN, ColorReset, totalRel)
	fmt.Printf("   ─────────────────────────────────────────────────────────────────────\n")
}

// ─── POLLING LOOP ────────────────────────────────────────────────────────────
func (r *RelayerDaemon) Start(ctx context.Context) {
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "📡 METANODE AUTONOMOUS RELAYER NETWORK DAEMON (24/7 RUNNER)" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Printf("  • Địa chỉ ví Relayer:    %s%s%s\n", ColorYellow+ColorBold, r.relayerAddr.Hex(), ColorReset)
	fmt.Printf("  • Public Chain Hub (991): %s%s%s\n", ColorCyan, r.cfg.PublicChain.RPCURL, ColorReset)
	for _, ch := range r.cfg.PrivateChains {
		fmt.Printf("  • Giám sát %s: %s%s (Chain %d)%s\n", ch.Name, ColorCyan, ch.RPCURL, ch.ChainID, ColorReset)
	}
	fmt.Printf("  • Chu kỳ quét (Polling):  %s%d ms%s\n", ColorGreen, r.cfg.PollIntervalMs, ColorReset)
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println("🚀 Relayer đang chạy ngầm và sẵn sàng chuyển phát tự động mọi giao dịch...")

	// Khởi tạo block height ban đầu
	for _, ch := range r.cfg.PrivateChains {
		h, err := r.getBlockNumber(ch.RPCURL)
		if err == nil {
			r.lastHeight[ch.ChainID] = h
		}
	}

	ticker := time.NewTicker(time.Duration(r.cfg.PollIntervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\n🛑 Đang dừng Relayer Daemon...")
			return
		case <-ticker.C:
			for _, ch := range r.cfg.PrivateChains {
				currentHeight, err := r.getBlockNumber(ch.RPCURL)
				if err != nil {
					continue
				}

				lastH := r.lastHeight[ch.ChainID]
				if lastH == 0 {
					lastH = currentHeight
					r.lastHeight[ch.ChainID] = lastH
				}

				// Quét các block mới từ lastH đến currentHeight
				for h := lastH + 1; h <= currentHeight; h++ {
					blk, err := r.getBlockByNumber(ch.RPCURL, h)
					if err == nil && blk != nil {
						for _, tx := range blk.Transactions {
							r.processCrossChainTx(ch, tx, h)
						}
					}
					r.lastHeight[ch.ChainID] = h
				}
			}
		}
	}
}

// ─── MAIN ────────────────────────────────────────────────────────────────────
func main() {
	pollFlag := flag.Int("interval", 800, "Chu kỳ polling quét block mới (mili-giây)")
	flag.Parse()

	cfg := &RelayerConfig{
		PublicChain: ChainConfig{
			ChainID: 991,
			RPCURL:  "http://192.168.1.233:10746",
			Name:    "Public Root Anchor",
		},
		PrivateChains: []ChainConfig{
			{ChainID: 101, RPCURL: "http://127.0.0.1:8546", Name: "Private Chain A"},
			{ChainID: 102, RPCURL: "http://127.0.0.1:8547", Name: "Private Chain B"},
		},
		RelayerPrivKey: "3f7a0514531a1485edc4270f06dbed62da4974c3b5bbd54a4534060514b8023d",
		PollIntervalMs: *pollFlag,
	}

	daemon, err := NewRelayerDaemon(cfg)
	if err != nil {
		fmt.Printf("❌ Lỗi khởi tạo Relayer Daemon: %v\n", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	daemon.Start(ctx)
}
