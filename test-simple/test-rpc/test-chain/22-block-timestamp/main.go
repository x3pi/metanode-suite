/*
 * BÀI TEST: 22-block-timestamp
 * MÔ TẢ   : Gửi giao dịch để lấy block.timestamp và verify các block properties.
 */
package main

import (
	"tool-test/test-simple/test-rpc/test-blockstm/config"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const bytecodeHex = "0x608060405234801561001057600080fd5b5061026a806100206000396000f3fe608060405234801561001057600080fd5b50600436106100625760003560e01c806307604a7e146100675780631b36fa78146100855780632abbd748146100a3578063433c070d146100c15780634840a051146100cb5780636e795a5d146100e9575b600080fd5b61006f610107565b60405161007c91906101bd565b60405180910390f35b61008d61010d565b60405161009a91906101bd565b60405180910390f35b6100ab610113565b6040516100b891906101bd565b60405180910390f35b6100c9610119565b005b6100d3610178565b6040516100e09190610219565b60405180910390f35b6100f161019e565b6040516100fe91906101bd565b60405180910390f35b60015481565b60005481565b60035481565b426000819055504360018190555041600260006101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055504660038190555048600481905550565b600260009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b60045481565b6000819050919050565b6101b7816101a4565b82525050565b60006020820190506101d260008301846101ae565b92915050565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b6000610203826101d8565b9050919050565b610213816101f8565b82525050565b600060208201905061022e600083018461020a565b9291505056fea26469706673582212203fabbfb2a358eedd75490ab3dda10644672f328eb7d42191cc329ae08b5612fa64736f6c63430008140033"
const abiJSON = `[{"inputs":[],"name":"saveAll","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"savedBaseFee","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"savedChainId","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"savedCoinbase","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"savedNumber","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"savedTimestamp","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`


type GeneratedKey struct {
	PrivateKey string `json:"private_key"`
}

func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: Kiểm tra giá trị BlockContext trong Contract")
	fmt.Println("==========================================================")

	configFlag := flag.String("config", "../config.json", "Đường dẫn file config")
keysFile := flag.String("keys", "", "Đường dẫn file chứa private keys tuỳ chọn (mặc định đọc từ config.json)")
	flag.Parse()

	cfg, err := config.LoadConfig(*configFlag)
	if err != nil {
		log.Fatalf("❌ Lỗi load config: %v", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC: %v", err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	bytecode, err := hexutil.Decode(bytecodeHex)
	if err != nil {
		log.Fatalf("❌ Lỗi decode bytecode hex: %v", err)
	}

	testKeys := loadPrivateKeys(*keysFile, cfg.PrivateKeys)
	cfg.PrivateKeys = testKeys

	pk0, err := crypto.HexToECDSA(testKeys[0])
	if err != nil {
		log.Fatalf("❌ Lỗi parse private key: %v", err)
	}
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying BlockTimestampTest contract...")
	contractAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		log.Fatalf("❌ Deploy thất bại: %v", err)
	}
	fmt.Printf("📌 Contract deployed at: %s\n\n", contractAddr.Hex())

	fmt.Println("🔥 Gửi giao dịch gọi hàm saveAll()...")
	txHash, err := sendSave(client, pk0, cfg.ChainID, from0, contractAddr, parsedABI)
	if err != nil {
		log.Fatalf("❌ Lỗi gửi giao dịch saveAll(): %v", err)
	}
	fmt.Printf("✅ Giao dịch gửi thành công: %s\n", txHash.Hex())

	fmt.Println("⏳ Chờ giao dịch được xác nhận (bằng receipt)...")
	receipt, err := waitReceipt(client, txHash)
	if err != nil {
		log.Fatalf("❌ Lỗi lấy receipt: %v", err)
	}

	blockInfo, err := client.BlockByNumber(context.Background(), receipt.BlockNumber)
	if err != nil {
		log.Fatalf("❌ Lỗi lấy thông tin block bằng RPC: %v", err)
	}

	fmt.Println("🔍 Đọc lại properties từ contract...")
	ts, _ := getSavedUint256(client, contractAddr, parsedABI, "savedTimestamp")
	num, _ := getSavedUint256(client, contractAddr, parsedABI, "savedNumber")
	chainId, _ := getSavedUint256(client, contractAddr, parsedABI, "savedChainId")
	baseFee, _ := getSavedUint256(client, contractAddr, parsedABI, "savedBaseFee")
	coinbase, _ := getSavedAddress(client, contractAddr, parsedABI, "savedCoinbase")

	fmt.Printf("\n📊 KẾT QUẢ ĐỌC TỪ EVM:\n")
	fmt.Printf("   - Timestamp : %d\n", ts)
	fmt.Printf("   - Number    : %d\n", num)
	fmt.Printf("   - ChainID   : %d\n", chainId)
	fmt.Printf("   - BaseFee   : %d\n", baseFee)
	fmt.Printf("   - Coinbase  : %s\n", coinbase.Hex())

	fmt.Printf("\n🔎 ĐỐI CHIẾU VỚI THÔNG TIN BLOCK %d TỪ RPC:\n", blockInfo.NumberU64())
	fmt.Printf("   - Timestamp : %d\n", blockInfo.Time())
	fmt.Printf("   - Number    : %d\n", blockInfo.NumberU64())
	fmt.Printf("   - Coinbase  : %s\n", blockInfo.Coinbase().Hex())
	fmt.Printf("   - BaseFee   : %v\n", blockInfo.BaseFee())

	allPassed := true
	if ts != blockInfo.Time() {
		fmt.Printf("   => ⚠️ LỖI: Timestamp không khớp! (Contract: %d, RPC: %d)\n", ts, blockInfo.Time())
		allPassed = false
	}
	if num != blockInfo.NumberU64() {
		fmt.Printf("   => ⚠️ LỖI: Block number không khớp!\n")
		allPassed = false
	}
	if chainId != uint64(cfg.ChainID) {
		fmt.Printf("   => ⚠️ LỖI: Chain ID không khớp! (Contract: %d, Config: %d)\n", chainId, cfg.ChainID)
		allPassed = false
	}

	if allPassed {
		fmt.Printf("\n🎉 TEST PASSED: EVM nhận được toàn bộ BlockContext hợp lệ!\n")
	} else {
		fmt.Printf("\n⚠️ TEST FAILED: Các giá trị BlockContext truyền cho EVM bị lỗi.\n")
	}
}

func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return nil, err
	}
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		gasPrice = big.NewInt(1000000000)
	}
	gasLimit, err := client.EstimateGas(context.Background(), ethereum.CallMsg{From: from, Data: bytecode})
	if err != nil {
		gasLimit = 5_000_000
	} else {
		gasLimit += 50_000
	}
	tx := types.NewContractCreation(nonce, big.NewInt(0), gasLimit, gasPrice, bytecode)
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
	if receipt.Status != 1 {
		return nil, fmt.Errorf("deploy reverted")
	}
	return &receipt.ContractAddress, nil
}

func sendSave(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI) (common.Hash, error) {
	data, err := parsedABI.Pack("saveAll")
	if err != nil {
		return common.Hash{}, err
	}
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return common.Hash{}, err
	}
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		gasPrice = big.NewInt(1000000000)
	}
	gasLimit, err := client.EstimateGas(context.Background(), ethereum.CallMsg{From: from, To: to, GasPrice: gasPrice, Data: data})
	if err != nil {
		gasLimit = 100_000
	} else {
		gasLimit += 10_000
	}
	tx := types.NewTransaction(nonce, *to, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return common.Hash{}, err
	}
	if err := client.SendTransaction(context.Background(), signedTx); err != nil {
		return common.Hash{}, err
	}
	return signedTx.Hash(), nil
}

func getSavedUint256(client *ethclient.Client, addr *common.Address, parsedABI abi.ABI, method string) (uint64, error) {
	data, _ := parsedABI.Pack(method)
	result, err := client.CallContract(context.Background(), ethereum.CallMsg{To: addr, Data: data}, nil)
	if err != nil {
		return 0, err
	}
	outputs, err := parsedABI.Unpack(method, result)
	if err != nil {
		return 0, err
	}
	if len(outputs) == 0 {
		return 0, fmt.Errorf("output rỗng")
	}
	val, ok := outputs[0].(*big.Int)
	if !ok {
		return 0, fmt.Errorf("kiểu trả về không phải *big.Int")
	}
	return val.Uint64(), nil
}

func getSavedAddress(client *ethclient.Client, addr *common.Address, parsedABI abi.ABI, method string) (common.Address, error) {
	data, _ := parsedABI.Pack(method)
	result, err := client.CallContract(context.Background(), ethereum.CallMsg{To: addr, Data: data}, nil)
	if err != nil {
		return common.Address{}, err
	}
	outputs, err := parsedABI.Unpack(method, result)
	if err != nil {
		return common.Address{}, err
	}
	if len(outputs) == 0 {
		return common.Address{}, fmt.Errorf("output rỗng")
	}
	val, ok := outputs[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("kiểu trả về không phải address")
	}
	return val, nil
}

func waitReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	timeoutStart := time.Now()
	for {
		if time.Since(timeoutStart) > 60*time.Second {
			fmt.Println("❌ Timeout waiting for receipt")
			os.Exit(1)
		}
		receipt, err := client.TransactionReceipt(context.Background(), txHash)

		if err != nil && !strings.Contains(err.Error(), "not found") {
			fmt.Printf("Lỗi kết nối RPC: %v\n", err)
			os.Exit(1)
		}
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			return receipt, nil
		}
		if err != nil && err.Error() != "not found" {
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// loadPrivateKeys loads private keys either from an explicitly passed keys file,
// or defaults to the keys defined in config.json.
// It supports both []string format and []GeneratedKey (index, private_key, address) format.
func loadPrivateKeys(keysFilePath string, cfgKeys []string) []string {
	if keysFilePath != "" {
		raw, err := os.ReadFile(keysFilePath)
		if err != nil {
			log.Fatalf("❌ Lỗi đọc file keys %s: %v", keysFilePath, err)
		}
		var strKeys []string
		if err := json.Unmarshal(raw, &strKeys); err == nil && len(strKeys) > 0 {
			return strKeys
		}
		var genKeys []struct {
			PrivateKey string `json:"private_key"`
		}
		if err := json.Unmarshal(raw, &genKeys); err == nil && len(genKeys) > 0 {
			var res []string
			for _, gk := range genKeys {
				if gk.PrivateKey != "" {
					res = append(res, gk.PrivateKey)
				}
			}
			if len(res) > 0 {
				return res
			}
		}
		log.Fatalf("❌ Không thể parse private key nào từ file %s", keysFilePath)
	}
	if len(cfgKeys) > 0 {
		return cfgKeys
	}
	log.Fatalf("❌ Không tìm thấy private key nào trong config.json hoặc file chỉ định")
	return nil
}
