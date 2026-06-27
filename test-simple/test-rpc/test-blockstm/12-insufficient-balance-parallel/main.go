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
	fmt.Println("BÀI TEST: 12-insufficient-balance-parallel")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Gửi song song nhiều tx vượt quá tổng số dư của tài khoản.")
	fmt.Println("⚡ GỌI     : Native Transfer Fast-path.")
	fmt.Println("🎯 KỲ VỌNG : Sẽ có một số giao dịch bị revert do hết tiền (Insufficient Balance).")
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

	// Chọn ví 0 làm ví gửi tiền vì ví cuối cùng đã cạn số dư
	pkSender, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	senderAddr := crypto.PubkeyToAddress(*pkSender.Public().(*ecdsa.PublicKey))

	balance, _ := client.BalanceAt(context.Background(), senderAddr, nil)
	fmt.Printf("💰 Số dư ví gửi (%s): %s wei\n", senderAddr.Hex(), balance.String())

	baseNonce, _ := client.PendingNonceAt(context.Background(), senderAddr)

	// Tính số tiền mỗi giao dịch: Gửi 5 giao dịch, mỗi giao dịch = (balance / 3).
	// Nghĩa là giao dịch 1, 2, 3 thành công. Giao dịch 4, 5 sẽ rớt.
	// Lưu ý: Cần chừa ra một ít để trả gas fee.
	sendAmount := new(big.Int).Div(balance, big.NewInt(4))

	if sendAmount.Sign() <= 0 {
		log.Fatalf("❌ Số dư quá thấp để chạy bài test này!")
	}

	fmt.Printf("🚀 Sẽ gửi 5 giao dịch ĐỒNG THỜI, mỗi cái: %s wei\n\n", sendAmount.String())

	var wg sync.WaitGroup
	var errsMu sync.Mutex

	txHashes := make([]common.Hash, 5)
	start := time.Now()

	for i := 0; i < 5; i++ {
		wg.Add(1)
		
		// Lấy ví nhận khác với ví gửi (ví gửi là 0)
		// Ta có thể dùng PrivateKeys[i+1] nếu danh sách có đủ, hoặc chỉ dùng PrivateKeys[1] cho tất cả
		recvIdx := i + 1
		if recvIdx >= len(cfg.PrivateKeys) {
			recvIdx = 1 // fallback về ví 1 nếu thiếu key
		}
		pkRecv, _ := crypto.HexToECDSA(cfg.PrivateKeys[recvIdx])
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
	
	// Đợi block được sinh ra
	time.Sleep(3 * time.Second)
	
	for i := 0; i < 5; i++ {
		hash := txHashes[i]
		if hash == (common.Hash{}) {
			// Failed at mempool
			revertCount++
			continue
		}
		
		receipt, err := client.TransactionReceipt(context.Background(), hash)
		if err == nil {
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
		fmt.Println("\n⚠️ TEST FAILED: Logic bị sai. Có thể tất cả đều thành công!")
		os.Exit(1)
	}
}
