package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
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
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorCyan   = "\033[36m"
	ColorBold   = "\033[1m"
	ColorPurple = "\033[35m"
	ColorRed    = "\033[31m"
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

// Bytecode của contract TestCounter (gồm getCount() và increment())
const CounterBytecodeHex = "6080604052348015600e575f5ffd5b506101818061001c5f395ff3fe608060405234801561000f575f5ffd5b5060043610610034575f3560e01c8063a87d942c14610038578063d09de08a14610056575b5f5ffd5b610040610060565b60405161004d91906100d2565b60405180910390f35b61005e610068565b005b5f5f54905090565b60015f5f8282546100799190610118565b925050819055507f20d8a6f5a693f9d1d627a598e8820f7a55ee74c183aa8f1a30e8d4e8dd9a8d845f546040516100b091906100d2565b60405180910390a1565b5f819050919050565b6100cc816100ba565b82525050565b5f6020820190506100e55f8301846100c3565b92915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f610122826100ba565b915061012d836100ba565b9250828201905080821115610145576101446100eb565b5b9291505056fea2646970667358221220a50f9c396b68c807fe73cad489a601188150c06d7345bf1edc37b01acd4d85e564736f6c63430008220033"

// JSON-RPC Model
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

var httpClient = &http.Client{Timeout: 15 * time.Second}

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
		return nil, fmt.Errorf("RPC Error [%d]: %s", jsonResp.Error.Code, jsonResp.Error.Message)
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

func waitForReceipt(url string, txHash common.Hash, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		res, err := callRPC(url, "eth_getTransactionReceipt", []interface{}{txHash.Hex()})
		if err == nil && len(res) > 0 && string(res) != "null" {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func formatMTN(wei *big.Int) string {
	if wei == nil {
		return "0.0000"
	}
	f := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(1e18))
	return fmt.Sprintf("%.4f MTN", f)
}

// EncodeRelayPayload gắn tiền tố chuẩn MTNRELAY1: cùng DestChainID cuối cùng
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

func main() {
	var rpcA, rpcB, keyA, keyB string
	var chainIDA, chainIDB, destChainID int64
	var transferAmountMTN int64
	var configPath string

	flag.StringVar(&configPath, "config", "../config.json", "Đường dẫn file config.json")
	flag.StringVar(&rpcA, "rpcA", "", "RPC URL Chain A (nguồn)")
	flag.StringVar(&rpcB, "rpcB", "", "RPC URL Chain B (đích)")
	flag.StringVar(&keyA, "keyA", "", "Private key Sender trên Chain A")
	flag.StringVar(&keyB, "keyB", "", "Private key Recipient trên Chain B")
	flag.Int64Var(&chainIDA, "chainIDA", 101, "Chain ID Chain A")
	flag.Int64Var(&chainIDB, "chainIDB", 102, "Chain ID Chain B")
	flag.Int64Var(&destChainID, "destChainID", 102, "Chain ID nhận lệnh")
	flag.Int64Var(&transferAmountMTN, "amount", 100, "Số lượng MTN cần chuyển xuyên chuỗi")
	flag.Parse()

	// Tự động load từ ../config.json nếu các flags chưa được truyền
	if data, err := os.ReadFile(configPath); err == nil {
		var cfg struct {
			PrivateChains struct {
				ChainA struct {
					ChainID     int64    `json:"chain_id"`
					RpcUrl      string   `json:"rpc_url"`
					PrivateKeys []string `json:"private_keys"`
				} `json:"chain_a"`
				ChainB struct {
					ChainID     int64    `json:"chain_id"`
					RpcUrl      string   `json:"rpc_url"`
					PrivateKeys []string `json:"private_keys"`
				} `json:"chain_b"`
			} `json:"private_chains"`
		}
		if err := json.Unmarshal(data, &cfg); err == nil {
			if rpcA == "" && cfg.PrivateChains.ChainA.RpcUrl != "" {
				rpcA = cfg.PrivateChains.ChainA.RpcUrl
			}
			if rpcB == "" && cfg.PrivateChains.ChainB.RpcUrl != "" {
				rpcB = cfg.PrivateChains.ChainB.RpcUrl
			}
			if keyA == "" && len(cfg.PrivateChains.ChainA.PrivateKeys) > 0 {
				keyA = cfg.PrivateChains.ChainA.PrivateKeys[0]
			}
			if keyB == "" && len(cfg.PrivateChains.ChainB.PrivateKeys) > 0 {
				keyB = cfg.PrivateChains.ChainB.PrivateKeys[0]
			}
			if cfg.PrivateChains.ChainA.ChainID > 0 {
				chainIDA = cfg.PrivateChains.ChainA.ChainID
			}
			if cfg.PrivateChains.ChainB.ChainID > 0 {
				chainIDB = cfg.PrivateChains.ChainB.ChainID
			}
		}
	}

	// Fallback mặc định
	if rpcA == "" {
		rpcA = "http://192.168.1.233:8546"
	}
	if rpcB == "" {
		rpcB = "http://192.168.1.233:8550"
	}
	if keyA == "" {
		keyA = "f0c569debd26c9e08924ead34931482ae9267b6cb8e6666bf7fc8023ca6a4106"
	}
	if keyB == "" {
		keyB = "ad1aec8715275f484f8a11a2f82065a031a2e895227773989fc8e3b7fc51051a"
	}

	privKeySender, err := crypto.HexToECDSA(keyA)
	if err != nil {
		log.Fatalf("❌ Parse keyA thất bại: %v", err)
	}
	senderAddr := crypto.PubkeyToAddress(privKeySender.PublicKey)

	privKeyRecipient, err := crypto.HexToECDSA(keyB)
	if err != nil {
		log.Fatalf("❌ Parse keyB thất bại: %v", err)
	}
	recipientAddr := crypto.PubkeyToAddress(privKeyRecipient.PublicKey)

	parsedGatewayABI, err := abi.JSON(strings.NewReader(GatewayABI))
	if err != nil {
		log.Fatalf("❌ Parse Gateway ABI thất bại: %v", err)
	}

	fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println(ColorBold + ColorCyan + "🌐 VÍ DỤ: CROSS-CHAIN TRANSFER (CHUYỂN TIỀN) & SMART CONTRACT CALL" + ColorReset)
	fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Printf("   🔗 Chain A (Nguồn)    : %s (ChainID: %d)\n", rpcA, chainIDA)
	fmt.Printf("   🔗 Chain B (Đích)     : %s (ChainID: %d)\n", rpcB, chainIDB)
	fmt.Printf("   👤 Sender (Chain A)   : %s%s%s\n", ColorCyan, senderAddr.Hex(), ColorReset)
	fmt.Printf("   🎯 Recipient (Chain B): %s%s%s\n", ColorPurple, recipientAddr.Hex(), ColorReset)
	fmt.Printf("   🚪 Gateway Precompile : %s\n", GatewayAddress.Hex())
	fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════\n" + ColorReset)

	// =========================================================================
	// PHẦN 1: CHUYỂN TIỀN XUYÊN CHUỖI (NATIVE ASSET TRANSFER)
	// =========================================================================
	fmt.Println(ColorBold + "🔥 [PHẦN 1] CROSS-CHAIN NATIVE ASSET TRANSFER" + ColorReset)
	balA_Before, _ := getBalance(rpcA, senderAddr.Hex())
	balB_Before, _ := getBalance(rpcB, recipientAddr.Hex())

	fmt.Printf("📊 Số dư trước khi chuyển:\n")
	fmt.Printf("   ├─ Chain A (Sender)   : %s\n", formatMTN(balA_Before))
	fmt.Printf("   └─ Chain B (Recipient): %s\n", formatMTN(balB_Before))

	transferAmount := new(big.Int).Mul(big.NewInt(transferAmountMTN), big.NewInt(1e18))
	tipAmount := new(big.Int).Mul(big.NewInt(1), big.NewInt(1e16)) // 0.01 MTN tip cho Relayer
	gasLimit := uint64(500000)
	gasPrice := big.NewInt(1000000000) // 1 Gwei

	// Định tuyến qua Reserve (Chain 991) theo kiến trúc Hub-and-Spoke Anchor
	reserveChainID := big.NewInt(991)
	relayTransferPayload := EncodeRelayPayload(uint64(chainIDB), nil)

	outboundTransferData, err := parsedGatewayABI.Pack(
		"outbound",
		reserveChainID,       // 1. DestChainID = 991 (Reserve Hub)
		recipientAddr,        // 2. Target = Ví nhận trên Chain B
		relayTransferPayload, // 3. Payload gắn tag MTNRELAY1:<DestChainID>
		big.NewInt(0),        // 4. AssetID = 0 (Native MTN)
		transferAmount,       // 5. Value
		tipAmount,            // 6. Tip
		big.NewInt(0),        // 7. GasFee
		uint8(1),             // 8. HopCount
		false,                // 9. Ordered
	)
	if err != nil {
		log.Fatalf("❌ Pack outbound Transfer data lỗi: %v", err)
	}

	nonceA, err := getNonce(rpcA, senderAddr.Hex())
	if err != nil {
		log.Fatalf("❌ Lấy nonce sender Chain A thất bại: %v", err)
	}

	totalBurn := new(big.Int).Add(transferAmount, tipAmount)
	txTransfer := types.NewTransaction(nonceA, GatewayAddress, totalBurn, gasLimit, gasPrice, outboundTransferData)
	signedTxTransfer, err := types.SignTx(txTransfer, types.NewEIP155Signer(big.NewInt(chainIDA)), privKeySender)
	if err != nil {
		log.Fatalf("❌ Ký tx transfer thất bại: %v", err)
	}
	rawTxTransferBytes, _ := signedTxTransfer.MarshalBinary()

	fmt.Printf("\n🚀 Gửi lệnh Outbound Transfer từ Chain A (Amount: %d MTN, Nonce: %d)...\n", transferAmountMTN, nonceA)
	txHashTransfer, err := sendRawTransaction(rpcA, rawTxTransferBytes)
	if err != nil {
		log.Fatalf("❌ Gửi tx transfer lên Chain A thất bại: %v", err)
	}
	fmt.Printf("   ✅ TxHash (Chain A): %s%s%s\n", ColorCyan, txHashTransfer.Hex(), ColorReset)
	waitForReceipt(rpcA, txHashTransfer, 10*time.Second)

	// =========================================================================
	// PHẦN 2: GỌI SMART CONTRACT XUYÊN CHUỖI (CROSS-CHAIN CONTRACT CALL)
	// =========================================================================
	fmt.Println("\n" + ColorBold + "📜 [PHẦN 2] CROSS-CHAIN SMART CONTRACT CALL" + ColorReset)

	// 2.1 Deploy contract TestCounter mẫu trên Chain B
	counterBytecode, _ := hexutil.Decode("0x" + CounterBytecodeHex)
	nonceB, err := getNonce(rpcB, recipientAddr.Hex())
	if err != nil {
		log.Fatalf("❌ Lấy nonce recipient Chain B thất bại: %v", err)
	}

	deployTx := types.NewContractCreation(nonceB, big.NewInt(0), gasLimit, gasPrice, counterBytecode)
	signedDeployTx, _ := types.SignTx(deployTx, types.NewEIP155Signer(big.NewInt(chainIDB)), privKeyRecipient)
	rawDeployTxBytes, _ := signedDeployTx.MarshalBinary()

	fmt.Printf("   [Setup] Deploying contract TestCounter trên Chain B...\n")
	deployHash, err := sendRawTransaction(rpcB, rawDeployTxBytes)
	if err != nil {
		log.Fatalf("❌ Deploy contract TestCounter trên Chain B lỗi: %v", err)
	}
	waitForReceipt(rpcB, deployHash, 10*time.Second)
	targetContractAddr := crypto.CreateAddress(recipientAddr, nonceB)
	fmt.Printf("   ✅ Contract TestCounter Address: %s%s%s (Chain B)\n", ColorPurple, targetContractAddr.Hex(), ColorReset)

	// 2.2 Client tại Chain A gọi Gateway Outbound sang hàm increment() (0xd09de08a)
	payloadIncrement, _ := hexutil.Decode("0xd09de08a") // selector của function increment()
	gasFeeAmount := big.NewInt(100_000_000_000_000)     // 0.0001 MTN cho remote EVM execution fee
	totalBurnCall := new(big.Int).Add(tipAmount, gasFeeAmount)

	outboundCallData, err := parsedGatewayABI.Pack(
		"outbound",
		big.NewInt(chainIDB), // DestChainID trực tiếp hoặc qua relay
		targetContractAddr,   // Target Smart Contract
		payloadIncrement,     // Payload = increment()
		big.NewInt(0),        // AssetID
		big.NewInt(0),        // Value = 0
		tipAmount,            // Tip
		gasFeeAmount,         // GasFee cho remote node thực thi
		uint8(1),             // HopCount
		false,                // Ordered
	)
	if err != nil {
		log.Fatalf("❌ Pack outbound Contract Call data lỗi: %v", err)
	}

	time.Sleep(1 * time.Second)
	nonceCall, _ := getNonce(rpcA, senderAddr.Hex())
	txCall := types.NewTransaction(nonceCall, GatewayAddress, totalBurnCall, gasLimit, gasPrice, outboundCallData)
	signedTxCall, _ := types.SignTx(txCall, types.NewEIP155Signer(big.NewInt(chainIDA)), privKeySender)
	rawTxCallBytes, _ := signedTxCall.MarshalBinary()

	fmt.Printf("\n🚀 Gửi lệnh Outbound Contract Call từ Chain A -> Chain B (Payload: 0xd09de08a, Nonce: %d)...\n", nonceCall)
	txHashCall, err := sendRawTransaction(rpcA, rawTxCallBytes)
	if err != nil {
		log.Fatalf("❌ Gửi tx contract call lên Chain A thất bại: %v", err)
	}
	fmt.Printf("   ✅ TxHash (Chain A): %s%s%s\n", ColorCyan, txHashCall.Hex(), ColorReset)
	waitForReceipt(rpcA, txHashCall, 10*time.Second)

	// =========================================================================
	// PHẦN 3: THEO DÕI RELAYER & KIỂM TRA ĐÍCH (POLLING)
	// =========================================================================
	fmt.Println("\n" + ColorBold + "⏳ [PHẦN 3] ĐỢI RELAYER XỬ LÝ VÀ ĐỐI SOÁT KẾT QUẢ TẠI CHAIN B" + ColorReset)

	successTransfer := false
	successCall := false

	for i := 1; i <= 60; i++ {
		// 1. Kiểm tra số dư Chain B
		if !successTransfer {
			balB_Current, err := getBalance(rpcB, recipientAddr.Hex())
			if err == nil && balB_Current != nil && balB_Before != nil {
				diff := new(big.Int).Sub(balB_Current, balB_Before)
				// Nhận được gần đủ transferAmount (trừ phí gas deploy contract mẫu)
				threshold := new(big.Int).Mul(big.NewInt(transferAmountMTN-5), big.NewInt(1e18))
				if diff.Cmp(threshold) >= 0 {
					successTransfer = true
					fmt.Printf("\n%s🎉 [BƯỚC 1 OK] TIỀN ĐÃ MINT THÀNH CÔNG TRÊN CHAIN B! (+%s)%s\n", ColorGreen+ColorBold, formatMTN(diff), ColorReset)
				}
			}
		}

		// 2. Kiểm tra biến đếm Counter trong smart contract trên Chain B
		if !successCall {
			// getCount() selector: 0xa87d942c
			getCountData, _ := hexutil.Decode("0xa87d942c")
			result, err := ethCall(rpcB, targetContractAddr, getCountData)
			if err == nil && result != "" && result != "0x" {
				val := new(big.Int).SetBytes(common.FromHex(result))
				if val != nil && val.Cmp(big.NewInt(1)) >= 0 {
					successCall = true
					fmt.Printf("\n%s🎉 [BƯỚC 2 OK] SMART CONTRACT CHAIN B ĐÃ NHẬN CALL & THỰC THI THÀNH CÔNG! (Counter = %s)%s\n", ColorGreen+ColorBold, val.String(), ColorReset)
				}
			}
		}

		if successTransfer && successCall {
			fmt.Printf("\n%s🏆 TẤT CẢ GIAO DỊCH CROSS-CHAIN (TRANSFER & CONTRACT CALL) ĐỀU THÀNH CÔNG!%s\n", ColorGreen+ColorBold, ColorReset)
			return
		}

		fmt.Printf(".")
		time.Sleep(1 * time.Second)
	}

	fmt.Printf("\n\n%s⚠️ Hết 60s chờ: Vui lòng kiểm tra daemon Relayer (`run_relayer_tmux.sh` hoặc relayer_daemon) đã hoạt động trên các chains hay chưa.%s\n", ColorYellow, ColorReset)
}
