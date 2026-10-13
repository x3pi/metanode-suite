package main

import (
	"crypto/ecdsa"
	"encoding/hex"
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
	"tool-test/pkg/logger"
	pb "tool-test/pkg/proto"
	"google.golang.org/protobuf/proto"
)

// ===================== HELPERS =====================

// waitReceipt đợi receipt với timeout 30s, retry 10ms
func waitReceipt(tcpClient *client_tcp.Client, txHash string) *pb.RpcReceipt {
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
			fmt.Printf("  ✅ Receipt: status=%s, gasUsed=%s, logs=%d\n",
				receipt.Status, receipt.GasUsed, len(receipt.Logs))
			return receipt
		}
		select {
		case <-timer.C:
			fmt.Println("  ⚠️ Timeout waiting for receipt")
			return nil
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// sendTxAndWait tạo, ký, gửi transaction + nhận receipt trực tiếp
func sendTxAndWait(
	tcpClient *client_tcp.Client,
	privKey *ecdsa.PrivateKey,
	fromAddr common.Address,
	toAddr common.Address,
	parsedABI abi.ABI,
	signer e_types.Signer,
	method string,
	args ...interface{},
) (string, *pb.Receipt) {
	nonce, err := tcpClient.RpcGetPendingNonce(fromAddr)
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

	txHash, receipt, err := tcpClient.RpcSendRawTransactionReceipt(rawTxHex)
	if err != nil {
		fmt.Printf("  ❌ SendTx %s: %v\n", method, err)
		return "", nil
	}
	fmt.Printf("  ✅ txHash: %s\n", txHash)

	if receipt != nil {
		fmt.Printf("  ✅ Receipt: status=%s, gasUsed=%d, logs=%d\n",
			receipt.Status.String(), receipt.GasUsed, len(receipt.EventLogs))
		return txHash, receipt
	}

	rpcRcpt := waitReceipt(tcpClient, txHash)
	if rpcRcpt != nil {
		return txHash, &pb.Receipt{
			TransactionHash: common.HexToHash(rpcRcpt.TransactionHash).Bytes(),
		}
	}
	return txHash, nil
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

// ===================== TEST DEMO =====================

// testDemoContract test các function gửi nhận giao dịch: setValue, increaseValue, subscribe event, getValue
func testDemoContract(
	tcpClient *client_tcp.Client,
	demoABI abi.ABI,
	contractAddr common.Address,
	privKey *ecdsa.PrivateKey,
	fromAddr common.Address,
	signer e_types.Signer,
) {
	fmt.Println("\n╔══════════════════════════════════════════════════════╗")
	fmt.Println("║  TEST: Demo Contract (setValue + increaseValue)      ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")

	// 1. Read current value
	fmt.Println("\n─── getValue (trước) ───")
	getValueData, _ := demoABI.Pack("getValue")
	rcpt1, err := tcpClient.RpcEthCallReceipt(contractAddr, getValueData)
	if err != nil {
		fmt.Printf("  ❌ %v\n", err)
		return
	}
	fmt.Printf("  ✅ eth_call Receipt: status=%s, gasUsed=%d, returnLen=%d\n",
		rcpt1.Status.String(), rcpt1.GasUsed, len(rcpt1.Return))
	results, err := demoABI.Unpack("getValue", rcpt1.Return)
	if err != nil {
		fmt.Printf("  ❌ Unpack getValue: %v\n", err)
		return
	}
	oldValue := results[0].(*big.Int)
	fmt.Printf("  ✅ getValue() = %s\n", oldValue.String())

	// 2. Subscribe 2 events
	fmt.Println("\n─── Subscribe ValueChanged + ValueIncreased ───")
	var wgChanged, wgIncreased sync.WaitGroup
	var onceChanged, onceIncreased sync.Once
	wgChanged.Add(1)
	wgIncreased.Add(1)

	sub1, _ := tcpClient.RpcSubscribe(
		[]string{contractAddr.Hex()},
		[]string{demoABI.Events["ValueChanged"].ID.Hex()},
		makeEventHandler("ValueChanged", &wgChanged, &onceChanged),
	)
	fmt.Printf("  ✅ ValueChanged subID=%s\n", sub1)

	sub2, _ := tcpClient.RpcSubscribe(
		[]string{contractAddr.Hex()},
		[]string{demoABI.Events["ValueIncreased"].ID.Hex()},
		makeEventHandler("ValueIncreased", &wgIncreased, &onceIncreased),
	)
	fmt.Printf("  ✅ ValueIncreased subID=%s\n", sub2)

	// 3. setValue(789) + wait receipt
	fmt.Println("\n─── setValue(789) ───")
	sendTxAndWait(tcpClient, privKey, fromAddr, contractAddr, demoABI, signer, "setValue", big.NewInt(789))

	// 4. increaseValue(100) + wait receipt
	fmt.Println("\n─── increaseValue(100) ───")
	sendTxAndWait(tcpClient, privKey, fromAddr, contractAddr, demoABI, signer, "increaseValue", big.NewInt(100))

	// 5. Wait events
	fmt.Println("\n  ⏳ Waiting for both events (max 10s)...")
	allDone := make(chan struct{})
	go func() {
		wgChanged.Wait()
		wgIncreased.Wait()
		close(allDone)
	}()
	select {
	case <-allDone:
		fmt.Println("  ✅ Both events received!")
	case <-time.After(10 * time.Second):
		fmt.Println("  ⚠️ Timeout waiting for events")
	}

	// 6. Unsubscribe
	tcpClient.RpcUnsubscribe(sub1)
	tcpClient.RpcUnsubscribe(sub2)

	// 7. Verify value
	fmt.Println("\n─── getValue (sau) ───")
	rcpt2, err := tcpClient.RpcEthCallReceipt(contractAddr, getValueData)
	if err != nil {
		fmt.Printf("  ❌ %v\n", err)
		return
	}
	fmt.Printf("  ✅ eth_call Receipt: status=%s, gasUsed=%d, returnLen=%d\n",
		rcpt2.Status.String(), rcpt2.GasUsed, len(rcpt2.Return))
	results2, err := demoABI.Unpack("getValue", rcpt2.Return)
	if err != nil {
		fmt.Printf("  ❌ Unpack getValue: %v\n", err)
		return
	}
	newValue := results2[0].(*big.Int)
	fmt.Printf("  ✅ getValue() = %s", newValue.String())
	if newValue.Int64() == 889 {
		fmt.Println(" ✅ (789 + 100 = 889)")
	} else {
		fmt.Println()
	}
}

// ===================== MAIN =====================

func main() {
	logger.SetConfig(&logger.LoggerConfig{
		Flag:    logger.FLAG_INFO,
		Outputs: []*os.File{os.Stdout},
	})

	configPath := flag.String("config", "config-test.json", "Path to TCP client config")
	flag.Parse()

	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║       TCP-RPC Demo Transaction Test (Port 9545)     ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")

	cfgRaw, err := tcp_config.LoadConfig(*configPath)
	if err != nil {
		logger.Error("Failed to load config from %s: %v", *configPath, err)
		os.Exit(1)
	}
	cfg := cfgRaw.(*tcp_config.ClientConfig)

	// Khởi tạo client
	tcpClient, err := client_tcp.NewClient(cfg)
	if err != nil {
		logger.Error("Failed to create TCP client: %v", err)
		os.Exit(1)
	}
	time.Sleep(1 * time.Second)

	// Common setup
	ethPrivKey, err := crypto.HexToECDSA(cfg.EthPrivateKey)
	if err != nil {
		logger.Error("Invalid eth private key: %v", err)
		os.Exit(1)
	}
	fromAddr := crypto.PubkeyToAddress(ethPrivKey.PublicKey)
	chainIdBig := big.NewInt(int64(cfg.ChainId))
	signer := e_types.NewEIP155Signer(chainIdBig)

	fmt.Printf("\n  Admin address: %s\n", fromAddr.Hex())
	fmt.Printf("  Chain ID: %d\n", cfg.ChainId)

	// === Test 0: Basic RPC ===
	fmt.Println("\n─── Basic: RpcNetVersion + RpcGetChainId ───")
	netVer, _ := tcpClient.RpcNetVersion()
	chainId, _ := tcpClient.RpcGetChainId()
	fmt.Printf("  ✅ NetVersion: %s, ChainId: %s\n", netVer, chainId)

	// Load demo ABI
	demoAbiBytes, err := os.ReadFile(cfg.DemoAbiPath_)
	if err != nil {
		logger.Error("Failed to read demo ABI from %s: %v", cfg.DemoAbiPath_, err)
		os.Exit(1)
	}
	demoABI, err := abi.JSON(strings.NewReader(string(demoAbiBytes)))
	if err != nil {
		logger.Error("Failed to parse demo ABI: %v", err)
		os.Exit(1)
	}
	contractAddr := common.HexToAddress(cfg.DemoContractAddress)

	// Run demo transaction test
	testDemoContract(tcpClient, demoABI, contractAddr, ethPrivKey, fromAddr, signer)

	fmt.Println("\n╔══════════════════════════════════════════════════════╗")
	fmt.Println("║       Demo transaction test completed!               ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
}
