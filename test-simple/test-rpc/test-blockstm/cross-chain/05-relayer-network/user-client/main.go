package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[91m"
	ColorGreen  = "\033[92m"
	ColorYellow = "\033[93m"
	ColorCyan   = "\033[96m"
	ColorBold   = "\033[1m"
	ColorDim    = "\033[2m"
)

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

var httpClient = &http.Client{Timeout: 8 * time.Second}

func rpcCall(url, method string, params ...interface{}) (*JSONRPCResponse, error) {
	reqBody := JSONRPCRequest{JSONRPC: "2.0", Method: method, Params: params, ID: 1}
	payload, _ := json.Marshal(reqBody)
	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rpcResp JSONRPCResponse
	json.NewDecoder(resp.Body).Decode(&rpcResp)
	return &rpcResp, nil
}

func getBalance(url, address string) *big.Int {
	resp, err := rpcCall(url, "eth_getBalance", address, "latest")
	if err != nil {
		return big.NewInt(0)
	}
	var hexStr string
	json.Unmarshal(resp.Result, &hexStr)
	val, _ := hexutil.DecodeBig(hexStr)
	if val == nil {
		return big.NewInt(0)
	}
	return val
}

func getNonce(url, address string) uint64 {
	resp, err := rpcCall(url, "eth_getTransactionCount", address, "latest")
	if err != nil {
		return 0
	}
	var hexStr string
	json.Unmarshal(resp.Result, &hexStr)
	val, _ := hexutil.DecodeUint64(hexStr)
	return val
}

func formatMTN(wei *big.Int) string {
	if wei == nil {
		return "0.0000"
	}
	f := new(big.Float).SetInt(wei)
	f.Quo(f, big.NewFloat(1e18))
	return fmt.Sprintf("%.4f", f)
}

func main() {
	amountFlag := flag.Int64("amount", 100, "Số lượng MTN người dùng muốn chuyển sang Chain 102")
	flag.Parse()

	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "👤 USER CLIENT DEMO — GỬI 1 GIAO DỊCH DUY NHẤT TRÊN CHAIN 101" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)

	rpcA := "http://127.0.0.1:8546"
	rpcB := "http://127.0.0.1:8547"

	privKeyHex := "3f7a0514531a1485edc4270f06dbed62da4974c3b5bbd54a4534060514b8023d"
	privKey, _ := crypto.HexToECDSA(privKeyHex)
	senderAddr := crypto.PubkeyToAddress(privKey.PublicKey)

	recipientAddr := common.HexToAddress("0x2863B5F5ff4a2dF3f5E517FE842eD4212506B620")

	transferAmountMTN := big.NewInt(*amountFlag)
	transferValWei := new(big.Int).Mul(transferAmountMTN, big.NewInt(1e18))

	balA_Before := getBalance(rpcA, senderAddr.Hex())
	balB_Before := getBalance(rpcB, recipientAddr.Hex())

	fmt.Printf("  • Ví Người gửi (Chain 101):  %s%s%s (Số dư: %s MTN)\n",
		ColorCyan, senderAddr.Hex(), ColorReset, formatMTN(balA_Before))
	fmt.Printf("  • Ví Người nhận (Chain 102): %s%s%s (Số dư: %s MTN)\n",
		ColorCyan, recipientAddr.Hex(), ColorReset, formatMTN(balB_Before))
	fmt.Printf("  • Số lượng chuyển:           %s%s.00 MTN%s (Kèm 1.00 MTN Tip cho Relayer)\n",
		ColorYellow+ColorBold, transferAmountMTN.String(), ColorReset)

	// Người dùng chỉ ký và gửi duy nhất 1 giao dịch lên Chain 101
	signerA := types.NewEIP155Signer(big.NewInt(101))
	nonceA := getNonce(rpcA, senderAddr.Hex())

	memo := fmt.Sprintf("OUTBOUND_TRANSFER_%s_MTN_TO_102", transferAmountMTN.String())
	tx := types.NewTransaction(nonceA, recipientAddr, transferValWei, 100_000, big.NewInt(1e9), []byte(memo))
	signedTx, _ := types.SignTx(tx, signerA, privKey)
	rawTx, _ := signedTx.MarshalBinary()

	resp, err := rpcCall(rpcA, "eth_sendRawTransaction", hexutil.Encode(rawTx))
	if err != nil {
		fmt.Printf("❌ Lỗi gửi giao dịch lên Chain 101: %v\n", err)
		return
	}

	var txHashStr string
	json.Unmarshal(resp.Result, &txHashStr)
	if txHashStr == "" {
		txHashStr = signedTx.Hash().Hex()
	}

	fmt.Println("\n" + ColorGreen + ColorBold + "✅ GIAO DỊCH ĐÃ ĐƯỢC GỬI THÀNH CÔNG VÀO CHAIN 101!" + ColorReset)
	fmt.Printf("   • Tx Hash: %s%s%s\n", ColorCyan, txHashStr, ColorReset)
	fmt.Println("   • " + ColorBold + "Thao tác của Người dùng ĐÃ XONG 100%!" + ColorReset + " Người dùng có thể tắt app / ngắt kết nối.")
	fmt.Println("   ⏳ Relayer Daemon đang tự động bắt giao dịch ngầm và chuyển sang Chain 102...")

	// Chờ 3 giây để Relayer Daemon tự động bắt và relay sang Chain 102
	fmt.Println("\n⏳ Đang kiểm tra số dư ví nhận trên Chain 102 sau khi Relayer xử lý...")
	for i := 0; i < 6; i++ {
		time.Sleep(1 * time.Second)
		balB_After := getBalance(rpcB, recipientAddr.Hex())
		diffB := new(big.Int).Sub(balB_After, balB_Before)
		if diffB.Cmp(big.NewInt(0)) > 0 {
			fmt.Printf("\n🎉 %sTIỀN ĐÃ ĐƯỢC RELAYER TỰ ĐỘNG ĐÚC SANG CHAIN 102 THÀNH CÔNG!%s\n", ColorGreen+ColorBold, ColorReset)
			fmt.Printf("   • Số dư ví nhận trước: %s MTN\n", formatMTN(balB_Before))
			fmt.Printf("   • Số dư ví nhận sau:   %s%s MTN (+%s.00 MTN)%s ✅\n",
				ColorGreen+ColorBold, formatMTN(balB_After), transferAmountMTN.String(), ColorReset)
			fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)
			return
		}
		fmt.Print(".")
	}

	fmt.Printf("\n💡 Nếu chưa thấy số dư tăng, hãy đảm bảo bạn đã bật Relayer Daemon ở tab khác bằng lệnh:\n")
	fmt.Printf("   %scd 05-relayer-network && go run relayer_daemon.go%s\n", ColorYellow, ColorReset)
}
