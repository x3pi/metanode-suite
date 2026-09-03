package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorCyan   = "\033[36m"
	ColorBold   = "\033[1m"
	ColorPurple = "\033[35m"
)

var GatewayAddress = common.HexToAddress("0x0000000000000000000000000000000000001002")

const GatewayABI = `[
	{
		"inputs": [
			{"internalType": "uint256", "name": "destChainId", "type": "uint256"},
			{"internalType": "address", "name": "target", "type": "address"},
			{"internalType": "bytes", "name": "payload", "type": "bytes"},
			{"internalType": "uint256", "name": "assetId", "type": "uint256"},
			{"internalType": "uint256", "name": "value", "type": "uint256"},
			{"internalType": "uint256", "name": "tip", "type": "uint256"},
			{"internalType": "uint256", "name": "gasFee", "type": "uint256"},
			{"internalType": "uint8", "name": "hopCount", "type": "uint8"},
			{"internalType": "bool", "name": "ordered", "type": "bool"}
		],
		"name": "outbound",
		"outputs": [{"internalType": "bytes32", "name": "messageId", "type": "bytes32"}],
		"stateMutability": "nonpayable",
		"type": "function"
	}
]`

// ─── RPC Helpers ─────────────────────────────────────────────────────────────
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

var httpClient = &http.Client{Timeout: 10 * time.Second}

func callRPC(url, method string, params []interface{}) (json.RawMessage, error) {
	reqBody, _ := json.Marshal(JSONRPCRequest{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var jsonResp JSONRPCResponse
	if err := json.Unmarshal(body, &jsonResp); err != nil {
		return nil, err
	}
	if jsonResp.Error != nil {
		return nil, fmt.Errorf("rpc error: %s", jsonResp.Error.Message)
	}
	return jsonResp.Result, nil
}

func getBalance(url, address string) (*big.Int, error) {
	res, err := callRPC(url, "eth_getBalance", []interface{}{address, "latest"})
	if err != nil {
		return big.NewInt(0), err
	}
	var hexStr string
	if err := json.Unmarshal(res, &hexStr); err != nil {
		return big.NewInt(0), err
	}
	val, err := hexutil.DecodeBig(hexStr)
	if err != nil || val == nil {
		return big.NewInt(0), err
	}
	return val, nil
}

func getNonce(url, address string) (uint64, error) {
	res, err := callRPC(url, "eth_getTransactionCount", []interface{}{address, "pending"})
	if err != nil {
		return 0, err
	}
	var hexStr string
	if err := json.Unmarshal(res, &hexStr); err != nil {
		return 0, err
	}
	return hexutil.DecodeUint64(hexStr)
}

func sendRawTransaction(url string, rawTx []byte) (common.Hash, error) {
	hexTx := hexutil.Encode(rawTx)
	res, err := callRPC(url, "eth_sendRawTransaction", []interface{}{hexTx})
	if err != nil {
		return common.Hash{}, err
	}
	var txHashStr string
	if err := json.Unmarshal(res, &txHashStr); err != nil {
		return common.Hash{}, err
	}
	return common.HexToHash(txHashStr), nil
}

func ethCall(url string, to common.Address, data []byte) (string, error) {
	callObj := map[string]string{
		"to":   to.Hex(),
		"data": hexutil.Encode(data),
	}
	res, err := callRPC(url, "eth_call", []interface{}{callObj, "latest"})
	if err != nil {
		return "", err
	}
	var out string
	json.Unmarshal(res, &out)
	return out, nil
}

func formatMTN(wei *big.Int) string {
	if wei == nil {
		return "0.0000"
	}
	f := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(1e18))
	return fmt.Sprintf("%.4f", f)
}

// EncodeRelayPayload gắn tiền tố MTNRELAY1: cùng DestChainID cuối cùng
func EncodeRelayPayload(finalDestChainID uint64, innerPayload []byte) []byte {
	prefix := []byte("MTNRELAY1:")
	buf := make([]byte, len(prefix)+8+len(innerPayload))
	copy(buf, prefix)
	buf[len(prefix)] = byte(finalDestChainID >> 56)
	buf[len(prefix)+1] = byte(finalDestChainID >> 48)
	buf[len(prefix)+2] = byte(finalDestChainID >> 40)
	buf[len(prefix)+3] = byte(finalDestChainID >> 32)
	buf[len(prefix)+4] = byte(finalDestChainID >> 24)
	buf[len(prefix)+5] = byte(finalDestChainID >> 16)
	buf[len(prefix)+6] = byte(finalDestChainID >> 8)
	buf[len(prefix)+7] = byte(finalDestChainID)
	copy(buf[len(prefix)+8:], innerPayload)
	return buf
}

func waitForReceipt(url string, txHash common.Hash, timeout time.Duration) {
	start := time.Now()
	for time.Since(start) < timeout {
		res, err := callRPC(url, "eth_getTransactionReceipt", []interface{}{txHash.Hex()})
		if err == nil && len(res) > 0 && string(res) != "null" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// ─── Config Types ────────────────────────────────────────────────────────────
type ChainEntry struct {
	ChainID     uint64
	RpcUrl      string
	PrivateKeys []string
}

type PrivateChainJson struct {
	ChainID     uint64   `json:"chain_id"`
	RpcUrl      string   `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
}

type ConfigStructure struct {
	PrivateChains map[string]PrivateChainJson `json:"private_chains"`
	Nodes         map[string]string           `json:"nodes"`
	PrivateKeys   []string                    `json:"private_keys"`
}

func sanitizeKey(k string) string {
	k = strings.TrimSpace(k)
	return strings.TrimPrefix(k, "0x")
}

func loadAvailableChains(configFilePath string) (map[string]ChainEntry, error) {
	paths := []string{
		configFilePath,
		"../config.json",
		"../../config.json",
		"/tmp/private_chains.json",
	}

	for _, p := range paths {
		if p == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil || len(data) == 0 {
			continue
		}

		var cfg ConfigStructure
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}

		chains := make(map[string]ChainEntry)

		for name, c := range cfg.PrivateChains {
			var keys []string
			for _, k := range c.PrivateKeys {
				if sanitized := sanitizeKey(k); sanitized != "" {
					keys = append(keys, sanitized)
				}
			}
			entry := ChainEntry{
				ChainID:     c.ChainID,
				RpcUrl:      c.RpcUrl,
				PrivateKeys: keys,
			}
			chains[fmt.Sprintf("%d", c.ChainID)] = entry
			chains[strings.ToLower(name)] = entry
		}

		for cidStr, rpc := range cfg.Nodes {
			if _, exists := chains[cidStr]; !exists {
				var cid uint64
				fmt.Sscanf(cidStr, "%d", &cid)
				entry := ChainEntry{
					ChainID:     cid,
					RpcUrl:      rpc,
					PrivateKeys: []string{},
				}
				chains[cidStr] = entry
				chains[fmt.Sprintf("chain_%s", cidStr)] = entry
			}
		}

		if len(chains) > 0 {
			return chains, nil
		}
	}
	return nil, fmt.Errorf("không tìm thấy file cấu hình hợp lệ (đã thử: %v)", paths)
}

// ─── MAIN ────────────────────────────────────────────────────────────────────
func main() {
	var targetFrom, targetTo, configPath string
	var rpcA, rpcB, keyA, keyB string
	var amountInput float64

	flag.StringVar(&targetFrom, "from", "101", "ID Chain nguồn (ví dụ: -from 101 hoặc --from 101)")
	flag.StringVar(&targetFrom, "src", "101", "Alias của -from")
	flag.StringVar(&targetFrom, "source", "101", "Alias của -from")
	flag.StringVar(&targetFrom, "chainA", "101", "Alias của -from")

	flag.StringVar(&targetTo, "to", "102", "ID Chain đích (ví dụ: -to 103 hoặc --to 103)")
	flag.StringVar(&targetTo, "dst", "102", "Alias của -to")
	flag.StringVar(&targetTo, "dest", "102", "Alias của -to")
	flag.StringVar(&targetTo, "chainB", "102", "Alias của -to")

	flag.Float64Var(&amountInput, "amount", 500.0, "Số lượng MTN muốn chuyển (ví dụ: -amount 100 hoặc --amount 100)")
	flag.Float64Var(&amountInput, "amt", 500.0, "Alias của -amount")
	flag.Float64Var(&amountInput, "value", 500.0, "Alias của -amount")

	flag.StringVar(&configPath, "config", "../config.json", "Đường dẫn file config.json")
	flag.StringVar(&rpcA, "rpcA", "", "Override RPC Chain nguồn")
	flag.StringVar(&rpcB, "rpcB", "", "Override RPC Chain đích")
	flag.StringVar(&keyA, "keyA", "", "Override Private key Sender")
	flag.StringVar(&keyB, "keyB", "", "Override Private key Recipient")

	flag.Usage = func() {
		fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
		fmt.Println(ColorBold + "📖 HƯỚNG DẪN SỬ DỤNG KỊCH BẢN CROSS-CHAIN PURE CLIENT" + ColorReset)
		fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
		fmt.Println("Cú pháp dùng Flag:")
		fmt.Println("  go run . -from <ChainID_Nguồn> -to <ChainID_Đích> -amount <Số_MTN>")
		fmt.Println("  go run . --from 101 --to 103 --amount 100")
		fmt.Println("  go run . -src 101 -dest 102 -amt 250")
		fmt.Println("\nCú pháp truyền nhanh (Positional Args):")
		fmt.Println("  go run . [ChainID_Nguồn] [ChainID_Đích] [Số_MTN]")
		fmt.Println("  go run . 101 103 100")
		fmt.Println("  go run . 102 101 50")
		fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
	}

	flag.Parse()

	// Nếu người dùng truyền theo dạng positional args: `go run . 101 103 100`
	posArgs := flag.Args()
	if len(posArgs) >= 1 {
		targetFrom = posArgs[0]
	}
	if len(posArgs) >= 2 {
		targetTo = posArgs[1]
	}
	if len(posArgs) >= 3 {
		if amt, err := strconv.ParseFloat(posArgs[2], 64); err == nil && amt > 0 {
			amountInput = amt
		}
	}

	// Default fallback keys
	defaultKeyA := "f0c569debd26c9e08924ead34931482ae9267b6cb8e6666bf7fc8023ca6a4106"
	defaultKeyB := "ad1aec8715275f484f8a11a2f82065a031a2e895227773989fc8e3b7fc51051a"

	availableChains, errCfg := loadAvailableChains(configPath)
	if errCfg != nil {
		fmt.Printf("⚠️ Cảnh báo đọc config: %v\n", errCfg)
	}

	fromEntry, okFrom := availableChains[strings.ToLower(targetFrom)]
	if !okFrom {
		var cid uint64 = 101
		fmt.Sscanf(targetFrom, "%d", &cid)
		fromEntry = ChainEntry{ChainID: cid, RpcUrl: "http://127.0.0.1:8546"}
	}

	toEntry, okTo := availableChains[strings.ToLower(targetTo)]
	if !okTo {
		var cid uint64 = 102
		fmt.Sscanf(targetTo, "%d", &cid)
		toEntry = ChainEntry{ChainID: cid, RpcUrl: "http://127.0.0.1:8547"}
	}

	// Áp dụng override nếu có
	if rpcA != "" {
		fromEntry.RpcUrl = rpcA
	}
	if rpcB != "" {
		toEntry.RpcUrl = rpcB
	}

	selectedKeyA := defaultKeyA
	if keyA != "" {
		selectedKeyA = sanitizeKey(keyA)
	} else if len(fromEntry.PrivateKeys) > 0 {
		selectedKeyA = fromEntry.PrivateKeys[0]
	}

	selectedKeyB := defaultKeyB
	if keyB != "" {
		selectedKeyB = sanitizeKey(keyB)
	} else if len(toEntry.PrivateKeys) > 0 {
		selectedKeyB = toEntry.PrivateKeys[0]
	}

	privKeySender, errKeyA := crypto.HexToECDSA(selectedKeyA)
	if errKeyA != nil {
		fmt.Printf("❌ Private Key Sender không hợp lệ: %v\n", errKeyA)
		return
	}
	senderAddr := crypto.PubkeyToAddress(privKeySender.PublicKey)

	privKeyRecipient, errKeyB := crypto.HexToECDSA(selectedKeyB)
	if errKeyB != nil {
		fmt.Printf("❌ Private Key Recipient không hợp lệ: %v\n", errKeyB)
		return
	}
	recipientAddr := crypto.PubkeyToAddress(privKeyRecipient.PublicKey)

	fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Printf(ColorBold+ColorCyan+"👩‍💻 KỊCH BẢN CLIENT PURE: TEST CHUYỂN TIỀN & SMART CONTRACT (CHAIN %d ➔ CHAIN %d)\n"+ColorReset, fromEntry.ChainID, toEntry.ChainID)
	fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Printf("🌐 CẤU HÌNH TUYẾN CHUỖI (FLAGS: -from %d -to %d -amount %.1f):\n", fromEntry.ChainID, toEntry.ChainID, amountInput)
	fmt.Printf("   ├─ %sChain Nguồn (--from %d):%s %s (Ví Sender: %s)\n", ColorBold+ColorYellow, fromEntry.ChainID, ColorReset, fromEntry.RpcUrl, senderAddr.Hex())
	fmt.Printf("   ├─ %sChain Đích  (--to %d):  %s %s (Ví Recipient: %s)\n", ColorBold+ColorYellow, toEntry.ChainID, ColorReset, toEntry.RpcUrl, recipientAddr.Hex())
	fmt.Printf("   └─ %sSố tiền     (--amount):   %s %.1f MTN\n", ColorBold+ColorYellow, ColorReset, amountInput)

	parsedGatewayABI, _ := abi.JSON(strings.NewReader(GatewayABI))

	// =========================================================================
	// PHẦN 1: CHUYỂN TIỀN (ASSET TRANSFER)
	// =========================================================================
	fmt.Printf("\n" + ColorBold + "🔥 PHẦN 1: CROSS-CHAIN TRANSFER (CHUYỂN %.1f MTN TỪ CHAIN %d SANG CHAIN %d)" + ColorReset + "\n", amountInput, fromEntry.ChainID, toEntry.ChainID)
	balA_Before, _ := getBalance(fromEntry.RpcUrl, senderAddr.Hex())
	balB_Before, _ := getBalance(toEntry.RpcUrl, recipientAddr.Hex())

	fmt.Printf("📊 SỐ DƯ BAN ĐẦU:\n")
	fmt.Printf("   ├─ Chain %d (Sender):    %s MTN\n", fromEntry.ChainID, formatMTN(balA_Before))
	fmt.Printf("   └─ Chain %d (Recipient): %s MTN\n", toEntry.ChainID, formatMTN(balB_Before))

	transferAmount := new(big.Int)
	new(big.Float).Mul(big.NewFloat(amountInput), big.NewFloat(1e18)).Int(transferAmount)
	tipAmount := new(big.Int).Mul(big.NewInt(100+time.Now().Unix()%100), big.NewInt(1e16)) // ~1 - 2 MTN Tip
	gasLimit := uint64(500000)
	gasPrice := big.NewInt(1000000000)

	outboundTransferData, errPack := parsedGatewayABI.Pack("outbound",
		new(big.Int).SetUint64(toEntry.ChainID),
		recipientAddr,
		[]byte{},
		big.NewInt(0),
		transferAmount,
		tipAmount,
		big.NewInt(0),
		uint8(1),
		false,
	)
	if errPack != nil {
		fmt.Printf("❌ Lỗi pack outbound: %v\n", errPack)
		return
	}

	nonceA, errNonce := getNonce(fromEntry.RpcUrl, senderAddr.Hex())
	if errNonce != nil {
		fmt.Printf("❌ Lỗi lấy nonce Chain %d: %v\n", fromEntry.ChainID, errNonce)
		return
	}
	totalBurn := new(big.Int).Add(transferAmount, tipAmount)

	txTransfer := types.NewTransaction(nonceA, GatewayAddress, totalBurn, gasLimit, gasPrice, outboundTransferData)
	signedTxTransfer, _ := types.SignTx(txTransfer, types.NewEIP155Signer(new(big.Int).SetUint64(fromEntry.ChainID)), privKeySender)
	rawTxTransferBytes, _ := signedTxTransfer.MarshalBinary()

	fmt.Printf("\n🚀 CLIENT NỘP LỆNH CHUYỂN TIỀN LÊN CHAIN %d (Nonce=%d)...\n", fromEntry.ChainID, nonceA)
	txHashTransfer, errSend := sendRawTransaction(fromEntry.RpcUrl, rawTxTransferBytes)
	if errSend != nil {
		fmt.Printf("   ❌ SendRawTransaction Transfer error: %v\n", errSend)
		return
	}
	fmt.Printf("   ✅ TxHash: %s\n", txHashTransfer.Hex())
	waitForReceipt(fromEntry.RpcUrl, txHashTransfer, 10*time.Second)

	// =========================================================================
	// PHẦN 2: CHUYỂN LỆNH GỌI SMART CONTRACT XUYÊN CHUỖI (CONTRACT CALL)
	// =========================================================================
	fmt.Printf("\n" + ColorBold + "📜 PHẦN 2: CROSS-CHAIN SMART CONTRACT CALL (GỌI HÀM INCREMENT TRÊN CHAIN %d)" + ColorReset + "\n", toEntry.ChainID)

	// 2.1 Deploy TestCounter Contract trên Chain B (do Recipient thực hiện để test)
	counterBytecodeHex := "608060405234801561000f575f80fd5b506101818061001d5f395ff3fe608060405234801561000f575f80fd5b5060043610610034575f3560e01c8063a87d942c14610038578063d09de08a14610056575b5f80fd5b610040610060565b60405161004d91906100d2565b60405180910390f35b61005e610068565b005b5f8054905090565b60015f808282546100799190610118565b925050819055507f20d8a6f5a693f9d1d627a598e8820f7a55ee74c183aa8f1a30e8d4e8dd9a8d845f546040516100b091906100d2565b60405180910390a1565b5f819050919050565b6100cc816100ba565b82525050565b5f6020820190506100e55f8301846100c3565b92915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f610122826100ba565b915061012d836100ba565b9250828201905080821115610145576101446100eb565b5b9291505056fea2646970667358221220124c20a0a92375b56d64655ddf70bcd5eccdd0fea4724fc3b1130c754d3eedd964736f6c63430008140033"
	counterBytecode, _ := hexutil.Decode("0x" + counterBytecodeHex)
	nonceB, _ := getNonce(toEntry.RpcUrl, recipientAddr.Hex())

	deployTx := types.NewContractCreation(nonceB, big.NewInt(0), gasLimit, gasPrice, counterBytecode)
	signedDeployTx, _ := types.SignTx(deployTx, types.NewEIP155Signer(new(big.Int).SetUint64(toEntry.ChainID)), privKeyRecipient)
	rawDeployTxBytes, _ := signedDeployTx.MarshalBinary()

	fmt.Printf("   [Setup] Đang deploy Contract TestCounter trên Chain %d...\n", toEntry.ChainID)
	deployHash, _ := sendRawTransaction(toEntry.RpcUrl, rawDeployTxBytes)
	waitForReceipt(toEntry.RpcUrl, deployHash, 10*time.Second)
	targetContractAddr := crypto.CreateAddress(recipientAddr, nonceB)
	fmt.Printf("   ✅ Contract TestCounter Address (Chain %d): %s%s%s\n", toEntry.ChainID, ColorPurple, targetContractAddr.Hex(), ColorReset)

	// 2.2 Client gửi lệnh outbound từ Chain A truyền payload "increment()" sang Contract ở Chain B
	payloadIncrement, _ := hexutil.Decode("0xd09de08a")
	gasFeeAmount := big.NewInt(100_000_000_000_000) // 0.0001 MTN for remote EVM execution
	totalBurnCall := new(big.Int).Add(tipAmount, gasFeeAmount)

	outboundCallData, _ := parsedGatewayABI.Pack("outbound",
		new(big.Int).SetUint64(toEntry.ChainID),
		targetContractAddr,
		payloadIncrement,
		big.NewInt(0),
		big.NewInt(0),
		tipAmount,
		gasFeeAmount,
		uint8(1),
		false,
	)
	time.Sleep(1 * time.Second)
	nonceCall, _ := getNonce(fromEntry.RpcUrl, senderAddr.Hex())

	txCall := types.NewTransaction(nonceCall, GatewayAddress, totalBurnCall, gasLimit, gasPrice, outboundCallData)
	signedTxCall, _ := types.SignTx(txCall, types.NewEIP155Signer(new(big.Int).SetUint64(fromEntry.ChainID)), privKeySender)
	rawTxCallBytes, _ := signedTxCall.MarshalBinary()

	fmt.Printf("\n🚀 CLIENT NỘP LỆNH CONTRACT CALL LÊN CHAIN %d (Nonce=%d)...\n", fromEntry.ChainID, nonceCall)
	txHashCall, errSendCall := sendRawTransaction(fromEntry.RpcUrl, rawTxCallBytes)
	if errSendCall != nil {
		fmt.Printf("   ❌ SendRawTransaction Call error: %v\n", errSendCall)
		return
	}
	fmt.Printf("   ✅ TxHash: %s (Gửi Payload: 0xd09de08a)\n", txHashCall.Hex())
	waitForReceipt(fromEntry.RpcUrl, txHashCall, 10*time.Second)

	// =========================================================================
	// PHẦN 3: ĐỢI RELAYER XỬ LÝ (POLLING)
	// =========================================================================
	fmt.Printf("\n⏳ CLIENT ĐỨNG ĐỢI HỆ THỐNG RELAYER XỬ LÝ CẢ 2 LỆNH TRÊN CHAIN %d...\n", toEntry.ChainID)

	successTransfer := false
	successCall := false

	expectedThreshold := new(big.Int)
	new(big.Float).Mul(big.NewFloat(amountInput*0.95), big.NewFloat(1e18)).Int(expectedThreshold)

	for i := 0; i < 60; i++ {
		// 1. Kiểm tra tiền đã cập bến chưa
		if !successTransfer {
			balB_Current, err := getBalance(toEntry.RpcUrl, recipientAddr.Hex())
			if err == nil && balB_Current != nil && balB_Before != nil {
				diff := new(big.Int).Sub(balB_Current, balB_Before)
				if diff.Cmp(expectedThreshold) >= 0 {
					successTransfer = true
					fmt.Printf("\n%s🎉 BINGOOOO! TIỀN ĐÃ MINT TRÊN CHAIN %d THÀNH CÔNG! (+%s MTN)%s\n", ColorGreen, toEntry.ChainID, formatMTN(diff), ColorReset)
				}
			}
		}

		// 2. Kiểm tra Contract TestCounter bên Chain B đã được kích hoạt hàm increment() (count == 1) chưa
		if !successCall {
			getCounterData, _ := hexutil.Decode("0xa87d942c")
			result, err := ethCall(toEntry.RpcUrl, targetContractAddr, getCounterData)
			if err == nil && result != "" && result != "0x" {
				val := new(big.Int).SetBytes(common.FromHex(result))
				if val != nil && val.Cmp(big.NewInt(1)) == 0 {
					successCall = true
					fmt.Printf("\n%s🎉 BINGOOOO! SMART CONTRACT CHAIN %d ĐÃ NHẬN LỆNH TỪ CHAIN %d VÀ THỰC THI THÀNH CÔNG! (Counter = 1)%s\n", ColorGreen, toEntry.ChainID, fromEntry.ChainID, ColorReset)
				}
			}
		}

		if successTransfer && successCall {
			fmt.Printf("\n✅ HOÀN TẤT KỊCH BẢN CLIENT (Chain %d ➔ Chain %d)!\n", fromEntry.ChainID, toEntry.ChainID)
			return
		}

		fmt.Printf(".")
		os.Stdout.Sync()
		time.Sleep(1 * time.Second)
	}

	fmt.Printf("\n\n❌ Quá thời gian chờ (60s)! Hệ thống ngầm (Relayer) có vẻ chưa chạy hoặc chưa xử lý kịp.\n")
}
