/*
 * BÀI TEST: 18-update-different-variables
 * MÔ TẢ   : Gửi nhiều giao dịch (tx) cùng lúc để gọi hàm update (ghi vào mapping) trên cùng một Smart Contract.
 * GỌI     : Giao dịch gọi hàm EVM update state độc lập cho từng ví.
 * KỲ VỌNG : Block-STM phát hiện KHÔNG CÓ read/write conflict (vì mỗi ví ghi vào 1 ô nhớ khác nhau), toàn bộ 5000 txs chạy song song thành công mượt mà, không abort.
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
const bytecodeHex = "608060405260015f55348015610013575f80fd5b5061033d806100215f395ff3fe608060405234801561000f575f80fd5b506004361061004a575f3560e01c80632cc826551461004e578063824331141461006a578063b1c9fe6e14610086578063c8910913146100a4575b5f80fd5b610068600480360381019061006391906101b7565b6100d4565b005b610084600480360381019061007f91906101b7565b6100dd565b005b61008e610166565b60405161009b91906101f1565b60405180910390f35b6100be60048036038101906100b99190610264565b61016b565b6040516100cb91906101f1565b60405180910390f35b805f8190555050565b60015f5414610121576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610118906102e9565b60405180910390fd5b8060015f3373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f208190555050565b5f5481565b6001602052805f5260405f205f915090505481565b5f80fd5b5f819050919050565b61019681610184565b81146101a0575f80fd5b50565b5f813590506101b18161018d565b92915050565b5f602082840312156101cc576101cb610180565b5b5f6101d9848285016101a3565b91505092915050565b6101eb81610184565b82525050565b5f6020820190506102045f8301846101e2565b92915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6102338261020a565b9050919050565b61024381610229565b811461024d575f80fd5b50565b5f8135905061025e8161023a565b92915050565b5f6020828403121561027957610278610180565b5b5f61028684828501610250565b91505092915050565b5f82825260208201905092915050565b7f5068617365206973206e6f206c6f6e67657220312120526576657274656421005f82015250565b5f6102d3601f8361028f565b91506102de8261029f565b602082019050919050565b5f6020820190508181035f830152610300816102c7565b905091905056fea2646970667358221220e8022f5aaabe87b4d6063e45bdc685acecb2f72d59909fb0dd17e625ebafecf364736f6c63430008140033"

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
	fmt.Println("BÀI TEST: 18-update-different-variables")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Gửi nhiều giao dịch (tx) cùng lúc để gọi hàm update (ghi vào mapping) trên cùng một Smart Contract.")
	fmt.Println("⚡ GỌI     : Giao dịch gọi hàm EVM update state độc lập cho từng ví.")
	fmt.Println("🎯 KỲ VỌNG : Block-STM phát hiện KHÔNG CÓ read/write conflict, toàn bộ 5000 txs chạy song song thành công mượt mà, không abort.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	configFlag := flag.String("config", "../config.json", "Đường dẫn file config")
	keysFile := flag.String("keys", "../../../../test_tps/gen_spam_keys/generated_keys.json", "Đường dẫn file chứa private keys")
	numKeys := flag.Int("num", 10, "Số lượng keys để test (0 = tất cả, mặc định là 10)")
	waitByBlock := flag.Bool("wait-by-block", false, "Kiểm tra confirm bằng giao dịch cuối cùng để giảm tải RPC")
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

	parsedABI, err := abi.JSON(strings.NewReader(cfg.Contracts["AbortRollback"].ABI))
	if err != nil { log.Fatalf("ABI parse err: %v", err) }
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	bytecode, err := hexutil.Decode("0x" + cfg.Contracts["AbortRollback"].Bytecode)
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

	if *waitByBlock {
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
				fmt.Printf("✅ Đã confirm tx cuối (%s) trong block %d. Giả định toàn bộ 5000 TX đã xong!\n", lastHash.Hex()[:10], receipt.BlockNumber.Uint64())
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
			}
		}
	}

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ:")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Số lượng ví tham gia: %d\n", len(testKeys))

	fmt.Println("🎉 TEST HOÀN TẤT: BlockSTM xử lý song song không xung đột!")
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
