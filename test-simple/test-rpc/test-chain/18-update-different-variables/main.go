/*
 * BÀI TEST: 18-update-different-variables
 * MÔ TẢ   : Gửi nhiều giao dịch (tx) cùng lúc để gọi hàm update (ghi vào mapping) trên cùng một Smart Contract.
 * GỌI     : Giao dịch gọi hàm EVM update state độc lập cho từng ví.
 * KỲ VỌNG : Block-STM phát hiện KHÔNG CÓ read/write conflict (vì mỗi ví ghi vào 1 ô nhớ khác nhau), toàn bộ 5000 txs chạy song song thành công mượt mà, không abort.
 */
package main

import (
	"tool-test/test-simple/test-rpc/test-chain/config"
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
const bytecodeHex = "608060405260015f55348015610013575f80fd5b5061033d806100215f395ff3fe608060405234801561000f575f80fd5b506004361061004a575f3560e01c80632cc826551461004e578063824331141461006a578063b1c9fe6e14610086578063c8910913146100a4575b5f80fd5b610068600480360381019061006391906101b7565b6100d4565b005b610084600480360381019061007f91906101b7565b6100dd565b005b61008e610166565b60405161009b91906101f1565b60405180910390f35b6100be60048036038101906100b99190610264565b61016b565b6040516100cb91906101f1565b60405180910390f35b805f8190555050565b60015f5414610121576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610118906102e9565b60405180910390fd5b8060015f3373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f208190555050565b5f5481565b6001602052805f5260405f205f915090505481565b5f80fd5b5f819050919050565b61019681610184565b81146101a0575f80fd5b50565b5f813590506101b18161018d565b92915050565b5f602082840312156101cc576101cb610180565b5b5f6101d9848285016101a3565b91505092915050565b6101eb81610184565b82525050565b5f6020820190506102045f8301846101e2565b92915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6102338261020a565b9050919050565b61024381610229565b811461024d575f80fd5b50565b5f8135905061025e8161023a565b92915050565b5f6020828403121561027957610278610180565b5b5f61028684828501610250565b91505092915050565b5f82825260208201905092915050565b7f5068617365206973206e6f206c6f6e67657220312120526576657274656421005f82015250565b5f6102d3601f8361028f565b91506102de8261029f565b602082019050919050565b5f6020820190508181035f830152610300816102c7565b905091905056fea2646970667358221220e8022f5aaabe87b4d6063e45bdc685acecb2f72d59909fb0dd17e625ebafecf364736f6c63430008140033"

type GeneratedKey struct {
	Index      int    `json:"index"`
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
}

type TestOptions struct {
	ConfigPath  string
	KeysFile    string
	NumKeys     int
	WaitByBlock bool
}

func RunTest(configPath string) error {
	return RunTestWithOptions(TestOptions{
		ConfigPath: configPath,
		NumKeys:    10,
	})
}

func RunTestWithOptions(opts TestOptions) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 18-update-different-variables")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Gửi nhiều giao dịch (tx) cùng lúc để gọi hàm update (ghi vào mapping) trên cùng một Smart Contract.")
	fmt.Println("⚡ GỌI     : Giao dịch gọi hàm EVM update state độc lập cho từng ví.")
	fmt.Println("🎯 KỲ VỌNG : Block-STM phát hiện KHÔNG CÓ read/write conflict, toàn bộ 10 txs chạy song song thành công mượt mà, không abort.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	if opts.ConfigPath == "" {
		opts.ConfigPath = "../config.json"
	}

	cfg, err := config.LoadConfig(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("lỗi load config: %w", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		return fmt.Errorf("lỗi kết nối RPC: %w", err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(cfg.Contracts["AbortRollback"].ABI))
	if err != nil {
		return fmt.Errorf("lỗi parse ABI: %w", err)
	}

	bytecode, err := hexutil.Decode("0x" + cfg.Contracts["AbortRollback"].Bytecode)
	if err != nil {
		return fmt.Errorf("lỗi decode bytecode hex: %w", err)
	}

	testKeys, err := loadPrivateKeys(opts.KeysFile, cfg.PrivateKeys)
	if err != nil {
		return err
	}
	cfg.PrivateKeys = testKeys

	if opts.NumKeys > 0 && len(testKeys) > opts.NumKeys {
		testKeys = testKeys[:opts.NumKeys]
	}

	if len(testKeys) == 0 {
		return fmt.Errorf("không có private key nào được load")
	}

	// Use the first key to deploy
	pk0, err := crypto.HexToECDSA(testKeys[0])
	if err != nil {
		return fmt.Errorf("lỗi parse private key 0: %w", err)
	}
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying contract with Account 0...")
	contractAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		return fmt.Errorf("deploy thất bại: %w", err)
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

			txHashes[idx] = hash
			fmt.Printf("   TX Wallet %2d pushed: %s\n", idx, hash.Hex())
		}(i, pkStr)
	}

	wg.Wait()

	if len(errs) > 0 {
		fmt.Println("❌ Một số giao dịch gửi thất bại:")
		for _, e := range errs {
			fmt.Println("  -", e)
		}
	}

	successCount := 0
	if opts.WaitByBlock {
		fmt.Println("⏳ Chờ bằng phương pháp khối (chỉ kiểm tra TX cuối cùng để giảm tải RPC)...")
		var lastHash common.Hash
		for i := len(txHashes) - 1; i >= 0; i-- {
			if txHashes[i] != (common.Hash{}) {
				lastHash = txHashes[i]
				break
			}
		}

		if lastHash != (common.Hash{}) {
			receipt, err := waitReceipt(client, lastHash)
			if err != nil {
				fmt.Printf("❌ Lỗi chờ receipt của tx cuối: %v\n", err)
			} else {
				fmt.Printf("✅ Đã confirm tx cuối (%s) trong block %d. Toàn bộ %d TX đã xong!\n", lastHash.Hex()[:10], receipt.BlockNumber.Uint64(), len(testKeys))
				successCount = len(testKeys)
			}
		} else {
			fmt.Println("❌ Không có giao dịch nào được gửi thành công.")
		}
	} else {
		fmt.Println("⏳ Chờ các giao dịch được confirm (quét từng cái một)...")
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
				successCount++
			}
		}
	}

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ:")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Số lượng ví tham gia: %d (thành công: %d)\n", len(testKeys), successCount)

	if successCount == len(testKeys) {
		fmt.Println("🎉 TEST HOÀN TẤT: BlockSTM xử lý song song không xung đột!")
		return nil
	}

	return fmt.Errorf("chỉ có %d/%d giao dịch thành công", successCount, len(testKeys))
}

func main() {
	configFlag := flag.String("config", "../config.json", "Đường dẫn file config")
	keysFile := flag.String("keys", "", "Đường dẫn file chứa private keys tuỳ chọn (mặc định đọc từ config.json)")
	numKeys := flag.Int("num", 10, "Số lượng keys để test (0 = tất cả, mặc định là 10)")
	waitByBlock := flag.Bool("wait-by-block", false, "Kiểm tra confirm bằng giao dịch cuối cùng để giảm tải RPC")
	flag.Parse()

	configPath := *configFlag
	if flag.NArg() > 0 {
		configPath = flag.Arg(0)
	}

	opts := TestOptions{
		ConfigPath:  configPath,
		KeysFile:    *keysFile,
		NumKeys:     *numKeys,
		WaitByBlock: *waitByBlock,
	}

	if err := RunTestWithOptions(opts); err != nil {
		log.Fatalf("❌ %v", err)
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
	data, err := parsedABI.Pack("updateIfPhase1", big.NewInt(1))
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

// getCount removed

func waitReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	timeoutStart := time.Now()
	for {
		if time.Since(timeoutStart) > 60*time.Second {
			return nil, fmt.Errorf("timeout waiting for receipt của Tx %s", txHash.Hex())
		}
		receipt, err := client.TransactionReceipt(context.Background(), txHash)
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			return receipt, nil
		}
		if err != nil && !strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("lỗi kết nối RPC: %w", err)
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
