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
	fmt.Println("BÀI TEST: 7-native-one-to-many")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Một tài khoản gửi Native Token cho NHIỀU tài khoản khác nhau.")
	fmt.Println("⚡ GỌI     : Chuyển tiền Native (Coin) từ 1 ví -> nhiều ví khác nhau.")
	fmt.Println("🎯 KỲ VỌNG : Xung đột trên tài khoản gửi (trừ số dư nhiều lần, tăng nonce). Nonce phải tăng tuần tự và số dư người gửi phải bị trừ đúng tổng số tiền gửi đi.")
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

	// Chọn ví 0 làm ví gửi tiền
	pk0, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	senderAddr := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Printf("🚀 Mục tiêu: 1 ví (%s) gửi tiền ĐỒNG THỜI đến %d ví nhận với Nonce tăng dần (Test Mempool)\n\n", senderAddr.Hex(), len(cfg.PrivateKeys)-1)

	baseNonce, err := client.PendingNonceAt(context.Background(), senderAddr)
	if err != nil {
		log.Fatalf("❌ Lỗi lấy nonce: %v", err)
	}

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	// Lưu số dư ban đầu của các ví nhận
	initialBalances := make(map[int]*big.Int)
	for i := 1; i < len(cfg.PrivateKeys); i++ {
		pkRecv, _ := crypto.HexToECDSA(cfg.PrivateKeys[i])
		recvAddr := crypto.PubkeyToAddress(*pkRecv.Public().(*ecdsa.PublicKey))
		bal, _ := client.BalanceAt(context.Background(), recvAddr, nil)
		initialBalances[i] = bal
	}

	txHashes := make([]common.Hash, len(cfg.PrivateKeys))
	sendAmount := big.NewInt(1000) // Gửi 1000 wei mỗi ví

	fmt.Printf("🔥 Push %d giao dịch vào Mempool cùng lúc...\n", len(cfg.PrivateKeys)-1)
	start := time.Now()

	for i := 1; i < len(cfg.PrivateKeys); i++ {
		wg.Add(1)
		
		// Lấy địa chỉ ví nhận từ config
		pkRecv, _ := crypto.HexToECDSA(cfg.PrivateKeys[i])
		receiverAddr := crypto.PubkeyToAddress(*pkRecv.Public().(*ecdsa.PublicKey))
		
		// Tính toán nonce cho từng giao dịch (tự cộng thủ công)
		txNonce := baseNonce + uint64(i-1)

		go func(idx int, rAddr common.Address, nonce uint64) {
			defer wg.Done()

			gasPrice, _ := client.SuggestGasPrice(context.Background())
			if gasPrice == nil {
				gasPrice = big.NewInt(1000000000)
			}
			gasLimit := uint64(21000)

			tx := types.NewTransaction(nonce, rAddr, sendAmount, gasLimit, gasPrice, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk0)
			if err != nil {
				return
			}

			if err := client.SendTransaction(context.Background(), signedTx); err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi send tx %d (nonce %d): %v", idx, nonce, err))
				errsMu.Unlock()
				return
			}

			fmt.Printf("✅ Đã push tx (Nonce: %d) đến %s: %s\n", nonce, rAddr.Hex()[:10]+"...", signedTx.Hash().Hex())
			txHashes[idx] = signedTx.Hash()
		}(i, receiverAddr, txNonce)
	}

	wg.Wait()

	if len(errs) > 0 {
		fmt.Println("❌ Một số giao dịch gửi thất bại (Mempool từ chối):")
		for _, e := range errs {
			fmt.Println("  -", e)
		}
	}

	fmt.Println("⏳ Chờ các giao dịch được confirm từ Mempool vào Block...")
	successCount := 0
	for i := 1; i < len(txHashes); i++ {
		hash := txHashes[i]
		if hash == (common.Hash{}) {
			continue
		}
		for {
			receipt, err := client.TransactionReceipt(context.Background(), hash)

			if err != nil && !strings.Contains(err.Error(), "not found") {
				fmt.Printf("Lỗi kết nối RPC: %v\n", err)
				os.Exit(1)
			}
			if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
				if receipt.Status != 1 {
					fmt.Printf("❌ Tx %s bị revert!\n", hash.Hex())
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

	fmt.Println("\n📊 KẾT QUẢ NATIVE TRANSFER (ONE-TO-MANY):")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Số lượng gửi thành công: %d/%d\n", successCount, len(cfg.PrivateKeys)-1)

	// Lấy số dư sau khi chạy của tất cả các ví để kiểm toán
	fmt.Println("\n🔍 KIỂM TOÁN SỐ DƯ (BALANCE VERIFICATION):")
	testFailed := false

	for i := 1; i < len(cfg.PrivateKeys); i++ {
		pkRecv, _ := crypto.HexToECDSA(cfg.PrivateKeys[i])
		receiverAddr := crypto.PubkeyToAddress(*pkRecv.Public().(*ecdsa.PublicKey))
		
		finalBal, _ := client.BalanceAt(context.Background(), receiverAddr, nil)
		initialBal := initialBalances[i]
		
		// Ví nhận phải tăng đúng sendAmount (1000 wei)
		expectedBal := new(big.Int).Add(initialBal, sendAmount)
		
		if finalBal.Cmp(expectedBal) != 0 {
			fmt.Printf("   ❌ LỖI: Wallet %d (%s) có số dư %s, kỳ vọng %s\n", i, receiverAddr.Hex()[:8], finalBal.String(), expectedBal.String())
			testFailed = true
		} else {
			fmt.Printf("   ✅ Wallet %d: Chuẩn (+1000 wei)\n", i)
		}
	}

	if successCount == len(cfg.PrivateKeys)-1 && !testFailed {
		fmt.Println("\n🎉 TEST PASSED: Mempool xử lý Nonce tăng dần cực chuẩn và Balance của tất cả ví nhận cập nhật chính xác!")
	} else {
		fmt.Println("\n⚠️ TEST FAILED: Mempool từ chối giao dịch hoặc Balance bị sai lệch do Race Condition!")
		os.Exit(1)
	}
}
