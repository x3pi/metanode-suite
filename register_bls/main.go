package main

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	e_types "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	client_tcp "tool-test/pkg/client-tcp"
	tcp_config "tool-test/pkg/client-tcp/config"

	"tool-test/pkg/bls"
	"tool-test/pkg/logger"
	pb "tool-test/pkg/proto"

	"google.golang.org/protobuf/proto"
)

// ===================== HELPERS =====================

// protoReceiptToRpcReceipt chuyển pb.Receipt (proto) → pb.RpcReceipt (để giữ interface thống nhất)
func protoReceiptToRpcReceipt(rcpt *pb.Receipt) *pb.RpcReceipt {
	if rcpt == nil {
		return nil
	}
	gasUsed := fmt.Sprintf("0x%x", rcpt.GasUsed)
	txHash := common.BytesToHash(rcpt.TransactionHash).Hex()

	return &pb.RpcReceipt{
		TransactionHash: txHash,
		Status:          rcpt.Status,
		GasUsed:         gasUsed,
	}
}

// waitReceiptPoll đợi receipt qua RPC poll (fallback khi không có receipt inline).
func waitReceiptPoll(tcpClient *client_tcp.Client, txHash string) *pb.RpcReceipt {
	if txHash == "" {
		return nil
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		receipt, err := tcpClient.RpcGetTransactionReceipt(txHash)
		if err != nil {
			fmt.Printf("  ❌ Receipt error: %v\n", err)
			return nil
		}
		if receipt != nil {
			fmt.Printf("  ✅ Receipt (poll): status=%s, gasUsed=%s, logs=%d\n",
				receipt.Status, receipt.GasUsed, len(receipt.Logs))

			return &pb.RpcReceipt{
				TransactionHash: receipt.TransactionHash,
				Status:          pb.RECEIPT_STATUS(receipt.Status),
				GasUsed:         receipt.GasUsed,
				// Ignore logs or other fields if not needed, or map them if needed
			}
		}
		select {
		case <-timer.C:
			fmt.Println("  ⚠️ Timeout waiting for receipt")
			return nil
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// sendTxAndWait tạo, ký, gửi transaction + đợi receipt.
// Server trả receipt trực tiếp trong RPC response (proto Receipt bytes).
func sendTxAndWait(
	tcpClient *client_tcp.Client,
	privKey *ecdsa.PrivateKey,
	fromAddr common.Address,
	toAddr common.Address,
	parsedABI abi.ABI,
	signer e_types.Signer,
	method string,
	args ...interface{},
) (string, *pb.RpcReceipt) {
	nonce, err := tcpClient.ChainGetNonce(fromAddr)
	if err != nil {
		fmt.Printf("  ❌ GetNonce: %v\n", err)
		return "", nil
	}

	inputData, err := parsedABI.Pack(method, args...)
	if err != nil {
		fmt.Printf("  ❌ Pack %s: %v\n", method, err)
		return "", nil
	}

	tx := e_types.NewTransaction(nonce, toAddr, big.NewInt(0), 20000000, big.NewInt(10000000), inputData)
	signedTx, _ := e_types.SignTx(tx, signer, privKey)
	rawTxBytes, _ := signedTx.MarshalBinary()
	rawTxHex := "0x" + hex.EncodeToString(rawTxBytes)

	txHash, rcptBytes, err := tcpClient.RpcSendRawTransactionWithReceipt(rawTxHex)
	if err != nil {
		fmt.Printf("  ❌ SendTx %s: %v\n", method, err)
		return "", nil
	}
	fmt.Printf("  ✅ txHash: %s\n", txHash)

	// Nếu có receipt bytes → parse proto Receipt
	if len(rcptBytes) > 0 {
		protoReceipt := &pb.Receipt{}
		if unmarshalErr := proto.Unmarshal(rcptBytes, protoReceipt); unmarshalErr == nil {
			rpcReceipt := protoReceiptToRpcReceipt(protoReceipt)
			fmt.Printf("  ✅ Receipt: status=%s, gasUsed=%s\n", rpcReceipt.Status, rpcReceipt.GasUsed)
			return txHash, rpcReceipt
		}
	}

	// Intercepted TX (chỉ có txHash) → poll RPC
	receipt := waitReceiptPoll(tcpClient, txHash)
	return txHash, receipt
}

// sendTxAndWaitRPC gửi TX và poll RPC để nhận receipt.
// Dùng cho các TX bị intercepted bởi RPC proxy (vd: confirmAccountWithoutSign)
func sendTxAndWaitRPC(
	tcpClient *client_tcp.Client,
	privKey *ecdsa.PrivateKey,
	fromAddr common.Address,
	toAddr common.Address,
	parsedABI abi.ABI,
	signer e_types.Signer,
	method string,
	args ...interface{},
) (string, *pb.RpcReceipt) {
	nonce, err := tcpClient.ChainGetNonce(fromAddr)
	if err != nil {
		fmt.Printf("  ❌ GetNonce: %v\n", err)
		return "", nil
	}

	inputData, err := parsedABI.Pack(method, args...)
	if err != nil {
		fmt.Printf("  ❌ Pack %s: %v\n", method, err)
		return "", nil
	}

	tx := e_types.NewTransaction(nonce, toAddr, big.NewInt(0), 20000000, big.NewInt(10000000), inputData)
	signedTx, _ := e_types.SignTx(tx, signer, privKey)
	rawTxBytes, _ := signedTx.MarshalBinary()
	rawTxHex := "0x" + hex.EncodeToString(rawTxBytes)

	txHash, err := tcpClient.RpcSendRawTransaction(rawTxHex)
	if err != nil {
		fmt.Printf("  ❌ SendTx %s: %v\n", method, err)
		return "", nil
	}
	fmt.Printf("  ✅ txHash: %s\n", txHash)

	receipt := waitReceiptPoll(tcpClient, txHash)
	return txHash, receipt
}

// sendTxExpectError gửi transaction mà mong đợi lỗi (revert test)
func sendTxExpectError(
	tcpClient *client_tcp.Client,
	privKey *ecdsa.PrivateKey,
	fromAddr common.Address,
	toAddr common.Address,
	parsedABI abi.ABI,
	signer e_types.Signer,
	method string,
	args ...interface{},
) {
	nonce, err := tcpClient.ChainGetNonce(fromAddr)
	if err != nil {
		fmt.Printf("  ❌ GetNonce: %v\n", err)
		return
	}

	inputData, err := parsedABI.Pack(method, args...)
	if err != nil {
		fmt.Printf("  ❌ Pack %s: %v\n", method, err)
		return
	}

	tx := e_types.NewTransaction(nonce, toAddr, big.NewInt(0), 20000000, big.NewInt(10000000), inputData)
	signedTx, _ := e_types.SignTx(tx, signer, privKey)
	rawTxBytes, _ := signedTx.MarshalBinary()
	rawTxHex := "0x" + hex.EncodeToString(rawTxBytes)

	txHash, err := tcpClient.RpcSendRawTransaction(rawTxHex)
	if err != nil {
		fmt.Printf("  ✅ Transaction reverted as expected!\n")
		fmt.Printf("     Error: %v\n", err)
		return
	}
	fmt.Printf("  ⚠️ Transaction did NOT revert (txHash=%s), checking receipt...\n", txHash)
	receipt := waitReceiptPoll(tcpClient, txHash)
	if receipt != nil && receipt.Status != 1 {
		fmt.Printf("  ✅ Receipt shows revert: status=%s\n", receipt.Status)
	} else if receipt != nil {
		fmt.Printf("  ❌ Transaction succeeded unexpectedly: status=%s\n", receipt.Status)
	}
}

// sendTxFreeGas gửi transaction bị intercepted bởi RPC (không có receipt).
// Các hàm free gas (addAuthorizedWallet, addContractFreeGas, …) trả về txHash trực tiếp.
// Trả về (txHash, true) nếu thành công, ("" false) nếu lỗi.
func sendTxFreeGas(
	tcpClient *client_tcp.Client,
	privKey *ecdsa.PrivateKey,
	fromAddr common.Address,
	toAddr common.Address,
	parsedABI abi.ABI,
	signer e_types.Signer,
	method string,
	args ...interface{},
) (string, bool) {
	nonce, err := tcpClient.ChainGetNonce(fromAddr)
	if err != nil {
		fmt.Printf("  ❌ GetNonce: %v\n", err)
		return "", false
	}

	inputData, err := parsedABI.Pack(method, args...)
	if err != nil {
		fmt.Printf("  ❌ Pack %s: %v\n", method, err)
		return "", false
	}

	tx := e_types.NewTransaction(nonce, toAddr, big.NewInt(0), 20000000, big.NewInt(10000000), inputData)
	signedTx, _ := e_types.SignTx(tx, signer, privKey)
	rawTxBytes, _ := signedTx.MarshalBinary()
	rawTxHex := "0x" + hex.EncodeToString(rawTxBytes)

	txHash, err := tcpClient.RpcSendRawTransaction(rawTxHex)
	if err != nil {
		fmt.Printf("  ❌ %s: %v\n", method, err)
		return "", false
	}
	fmt.Printf("  ✅ %s → txHash: %s\n", method, txHash)
	return txHash, true
}

// makeEventHandler tạo callback log event chung
func makeEventHandler(eventName string, wg *sync.WaitGroup, once *sync.Once) func([]byte) {
	return func(eventData []byte) {
		event := &pb.RpcEvent{}
		if err := proto.Unmarshal(eventData, event); err != nil {
			fmt.Printf("  ❌ Failed to parse RpcEvent: %v\n", err)
			return
		}

		fmt.Printf("\n  📡 [%s] EVENT RECEIVED!\n", eventName)
		fmt.Printf("  ├─ SubscriptionID: %s\n", event.SubscriptionId)
		if event.Log != nil {
			fmt.Printf("  ├─ Contract:       %s\n", event.Log.Address)
			fmt.Printf("  ├─ BlockNumber:    %s\n", event.Log.BlockNumber)
			fmt.Printf("  ├─ TxHash:         %s\n", event.Log.TransactionHash)
			fmt.Printf("  ├─ Topics:         %v\n", event.Log.Topics)
			fmt.Printf("  └─ Data:           %s\n", event.Log.Data)
		}

		if once != nil && wg != nil {
			once.Do(func() {
				wg.Done()
			})
		}
	}
}

// ===================== TEST: BLS Registration =====================

func testBlsRegistration(
	tcpClient *client_tcp.Client,
	accountABI abi.ABI,
	accountContract common.Address,
	adminPrivKey *ecdsa.PrivateKey,
	adminAddr common.Address,
	signer e_types.Signer,
	count int,
	outFile string,
) {
	bls.Init()
	fmt.Println("\n╔══════════════════════════════════════════════════════╗")
	fmt.Printf("║  TEST: BLS Registration + Confirm (%d keys)           ║\n", count)
	fmt.Println("╚══════════════════════════════════════════════════════╝")

	type KeyGenResult struct {
		Address       string `json:"address"`
		PrivateKey    string `json:"private_key"`
		BlsPubKey     string `json:"bls_pub_key"`
		BlsPrivateKey string `json:"bls_private_key"`
	}
	var results []KeyGenResult

	for i := 0; i < count; i++ {
		fmt.Printf("\n─── Generating Key %d of %d ───\n", i+1, count)
		// Step 1: Tạo private key mới
		newPrivKey, err := crypto.GenerateKey()
		if err != nil {
			fmt.Printf("  ❌ GenerateKey: %v\n", err)
			continue
		}
		newAddr := crypto.PubkeyToAddress(newPrivKey.PublicKey)
		newPrivKeyHex := hex.EncodeToString(crypto.FromECDSA(newPrivKey))
		fmt.Printf("  ✅ New address:     %s\n", newAddr.Hex())
		fmt.Printf("  ✅ New private key: %s\n", newPrivKeyHex)

		// Step 2: Gửi getPublickeyBls qua RpcEthCall để lấy BLS pubkey từ node
		getPubKeyInput, _ := accountABI.Pack("getPublickeyBls")
		blsPubKeyResult, err := tcpClient.RpcEthCall(accountContract, getPubKeyInput)
		if err != nil {
			fmt.Printf("  ❌ getPublickeyBls RpcEthCall: %v\n", err)
			continue
		}
		
		var blsPubKey []byte
		var hexStr string
		if err := json.Unmarshal(blsPubKeyResult, &hexStr); err == nil {
			hexStr = strings.TrimPrefix(hexStr, "0x")
			blsPubKey, err = hex.DecodeString(hexStr)
		} else {
			err = accountABI.UnpackIntoInterface(&blsPubKey, "getPublickeyBls", blsPubKeyResult)
		}

		if err != nil {
			fmt.Printf("  ❌ Decode getPublickeyBls: %v\n", err)
			continue
		}

		serverBlsPubKey := "0x" + hex.EncodeToString(blsPubKey)
		fmt.Printf("  ✅ Fetched BLS PublicKey from node: %s (%d bytes)\n", serverBlsPubKey, len(blsPubKey))

		if len(blsPubKey) == 0 {
			fmt.Println("  ❌ BLS pubkey rỗng, bỏ qua")
			continue
		}

		// Step 3: Gửi setBlsPublicKey
		var wgRegister sync.WaitGroup
		wgRegister.Add(1)

		nonce, _ := tcpClient.ChainGetNonce(newAddr)
		inputData, _ := accountABI.Pack("setBlsPublicKey", blsPubKey)
		tx := e_types.NewTransaction(nonce, accountContract, big.NewInt(0), 20000000, big.NewInt(10000000), inputData)
		signedTx, _ := e_types.SignTx(tx, signer, newPrivKey)
		rawTxBytes, _ := signedTx.MarshalBinary()
		txHash, err := tcpClient.RpcSendRawTransaction("0x" + hex.EncodeToString(rawTxBytes))
		if err != nil {
			fmt.Printf("  ❌ setBlsPublicKey: %v\n", err)
			continue
		}
		fmt.Printf("  ✅ setBlsPublicKey sent: txHash=%s\n", txHash)

		// Đợi RegisterBls event
		fmt.Println("  ⏳ Waiting for RegisterBls event (max 10s)...")
		regDone := make(chan struct{})
		go func() {
			wgRegister.Wait()
			close(regDone)
		}()

		// Step 4: confirmAccountWithoutSign (admin confirm)
		// Dùng RPC poll vì TX này bị intercepted bởi RPC proxy → receipt về qua kênh RPC,
		// không push qua TCP direct (port 4200). sendTxAndWait sẽ timeout nếu dùng directClient.
		fmt.Printf("  ℹ️  Admin %s confirming %s...\n", adminAddr.Hex(), newAddr.Hex())
		txHash2, receipt2 := sendTxAndWaitRPC(
			tcpClient, adminPrivKey, adminAddr, accountContract,
			accountABI, signer,
			"confirmAccountWithoutSign", newAddr,
		)
		if receipt2 != nil {
			fmt.Printf("  ✅ confirmAccountWithoutSign OK: txHash=%s\n", txHash2)
		} else {
			fmt.Println("  ⚠️ confirmAccountWithoutSign — receipt not available (may be intercepted)")
		}

		results = append(results, KeyGenResult{
			Address:       newAddr.Hex(),
			PrivateKey:    newPrivKeyHex,
			BlsPubKey:     serverBlsPubKey,
			BlsPrivateKey: "", // private key được node giữ
		})

		// Nghỉ 1s giữa các lần tạo
		if i < count-1 {
			time.Sleep(1 * time.Second)
		}
	}

	fmt.Println("\n  ✅ BLS Registration flow completed!")
	if len(results) > 0 {
		outBytes, err := json.MarshalIndent(results, "", "  ")
		if err == nil {
			err = os.WriteFile(outFile, outBytes, 0644)
			if err == nil {
				fmt.Printf("  💾 Đã lưu thành công %d keys vào file: %s\n", len(results), outFile)
			} else {
				fmt.Printf("  ❌ Không thể lưu file %s: %v\n", outFile, err)
			}
		}
	}
}

// ===================== REGISTER BLS FOR EXISTING WALLET =====================

// registerBlsForExistingWallet đăng ký BLS pubkey của node cho một ví (private key) đã có sẵn.
// Flow:
//  1. Lấy BLS pubkey từ node qua getPublickeyBls
//  2. Gọi setBlsPublicKey từ ví của người dùng
//  3. (Optional) Admin confirm nếu truyền admin key
func registerBlsForExistingWallet(
	tcpClient *client_tcp.Client,
	accountABI abi.ABI,
	accountContract common.Address,
	userPrivKey *ecdsa.PrivateKey,
	userAddr common.Address,
	adminPrivKey *ecdsa.PrivateKey,
	adminAddr common.Address,
	signer e_types.Signer,
	doConfirm bool,
) {
	bls.Init()
	fmt.Println("\n╔══════════════════════════════════════════════════════╗")
	fmt.Println("║  Register BLS for Existing Wallet                    ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Printf("  Wallet address: %s\n", userAddr.Hex())

	// Step 1: Lấy BLS pubkey từ node
	fmt.Println("  ⏳ Fetching BLS public key from node...")
	getPubKeyInput, _ := accountABI.Pack("getPublickeyBls")
	blsPubKeyResult, err := tcpClient.RpcEthCall(accountContract, getPubKeyInput)
	if err != nil {
		fmt.Printf("  ❌ getPublickeyBls RpcEthCall: %v\n", err)
		return
	}

	var blsPubKey []byte
	var hexStr string
	if err := json.Unmarshal(blsPubKeyResult, &hexStr); err == nil {
		hexStr = strings.TrimPrefix(hexStr, "0x")
		blsPubKey, err = hex.DecodeString(hexStr)
	} else {
		err = accountABI.UnpackIntoInterface(&blsPubKey, "getPublickeyBls", blsPubKeyResult)
	}
	if err != nil || len(blsPubKey) == 0 {
		fmt.Printf("  ❌ Failed to decode BLS pubkey: %v\n", err)
		return
	}
	fmt.Printf("  ✅ BLS PublicKey from node: 0x%s (%d bytes)\n", hex.EncodeToString(blsPubKey), len(blsPubKey))

	// Step 2: Gọi setBlsPublicKey từ ví của người dùng
	fmt.Println("  ⏳ Sending setBlsPublicKey transaction...")
	nonce, err := tcpClient.ChainGetNonce(userAddr)
	if err != nil {
		fmt.Printf("  ❌ GetNonce: %v\n", err)
		return
	}
	inputData, err := accountABI.Pack("setBlsPublicKey", blsPubKey)
	if err != nil {
		fmt.Printf("  ❌ Pack setBlsPublicKey: %v\n", err)
		return
	}
	tx := e_types.NewTransaction(nonce, accountContract, big.NewInt(0), 20000000, big.NewInt(10000000), inputData)
	signedTx, _ := e_types.SignTx(tx, signer, userPrivKey)
	rawTxBytes, _ := signedTx.MarshalBinary()
	txHash, err := tcpClient.RpcSendRawTransaction("0x" + hex.EncodeToString(rawTxBytes))
	if err != nil {
		fmt.Printf("  ❌ setBlsPublicKey send failed: %v\n", err)
		return
	}
	fmt.Printf("  ✅ setBlsPublicKey txHash: %s\n", txHash)

	// Step 3: (Optional) Admin confirm
	if doConfirm && adminPrivKey != nil {
		fmt.Printf("  ⏳ Admin %s confirming wallet %s...\n", adminAddr.Hex(), userAddr.Hex())
		txHash2, receipt2 := sendTxAndWaitRPC(
			tcpClient, adminPrivKey, adminAddr, accountContract,
			accountABI, signer,
			"confirmAccountWithoutSign", userAddr,
		)
		if receipt2 != nil {
			fmt.Printf("  ✅ confirmAccountWithoutSign OK: txHash=%s\n", txHash2)
		} else {
			fmt.Println("  ⚠️ confirmAccountWithoutSign — receipt not available (may be intercepted)")
		}
	} else {
		fmt.Println("  ℹ️  Bỏ qua bước confirmAccountWithoutSign (không có -admin-pk hoặc -no-confirm)")
	}

	fmt.Println("\n  ✅ Register BLS for existing wallet completed!")
	fmt.Printf("  ├─ Wallet:     %s\n", userAddr.Hex())
	fmt.Printf("  └─ BLS PubKey: 0x%s\n", hex.EncodeToString(blsPubKey))
}

// ===================== MAIN =====================

func main() {
	logger.SetConfig(&logger.LoggerConfig{
		Flag:    logger.FLAG_INFO,
		Outputs: []*os.File{os.Stdout},
	})

	configPath := flag.String("config", "config-test.json", "Path to TCP RPC client config")
	blsCount := flag.Int("count", 1, "Number of BLS keys to generate")
	outJson := flag.String("out", "bls_keys.json", "Output JSON file for generated BLS keys")
	// --- Flag cho chế độ đăng ký BLS từ ví có sẵn ---
	walletPk := flag.String("wallet-pk", "", "Private key (hex) của ví cần đăng ký BLS (nếu truyền, bỏ qua chế độ generate)")
	adminPk := flag.String("admin-pk", "", "Private key (hex) của admin để confirm (tuỳ chọn, chỉ dùng khi -wallet-pk được chỉ định)")
	noConfirm := flag.Bool("no-confirm", false, "Bỏ qua bước confirmAccountWithoutSign")
	flag.Parse()

	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║       TCP-RPC Test Suite: Register BLS               ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")

	cfgRaw, _ := tcp_config.LoadConfig(*configPath)
	cfg := cfgRaw.(*tcp_config.ClientConfig)

	// Khởi tạo RPC client (dùng cho gửi transaction, eth_call, subscribe, ...)
	tcpClient, err := client_tcp.NewClient(cfg)
	if err != nil {
		logger.Error("Failed to create TCP RPC client: %v", err)
		os.Exit(1)
	}
	time.Sleep(1 * time.Second)

	// Common setup (dùng private key trong config làm admin mặc định)
	ethPrivKey, _ := crypto.HexToECDSA(cfg.EthPrivateKey)
	fromAddr := crypto.PubkeyToAddress(ethPrivKey.PublicKey)
	chainIdBig := big.NewInt(int64(cfg.ChainId))
	signer := e_types.NewEIP155Signer(chainIdBig)

	fmt.Printf("\n  Admin address: %s\n", fromAddr.Hex())
	fmt.Printf("  Chain ID: %d\n", cfg.ChainId)

	accountABI, _ := abi.JSON(strings.NewReader(accountAbiJSON))
	accountContract := common.HexToAddress("0x00000000000000000000000000000000D844bb55")

	if *walletPk != "" {
		// Chế độ đăng ký BLS cho ví có sẵn
		userPrivKey, err := crypto.HexToECDSA(strings.TrimPrefix(*walletPk, "0x"))
		if err != nil {
			logger.Error("Invalid -wallet-pk: %v", err)
			os.Exit(1)
		}
		userAddr := crypto.PubkeyToAddress(userPrivKey.PublicKey)

		// Admin key: ưu tiên -admin-pk, fallback về key trong config
		var resolvedAdminPrivKey *ecdsa.PrivateKey
		var resolvedAdminAddr common.Address
		if *adminPk != "" {
			resolvedAdminPrivKey, err = crypto.HexToECDSA(strings.TrimPrefix(*adminPk, "0x"))
			if err != nil {
				logger.Error("Invalid -admin-pk: %v", err)
				os.Exit(1)
			}
			resolvedAdminAddr = crypto.PubkeyToAddress(resolvedAdminPrivKey.PublicKey)
		} else {
			resolvedAdminPrivKey = ethPrivKey
			resolvedAdminAddr = fromAddr
		}

		registerBlsForExistingWallet(
			tcpClient, accountABI, accountContract,
			userPrivKey, userAddr,
			resolvedAdminPrivKey, resolvedAdminAddr,
			signer, !*noConfirm,
		)
	} else {
		// Chế độ mặc định: tạo key mới và đăng ký
		testBlsRegistration(tcpClient, accountABI, accountContract, ethPrivKey, fromAddr, signer, *blsCount, *outJson)
	}

	fmt.Println("\n╔══════════════════════════════════════════════════════╗")
	fmt.Println("║       All tests completed!                           ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
}

// Account ABI (chỉ các function cần dùng)
const accountAbiJSON = `[
	{
		"anonymous": false,
		"inputs": [
			{"indexed": false, "internalType": "address", "name": "account", "type": "address"},
			{"indexed": false, "internalType": "uint256", "name": "time", "type": "uint256"},
			{"indexed": false, "internalType": "bytes", "name": "publicKey", "type": "bytes"},
			{"indexed": false, "internalType": "string", "name": "message", "type": "string"}
		],
		"name": "RegisterBls",
		"type": "event"
	},
	{
		"anonymous": false,
		"inputs": [
			{"indexed": false, "internalType": "address", "name": "account", "type": "address"},
			{"indexed": false, "internalType": "uint256", "name": "time", "type": "uint256"},
			{"indexed": false, "internalType": "string", "name": "message", "type": "string"}
		],
		"name": "AccountConfirmed",
		"type": "event"
	},
	{"inputs":[{"internalType":"bytes","name":"_publicKey","type":"bytes"}],"name":"setBlsPublicKey","outputs":[],"stateMutability":"nonpayable","type":"function"},
	{"inputs":[{"internalType":"address","name":"_account","type":"address"}],"name":"confirmAccountWithoutSign","outputs":[],"stateMutability":"nonpayable","type":"function"},
	{"inputs":[],"name":"getPublickeyBls","outputs":[{"internalType":"bytes","name":"","type":"bytes"}],"stateMutability":"view","type":"function"},
	{"inputs":[{"internalType":"address","name":"walletAddress","type":"address"}],"name":"addAuthorizedWallet","outputs":[],"stateMutability":"nonpayable","type":"function"},
	{"inputs":[{"internalType":"address","name":"walletAddress","type":"address"}],"name":"removeAuthorizedWallet","outputs":[],"stateMutability":"nonpayable","type":"function"},
	{"inputs":[{"internalType":"uint256","name":"page","type":"uint256"},{"internalType":"uint256","name":"pageSize","type":"uint256"}],"name":"getAllAuthorizedWallets","outputs":[],"stateMutability":"nonpayable","type":"function"},
	{"inputs":[{"internalType":"address","name":"adminAddress","type":"address"}],"name":"addAdmin","outputs":[],"stateMutability":"nonpayable","type":"function"},
	{"inputs":[{"internalType":"address","name":"adminAddress","type":"address"}],"name":"removeAdmin","outputs":[],"stateMutability":"nonpayable","type":"function"},
	{"inputs":[{"internalType":"uint256","name":"page","type":"uint256"},{"internalType":"uint256","name":"pageSize","type":"uint256"}],"name":"getAllAdmins","outputs":[],"stateMutability":"nonpayable","type":"function"},
	{"inputs":[{"internalType":"address","name":"contractAddress","type":"address"}],"name":"addContractFreeGas","outputs":[],"stateMutability":"nonpayable","type":"function"},
	{"inputs":[{"internalType":"address","name":"contractAddress","type":"address"}],"name":"removeContractFreeGas","outputs":[],"stateMutability":"nonpayable","type":"function"},
	{"inputs":[{"internalType":"address","name":"adder","type":"address"},{"internalType":"uint256","name":"page","type":"uint256"},{"internalType":"uint256","name":"pageSize","type":"uint256"}],"name":"getMyContracts","outputs":[],"stateMutability":"nonpayable","type":"function"}
]`
