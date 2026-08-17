/*
 * BÀI TEST: 21-sequential-nonce-same-wallet
 * MÔ TẢ   : Gửi liên tục nhiều giao dịch (tx) cùng lúc từ MỘT ví duy nhất (cùng địa chỉ) với nonce tăng dần liên tục, gọi hàm update (tăng biến count) trên cùng một Smart Contract.
 * GỌI     : Giao dịch gọi hàm EVM update state trên 1 contract duy nhất.
 * KỲ VỌNG : Hệ thống (Block-STM) phải sắp xếp đúng thứ tự nonce của ví này và xử lý tuần tự một cách chính xác mà không bị race condition hoặc lỗi. Giá trị count cuối cùng phải bằng tổng số tx thành công.
 */
package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
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

// ABI definitions
const bytecodeHex = "6080604052348015600e575f5ffd5b506101818061001c5f395ff3fe608060405234801561000f575f5ffd5b5060043610610034575f3560e01c8063a87d942c14610038578063d09de08a14610056575b5f5ffd5b610040610060565b60405161004d91906100d2565b60405180910390f35b61005e610068565b005b5f5f54905090565b60015f5f8282546100799190610118565b925050819055507f20d8a6f5a693f9d1d627a598e8820f7a55ee74c183aa8f1a30e8d4e8dd9a8d845f546040516100b091906100d2565b60405180910390a1565b5f819050919050565b6100cc816100ba565b82525050565b5f6020820190506100e55f8301846100c3565b92915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f610122826100ba565b915061012d836100ba565b9250828201905080821115610145576101446100eb565b5b9291505056fea264697066735822122039d409b6689485dd66eca57d0dcf22759cc7ed07190b1be8653d9dbfaf9f518464736f6c63430008220033"

type ContractData struct {
	ABI      string `json:"abi"`
	Bytecode string `json:"bytecode"`
}

type Config struct {
	PrivateKeys []string                  `json:"private_keys"`
	RPCUrl      string                  `json:"rpc_url"`
	ChainID     int64                   `json:"chain_id"`
	Contracts   map[string]ContractData `json:"contracts"`
}

type GeneratedKey struct {
	Index      int    `json:"index"`
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
}

func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 21-sequential-nonce-same-wallet")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Gửi liên tục nhiều giao dịch từ MỘT ví với nonce tuần tự để gọi contract.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	configFlag := flag.String("config", "../config.json", "Đường dẫn file config")
keysFile := flag.String("keys", "", "Đường dẫn file chứa private keys tuỳ chọn (mặc định đọc từ config.json)")
	numTxs := flag.Int("num", 10, "Số lượng transaction liên tiếp (nonce tăng dần) muốn gửi")
	flag.Parse()

	configPath := *configFlag
	if flag.NArg() > 0 {
		configPath = flag.Arg(0)
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

	parsedABI, err := abi.JSON(strings.NewReader(cfg.Contracts["TestCounter"].ABI))
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	bytecode, err := hexutil.Decode("0x" + cfg.Contracts["TestCounter"].Bytecode)
	if err != nil {
		log.Fatalf("❌ Lỗi decode bytecode hex: %v", err)
	}

	testKeys := loadPrivateKeys(*keysFile, cfg.PrivateKeys)
	cfg.PrivateKeys = testKeys
	
	if len(testKeys) == 0 {
		log.Fatalf("❌ Không có private key nào được load")
	}

	// Chọn 1 ví duy nhất để test
	pk, err := crypto.HexToECDSA(testKeys[0])
	if err != nil {
		log.Fatalf("❌ Lỗi parse private key 0: %v", err)
	}
	from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

	fmt.Printf("🚀 Deploying contract with Account 0 (%s)...\n", from.Hex())
	contractAddr, err := deployContract(client, pk, cfg.ChainID, from, bytecode)
	if err != nil {
		log.Fatalf("❌ Deploy thất bại: %v", err)
	}
	fmt.Printf("📌 Contract deployed at: %s\n\n", contractAddr.Hex())

	fmt.Printf("🔥 Gửi %d giao dịch liên tiếp từ VÍ DUY NHẤT để update contract...\n", *numTxs)
	start := time.Now()

	// Lấy nonce hiện tại của ví
	startNonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		log.Fatalf("❌ Không thể lấy nonce của ví: %v", err)
	}

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	txHashes := make([]common.Hash, *numTxs)

	for i := 0; i < *numTxs; i++ {
		wg.Add(1)
		// Gửi song song nhưng set cứng nonce từ startNonce + i
		go func(idx int, currentNonce uint64) {
			defer wg.Done()

			hash, err := sendIncrementWithNonce(client, pk, cfg.ChainID, from, contractAddr, parsedABI, currentNonce)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi send tx (nonce %d): %v", currentNonce, err))
				errsMu.Unlock()
				return
			}

			fmt.Printf("✅ Đã phát sóng tx với nonce %d: %s\n", currentNonce, hash.Hex())
			txHashes[idx] = hash
		}(i, startNonce+uint64(i))
	}

	wg.Wait()

	if len(errs) > 0 {
		fmt.Println("❌ Một số giao dịch gửi thất bại:")
		for _, e := range errs {
			fmt.Println("  -", e)
		}
	}

	fmt.Println("⏳ Chờ các giao dịch được confirm...")
	for i, hash := range txHashes {
		if hash == (common.Hash{}) {
			continue
		}
		receipt, err := waitReceipt(client, hash)
		if err != nil {
			fmt.Printf("❌ Tx %d (nonce %d) chờ receipt thất bại: %v\n", i, startNonce+uint64(i), err)
		} else if receipt.Status != 1 {
			fmt.Printf("❌ Tx %d (nonce %d) bị revert!\n", i, startNonce+uint64(i))
		} else {
			fmt.Printf("✅ Tx %d (nonce %d) %s confirmed trong block %d\n", i, startNonce+uint64(i), hash.Hex()[:10]+"...", receipt.BlockNumber.Uint64())
		}
	}

	actual, err := getCount(client, contractAddr, parsedABI)
	if err != nil {
		log.Fatalf("❌ Lỗi getCount(): %v", err)
	}

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ:")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Giá trị count cuối cùng: %d\n", actual)
	fmt.Printf("Số lượng giao dịch kỳ vọng: %d\n", *numTxs)

	if actual == uint64(*numTxs) {
		fmt.Println("🎉 TEST PASSED: Hệ thống xử lý mượt mà loạt giao dịch tuần tự từ 1 ví!")
	} else {
		fmt.Printf("⚠️ TEST FAILED: Kỳ vọng %d nhưng nhận %d\n", *numTxs, actual)
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

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

func sendIncrementWithNonce(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI, nonce uint64) (common.Hash, error) {
	data, err := parsedABI.Pack("increment")
	if err != nil {
		return common.Hash{}, err
	}

	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		gasPrice = big.NewInt(1000000000)
	}

	// Hardcode gasLimit for speed and to avoid nonce errors during estimation when broadcasting concurrently
	gasLimit := uint64(150_000)

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

func getCount(client *ethclient.Client, addr *common.Address, parsedABI abi.ABI) (uint64, error) {
	data, _ := parsedABI.Pack("getCount")
	result, err := client.CallContract(context.Background(), ethereum.CallMsg{To: addr, Data: data}, nil)
	if err != nil {
		return 0, err
	}
	outputs, err := parsedABI.Unpack("getCount", result)
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
