package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ─── CONFIG & SETUP ─────────────────────────────────────────────────────────

type Config struct {
	RPCUrl      string   `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
	ChainID     int64    `json:"chain_id"`
}

func main() {
	configPath := "../config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc config %s: %v", configPath, err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Fatalf("❌ Lỗi parse config: %v", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC: %v", err)
	}

	// Đọc ABI và BIN
	abiBytes, err := os.ReadFile("../contracts/BlockSTMTests_sol_AMMSimulator.abi")
	if err != nil {
		log.Fatalf("❌ Không tìm thấy BlockSTMTests_sol_AMMSimulator.abi. Hãy compile BlockSTMTests.sol bằng solc.")
	}
	parsedABI, err := abi.JSON(strings.NewReader(string(abiBytes)))
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	binBytes, err := os.ReadFile("../contracts/BlockSTMTests_sol_AMMSimulator.bin")
	if err != nil {
		log.Fatalf("❌ Không tìm thấy BlockSTMTests_sol_AMMSimulator.bin. Hãy compile BlockSTMTests.sol bằng solc.")
	}
	bytecode, err := hexutil.Decode("0x" + strings.TrimSpace(string(binBytes)))
	if err != nil {
		log.Fatalf("❌ Lỗi decode bytecode hex: %v", err)
	}

	pk0, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying AMMSimulator Contract...")
	contractAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		log.Fatalf("❌ Deploy thất bại: %v", err)
	}
	fmt.Printf("📌 AMM Contract: %s\n\n", contractAddr.Hex())

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	fmt.Printf("🔥 Gửi %d lệnh SWAP song song (High Contention AMM)...\n", len(cfg.PrivateKeys))
	start := time.Now()

	txHashes := make([]common.Hash, len(cfg.PrivateKeys))

	// Mỗi user swap 1000 token A lấy token B
	amountIn := big.NewInt(1000)

	for i, pkStr := range cfg.PrivateKeys {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()
			pk, _ := crypto.HexToECDSA(pKeyHex)
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

			data, _ := parsedABI.Pack("swapAToB", amountIn)
			hash, err := sendTx(client, pk, cfg.ChainID, from, contractAddr, data)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi ví %d: %v", idx, err))
				errsMu.Unlock()
				return
			}
			txHashes[idx] = hash
		}(i, pkStr)
	}

	wg.Wait()
	if len(errs) > 0 {
		fmt.Println("❌ Một số giao dịch gửi thất bại:")
		for _, e := range errs {
			fmt.Println("  -", e)
		}
	}

	fmt.Println("⏳ Chờ các lệnh SWAP confirm...")
	for i, hash := range txHashes {
		if hash == (common.Hash{}) { continue }
		receipt, _ := waitReceipt(client, hash)
		if receipt.Status != 1 {
			fmt.Printf("❌ Wallet %d SWAP REVERTED!\n", i)
		} else {
			fmt.Printf("✅ Wallet %d SWAP %s confirmed\n", i, hash.Hex()[:10]+"...")
		}
	}

	// Đọc lại reserve
	resA, _ := getUint256(client, contractAddr, parsedABI, "reserveA")
	resB, _ := getUint256(client, contractAddr, parsedABI, "reserveB")

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ AMM SWAP (Block-STM):")
	fmt.Printf("Thời gian chạy: %v\n", elapsed)
	fmt.Printf("Reserve A cuối: %s\n", resA.String())
	fmt.Printf("Reserve B cuối: %s\n", resB.String())
	
	// Tính k = reserveA * reserveB ban đầu (1M * 1M)
	// Lúc đầu: A = 1,000,000 ; B = 1,000,000
	// Sau n lệnh swap, nếu Block-STM tính đúng, công thức k của AMM phải được bảo toàn gần nhất có thể
	fmt.Println("🎉 TEST FINISHED! Nếu Reserve thay đổi chính xác, Block-STM đã sắp xếp lệnh song song thành công.")
}

// ─── HELPERS ─────────────────────────────────────────────────────────────────
func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	nonce, _ := client.PendingNonceAt(context.Background(), from)
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil { gasPrice = big.NewInt(1e9) }
	
	tx := types.NewContractCreation(nonce, big.NewInt(0), 5000000, gasPrice, bytecode)
	signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err := client.SendTransaction(context.Background(), signedTx); err != nil {
		return nil, err
	}
	receipt, _ := waitReceipt(client, signedTx.Hash())
	if receipt.Status != 1 { return nil, fmt.Errorf("deploy reverted") }
	return &receipt.ContractAddress, nil
}

func sendTx(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, data []byte) (common.Hash, error) {
	nonce, _ := client.PendingNonceAt(context.Background(), from)
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil { gasPrice = big.NewInt(1e9) }
	
	tx := types.NewTransaction(nonce, *to, big.NewInt(0), 1000000, gasPrice, data)
	signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err := client.SendTransaction(context.Background(), signedTx); err != nil {
		return common.Hash{}, err
	}
	return signedTx.Hash(), nil
}

func waitReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	for {
		receipt, err := client.TransactionReceipt(context.Background(), txHash)
		if err == nil { return receipt, nil }
		time.Sleep(300 * time.Millisecond)
	}
}

func getUint256(client *ethclient.Client, addr *common.Address, parsedABI abi.ABI, method string) (*big.Int, error) {
	data, _ := parsedABI.Pack(method)
	result, err := client.CallContract(context.Background(), ethereum.CallMsg{To: addr, Data: data}, nil)
	if err != nil { return nil, err }
	outputs, _ := parsedABI.Unpack(method, result)
	return outputs[0].(*big.Int), nil
}
