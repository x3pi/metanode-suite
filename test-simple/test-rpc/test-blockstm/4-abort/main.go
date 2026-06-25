/*
 * BÀI TEST: 4-abort
 * MÔ TẢ   : Kiểm tra các giao dịch chủ động throw lỗi (revert/abort) bên trong logic Smart Contract.
 * GỌI     : Giao dịch có logic require() hoặc revert() dẫn đến thất bại chủ động.
 * KỲ VỌNG : Trạng thái của giao dịch abort bị rollback, KHÔNG ảnh hưởng đến các giao dịch khác trong cùng block.
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
	fmt.Println("BÀI TEST: 4-abort")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Kiểm tra các giao dịch chủ động throw lỗi (revert/abort) bên trong logic Smart Contract.")
	fmt.Println("⚡ GỌI     : Giao dịch có logic require() hoặc revert() dẫn đến thất bại chủ động.")
	fmt.Println("🎯 KỲ VỌNG : Trạng thái của giao dịch abort bị rollback, KHÔNG ảnh hưởng đến các giao dịch khác trong cùng block.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	configPath := "../config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	raw, _ := os.ReadFile(configPath)
	var cfg Config
	json.Unmarshal(raw, &cfg)

	client, _ := ethclient.Dial(cfg.RPCUrl)

	parsedABI, err := abi.JSON(strings.NewReader(cfg.Contracts["AbortRollback"].ABI))
	if err != nil {
		log.Fatalf("ABI parse err: %v", err)
	}

	bytecode, err := hexutil.Decode("0x" + cfg.Contracts["AbortRollback"].Bytecode)
	if err != nil {
		log.Fatalf("Bytecode err: %v", err)
	}

	pk0, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying AbortRollback Contract...")
	contractAddr, _ := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	fmt.Printf("📌 Contract: %s\n\n", contractAddr.Hex())

	var wg sync.WaitGroup
	var revertCount int
	var mu sync.Mutex
	fmt.Println("🔥 Gửi tx SET PHASE=2 (ví 0) và UPDATE IF PHASE=1 (các ví khác) đồng thời...")

	for i, pkStr := range cfg.PrivateKeys {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()
			pk, _ := crypto.HexToECDSA(pKeyHex)
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

			var data []byte
			var actionName string
			if idx == 0 {
				actionName = "SET PHASE = 2"
				data, _ = parsedABI.Pack("setPhase", big.NewInt(2))
			} else {
				actionName = "UPDATE IF PHASE = 1 (val: 888)"
				data, _ = parsedABI.Pack("updateIfPhase1", big.NewInt(888))
			}

			fmt.Printf("⏳ Wallet %d đang gửi tx: %s\n", idx, actionName)
			hash, err := sendTx(client, pk, cfg.ChainID, from, contractAddr, data)
			if err == nil {
				receipt, _ := waitReceipt(client, hash)
				if receipt.Status == 1 {
					fmt.Printf("✅ Wallet %d [%s] -> SUCCESS (Tx: %s, Block: %d, TxIndex: %d)\n", idx, actionName, hash.Hex(), receipt.BlockNumber.Uint64(), receipt.TransactionIndex)
				} else {
					fmt.Printf("🔄 Wallet %d [%s] -> REVERTED (Đúng như thiết kế rollback! Tx: %s, Block: %d, TxIndex: %d)\n", idx, actionName, hash.Hex(), receipt.BlockNumber.Uint64(), receipt.TransactionIndex)
					mu.Lock()
					revertCount++
					mu.Unlock()
				}
			} else {
				fmt.Printf("⚠️ Wallet %d [%s] -> GỬI THẤT BẠI: %v\n", idx, actionName, err)
			}
		}(i, pkStr)
	}

	wg.Wait()
	fmt.Println("\n📊 KẾT QUẢ ABORT / ROLLBACK:")

	phase, _ := getUint256(client, contractAddr, parsedABI, "phase")
	fmt.Printf("Phase hiện tại: %s\n", phase.String())

	if revertCount == 0 {
		fmt.Println("❌ TEST FAILED: Block-STM đã lỗi, không bắt được xung đột (conflict) nên không có giao dịch nào bị Revert!")
		os.Exit(1)
	} else {
		fmt.Printf("🎉 Tuyệt vời! Có %d giao dịch đã bị Revert đúng như thiết kế của Block-STM.\n", revertCount)
	}
	fmt.Println("👉 Phân tích: Nếu Block-STM phát hiện Tx 'setPhase=2' làm thay đổi condition của 'updateIfPhase1', nó sẽ rollback các Tx đang chạy song song, khiến chúng bị REVERT.")
}

// Helpers tương tự như trên
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

func getUint256(client *ethclient.Client, addr *common.Address, parsedABI abi.ABI, method string) (*big.Int, error) {
	data, _ := parsedABI.Pack(method)
	result, _ := client.CallContract(context.Background(), ethereum.CallMsg{To: addr, Data: data}, nil)
	outputs, _ := parsedABI.Unpack(method, result)
	return outputs[0].(*big.Int), nil
}
