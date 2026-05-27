package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ABI definitions
const counterABIJSON = `[
  {"inputs":[],"name":"increment","outputs":[],"stateMutability":"nonpayable","type":"function"},
  {"inputs":[],"name":"getCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},
  {"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"newCount","type":"uint256"}],"name":"Incremented","type":"event"}
]`

type Config struct {
	RPCUrl     string `json:"rpc_url"`
	PrivateKey string `json:"private_key"`
	ChainID    int64  `json:"chain_id"`
}

func main() {
	// Load config
	configPath := "../config-local.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Bytecode path (optional 2nd arg)
	bytecodePath := "TestCounter.bin"
	if len(os.Args) > 2 {
		bytecodePath = os.Args[2]
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("❌ Không đọc được config %s: %v", configPath, err)
	}
	var cfg Config
	json.Unmarshal(raw, &cfg)

	// Connect RPC
	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC: %v", err)
	}

	// Load private key
	privateKey, err := crypto.HexToECDSA(cfg.PrivateKey)
	if err != nil {
		log.Fatalf("❌ Lỗi parse private key: %v", err)
	}
	fromAddress := crypto.PubkeyToAddress(*privateKey.Public().(*ecdsa.PublicKey))

	// Parse ABI
	parsedABI, err := abi.JSON(strings.NewReader(counterABIJSON))
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	// ── DEPLOY ─────────────────────────────────────────────────────────────
	if _, serr := os.Stat(bytecodePath); os.IsNotExist(serr) {
		log.Fatalf("❌ Không tìm thấy file bytecode tại %s\n"+
			"   Hãy biên dịch contract bằng lệnh:\n"+
			"   solc --bin --abi ../../contract/test-counter.sol -o . --overwrite", bytecodePath)
	}

	hexBytes, err := os.ReadFile(bytecodePath)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc bytecode: %v", err)
	}
	hexStr := strings.TrimSpace(string(hexBytes))
	if !strings.HasPrefix(hexStr, "0x") {
		hexStr = "0x" + hexStr
	}
	bytecode, err := hexutil.Decode(hexStr)
	if err != nil {
		log.Fatalf("❌ Lỗi decode bytecode hex: %v", err)
	}

	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║   🚀  TestCounter — Sequential Verifier     ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Printf("🔗 RPC: %s  |  Chain: %d\n", cfg.RPCUrl, cfg.ChainID)
	fmt.Printf("👤 From: %s\n\n", fromAddress.Hex())

	contractAddr, err := deployContract(client, privateKey, cfg.ChainID, fromAddress, bytecode)
	if err != nil {
		log.Fatalf("❌ Deploy thất bại: %v", err)
	}
	fmt.Printf("📌 Contract: %s\n", contractAddr.Hex())
	fmt.Println("🔁 Bắt đầu spam increment → verify tuần tự... (Ctrl+C để dừng)\n")

	// ── Ctrl+C handler ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// ── LOOP ────────────────────────────────────────────────────────────────
	expected := uint64(0)
	round := 0

	for {
		select {
		case <-quit:
			fmt.Printf("\n🛑 Dừng! Tổng vòng thành công: %d\n", round)
			os.Exit(0)
		default:
		}

		round++
		expected++

		// Step 1: Gọi increment()
		txHash, err := sendIncrement(client, privateKey, cfg.ChainID, fromAddress, contractAddr, parsedABI)
		if err != nil {
			fmt.Printf("❌ [Round %d] increment() FAILED: %v\n", round, err)
			os.Exit(1)
		}

		// Step 2: Đợi receipt confirm
		receipt, err := waitReceipt(client, txHash)
		if err != nil {
			fmt.Printf("❌ [Round %d] Lỗi chờ receipt: %v\n", round, err)
			os.Exit(1)
		}
		if receipt.Status != 1 {
			fmt.Printf("❌ [Round %d] Tx REVERTED! Dừng test.\n", round)
			os.Exit(1)
		}

		// Step 3: getCount() — đọc giá trị thực tế
		actual, err := getCount(client, contractAddr, parsedABI)
		if err != nil {
			fmt.Printf("❌ [Round %d] getCount() FAILED: %v\n", round, err)
			os.Exit(1)
		}

		// Step 4: Verify tuần tự
		if actual != expected {
			fmt.Printf("\n🚨🚨🚨 SEQUENTIAL ERROR tại Round %d!\n", round)
			fmt.Printf("   Expected = %d\n", expected)
			fmt.Printf("   Got      = %d\n", actual)
			fmt.Printf("   Sai lệch = %d\n", int64(actual)-int64(expected))
			os.Exit(1)
		}

		shortHash := txHash.Hex()[:10] + "..."
		fmt.Printf("✅ [Round %5d] increment → count=%d  (gas=%d, tx=%s)\n",
			round, actual, receipt.GasUsed, shortHash)
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	nonce, _ := client.PendingNonceAt(context.Background(), from)
	gasPrice, _ := client.SuggestGasPrice(context.Background())

	gasLimit, err := client.EstimateGas(context.Background(), ethereum.CallMsg{From: from, Data: bytecode})
	if err != nil {
		gasLimit = 5_000_000
	} else {
		gasLimit += 50_000
	}

	tx := types.NewContractCreation(nonce, big.NewInt(0), gasLimit, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return nil, err
	}

	fmt.Printf("⚡ Deploying... (bytecode: %d bytes, gas: %d)\n", len(bytecode), gasLimit)
	if err := client.SendTransaction(context.Background(), signedTx); err != nil {
		return nil, err
	}

	receipt, err := waitReceipt(client, signedTx.Hash())
	if err != nil {
		return nil, err
	}
	if receipt.Status != 1 {
		return nil, fmt.Errorf("deploy reverted")
	}
	return &receipt.ContractAddress, nil
}

func sendIncrement(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI) (common.Hash, error) {
	data, err := parsedABI.Pack("increment")
	if err != nil {
		return common.Hash{}, err
	}

	nonce, _ := client.PendingNonceAt(context.Background(), from)
	gasPrice, _ := client.SuggestGasPrice(context.Background())

	gasLimit, err := client.EstimateGas(context.Background(), ethereum.CallMsg{From: from, To: to, GasPrice: gasPrice, Data: data})
	if err != nil {
		gasLimit = 100_000
	} else {
		gasLimit += 10_000
	}

	tx := types.NewTransaction(nonce, *to, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return common.Hash{}, err
	}

	if err := client.SendTransaction(context.Background(), signedTx); err != nil {
		return common.Hash{}, err
	}
	return signedTx.Hash(), nil
}

func getCount(client *ethclient.Client, addr *common.Address, parsedABI abi.ABI) (uint64, error) {
	data, _ := parsedABI.Pack("getCount")
	result, err := client.CallContract(context.Background(), ethereum.CallMsg{To: addr, Data: data}, nil)
	if err != nil {
		return 0, err
	}
	outputs, err := parsedABI.Unpack("getCount", result)
	if err != nil {
		return 0, err
	}
	if len(outputs) == 0 {
		return 0, fmt.Errorf("output rỗng")
	}
	val, ok := outputs[0].(*big.Int)
	if !ok {
		return 0, fmt.Errorf("kiểu trả về không phải *big.Int")
	}
	return val.Uint64(), nil
}

func waitReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	for {
		receipt, err := client.TransactionReceipt(context.Background(), txHash)
		if err == nil {
			return receipt, nil
		}
		if err != ethereum.NotFound {
			return nil, err
		}
		time.Sleep(300 * time.Millisecond)
	}
}
