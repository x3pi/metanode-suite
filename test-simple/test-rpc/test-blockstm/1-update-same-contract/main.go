/*
 * BÀI TEST: 1-update-same-contract
 * MÔ TẢ   : Gửi nhiều giao dịch (tx) cùng lúc để gọi hàm update (tăng biến count) trên cùng một Smart Contract.
 * GỌI     : Giao dịch gọi hàm EVM update state trên 1 contract duy nhất.
 * KỲ VỌNG : Block-STM phải phát hiện read/write conflict, abort và re-execute để đảm bảo tính tuần tự. Giá trị count cuối cùng phải bằng tổng số tx thành công.
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
	fmt.Println("BÀI TEST: 1-update-same-contract")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Gửi nhiều giao dịch (tx) cùng lúc để gọi hàm update (tăng biến count) trên cùng một Smart Contract.")
	fmt.Println("⚡ GỌI     : Giao dịch gọi hàm EVM update state trên 1 contract duy nhất.")
	fmt.Println("🎯 KỲ VỌNG : Block-STM phải phát hiện read/write conflict, abort và re-execute để đảm bảo tính tuần tự. Giá trị count cuối cùng phải bằng tổng số tx thành công.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	configFlag := flag.String("config", "../config.json", "Đường dẫn file config")
	keysFile := flag.String("keys", "../../../../test_tps/gen_spam_keys/generated_keys.json", "Đường dẫn file chứa private keys")
	numKeys := flag.Int("num", 10, "Số lượng keys để test (0 = tất cả, mặc định là 10)")
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
	if err != nil { log.Fatalf("ABI parse err: %v", err) }
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	bytecode, err := hexutil.Decode("0x" + cfg.Contracts["TestCounter"].Bytecode)
	if err != nil { log.Fatalf("Bytecode err: %v", err) }
	if err != nil {
		log.Fatalf("❌ Lỗi decode bytecode hex: %v", err)
	}

	keysRaw, err := os.ReadFile(*keysFile)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc file keys %s: %v", *keysFile, err)
	}
	var genKeys []GeneratedKey
	if err := json.Unmarshal(keysRaw, &genKeys); err != nil {
		log.Fatalf("❌ Lỗi parse keys: %v", err)
	}

	var testKeys []string
	for _, gk := range genKeys {
		testKeys = append(testKeys, gk.PrivateKey)
	}

	if *numKeys > 0 && len(testKeys) > *numKeys {
		testKeys = testKeys[:*numKeys]
	}

	if len(testKeys) == 0 {
		log.Fatalf("❌ Không có private key nào được load")
	}

	// Use the first key to deploy
	pk0, err := crypto.HexToECDSA(testKeys[0])
	if err != nil {
		log.Fatalf("❌ Lỗi parse private key 0: %v", err)
	}
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying contract with Account 0...")
	contractAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		log.Fatalf("❌ Deploy thất bại: %v", err)
	}
	fmt.Printf("📌 Contract deployed at: %s\n\n", contractAddr.Hex())

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	fmt.Printf("🔥 Gửi %d giao dịch đồng thời để update contract...\n", len(testKeys))
	start := time.Now()

	// WaitGroup and channels to track tx hashes
	txHashes := make([]common.Hash, len(testKeys))

	for i, pkStr := range testKeys {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()

			pk, err := crypto.HexToECDSA(pKeyHex)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi key %d: %v", idx, err))
				errsMu.Unlock()
				return
			}
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

			hash, err := sendIncrement(client, pk, cfg.ChainID, from, contractAddr, parsedABI)
			if err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi send tx từ wallet %d: %v", idx, err))
				errsMu.Unlock()
				return
			}

			fmt.Printf("✅ Wallet %d gửi tx thành công: %s\n", idx, hash.Hex())
			txHashes[idx] = hash
		}(i, pkStr)
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
			fmt.Printf("❌ Wallet %d chờ receipt thất bại: %v\n", i, err)
		} else if receipt.Status != 1 {
			fmt.Printf("❌ Wallet %d Tx bị revert!\n", i)
		} else {
			fmt.Printf("✅ Wallet %d Tx %s confirmed trong block %d\n", i, hash.Hex()[:10]+"...", receipt.BlockNumber.Uint64())
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
	fmt.Printf("Số lượng ví tham gia: %d\n", len(testKeys))

	if actual == uint64(len(testKeys)) {
		fmt.Println("🎉 TEST PASSED: BlockSTM xử lý đúng!")
	} else {
		fmt.Printf("⚠️ TEST FAILED: Kỳ vọng %d nhưng nhận %d\n", len(testKeys), actual)
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

func sendIncrement(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI) (common.Hash, error) {
	data, err := parsedABI.Pack("increment")
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
	for {
		receipt, err := client.TransactionReceipt(context.Background(), txHash)
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			return receipt, nil
		}
		if err != nil && err.Error() != "not found" {
			return nil, err
		}
		time.Sleep(300 * time.Millisecond)
	}
}
