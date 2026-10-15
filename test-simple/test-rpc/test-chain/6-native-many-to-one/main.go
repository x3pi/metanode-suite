/*
 * BÀI TEST: 6-native-many-to-one
 * MÔ TẢ   : Nhiều tài khoản khác nhau cùng chuyển Native Token vào chung MỘT tài khoản nhận.
 * GỌI     : Chuyển tiền Native (Coin) từ nhiều ví -> 1 ví duy nhất.
 * KỲ VỌNG : Xung đột trên tài khoản nhận (cộng dồn số dư). Số dư cuối cùng của người nhận phải bằng tổng số dư ban đầu cộng tổng số tiền đã nhận.
 */
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
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

	// Chọn ví 0 làm đích nhận tiền (vì đã được đăng ký trên cluster, và không gửi tx để không bị trừ gas)
	pkRecv, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("invalid receiver private key[0]: %w", err)
	}
	receiverAddr := crypto.PubkeyToAddress(*pkRecv.Public().(*ecdsa.PublicKey))

	senderKeys := cfg.PrivateKeys[1:]
	fmt.Printf("🚀 Mục tiêu: %d ví gửi tiền ĐỒNG THỜI đến 1 ví nhận đã đăng ký: %s\n", len(senderKeys), receiverAddr.Hex())

	initialBalance, err := client.BalanceAt(context.Background(), receiverAddr, nil)
	if err != nil {
		return fmt.Errorf("lỗi lấy initial balance: %w", err)
	}
	fmt.Printf("💰 Số dư ban đầu của ví nhận: %s wei\n\n", initialBalance.String())

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	txHashes := make([]common.Hash, len(senderKeys))
	sendAmount := big.NewInt(1000) // Gửi 1000 wei mỗi ví

	fmt.Printf("🔥 Gửi %d giao dịch Native Transfer đồng thời...\n", len(senderKeys))
	start := time.Now()

	for i := 0; i < len(senderKeys); i++ {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()
			pk, err := crypto.HexToECDSA(pKeyHex)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi ví %d key: %v", idx+1, err))
				errsMu.Unlock()
				return
			}
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

			nonce, err := client.PendingNonceAt(context.Background(), from)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi ví %d nonce: %v", idx+1, err))
				errsMu.Unlock()
				return
			}

			gasLimit := uint64(21000)
			gasPrice := big.NewInt(1e9)

			tx := types.NewTransaction(nonce, receiverAddr, sendAmount, gasLimit, gasPrice, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi ví %d sign: %v", idx+1, err))
				errsMu.Unlock()
				return
			}

			if err := client.SendTransaction(context.Background(), signedTx); err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi ví %d send: %v", idx+1, err))
				errsMu.Unlock()
				return
			}

			fmt.Printf("✅ Wallet %d gửi tx thành công: %s\n", idx+1, signedTx.Hash().Hex())
			txHashes[idx] = signedTx.Hash()
		}(i, senderKeys[i])
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
					return fmt.Errorf("wallet %d Tx bị revert: %s", i+1, hash.Hex())
				}
				fmt.Printf("✅ Wallet %d Tx %s confirmed trong block %d\n", i+1, hash.Hex()[:10]+"...", receipt.BlockNumber.Uint64())
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

	expectedAdded := new(big.Int).Mul(sendAmount, big.NewInt(int64(len(senderKeys))))
	expectedFinal := new(big.Int).Add(initialBalance, expectedAdded)

	fmt.Println("\n📊 KẾT QUẢ NATIVE TRANSFER (MANY-TO-ONE):")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Số dư ban đầu: %s wei\n", initialBalance.String())
	fmt.Printf("Số dư cuối cùng: %s wei\n", finalBalance.String())
	fmt.Printf("Kỳ vọng: %s wei\n", expectedFinal.String())

	if finalBalance.Cmp(expectedFinal) != 0 {
		return fmt.Errorf("TEST FAILED: Sai lệch Balance! Khả năng xảy ra race condition (Fast-Path bug)")
	}

	fmt.Println("🎉 Concurrent Native Transfer thành công: Balance ví nhận cập nhật chính xác tuyệt đối!")

	// =========================================================================
	// CASE KIỂM THỬ ACCOUNT GATE: Chuyển tiền tới tài khoản CHƯA đăng ký trên cụm
	// Kỳ vọng: Hệ thống (Account Gate) phải từ chối và báo lỗi chưa đăng ký
	// =========================================================================
	fmt.Println("\n🛡️ [CASE KIỂM THỬ ACCOUNT GATE] Chuyển tiền tới tài khoản CHƯA đăng ký trên cụm...")
	unregisteredAddr := common.HexToAddress("0x0000000000000000000000000000000000000088")
	pkSender, err := crypto.HexToECDSA(senderKeys[0])
	if err != nil {
		return fmt.Errorf("invalid sender key: %w", err)
	}
	senderAddr := crypto.PubkeyToAddress(*pkSender.Public().(*ecdsa.PublicKey))

	unregNonce, err := client.PendingNonceAt(context.Background(), senderAddr)
	if err != nil {
		return fmt.Errorf("lỗi lấy nonce ví kiểm tra gate: %w", err)
	}

	unregTx := types.NewTransaction(unregNonce, unregisteredAddr, sendAmount, uint64(21000), big.NewInt(1e9), nil)
	signedUnregTx, err := types.SignTx(unregTx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pkSender)
	if err != nil {
		return fmt.Errorf("lỗi ký transaction unreg: %w", err)
	}

	sendErr := client.SendTransaction(context.Background(), signedUnregTx)

	// Kiểm tra xem chain có bật Account Gate hay không qua RPC
	rpcClient := &http.Client{Timeout: 5 * time.Second}
	hasGate := false
	type rpcResp struct {
		Result struct {
			AccountGate bool `json:"accountGate"`
		} `json:"result"`
	}
	reqBody := []byte(`{"jsonrpc":"2.0","method":"mtn_getClusterIdentity","params":[],"id":1}`)
	if resp, err := rpcClient.Post(cfg.RPCUrl, "application/json", bytes.NewBuffer(reqBody)); err == nil {
		var r rpcResp
		if b, err := io.ReadAll(resp.Body); err == nil {
			_ = json.Unmarshal(b, &r)
			hasGate = r.Result.AccountGate
		}
		_ = resp.Body.Close()
	}

	if sendErr == nil {
		if hasGate {
			return fmt.Errorf("TEST FAILED: Cụm bật Account Gate nhưng cho phép chuyển tiền tới ví chưa đăng ký (%s)", unregisteredAddr.Hex())
		}
		fmt.Printf("   ℹ️ Node cho phép gửi tiền (Account Gate không kích hoạt trên chain này)\n")
	} else {
		errStr := sendErr.Error()
		if strings.Contains(errStr, "recipient not registered") || strings.Contains(errStr, "not registered") {
			fmt.Printf("   ✅ ĐÚNG KỲ VỌNG: Node từ chối chuyển tiền tới ví chưa đăng ký (%s):\n      👉 Lỗi nhận được: \"%v\"\n", unregisteredAddr.Hex(), sendErr)
		} else {
			return fmt.Errorf("giao dịch bị từ chối nhưng lỗi không mong muốn: %w", sendErr)
		}
	}

	fmt.Println("🎉 TEST PASSED: Toàn bộ kiểm thử Native Transfer (Many-to-One) và Account Gate hoàn tất xuất sắc!")
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
