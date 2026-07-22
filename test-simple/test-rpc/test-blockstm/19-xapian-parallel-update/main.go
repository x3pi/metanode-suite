/*
 * BÀI TEST: 15-xapian-shared-update
 * MÔ TẢ   : Gửi nhiều giao dịch (tx) cùng lúc từ 10 ví khác nhau để gọi hàm cập nhật (tăng biến sharedCounter)
 *           lưu trong Xapian DB trên cùng một Smart Contract.
 * GỌI     : Giao dịch gọi hàm EVM update Xapian DB (qua precompile) trên 1 contract duy nhất.
 * KỲ VỌNG : Block-STM phải phát hiện read/write conflict trên Xapian DB Document, abort và re-execute
 *           để đảm bảo tính tuần tự. Giá trị counter cuối cùng lưu trong Xapian DB phải bằng tổng số tx thành công.
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

const abiJSON = `[{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"wallet","type":"address"},{"indexed":false,"internalType":"uint256","name":"newCounter","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"docId","type":"uint256"}],"name":"ParallelUpdated","type":"event"},{"inputs":[{"internalType":"address","name":"user","type":"address"}],"name":"getUserDataFromDB","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"incrementUser","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"initializeDoc","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"userDocIds","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`

const bytecodeHex = "0x608060405234801562000010575f80fd5b5061010773ffffffffffffffffffffffffffffffffffffffff1663fbdddaf06040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e00000000000000008152506040518263ffffffff1660e01b815260040162000083919062000161565b6020604051808303815f875af1158015620000a0573d5f803e3d5ffd5b505050506040513d601f19601f82011682018060405250810190620000c69190620001c1565b50620001f1565b5f81519050919050565b5f82825260208201905092915050565b5f5b8381101562000106578082015181840152602081019050620000e9565b5f8484015250505050565b5f601f19601f8301169050919050565b5f6200012d82620000cd565b620001398185620000d7565b93506200014b818560208601620000e7565b620001568162000111565b840191505092915050565b5f6020820190508181035f8301526200017b818462000121565b905092915050565b5f80fd5b5f8115159050919050565b6200019d8162000187565b8114620001a8575f80fd5b50565b5f81519050620001bb8162000192565b92915050565b5f60208284031215620001d957620001d862000183565b5b5f620001e884828501620001ab565b91505092915050565b610de280620001ff5f395ff3fe608060405234801561000f575f80fd5b506004361061004a575f3560e01c8063147806e31461004e578063a3e6f20714610058578063b4340bbe14610088578063d03df7a014610092575b5f80fd5b6100566100c2565b005b610072600480360381019061006d9190610855565b6104aa565b60405161007f9190610898565b60405180910390f35b610090610645565b005b6100ac60048036038101906100a79190610855565b6107d6565b6040516100b99190610898565b60405180910390f35b5f805f3373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f205490505f8082036102285761010773ffffffffffffffffffffffffffffffffffffffff16639d4799416040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e000000000000000081525060016040516020016101709190610898565b6040516020818303038152906040526040518363ffffffff1660e01b815260040161019c92919061098d565b6020604051808303815f875af11580156101b8573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906101dc91906109ec565b9150815f803373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f20819055506001905061041a565b5f61010773ffffffffffffffffffffffffffffffffffffffff16633e7c7f8b6040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e0000000000000000815250856040518363ffffffff1660e01b815260040161029b929190610a17565b5f604051808303815f875af11580156102b6573d5f803e3d5ffd5b505050506040513d5f823e3d601f19601f820116820180604052508101906102de9190610b63565b9050808060200190518101906102f491906109ec565b91506001826103039190610bd7565b915061010773ffffffffffffffffffffffffffffffffffffffff1663c5d187dd6040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e0000000000000000815250858560405160200161036b9190610898565b6040516020818303038152906040526040518463ffffffff1660e01b815260040161039893929190610c0a565b6020604051808303815f875af11580156103b4573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906103d891906109ec565b5f803373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f2081905550505b3373ffffffffffffffffffffffffffffffffffffffff167fbacacd063536a5bb218e91592803262fcc97f5a0282d5776c4b387a3c7261666825f803373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f205460405161049e929190610c4d565b60405180910390a25050565b5f805f808473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f205490505f810361052d576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161052490610cbe565b60405180910390fd5b5f61010773ffffffffffffffffffffffffffffffffffffffff16633e7c7f8b6040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e0000000000000000815250846040518363ffffffff1660e01b81526004016105a0929190610a17565b5f604051808303815f875af11580156105bb573d5f803e3d5ffd5b505050506040513d5f823e3d601f19601f820116820180604052508101906105e39190610b63565b90505f815111610628576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161061f90610d26565b60405180910390fd5b8080602001905181019061063c91906109ec565b92505050919050565b5f805f3373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f2054146106c3576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016106ba90610d8e565b60405180910390fd5b61010773ffffffffffffffffffffffffffffffffffffffff16639d4799416040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e00000000000000008152505f6040516020016107289190610898565b6040516020818303038152906040526040518363ffffffff1660e01b815260040161075492919061098d565b6020604051808303815f875af1158015610770573d5f803e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061079491906109ec565b5f803373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f2081905550565b5f602052805f5260405f205f915090505481565b5f604051905090565b5f80fd5b5f80fd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f610824826107fb565b9050919050565b6108348161081a565b811461083e575f80fd5b50565b5f8135905061084f8161082b565b92915050565b5f6020828403121561086a576108696107f3565b5b5f61087784828501610841565b91505092915050565b5f819050919050565b61089281610880565b82525050565b5f6020820190506108ab5f830184610889565b92915050565b5f81519050919050565b5f82825260208201905092915050565b5f5b838110156108e85780820151818401526020810190506108cd565b5f8484015250505050565b5f601f19601f8301169050919050565b5f61090d826108b1565b61091781856108bb565b93506109278185602086016108cb565b610930816108f3565b840191505092915050565b5f81519050919050565b5f82825260208201905092915050565b5f61095f8261093b565b6109698185610945565b93506109798185602086016108cb565b610982816108f3565b840191505092915050565b5f6040820190508181035f8301526109a58185610903565b905081810360208301526109b98184610955565b90509392505050565b6109cb81610880565b81146109d5575f80fd5b50565b5f815190506109e6816109c2565b92915050565b5f60208284031215610a0157610a006107f3565b5b5f610a0e848285016109d8565b91505092915050565b5f6040820190508181035f830152610a2f8185610903565b9050610a3e6020830184610889565b9392505050565b5f80fd5b5f80fd5b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b610a83826108f3565b810181811067ffffffffffffffff82111715610aa257610aa1610a4d565b5b80604052505050565b5f610ab46107ea565b9050610ac08282610a7a565b919050565b5f67ffffffffffffffff821115610adf57610ade610a4d565b5b610ae8826108f3565b9050602081019050919050565b5f610b07610b0284610ac5565b610aab565b905082815260208101848484011115610b2357610b22610a49565b5b610b2e8482856108cb565b509392505050565b5f82601f830112610b4a57610b49610a45565b5b8151610b5a848260208601610af5565b91505092915050565b5f60208284031215610b7857610b776107f3565b5b5f82015167ffffffffffffffff811115610b9557610b946107f7565b5b610ba184828501610b36565b91505092915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f610be182610880565b9150610bec83610880565b9250828201905080821115610c0457610c03610baa565b5b92915050565b5f6060820190508181035f830152610c228186610903565b9050610c316020830185610889565b8181036040830152610c438184610955565b9050949350505050565b5f604082019050610c605f830185610889565b610c6d6020830184610889565b9392505050565b7f4e6f7420696e697469616c697a656400000000000000000000000000000000005f82015250565b5f610ca8600f836108bb565b9150610cb382610c74565b602082019050919050565b5f6020820190508181035f830152610cd581610c9c565b9050919050565b7f44617461206e6f7420666f756e6420696e2058617069616e00000000000000005f82015250565b5f610d106018836108bb565b9150610d1b82610cdc565b602082019050919050565b5f6020820190508181035f830152610d3d81610d04565b9050919050565b7f416c726561647920696e697469616c697a6564000000000000000000000000005f82015250565b5f610d786013836108bb565b9150610d8382610d44565b602082019050919050565b5f6020820190508181035f830152610da581610d6c565b905091905056fea264697066735822122027362f8eea02f4ec5a71e7b3e06ec71a03312931c0a45ad94a01ee0e9ab11c9064736f6c63430008140033"

type Config struct {
	RPCUrl      string   `json:"rpc_url"`
	ChainID     int64    `json:"chain_id"`
}

type GeneratedKey struct {
	Index      int    `json:"index"`
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
}

func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 15-xapian-shared-update")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Gửi nhiều tx cùng lúc cập nhật chung 1 giá trị trên Xapian DB.")
	fmt.Println("⚡ GỌI     : Giao dịch gọi hàm incrementUser() từ 10 ví.")
	fmt.Println("🎯 KỲ VỌNG : Block-STM phải phát hiện read/write conflict, re-execute và cho kết quả đúng.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	rounds := flag.Int("rounds", 1, "Số round muốn test")
	waitMethod := flag.String("wait-method", "block", "Phương thức chờ giao dịch: 'block' hoặc 'receipt'")
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

	parsedABI, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	bytecode, err := hexutil.Decode(bytecodeHex)
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

	fmt.Println("⚙️ Initializing Document...")
	initHash, err := sendInitializeDoc(client, pk0, cfg.ChainID, from0, contractAddr, parsedABI)
	if err != nil {
		log.Fatalf("❌ InitializeDoc failed: %v", err)
	}
	fmt.Printf("   TX Hash: %s\n", initHash.Hex())

	initReceipt, err := waitReceipt(client, initHash)
	if err != nil || initReceipt.Status != 1 {
		log.Fatalf("❌ Khởi tạo Document thất bại!")
	}
	fmt.Println("✅ InitializeDoc thành công!\n")

	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Fatalf("❌ Lỗi lấy Header: %v", err)
	}
	startBlock := header.Number.Uint64()

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	start := time.Now()
	totalSuccess := 0

	for r := 1; r <= *rounds; r++ {
		fmt.Printf("\n🔥 --- ROUND %d/%d --- 🔥\n", r, *rounds)
		fmt.Printf("🔥 Gửi %d giao dịch đồng thời để update Xapian DB...\n", len(testKeys))

		// WaitGroup and channels to track tx hashes
		txHashes := make([]common.Hash, len(testKeys))

		for i, pkStr := range testKeys {
			wg.Add(1)
			go func(idx int, pKeyHex string) {
				defer wg.Done()

				pk, err := crypto.HexToECDSA(pKeyHex)
				if err != nil {
					errsMu.Lock()
					errs = append(errs, fmt.Errorf("round %d - lỗi key %d: %v", r, idx, err))
					errsMu.Unlock()
					return
				}
				from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

				hash, err := sendIncrementUser(client, pk, cfg.ChainID, from, contractAddr, parsedABI)
				if err != nil {
					errsMu.Lock()
					errs = append(errs, fmt.Errorf("round %d - lỗi send tx từ wallet %d: %v", r, idx, err))
					errsMu.Unlock()
					return
				}

				fmt.Printf("✅ Round %d - Wallet %d gửi tx thành công: %s\n", r, idx, hash.Hex())
				txHashes[idx] = hash
			}(i, pkStr)
		}

		wg.Wait()

		if len(errs) > 0 {
			fmt.Println("❌ Một số giao dịch gửi thất bại trong round này:")
			errsMu.Lock()
			for _, e := range errs {
				fmt.Println("  -", e)
			}
			errs = nil // reset cho round sau
			errsMu.Unlock()
		}

		var roundSuccess int
		if *waitMethod == "receipt" {
			fmt.Println("⏳ Chờ các giao dịch được confirm bằng cách lấy Receipt...")
			successCount := 0
			var wgReceipt sync.WaitGroup
			var mu sync.Mutex
			
			donePrint := make(chan struct{})
			go func() {
				ticker := time.NewTicker(3 * time.Second)
				defer ticker.Stop()
				startTime := time.Now()
				for {
					select {
					case <-ticker.C:
						mu.Lock()
						current := successCount
						mu.Unlock()
						fmt.Printf("   [⏳ Waiting Receipt] Đã confirm %d/%d txs... (Thời gian chờ: %v)\n", current, len(txHashes), time.Since(startTime).Round(time.Second))
					case <-donePrint:
						return
					}
				}
			}()

			// Giới hạn concurrency để không ddos sập Node RPC
			sem := make(chan struct{}, 50)
			
			for _, h := range txHashes {
				if h == (common.Hash{}) {
					continue
				}
				wgReceipt.Add(1)
				go func(txHash common.Hash) {
					defer wgReceipt.Done()
					sem <- struct{}{} // Acquire token
					defer func() { <-sem }() // Release token
					
					receipt, err := waitReceipt(client, txHash)
					if err == nil && receipt != nil && receipt.Status == 1 {
						mu.Lock()
						successCount++
						mu.Unlock()
					}
				}(h)
			}
			wgReceipt.Wait()
			close(donePrint)
			roundSuccess = successCount
			totalSuccess += roundSuccess
			fmt.Printf("✅ Đã confirm %d/%d giao dịch bằng Receipt trong round %d\n", roundSuccess, len(txHashes), r)
		} else {
			fmt.Println("⏳ Chờ các giao dịch được confirm bằng cách quét Block...")
			successCount, err := waitForTxHashesByBlock(client, txHashes, startBlock)
			if err != nil {
				fmt.Printf("❌ Lỗi khi chờ block: %v\n", err)
			}
			roundSuccess = successCount
			totalSuccess += roundSuccess
			fmt.Printf("✅ Đã confirm %d/%d giao dịch bằng quét Block trong round %d\n", roundSuccess, len(txHashes), r)
		}

		// Verify that from0 incremented correctly for this round
		actual, err := getUserDataFromDB(client, contractAddr, parsedABI, from0)
		if err != nil {
			log.Fatalf("❌ Lỗi getUserDataFromDB(): %v", err)
		}

		// Since from0 sent exactly 1 transaction per round, its counter should be `r`.
		expectedSuccess := r
		if actual.Uint64() == uint64(expectedSuccess) {
			fmt.Printf("✅ ROUND %d SUCCESS! from0 Counter = %d (Khớp)\n", r, actual.Uint64())
		} else {
			fmt.Printf("❌ ROUND %d FAILED! from0 Counter = %d (Expected: %d)\n", r, actual.Uint64(), expectedSuccess)
		}
	}

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ:")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Tổng số lượng tx thành công: %d (trên %d round, mỗi round %d ví)\n", totalSuccess, *rounds, len(testKeys))
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

// sendIncrementUser calls incrementUser()
func sendIncrementUser(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI) (common.Hash, error) {
	data, err := parsedABI.Pack("incrementUser")
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
		gasLimit = 1_000_000
	} else {
		gasLimit += 100_000
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

func sendInitializeDoc(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI) (common.Hash, error) {
	data, err := parsedABI.Pack("initializeDoc")
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
		gasLimit = 2_000_000
	} else {
		gasLimit += 200_000
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

func getUserDataFromDB(client *ethclient.Client, contractAddr *common.Address, parsedABI abi.ABI, user common.Address) (*big.Int, error) {
	data, err := parsedABI.Pack("getUserDataFromDB", user)
	if err != nil {
		return nil, err
	}

	msg := ethereum.CallMsg{
		To:   contractAddr,
		Data: data,
	}
	result, err := client.CallContract(context.Background(), msg, nil)
	if err != nil {
		return nil, err
	}

	res, err := parsedABI.Unpack("getUserDataFromDB", result)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("không có kết quả trả về")
	}

	val, ok := res[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("kết quả không phải là *big.Int")
	}
	return val, nil
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

func waitForTxHashesByBlock(client *ethclient.Client, txHashes []common.Hash, startBlock uint64) (int, error) {
	pending := make(map[common.Hash]bool)
	for _, h := range txHashes {
		if h != (common.Hash{}) {
			pending[h] = true
		}
	}

	if len(pending) == 0 {
		return 0, nil
	}

	lastChecked := startBlock
	totalSuccess := 0
	totalTxs := len(pending)

	fmt.Printf("   [Info] Đang chờ %d giao dịch từ block %d...\n", totalTxs, lastChecked)

	startTime := time.Now()
	lastLogTime := time.Now()
	var currentLatestBlock uint64 = lastChecked

	for len(pending) > 0 {
		header, err := client.HeaderByNumber(context.Background(), nil)
		if err != nil {
			fmt.Printf("   [Error] HeaderByNumber lỗi: %v. Sẽ thử lại sau...\n", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		latestBlock := header.Number.Uint64()
		currentLatestBlock = latestBlock


		for b := lastChecked; b <= latestBlock; b++ {
			var block *types.Block
			var err error
			for retry := 0; retry < 3; retry++ {
				block, err = client.BlockByNumber(context.Background(), big.NewInt(int64(b)))
				if err == nil {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			if err != nil {
				fmt.Printf("   [Error] Không thể lấy block %d: %v. Dừng việc quét các block tiếp theo và sẽ thử lại!\n", b, err)
				break
			}

			txs := block.Transactions()
			foundInBlock := 0
			for _, tx := range txs {
				if pending[tx.Hash()] {
					delete(pending, tx.Hash())
					totalSuccess++
					foundInBlock++
				}
			}
			if foundInBlock > 0 {
				fmt.Printf("   [Info] Block %d chứa %d giao dịch của round này (còn lại: %d)\n", b, foundInBlock, len(pending))
			}
			lastChecked = b + 1
		}

		if len(pending) > 0 {
			if time.Since(lastLogTime) > 3*time.Second {
				fmt.Printf("   [⏳ Waiting] Đã confirm %d/%d txs... (Đang check tới block %d, Thời gian chờ: %v)\n", totalSuccess, totalTxs, currentLatestBlock, time.Since(startTime).Round(time.Second))
				lastLogTime = time.Now()
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	return totalSuccess, nil
}
