/*
 * BÀI TEST: 7-native-one-to-many
 * MÔ TẢ   : Một tài khoản gửi Native Token cho NHIỀU tài khoản khác nhau.
 * GỌI     : Chuyển tiền Native (Coin) từ 1 ví -> nhiều ví khác nhau.
 * KỲ VỌNG : Xung đột trên tài khoản gửi (trừ số dư nhiều lần, tăng nonce). Nonce phải tăng tuần tự và số dư người gửi phải bị trừ đúng tổng số tiền gửi đi.
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
	fmt.Println("BÀI TEST: 7-native-one-to-many")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Một tài khoản gửi Native Token cho NHIỀU tài khoản khác nhau.")
	fmt.Println("⚡ GỌI     : Chuyển tiền Native (Coin) từ 1 ví -> nhiều ví khác nhau.")
	fmt.Println("🎯 KỲ VỌNG : Xung đột trên tài khoản gửi (trừ số dư nhiều lần, tăng nonce). Nonce phải tăng tuần tự và số dư người gửi phải bị trừ đúng tổng số tiền gửi đi.")
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

	// Chọn ví 0 làm ví gửi tiền
	pk0, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("invalid private key[0]: %w", err)
	}
	senderAddr := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Printf("🚀 Mục tiêu: 1 ví (%s) gửi tiền ĐỒNG THỜI đến %d ví nhận với Nonce tăng dần (Test Mempool)\n\n", senderAddr.Hex(), len(cfg.PrivateKeys)-1)

	baseNonce, err := client.PendingNonceAt(context.Background(), senderAddr)
	if err != nil {
		return fmt.Errorf("lỗi lấy nonce: %w", err)
	}

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	txHashes := make([]common.Hash, len(cfg.PrivateKeys))
	sendAmount := big.NewInt(1000)

	// Lưu số dư ban đầu của các ví nhận
	initialBalances := make(map[int]*big.Int)
	for i := 1; i < len(cfg.PrivateKeys); i++ {
		pkRecv, err := crypto.HexToECDSA(cfg.PrivateKeys[i])
		if err != nil {
			return fmt.Errorf("invalid private key[%d]: %w", i, err)
		}
		receiverAddr := crypto.PubkeyToAddress(*pkRecv.Public().(*ecdsa.PublicKey))
		bal, err := client.BalanceAt(context.Background(), receiverAddr, nil)
		if err != nil {
			return fmt.Errorf("lỗi lấy balance ban đầu ví %d: %w", i, err)
		}
		initialBalances[i] = bal
	}

	fmt.Printf("🔥 Push %d giao dịch vào Mempool cùng lúc...\n", len(cfg.PrivateKeys)-1)
	start := time.Now()

	for i := 1; i < len(cfg.PrivateKeys); i++ {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()
			pkRecv, err := crypto.HexToECDSA(pKeyHex)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, err)
				errsMu.Unlock()
				return
			}
			receiverAddr := crypto.PubkeyToAddress(*pkRecv.Public().(*ecdsa.PublicKey))

			targetNonce := baseNonce + uint64(idx-1)

			gasLimit := uint64(21000)
			gasPrice := big.NewInt(1e9)

			tx := types.NewTransaction(targetNonce, receiverAddr, sendAmount, gasLimit, gasPrice, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk0)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi sign: %v", err))
				errsMu.Unlock()
				return
			}

			if err := client.SendTransaction(context.Background(), signedTx); err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi gửi tx nonce %d: %v", targetNonce, err))
				errsMu.Unlock()
				return
			}

			fmt.Printf("✅ Đã push tx (Nonce: %d) đến %s...: %s\n", targetNonce, receiverAddr.Hex()[:10], signedTx.Hash().Hex())
			txHashes[idx] = signedTx.Hash()
		}(i, cfg.PrivateKeys[i])
	}

	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("có lỗi khi gửi giao dịch: %v", errs[0])
	}

	fmt.Println("⏳ Chờ các giao dịch được confirm từ Mempool vào Block...")
	successCount := 0
	for i := 1; i < len(txHashes); i++ {
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
					return fmt.Errorf("tx %s bị revert", hash.Hex())
				}
				fmt.Printf("✅ Tx %s confirmed trong block %d\n", hash.Hex()[:10]+"...", receipt.BlockNumber.Uint64())
				successCount++
				break
			}
			if err != nil && !strings.Contains(err.Error(), "not found") {
				return fmt.Errorf("lỗi kết nối RPC: %w", err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	elapsed := time.Since(start)

	fmt.Println("\n📊 KẾT QUẢ NATIVE TRANSFER (ONE-TO-MANY):")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Số lượng gửi thành công: %d/%d\n", successCount, len(cfg.PrivateKeys)-1)

	fmt.Println("\n🔍 KIỂM TOÁN SỐ DƯ (BALANCE VERIFICATION):")
	testFailed := false

	for i := 1; i < len(cfg.PrivateKeys); i++ {
		pkRecv, _ := crypto.HexToECDSA(cfg.PrivateKeys[i])
		receiverAddr := crypto.PubkeyToAddress(*pkRecv.Public().(*ecdsa.PublicKey))

		finalBal, err := client.BalanceAt(context.Background(), receiverAddr, nil)
		if err != nil {
			return fmt.Errorf("lỗi lấy final balance ví %d: %w", i, err)
		}
		initialBal := initialBalances[i]

		expectedBal := new(big.Int).Add(initialBal, sendAmount)

		if finalBal.Cmp(expectedBal) != 0 {
			fmt.Printf("   ❌ LỖI: Wallet %d (%s) có số dư %s, kỳ vọng %s\n", i, receiverAddr.Hex()[:8], finalBal.String(), expectedBal.String())
			testFailed = true
		} else {
			fmt.Printf("   ✅ Wallet %d: Chuẩn (+1000 wei)\n", i)
		}
	}

	if successCount != len(cfg.PrivateKeys)-1 || testFailed {
		return fmt.Errorf("TEST FAILED: Mempool từ chối giao dịch hoặc Balance bị sai lệch do Race Condition")
	}

	fmt.Println("\n🎉 TEST PASSED: Mempool xử lý Nonce tăng dần cực chuẩn và Balance của tất cả ví nhận cập nhật chính xác!")
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

