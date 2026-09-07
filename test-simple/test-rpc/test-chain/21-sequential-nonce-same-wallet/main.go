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
	"tool-test/test-simple/test-rpc/test-chain/config"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type GeneratedKey struct {
	Index      int    `json:"index"`
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
}

type TestOptions struct {
	ConfigPath string
	KeysFile   string
	NumTxs     int
}

func RunTest(configPath string) error {
	return RunTestWithOptions(TestOptions{
		ConfigPath: configPath,
		NumTxs:     10,
	})
}

func RunTestWithOptions(opts TestOptions) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 21-sequential-nonce-same-wallet")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Gửi liên tục nhiều giao dịch từ MỘT ví với nonce tuần tự để gọi contract.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	if opts.ConfigPath == "" {
		opts.ConfigPath = "../config.json"
	}
	if opts.NumTxs <= 0 {
		opts.NumTxs = 10
	}

	cfg, err := config.LoadConfig(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("❌ Lỗi load config: %v", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		return fmt.Errorf("❌ Lỗi kết nối RPC: %v", err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(cfg.Contracts["TestCounter"].ABI))
	if err != nil {
		return fmt.Errorf("❌ Lỗi parse ABI: %v", err)
	}

	bytecode, err := hexutil.Decode("0x" + cfg.Contracts["TestCounter"].Bytecode)
	if err != nil {
		return fmt.Errorf("❌ Lỗi decode bytecode hex: %v", err)
	}

	testKeys, err := loadPrivateKeys(opts.KeysFile, cfg.PrivateKeys)
	if err != nil {
		return fmt.Errorf("❌ Lỗi load private keys: %w", err)
	}
	cfg.PrivateKeys = testKeys

	if len(testKeys) == 0 {
		return fmt.Errorf("❌ Không có private key nào được load")
	}

	// Chọn 1 ví duy nhất để test
	pk, err := crypto.HexToECDSA(testKeys[0])
	if err != nil {
		return fmt.Errorf("❌ Lỗi parse private key 0: %v", err)
	}
	from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

	fmt.Printf("🚀 Deploying contract with Account 0 (%s)...\n", from.Hex())
	contractAddr, err := deployContract(client, pk, cfg.ChainID, from, bytecode)
	if err != nil {
		return fmt.Errorf("❌ Deploy thất bại: %v", err)
	}
	fmt.Printf("📌 Contract deployed at: %s\n\n", contractAddr.Hex())

	fmt.Printf("🔥 Gửi %d giao dịch liên tiếp từ VÍ DUY NHẤT để update contract...\n", opts.NumTxs)
	start := time.Now()

	// Lấy nonce hiện tại của ví
	startNonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return fmt.Errorf("❌ Không thể lấy nonce của ví: %v", err)
	}

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	txHashes := make([]common.Hash, opts.NumTxs)

	for i := 0; i < opts.NumTxs; i++ {
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
		return fmt.Errorf("❌ Lỗi getCount(): %v", err)
	}

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ:")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Giá trị count cuối cùng: %d\n", actual)
	fmt.Printf("Số lượng giao dịch kỳ vọng: %d\n", opts.NumTxs)

	if actual == uint64(opts.NumTxs) {
		fmt.Println("🎉 TEST PASSED: Hệ thống xử lý mượt mà loạt giao dịch tuần tự từ 1 ví!")
		return nil
	}
	return fmt.Errorf("⚠️ TEST FAILED: Kỳ vọng %d nhưng nhận %d", opts.NumTxs, actual)
}

func main() {
	configFlag := flag.String("config", "../config.json", "Đường dẫn file config")
	keysFile := flag.String("keys", "", "Đường dẫn file chứa private keys tuỳ chọn (mặc định đọc từ config.json)")
	numTxs := flag.Int("num", 10, "Số lượng transaction liên tiếp (nonce tăng dần) muốn gửi")
	flag.Parse()

	configPath := *configFlag
	if flag.NArg() > 0 {
		configPath = flag.Arg(0)
	}

	opts := TestOptions{
		ConfigPath: configPath,
		KeysFile:   *keysFile,
		NumTxs:     *numTxs,
	}
	if err := RunTestWithOptions(opts); err != nil {
		log.Fatalf("%v", err)
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
			return nil, fmt.Errorf("❌ Timeout waiting for receipt %s", txHash.Hex())
		}
		receipt, err := client.TransactionReceipt(context.Background(), txHash)

		if err != nil && !strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("lỗi kết nối RPC: %w", err)
		}
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			return receipt, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// loadPrivateKeys loads private keys either from an explicitly passed keys file,
// or defaults to the keys defined in config.json.
// It supports both []string format and []GeneratedKey (index, private_key, address) format.
func loadPrivateKeys(keysFilePath string, cfgKeys []string) ([]string, error) {
	if keysFilePath != "" {
		raw, err := os.ReadFile(keysFilePath)
		if err != nil {
			return nil, fmt.Errorf("lỗi đọc file keys %s: %w", keysFilePath, err)
		}
		var strKeys []string
		if err := json.Unmarshal(raw, &strKeys); err == nil && len(strKeys) > 0 {
			return strKeys, nil
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
				return res, nil
			}
		}
		return nil, fmt.Errorf("không thể parse private key nào từ file %s", keysFilePath)
	}
	if len(cfgKeys) > 0 {
		return cfgKeys, nil
	}
	return nil, fmt.Errorf("không tìm thấy private key nào trong config.json hoặc file chỉ định")
}
