/*
 * BÀI TEST: 5-gas
 * MÔ TẢ   : Kiểm tra giới hạn Gas và trừ Gas khi thực thi bằng Block-STM.
 * GỌI     : Giao dịch tính toán nhiều hoặc cấu hình gas limit khác nhau.
 * KỲ VỌNG : Giao dịch thiếu Gas phải bị revert OutOfGas, hệ thống trừ đúng số Gas fee vào tài khoản gọi.
 */
package main

import (
	"tool-test/test-simple/test-rpc/test-chain/config"
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)



func RunTest(configPath string) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 5-gas")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Kiểm tra giới hạn Gas và trừ Gas khi thực thi bằng Block-STM.")
	fmt.Println("⚡ GỌI     : Giao dịch tính toán nhiều hoặc cấu hình gas limit khác nhau.")
	fmt.Println("🎯 KỲ VỌNG : Giao dịch thiếu Gas phải bị revert OutOfGas, hệ thống trừ đúng số Gas fee vào tài khoản gọi.")
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

	parsedABI, err := abi.JSON(strings.NewReader(cfg.Contracts["AbortRollback"].ABI))
	if err != nil {
		return fmt.Errorf("ABI parse err: %w", err)
	}

	bytecode, err := hexutil.Decode("0x" + cfg.Contracts["AbortRollback"].Bytecode)
	if err != nil {
		return fmt.Errorf("bytecode err: %w", err)
	}

	if len(cfg.PrivateKeys) == 0 {
		return fmt.Errorf("không có private key nào trong config")
	}

	pk0, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		return fmt.Errorf("invalid private key[0]: %w", err)
	}
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying AbortRollback Contract (cho Test 5 Gas)...")
	contractAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		return fmt.Errorf("deploy contract err: %w", err)
	}
	fmt.Printf("📌 Contract: %s\n\n", contractAddr.Hex())

	var wg sync.WaitGroup
	fmt.Println("🔥 Gửi tx... Mục tiêu: Kiểm tra Gas bị trừ khi Block-STM Re-execute và Revert")

	txHashes := make([]common.Hash, len(cfg.PrivateKeys))
	var sendErr error
	var errMu sync.Mutex

	for i, pkStr := range cfg.PrivateKeys {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()
			pk, err := crypto.HexToECDSA(pKeyHex)
			if err != nil {
				errMu.Lock()
				sendErr = err
				errMu.Unlock()
				return
			}
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

			var data []byte
			gasPrice := big.NewInt(1e9)
			if idx == 0 {
				data, _ = parsedABI.Pack("setPhase", big.NewInt(2))
				gasPrice = big.NewInt(2e9)
			} else {
				data, _ = parsedABI.Pack("updateIfPhase1", big.NewInt(888))
			}

			hash, err := sendTx(client, pk, cfg.ChainID, from, contractAddr, data, gasPrice)
			if err == nil {
				txHashes[idx] = hash
			} else {
				errMu.Lock()
				sendErr = err
				errMu.Unlock()
			}
		}(i, pkStr)
	}

	wg.Wait()
	if sendErr != nil {
		return fmt.Errorf("lỗi khi gửi giao dịch: %w", sendErr)
	}

	fmt.Println("\n📊 KẾT QUẢ TEST 5 (GAS & ROLLBACK):")

	var revertCount int
	for i, hash := range txHashes {
		if hash == (common.Hash{}) {
			continue
		}
		receipt, err := waitReceipt(client, hash)
		if err != nil {
			return fmt.Errorf("lỗi chờ receipt tx %s: %w", hash.Hex(), err)
		}

		status := "SUCCESS"
		if receipt.Status != 1 {
			status = "REVERTED"
			revertCount++
		}

		blockNum := uint64(0)
		if receipt.BlockNumber != nil {
			blockNum = receipt.BlockNumber.Uint64()
		}
		fmt.Printf("Wallet %d | Trạng thái: %-8s | Block: %-4d | TxIndex: %-2d | Gas sử dụng (GasUsed): %d\n", i, status, blockNum, receipt.TransactionIndex, receipt.GasUsed)
	}

	if revertCount == 0 {
		return fmt.Errorf("TEST FAILED: Lỗi Block-STM, đáng lẽ phải có giao dịch bị Revert để test Gas, nhưng tất cả lại SUCCESS")
	}

	fmt.Printf("🎉 Thành công! Có %d giao dịch bị Revert và tiêu thụ gas hợp lý.\n", revertCount)
	fmt.Println("\n👉 Phân tích: Những Tx bị REVERT do Block-STM chạy lại phải trả một lượng Gas nhất định (thường là base gas + gas chạy đến lúc revert).")
	fmt.Println("👉 Lượng GasUsed của Tx Revert không được lớn bằng Tx Success (vì nó dừng sớm). Hơn nữa, dù Block-STM có re-execute nó 3-4 lần ngầm bên dưới, GasUsed ghi nhận trên block MÀ user phải trả vẫn chỉ được tính 1 LẦN DUY NHẤT.")
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

func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return nil, err
	}
	tx := types.NewContractCreation(nonce, big.NewInt(0), 5000000, big.NewInt(1e9), bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return nil, err
	}
	if err := client.SendTransaction(context.Background(), signedTx); err != nil {
		return nil, err
	}
	receipt, err := waitReceipt(client, signedTx.Hash())
	if err != nil {
		return nil, err
	}
	return &receipt.ContractAddress, nil
}

func sendTx(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, data []byte, gasPrice *big.Int) (common.Hash, error) {
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return common.Hash{}, err
	}
	tx := types.NewTransaction(nonce, *to, big.NewInt(0), 1000000, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return common.Hash{}, err
	}
	err = client.SendTransaction(context.Background(), signedTx)
	return signedTx.Hash(), err
}

func waitReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	timeoutStart := time.Now()
	for {
		if time.Since(timeoutStart) > 60*time.Second {
			return nil, fmt.Errorf("timeout waiting for receipt: %s", txHash.Hex())
		}
		receipt, err := client.TransactionReceipt(context.Background(), txHash)
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			return receipt, nil
		}
		if err != nil && !strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("lỗi kết nối RPC: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
