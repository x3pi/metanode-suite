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

// Demo contract bytecode (from test-simple/test-tcp/caller-tcp/data-demo-test.json)
const demoContractBytecode = "0x6080604052348015600f57600080fd5b506103ef8061001f6000396000f3fe608060405234801561001057600080fd5b506004361061004c5760003560e01c8063160ef80714610051578063209652551461006d5780633fa4f2451461008b57806355241077146100a9575b600080fd5b61006b60048036038101906100669190610222565b6100c5565b005b610075610175565b604051610082919061025e565b60405180910390f35b61009361017e565b6040516100a0919061025e565b60405180910390f35b6100c360048036038101906100be9190610222565b610184565b005b60008111610108576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016100ff906102d6565b60405180910390fd5b806000808282546101199190610325565b925050819055503373ffffffffffffffffffffffffffffffffffffffff167f4b3fd1f53a9fe860fd8411e7f19ff4c49f28111e397dfc5e5db3bd9e99e24eb28260005460405161016a929190610359565b60405180910390a250565b60008054905090565b60005481565b600080549050816000819055503373ffffffffffffffffffffffffffffffffffffffff167fb485dddf4584d4931927aeccc95476c87e02c5b136fb4fbbac317b823d9a522c8284426040516101db93929190610382565b60405180910390a25050565b600080fd5b6000819050919050565b6101ff816101ec565b811461020a57600080fd5b50565b60008135905061021c816101f6565b92915050565b600060208284031215610238576102376101e7565b5b60006102468482850161020d565b91505092915050565b610258816101ec565b82525050565b6000602082019050610273600083018461024f565b92915050565b600082825260208201905092915050565b7f416d6f756e74206d7573742062652067726561746572207468616e2030000000600082015250565b60006102c0601d83610279565b91506102cb8261028a565b602082019050919050565b600060208201905081810360008301526102ef816102b3565b9050919050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601160045260246000fd5b6000610330826101ec565b915061033b836101ec565b9250828201905080821115610353576103526102f6565b5b92915050565b600060408201905061036e600083018561024f565b61037b602083018461024f565b9392505050565b6000606082019050610397600083018661024f565b6103a4602083018561024f565b6103b1604083018461024f565b94935050505056fea2646970667358221220ebe59c84eca78988aae26388c7d56a0df5f270358c2ad6dbc3013cab5c5790c164736f6c63430008230033"

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

// deployContract deploy contract mới qua TCP port 9545
func deployContract(
	tcpClient *client_tcp.Client,
	privKey *ecdsa.PrivateKey,
	fromAddr common.Address,
	signer e_types.Signer,
	bytecodeHex string,
) (common.Address, error) {
	fmt.Println("\n╔══════════════════════════════════════════════════════╗")
	fmt.Println("║  DEPLOY: Deploying Demo Contract via TCP...          ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")

	cleanHex := strings.TrimPrefix(bytecodeHex, "0x")
	bytecode, err := hex.DecodeString(cleanHex)
	if err != nil {
		return common.Address{}, fmt.Errorf("invalid bytecode hex: %w", err)
	}

	nonce, err := tcpClient.RpcGetPendingNonce(fromAddr)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to get nonce: %w", err)
	}
	fmt.Printf("  📝 Deploy Nonce: %d\n", nonce)

	tx := e_types.NewContractCreation(nonce, big.NewInt(0), 20000000, big.NewInt(10000000), bytecode)
	signedTx, err := e_types.SignTx(tx, signer, privKey)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to sign deploy tx: %w", err)
	}

	rawTxBytes, _ := signedTx.MarshalBinary()
	rawTxHex := "0x" + hex.EncodeToString(rawTxBytes)

	txHash, receipt, err := tcpClient.RpcSendRawTransactionReceipt(rawTxHex)
	if err != nil {
		return common.Address{}, fmt.Errorf("deploy error: %w", err)
	}
	fmt.Printf("  ✅ Deploy txHash: %s\n", txHash)

	if receipt != nil {
		fmt.Printf("  ✅ Deploy Receipt: status=%s, gasUsed=%d\n",
			receipt.Status.String(), receipt.GasUsed)
	} else {
		rpcRcpt := waitReceipt(tcpClient, txHash)
		if rpcRcpt == nil {
			return common.Address{}, fmt.Errorf("timeout waiting for deploy receipt")
		}
		fmt.Printf("  ✅ Deploy Receipt (poll): status=%s, gasUsed=%s\n",
			rpcRcpt.Status, rpcRcpt.GasUsed)
	}

	deployedAddr := crypto.CreateAddress(fromAddr, nonce)
	fmt.Printf("  🎉 Deployed Contract Address (CREATE): %s\n", deployedAddr.Hex())
	return deployedAddr, nil
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
	fmt.Printf("║  TEST: Demo Contract (%s) \n", contractAddr.Hex())
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
	forceDeploy := flag.Bool("deploy", false, "Force deploy a new demo contract even if address is configured")
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

	// 1. Xác định Contract Address: Tự động deploy nếu chưa cấu hình hoặc truyền flag -deploy
	var contractAddr common.Address
	if cfg.DemoContractAddress != "" && !*forceDeploy {
		contractAddr = common.HexToAddress(cfg.DemoContractAddress)
		fmt.Printf("\n  📌 Dùng contract có sẵn từ config: %s\n", contractAddr.Hex())
	} else {
		// Deploy contract mới qua TCP
		contractAddr, err = deployContract(tcpClient, ethPrivKey, fromAddr, signer, demoContractBytecode)
		if err != nil {
			logger.Error("❌ Deploy contract thất bại: %v", err)
			os.Exit(1)
		}
	}

	// 2. Chạy test giao dịch với contract vừa xác định/deploy
	testDemoContract(tcpClient, demoABI, contractAddr, ethPrivKey, fromAddr, signer)

	fmt.Println("\n╔══════════════════════════════════════════════════════╗")
	fmt.Println("║       Demo transaction test completed!               ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
}
