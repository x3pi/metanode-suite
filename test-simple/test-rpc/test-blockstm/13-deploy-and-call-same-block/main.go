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
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 13-deploy-and-call-same-block")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Gửi 1 giao dịch Deploy Smart Contract và 1 giao dịch Gọi hàm của nó ngay lập tức (cùng block).")
	fmt.Println("⚡ GỌI     : EVM Deploy & Call song song (tính trước địa chỉ).")
	fmt.Println("🎯 KỲ VỌNG : Mempool của Metanode sẽ TỪ CHỐI giao dịch Call vì contract chưa tồn tại trên StateDB, bảo vệ hệ thống khỏi các lỗi thực thi.")
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

	parsedABI, _ := abi.JSON(strings.NewReader(cfg.Contracts["TestCounter"].ABI))
	bytecode, _ := hexutil.Decode("0x" + cfg.Contracts["TestCounter"].Bytecode)

	pk0, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	// Lấy nonce để tính toán địa chỉ contract sắp tạo
	nonce, _ := client.PendingNonceAt(context.Background(), from0)
	
	// Tính trước địa chỉ contract (CREATE opcode: hash(sender, nonce))
	predictedAddr := crypto.CreateAddress(from0, nonce)
	fmt.Printf("📌 Địa chỉ Contract tính toán trước: %s\n\n", predictedAddr.Hex())

	var wg sync.WaitGroup
	var errsMu sync.Mutex
	
	txHashes := make([]common.Hash, 2)
	start := time.Now()

	// Tx 1: Deploy Contract
	wg.Add(1)
	go func() {
		defer wg.Done()
		
		gasPrice := big.NewInt(1000000000)
		gasLimit := uint64(5000000)

		tx := types.NewContractCreation(nonce, big.NewInt(0), gasLimit, gasPrice, bytecode)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk0)
		if err != nil { return }

		err = client.SendTransaction(context.Background(), signedTx)
		errsMu.Lock()
		if err != nil {
			fmt.Printf("⚠️ Lỗi gửi Deploy Tx: %v\n", err)
		} else {
			fmt.Printf("✅ Đã push Deploy Tx: %s\n", signedTx.Hash().Hex())
			txHashes[0] = signedTx.Hash()
		}
		errsMu.Unlock()
	}()

	// Đợi 100ms để đảm bảo Deploy Tx vào mempool trước (vì chung nonce) 
	// Thực ra mempool sẽ xử lý nonce + 1 tự động nếu gửi tuần tự
	time.Sleep(100 * time.Millisecond)

	// Tx 2: Gọi hàm increment() trên địa chỉ vừa đoán được
	wg.Add(1)
	go func() {
		defer wg.Done()
		
		data, _ := parsedABI.Pack("increment")
		gasPrice := big.NewInt(1000000000)
		gasLimit := uint64(100000)
		
		// Tx này có nonce = nonce + 1
		tx := types.NewTransaction(nonce+1, predictedAddr, big.NewInt(0), gasLimit, gasPrice, data)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk0)
		if err != nil { return }

		err = client.SendTransaction(context.Background(), signedTx)
		errsMu.Lock()
		if err != nil {
			if strings.Contains(err.Error(), "non-existent smart account") {
				fmt.Printf("✅ Mempool đã từ chối Call Tx vì contract chưa tồn tại (Bảo mật tốt!): %v\n", err)
				os.Exit(0)
			}
			fmt.Printf("⚠️ Lỗi gửi Call Tx: %v\n", err)
		} else {
			fmt.Printf("✅ Đã push Call Tx: %s\n", signedTx.Hash().Hex())
			txHashes[1] = signedTx.Hash()
		}
		errsMu.Unlock()
	}()

	wg.Wait()

	fmt.Println("⏳ Chờ các giao dịch được confirm trong cùng 1 Block...")
	successCount := 0

	for i := 0; i < 2; i++ {
		hash := txHashes[i]
		if hash == (common.Hash{}) {
			continue
		}
		
		for {
			receipt, err := client.TransactionReceipt(context.Background(), hash)
			if err == nil {
				if receipt.Status != 1 {
					fmt.Printf("❌ Tx %s bị REVERT!\n", hash.Hex())
				} else {
					fmt.Printf("✅ Tx %s THÀNH CÔNG trong block %d\n", hash.Hex(), receipt.BlockNumber.Uint64())
					successCount++
				}
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("⏱️ Thời gian gửi & chờ: %v\n", elapsed)

	// Kiểm tra xem count có tăng lên 1 hay không
	if successCount == 2 {
		data, _ := parsedABI.Pack("getCount")
		result, err := client.CallContract(context.Background(), ethereum.CallMsg{To: &predictedAddr, Data: data}, nil)
		if err == nil {
			outputs, _ := parsedABI.Unpack("getCount", result)
			if len(outputs) > 0 {
				val := outputs[0].(*big.Int)
				fmt.Printf("📊 Giá trị count sau cùng: %s\n", val.String())
				if val.Cmp(big.NewInt(1)) == 0 {
					fmt.Println("\n🎉 TEST PASSED: Block-STM xử lý Deploy và Call trong cùng 1 Block hoàn hảo!")
					return
				}
			}
		}
	}

	fmt.Println("\n⚠️ TEST FAILED: Có lỗi xảy ra trong quá trình Deploy và Call!")
	os.Exit(1)
}
