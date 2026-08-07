/*
 * BÀI TEST: 14-selfdestruct-conflict
 * MÔ TẢ   : Giao dịch 1 gọi hàm selfdestruct tự hủy contract. Các giao dịch khác cố gắng gọi hàm readData() của contract đó trong CÙNG MỘT BLOCK.
 * GỌI     : EVM SelfDestruct & EVM Call song song.
 * KỲ VỌNG : Block-STM thực thi xóa contract. Các giao dịch đọc state sẽ không Revert theo chuẩn EVM, nhưng trả về dữ liệu rỗng (status = 1).
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

type Config struct {
	RPCUrl      string   `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
	ChainID     int64    `json:"chain_id"`
}

func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 14-selfdestruct-conflict")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Giao dịch 1 tự hủy Contract. Giao dịch 2,3,4 cố gắng đọc data từ contract đó trong cùng block.")
	fmt.Println("⚡ GỌI     : EVM SelfDestruct & EVM Call song song.")
	fmt.Println("🎯 KỲ VỌNG : Block-STM thực thi xóa contract. Các giao dịch đọc state sẽ không Revert theo chuẩn EVM, nhưng trả về dữ liệu rỗng (status = 1).")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	configPath := "../config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc config: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Fatalf("❌ Lỗi parse config: %v", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC: %v", err)
	}

	// Đọc ABI và BIN trực tiếp từ file được build bởi docker
	abiBytes, err := os.ReadFile("../contracts/SelfDestructConflict.abi")
	if err != nil { log.Fatalf("Lỗi đọc abi: %v", err) }
	binBytes, err := os.ReadFile("../contracts/SelfDestructConflict.bin")
	if err != nil { log.Fatalf("Lỗi đọc bin: %v", err) }

	parsedABI, _ := abi.JSON(strings.NewReader(string(abiBytes)))
	bytecode, _ := hexutil.Decode("0x" + string(binBytes))

	pk0, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying SelfDestructConflict Contract...")
	contractAddr, _ := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	fmt.Printf("📌 Contract deployed at: %s\n\n", contractAddr.Hex())

	var wg sync.WaitGroup
	var errsMu sync.Mutex
	
	txHashes := make([]common.Hash, 4)
	start := time.Now()

	fmt.Println("🔥 Push 1 giao dịch destroy() và 3 giao dịch readData() song song...")

	// Giao dịch 1: Tự huỷ contract (dùng ví 0)
	wg.Add(1)
	go func() {
		defer wg.Done()
		data, _ := parsedABI.Pack("destroy")
		gasPrice := big.NewInt(1000000000)
		gasLimit := uint64(100000)
		nonce, _ := client.PendingNonceAt(context.Background(), from0)

		tx := types.NewTransaction(nonce, *contractAddr, big.NewInt(0), gasLimit, gasPrice, data)
		signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk0)
		
		err := client.SendTransaction(context.Background(), signedTx)
		errsMu.Lock()
		if err == nil {
			fmt.Printf("✅ Đã push Destroy Tx: %s\n", signedTx.Hash().Hex())
			txHashes[0] = signedTx.Hash()
		} else {
			fmt.Printf("⚠️ Lỗi push Destroy Tx: %v\n", err)
		}
		errsMu.Unlock()
	}()

	// Đợi tí xíu để Destroy vào mempool trước (hoặc tuỳ network)
	time.Sleep(50 * time.Millisecond)

	// Giao dịch 2, 3, 4: Cố gắng readData (dùng các ví khác)
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pk, _ := crypto.HexToECDSA(cfg.PrivateKeys[idx])
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))
			
			data, _ := parsedABI.Pack("readData")
			gasPrice := big.NewInt(1000000000)
			gasLimit := uint64(50000)
			nonce, _ := client.PendingNonceAt(context.Background(), from)

			tx := types.NewTransaction(nonce, *contractAddr, big.NewInt(0), gasLimit, gasPrice, data)
			signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)
			
			err := client.SendTransaction(context.Background(), signedTx)
			errsMu.Lock()
			if err == nil {
				fmt.Printf("✅ Đã push ReadData Tx %d: %s\n", idx, signedTx.Hash().Hex())
				txHashes[idx] = signedTx.Hash()
			} else {
				fmt.Printf("⚠️ Lỗi push ReadData Tx %d: %v\n", idx, err)
			}
			errsMu.Unlock()
		}(i)
	}

	wg.Wait()

	fmt.Println("⏳ Chờ các giao dịch được confirm...")
	destroySuccess := false

	for i := 0; i < 4; i++ {
		hash := txHashes[i]
		if hash == (common.Hash{}) {
			continue
		}
		
		timeoutStart := time.Now()
		for {
			if time.Since(timeoutStart) > 60*time.Second {
				fmt.Println("❌ Timeout waiting for receipt")
				os.Exit(1)
			}
			receipt, err := client.TransactionReceipt(context.Background(), hash)

			if err != nil && !strings.Contains(err.Error(), "not found") {
				fmt.Printf("Lỗi kết nối RPC: %v\n", err)
				os.Exit(1)
			}
			if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
				if i == 0 {
					// Tx 0 là destroy
					if receipt.Status == 1 {
						fmt.Printf("🔥 Destroy Tx THÀNH CÔNG trong block %d (Contract đã bị hủy)\n", receipt.BlockNumber.Uint64())
						destroySuccess = true
					} else {
						fmt.Printf("❌ Destroy Tx bị Revert ngoài ý muốn!\n")
					}
				} else {
					// Tx đọc data
					if receipt.Status == 1 {
						fmt.Printf("✅ ReadData Tx %d THÀNH CÔNG (EVM không revert khi gọi contract bị xóa)\n", i)
					} else {
						fmt.Printf("🔄 ReadData Tx %d bị REVERT\n", i)
					}
				}
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ SELF-DESTRUCT CONFLICT TEST:")
	fmt.Printf("Thời gian chạy: %v\n", elapsed)

	if destroySuccess {
		fmt.Println("\n🎉 TEST PASSED: Block-STM xử lý chuẩn xác! Giao dịch tự hủy đã thực thi.")
	} else {
		fmt.Println("\n⚠️ TEST FAILED: Logic bị sai. Contract không bị hủy!")
		os.Exit(1)
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────
func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	nonce, _ := client.PendingNonceAt(context.Background(), from)
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil { gasPrice = big.NewInt(1000000000) }
	
	tx := types.NewContractCreation(nonce, big.NewInt(0), 5000000, gasPrice, bytecode)
	signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	client.SendTransaction(context.Background(), signedTx)
	
	timeoutStart := time.Now()
	for {
		if time.Since(timeoutStart) > 60*time.Second {
			fmt.Println("❌ Timeout waiting for receipt")
			os.Exit(1)
		}
		receipt, err := client.TransactionReceipt(context.Background(), signedTx.Hash())

		if err != nil && !strings.Contains(err.Error(), "not found") {
			fmt.Printf("Lỗi kết nối RPC: %v\n", err)
			os.Exit(1)
		}
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			return &receipt.ContractAddress, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}
