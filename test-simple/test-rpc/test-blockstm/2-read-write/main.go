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

type Config struct {
	RPCUrl      string   `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
	ChainID     int64    `json:"chain_id"`
}

func main() {
	configPath := "../../update-same-contract/config.json"
	if len(os.Args) > 1 { configPath = os.Args[1] }

	raw, _ := os.ReadFile(configPath)
	var cfg Config
	json.Unmarshal(raw, &cfg)

	client, _ := ethclient.Dial(cfg.RPCUrl)

	abiBytes, err := os.ReadFile("../contracts/BlockSTMTests_sol_ReadWriteConflict.abi")
	if err != nil { log.Fatalf("❌ Thiếu file ABI") }
	parsedABI, _ := abi.JSON(strings.NewReader(string(abiBytes)))

	binBytes, _ := os.ReadFile("../contracts/BlockSTMTests_sol_ReadWriteConflict.bin")
	bytecode, _ := hexutil.Decode("0x" + strings.TrimSpace(string(binBytes)))

	pk0, _ := crypto.HexToECDSA(cfg.PrivateKeys[0])
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying ReadWriteConflict Contract...")
	contractAddr, _ := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	fmt.Printf("📌 Contract: %s\n\n", contractAddr.Hex())

	var wg sync.WaitGroup
	fmt.Println("🔥 Gửi tx WRITE (từ ví 0) và READ (từ các ví khác) đồng thời...")
	
	// Ví 0 sẽ gọi writeData(9999)
	// Các ví khác sẽ gọi readDataAndSave()
	
	for i, pkStr := range cfg.PrivateKeys {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()
			pk, _ := crypto.HexToECDSA(pKeyHex)
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

			var data []byte
			if idx == 0 {
				data, _ = parsedABI.Pack("writeData", big.NewInt(9999))
			} else {
				data, _ = parsedABI.Pack("readDataAndSave")
			}

			hash, err := sendTx(client, pk, cfg.ChainID, from, contractAddr, data)
			if err == nil {
				fmt.Printf("✅ Wallet %d gửi tx thành công: %s\n", idx, hash.Hex())
				waitReceipt(client, hash)
			}
		}(i, pkStr)
	}

	wg.Wait()
	fmt.Println("\n📊 KẾT QUẢ READ-WRITE CONFLICT:")
	
	// Đọc sharedData
	shared, _ := getUint256(client, contractAddr, parsedABI, "sharedData")
	fmt.Printf("Giá trị sharedData cuối cùng: %s\n", shared.String())

	// Đọc kết quả lưu của các ví khác xem đã đọc được giá trị cũ hay mới
	for i := 1; i < len(cfg.PrivateKeys); i++ {
		pk, _ := crypto.HexToECDSA(cfg.PrivateKeys[i])
		from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))
		
		d, _ := parsedABI.Pack("userReads", from)
		res, _ := client.CallContract(context.Background(), ethereum.CallMsg{To: contractAddr, Data: d}, nil)
		out, _ := parsedABI.Unpack("userReads", res)
		val := out[0].(*big.Int)
		
		fmt.Printf("Wallet %d đọc được giá trị: %s\n", i, val.String())
	}
	fmt.Println("👉 Phân tích: Nếu Wallet đọc được 0 (giá trị cũ) -> Tx READ chạy trước WRITE.")
	fmt.Println("👉 Nếu đọc được 9999 -> Block-STM đã xếp WRITE trước hoặc đã Re-Execute READ sau khi WRITE thành công.")
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
		if err == nil { return receipt, nil }
		time.Sleep(300 * time.Millisecond)
	}
}

func getUint256(client *ethclient.Client, addr *common.Address, parsedABI abi.ABI, method string) (*big.Int, error) {
	data, _ := parsedABI.Pack(method)
	result, _ := client.CallContract(context.Background(), ethereum.CallMsg{To: addr, Data: data}, nil)
	outputs, _ := parsedABI.Unpack(method, result)
	return outputs[0].(*big.Int), nil
}
