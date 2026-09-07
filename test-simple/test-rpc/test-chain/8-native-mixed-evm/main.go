/*
 * BÀI TEST: 8-native-mixed-evm
 * MÔ TẢ   : Đan xen các giao dịch chuyển Native Token và các giao dịch gọi Smart Contract (EVM).
 * GỌI     : Chuyển tiền Native và gọi Smart Contract xen kẽ trong cùng batch.
 * KỲ VỌNG : Cả hai loại giao dịch được xử lý song song và an toàn, không bị ảnh hưởng (side-effect) chéo.
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

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Dùng chung contract ReadWriteConflict từ thư mục 2-read-write


func RunTest(configPath string) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 8-native-mixed-evm")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Đan xen các giao dịch chuyển Native Token và các giao dịch gọi Smart Contract (EVM).")
	fmt.Println("⚡ GỌI     : Chuyển tiền Native và gọi Smart Contract xen kẽ trong cùng batch.")
	fmt.Println("🎯 KỲ VỌNG : Cả hai loại giao dịch được xử lý song song và an toàn, không bị ảnh hưởng (side-effect) chéo.")
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

	if len(cfg.PrivateKeys) < 4 {
		return fmt.Errorf("cần ít nhất 4 private keys để test")
	}

	parsedABI, err := abi.JSON(strings.NewReader(cfg.Contracts["DepositContract"].ABI))
	if err != nil {
		return fmt.Errorf("ABI parse err: %w", err)
	}
	bytecode, err := hexutil.Decode("0x" + cfg.Contracts["DepositContract"].Bytecode)
	if err != nil {
		return fmt.Errorf("bytecode err: %w", err)
	}

	pk0, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("invalid private key[0]: %w", err)
	}
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying EVM DepositContract...")
	contractAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		return fmt.Errorf("deploy thất bại: %w", err)
	}
	fmt.Printf("📌 Contract deployed at: %s\n\n", contractAddr.Hex())

	pkRecv, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("invalid receiver key: %w", err)
	}
	receiverAddr := crypto.PubkeyToAddress(*pkRecv.Public().(*ecdsa.PublicKey))

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	txHashes := make([]common.Hash, len(cfg.PrivateKeys)-1)
	sendAmountNative := big.NewInt(1000)
	sendAmountEVM := big.NewInt(1000000)

	initialContractBal, err := client.BalanceAt(context.Background(), *contractAddr, nil)
	if err != nil {
		return fmt.Errorf("lỗi lấy initialContractBal: %w", err)
	}
	initialReceiverBal, err := client.BalanceAt(context.Background(), receiverAddr, nil)
	if err != nil {
		return fmt.Errorf("lỗi lấy initialReceiverBal: %w", err)
	}

	fmt.Println("🔥 Bắt đầu test Mixed Block-STM: EVM Contract Call (gửi tiền) trộn lẫn với Native Transfer...")
	start := time.Now()

	for i := 1; i < len(cfg.PrivateKeys); i++ {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()
			pk, err := crypto.HexToECDSA(pKeyHex)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("Wallet %d invalid key: %v", idx, err))
				errsMu.Unlock()
				return
			}
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))
			nonce, err := client.PendingNonceAt(context.Background(), from)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("Wallet %d nonce err: %v", idx, err))
				errsMu.Unlock()
				return
			}
			gasPrice, _ := client.SuggestGasPrice(context.Background())
			if gasPrice == nil {
				gasPrice = big.NewInt(1000000000)
			}

			var signedTx *types.Transaction

			if idx == 1 {
				data, err := parsedABI.Pack("deposit")
				if err != nil {
					errsMu.Lock()
					errs = append(errs, fmt.Errorf("Wallet %d pack deposit: %v", idx, err))
					errsMu.Unlock()
					return
				}
				gasLimit := uint64(100000)
				tx := types.NewTransaction(nonce, *contractAddr, sendAmountEVM, gasLimit, gasPrice, data)
				signedTx, err = types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)
				if err != nil {
					errsMu.Lock()
					errs = append(errs, fmt.Errorf("Wallet %d sign tx: %v", idx, err))
					errsMu.Unlock()
					return
				}
				fmt.Printf("⏳ Wallet %d đang gửi tx: EVM Contract Call deposit() + %s wei...\n", idx, sendAmountEVM.String())
			} else {
				gasLimit := uint64(21000)
				tx := types.NewTransaction(nonce, receiverAddr, sendAmountNative, gasLimit, gasPrice, nil)
				signedTx, err = types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)
				if err != nil {
					errsMu.Lock()
					errs = append(errs, fmt.Errorf("Wallet %d sign tx: %v", idx, err))
					errsMu.Unlock()
					return
				}
				fmt.Printf("⏳ Wallet %d đang gửi tx: NATIVE Transfer %s wei...\n", idx, sendAmountNative.String())
			}

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
		return fmt.Errorf("có lỗi gửi giao dịch: %v", errs[0])
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
					return fmt.Errorf("tx %s bị revert", hash.Hex())
				}
				fmt.Printf("✅ Tx %s confirmed trong block %d\n", hash.Hex()[:10]+"...", receipt.BlockNumber.Uint64())
				break
			}
			if err != nil && !strings.Contains(err.Error(), "not found") {
				return fmt.Errorf("lỗi kết nối RPC: %w", err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	elapsed := time.Since(start)

	finalContractBal, err := client.BalanceAt(context.Background(), *contractAddr, nil)
	if err != nil {
		return fmt.Errorf("lỗi lấy finalContractBal: %w", err)
	}
	finalReceiverBal, err := client.BalanceAt(context.Background(), receiverAddr, nil)
	if err != nil {
		return fmt.Errorf("lỗi lấy finalReceiverBal: %w", err)
	}

	expectedContractBal := new(big.Int).Add(initialContractBal, sendAmountEVM)
	expectedReceiverBal := new(big.Int).Add(initialReceiverBal, new(big.Int).Mul(sendAmountNative, big.NewInt(int64(len(cfg.PrivateKeys)-2))))

	fmt.Println("\n📊 KẾT QUẢ MIXED NATIVE + EVM BLOCK-STM:")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)

	testFailed := false

	fmt.Printf("\n🔍 KIỂM TOÁN SỐ DƯ CONTRACT:\n")
	if finalContractBal.Cmp(expectedContractBal) != 0 {
		fmt.Printf("   ❌ LỖI: Contract Balance thực tế = %s, kỳ vọng = %s\n", finalContractBal.String(), expectedContractBal.String())
		testFailed = true
	} else {
		fmt.Printf("   ✅ Contract Balance cập nhật CHUẨN XÁC: %s wei\n", finalContractBal.String())
	}

	fmt.Printf("\n🔍 KIỂM TOÁN SỐ DƯ VÍ NHẬN NATIVE:\n")
	if finalReceiverBal.Cmp(expectedReceiverBal) != 0 {
		fmt.Printf("   ❌ LỖI: Receiver Balance thực tế = %s, kỳ vọng = %s\n", finalReceiverBal.String(), expectedReceiverBal.String())
		testFailed = true
	} else {
		fmt.Printf("   ✅ Receiver Balance cập nhật CHUẨN XÁC: %s wei\n", finalReceiverBal.String())
	}

	if testFailed {
		return fmt.Errorf("TEST FAILED: Block-STM xử lý sai lệch số dư khi trộn lẫn Native Transfer và EVM Call")
	}

	fmt.Println("\n🎉 TEST PASSED: Block-STM xử lý chuẩn xác cả giao dịch Native và Smart Contract xen kẽ, không có Race Condition về Balance!")
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

// Helper deploy
func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return nil, err
	}
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil {
		gasPrice = big.NewInt(1000000000)
	}
	gasLimit := uint64(5000000)

	tx := types.NewContractCreation(nonce, big.NewInt(0), gasLimit, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return nil, err
	}

	if err := client.SendTransaction(context.Background(), signedTx); err != nil {
		return nil, err
	}

	timeoutStart := time.Now()
	for {
		if time.Since(timeoutStart) > 60*time.Second {
			return nil, fmt.Errorf("timeout waiting for deploy receipt: %s", signedTx.Hash().Hex())
		}
		receipt, err := client.TransactionReceipt(context.Background(), signedTx.Hash())
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			if receipt.Status != 1 {
				return nil, fmt.Errorf("deploy reverted")
			}
			return &receipt.ContractAddress, nil
		}
		if err != nil && !strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("lỗi kết nối RPC: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
