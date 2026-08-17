/*
 * BÀI TEST: 23-contract-creator-info
 * MÔ TẢ   : Gửi giao dịch deploy contract và dùng RPC mtn_getAccountState để kiểm chứng CreatorPublicKey và StorageAddress được lưu chuẩn xác không.
 */
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Dùng tạm bytecode của bài 22 cho lẹ
const bytecodeHex = "0x608060405234801561001057600080fd5b5061026a806100206000396000f3fe608060405234801561001057600080fd5b50600436106100625760003560e01c806307604a7e146100675780631b36fa78146100855780632abbd748146100a3578063433c070d146100c15780634840a051146100cb5780636e795a5d146100e9575b600080fd5b61006f610107565b60405161007c91906101bd565b60405180910390f35b61008d61010d565b60405161009a91906101bd565b60405180910390f35b6100ab610113565b6040516100b891906101bd565b60405180910390f35b6100c9610119565b005b6100d3610178565b6040516100e09190610219565b60405180910390f35b6100f161019e565b6040516100fe91906101bd565b60405180910390f35b60015481565b60005481565b60035481565b426000819055504360018190555041600260006101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055504660038190555048600481905550565b600260009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b60045481565b6000819050919050565b6101b7816101a4565b82525050565b60006020820190506101d260008301846101ae565b92915050565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b6000610203826101d8565b9050919050565b610213816101f8565b82525050565b600060208201905061022e600083018461020a565b9291505056fea26469706673582212203fabbfb2a358eedd75490ab3dda10644672f328eb7d42191cc329ae08b5612fa64736f6c63430008140033"

type Config struct {
	PrivateKeys []string                  `json:"private_keys"`
	RPCUrl  string `json:"rpc_url"`
	ChainID int64  `json:"chain_id"`
}

type GeneratedKey struct {
	PrivateKey string `json:"private_key"`
}

type RPCResponse struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  struct {
		AccountType        int    `json:"accountType"`
		Address            string `json:"address"`
		PublicKeyBls       string `json:"publicKeyBls"`
		SmartContractState string `json:"smartContractState"`
	} `json:"result"`
}

func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: Kiểm tra CreatorPublicKey & StorageAddress")
	fmt.Println("==========================================================")

	configFlag := flag.String("config", "../config.json", "Đường dẫn file config")
keysFile := flag.String("keys", "", "Đường dẫn file chứa private keys tuỳ chọn (mặc định đọc từ config.json)")
	flag.Parse()

	raw, err := os.ReadFile(*configFlag)
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

	fmt.Println("🚀 Deploying Contract...")
	contractAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		log.Fatalf("❌ Deploy thất bại: %v", err)
	}
	fmt.Printf("📌 Contract deployed at: %s\n\n", contractAddr.Hex())

	// Chờ 2s để đảm bảo mạng đã đồng bộ
	time.Sleep(2 * time.Second)

	fmt.Println("🔍 Đọc AccountState từ RPC...")
	reqBody := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"mtn_getAccountState","params":["%s","latest"]}`, contractAddr.Hex()))

	resp, err := http.Post(cfg.RPCUrl, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		log.Fatalf("❌ Lỗi gọi RPC mtn_getAccountState: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc response body: %v", err)
	}

	var rpcResp RPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		log.Fatalf("❌ Lỗi parse JSON response: %v\nBody: %s", err, string(body))
	}

	fmt.Println("\n📊 KẾT QUẢ TỪ RPC:")
	fmt.Printf("   - Contract Address   : %s\n", rpcResp.Result.Address)
	fmt.Printf("   - AccountType        : %d (1 = Smart Contract)\n", rpcResp.Result.AccountType)

	// SmartContractState trả về chuỗi Hex mã hoá Protobuf.
	// Nếu nó rỗng (empty string) nghĩa là lỗi cũ đã xảy ra!
	fmt.Printf("   - SmartContractState : [%d chars]\n", len(rpcResp.Result.SmartContractState))

	if len(rpcResp.Result.SmartContractState) == 0 {
		fmt.Println("\n⚠️ TEST FAILED: SmartContractState BỊ RỖNG!")
		fmt.Println("Lỗi: Contract không lưu được thông tin CreatorPublicKey & StorageAddress xuống DB!")
	} else {
		fmt.Println("\n✅ TEST PASSED: SmartContractState ĐÃ ĐƯỢC LƯU!")
		fmt.Println("Contract đã được lưu đẩy đủ thông tin gia phả.")
		fmt.Println("\n🔎 CHI TIẾT SmartContractState (Hex từ Protobuf):")
		// In ra toàn bộ chuỗi Hex Protobuf để user thấy Creator Public Key nằm trong đó
		hexStr := rpcResp.Result.SmartContractState

		fmt.Printf("   %s\n", hexStr)
	}
}

// deployContract deploy một contract mới
func deployContract(client *ethclient.Client, privateKey *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (common.Address, error) {
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return common.Address{}, fmt.Errorf("lỗi lấy nonce: %w", err)
	}

	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		return common.Address{}, fmt.Errorf("lỗi lấy gas price: %w", err)
	}

	gasLimit := uint64(5000000)

	tx := types.NewContractCreation(nonce, big.NewInt(0), gasLimit, gasPrice, bytecode)

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), privateKey)
	if err != nil {
		return common.Address{}, fmt.Errorf("lỗi ký transaction: %w", err)
	}

	err = client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		return common.Address{}, fmt.Errorf("lỗi gửi transaction: %w", err)
	}

	// Đợi transaction được mine
	var receipt *types.Receipt
	timeoutStart := time.Now()
	for {
		if time.Since(timeoutStart) > 60*time.Second {
			fmt.Println("❌ Timeout waiting for receipt")
			os.Exit(1)
		}
		receipt, err = client.TransactionReceipt(context.Background(), signedTx.Hash())
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if receipt.Status != 1 {
		return common.Address{}, fmt.Errorf("transaction thất bại, status: %v", receipt.Status)
	}

	return receipt.ContractAddress, nil
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
