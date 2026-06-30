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

const abiJSON = `[{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"wallet","type":"address"},{"indexed":false,"internalType":"uint256","name":"newCounter","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"docId","type":"uint256"}],"name":"SharedUpdated","type":"event"},{"inputs":[],"name":"getSharedDataFromDB","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"incrementShared","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"initializeDoc","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"sharedCounter","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"sharedDocId","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`
const bytecodeHex = "0x608060405234801561000f575f80fd5b5061010773ffffffffffffffffffffffffffffffffffffffff1663fbdddaf06040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152506040518263ffffffff1660e01b81526004016100809190610150565b6020604051808303815f875af115801561009c573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906100c091906101a9565b506101d4565b5f81519050919050565b5f82825260208201905092915050565b5f5b838110156100fd5780820151818401526020810190506100e2565b5f8484015250505050565b5f601f19601f8301169050919050565b5f610122826100c6565b61012c81856100d0565b935061013c8185602086016100e0565b61014581610108565b840191505092915050565b5f6020820190508181035f8301526101688184610118565b905092915050565b5f80fd5b5f8115159050919050565b61018881610174565b8114610192575f80fd5b50565b5f815190506101a38161017f565b92915050565b5f602082840312156101be576101bd610170565b5b5f6101cb84828501610195565b91505092915050565b6108ab80620001e25f395ff3fe608060405234801561000f575f80fd5b5060043610610055575f3560e01c80630b6f8f48146100595780638cc38296146100775780638ff7572a14610095578063b4340bbe146100b3578063d32a9a59146100bd575b5f80fd5b6100616100c7565b60405161006e9190610420565b60405180910390f35b61007f61019b565b60405161008c9190610420565b60405180910390f35b61009d6101a0565b6040516100aa9190610420565b60405180910390f35b6100bb6101a6565b005b6100c56102c1565b005b5f8061010773ffffffffffffffffffffffffffffffffffffffff16633e7c7f8b6040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f546040518363ffffffff1660e01b815260040161013c9291906104c3565b5f604051808303815f875af1158015610157573d5f803e3d5ffd5b505050506040513d5f823e3d601f19601f8201168201806040525081019061017f9190610620565b9050808060200190518101906101959190610691565b91505090565b5f5481565b60015481565b5f8054146101e9576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101e090610706565b60405180910390fd5b61010773ffffffffffffffffffffffffffffffffffffffff16639d4799416040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f60405160200161024e9190610420565b6040516020818303038152906040526040518363ffffffff1660e01b815260040161027a929190610776565b6020604051808303815f875af1158015610296573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906102ba9190610691565b5f81905550565b6001805f8282546102d291906107d8565b925050819055505f600154905061010773ffffffffffffffffffffffffffffffffffffffff1663c5d187dd6040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f54846040516020016103469190610420565b6040516020818303038152906040526040518463ffffffff1660e01b81526004016103739392919061080b565b6020604051808303815f875af115801561038f573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906103b39190610691565b503373ffffffffffffffffffffffffffffffffffffffff167f3d523d852046afd5dba341b55c89c4355e15f6c45d6785c48472c0b7eb922911825f546040516103fd92919061084e565b60405180910390a250565b5f819050919050565b61041a81610408565b82525050565b5f6020820190506104335f830184610411565b92915050565b5f81519050919050565b5f82825260208201905092915050565b5f5b83811015610470578082015181840152602081019050610455565b5f8484015250505050565b5f601f19601f8301169050919050565b5f61049582610439565b61049f8185610443565b93506104af818560208601610453565b6104b88161047b565b840191505092915050565b5f6040820190508181035f8301526104db818561048b565b90506104ea6020830184610411565b9392505050565b5f604051905090565b5f80fd5b5f80fd5b5f80fd5b5f80fd5b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b6105408261047b565b810181811067ffffffffffffffff8211171561055f5761055e61050a565b5b80604052505050565b5f6105716104f1565b905061057d8282610537565b919050565b5f67ffffffffffffffff82111561059c5761059b61050a565b5b6105a58261047b565b9050602081019050919050565b5f6105c46105bf84610582565b610568565b9050828152602081018484840111156105e0576105df610506565b5b6105eb848285610453565b509392505050565b5f82601f83011261060757610606610502565b5b81516106178482602086016105b2565b91505092915050565b5f60208284031215610635576106346104fa565b5b5f82015167ffffffffffffffff811115610652576106516104fe565b5b61065e848285016105f3565b91505092915050565b61067081610408565b811461067a575f80fd5b50565b5f8151905061068b81610667565b92915050565b5f602082840312156106a6576106a56104fa565b5b5f6106b38482850161067d565b91505092915050565b7f416c726561647920696e697469616c697a6564000000000000000000000000005f82015250565b5f6106f0601383610443565b91506106fb826106bc565b602082019050919050565b5f6020820190508181035f83015261071d816106e4565b9050919050565b5f81519050919050565b5f82825260208201905092915050565b5f61074882610724565b610752818561072e565b9350610762818560208601610453565b61076b8161047b565b840191505092915050565b5f6040820190508181035f83015261078e818561048b565b905081810360208301526107a2818461073e565b90509392505050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f6107e282610408565b91506107ed83610408565b9250828201905080821115610805576108046107ab565b5b92915050565b5f6060820190508181035f830152610823818661048b565b90506108326020830185610411565b8181036040830152610844818461073e565b9050949350505050565b5f6040820190506108615f830185610411565b61086e6020830184610411565b939250505056fea26469706673582212204b9d38e9071344784d6b4076f31b5b102e9c0ab749e8a0dfc3d19203bd39221364736f6c63430008140033"

type Config struct {
	RPCUrl      string   `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
	ChainID     int64    `json:"chain_id"`
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

	configPath := "../config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
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

	if len(cfg.PrivateKeys) == 0 {
		log.Fatalf("❌ Không có private key nào trong config")
	}

	// Lấy tối đa 10 private keys cho bài test này
	testKeys := cfg.PrivateKeys
	if len(testKeys) > 10 {
		testKeys = testKeys[:10]
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

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	fmt.Printf("🔥 Gửi %d giao dịch đồng thời để update Xapian DB...\n", len(testKeys))
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

			hash, err := sendIncrementShared(client, pk, cfg.ChainID, from, contractAddr, parsedABI)
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

	actual, err := getSharedDataFromDB(client, contractAddr, parsedABI)
	if err != nil {
		log.Fatalf("❌ Lỗi getSharedDataFromDB(): %v", err)
	}

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ:")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Giá trị counter cuối cùng lưu trong Xapian DB: %d\n", actual)
	fmt.Printf("Số lượng ví tham gia: %d\n", len(testKeys))

	if actual == uint64(len(testKeys)) {
		fmt.Println("🎉 TEST PASSED: BlockSTM xử lý write conflict trên Xapian DB đúng!")
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
