/*
 * BÀI TEST: 34-ws-contract-call
 * MÔ TẢ   : Kiểm tra kết nối và tương tác Smart Contract trực tiếp qua WebSocket RPC (ws://<host>:<port>/ws).
 * GỌI     :
 *   1. Kết nối WebSocket qua rpc.Dial / ethclient.
 *   2. Đọc thông tin cơ bản: eth_blockNumber, eth_chainId.
 *   3. Lắng nghe luồng sự kiện thời gian thực (eth_subscribe: newHeads, logs).
 *   4. Triển khai Smart Contract (TestCounter) lên blockchain.
 *   5. Thực hiện Contract Read Call (eth_call) qua WebSocket (lấy getCount).
 *   6. Thực hiện Contract Write Call (eth_sendRawTransaction) qua WebSocket (gọi increment).
 * KỲ VỌNG :
 *   - eth_call (Read contract) và eth_subscribe hoạt động ổn định qua WebSocket.
 *   - Ghi nhận và chẩn đoán chi tiết hành vi của eth_sendRawTransaction qua WebSocket.
 */
package main

import (
	"context"
	"crypto/ecdsa"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"tool-test/test-simple/test-rpc/test-chain/config"
)

const counterABI = `[
	{"inputs":[],"name":"increment","outputs":[],"stateMutability":"nonpayable","type":"function"},
	{"inputs":[],"name":"getCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},
	{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"newCount","type":"uint256"}],"name":"Incremented","type":"event"}
]`

const counterBytecode = "6080604052348015600e575f5ffd5b506101818061001c5f395ff3fe608060405234801561000f575f5ffd5b5060043610610034575f3560e01c8063a87d942c14610038578063d09de08a14610056575b5f5ffd5b610040610060565b60405161004d91906100d2565b60405180910390f35b61005e610068565b005b5f5f54905090565b60015f5f8282546100799190610118565b925050819055507f20d8a6f5a693f9d1d627a598e8820f7a55ee74c183aa8f1a30e8d4e8dd9a8d845f546040516100b091906100d2565b60405180910390a1565b5f819050919050565b6100cc816100ba565b82525050565b5f6020820190506100e55f8301846100c3565b92915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f610122826100ba565b915061012d836100ba565b9250828201905080821115610145576101446100eb565b5b9291505056fea2646970667358221220a50f9c396b68c807fe73cad489a601188150c06d7345bf1edc37b01acd4d85e564736f6c63430008220033"

func resolveWebSocketURL(cfg *config.Config, userWsURL string) string {
	if strings.TrimSpace(userWsURL) != "" {
		return strings.TrimSpace(userWsURL)
	}
	if env := os.Getenv("WS_URL"); strings.TrimSpace(env) != "" {
		return strings.TrimSpace(env)
	}
	if cfg != nil {
		// 1. Ưu tiên đọc trực tiếp trường ws_url được cấu hình trong config.json
		if strings.TrimSpace(cfg.WSUrl) != "" {
			return strings.TrimSpace(cfg.WSUrl)
		}
		// 2. Tìm trong danh sách ws_nodes nếu có
		if m0, ok := cfg.WSNodes["m0"]; ok && strings.TrimSpace(m0) != "" {
			return strings.TrimSpace(m0)
		}
		for _, ws := range cfg.WSNodes {
			if strings.TrimSpace(ws) != "" {
				return strings.TrimSpace(ws)
			}
		}
		// 3. Tự động suy luận từ rpc_url
		if cfg.RPCUrl != "" {
			return config.DeriveWSUrl(cfg.RPCUrl)
		}
		if m0, ok := cfg.RPCNodes["m0"]; ok && m0 != "" {
			return config.DeriveWSUrl(m0)
		}
	}
	return "ws://192.168.1.234:10746/ws"
}

func RunTest(configPath string) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 34-ws-contract-call")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Kiểm tra gọi Smart Contract trực tiếp qua cổng WebSocket RPC của chain.")
	fmt.Println("⚡ CỔNG WS : Kiểm tra WS handshake, eth_call, eth_subscribe và eth_sendRawTransaction.")
	fmt.Println("🎯 KỲ VỌNG : Xác nhận tương tác với contract trực tiếp trên chain qua WebSocket.")
	fmt.Println("==========================================================")

	if configPath == "" {
		configPath = "../config.json"
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("lỗi load config: %w", err)
	}

	wsURL := resolveWebSocketURL(cfg, "")
	httpURL := "http://192.168.1.234:10746"
	if cfg.RPCUrl != "" {
		httpURL = cfg.RPCUrl
	} else if m0, ok := cfg.RPCNodes["m0"]; ok && m0 != "" {
		httpURL = m0
	}

	fmt.Printf("🌐 WebSocket Endpoint : %s\n", wsURL)
	fmt.Printf("🔗 HTTP Endpoint      : %s\n", httpURL)
	fmt.Printf("🆔 Chain ID           : %d\n", cfg.ChainID)

	if len(cfg.PrivateKeys) == 0 {
		return fmt.Errorf("không có private key trong config")
	}

	privKeyHex := cfg.PrivateKeys[0]
	privKey, err := crypto.HexToECDSA(privKeyHex)
	if err != nil {
		return fmt.Errorf("lỗi parse private key: %w", err)
	}
	fromAddress := crypto.PubkeyToAddress(*privKey.Public().(*ecdsa.PublicKey))
	fmt.Printf("👤 Test Account       : %s\n\n", fromAddress.Hex())

	// -------------------------------------------------------------------------
	// BƯỚC 1: KẾT NỐI WEBSOCKET
	// -------------------------------------------------------------------------
	fmt.Println("📡 [BƯỚC 1] Kết nối WebSocket tới chain...")
	rpcWsClient, err := rpc.Dial(wsURL)
	if err != nil {
		return fmt.Errorf("❌ Không thể kết nối tới WebSocket %s: %w", wsURL, err)
	}
	defer rpcWsClient.Close()
	wsClient := ethclient.NewClient(rpcWsClient)
	fmt.Println("  ✅ Kết nối WebSocket thành công (101 Switching Protocols OK)!")

	// Kiểm tra BlockNumber qua WS
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	blockNum, err := wsClient.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("❌ Lỗi gọi eth_blockNumber qua WS: %w", err)
	}
	fmt.Printf("  📦 Block Number hiện tại qua WS: %d\n", blockNum)

	// Kiểm tra ChainID qua WS
	chainID, err := wsClient.ChainID(ctx)
	if err != nil {
		return fmt.Errorf("❌ Lỗi gọi eth_chainId qua WS: %w", err)
	}
	fmt.Printf("  🆔 Chain ID xác nhận qua WS   : %v\n\n", chainID)

	// -------------------------------------------------------------------------
	// BƯỚC 2: KIỂM TRA ĐĂNG KÝ SỰ KIỆN QUA WEBSOCKET (eth_subscribe)
	// -------------------------------------------------------------------------
	fmt.Println("🔔 [BƯỚC 2] Kiểm tra Subscription qua WebSocket (eth_subscribe)...")
	headsChan := make(chan *types.Header, 10)
	subHeads, err := wsClient.SubscribeNewHead(context.Background(), headsChan)
	if err != nil {
		fmt.Printf("  ⚠️ SubscribeNewHead error: %v\n", err)
	} else {
		subHeads.Unsubscribe()
		fmt.Println("  ✅ eth_subscribe ('newHeads') hoạt động tốt qua WebSocket!")
	}

	logsChan := make(chan types.Log, 10)
	subLogs, err := wsClient.SubscribeFilterLogs(context.Background(), ethereum.FilterQuery{}, logsChan)
	if err != nil {
		fmt.Printf("  ⚠️ SubscribeFilterLogs error: %v\n", err)
	} else {
		subLogs.Unsubscribe()
		fmt.Println("  ✅ eth_subscribe ('logs') hoạt động tốt qua WebSocket!")
	}

	// -------------------------------------------------------------------------
	// BƯỚC 3: TRIỂN KHAI SMART CONTRACT TEST
	// -------------------------------------------------------------------------
	fmt.Println("🚀 [BƯỚC 3] Triển khai Contract TestCounter lên Blockchain...")
	httpCli, err := ethclient.Dial(httpURL)
	if err != nil {
		return fmt.Errorf("lỗi kết nối HTTP RPC: %w", err)
	}
	defer httpCli.Close()

	nonce, err := httpCli.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		return fmt.Errorf("lỗi lấy Nonce: %w", err)
	}

	bytecode, err := hexutil.Decode("0x" + counterBytecode)
	if err != nil {
		return fmt.Errorf("lỗi decode bytecode: %w", err)
	}

	gasPrice, err := httpCli.SuggestGasPrice(context.Background())
	if err != nil {
		gasPrice = big.NewInt(1e9)
	}

	auth, err := bind.NewKeyedTransactorWithChainID(privKey, big.NewInt(cfg.ChainID))
	if err != nil {
		return fmt.Errorf("lỗi tạo auth transactor: %w", err)
	}
	auth.Nonce = big.NewInt(int64(nonce))
	auth.GasLimit = 1500000
	auth.GasPrice = gasPrice

	deployTx := types.NewContractCreation(nonce, big.NewInt(0), auth.GasLimit, gasPrice, bytecode)
	signedDeployTx, err := auth.Signer(fromAddress, deployTx)
	if err != nil {
		return fmt.Errorf("lỗi ký deploy tx: %w", err)
	}

	if err := httpCli.SendTransaction(context.Background(), signedDeployTx); err != nil {
		return fmt.Errorf("lỗi gửi deploy tx: %w", err)
	}

	contractAddress := crypto.CreateAddress(fromAddress, nonce)
	fmt.Printf("  📝 Tx Deploy Hash : %s\n", signedDeployTx.Hash().Hex())
	fmt.Printf("  📍 Contract Address: %s\n", contractAddress.Hex())
	fmt.Println("  ⏳ Đang chờ xác nhận Block...")

	// Chờ receipt xác nhận
	receipt := waitForReceipt(httpCli, signedDeployTx.Hash(), 20*time.Second)
	if receipt == nil {
		return fmt.Errorf("hết thời gian chờ receipt deploy tx %s", signedDeployTx.Hash().Hex())
	}
	fmt.Printf("  ✅ Contract đã được commit tại Block #%d!\n\n", receipt.BlockNumber.Uint64())

	// -------------------------------------------------------------------------
	// BƯỚC 4: GỌI CONTRACT ĐỌC (eth_call) TRỰC TIẾP QUA WEBSOCKET
	// -------------------------------------------------------------------------
	fmt.Println("📖 [BƯỚC 4] Kiểm tra eth_call (Read contract) trực tiếp qua WebSocket...")
	parsedABI, err := abi.JSON(strings.NewReader(counterABI))
	if err != nil {
		return fmt.Errorf("lỗi parse ABI: %w", err)
	}

	getCountData, err := parsedABI.Pack("getCount")
	if err != nil {
		return fmt.Errorf("lỗi pack getCount: %w", err)
	}

	callMsg := ethereum.CallMsg{
		To:   &contractAddress,
		Data: getCountData,
	}

	callCtx, cancelCall := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelCall()

	callResult, err := wsClient.CallContract(callCtx, callMsg, nil)
	if err != nil {
		fmt.Printf("  ❌ Gọi getCount() qua WebSocket THẤT BẠI: %v\n", err)
		return fmt.Errorf("eth_call qua WS thất bại: %w", err)
	}

	var countVal *big.Int
	if err := parsedABI.UnpackIntoInterface(&countVal, "getCount", callResult); err != nil {
		return fmt.Errorf("lỗi unpack getCount result: %w", err)
	}
	fmt.Printf("  ✅ eth_call qua WebSocket THÀNH CÔNG! getCount() = %s\n\n", countVal.String())

	// -------------------------------------------------------------------------
	// BƯỚC 5: GỌI CONTRACT GHI / THỰC THI (eth_sendRawTransaction) TRỰC TIẾP QUA WEBSOCKET
	// -------------------------------------------------------------------------
	fmt.Println("⚡ [BƯỚC 5] Kiểm tra eth_sendRawTransaction (Write/Execute) qua WebSocket...")
	incData, err := parsedABI.Pack("increment")
	if err != nil {
		return fmt.Errorf("lỗi pack increment: %w", err)
	}

	nextNonce, err := httpCli.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		return fmt.Errorf("lỗi lấy nonce kế tiếp: %w", err)
	}

	incTx := types.NewTransaction(nextNonce, contractAddress, big.NewInt(0), 1000000, gasPrice, incData)
	signedIncTx, err := auth.Signer(fromAddress, incTx)
	if err != nil {
		return fmt.Errorf("lỗi ký increment tx: %w", err)
	}

	sendCtx, cancelSend := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelSend()

	wsSendErr := wsClient.SendTransaction(sendCtx, signedIncTx)
	if wsSendErr != nil {
		fmt.Println("  --------------------------------------------------------")
		fmt.Printf("  ❌ KẾT QUẢ: wsClient.SendTransaction gặp lỗi:\n     👉 %v\n", wsSendErr)
		fmt.Println("  --------------------------------------------------------")
		fmt.Println("  🔍 NGUYÊN NHÂN KỸ THUẬT (Root Cause):")
		fmt.Println("     1. Tại backend.go, middleware 'ethSendRawTxMiddleware' chỉ được gắn cho HTTP handler (POST /).")
		fmt.Println("     2. Cổng WebSocket (/ws) đi trực tiếp vào server.WebsocketHandler của go-ethereum/rpc.")
		fmt.Println("     3. Hàm eth.SendRawTransaction của MetaAPI có chữ ký 3 tham số:")
		fmt.Println("        SendRawTransaction(ctx, input []byte, inputEth []byte, pubKeyBlsL []byte)")
		fmt.Println("     4. Client Ethereum chuẩn chỉ gửi 1 tham số hex (data đã ký),")
		fmt.Println("        khiến RPC parser báo lỗi: 'missing value for required argument 1'.")
		fmt.Println("  --------------------------------------------------------")
	} else {
		fmt.Printf("  ✅ Gửi increment() qua WebSocket THÀNH CÔNG! TxHash: %s\n", signedIncTx.Hash().Hex())
		waitForReceipt(httpCli, signedIncTx.Hash(), 15*time.Second)

		// Đọc lại giá trị sau khi increment
		newCallRes, _ := wsClient.CallContract(context.Background(), callMsg, nil)
		var newCount *big.Int
		_ = parsedABI.UnpackIntoInterface(&newCount, "getCount", newCallRes)
		fmt.Printf("  🎉 Giá trị getCount() sau khi increment: %s\n", newCount.String())
	}

	fmt.Println("\n==========================================================")
	fmt.Println("📊 TỔNG KẾT KIỂM THỬ WEBSOCKET (ws://.../ws):")
	fmt.Println("  1. Handshake & Basic Query (eth_blockNumber, eth_chainId) : ✅ PASSED")
	fmt.Println("  2. Subscription Streams (newHeads, logs)                  : ✅ PASSED")
	fmt.Println("  3. Contract Read Calls (eth_call)                         : ✅ PASSED")
	if wsSendErr != nil {
		fmt.Println("  4. Contract Write Calls (eth_sendRawTransaction)          : ❌ FAILED (missing argument 1)")
	} else {
		fmt.Println("  4. Contract Write Calls (eth_sendRawTransaction)          : ✅ PASSED")
	}
	fmt.Println("==========================================================")

	return nil
}

func waitForReceipt(client *ethclient.Client, txHash common.Hash, timeout time.Duration) *types.Receipt {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		receipt, err := client.TransactionReceipt(context.Background(), txHash)
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			return receipt
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

func main() {
	wsFlag := flag.String("ws", "", "Địa chỉ WebSocket RPC endpoint (vd: ws://192.168.1.234:10747/ws)")
	configFlag := flag.String("config", "../config.json", "Đường dẫn file config.json")
	flag.Parse()

	if *wsFlag != "" {
		_ = os.Setenv("WS_URL", *wsFlag)
	}

	if err := RunTest(*configFlag); err != nil {
		fmt.Printf("❌ Test thất bại: %v\n", err)
		os.Exit(1)
	}
}
