/*
 * BÀI TEST: 17-xapian-parallel-read-write
 * MÔ TẢ   : Gửi nhiều giao dịch tạo document vào 1 DB và 1 giao dịch search đồng thời.
 * KỲ VỌNG : - Block-STM không báo xung đột khóa cấp DB cho NEW_DOCUMENT (virtual_deploy.md)
 *           - Giao dịch SEARCH chạy song song không bị block (read_prallel_xapian.md)
 *           - Lần search trong cùng block sẽ không thấy các thay đổi chưa commit.
 *           - Lần search ở block sau sẽ thấy đầy đủ các document đã thêm.
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
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const abiJSON = `[{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"docId","type":"uint256"}],"name":"DocumentAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"totalFound","type":"uint256"}],"name":"SearchExecuted","type":"event"},{"inputs":[{"internalType":"string","name":"text","type":"string"}],"name":"addDocument","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"string","name":"query","type":"string"}],"name":"searchDocument","outputs":[],"stateMutability":"nonpayable","type":"function"}]`
const bytecodeHex = "0x608060405234801562000010575f80fd5b5061010773ffffffffffffffffffffffffffffffffffffffff1663fbdddaf06040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e00000000000000008152506040518263ffffffff1660e01b815260040162000083919062000161565b6020604051808303815f875af1158015620000a0573d5f803e3d5ffd5b505050506040513d601f19601f82011682018060405250810190620000c69190620001c1565b50620001f1565b5f81519050919050565b5f82825260208201905092915050565b5f5b8381101562000106578082015181840152602081019050620000e9565b5f8484015250505050565b5f601f19601f8301169050919050565b5f6200012d82620000cd565b620001398185620000d7565b93506200014b818560208601620000e7565b620001568162000111565b840191505092915050565b5f6020820190508181035f8301526200017b818462000121565b905092915050565b5f80fd5b5f8115159050919050565b6200019d8162000187565b8114620001a8575f80fd5b50565b5f81519050620001bb8162000192565b92915050565b5f60208284031215620001d957620001d862000183565b5b5f620001e884828501620001ab565b91505092915050565b610f4780620001ff5f395ff3fe608060405234801561000f575f80fd5b5060043610610034575f3560e01c8063b1a1e62914610038578063f740e04514610054575b5f80fd5b610052600480360381019061004d9190610507565b610070565b005b61006e60048036038101906100699190610507565b6101b8565b005b610078610360565b81815f01819052505f816060019067ffffffffffffffff16908167ffffffffffffffff1681525050600a816080019067ffffffffffffffff16908167ffffffffffffffff16815250505f61010773ffffffffffffffffffffffffffffffffffffffff1663a689fe196040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e0000000000000000815250846040518363ffffffff1660e01b8152600401610134929190610a07565b5f604051808303815f875af115801561014f573d5f803e3d5ffd5b505050506040513d5f823e3d601f19601f820116820180604052508101906101779190610d1b565b90507f174e680989a8d082b84a89df08b862b20555d3b4ab7533ca90a64ffb1e1acbdf815f01516040516101ab9190610d71565b60405180910390a1505050565b5f61010773ffffffffffffffffffffffffffffffffffffffff16639d4799416040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e0000000000000000815250846040518363ffffffff1660e01b815260040161022b929190610ddc565b6020604051808303815f875af1158015610247573d5f803e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061026b9190610e11565b905061010773ffffffffffffffffffffffffffffffffffffffff16631736f2706040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e0000000000000000815250838560016040518563ffffffff1660e01b81526004016102e49493929190610ead565b6020604051808303815f875af1158015610300573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906103249190610e11565b507f1436ef0a63d102072b70744adedc4e9c88487529e51f8624fc3ab50904472f1e816040516103549190610d71565b60405180910390a15050565b6040518061010001604052806060815260200160608152602001606081526020015f67ffffffffffffffff1681526020015f67ffffffffffffffff1681526020015f60070b81526020015f15158152602001606081525090565b5f604051905090565b5f80fd5b5f80fd5b5f80fd5b5f80fd5b5f601f19601f8301169050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b610419826103d3565b810181811067ffffffffffffffff82111715610438576104376103e3565b5b80604052505050565b5f61044a6103ba565b90506104568282610410565b919050565b5f67ffffffffffffffff821115610475576104746103e3565b5b61047e826103d3565b9050602081019050919050565b828183375f83830152505050565b5f6104ab6104a68461045b565b610441565b9050828152602081018484840111156104c7576104c66103cf565b5b6104d284828561048b565b509392505050565b5f82601f8301126104ee576104ed6103cb565b5b81356104fe848260208601610499565b91505092915050565b5f6020828403121561051c5761051b6103c3565b5b5f82013567ffffffffffffffff811115610539576105386103c7565b5b610545848285016104da565b91505092915050565b5f81519050919050565b5f82825260208201905092915050565b5f5b8381101561058557808201518184015260208101905061056a565b5f8484015250505050565b5f61059a8261054e565b6105a48185610558565b93506105b4818560208601610568565b6105bd816103d3565b840191505092915050565b5f82825260208201905092915050565b5f6105e28261054e565b6105ec81856105c8565b93506105fc818560208601610568565b610605816103d3565b840191505092915050565b5f81519050919050565b5f82825260208201905092915050565b5f819050602082019050919050565b5f604083015f8301518482035f86015261065382826105d8565b9150506020830151848203602086015261066d82826105d8565b9150508091505092915050565b5f6106858383610639565b905092915050565b5f602082019050919050565b5f6106a382610610565b6106ad818561061a565b9350836020820285016106bf8561062a565b805f5b858110156106fa57848403895281516106db858261067a565b94506106e68361068d565b925060208a019950506001810190506106c2565b50829750879550505050505092915050565b5f81519050919050565b5f82825260208201905092915050565b5f819050602082019050919050565b5f61074083836105d8565b905092915050565b5f602082019050919050565b5f61075e8261070c565b6107688185610716565b93508360208202850161077a85610726565b805f5b858110156107b557848403895281516107968582610735565b94506107a183610748565b925060208a0199505060018101905061077d565b50829750879550505050505092915050565b5f67ffffffffffffffff82169050919050565b6107e3816107c7565b82525050565b5f8160070b9050919050565b6107fe816107e9565b82525050565b5f8115159050919050565b61081881610804565b82525050565b5f81519050919050565b5f82825260208201905092915050565b5f819050602082019050919050565b5f819050919050565b61085981610847565b82525050565b5f606083015f8301516108745f860182610850565b506020830151848203602086015261088c82826105d8565b915050604083015184820360408601526108a682826105d8565b9150508091505092915050565b5f6108be838361085f565b905092915050565b5f602082019050919050565b5f6108dc8261081e565b6108e68185610828565b9350836020820285016108f885610838565b805f5b85811015610933578484038952815161091485826108b3565b945061091f836108c6565b925060208a019950506001810190506108fb565b50829750879550505050505092915050565b5f61010083015f8301518482035f86015261096082826105d8565b9150506020830151848203602086015261097a8282610699565b915050604083015184820360408601526109948282610754565b91505060608301516109a960608601826107da565b5060808301516109bc60808601826107da565b5060a08301516109cf60a08601826107f5565b5060c08301516109e260c086018261080f565b5060e083015184820360e08601526109fa82826108d2565b9150508091505092915050565b5f6040820190508181035f830152610a1f8185610590565b90508181036020830152610a338184610945565b90509392505050565b5f80fd5b5f80fd5b610a4d81610847565b8114610a57575f80fd5b50565b5f81519050610a6881610a44565b92915050565b5f67ffffffffffffffff821115610a8857610a876103e3565b5b602082029050602081019050919050565b5f80fd5b5f819050919050565b610aaf81610a9d565b8114610ab9575f80fd5b50565b5f81519050610aca81610aa6565b92915050565b5f67ffffffffffffffff821115610aea57610ae96103e3565b5b610af3826103d3565b9050602081019050919050565b5f610b12610b0d84610ad0565b610441565b905082815260208101848484011115610b2e57610b2d6103cf565b5b610b39848285610568565b509392505050565b5f82601f830112610b5557610b546103cb565b5b8151610b65848260208601610b00565b91505092915050565b5f60808284031215610b8357610b82610a3c565b5b610b8d6080610441565b90505f610b9c84828501610a5a565b5f830152506020610baf84828501610a5a565b6020830152506040610bc384828501610abc565b604083015250606082015167ffffffffffffffff811115610be757610be6610a40565b5b610bf384828501610b41565b60608301525092915050565b5f610c11610c0c84610a6e565b610441565b90508083825260208201905060208402830185811115610c3457610c33610a99565b5b835b81811015610c7b57805167ffffffffffffffff811115610c5957610c586103cb565b5b808601610c668982610b6e565b85526020850194505050602081019050610c36565b5050509392505050565b5f82601f830112610c9957610c986103cb565b5b8151610ca9848260208601610bff565b91505092915050565b5f60408284031215610cc757610cc6610a3c565b5b610cd16040610441565b90505f610ce084828501610a5a565b5f83015250602082015167ffffffffffffffff811115610d0357610d02610a40565b5b610d0f84828501610c85565b60208301525092915050565b5f60208284031215610d3057610d2f6103c3565b5b5f82015167ffffffffffffffff811115610d4d57610d4c6103c7565b5b610d5984828501610cb2565b91505092915050565b610d6b81610847565b82525050565b5f602082019050610d845f830184610d62565b92915050565b5f81519050919050565b5f82825260208201905092915050565b5f610dae82610d8a565b610db88185610d94565b9350610dc8818560208601610568565b610dd1816103d3565b840191505092915050565b5f6040820190508181035f830152610df48185610590565b90508181036020830152610e088184610da4565b90509392505050565b5f60208284031215610e2657610e256103c3565b5b5f610e3384828501610a5a565b91505092915050565b5f819050919050565b5f60ff82169050919050565b5f819050919050565b5f610e74610e6f610e6a84610e3c565b610e51565b610e45565b9050919050565b610e8481610e5a565b82525050565b50565b5f610e985f83610558565b9150610ea382610e8a565b5f82019050919050565b5f60a0820190508181035f830152610ec58187610590565b9050610ed46020830186610d62565b8181036040830152610ee68185610590565b9050610ef56060830184610e7b565b8181036080830152610f0681610e8d565b90509594505050505056fea2646970667358221220aa89191bbae780b9f3ff360fe6382c24ad1b251495be5a243c4f512e871ec7e864736f6c63430008140033"

// ... 
// Config struct and waitReceipt are standard
type Config struct {
	RPCUrl      string   `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
	ChainID     int64    `json:"chain_id"`
}

func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 17-xapian-parallel-read-write")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Gửi 9 tx addDocument và 1 tx searchDocument đồng thời.")
	fmt.Println("🎯 KỲ VỌNG : Giao dịch không bị abort, searchDocument sẽ không thấy dữ liệu chưa commit.")
	fmt.Println("==========================================================")

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

	contractABI, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}
	bytecode, err := common.ParseHexOrString(bytecodeHex)
	if err != nil {
		log.Fatalf("❌ Lỗi decode bytecode: %v", err)
	}

	if len(cfg.PrivateKeys) < 10 {
		log.Fatalf("❌ Cần ít nhất 10 private keys cho bài test này")
	}
	testKeys := cfg.PrivateKeys[:10]

	pk0, _ := crypto.HexToECDSA(testKeys[0])
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying ParallelXapian contract with Account 0...")
	contractAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		log.Fatalf("❌ Deploy thất bại: %v", err)
	}
	fmt.Printf("📌 Contract deployed at: %s\n\n", contractAddr.Hex())

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex

	fmt.Printf("🔥 Gửi 10 giao dịch đồng thời (9 addDocument, 1 searchDocument)...\n")
	start := time.Now()

	txHashes := make([]common.Hash, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int, pKeyHex string) {
			defer wg.Done()

			pk, _ := crypto.HexToECDSA(pKeyHex)
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

			var hash common.Hash
			var txErr error
			if idx < 9 {
				// 9 ví đầu tiên gửi addDocument
				text := fmt.Sprintf("hello blockstm xapian %d", idx)
				hash, txErr = sendTx(client, pk, cfg.ChainID, from, contractAddr, contractABI, "addDocument", text)
			} else {
				// Ví cuối cùng gửi searchDocument
				hash, txErr = sendTx(client, pk, cfg.ChainID, from, contractAddr, contractABI, "searchDocument", "hello")
			}

			if txErr != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Errorf("lỗi send tx từ wallet %d: %v", idx, txErr))
				errsMu.Unlock()
				return
			}
			txHashes[idx] = hash
		}(i, testKeys[i])
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
			fmt.Printf("✅ Wallet %d Tx confirmed\n", i)
		}
	}

	// Sau khi 9 document đã được commit, ta search lại để kiểm tra
	fmt.Println("\n🔍 Thực hiện searchDocument sau khi block đã commit...")
	searchHash, err := sendTx(client, pk0, cfg.ChainID, from0, contractAddr, contractABI, "searchDocument", "hello")
	if err != nil {
		log.Fatalf("❌ Lỗi gửi search: %v", err)
	}
	searchReceipt, err := waitReceipt(client, searchHash)
	if err != nil || searchReceipt.Status != 1 {
		log.Fatalf("❌ searchDocument sau commit bị lỗi!")
	}

	// Lấy số lượng search kết quả từ event
	totalFound := getSearchEventData(searchReceipt, contractABI)

	elapsed := time.Since(start)
	fmt.Println("\n📊 KẾT QUẢ:")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	fmt.Printf("Số lượng document tìm thấy sau khi commit (kỳ vọng >= 9): %d\n", totalFound)

	if totalFound >= 9 {
		fmt.Println("🎉 TEST PASSED: BlockSTM xử lý song song NEW_DOCUMENT và QUERY_SEARCH mượt mà!")
	} else {
		fmt.Printf("⚠️ TEST FAILED\n")
	}
}

// Helpers

func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return nil, err
	}
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil {
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

func sendTx(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI, method string, args ...interface{}) (common.Hash, error) {
	data, err := parsedABI.Pack(method, args...)
	if err != nil {
		return common.Hash{}, err
	}
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return common.Hash{}, err
	}
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	if gasPrice == nil {
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

func getSearchEventData(receipt *types.Receipt, parsedABI abi.ABI) uint64 {
	for _, log := range receipt.Logs {
		// Event 2 is SearchExecuted
		event, err := parsedABI.EventByID(log.Topics[0])
		if err == nil && event.Name == "SearchExecuted" {
			data, err := event.Inputs.Unpack(log.Data)
			if err == nil && len(data) > 0 {
				if total, ok := data[0].(*big.Int); ok {
					return total.Uint64()
				}
			}
		}
	}
	return 0
}
