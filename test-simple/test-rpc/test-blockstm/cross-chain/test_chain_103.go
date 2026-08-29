package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
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

var httpClient = &http.Client{Timeout: 10 * time.Second}

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

func waitForReceipt(url string, txHash common.Hash, timeout time.Duration) error {
	start := time.Now()
	for time.Since(start) < timeout {
		res, err := callRPC(url, "eth_getTransactionReceipt", []interface{}{txHash.Hex()})
		if err == nil && len(res) > 0 && string(res) != "null" {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for receipt: %s", txHash.Hex())
}

func formatMTN(wei *big.Int) string {
	if wei == nil {
		return "0.0000"
	}
	f := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(1e18))
	return fmt.Sprintf("%.4f", f)
}

func main() {
	rpc101 := "http://127.0.0.1:8546"
	rpc102 := "http://127.0.0.1:8547"
	rpc103 := "http://127.0.0.1:8548"
	rpc991 := "http://192.168.1.232:10746"

	keySender := "3f7a0514531a1485edc4270f06dbed62da4974c3b5bbd54a4534060514b8023d"
	privKeySender, _ := crypto.HexToECDSA(keySender)
	senderAddr := crypto.PubkeyToAddress(privKeySender.PublicKey)

	keyRecipient := "2aad2565bed5347214de1c14752933e9a410a76daed530e80ed6ce7af9363cf4"
	privKeyRecipient, _ := crypto.HexToECDSA(keyRecipient)
	recipientAddr := crypto.PubkeyToAddress(privKeyRecipient.PublicKey)

	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("🌐 METANODE: TEST CROSS-CHAIN TRANSFER CHAIN 101 ➔ CHAIN 103 (NEW PRIVATE CHAIN)")
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Printf("Sender (Chain 101):    %s\n", senderAddr.Hex())
	fmt.Printf("Recipient (Chain 103): %s\n", recipientAddr.Hex())

	// Check block numbers
	b101, _ := callRPC(rpc101, "eth_blockNumber", []interface{}{})
	b102, _ := callRPC(rpc102, "eth_blockNumber", []interface{}{})
	b103, _ := callRPC(rpc103, "eth_blockNumber", []interface{}{})
	b991, _ := callRPC(rpc991, "eth_blockNumber", []interface{}{})
	fmt.Printf("Block heights: RootAnchor(991)=%s, Chain101=%s, Chain102=%s, Chain103=%s\n\n",
		string(b991), string(b101), string(b102), string(b103))

	// Initial balances
	bal101_Before, _ := getBalance(rpc101, senderAddr.Hex())
	bal103_Before, _ := getBalance(rpc103, recipientAddr.Hex())
	fmt.Printf("📊 SỐ DƯ BAN ĐẦU:\n")
	fmt.Printf("   ├─ Chain 101 (Sender):    %s MTN (%s wei)\n", formatMTN(bal101_Before), bal101_Before.String())
	fmt.Printf("   └─ Chain 103 (Recipient): %s MTN (%s wei)\n", formatMTN(bal103_Before), bal103_Before.String())

	// Step 1: Outbound transfer from Chain 101 to Chain 103
	parsedGatewayABI, _ := abi.JSON(strings.NewReader(GatewayABI))
	transferAmount := new(big.Int).Mul(big.NewInt(100), big.NewInt(1e18)) // 100 MTN
	tipAmount := big.NewInt(1_000_000_000_000_000_000)                    // 1 MTN Tip
	gasLimit := uint64(500000)
	gasPrice := big.NewInt(1000000000)

	// destChainId = 103
	outboundData, err := parsedGatewayABI.Pack("outbound", big.NewInt(103), recipientAddr, []byte{}, big.NewInt(0), transferAmount, tipAmount, big.NewInt(0), uint8(1), false)
	if err != nil {
		fmt.Printf("❌ Pack error: %v\n", err)
		return
	}

	nonce101, err := getNonce(rpc101, senderAddr.Hex())
	if err != nil {
		fmt.Printf("❌ Get nonce error: %v\n", err)
		return
	}

	totalBurn := new(big.Int).Add(transferAmount, tipAmount)
	tx := types.NewTransaction(nonce101, GatewayAddress, totalBurn, gasLimit, gasPrice, outboundData)
	signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(101)), privKeySender)
	rawTxBytes, _ := signedTx.MarshalBinary()

	fmt.Printf("\n🚀 GỬI 100 MTN TỪ CHAIN 101 SANG PRIVATE CHAIN 103 MỚI (Nonce=%d)...\n", nonce101)
	txHash, err := sendRawTransaction(rpc101, rawTxBytes)
	if err != nil {
		fmt.Printf("❌ SendRawTransaction error: %v\n", err)
		return
	}
	fmt.Printf("   ✅ TxHash on Chain 101: %s\n", txHash.Hex())

	if err := waitForReceipt(rpc101, txHash, 15*time.Second); err != nil {
		fmt.Printf("❌ Wait for receipt error: %v\n", err)
		return
	}
	fmt.Printf("   ✅ Transaction mined on Chain 101!\n")

	// Step 2: Wait for Relayer Daemon to relay from Chain 101 -> Chain 103
	fmt.Printf("\n⏳ ĐỢI RELAYER DAEMON XỬ LÝ CHUYỂN TIỀN VỀ CHAIN 103...\n")
	success := false
	for i := 0; i < 45; i++ {
		bal103_Current, err := getBalance(rpc103, recipientAddr.Hex())
		if err == nil && bal103_Current != nil && bal103_Before != nil {
			diff := new(big.Int).Sub(bal103_Current, bal103_Before)
			threshold := new(big.Int).Mul(big.NewInt(99), big.NewInt(1e18)) // >= 99 MTN
			if diff.Cmp(threshold) >= 0 {
				success = true
				fmt.Printf("\n🎉 THÀNH CÔNG RỰC RỠ! CHAIN 103 ĐÃ NHẬN ĐƯỢC TIỀN (+%s MTN)!\n", formatMTN(diff))
				fmt.Printf("   📊 Số dư mới trên Chain 103: %s MTN\n", formatMTN(bal103_Current))
				break
			}
		}
		fmt.Print(".")
		time.Sleep(1 * time.Second)
	}

	if !success {
		fmt.Printf("\n❌ Quá thời gian chờ (45s). Kiểm tra relayer.log để biết chi tiết.\n")
		return
	}

	// Step 3: Test transfer from Chain 103 back to Chain 102
	fmt.Println("\n═══════════════════════════════════════════════════════════════")
	fmt.Println("🌐 TEST TIẾP: CHUYỂN 30 MTN TỪ CHAIN 103 SANG CHAIN 102")
	fmt.Println("═══════════════════════════════════════════════════════════════")
	bal102_Before, _ := getBalance(rpc102, senderAddr.Hex())
	fmt.Printf("📊 Số dư trước khi chuyển trên Chain 102: %s MTN\n", formatMTN(bal102_Before))

	transferBackAmount := new(big.Int).Mul(big.NewInt(30), big.NewInt(1e18))
	outboundBackData, _ := parsedGatewayABI.Pack("outbound", big.NewInt(102), senderAddr, []byte{}, big.NewInt(0), transferBackAmount, tipAmount, big.NewInt(0), uint8(1), false)
	nonce103, _ := getNonce(rpc103, recipientAddr.Hex())
	totalBurnBack := new(big.Int).Add(transferBackAmount, tipAmount)

	txBack := types.NewTransaction(nonce103, GatewayAddress, totalBurnBack, gasLimit, gasPrice, outboundBackData)
	signedTxBack, _ := types.SignTx(txBack, types.NewEIP155Signer(big.NewInt(103)), privKeyRecipient)
	rawTxBackBytes, _ := signedTxBack.MarshalBinary()

	fmt.Printf("🚀 GỬI 30 MTN TỪ CHAIN 103 SANG CHAIN 102 (Nonce=%d)...\n", nonce103)
	txHashBack, err := sendRawTransaction(rpc103, rawTxBackBytes)
	if err != nil {
		fmt.Printf("❌ SendRawTransaction error on Chain 103: %v\n", err)
		return
	}
	fmt.Printf("   ✅ TxHash on Chain 103: %s\n", txHashBack.Hex())
	waitForReceipt(rpc103, txHashBack, 15*time.Second)

	fmt.Printf("\n⏳ ĐỢI RELAYER DAEMON XỬ LÝ CHUYỂN TIỀN VỀ CHAIN 102...\n")
	successBack := false
	for i := 0; i < 45; i++ {
		bal102_Current, err := getBalance(rpc102, senderAddr.Hex())
		if err == nil && bal102_Current != nil && bal102_Before != nil {
			diff := new(big.Int).Sub(bal102_Current, bal102_Before)
			threshold := new(big.Int).Mul(big.NewInt(29), big.NewInt(1e18)) // >= 29 MTN
			if diff.Cmp(threshold) >= 0 {
				successBack = true
				fmt.Printf("\n🎉 THÀNH CÔNG RỰC RỠ! CHAIN 102 ĐÃ NHẬN ĐƯỢC TIỀN TỪ CHAIN 103 (+%s MTN)!\n", formatMTN(diff))
				fmt.Printf("   📊 Số dư mới trên Chain 102: %s MTN\n", formatMTN(bal102_Current))
				break
			}
		}
		fmt.Print(".")
		time.Sleep(1 * time.Second)
	}

	if !successBack {
		fmt.Printf("\n❌ Quá thời gian chờ (45s). Kiểm tra relayer.log để biết chi tiết.\n")
		return
	}

	fmt.Println("\n═══════════════════════════════════════════════════════════════")
	fmt.Println("🏆 TẤT CẢ CÁC BƯỚC TEST PRIVATE CHAIN 103 MỚI ĐÃ HOÀN TẤT XUẤT SẮC 100%!")
	fmt.Println("═══════════════════════════════════════════════════════════════")
}
