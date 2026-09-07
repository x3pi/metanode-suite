/*
 * BÀI TEST: 9-cross-contract-call
 * MÔ TẢ   : Contract A thực hiện gọi chéo sang Contract B (Cross-contract call).
 * GỌI     : Tx gọi Contract A, bên trong logic của Contract A tiếp tục gọi hàm của Contract B.
 * KỲ VỌNG : Read/Write set được ghi nhận đầy đủ cho cả Contract A và B. Nếu có conflict ở B thì toàn bộ chain call phải re-execute hợp lý.
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

func RunTest(configPath string) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 9-cross-contract-call")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Contract A thực hiện gọi chéo sang Contract B (Cross-contract call).")
	fmt.Println("⚡ GỌI     : Tx gọi Contract A, bên trong logic của Contract A tiếp tục gọi hàm của Contract B.")
	fmt.Println("🎯 KỲ VỌNG : Read/Write set được ghi nhận đầy đủ cho cả Contract A và B. Nếu có conflict ở B thì toàn bộ chain call phải re-execute hợp lý.")
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

	if len(cfg.PrivateKeys) < 4 {
		return fmt.Errorf("cần ít nhất 4 private keys để test")
	}

	// 1. Load ABI & Bytecode for TargetContract and CallerContract
	parsedTargetABI, err := abi.JSON(strings.NewReader(cfg.Contracts["TargetContract"].ABI))
	if err != nil {
		return fmt.Errorf("ABI parse TargetContract err: %w", err)
	}
	bytecodeTarget, err := hexutil.Decode("0x" + cfg.Contracts["TargetContract"].Bytecode)
	if err != nil {
		return fmt.Errorf("bytecode TargetContract err: %w", err)
	}

	parsedCallerABI, err := abi.JSON(strings.NewReader(cfg.Contracts["CallerContract"].ABI))
	if err != nil {
		return fmt.Errorf("ABI parse CallerContract err: %w", err)
	}
	bytecodeCaller, err := hexutil.Decode("0x" + cfg.Contracts["CallerContract"].Bytecode)
	if err != nil {
		return fmt.Errorf("bytecode CallerContract err: %w", err)
	}

	pk0, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("invalid private key[0]: %w", err)
	}
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	// 2. Deploy TargetContract
	fmt.Println("🚀 Deploying TargetContract...")
	targetAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecodeTarget, nil)
	if err != nil {
		return fmt.Errorf("deploy TargetContract thất bại: %w", err)
	}
	fmt.Printf("📌 TargetContract deployed at: %s\n", targetAddr.Hex())

	// 3. Deploy CallerContract(targetAddr)
	fmt.Println("🚀 Deploying CallerContract...")
	constructorData, err := parsedCallerABI.Pack("", *targetAddr)
	if err != nil {
		return fmt.Errorf("pack constructor CallerContract thất bại: %w", err)
	}
	bytecodeCallerWithArgs := append(bytecodeCaller, constructorData...)

	callerAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecodeCallerWithArgs, nil)
	if err != nil {
		return fmt.Errorf("deploy CallerContract thất bại: %w", err)
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
			pk, err := crypto.HexToECDSA(pKeyHex)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("Wallet %d invalid pk: %v", idx, err))
				errsMu.Unlock()
				return
			}
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))
			nonce, err := client.PendingNonceAt(context.Background(), from)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("Wallet %d nonce error: %v", idx, err))
				errsMu.Unlock()
				return
			}
			gasPrice, _ := client.SuggestGasPrice(context.Background())
			if gasPrice == nil {
				gasPrice = big.NewInt(1000000000)
			}

			valToAdd := big.NewInt(int64(idx * 100))
			data, err := parsedCallerABI.Pack("callTarget", valToAdd)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("Wallet %d pack error: %v", idx, err))
				errsMu.Unlock()
				return
			}
			gasLimit := uint64(200000)

			tx := types.NewTransaction(nonce, *callerAddr, big.NewInt(0), gasLimit, gasPrice, data)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("Wallet %d sign tx error: %v", idx, err))
				errsMu.Unlock()
				return
			}

			fmt.Printf("⏳ Wallet %d đang gửi tx: CallerContract.callTarget(%s)...\n", idx, valToAdd.String())

			if err := client.SendTransaction(context.Background(), signedTx); err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("Wallet %d send tx error: %v", idx, err))
				errsMu.Unlock()
				return
			}
			txHashes[idx-1] = signedTx.Hash()
		}(i, cfg.PrivateKeys[i])
	}

	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("có lỗi khi gửi giao dịch: %v", errs[0])
	}

	fmt.Println("⏳ Chờ các giao dịch được confirm...")
	successCount := 0
	revertCount := 0
	for i := 0; i < len(txHashes); i++ {
		hash := txHashes[i]
		if hash == (common.Hash{}) {
			continue
		}
		timeoutStart := time.Now()
		for {
			if time.Since(timeoutStart) > 60*time.Second {
				return fmt.Errorf("timeout waiting for receipt: %s", hash.Hex())
			}
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
			if err != nil && !strings.Contains(err.Error(), "not found") {
				return fmt.Errorf("lỗi kết nối RPC: %w", err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ CROSS-CONTRACT CALLS (BLOCK-STM):")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Thành công: %d, Revert: %d\n", successCount, revertCount)

	data, err := parsedTargetABI.Pack("value")
	if err != nil {
		return fmt.Errorf("pack target value err: %w", err)
	}
	msg := ethereum.CallMsg{To: targetAddr, Data: data}
	resBytes, err := client.CallContract(context.Background(), msg, nil)
	var finalValue *big.Int
	if err != nil {
		return fmt.Errorf("lỗi đọc TargetContract.value: %v", err)
	} else {
		unpacked, err := parsedTargetABI.Unpack("value", resBytes)
		if err != nil {
			return fmt.Errorf("unpack target value err: %w", err)
		}
		finalValue = unpacked[0].(*big.Int)
		fmt.Printf("🔍 TargetContract.value cuối cùng = %s\n", finalValue.String())
	}

	expectedSum := int64(0)
	for i := 1; i <= numTxs; i++ {
		expectedSum += int64(i * 100)
	}

	if successCount == numTxs && finalValue != nil && finalValue.Int64() == expectedSum {
		fmt.Printf("🎉 TEST PASSED: Block-STM xử lý mượt mà Cross-Contract Calls (Internal Calls). Storage cộng dồn chuẩn xác! Tổng = %d\n", expectedSum)
		return nil
	} else {
		actualVal := "nil"
		if finalValue != nil {
			actualVal = finalValue.String()
		}
		return fmt.Errorf("TEST FAILED: Block-STM xử lý sai! Kỳ vọng = %d, Thực tế = %s", expectedSum, actualVal)
	}
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

// Helper deploy (Hỗ trợ params)
func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte, constructorArgs []byte) (*common.Address, error) {
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return nil, err
	}
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		return nil, err
	}
	if gasPrice == nil {
		gasPrice = big.NewInt(1000000000)
	}

	data := append(bytecode, constructorArgs...)
	tx := types.NewContractCreation(nonce, big.NewInt(0), 3000000, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return nil, err
	}

	err = client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		return nil, err
	}

	timeoutStart := time.Now()
	for {
		if time.Since(timeoutStart) > 60*time.Second {
			return nil, fmt.Errorf("timeout waiting for receipt")
		}
		receipt, err := client.TransactionReceipt(context.Background(), signedTx.Hash())

		if err != nil && !strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("lỗi kết nối RPC: %v", err)
		}
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			if receipt.Status == 1 {
				addr := crypto.CreateAddress(from, nonce)
				return &addr, nil
			}
			return nil, fmt.Errorf("transaction reverted")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
