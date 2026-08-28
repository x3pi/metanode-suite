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

// ─── MAIN ────────────────────────────────────────────────────────────────────
func main() {
	var rpcA, rpcB, keyA, keyB string
	flag.StringVar(&rpcA, "rpcA", "http://127.0.0.1:8546", "RPC Chain A")
	flag.StringVar(&rpcB, "rpcB", "http://127.0.0.1:8547", "RPC Chain B")
	flag.StringVar(&keyA, "keyA", "f0c569debd26c9e08924ead34931482ae9267b6cb8e6666bf7fc8023ca6a4106", "Private key Sender")
	flag.StringVar(&keyB, "keyB", "ad1aec8715275f484f8a11a2f82065a031a2e895227773989fc8e3b7fc51051a", "Private key Recipient (dùng để deploy contract trên Chain B)")
	flag.Parse()

	// Tự động tải endpoint RPC từ config.json của test-blockstm hoặc /tmp/private_chains.json
	for _, cfgPath := range []string{"../config.json", "../../config.json", "/tmp/private_chains.json"} {
		if data, err := os.ReadFile(cfgPath); err == nil {
			var bCfg struct {
				PrivateChains struct {
					ChainA struct {
						RpcUrl string `json:"rpc_url"`
					} `json:"chain_a"`
					ChainB struct {
						RpcUrl string `json:"rpc_url"`
					} `json:"chain_b"`
				} `json:"private_chains"`
				Nodes map[string]string `json:"nodes"`
			}
			if err := json.Unmarshal(data, &bCfg); err == nil {
				if rpcA == "http://127.0.0.1:8546" {
					if bCfg.PrivateChains.ChainA.RpcUrl != "" {
						rpcA = bCfg.PrivateChains.ChainA.RpcUrl
					} else if bCfg.Nodes["101"] != "" {
						rpcA = bCfg.Nodes["101"]
					}
				}
				if rpcB == "http://127.0.0.1:8547" {
					if bCfg.PrivateChains.ChainB.RpcUrl != "" {
						rpcB = bCfg.PrivateChains.ChainB.RpcUrl
					} else if bCfg.Nodes["102"] != "" {
						rpcB = bCfg.Nodes["102"]
					}
				}
			}
		}
	}

	fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println(ColorBold + ColorCyan + "👩‍💻 KỊCH BẢN CLIENT PURE: TEST CHUYỂN TIỀN & GỌI SMART CONTRACT XUYÊN CHUỖI" + ColorReset)
	fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)

	privKeySender, _ := crypto.HexToECDSA(keyA)
	senderAddr := crypto.PubkeyToAddress(privKeySender.PublicKey)
	privKeyRecipient, _ := crypto.HexToECDSA(keyB)
	recipientAddr := crypto.PubkeyToAddress(privKeyRecipient.PublicKey)
	parsedGatewayABI, _ := abi.JSON(strings.NewReader(GatewayABI))

	// =========================================================================
	// PHẦN 1: CHUYỂN TIỀN (ASSET TRANSFER)
	// =========================================================================
	fmt.Println("\n" + ColorBold + "🔥 PHẦN 1: CROSS-CHAIN TRANSFER (CHUYỂN 500 MTN)" + ColorReset)
	balA_Before, _ := getBalance(rpcA, senderAddr.Hex())
	balB_Before, _ := getBalance(rpcB, recipientAddr.Hex())

	fmt.Printf("📊 SỐ DƯ BAN ĐẦU:\n")
	fmt.Printf("   ├─ Chain A (Sender):    %s MTN\n", formatMTN(balA_Before))
	fmt.Printf("   └─ Chain B (Recipient): %s MTN\n", formatMTN(balB_Before))

	transferAmount := new(big.Int).Mul(big.NewInt(500), big.NewInt(1e18))
	tipAmount := new(big.Int).Mul(big.NewInt(100+time.Now().Unix()%100), big.NewInt(1e16)) // ~1 - 2 MTN Tip
	gasLimit := uint64(500000)
	gasPrice := big.NewInt(1000000000)

	// Luân chuyển tiền 2 chặng qua Reserve (Chain 991) theo luật an ninh Invariant C8
	reserveChainID := big.NewInt(991)
	relayTransferPayload := EncodeRelayPayload(102, nil)
	outboundTransferData, _ := parsedGatewayABI.Pack(
		"outbound",
		reserveChainID,       // DestChainID = 991 (Reserve Anchor)
		recipientAddr,        // Target là địa chỉ ví nhận trên Chain 102
		relayTransferPayload, // Gắn tag chuyển tiếp sang Chain 102
		big.NewInt(0),        // AssetID (0 = Native MTN)
		transferAmount,       // 500 MTN
		tipAmount,
		big.NewInt(0),
		uint8(1),
		false,
	)
	nonceA, errNonce := getNonce(rpcA, senderAddr.Hex())
	if errNonce != nil {
		fmt.Printf("❌ Lỗi lấy nonce: %v\n", errNonce)
		return
	}
	totalBurn := new(big.Int).Add(transferAmount, tipAmount)

	txTransfer := types.NewTransaction(nonceA, GatewayAddress, totalBurn, gasLimit, gasPrice, outboundTransferData)
	signedTxTransfer, _ := types.SignTx(txTransfer, types.NewEIP155Signer(big.NewInt(101)), privKeySender)
	rawTxTransferBytes, _ := signedTxTransfer.MarshalBinary()

	fmt.Printf("\n🚀 CLIENT NỘP LỆNH CHUYỂN TIỀN LÊN CHAIN A (Nonce=%d)...\n", nonceA)
	txHashTransfer, errSend := sendRawTransaction(rpcA, rawTxTransferBytes)
	if errSend != nil {
		fmt.Printf("   ❌ SendRawTransaction Transfer error: %v\n", errSend)
		return
	}
	fmt.Printf("   ✅ TxHash: %s\n", txHashTransfer.Hex())
	waitForReceipt(rpcA, txHashTransfer, 10*time.Second)

	// =========================================================================
	// PHẦN 2: CHUYỂN LỆNH GỌI SMART CONTRACT XUYÊN CHUỖI (CONTRACT CALL)
	// =========================================================================
	fmt.Println("\n" + ColorBold + "📜 PHẦN 2: CROSS-CHAIN SMART CONTRACT CALL (GỌI HÀM INCREMENT)" + ColorReset)

	// 2.1 Deploy TestCounter Contract trên Chain B (do Recipient thực hiện để test)
	counterBytecodeHex := "608060405234801561000f575f80fd5b506101818061001d5f395ff3fe608060405234801561000f575f80fd5b5060043610610034575f3560e01c8063a87d942c14610038578063d09de08a14610056575b5f80fd5b610040610060565b60405161004d91906100d2565b60405180910390f35b61005e610068565b005b5f8054905090565b60015f808282546100799190610118565b925050819055507f20d8a6f5a693f9d1d627a598e8820f7a55ee74c183aa8f1a30e8d4e8dd9a8d845f546040516100b091906100d2565b60405180910390a1565b5f819050919050565b6100cc816100ba565b82525050565b5f6020820190506100e55f8301846100c3565b92915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f610122826100ba565b915061012d836100ba565b9250828201905080821115610145576101446100eb565b5b9291505056fea2646970667358221220124c20a0a92375b56d64655ddf70bcd5eccdd0fea4724fc3b1130c754d3eedd964736f6c63430008140033"
	counterBytecode, _ := hexutil.Decode("0x" + counterBytecodeHex)
	nonceB, _ := getNonce(rpcB, recipientAddr.Hex())

	deployTx := types.NewContractCreation(nonceB, big.NewInt(0), gasLimit, gasPrice, counterBytecode)
	signedDeployTx, _ := types.SignTx(deployTx, types.NewEIP155Signer(big.NewInt(102)), privKeyRecipient)
	rawDeployTxBytes, _ := signedDeployTx.MarshalBinary()

	fmt.Printf("   [Setup] Đang deploy Contract TestCounter trên Chain B...\n")
	deployHash, _ := sendRawTransaction(rpcB, rawDeployTxBytes)
	waitForReceipt(rpcB, deployHash, 10*time.Second)
	targetContractAddr := crypto.CreateAddress(recipientAddr, nonceB)
	fmt.Printf("   ✅ Contract TestCounter Address (Chain B): %s%s%s\n", ColorPurple, targetContractAddr.Hex(), ColorReset)

	// 2.2 Client gửi lệnh outbound từ Chain A truyền payload "increment()" sang Contract ở Chain B
	payloadIncrement, _ := hexutil.Decode("0xd09de08a")
	gasFeeAmount := big.NewInt(100_000_000_000_000) // 0.0001 MTN for remote EVM execution
	totalBurnCall := new(big.Int).Add(tipAmount, gasFeeAmount)

	outboundCallData, _ := parsedGatewayABI.Pack("outbound", big.NewInt(102), targetContractAddr, payloadIncrement, big.NewInt(0), big.NewInt(0), tipAmount, gasFeeAmount, uint8(1), false)
	time.Sleep(1 * time.Second)
	nonceCall, _ := getNonce(rpcA, senderAddr.Hex())

	txCall := types.NewTransaction(nonceCall, GatewayAddress, totalBurnCall, gasLimit, gasPrice, outboundCallData)
	signedTxCall, _ := types.SignTx(txCall, types.NewEIP155Signer(big.NewInt(101)), privKeySender)
	rawTxCallBytes, _ := signedTxCall.MarshalBinary()

	fmt.Printf("\n🚀 CLIENT NỘP LỆNH CONTRACT CALL LÊN CHAIN A (Nonce=%d)...\n", nonceCall)
	txHashCall, errSendCall := sendRawTransaction(rpcA, rawTxCallBytes)
	if errSendCall != nil {
		fmt.Printf("   ❌ SendRawTransaction Call error: %v\n", errSendCall)
		return
	}
	fmt.Printf("   ✅ TxHash: %s (Gửi Payload: 0xd09de08a)\n", txHashCall.Hex())
	waitForReceipt(rpcA, txHashCall, 10*time.Second)

	// =========================================================================
	// PHẦN 3: ĐỢI RELAYER XỬ LÝ (POLLING)
	// =========================================================================
	fmt.Printf("\n⏳ CLIENT ĐỨNG ĐỢI HỆ THỐNG RELAYER XỬ LÝ CẢ 2 LỆNH TRÊN...\n")

	successTransfer := false
	successCall := false

	for i := 0; i < 60; i++ {
		// 1. Kiểm tra tiền đã cập bến chưa
		if !successTransfer {
			balB_Current, err := getBalance(rpcB, recipientAddr.Hex())
			if err == nil && balB_Current != nil && balB_Before != nil {
				diff := new(big.Int).Sub(balB_Current, balB_Before)
				// Nhận được >= 490 MTN (sau khi trừ gas deploy TestCounter của recipient)
				threshold := new(big.Int).Mul(big.NewInt(490), big.NewInt(1e18))
				if diff.Cmp(threshold) >= 0 {
					successTransfer = true
					fmt.Printf("\n%s🎉 BINGOOOO! TIỀN ĐÃ MINT BÊN CHAIN B THÀNH CÔNG! (+%s MTN)%s\n", ColorGreen, formatMTN(diff), ColorReset)
				}
			}
		}

		// 2. Kiểm tra Contract TestCounter bên Chain B đã được kích hoạt hàm increment() (count == 1) chưa
		if !successCall {
			// getCounter() selector: 0xa87d942c
			getCounterData, _ := hexutil.Decode("0xa87d942c")
			result, err := ethCall(rpcB, targetContractAddr, getCounterData)
			if err == nil && result != "" && result != "0x" {
				val := new(big.Int).SetBytes(common.FromHex(result))
				if val != nil && val.Cmp(big.NewInt(1)) == 0 {
					successCall = true
					fmt.Printf("\n%s🎉 BINGOOOO! SMART CONTRACT CHAIN B ĐÃ NHẬN LỆNH TỪ CHAIN A VÀ THỰC THI THÀNH CÔNG! (Counter = 1)%s\n", ColorGreen, ColorReset)
				}
			}
		}

		if successTransfer && successCall {
			fmt.Printf("\n✅ HOÀN TẤT KỊCH BẢN CLIENT!\n")
			return
		}

		fmt.Printf(".")
		time.Sleep(1 * time.Second)
	}

	fmt.Printf("\n\n❌ Quá thời gian chờ (60s)! Hệ thống ngầm (Relayer) có vẻ chưa chạy hoặc chưa xử lý kịp.\n")
}
