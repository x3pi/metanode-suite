/*
 * BÀI TEST: 11-double-spending-same-nonce
 * MÔ TẢ   : Một tài khoản gửi NHIỀU giao dịch đi với CÙNG MỘT NONCE (Double Spending).
 * GỌI     : Chuyển tiền Native (Coin) từ 1 ví đi nhiều nơi nhưng cố tình set chung 1 nonce.
 * KỲ VỌNG : Chỉ có 1 giao dịch được xác nhận thành công. Các giao dịch còn lại phải bị loại bỏ (Revert/Failed).
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
	fmt.Println("BÀI TEST: 11-double-spending-same-nonce")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Một tài khoản gửi NHIỀU giao dịch đi với CÙNG MỘT NONCE (Double Spending).")
	fmt.Println("⚡ GỌI     : Chuyển tiền Native (Coin) từ 1 ví đi nhiều nơi nhưng cố tình set chung 1 nonce.")
	fmt.Println("🎯 KỲ VỌNG : Chỉ có 1 giao dịch được xác nhận thành công. Các giao dịch còn lại phải bị loại bỏ (Revert/Failed).")
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

	pk0, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	senderAddr := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Printf("🚀 Mục tiêu: 1 ví (%s) gửi 5 giao dịch ĐỒNG THỜI với CÙNG MỘT NONCE\n\n", senderAddr.Hex())

	baseNonce, err := client.PendingNonceAt(context.Background(), senderAddr)
	if err != nil {
		log.Fatalf("❌ Lỗi lấy nonce: %v", err)
	}

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	txHashes := make([]common.Hash, 5)
	sendAmount := big.NewInt(1000)

	start := time.Now()

	for i := 0; i < 5; i++ {
		wg.Add(1)
		
		// Lấy ví nhận bất kỳ
		pkRecv, _ := crypto.HexToECDSA(cfg.PrivateKeys[i%len(cfg.PrivateKeys)])
		receiverAddr := crypto.PubkeyToAddress(*pkRecv.Public().(*ecdsa.PublicKey))
		
		// TẤT CẢ GIAO DỊCH ĐỀU DÙNG CHUNG 1 NONCE
		txNonce := baseNonce

		go func(idx int, rAddr common.Address, nonce uint64) {
			defer wg.Done()

			gasPrice := big.NewInt(1000000000)
			gasLimit := uint64(21000)

			tx := types.NewTransaction(nonce, rAddr, sendAmount, gasLimit, gasPrice, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk0)
			if err != nil {
				return
			}

			if err := client.SendTransaction(context.Background(), signedTx); err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("Lỗi gửi (bị mempool chặn ngay lập tức): %v", err))
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
	failedCount := 0
	for i := 0; i < 5; i++ {
		hash := txHashes[i]
		if hash == (common.Hash{}) {
			failedCount++
			continue
		}
		
		// Đợi 2 giây cho mempool và block tạo ra
		time.Sleep(1 * time.Second)
		receipt, err := client.TransactionReceipt(context.Background(), hash)
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			if receipt.Status != 1 {
				fmt.Printf("❌ Tx %s bị REVERT!\n", hash.Hex())
				failedCount++
			} else {
				fmt.Printf("✅ Tx %s THÀNH CÔNG trong block %d\n", hash.Hex(), receipt.BlockNumber.Uint64())
				successCount++
			}
		} else {
			fmt.Printf("❌ Tx %s FAILED (Không được cho vào block, bị drop)\n", hash.Hex())
			failedCount++
		}
	}

	elapsed := time.Since(start)

	fmt.Println("\n📊 KẾT QUẢ DOUBLE SPENDING TEST:")
	fmt.Printf("Thời gian chạy: %v\n", elapsed)
	fmt.Printf("Số lượng thành công: %d (Kỳ vọng: 1)\n", successCount)
	fmt.Printf("Số lượng thất bại / từ chối: %d (Kỳ vọng: 4)\n", failedCount)

	if successCount == 1 {
		fmt.Println("\n🎉 TEST PASSED: Block-STM xử lý chuẩn xác, chặn đứng các giao dịch Double Spend cùng Nonce!")
	} else {
		fmt.Println("\n⚠️ TEST FAILED: Phát hiện lỗi bảo mật! Có thể có nhiều hơn 1 tx được xác nhận hoặc tất cả đều rớt!")
		os.Exit(1)
	}
}
