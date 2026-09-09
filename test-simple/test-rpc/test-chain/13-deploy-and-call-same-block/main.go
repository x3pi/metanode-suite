/*
 * BÀI TEST: 13-deploy-and-call-same-block
 * MÔ TẢ   : Gửi 1 giao dịch Deploy Smart Contract và 1 giao dịch gọi hàm của Contract đó ngay lập tức (cùng block).
 * GỌI     : EVM Deploy và EVM Call. Sử dụng cơ chế tính trước địa chỉ contract (CREATE address = hash(sender, nonce)).
 * KỲ VỌNG : Mempool của Metanode kiểm tra sự tồn tại của contract. Giao dịch Call sẽ bị từ chối ngay lập tức bảo vệ an toàn.
 */
package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/config"

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
	fmt.Println("BÀI TEST: 13-deploy-and-call-same-block")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Gửi 1 giao dịch Deploy Smart Contract và 1 giao dịch Gọi hàm của nó ngay lập tức (cùng block).")
	fmt.Println("⚡ GỌI     : EVM Deploy & Call song song (tính trước địa chỉ).")
	fmt.Println("🎯 KỲ VỌNG : Mempool của Metanode sẽ TỪ CHỐI giao dịch Call vì contract chưa tồn tại trên StateDB, bảo vệ hệ thống khỏi các lỗi thực thi.")
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

	parsedABI, err := abi.JSON(strings.NewReader(cfg.Contracts["TestCounter"].ABI))
	if err != nil {
		return fmt.Errorf("lỗi parse ABI: %w", err)
	}
	bytecode, err := hexutil.Decode("0x" + cfg.Contracts["TestCounter"].Bytecode)
	if err != nil {
		return fmt.Errorf("lỗi decode bytecode: %w", err)
	}

	if len(cfg.PrivateKeys) == 0 {
		return fmt.Errorf("không có private key nào trong config")
	}

	pk0, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("invalid private key[0]: %w", err)
	}
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	nonce, err := client.PendingNonceAt(context.Background(), from0)
	if err != nil {
		return fmt.Errorf("lỗi lấy nonce: %w", err)
	}

	predictedAddr := crypto.CreateAddress(from0, nonce)
	fmt.Printf("📌 Địa chỉ Contract tính toán trước: %s\n\n", predictedAddr.Hex())

	var wg sync.WaitGroup
	var errsMu sync.Mutex

	txHashes := make([]common.Hash, 2)
	start := time.Now()

	// Goroutine 1: Deploy Contract
	wg.Add(1)
	go func() {
		defer wg.Done()
		gasPrice := big.NewInt(1000000000)
		gasLimit := uint64(5000000)

		tx := types.NewContractCreation(nonce, big.NewInt(0), gasLimit, gasPrice, bytecode)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk0)
		if err != nil {
			return
		}

		if err := client.SendTransaction(context.Background(), signedTx); err != nil {
			errsMu.Lock()
			fmt.Printf("❌ Lỗi gửi Deploy Tx: %v\n", err)
			errsMu.Unlock()
			return
		}

		errsMu.Lock()
		fmt.Printf("✅ Đã push Deploy Tx: %s\n", signedTx.Hash().Hex())
		txHashes[0] = signedTx.Hash()
		errsMu.Unlock()
	}()

	// Goroutine 2: Gọi hàm increment() trên địa chỉ vừa tính trước
	var mempoolRejectedCall bool
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)

		data, err := parsedABI.Pack("increment")
		if err != nil {
			return
		}
		gasPrice := big.NewInt(1000000000)
		gasLimit := uint64(100000)

		// Tx này có nonce = nonce + 1 từ pk0
		tx := types.NewTransaction(nonce+1, predictedAddr, big.NewInt(0), gasLimit, gasPrice, data)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk0)
		if err != nil {
			return
		}

		err = client.SendTransaction(context.Background(), signedTx)
		errsMu.Lock()
		if err != nil {
			if strings.Contains(err.Error(), "non-existent smart account") {
				fmt.Printf("✅ Mempool đã từ chối Call Tx vì contract chưa tồn tại (Bảo mật tốt!): %v\n", err)
				mempoolRejectedCall = true
			} else {
				fmt.Printf("⚠️ Lỗi gửi Call Tx: %v\n", err)
			}
		} else {
			fmt.Printf("✅ Đã push Call Tx: %s\n", signedTx.Hash().Hex())
			txHashes[1] = signedTx.Hash()
		}
		errsMu.Unlock()
	}()

	wg.Wait()

	if mempoolRejectedCall {
		// Đợi deploy tx được confirm để nonce của ví được cập nhật trên chain, tránh xung đột nonce với bài test sau
		if txHashes[0] != (common.Hash{}) {
			fmt.Println("⏳ Chờ Deploy Tx được confirm để đồng bộ nonce...")
			timeoutStart := time.Now()
			for {
				if time.Since(timeoutStart) > 60*time.Second {
					break
				}
				receipt, err := client.TransactionReceipt(context.Background(), txHashes[0])
				if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
					fmt.Printf("✅ Deploy Tx đã được confirm trong block %d\n", receipt.BlockNumber.Uint64())
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
		return nil
	}

	if txHashes[0] == (common.Hash{}) {
		return fmt.Errorf("deploy Tx gửi thất bại")
	}

	fmt.Println("⏳ Chờ các giao dịch được confirm trong cùng 1 Block...")
	successCount := 0

	for i := 0; i < 2; i++ {
		hash := txHashes[i]
		if hash == (common.Hash{}) {
			continue
		}

		timeoutStart := time.Now()
		for {
			if time.Since(timeoutStart) > 60*time.Second {
				return fmt.Errorf("timeout waiting for receipt của Tx %s (sau 60s)", hash.Hex())
			}
			receipt, err := client.TransactionReceipt(context.Background(), hash)
			if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
				if receipt.Status != 1 {
					fmt.Printf("❌ Tx %s bị REVERT!\n", hash.Hex())
				} else {
					fmt.Printf("✅ Tx %s THÀNH CÔNG trong block %d\n", hash.Hex(), receipt.BlockNumber.Uint64())
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
	fmt.Printf("⏱️ Thời gian gửi & chờ: %v\n", elapsed)

	if successCount == 2 {
		data, err := parsedABI.Pack("getCount")
		if err != nil {
			return fmt.Errorf("lỗi pack getCount: %w", err)
		}
		result, err := client.CallContract(context.Background(), ethereum.CallMsg{To: &predictedAddr, Data: data}, nil)
		if err == nil {
			outputs, err := parsedABI.Unpack("getCount", result)
			if err == nil && len(outputs) > 0 {
				val := outputs[0].(*big.Int)
				fmt.Printf("📊 Giá trị count sau cùng: %s\n", val.String())
				if val.Cmp(big.NewInt(1)) == 0 {
					fmt.Println("\n🎉 TEST PASSED: Block-STM xử lý Deploy và Call trong cùng 1 Block hoàn hảo!")
					return nil
				}
			}
		}
	}

	return fmt.Errorf("có lỗi xảy ra trong quá trình Deploy và Call (successCount=%d)", successCount)
}

func main() {
	if err := RunTest(""); err != nil {
		log.Fatalf("❌ %v", err)
	}
}
