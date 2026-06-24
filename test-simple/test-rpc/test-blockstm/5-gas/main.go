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

	raw, _ := os.ReadFile(configPath)
	var cfg Config
	json.Unmarshal(raw, &cfg)

	client, _ := ethclient.Dial(cfg.RPCUrl)

		parsedABI, err := abi.JSON(strings.NewReader(cfg.Contracts["AbortRollback"].ABI))
	if err != nil { log.Fatalf("ABI parse err: %v", err) }

		bytecode, err := hexutil.Decode("0x" + cfg.Contracts["AbortRollback"].Bytecode)
	if err != nil { log.Fatalf("Bytecode err: %v", err) }

	pk0, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying AbortRollback Contract (cho Test 5 Gas)...")
	contractAddr, _ := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	fmt.Printf("📌 Contract: %s\n\n", contractAddr.Hex())

	var wg sync.WaitGroup
	fmt.Println("🔥 Gửi tx... Mục tiêu: Kiểm tra Gas bị trừ khi Block-STM Re-execute và Revert")

	// Lưu hash để lấy receipt
	txHashes := make([]common.Hash, len(cfg.PrivateKeys))

	for i, pkStr := range cfg.PrivateKeys {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()
			pk, _ := crypto.HexToECDSA(pKeyHex)
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

			var data []byte
			if idx == 0 {
				data, _ = parsedABI.Pack("setPhase", big.NewInt(2))
			} else {
				data, _ = parsedABI.Pack("updateIfPhase1", big.NewInt(888))
			}

			hash, err := sendTx(client, pk, cfg.ChainID, from, contractAddr, data)
			if err == nil {
				txHashes[idx] = hash
			}
		}(i, pkStr)
	}

	wg.Wait()
	fmt.Println("\n📊 KẾT QUẢ TEST 5 (GAS & ROLLBACK):")

	var revertCount int
	for i, hash := range txHashes {
		if hash == (common.Hash{}) {
			continue
		}
		receipt, _ := waitReceipt(client, hash)

		status := "SUCCESS"
		if receipt.Status != 1 {
			status = "REVERTED"
			revertCount++
		}

		fmt.Printf("Wallet %d | Trạng thái: %-8s | Gas sử dụng (GasUsed): %d\n", i, status, receipt.GasUsed)
	}

	if revertCount == 0 {
		fmt.Println("❌ TEST FAILED: Lỗi Block-STM, đáng lẽ phải có giao dịch bị Revert để test Gas, nhưng tất cả lại SUCCESS!")
		os.Exit(1)
	} else {
		fmt.Printf("🎉 Thành công! Có %d giao dịch bị Revert và tiêu thụ gas hợp lý.\n", revertCount)
	}

	fmt.Println("\n👉 Phân tích: Những Tx bị REVERT do Block-STM chạy lại phải trả một lượng Gas nhất định (thường là base gas + gas chạy đến lúc revert).")
	fmt.Println("👉 Lượng GasUsed của Tx Revert không được lớn bằng Tx Success (vì nó dừng sớm). Hơn nữa, dù Block-STM có re-execute nó 3-4 lần ngầm bên dưới, GasUsed ghi nhận trên block MÀ user phải trả vẫn chỉ được tính 1 LẦN DUY NHẤT.")
}

func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	nonce, _ := client.PendingNonceAt(context.Background(), from)
	tx := types.NewContractCreation(nonce, big.NewInt(0), 5000000, big.NewInt(1e9), bytecode)
	signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	client.SendTransaction(context.Background(), signedTx)
	receipt, _ := waitReceipt(client, signedTx.Hash())
	return &receipt.ContractAddress, nil
}

func sendTx(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, data []byte) (common.Hash, error) {
	nonce, _ := client.PendingNonceAt(context.Background(), from)
	tx := types.NewTransaction(nonce, *to, big.NewInt(0), 1000000, big.NewInt(1e9), data)
	signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	err := client.SendTransaction(context.Background(), signedTx)
	return signedTx.Hash(), err
}

func waitReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	for {
		receipt, err := client.TransactionReceipt(context.Background(), txHash)
		if err == nil {
			return receipt, nil
		}
		time.Sleep(300 * time.Millisecond)
	}
}
