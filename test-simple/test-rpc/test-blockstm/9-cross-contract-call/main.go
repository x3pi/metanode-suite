/*
 * BÀI TEST: 9-cross-contract-call
 * MÔ TẢ   : Contract A thực hiện gọi chéo sang Contract B (Cross-contract call).
 * GỌI     : Tx gọi Contract A, bên trong logic của Contract A tiếp tục gọi hàm của Contract B.
 * KỲ VỌNG : Read/Write set được ghi nhận đầy đủ cho cả Contract A và B. Nếu có conflict ở B thì toàn bộ chain call phải re-execute hợp lý.
 */
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

type Config struct {
	RPCUrl      string `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
	ChainID     int64    `json:"chain_id"`
	Contracts   map[string]struct {
		ABI      string `json:"abi"`
		Bytecode string `json:"bytecode"`
	} `json:"contracts"`
}

func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 9-cross-contract-call")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Contract A thực hiện gọi chéo sang Contract B (Cross-contract call).")
	fmt.Println("⚡ GỌI     : Tx gọi Contract A, bên trong logic của Contract A tiếp tục gọi hàm của Contract B.")
	fmt.Println("🎯 KỲ VỌNG : Read/Write set được ghi nhận đầy đủ cho cả Contract A và B. Nếu có conflict ở B thì toàn bộ chain call phải re-execute hợp lý.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

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

	if len(cfg.PrivateKeys) < 4 {
		log.Fatalf("❌ Cần ít nhất 4 private keys để test")
	}

	// 1. Load ABI & Bytecode for TargetContract and CallerContract
	parsedTargetABI, err := abi.JSON(strings.NewReader(cfg.Contracts["TargetContract"].ABI))
	if err != nil { log.Fatalf("ABI parse TargetContract err: %v", err) }
	bytecodeTarget, err := hexutil.Decode("0x" + cfg.Contracts["TargetContract"].Bytecode)
	if err != nil { log.Fatalf("Bytecode TargetContract err: %v", err) }

	parsedCallerABI, err := abi.JSON(strings.NewReader(cfg.Contracts["CallerContract"].ABI))
	if err != nil { log.Fatalf("ABI parse CallerContract err: %v", err) }
	bytecodeCaller, err := hexutil.Decode("0x" + cfg.Contracts["CallerContract"].Bytecode)
	if err != nil { log.Fatalf("Bytecode CallerContract err: %v", err) }

	pk0, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	// 2. Deploy TargetContract
	fmt.Println("🚀 Deploying TargetContract...")
	targetAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecodeTarget, nil)
	if err != nil {
		log.Fatalf("❌ Deploy TargetContract thất bại: %v", err)
	}
	fmt.Printf("📌 TargetContract deployed at: %s\n", targetAddr.Hex())

	// 3. Deploy CallerContract(targetAddr)
	fmt.Println("🚀 Deploying CallerContract...")
	// Pack constructor args
	constructorData, err := parsedCallerABI.Pack("", *targetAddr)
	if err != nil {
		log.Fatalf("❌ Pack constructor CallerContract thất bại: %v", err)
	}
	bytecodeCallerWithArgs := append(bytecodeCaller, constructorData...)

	callerAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecodeCallerWithArgs, nil)
	if err != nil {
		log.Fatalf("❌ Deploy CallerContract thất bại: %v", err)
	}
	fmt.Printf("📌 CallerContract deployed at: %s\n\n", callerAddr.Hex())

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	numTxs := len(cfg.PrivateKeys) - 1
	txHashes := make([]common.Hash, numTxs)

	fmt.Printf("🔥 Bắt đầu test Cross-Contract Calls: %d ví cùng gọi CallerContract.callTarget() trỏ về 1 TargetContract...\n", numTxs)
	start := time.Now()

	for i := 1; i <= numTxs; i++ {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()
			pk, _ := crypto.HexToECDSA(pKeyHex)
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))
			nonce, _ := client.PendingNonceAt(context.Background(), from)
			gasPrice, _ := client.SuggestGasPrice(context.Background())
			if gasPrice == nil { gasPrice = big.NewInt(1000000000) }

			// Call CallerContract.callTarget(idx * 100)
			callValue := big.NewInt(int64(idx * 100))
			data, _ := parsedCallerABI.Pack("callTarget", callValue)
			gasLimit := uint64(200000) // Cần nhiều gas hơn cho internal calls

			tx := types.NewTransaction(nonce, *callerAddr, big.NewInt(0), gasLimit, gasPrice, data)
			signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)

			fmt.Printf("⏳ Wallet %d đang gửi tx: CallerContract.callTarget(%s)...\n", idx, callValue.String())

			if err := client.SendTransaction(context.Background(), signedTx); err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("Wallet %d lỗi: %v", idx, err))
				errsMu.Unlock()
				return
			}
			txHashes[idx-1] = signedTx.Hash()
		}(i, cfg.PrivateKeys[i])
	}

	wg.Wait()

	if len(errs) > 0 {
		fmt.Println("❌ Một số giao dịch gửi thất bại:")
		for _, e := range errs {
			fmt.Println("  -", e)
		}
	}

	fmt.Println("⏳ Chờ các giao dịch được confirm...")
	successCount := 0
	revertCount := 0
	for i := 0; i < len(txHashes); i++ {
		hash := txHashes[i]
		if hash == (common.Hash{}) { continue }
		for {
			receipt, err := client.TransactionReceipt(context.Background(), hash)
			if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
				if receipt.Status != 1 {
					fmt.Printf("❌ Tx %s bị revert!\n", hash.Hex())
					revertCount++
				} else {
					fmt.Printf("✅ Tx %s confirmed trong block %d\n", hash.Hex()[:10]+"...", receipt.BlockNumber.Uint64())
					successCount++
				}
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ CROSS-CONTRACT CALLS (BLOCK-STM):")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Thành công: %d, Revert: %d\n", successCount, revertCount)

	// Lấy giá trị cuối cùng của TargetContract
	data, _ := parsedTargetABI.Pack("value")
	msg := ethereum.CallMsg{To: targetAddr, Data: data}
	resBytes, err := client.CallContract(context.Background(), msg, nil)
	var finalValue *big.Int
	if err != nil {
		fmt.Printf("❌ Lỗi đọc TargetContract.value: %v\n", err)
	} else {
		unpacked, _ := parsedTargetABI.Unpack("value", resBytes)
		finalValue = unpacked[0].(*big.Int)
		fmt.Printf("🔍 TargetContract.value cuối cùng = %s\n", finalValue.String())
	}

	expectedSum := int64(0)
	for i := 1; i <= numTxs; i++ {
		expectedSum += int64(i * 100)
	}

	// Nếu Block-STM xử lý chuẩn, tất cả phải success và giá trị phải khớp với tổng cộng dồn
	if successCount == numTxs && finalValue != nil && finalValue.Int64() == expectedSum {
		fmt.Printf("🎉 TEST PASSED: Block-STM xử lý mượt mà Cross-Contract Calls (Internal Calls). Storage cộng dồn chuẩn xác! Tổng = %d\n", expectedSum)
	} else {
		actualVal := "nil"
		if finalValue != nil {
			actualVal = finalValue.String()
		}
		fmt.Printf("⚠️ TEST FAILED: Block-STM xử lý sai! Kỳ vọng = %d, Thực tế = %s\n", expectedSum, actualVal)
		os.Exit(1)
	}
}

// Helper deploy (Hỗ trợ params)
func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte, constructorArgs []byte) (*common.Address, error) {
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil { return nil, err }
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil { return nil, err }
	if gasPrice == nil { gasPrice = big.NewInt(1000000000) }

	data := append(bytecode, constructorArgs...)
	tx := types.NewContractCreation(nonce, big.NewInt(0), 3000000, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil { return nil, err }

	err = client.SendTransaction(context.Background(), signedTx)
	if err != nil { return nil, err }

	for {
		receipt, err := client.TransactionReceipt(context.Background(), signedTx.Hash())
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			if receipt.Status == 1 {
				addr := crypto.CreateAddress(from, nonce)
				return &addr, nil
			}
			return nil, fmt.Errorf("transaction reverted")
		}
		time.Sleep(500 * time.Millisecond)
	}
}
