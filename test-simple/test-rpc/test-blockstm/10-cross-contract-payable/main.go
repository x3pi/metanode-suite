/*
 * BÀI TEST: 10-cross-contract-payable
 * MÔ TẢ   : Contract A gọi sang Contract B đồng thời đính kèm value (chuyển Native Token).
 * GỌI     : Cross-contract call kèm thuộc tính payable (chuyển value).
 * KỲ VỌNG : State thay đổi cả ở logic EVM (storage) và balance (Native token). Trạng thái sau cùng phải khớp hoàn toàn giá trị dư mới và biến storage.
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
	fmt.Println("BÀI TEST: 10-cross-contract-payable")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Contract A gọi sang Contract B đồng thời đính kèm value (chuyển Native Token).")
	fmt.Println("⚡ GỌI     : Cross-contract call kèm thuộc tính payable (chuyển value).")
	fmt.Println("🎯 KỲ VỌNG : State thay đổi cả ở logic EVM (storage) và balance (Native token). Trạng thái sau cùng phải khớp hoàn toàn giá trị dư mới và biến storage.")
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

	// 1. Load ABI & Bytecode for PayableTargetContract and PayableCallerContract
	parsedTargetABI, err := abi.JSON(strings.NewReader(cfg.Contracts["PayableTargetContract"].ABI))
	if err != nil { log.Fatalf("ABI parse PayableTargetContract err: %v", err) }
	bytecodeTarget, err := hexutil.Decode("0x" + cfg.Contracts["PayableTargetContract"].Bytecode)
	if err != nil { log.Fatalf("Bytecode PayableTargetContract err: %v", err) }

	parsedCallerABI, err := abi.JSON(strings.NewReader(cfg.Contracts["PayableCallerContract"].ABI))
	if err != nil { log.Fatalf("ABI parse PayableCallerContract err: %v", err) }
	bytecodeCaller, err := hexutil.Decode("0x" + cfg.Contracts["PayableCallerContract"].Bytecode)
	if err != nil { log.Fatalf("Bytecode PayableCallerContract err: %v", err) }

	pk0, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	// 2. Deploy PayableTargetContract
	fmt.Println("🚀 Deploying PayableTargetContract...")
	targetAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecodeTarget, nil)
	if err != nil {
		log.Fatalf("❌ Deploy PayableTargetContract thất bại: %v", err)
	}
	fmt.Printf("📌 PayableTargetContract deployed at: %s\n", targetAddr.Hex())

	// 3. Deploy PayableCallerContract(targetAddr)
	fmt.Println("🚀 Deploying PayableCallerContract...")
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
	fmt.Printf("📌 PayableCallerContract deployed at: %s\n\n", callerAddr.Hex())

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	numTxs := len(cfg.PrivateKeys) - 1
	txHashes := make([]common.Hash, numTxs)

	fmt.Printf("🔥 Bắt đầu test Cross-Contract Payable: %d ví cùng gọi PayableCallerContract.callTarget() gửi kèm ETH trỏ về 1 PayableTargetContract...\n", numTxs)
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

			// Call PayableCallerContract.callTarget() with ETH value
			callValue := big.NewInt(int64(idx * 100))
			data, _ := parsedCallerABI.Pack("callTarget")
			gasLimit := uint64(200000) // Cần nhiều gas hơn cho internal calls

			tx := types.NewTransaction(nonce, *callerAddr, callValue, gasLimit, gasPrice, data)
			signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)

			fmt.Printf("⏳ Wallet %d đang gửi tx: PayableCallerContract.callTarget() kèm %s wei...\n", idx, callValue.String())

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
		timeoutStart := time.Now()
		for {
			if time.Since(timeoutStart) > 60*time.Second {
				fmt.Println("❌ Timeout waiting for receipt")
				os.Exit(1)
			}
			receipt, err := client.TransactionReceipt(context.Background(), hash)

			if err != nil && !strings.Contains(err.Error(), "not found") {
				fmt.Printf("Lỗi kết nối RPC: %v\n", err)
				os.Exit(1)
			}
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
			time.Sleep(100 * time.Millisecond)
		}
	}

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ CROSS-CONTRACT PAYABLE CALLS (BLOCK-STM):")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Thành công: %d, Revert: %d\n", successCount, revertCount)

	// Kiểm tra giá trị "value" của PayableTargetContract (phải bằng numTxs)
	dataValue, _ := parsedTargetABI.Pack("value")
	msgValue := ethereum.CallMsg{To: targetAddr, Data: dataValue}
	resValueBytes, _ := client.CallContract(context.Background(), msgValue, nil)
	unpackedValue, _ := parsedTargetABI.Unpack("value", resValueBytes)
	finalValue := unpackedValue[0].(*big.Int)
	fmt.Printf("🔍 PayableTargetContract.value cuối cùng = %s (Kỳ vọng: %d)\n", finalValue.String(), numTxs)

	// Lấy giá trị tổng ETH nhận được (totalEthReceived)
	dataEth, _ := parsedTargetABI.Pack("totalEthReceived")
	msgEth := ethereum.CallMsg{To: targetAddr, Data: dataEth}
	resEthBytes, _ := client.CallContract(context.Background(), msgEth, nil)
	unpackedEth, _ := parsedTargetABI.Unpack("totalEthReceived", resEthBytes)
	finalTotalEth := unpackedEth[0].(*big.Int)
	
	expectedEthSum := int64(0)
	for i := 1; i <= numTxs; i++ {
		expectedEthSum += int64(i * 100)
	}
	fmt.Printf("🔍 PayableTargetContract.totalEthReceived cuối cùng = %s (Kỳ vọng: %d)\n", finalTotalEth.String(), expectedEthSum)

	// Lấy Balance thực tế của PayableTargetContract trên Blockchain
	actualBalance, _ := client.BalanceAt(context.Background(), *targetAddr, nil)
	fmt.Printf("🔍 Balance thực tế của PayableTargetContract = %s wei (Kỳ vọng: %d wei)\n", actualBalance.String(), expectedEthSum)

	// Nếu Block-STM xử lý chuẩn, tất cả phải khớp
	if successCount == numTxs && finalValue.Int64() == int64(numTxs) && finalTotalEth.Int64() == expectedEthSum && actualBalance.Int64() == expectedEthSum {
		fmt.Println("🎉 TEST PASSED: Block-STM xử lý mượt mà Cross-Contract Payable Calls! Storage và Số dư ETH cộng dồn cực kỳ chuẩn xác!")
	} else {
		fmt.Println("⚠️ TEST FAILED: Block-STM xử lý sai Storage hoặc Balance trong internal calls!")
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

	timeoutStart := time.Now()
	for {
		if time.Since(timeoutStart) > 60*time.Second {
			fmt.Println("❌ Timeout waiting for receipt")
			os.Exit(1)
		}
		receipt, err := client.TransactionReceipt(context.Background(), signedTx.Hash())

		if err != nil && !strings.Contains(err.Error(), "not found") {
			fmt.Printf("Lỗi kết nối RPC: %v\n", err)
			os.Exit(1)
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
