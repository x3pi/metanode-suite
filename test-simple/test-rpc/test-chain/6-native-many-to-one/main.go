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
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/config"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func RunTest(configPath string) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 6-native-many-to-one")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Nhiều tài khoản khác nhau cùng chuyển Native Token vào chung MỘT tài khoản nhận.")
	fmt.Println("⚡ GỌI     : Chuyển tiền Native (Coin) từ nhiều ví -> 1 ví duy nhất.")
	fmt.Println("🎯 KỲ VỌNG : Xung đột trên tài khoản nhận (cộng dồn số dư). Số dư cuối cùng của người nhận phải bằng tổng số dư ban đầu cộng tổng số tiền đã nhận.")
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

	if len(cfg.PrivateKeys) < 2 {
		return fmt.Errorf("cần ít nhất 2 private keys để test")
	}

	// Dùng địa chỉ chuyên biệt làm đích nhận tiền (để không bị trừ phí gas làm sai lệch balance)
	receiverAddr := common.HexToAddress("0x0000000000000000000000000000000000000088")

	fmt.Printf("🚀 Mục tiêu: %d ví gửi tiền ĐỒNG THỜI đến 1 ví nhận: %s\n", len(cfg.PrivateKeys), receiverAddr.Hex())

	initialBalance, err := client.BalanceAt(context.Background(), receiverAddr, nil)
	if err != nil {
		return fmt.Errorf("lỗi lấy initial balance: %w", err)
	}
	fmt.Printf("💰 Số dư ban đầu của ví nhận: %s wei\n\n", initialBalance.String())

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	txHashes := make([]common.Hash, len(cfg.PrivateKeys))
	sendAmount := big.NewInt(1000) // Gửi 1000 wei mỗi ví

	fmt.Printf("🔥 Gửi %d giao dịch Native Transfer đồng thời...\n", len(cfg.PrivateKeys))
	start := time.Now()

	for i := 0; i < len(cfg.PrivateKeys); i++ {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()
			pk, err := crypto.HexToECDSA(pKeyHex)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi ví %d key: %v", idx, err))
				errsMu.Unlock()
				return
			}
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

			nonce, err := client.PendingNonceAt(context.Background(), from)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi ví %d nonce: %v", idx, err))
				errsMu.Unlock()
				return
			}

			gasLimit := uint64(21000)
			gasPrice := big.NewInt(1e9)

			tx := types.NewTransaction(nonce, receiverAddr, sendAmount, gasLimit, gasPrice, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi ví %d sign: %v", idx, err))
				errsMu.Unlock()
				return
			}

			if err := client.SendTransaction(context.Background(), signedTx); err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi ví %d send: %v", idx, err))
				errsMu.Unlock()
				return
			}

			fmt.Printf("✅ Wallet %d gửi tx thành công: %s\n", idx, signedTx.Hash().Hex())
			txHashes[idx] = signedTx.Hash()
		}(i, cfg.PrivateKeys[i])
	}

	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("có giao dịch gửi thất bại: %v", errs[0])
	}

	fmt.Println("⏳ Chờ các giao dịch được confirm...")
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
					return fmt.Errorf("wallet %d Tx bị revert: %s", i, hash.Hex())
				}
				fmt.Printf("✅ Wallet %d Tx %s confirmed trong block %d\n", i, hash.Hex()[:10]+"...", receipt.BlockNumber.Uint64())
				break
			}
			if err != nil && !strings.Contains(err.Error(), "not found") {
				return fmt.Errorf("lỗi kết nối RPC: %w", err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	finalBalance, err := client.BalanceAt(context.Background(), receiverAddr, nil)
	if err != nil {
		return fmt.Errorf("lỗi lấy final balance: %w", err)
	}
	elapsed := time.Since(start)

	expectedAdded := new(big.Int).Mul(sendAmount, big.NewInt(int64(len(cfg.PrivateKeys))))
	expectedFinal := new(big.Int).Add(initialBalance, expectedAdded)

	fmt.Println("\n📊 KẾT QUẢ NATIVE TRANSFER (MANY-TO-ONE):")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Số dư ban đầu: %s wei\n", initialBalance.String())
	fmt.Printf("Số dư cuối cùng: %s wei\n", finalBalance.String())
	fmt.Printf("Kỳ vọng: %s wei\n", expectedFinal.String())

	if finalBalance.Cmp(expectedFinal) != 0 {
		return fmt.Errorf("TEST FAILED: Sai lệch Balance! Khả năng xảy ra race condition (Fast-Path bug)")
	}

	fmt.Println("🎉 TEST PASSED: Balance cập nhật chính xác, không bị race condition!")
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
