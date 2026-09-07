/*
 * BÀI TEST: 3-amm-dex
 * MÔ TẢ   : Mô phỏng swap token trên một AMM DEX với sự tranh chấp cao ở Pool Reserve.
 * GỌI     : Nhiều user gọi hàm swap token cùng lúc làm thay đổi reserve của Pool.
 * KỲ VỌNG : Block-STM xử lý mượt mà các conflict trên biến reserve, đảm bảo số dư token sau swap tuân thủ đúng công thức Constant Product.
 */
package main

import (
	"tool-test/test-simple/test-rpc/test-chain/config"
	"context"
	"crypto/ecdsa"
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

func RunTest(configPath string) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 3-amm-dex")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Mô phỏng swap token trên một AMM DEX với sự tranh chấp cao ở Pool Reserve.")
	fmt.Println("⚡ GỌI     : Nhiều user gọi hàm swap token cùng lúc làm thay đổi reserve của Pool.")
	fmt.Println("🎯 KỲ VỌNG : Block-STM xử lý mượt mà các conflict trên biến reserve, đảm bảo số dư token sau swap tuân thủ đúng công thức Constant Product.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	if configPath == "" {
		configPath = "../config.json"
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("lỗi load config: %w", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		return fmt.Errorf("lỗi kết nối RPC: %w", err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(cfg.Contracts["AMMSimulator"].ABI))
	if err != nil {
		return fmt.Errorf("lỗi parse ABI: %w", err)
	}

	bytecode, err := hexutil.Decode("0x" + cfg.Contracts["AMMSimulator"].Bytecode)
	if err != nil {
		return fmt.Errorf("lỗi decode bytecode hex: %w", err)
	}

	if len(cfg.PrivateKeys) == 0 {
		return fmt.Errorf("không có private key nào trong config")
	}

	pk0, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("invalid private key[0]: %w", err)
	}
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying AMMSimulator Contract...")
	contractAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		return fmt.Errorf("deploy contract err: %w", err)
	}
	fmt.Printf("📌 AMM Contract: %s\n\n", contractAddr.Hex())

	initialResA, err := getUint256(client, contractAddr, parsedABI, "reserveA")
	if err != nil {
		return fmt.Errorf("lỗi đọc reserveA ban đầu: %w", err)
	}

	var wg sync.WaitGroup
	fmt.Printf("🔥 Gửi %d lệnh SWAP song song (High Contention AMM)...\n", len(cfg.PrivateKeys))

	txHashes := make([]common.Hash, len(cfg.PrivateKeys))
	start := time.Now()
	var sendErr error
	var errMu sync.Mutex

	for i, pkStr := range cfg.PrivateKeys {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()
			pk, err := crypto.HexToECDSA(pKeyHex)
			if err != nil {
				errMu.Lock()
				sendErr = err
				errMu.Unlock()
				return
			}
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

			amountIn := big.NewInt(1000)
			data, err := parsedABI.Pack("swapAToB", amountIn)
			if err != nil {
				errMu.Lock()
				sendErr = err
				errMu.Unlock()
				return
			}

			hash, err := sendTx(client, pk, cfg.ChainID, from, contractAddr, data)
			if err == nil {
				txHashes[idx] = hash
			} else {
				errMu.Lock()
				sendErr = err
				errMu.Unlock()
			}
		}(i, pkStr)
	}

	wg.Wait()
	if sendErr != nil {
		return fmt.Errorf("lỗi gửi giao dịch swap: %w", sendErr)
	}

	fmt.Println("⏳ Chờ các lệnh SWAP confirm...")
	for i, hash := range txHashes {
		if hash == (common.Hash{}) {
			continue
		}
		_, err := waitReceipt(client, hash)
		if err != nil {
			return fmt.Errorf("lỗi chờ receipt swap tx %s: %w", hash.Hex(), err)
		}
		fmt.Printf("✅ Wallet %d SWAP %s... confirmed\n", i, hash.Hex()[:10])
	}
	elapsed := time.Since(start)

	resA, err := getUint256(client, contractAddr, parsedABI, "reserveA")
	if err != nil {
		return fmt.Errorf("lỗi đọc reserveA cuối: %w", err)
	}
	resB, err := getUint256(client, contractAddr, parsedABI, "reserveB")
	if err != nil {
		return fmt.Errorf("lỗi đọc reserveB cuối: %w", err)
	}

	fmt.Println("\n📊 KẾT QUẢ AMM SWAP (Block-STM):")
	fmt.Printf("Thời gian chạy: %v\n", elapsed)
	fmt.Printf("Reserve A cuối: %s\n", resA.String())
	fmt.Printf("Reserve B cuối: %s\n", resB.String())

	totalInput := big.NewInt(int64(1000 * len(cfg.PrivateKeys)))
	expectedResA := new(big.Int).Add(initialResA, totalInput)

	fmt.Printf("👉 Reserve A kỳ vọng: %s\n", expectedResA.String())

	if resA.Cmp(expectedResA) != 0 {
		return fmt.Errorf("TEST FAILED: Block-STM bị lỗi! Các lệnh Swap đã đọc chung một dữ liệu cũ (Stale Read) và lưu đè lên nhau, làm thất thoát tiền trong Pool")
	}

	fmt.Println("🎉 KẾT QUẢ ĐÚNG: Nếu Reserve thay đổi chính xác, Block-STM đã sắp xếp lệnh song song thành công và tránh được xung đột dữ liệu.")
	return nil
}

func main() {
	configPath := "../config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	if err := RunTest(configPath); err != nil {
		log.Fatalf("❌ %v", err)
	}
}

// ─── HELPERS ─────────────────────────────────────────────────────────────────
func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return nil, err
	}
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil {
		gasPrice = big.NewInt(1e9)
	}

	tx := types.NewContractCreation(nonce, big.NewInt(0), 5000000, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return nil, err
	}
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

func sendTx(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, data []byte) (common.Hash, error) {
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return common.Hash{}, err
	}
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil {
		gasPrice = big.NewInt(1e9)
	}

	tx := types.NewTransaction(nonce, *to, big.NewInt(0), 1000000, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return common.Hash{}, err
	}
	if err := client.SendTransaction(context.Background(), signedTx); err != nil {
		return common.Hash{}, err
	}
	return signedTx.Hash(), nil
}

func waitReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	timeoutStart := time.Now()
	for {
		if time.Since(timeoutStart) > 60*time.Second {
			return nil, fmt.Errorf("timeout waiting for receipt: %s", txHash.Hex())
		}
		receipt, err := client.TransactionReceipt(context.Background(), txHash)
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			return receipt, nil
		}
		if err != nil && !strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("lỗi kết nối RPC: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func getUint256(client *ethclient.Client, addr *common.Address, parsedABI abi.ABI, method string) (*big.Int, error) {
	data, _ := parsedABI.Pack(method)
	result, err := client.CallContract(context.Background(), ethereum.CallMsg{To: addr, Data: data}, nil)
	if err != nil { return nil, err }
	outputs, _ := parsedABI.Unpack(method, result)
	return outputs[0].(*big.Int), nil
}
