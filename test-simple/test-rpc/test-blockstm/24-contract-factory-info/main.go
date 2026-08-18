/*
 * BÀI TEST: 24-contract-factory-info
 * MÔ TẢ   : Deploy Factory Contract, gọi hàm tạo Contract Con (Internal Deploy).
 * Cố gắng đọc AccountState của Contract Con xem có kế thừa được CreatorPublicKey không.
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
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Bytecode của Factory Contract (do solc biên dịch)
const bytecodeHex = "0x608060405234801561000f575f80fd5b5061034e8061001d5f395ff3fe608060405234801561000f575f80fd5b5060043610610029575f3560e01c80630419eca51461002d575b5f80fd5b61004760048036038101906100429190610116565b61005d565b6040516100549190610180565b60405180910390f35b5f808260405161006c906100d2565b61007691906101a8565b604051809103905ff08015801561008f573d5f803e3d5ffd5b5090507f7b4b5576882318f3025ede3b4525a692b9a9792674c6bd0f82ce62845374a921816040516100c19190610180565b60405180910390a180915050919050565b610157806101c283390190565b5f80fd5b5f819050919050565b6100f5816100e3565b81146100ff575f80fd5b50565b5f81359050610110816100ec565b92915050565b5f6020828403121561012b5761012a6100df565b5b5f61013884828501610102565b91505092915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f61016a82610141565b9050919050565b61017a81610160565b82525050565b5f6020820190506101935f830184610171565b92915050565b6101a2816100e3565b82525050565b5f6020820190506101bb5f830184610199565b9291505056fe608060405234801561000f575f80fd5b5060405161015738038061015783398181016040528101906100319190610074565b805f819055505061009f565b5f80fd5b5f819050919050565b61005381610041565b811461005d575f80fd5b50565b5f8151905061006e8161004a565b92915050565b5f602082840312156100895761008861003d565b5b5f61009684828501610060565b91505092915050565b60ac806100ab5f395ff3fe6080604052348015600e575f80fd5b50600436106026575f3560e01c80630c55699c14602a575b5f80fd5b60306044565b604051603b9190605f565b60405180910390f35b5f5481565b5f819050919050565b6059816049565b82525050565b5f60208201905060705f8301846052565b9291505056fea26469706673582212208db137adc37767e60069be27788476572f05efe8ba42b5f8b0d057a4e44f591764736f6c63430008140033a2646970667358221220f4c50dfeb75ea6fbdedb0d80ee55b9b1d5223e6bbbe8578c02d981f076eb076664736f6c63430008140033"

const abiJSON = `[{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"childAddress","type":"address"}],"name":"ChildCreated","type":"event"},{"inputs":[{"internalType":"uint256","name":"_x","type":"uint256"}],"name":"createChild","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"nonpayable","type":"function"}]`

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
	fmt.Println("BÀI TEST: Factory (Contract đẻ Contract) & Kế thừa Gia Phả")
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

	// 1. Deploy Factory
	fmt.Println("🚀 BƯỚC 1: Deploying Factory Contract...")
	factoryAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		log.Fatalf("❌ Deploy Factory thất bại: %v", err)
	}
	fmt.Printf("📌 Factory deployed at: %s\n\n", factoryAddr.Hex())

	time.Sleep(2 * time.Second)

	// 2. Call createChild(42)
	fmt.Println("🔥 BƯỚC 2: Gọi Factory.createChild(42) để đẻ Contract Con...")
	callData, err := parsedABI.Pack("createChild", big.NewInt(42))
	if err != nil {
		log.Fatalf("❌ Lỗi pack ABI: %v", err)
	}

	receipt, err := sendTransactionAndWait(client, pk0, cfg.ChainID, from0, factoryAddr, callData)
	if err != nil {
		log.Fatalf("❌ Lỗi giao dịch createChild: %v", err)
	}
	fmt.Printf("✅ Giao dịch thành công. Hash: %s\n", receipt.TxHash.Hex())

	// Lọc Log Event ChildCreated
	var childAddr common.Address
	for _, vLog := range receipt.Logs {
		if vLog.Topics[0] == parsedABI.Events["ChildCreated"].ID {
			unpacked, err := parsedABI.Unpack("ChildCreated", vLog.Data)
			if err == nil && len(unpacked) > 0 {
				childAddr = unpacked[0].(common.Address)
				break
			}
		}
	}

	if childAddr == (common.Address{}) {
		log.Fatalf("❌ Không tìm thấy Event ChildCreated!")
	}
	fmt.Printf("👶 Contract Con được đẻ ra tại: %s\n\n", childAddr.Hex())

	// 3. Truy vấn Account State của Contract Con
	fmt.Printf("🔍 BƯỚC 3: Đọc AccountState của Contract Con (%s) từ RPC...\n", childAddr.Hex())
	reqBody := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"mtn_getAccountState","params":["%s","latest"]}`, childAddr.Hex()))

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

	fmt.Println("\n📊 KẾT QUẢ CỦA CONTRACT CON TỪ RPC:")
	fmt.Printf("   - Contract Address   : %s\n", rpcResp.Result.Address)
	fmt.Printf("   - AccountType        : %d (1 = Smart Contract)\n", rpcResp.Result.AccountType)
	fmt.Printf("   - Kích thước SmartContractState : [%d chars]\n", len(rpcResp.Result.SmartContractState))

	if len(rpcResp.Result.SmartContractState) == 0 {
		fmt.Println("\n⚠️ TEST FAILED: Contract Con bị mồ côi (SmartContractState rỗng)!")
		fmt.Println("Lỗi: Quá trình thừa kế Gia Phả từ Factory sang Child thất bại!")
	} else {
		fmt.Println("\n✅ TEST PASSED: Contract Con đã được lưu và thừa kế đủ thông tin!")
		fmt.Println("\n🔎 CHI TIẾT SmartContractState của Contract Con (Protobuf Hex):")
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

	tx := types.NewContractCreation(nonce, big.NewInt(0), uint64(5000000), gasPrice, bytecode)

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

// sendTransactionAndWait
func sendTransactionAndWait(client *ethclient.Client, privateKey *ecdsa.PrivateKey, chainID int64, from, to common.Address, data []byte) (*types.Receipt, error) {
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return nil, fmt.Errorf("lỗi lấy nonce: %w", err)
	}

	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("lỗi lấy gas price: %w", err)
	}

	tx := types.NewTransaction(nonce, to, big.NewInt(0), uint64(5000000), gasPrice, data)

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), privateKey)
	if err != nil {
		return nil, fmt.Errorf("lỗi ký transaction: %w", err)
	}

	err = client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		return nil, fmt.Errorf("lỗi gửi transaction: %w", err)
	}

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
		return nil, fmt.Errorf("transaction thất bại, status: %v", receipt.Status)
	}

	return receipt, nil
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
