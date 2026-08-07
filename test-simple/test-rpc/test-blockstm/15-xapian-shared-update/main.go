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

const abiJSON = `[
  {
    "inputs": [],
    "stateMutability": "nonpayable",
    "type": "constructor"
  },
  {
    "anonymous": false,
    "inputs": [
      {
        "indexed": true,
        "internalType": "address",
        "name": "wallet",
        "type": "address"
      },
      {
        "indexed": false,
        "internalType": "uint256",
        "name": "newCounter",
        "type": "uint256"
      },
      {
        "indexed": false,
        "internalType": "uint256",
        "name": "docId",
        "type": "uint256"
      }
    ],
    "name": "SharedUpdated",
    "type": "event"
  },
  {
    "inputs": [],
    "name": "getSharedDataFromDB",
    "outputs": [
      {
        "internalType": "uint256",
        "name": "",
        "type": "uint256"
      }
    ],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "incrementShared",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "initializeDoc",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "sharedDocId",
    "outputs": [
      {
        "internalType": "uint256",
        "name": "",
        "type": "uint256"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  }
]`
const bytecodeHex = "0x608060405234801561000f575f5ffd5b5061010773ffffffffffffffffffffffffffffffffffffffff1663fbdddaf06040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152506040518263ffffffff1660e01b81526004016100809190610136565b6020604051808303815f875af115801561009c573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906100c0919061018f565b506101ba565b5f81519050919050565b5f82825260208201905092915050565b8281835e5f83830152505050565b5f601f19601f8301169050919050565b5f610108826100c6565b61011281856100d0565b93506101228185602086016100e0565b61012b816100ee565b840191505092915050565b5f6020820190508181035f83015261014e81846100fe565b905092915050565b5f5ffd5b5f8115159050919050565b61016e8161015a565b8114610178575f5ffd5b50565b5f8151905061018981610165565b92915050565b5f602082840312156101a4576101a3610156565b5b5f6101b18482850161017b565b91505092915050565b610928806101c75f395ff3fe608060405234801561000f575f5ffd5b506004361061004a575f3560e01c80630b6f8f481461004e5780638cc382961461006c578063b4340bbe1461008a578063d32a9a5914610094575b5f5ffd5b61005661009e565b60405161006391906104b7565b60405180910390f35b6100746101b5565b60405161008191906104b7565b60405180910390f35b6100926101ba565b005b61009c610292565b005b5f5f61010773ffffffffffffffffffffffffffffffffffffffff16633e7c7f8b6040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f546040518363ffffffff1660e01b8152600401610113929190610540565b5f604051808303815f875af115801561012e573d5f5f3e3d5ffd5b505050506040513d5f823e3d601f19601f82011682018060405250810190610156919061069d565b90505f81511161019b576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101929061072e565b60405180910390fd5b808060200190518101906101af9190610776565b91505090565b5f5481565b61010773ffffffffffffffffffffffffffffffffffffffff16639d4799416040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f60405160200161021f91906104b7565b6040516020818303038152906040526040518363ffffffff1660e01b815260040161024b9291906107f3565b6020604051808303815f875af1158015610267573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061028b9190610776565b5f81905550565b5f61010773ffffffffffffffffffffffffffffffffffffffff16633e7c7f8b6040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f546040518363ffffffff1660e01b8152600401610306929190610540565b5f604051808303815f875af1158015610321573d5f5f3e3d5ffd5b505050506040513d5f823e3d601f19601f82011682018060405250810190610349919061069d565b90505f818060200190518101906103609190610776565b905060018161036f9190610855565b905061010773ffffffffffffffffffffffffffffffffffffffff1663c5d187dd6040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f54846040516020016103d891906104b7565b6040516020818303038152906040526040518463ffffffff1660e01b815260040161040593929190610888565b6020604051808303815f875af1158015610421573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906104459190610776565b5f819055503373ffffffffffffffffffffffffffffffffffffffff167f3d523d852046afd5dba341b55c89c4355e15f6c45d6785c48472c0b7eb922911825f546040516104939291906108cb565b60405180910390a25050565b5f819050919050565b6104b18161049f565b82525050565b5f6020820190506104ca5f8301846104a8565b92915050565b5f81519050919050565b5f82825260208201905092915050565b8281835e5f83830152505050565b5f601f19601f8301169050919050565b5f610512826104d0565b61051c81856104da565b935061052c8185602086016104ea565b610535816104f8565b840191505092915050565b5f6040820190508181035f8301526105588185610508565b905061056760208301846104a8565b9392505050565b5f604051905090565b5f5ffd5b5f5ffd5b5f5ffd5b5f5ffd5b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b6105bd826104f8565b810181811067ffffffffffffffff821117156105dc576105db610587565b5b80604052505050565b5f6105ee61056e565b90506105fa82826105b4565b919050565b5f67ffffffffffffffff82111561061957610618610587565b5b610622826104f8565b9050602081019050919050565b5f61064161063c846105ff565b6105e5565b90508281526020810184848401111561065d5761065c610583565b5b6106688482856104ea565b509392505050565b5f82601f8301126106845761068361057f565b5b815161069484826020860161062f565b91505092915050565b5f602082840312156106b2576106b1610577565b5b5f82015167ffffffffffffffff8111156106cf576106ce61057b565b5b6106db84828501610670565b91505092915050565b7f44617461206e6f7420666f756e6420696e2058617069616e00000000000000005f82015250565b5f6107186018836104da565b9150610723826106e4565b602082019050919050565b5f6020820190508181035f8301526107458161070c565b9050919050565b6107558161049f565b811461075f575f5ffd5b50565b5f815190506107708161074c565b92915050565b5f6020828403121561078b5761078a610577565b5b5f61079884828501610762565b91505092915050565b5f81519050919050565b5f82825260208201905092915050565b5f6107c5826107a1565b6107cf81856107ab565b93506107df8185602086016104ea565b6107e8816104f8565b840191505092915050565b5f6040820190508181035f83015261080b8185610508565b9050818103602083015261081f81846107bb565b90509392505050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f61085f8261049f565b915061086a8361049f565b925082820190508082111561088257610881610828565b5b92915050565b5f6060820190508181035f8301526108a08186610508565b90506108af60208301856104a8565b81810360408301526108c181846107bb565b9050949350505050565b5f6040820190506108de5f8301856104a8565b6108eb60208301846104a8565b939250505056fea2646970667358221220d92a869bfc8dd4924b06987339722c5496b5592ac9c3625305bcc545679ef75764736f6c63430008220033"

type Config struct {
	RPCUrl   string            `json:"rpc_url"`
	RPCNodes map[string]string `json:"rpc_nodes"`
	ChainID  int64             `json:"chain_id"`
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
	fmt.Println("⚡ GỌI     : Giao dịch gọi hàm incrementShared() từ 10 ví.")
	fmt.Println("🎯 KỲ VỌNG : Block-STM phải phát hiện read/write conflict, re-execute và cho kết quả đúng.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	rounds := flag.Int("rounds", 1, "Số round muốn test")
	configFlag := flag.String("config", "../config.json", "Đường dẫn file config")
	keysFile := flag.String("keys", "../../../../test_tps/gen_spam_keys/generated_keys.json", "Đường dẫn file chứa private keys")
	multiNodes := flag.Bool("multi", false, "Chế độ gửi giao dịch dàn trải lên nhiều RPC node từ config.json")
	numKeys := flag.Int("num", 10, "Số lượng keys để test (0 = tất cả, mặc định là 10)")
	waitMethod := flag.String("wait-method", "block", "Phương thức chờ giao dịch: 'block' hoặc 'receipt'")
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

	var rpcClients []*ethclient.Client
	if *multiNodes && len(cfg.RPCNodes) > 0 {
		for name, url := range cfg.RPCNodes {
			if c, e := ethclient.Dial(url); e == nil {
				rpcClients = append(rpcClients, c)
			} else {
				fmt.Printf("⚠️ Lỗi kết nối node %s (%s): %v\n", name, url, e)
			}
		}
	}
	if len(rpcClients) == 0 {
		if *multiNodes {
			fmt.Println("⚠️ Không cấu hình rpc_nodes trong config.json hoặc kết nối lỗi, fallback về RPC mặc định")
		}
		rpcClients = append(rpcClients, client)
	} else {
		fmt.Printf("🌐 Đã kết nối tới %d nodes RPC (Chế độ multi)\n", len(rpcClients))
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
	fmt.Println("✅ InitializeDoc thành công!")

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	type RoundSummary struct {
		Round        int
		SuccessTx    int
		TotalSuccess int
		DBValue      uint64
	}
	var summaries []RoundSummary

	start := time.Now()
	totalSuccess := 0

	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Fatalf("❌ Lỗi lấy startBlock: %v", err)
	}
	startBlock := header.Number.Uint64()

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

				clientForTx := rpcClients[idx%len(rpcClients)]
				hash, err := sendIncrementShared(clientForTx, pk, cfg.ChainID, from, contractAddr, parsedABI)
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

			// Giới hạn concurrency để không ddos sập Node RPC (Tối đa 50 goroutines cùng lúc)
			sem := make(chan struct{}, 50)
			
			// Chờ receipt song song để tăng tốc
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

		roundActual, err := getSharedDataFromDB(client, contractAddr, parsedABI)
		if err != nil {
			fmt.Printf("❌ Lỗi getSharedDataFromDB() sau round %d: %v\n", r, err)
		} else {
			fmt.Printf("\n📊 KẾT QUẢ ROUND %d:\n", r)
			fmt.Printf("   - Số tx thành công round này : %d\n", roundSuccess)
			fmt.Printf("   - Tổng tx thành công đến hiện tại: %d\n", totalSuccess)
			fmt.Printf("   - Giá trị Xapian DB thực tế  : %d\n", roundActual)
			if uint64(totalSuccess) == roundActual {
				fmt.Printf("   => ✅ ROUND PASSED\n")
			} else {
				fmt.Printf("   => ⚠️ ROUND FAILED (Lệch %d)\n", int(roundActual)-totalSuccess)
			}
			summaries = append(summaries, RoundSummary{
				Round:        r,
				SuccessTx:    roundSuccess,
				TotalSuccess: totalSuccess,
				DBValue:      roundActual,
			})
		}
	}

	actual, err := getSharedDataFromDB(client, contractAddr, parsedABI)
	if err != nil {
		log.Fatalf("❌ Lỗi getSharedDataFromDB(): %v", err)
	}

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ:")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Giá trị counter cuối cùng lưu trong Xapian DB: %d\n", actual)
	fmt.Printf("Tổng số lượng tx thành công: %d (trên %d round, mỗi round %d ví)\n", totalSuccess, *rounds, len(testKeys))

	fmt.Println("\n📋 BẢNG TỔNG HỢP CÁC ROUND:")
	fmt.Println("-------------------------------------------------------------------------")
	fmt.Printf("%-10s | %-12s | %-14s | %-10s | %-10s\n", "Round", "Tx Success", "Total Success", "DB Value", "Status")
	fmt.Println("-------------------------------------------------------------------------")
	for _, s := range summaries {
		status := "✅ PASSED"
		if uint64(s.TotalSuccess) != s.DBValue {
			status = "⚠️ FAILED"
		}
		fmt.Printf("%-10d | %-12d | %-14d | %-10d | %-10s\n", s.Round, s.SuccessTx, s.TotalSuccess, s.DBValue, status)
	}
	fmt.Println("-------------------------------------------------------------------------")

	fmt.Println("\n🏁 KẾT LUẬN CUỐI CÙNG:")
	if actual == uint64(totalSuccess) {
		fmt.Println("🎉 TEST PASSED: BlockSTM xử lý write conflict trên Xapian DB đúng!")
	} else {
		fmt.Printf("⚠️ TEST FAILED: Kỳ vọng %d nhưng nhận %d\n", totalSuccess, actual)
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	fmt.Println("  [Debug] PendingNonceAt...")
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return nil, err
	}
	fmt.Println("  [Debug] SuggestGasPrice...")
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		gasPrice = big.NewInt(1000000000)
	}

	fmt.Println("  [Debug] EstimateGas...")
	gasLimit, err := client.EstimateGas(context.Background(), ethereum.CallMsg{From: from, Data: bytecode})
	if err != nil {
		gasLimit = 5_000_000
	} else {
		gasLimit += 50_000
	}

	fmt.Println("  [Debug] SignTx & SendTransaction...")
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

func sendIncrementShared(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI) (common.Hash, error) {
	data, err := parsedABI.Pack("incrementShared")
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

func getSharedDataFromDB(client *ethclient.Client, addr *common.Address, parsedABI abi.ABI) (uint64, error) {
	data, _ := parsedABI.Pack("getSharedDataFromDB")
	result, err := client.CallContract(context.Background(), ethereum.CallMsg{To: addr, Data: data}, nil)
	if err != nil {
		return 0, err
	}
	outputs, err := parsedABI.Unpack("getSharedDataFromDB", result)
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
	// start := time.Now()
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
		// if time.Since(start) > 5*time.Second {
		// 	fmt.Printf("    [Debug] ...vẫn đang chờ receipt cho tx %s (đã chờ %v)...\n", txHash.Hex(), time.Since(start).Round(time.Second))
		// 	start = time.Now() // reset để in sau mỗi 5s
		// }
		time.Sleep(100 * time.Millisecond)
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
			time.Sleep(100 * time.Millisecond)
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
