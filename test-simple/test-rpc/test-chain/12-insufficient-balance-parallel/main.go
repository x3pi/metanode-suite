/*
 * BÀI TEST: 12-insufficient-balance-parallel
 * MÔ TẢ   : Gửi song song nhiều giao dịch chuyển tiền vượt quá tổng số dư của tài khoản.
 * GỌI     : Chuyển Native (Fast-path). Ví A có 100 coin, bắn song song 5 giao dịch, mỗi cái chuyển 30 coin.
 * KỲ VỌNG : Chỉ 3 giao dịch đầu (tổng 90) thành công, 2 giao dịch sau phải bị Revert vì thiếu tiền.
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
	fmt.Println("BÀI TEST: 12-insufficient-balance-parallel")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Gửi song song nhiều tx vượt quá tổng số dư của tài khoản.")
	fmt.Println("⚡ GỌI     : Native Transfer Fast-path.")
	fmt.Println("🎯 KỲ VỌNG : Sẽ có một số giao dịch bị revert do hết tiền (Insufficient Balance).")
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

	pkSender, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("invalid private key[0]: %w", err)
	}
	senderAddr := crypto.PubkeyToAddress(*pkSender.Public().(*ecdsa.PublicKey))

	balance, err := client.BalanceAt(context.Background(), senderAddr, nil)
	if err != nil {
		return fmt.Errorf("lỗi lấy balance: %w", err)
	}
	fmt.Printf("💰 Số dư ví gửi (%s): %s wei\n", senderAddr.Hex(), balance.String())

	baseNonce, err := client.PendingNonceAt(context.Background(), senderAddr)
	if err != nil {
		return fmt.Errorf("lỗi lấy nonce: %w", err)
	}

	sendAmount := new(big.Int).Div(balance, big.NewInt(4))
	fmt.Printf("🚀 Sẽ gửi 5 giao dịch ĐỒNG THỜI, mỗi cái: %s wei\n\n", sendAmount.String())

	var wg sync.WaitGroup
	var errsMu sync.Mutex

	txHashes := make([]common.Hash, 5)
	start := time.Now()

	for i := 0; i < 5; i++ {
		wg.Add(1)

		pkRecv, err := crypto.HexToECDSA(cfg.PrivateKeys[(i+1)%len(cfg.PrivateKeys)])
		if err != nil {
			return fmt.Errorf("invalid receiver key %d: %w", i, err)
		}
		receiverAddr := crypto.PubkeyToAddress(*pkRecv.Public().(*ecdsa.PublicKey))

		txNonce := baseNonce + uint64(i)

		go func(idx int, rAddr common.Address, nonce uint64) {
			defer wg.Done()

			gasPrice := big.NewInt(1000000000)
			gasLimit := uint64(21000)

			tx := types.NewTransaction(nonce, rAddr, sendAmount, gasLimit, gasPrice, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pkSender)
			if err != nil {
				return
			}

			if err := client.SendTransaction(context.Background(), signedTx); err != nil {
				errsMu.Lock()
				fmt.Printf("⚠️ Lỗi gửi tx %d (nonce %d): %v\n", idx, nonce, err)
				errsMu.Unlock()
				return
			}

			fmt.Printf("✅ Đã push tx (Nonce: %d): %s\n", nonce, signedTx.Hash().Hex())
			txHashes[idx] = signedTx.Hash()
		}(i, receiverAddr, txNonce)
	}

	wg.Wait()

	fmt.Println("⏳ Chờ các giao dịch được confirm...")
	successCount := 0
	revertCount := 0

	time.Sleep(3 * time.Second)

	for i := 0; i < 5; i++ {
		hash := txHashes[i]
		if hash == (common.Hash{}) {
			revertCount++
			continue
		}

		receipt, err := client.TransactionReceipt(context.Background(), hash)
		if err != nil && !strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("lỗi kết nối RPC: %w", err)
		}
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			if receipt.Status != 1 {
				fmt.Printf("🔄 Tx %s BỊ REVERT ĐÚNG NHƯ KỲ VỌNG! (Thiếu tiền)\n", hash.Hex())
				revertCount++
			} else {
				fmt.Printf("✅ Tx %s THÀNH CÔNG trong block %d\n", hash.Hex(), receipt.BlockNumber.Uint64())
				successCount++
			}
		} else {
			fmt.Printf("🔄 Tx %s FAILED (Không lọt vào block)\n", hash.Hex())
			revertCount++
		}
	}

	elapsed := time.Since(start)

	fmt.Println("\n📊 KẾT QUẢ INSUFFICIENT BALANCE TEST:")
	fmt.Printf("Thời gian chạy: %v\n", elapsed)
	fmt.Printf("Số lượng thành công: %d (Kỳ vọng: 3 hoặc ít hơn)\n", successCount)
	fmt.Printf("Số lượng bị từ chối / Revert: %d (Kỳ vọng: >0)\n", revertCount)

	if revertCount > 0 {
		fmt.Println("\n🎉 TEST PASSED: Block-STM hoặc Mempool xử lý hoàn hảo! (Phát hiện hết tiền song song)")
	} else {
		return fmt.Errorf("TEST FAILED: Logic bị sai. Có thể tất cả đều thành công")
	}
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
