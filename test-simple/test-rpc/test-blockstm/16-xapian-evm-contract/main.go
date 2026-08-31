/*
 * BÀI TEST: 16-xapian-evm-contract
 * MÔ TẢ   : Gửi nhiều giao dịch (tx) cùng lúc từ 10 ví khác nhau để gọi hàm cập nhật (tăng biến sharedCounter)
 *           và tăng biến count trên EVM contract.
 * GỌI     : 5 ví gọi Xapian contract, 5 ví gọi EVM contract
 * KỲ VỌNG : Giá trị count của EVM là 10, của Xapian là 5.
 */
package main

import (
	"tool-test/test-simple/test-rpc/test-blockstm/config"
	"context"
	"crypto/ecdsa"
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

const counterAbiJSON = `[{"inputs":[],"name":"count","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"increase","outputs":[],"stateMutability":"nonpayable","type":"function"}]`
const counterBytecodeHex = "0x608060405234801561000f575f80fd5b506101588061001d5f395ff3fe608060405234801561000f575f80fd5b5060043610610034575f3560e01c806306661abd14610038578063e8927fbc14610056575b5f80fd5b610040610060565b60405161004d9190610095565b60405180910390f35b61005e610065565b005b5f5481565b5f80815480929190610076906100db565b9190505550565b5f819050919050565b61008f8161007d565b82525050565b5f6020820190506100a85f830184610086565b92915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f6100e58261007d565b91507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff8203610117576101166100ae565b5b60018201905091905056fea26469706673582212202f0b2f35f463bf9ea5e53037345bfa8d79d0908cc16b43f47f4e8f355853259964736f6c63430008140033"

const sharedUpdateAbiJSON = `[{"inputs":[{"internalType":"address","name":"_counter","type":"address"}],"stateMutability":"nonpayable","type":"constructor"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"wallet","type":"address"},{"indexed":false,"internalType":"uint256","name":"newCounter","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"docId","type":"uint256"}],"name":"SharedUpdated","type":"event"},{"inputs":[],"name":"counterContract","outputs":[{"internalType":"contract Counter","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"getSharedDataFromDB","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"incrementShared","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"initializeDoc","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"sharedDocId","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`
const sharedUpdateBytecodeHex = "0x608060405234801562000010575f80fd5b5060405162000d9538038062000d95833981810160405281019062000036919062000198565b61010773ffffffffffffffffffffffffffffffffffffffff1663fbdddaf06040518060400160405280601a81526020017f626c6f636b73746d5f7368617265645f78617069616e5f65766d0000000000008152506040518263ffffffff1660e01b8152600401620000a891906200025c565b6020604051808303815f875af1158015620000c5573d5f803e3d5ffd5b505050506040513d601f19601f82011682018060405250810190620000eb9190620002b8565b508060015f6101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555050620002e8565b5f80fd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f620001628262000137565b9050919050565b620001748162000156565b81146200017f575f80fd5b50565b5f81519050620001928162000169565b92915050565b5f60208284031215620001b057620001af62000133565b5b5f620001bf8482850162000182565b91505092915050565b5f81519050919050565b5f82825260208201905092915050565b5f5b8381101562000201578082015181840152602081019050620001e4565b5f8484015250505050565b5f601f19601f8301169050919050565b5f6200022882620001c8565b620002348185620001d2565b935062000246818560208601620001e2565b62000251816200020c565b840191505092915050565b5f6020820190508181035f8301526200027681846200021c565b905092915050565b5f8115159050919050565b62000294816200027e565b81146200029f575f80fd5b50565b5f81519050620002b28162000289565b92915050565b5f60208284031215620002d057620002cf62000133565b5b5f620002df84828501620002a2565b91505092915050565b610a9f80620002f65f395ff3fe608060405234801561000f575f80fd5b5060043610610055575f3560e01c80630b6f8f481461005957806318a5f999146100775780638cc3829614610095578063b4340bbe146100b3578063d32a9a59146100bd575b5f80fd5b6100616100c7565b60405161006e9190610581565b60405180910390f35b61007f6101de565b60405161008c9190610614565b60405180910390f35b61009d610203565b6040516100aa9190610581565b60405180910390f35b6100bb610208565b005b6100c56102e0565b005b5f8061010773ffffffffffffffffffffffffffffffffffffffff16633e7c7f8b6040518060400160405280601a81526020017f626c6f636b73746d5f7368617265645f78617069616e5f65766d0000000000008152505f546040518363ffffffff1660e01b815260040161013c9291906106b7565b5f604051808303815f875af1158015610157573d5f803e3d5ffd5b505050506040513d5f823e3d601f19601f8201168201806040525081019061017f9190610814565b90505f8151116101c4576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101bb906108a5565b60405180910390fd5b808060200190518101906101d891906108ed565b91505090565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b5f5481565b61010773ffffffffffffffffffffffffffffffffffffffff16639d4799416040518060400160405280601a81526020017f626c6f636b73746d5f7368617265645f78617069616e5f65766d0000000000008152505f60405160200161026d9190610581565b6040516020818303038152906040526040518363ffffffff1660e01b815260040161029992919061096a565b6020604051808303815f875af11580156102b5573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906102d991906108ed565b5f81905550565b60015f9054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663e8927fbc6040518163ffffffff1660e01b81526004015f604051808303815f87803b158015610346575f80fd5b505af1158015610358573d5f803e3d5ffd5b505050505f61010773ffffffffffffffffffffffffffffffffffffffff16633e7c7f8b6040518060400160405280601a81526020017f626c6f636b73746d5f7368617265645f78617069616e5f65766d0000000000008152505f546040518363ffffffff1660e01b81526004016103d09291906106b7565b5f604051808303815f875af11580156103eb573d5f803e3d5ffd5b505050506040513d5f823e3d601f19601f820116820180604052508101906104139190610814565b90505f8180602001905181019061042a91906108ed565b905060018161043991906109cc565b905061010773ffffffffffffffffffffffffffffffffffffffff1663c5d187dd6040518060400160405280601a81526020017f626c6f636b73746d5f7368617265645f78617069616e5f65766d0000000000008152505f54846040516020016104a29190610581565b6040516020818303038152906040526040518463ffffffff1660e01b81526004016104cf939291906109ff565b6020604051808303815f875af11580156104eb573d5f803e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061050f91906108ed565b5f819055503373ffffffffffffffffffffffffffffffffffffffff167f3d523d852046afd5dba341b55c89c4355e15f6c45d6785c48472c0b7eb922911825f5460405161055d929190610a42565b60405180910390a25050565b5f819050919050565b61057b81610569565b82525050565b5f6020820190506105945f830184610572565b92915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f819050919050565b5f6105dc6105d76105d28461059a565b6105b9565b61059a565b9050919050565b5f6105ed826105c2565b9050919050565b5f6105fe826105e3565b9050919050565b61060e816105f4565b82525050565b5f6020820190506106275f830184610605565b92915050565b5f81519050919050565b5f82825260208201905092915050565b5f5b83811015610664578082015181840152602081019050610649565b5f8484015250505050565b5f601f19601f8301169050919050565b5f6106898261062d565b6106938185610637565b93506106a3818560208601610647565b6106ac8161066f565b840191505092915050565b5f6040820190508181035f8301526106cf818561067f565b90506106de6020830184610572565b9392505050565b5f604051905090565b5f80fd5b5f80fd5b5f80fd5b5f80fd5b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b6107348261066f565b810181811067ffffffffffffffff82111715610753576107526106fe565b5b80604052505050565b5f6107656106e5565b9050610771828261072b565b919050565b5f67ffffffffffffffff8211156107905761078f6106fe565b5b6107998261066f565b9050602081019050919050565b5f6107b86107b384610776565b61075c565b9050828152602081018484840111156107d4576107d36106fa565b5b6107df848285610647565b509392505050565b5f82601f8301126107fb576107fa6106f6565b5b815161080b8482602086016107a6565b91505092915050565b5f60208284031215610829576108286106ee565b5b5f82015167ffffffffffffffff811115610846576108456106f2565b5b610852848285016107e7565b91505092915050565b7f44617461206e6f7420666f756e6420696e2058617069616e00000000000000005f82015250565b5f61088f601883610637565b915061089a8261085b565b602082019050919050565b5f6020820190508181035f8301526108bc81610883565b9050919050565b6108cc81610569565b81146108d6575f80fd5b50565b5f815190506108e7816108c3565b92915050565b5f60208284031215610902576109016106ee565b5b5f61090f848285016108d9565b91505092915050565b5f81519050919050565b5f82825260208201905092915050565b5f61093c82610918565b6109468185610922565b9350610956818560208601610647565b61095f8161066f565b840191505092915050565b5f6040820190508181035f830152610982818561067f565b905081810360208301526109968184610932565b90509392505050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f6109d682610569565b91506109e183610569565b92508282019050808211156109f9576109f861099f565b5b92915050565b5f6060820190508181035f830152610a17818661067f565b9050610a266020830185610572565b8181036040830152610a388184610932565b9050949350505050565b5f604082019050610a555f830185610572565b610a626020830184610572565b939250505056fea2646970667358221220729002feed048c551e54303f53c2e87ba43fe5da801aa55c6789bfe11038201864736f6c63430008140033"


func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 16-xapian-evm-contract")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Gửi nhiều tx cùng lúc cập nhật chung 1 giá trị trên Xapian DB và 1 biến trên EVM.")
	fmt.Println("⚡ GỌI     : 5 tx gọi incrementShared() vào SharedUpdate, 5 tx gọi increase() vào Counter.")
	fmt.Println("🎯 KỲ VỌNG : Block-STM xử lý chuẩn, Counter = 10, Xapian = 5.")
	fmt.Println("==========================================================")

	configPath := "../config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi load config: %v", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC: %v", err)
	}

	counterABI, err := abi.JSON(strings.NewReader(counterAbiJSON))
	if err != nil {
		log.Fatalf("❌ Lỗi parse Counter ABI: %v", err)
	}
	counterBytecode, err := hexutil.Decode(counterBytecodeHex)
	if err != nil {
		log.Fatalf("❌ Lỗi decode Counter bytecode: %v", err)
	}

	sharedABI, err := abi.JSON(strings.NewReader(sharedUpdateAbiJSON))
	if err != nil {
		log.Fatalf("❌ Lỗi parse SharedUpdate ABI: %v", err)
	}
	sharedBytecode, err := hexutil.Decode(sharedUpdateBytecodeHex)
	if err != nil {
		log.Fatalf("❌ Lỗi decode SharedUpdate bytecode: %v", err)
	}

	if len(cfg.PrivateKeys) == 0 {
		log.Fatalf("❌ Không có private key nào trong config")
	}

	testKeys := cfg.PrivateKeys
	if len(testKeys) > 10 {
		testKeys = testKeys[:10]
	}
	if len(testKeys) < 10 {
		log.Fatalf("❌ Cần ít nhất 10 private keys cho bài test này, hiện có %d", len(testKeys))
	}

	pk0, err := crypto.HexToECDSA(testKeys[0])
	if err != nil {
		log.Fatalf("❌ Lỗi parse private key 0: %v", err)
	}
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying Counter contract with Account 0...")
	counterAddr, err := deployContract(client, pk0, cfg.ChainID, from0, counterBytecode)
	if err != nil {
		log.Fatalf("❌ Deploy Counter thất bại: %v", err)
	}
	fmt.Printf("📌 Counter deployed at: %s\n\n", counterAddr.Hex())

	fmt.Println("🚀 Deploying SharedUpdate contract with Account 0...")
	// pack constructor arguments (Counter address)
	constructorArgs, err := sharedABI.Pack("", *counterAddr)
	if err != nil {
		log.Fatalf("❌ Pack constructor args thất bại: %v", err)
	}
	fullSharedBytecode := append(sharedBytecode, constructorArgs...)
	
	sharedAddr, err := deployContract(client, pk0, cfg.ChainID, from0, fullSharedBytecode)
	if err != nil {
		log.Fatalf("❌ Deploy SharedUpdate thất bại: %v", err)
	}
	fmt.Printf("📌 SharedUpdate deployed at: %s\n\n", sharedAddr.Hex())

	fmt.Println("⚙️ Initializing Document...")
	initHash, err := sendInitializeDoc(client, pk0, cfg.ChainID, from0, sharedAddr, sharedABI)
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

	fmt.Printf("🔥 Gửi %d giao dịch đồng thời...\n", len(testKeys))
	start := time.Now()

	txHashes := make([]common.Hash, len(testKeys))

	for i, pkStr := range testKeys {
		wg.Add(1)
		time.Sleep(3 * time.Second)
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

			var hash common.Hash
			if idx < 5 { // 5 ví đầu gọi Xapian contract
				hash, err = sendTx(client, pk, cfg.ChainID, from, sharedAddr, sharedABI, "incrementShared")
			} else { // 5 ví sau gọi Counter contract
				hash, err = sendTx(client, pk, cfg.ChainID, from, counterAddr, counterABI, "increase")
			}

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
			fmt.Printf("✅ Wallet %d Tx %s confirmed\n", i, hash.Hex()[:10]+"...")
		}
	}

	xapianCount, err := getSharedDataFromDB(client, sharedAddr, sharedABI)
	if err != nil {
		log.Fatalf("❌ Lỗi getSharedDataFromDB(): %v", err)
	}
	
	evmCount, err := getCounterData(client, counterAddr, counterABI)
	if err != nil {
		log.Fatalf("❌ Lỗi getCounterData(): %v", err)
	}

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ:")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Giá trị counter Xapian (kỳ vọng 5): %d\n", xapianCount)
	fmt.Printf("Giá trị counter EVM (kỳ vọng 10): %d\n", evmCount)

	if xapianCount == 5 && evmCount == 10 {
		fmt.Println("🎉 TEST PASSED: BlockSTM xử lý write conflict trên Xapian DB và EVM state đúng!")
	} else {
		fmt.Printf("⚠️ TEST FAILED\n")
	}
}

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

func sendTx(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI, method string) (common.Hash, error) {
	data, err := parsedABI.Pack(method)
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
	return sendTx(client, pk, chainID, from, to, parsedABI, "initializeDoc")
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

func getCounterData(client *ethclient.Client, addr *common.Address, parsedABI abi.ABI) (uint64, error) {
	data, _ := parsedABI.Pack("count")
	result, err := client.CallContract(context.Background(), ethereum.CallMsg{To: addr, Data: data}, nil)
	if err != nil {
		return 0, err
	}
	outputs, err := parsedABI.Unpack("count", result)
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
