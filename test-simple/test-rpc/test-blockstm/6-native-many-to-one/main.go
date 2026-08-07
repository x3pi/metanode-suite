/*
 * BÀI TEST: 6-native-many-to-one
 * MÔ TẢ   : Nhiều tài khoản khác nhau cùng chuyển Native Token vào chung MỘT tài khoản nhận.
 * GỌI     : Chuyển tiền Native (Coin) từ nhiều ví -> 1 ví duy nhất.
 * KỲ VỌNG : Xung đột trên tài khoản nhận (cộng dồn số dư). Số dư cuối cùng của người nhận phải bằng tổng số dư ban đầu cộng tổng số tiền đã nhận.
 */
package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"strings"
	"log"
	"math/big"
	"os"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Config struct {
	RPCUrl      string   `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
	ChainID     int64    `json:"chain_id"`
}

func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 6-native-many-to-one")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Nhiều tài khoản khác nhau cùng chuyển Native Token vào chung MỘT tài khoản nhận.")
	fmt.Println("⚡ GỌI     : Chuyển tiền Native (Coin) từ nhiều ví -> 1 ví duy nhất.")
	fmt.Println("🎯 KỲ VỌNG : Xung đột trên tài khoản nhận (cộng dồn số dư). Số dư cuối cùng của người nhận phải bằng tổng số dư ban đầu cộng tổng số tiền đã nhận.")
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

	if len(cfg.PrivateKeys) < 2 {
		log.Fatalf("❌ Cần ít nhất 2 private keys để test")
	}

	// Chọn ví 0 làm ví nhận tiền
	pk0, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	receiverAddr := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Printf("🚀 Mục tiêu: 10 ví gửi tiền ĐỒNG THỜI đến 1 ví nhận: %s\n", receiverAddr.Hex())

	initialBalance, _ := client.BalanceAt(context.Background(), receiverAddr, nil)
	fmt.Printf("💰 Số dư ban đầu của ví nhận: %s wei\n\n", initialBalance.String())

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	txHashes := make([]common.Hash, len(cfg.PrivateKeys))
	sendAmount := big.NewInt(1000) // Gửi 1000 wei mỗi ví

	fmt.Printf("🔥 Gửi %d giao dịch Native Transfer đồng thời...\n", len(cfg.PrivateKeys)-1)
	start := time.Now()

	for i := 1; i < len(cfg.PrivateKeys); i++ {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()

			pk, err := crypto.HexToECDSA(pKeyHex)
			if err != nil {
				return
			}
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

			nonce, err := client.PendingNonceAt(context.Background(), from)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, err)
				errsMu.Unlock()
				return
			}
			gasPrice, _ := client.SuggestGasPrice(context.Background())
			if gasPrice == nil {
				gasPrice = big.NewInt(1000000000)
			}
			gasLimit := uint64(21000)

			tx := types.NewTransaction(nonce, receiverAddr, sendAmount, gasLimit, gasPrice, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)
			if err != nil {
				return
			}

			if err := client.SendTransaction(context.Background(), signedTx); err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi send tx từ wallet %d: %v", idx, err))
				errsMu.Unlock()
				return
			}

			fmt.Printf("✅ Wallet %d gửi tx thành công: %s\n", idx, signedTx.Hash().Hex())
			txHashes[idx] = signedTx.Hash()
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
	for i := 1; i < len(txHashes); i++ {
		hash := txHashes[i]
		if hash == (common.Hash{}) {
			continue
		}
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
					fmt.Printf("❌ Wallet %d Tx bị revert!\n", i)
				} else {
					fmt.Printf("✅ Wallet %d Tx %s confirmed trong block %d\n", i, hash.Hex()[:10]+"...", receipt.BlockNumber.Uint64())
				}
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	finalBalance, _ := client.BalanceAt(context.Background(), receiverAddr, nil)
	elapsed := time.Since(start)

	expectedAdded := new(big.Int).Mul(sendAmount, big.NewInt(int64(len(cfg.PrivateKeys)-1)))
	expectedFinal := new(big.Int).Add(initialBalance, expectedAdded)

	fmt.Println("\n📊 KẾT QUẢ NATIVE TRANSFER (MANY-TO-ONE):")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Số dư ban đầu: %s wei\n", initialBalance.String())
	fmt.Printf("Số dư cuối cùng: %s wei\n", finalBalance.String())
	fmt.Printf("Kỳ vọng: %s wei\n", expectedFinal.String())

	if finalBalance.Cmp(expectedFinal) == 0 {
		fmt.Println("🎉 TEST PASSED: Balance cập nhật chính xác, không bị race condition!")
	} else {
		fmt.Println("⚠️ TEST FAILED: Sai lệch Balance! Khả năng xảy ra race condition (Fast-Path bug).")
	}
}
