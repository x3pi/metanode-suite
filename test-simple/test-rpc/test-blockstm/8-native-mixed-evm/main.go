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

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Dùng chung contract ReadWriteConflict từ thư mục 2-read-write
type ContractData struct {
	ABI      string `json:"abi"`
	Bytecode string `json:"bytecode"`
}

type Config struct {
	RPCUrl      string                  `json:"rpc_url"`
	PrivateKeys []string                `json:"private_keys"`
	ChainID     int64                   `json:"chain_id"`
	Contracts   map[string]ContractData `json:"contracts"`
}

func main() {
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

	if len(cfg.PrivateKeys) < 4 {
		log.Fatalf("❌ Cần ít nhất 4 private keys để test")
	}

	parsedABI, err := abi.JSON(strings.NewReader(cfg.Contracts["DepositContract"].ABI))
	if err != nil { log.Fatalf("ABI parse err: %v", err) }
	bytecode, err := hexutil.Decode("0x" + cfg.Contracts["DepositContract"].Bytecode)
	if err != nil { log.Fatalf("Bytecode err: %v", err) }

	pk0, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying EVM DepositContract...")
	contractAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		log.Fatalf("❌ Deploy thất bại: %v", err)
	}
	fmt.Printf("📌 Contract deployed at: %s\n\n", contractAddr.Hex())

	pkRecv, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	receiverAddr := crypto.PubkeyToAddress(*pkRecv.Public().(*ecdsa.PublicKey))

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	txHashes := make([]common.Hash, len(cfg.PrivateKeys)-1)
	sendAmountNative := big.NewInt(1000)
	sendAmountEVM := big.NewInt(1000000)

	initialContractBal, _ := client.BalanceAt(context.Background(), *contractAddr, nil)
	initialReceiverBal, _ := client.BalanceAt(context.Background(), receiverAddr, nil)

	fmt.Println("🔥 Bắt đầu test Mixed Block-STM: EVM Contract Call (gửi tiền) trộn lẫn với Native Transfer...")
	start := time.Now()

	// Ví 1 gửi giao dịch EVM gọi hàm deposit() kèm theo 1,000,000 wei
	// Các ví còn lại gửi giao dịch Native Transfer 1000 wei
	for i := 1; i < len(cfg.PrivateKeys); i++ {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()
			pk, _ := crypto.HexToECDSA(pKeyHex)
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))
			nonce, _ := client.PendingNonceAt(context.Background(), from)
			gasPrice, _ := client.SuggestGasPrice(context.Background())
			if gasPrice == nil { gasPrice = big.NewInt(1000000000) }

			var signedTx *types.Transaction

			if idx == 1 {
				// Gửi EVM Contract Call kèm tiền (payable)
				data, _ := parsedABI.Pack("deposit")
				gasLimit := uint64(100000)
				tx := types.NewTransaction(nonce, *contractAddr, sendAmountEVM, gasLimit, gasPrice, data)
				signedTx, _ = types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)
				fmt.Printf("⏳ Wallet %d đang gửi tx: EVM Contract Call deposit() + %s wei...\n", idx, sendAmountEVM.String())
			} else {
				// Gửi Native Transfer
				gasLimit := uint64(21000)
				tx := types.NewTransaction(nonce, receiverAddr, sendAmountNative, gasLimit, gasPrice, nil)
				signedTx, _ = types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)
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
		fmt.Println("❌ Một số giao dịch gửi thất bại:")
		for _, e := range errs {
			fmt.Println("  -", e)
		}
	}

	fmt.Println("⏳ Chờ các giao dịch được confirm...")
	for i := 0; i < len(txHashes); i++ {
		hash := txHashes[i]
		if hash == (common.Hash{}) { continue }
		for {
			receipt, err := client.TransactionReceipt(context.Background(), hash)
			if err == nil {
				if receipt.Status != 1 {
					fmt.Printf("❌ Tx %s bị revert!\n", hash.Hex())
				} else {
					fmt.Printf("✅ Tx %s confirmed trong block %d\n", hash.Hex()[:10]+"...", receipt.BlockNumber.Uint64())
				}
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	elapsed := time.Since(start)
	
	finalContractBal, _ := client.BalanceAt(context.Background(), *contractAddr, nil)
	finalReceiverBal, _ := client.BalanceAt(context.Background(), receiverAddr, nil)

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
		fmt.Println("\n⚠️ TEST FAILED: Block-STM xử lý sai lệch số dư khi trộn lẫn Native Transfer và EVM Call!")
		os.Exit(1)
	} else {
		fmt.Println("\n🎉 TEST PASSED: Block-STM xử lý chuẩn xác cả giao dịch Native và Smart Contract xen kẽ, không có Race Condition về Balance!")
	}
}

// Helper deploy
func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil { return nil, err }
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil { gasPrice = big.NewInt(1000000000) }
	gasLimit := uint64(5000000)

	tx := types.NewContractCreation(nonce, big.NewInt(0), gasLimit, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil { return nil, err }

	if err := client.SendTransaction(context.Background(), signedTx); err != nil { return nil, err }

	for {
		receipt, err := client.TransactionReceipt(context.Background(), signedTx.Hash())
		if err == nil {
			if receipt.Status != 1 { return nil, fmt.Errorf("deploy reverted") }
			return &receipt.ContractAddress, nil
		}
		if err != ethereum.NotFound { return nil, err }
		time.Sleep(300 * time.Millisecond)
	}
}
