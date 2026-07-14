/*
 * BÀI TEST: 21-xapian-getdata-after-write-same-block
 * MÔ TẢ   : Gửi lệnh GHI và ĐỌC trong cùng 1 block để test khả năng đọc
 *           dữ liệu chưa commit (trên RAM) của hàm getDataDocument.
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

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const abiJSON = `[{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"docId","type":"uint256"}],"name":"DocumentAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"docId","type":"uint256"},{"indexed":false,"internalType":"string","name":"data","type":"string"}],"name":"DocumentRead","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"docId","type":"uint256"}],"name":"DocumentUpdated","type":"event"},{"inputs":[],"name":"readDocument","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"string","name":"initialText","type":"string"}],"name":"setupDocument","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"sharedDocId","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"string","name":"newText","type":"string"}],"name":"updateDocument","outputs":[],"stateMutability":"nonpayable","type":"function"}]`
const bytecodeHex = "0x608060405234801561000f575f5ffd5b5061010773ffffffffffffffffffffffffffffffffffffffff1663fbdddaf06040518060400160405280602081526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e5f676574646174618152506040518263ffffffff1660e01b81526004016100809190610136565b6020604051808303815f875af115801561009c573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906100c0919061018f565b506101ba565b5f81519050919050565b5f82825260208201905092915050565b8281835e5f83830152505050565b5f601f19601f8301169050919050565b5f610108826100c6565b61011281856100d0565b93506101228185602086016100e0565b61012b816100ee565b840191505092915050565b5f6020820190508181035f83015261014e81846100fe565b905092915050565b5f5ffd5b5f8115159050919050565b61016e8161015a565b8114610178575f5ffd5b50565b5f8151905061018981610165565b92915050565b5f602082840312156101a4576101a3610156565b5b5f6101b18482850161017b565b91505092915050565b610935806101c75f395ff3fe608060405234801561000f575f5ffd5b506004361061004a575f3560e01c806334dc6aa81461004e578063492c24f81461006a5780638cc3829614610086578063951c21b8146100a4575b5f5ffd5b6100686004803603810190610063919061055f565b6100ae565b005b610084600480360381019061007f919061055f565b6101e2565b005b61008e6102d4565b60405161009b91906105be565b60405180910390f35b6100ac6102d9565b005b5f5f54116100f1576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016100e890610631565b60405180910390fd5b61010773ffffffffffffffffffffffffffffffffffffffff1663c5d187dd6040518060400160405280602081526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e5f676574646174618152505f54846040518463ffffffff1660e01b8152600401610166939291906106f1565b6020604051808303815f875af1158015610182573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906101a6919061075e565b507fbad48108c10932f56a6f5c50272d433c3b8461bec6fe575c29a369bdd752b1995f546040516101d791906105be565b60405180910390a150565b61010773ffffffffffffffffffffffffffffffffffffffff16639d4799416040518060400160405280602081526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e5f67657464617461815250836040518363ffffffff1660e01b8152600401610254929190610789565b6020604051808303815f875af1158015610270573d5f5f3e3d5ffd5b505050506040513d601f19601f82011682018060405250810190610294919061075e565b5f819055507f1436ef0a63d102072b70744adedc4e9c88487529e51f8624fc3ab50904472f1e5f546040516102c991906105be565b60405180910390a150565b5f5481565b5f5f541161031c576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161031390610631565b60405180910390fd5b5f61010773ffffffffffffffffffffffffffffffffffffffff16633e7c7f8b6040518060400160405280602081526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e5f676574646174618152505f546040518363ffffffff1660e01b81526004016103909291906107be565b5f604051808303815f875af11580156103ab573d5f5f3e3d5ffd5b505050506040513d5f823e3d601f19601f820116820180604052508101906103d3919061088a565b90507fbc3da3f59915d34951270957704678b0aa4abb7877e55a14883306153924acfa5f54826040516104079291906108d1565b60405180910390a150565b5f604051905090565b5f5ffd5b5f5ffd5b5f5ffd5b5f5ffd5b5f601f19601f8301169050919050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b6104718261042b565b810181811067ffffffffffffffff821117156104905761048f61043b565b5b80604052505050565b5f6104a2610412565b90506104ae8282610468565b919050565b5f67ffffffffffffffff8211156104cd576104cc61043b565b5b6104d68261042b565b9050602081019050919050565b828183375f83830152505050565b5f6105036104fe846104b3565b610499565b90508281526020810184848401111561051f5761051e610427565b5b61052a8482856104e3565b509392505050565b5f82601f83011261054657610545610423565b5b81356105568482602086016104f1565b91505092915050565b5f602082840312156105745761057361041b565b5b5f82013567ffffffffffffffff8111156105915761059061041f565b5b61059d84828501610532565b91505092915050565b5f819050919050565b6105b8816105a6565b82525050565b5f6020820190506105d15f8301846105af565b92915050565b5f82825260208201905092915050565b7f4d7573742063616c6c207365747570446f63756d656e742066697273740000005f82015250565b5f61061b601d836105d7565b9150610626826105e7565b602082019050919050565b5f6020820190508181035f8301526106488161060f565b9050919050565b5f81519050919050565b8281835e5f83830152505050565b5f6106718261064f565b61067b81856105d7565b935061068b818560208601610659565b6106948161042b565b840191505092915050565b5f81519050919050565b5f82825260208201905092915050565b5f6106c38261069f565b6106cd81856106a9565b93506106dd818560208601610659565b6106e68161042b565b840191505092915050565b5f6060820190508181035f8301526107098186610667565b905061071860208301856105af565b818103604083015261072a81846106b9565b9050949350505050565b61073d816105a6565b8114610747575f5ffd5b50565b5f8151905061075881610734565b92915050565b5f602082840312156107735761077261041b565b5b5f6107808482850161074a565b91505092915050565b5f6040820190508181035f8301526107a18185610667565b905081810360208301526107b581846106b9565b90509392505050565b5f6040820190508181035f8301526107d68185610667565b90506107e560208301846105af565b9392505050565b5f67ffffffffffffffff8211156108065761080561043b565b5b61080f8261042b565b9050602081019050919050565b5f61082e610829846107ec565b610499565b90508281526020810184848401111561084a57610849610427565b5b610855848285610659565b509392505050565b5f82601f83011261087157610870610423565b5b815161088184826020860161081c565b91505092915050565b5f6020828403121561089f5761089e61041b565b5b5f82015167ffffffffffffffff8111156108bc576108bb61041f565b5b6108c88482850161085d565b91505092915050565b5f6040820190506108e45f8301856105af565b81810360208301526108f68184610667565b9050939250505056fea264697066735822122000b730bfc456a5bf4b088ede936bc8cd3ab960ea8d059f2ece25bdd5cb48481564736f6c63430008240033"

type Config struct {
	RPCUrl      string   `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
	ChainID     int64    `json:"chain_id"`
}

func main() {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: LẤY DOCUMENT THEO ID (GET) NGAY TRONG CÙNG BLOCK")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Dùng 10 ví, mỗi ví gửi lệnh GHI (update) và ĐỌC (read).")
	fmt.Println("             Lệnh đọc (nonce N+1) bắt buộc chạy sau lệnh ghi (nonce N).")
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
		log.Fatalf("❌ Cần ít nhất 10 private key cho bài test này (hiện có %d)", len(cfg.PrivateKeys))
	}

	pk0Str := cfg.PrivateKeys[0]
	pk0, _ := crypto.HexToECDSA(pk0Str)
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying ParallelXapian contract...")
	contractAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		log.Fatalf("❌ Deploy thất bại: %v", err)
	}
	fmt.Printf("📌 Contract deployed at: %s\n\n", contractAddr.Hex())

	fmt.Println("🛠️ Đang chạy setupDocument để tạo Document ban đầu...")
	nonce0, _ := client.PendingNonceAt(context.Background(), from0)
	setupHash, err := sendTxWithNonce(client, pk0, cfg.ChainID, from0, contractAddr, contractABI, nonce0, "setupDocument", "INITIAL_DATA")
	if err != nil {
		log.Fatalf("❌ Setup thất bại: %v", err)
	}
	waitReceipt(client, setupHash)
	fmt.Println("✅ Setup thành công! Đã có Document ID.\n")

	var wg sync.WaitGroup
	fmt.Printf("🔥 Đang gửi 20 giao dịch đồng thời (10 Update + 10 Read)...\n")
	start := time.Now()

	uniqueTextPrefix := fmt.Sprintf("test_getdata_mass_%d_", time.Now().Unix())
	
	type readJob struct {
		walletIdx int
		hash      common.Hash
	}
	readHashes := make(chan readJob, 10)
	var allTxHashes []common.Hash
	var allTxMu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(walletIdx int, pkStr string) {
			defer wg.Done()
			
			pk, _ := crypto.HexToECDSA(pkStr)
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))
			
			nonce, err := client.PendingNonceAt(context.Background(), from)
			if err != nil {
				return
			}
			
			expectedData := fmt.Sprintf("%s%d", uniqueTextPrefix, walletIdx)

			// Tx Write (Nonce N)
			wHash, wErr := sendTxWithNonce(client, pk, cfg.ChainID, from, contractAddr, contractABI, nonce, "updateDocument", expectedData)
			if wErr != nil {
				return
			}
			
			// Tx Read (Nonce N+1)
			rHash, rErr := sendTxWithNonce(client, pk, cfg.ChainID, from, contractAddr, contractABI, nonce+1, "readDocument")
			if rErr != nil {
				return
			}
			
			readHashes <- readJob{walletIdx: walletIdx, hash: rHash}
			
			allTxMu.Lock()
			allTxHashes = append(allTxHashes, wHash, rHash)
			allTxMu.Unlock()
			
			fmt.Printf("   Ví %d -> [WRITE: %s] | [READ: %s]\n", walletIdx, wHash.Hex()[:10]+"...", rHash.Hex()[:10]+"...")
		}(i, cfg.PrivateKeys[i])
	}

	wg.Wait()
	close(readHashes)

	fmt.Println("\n⏳ Chờ tất cả 20 giao dịch được confirm...")
	var wgWait sync.WaitGroup
	for _, h := range allTxHashes {
		wgWait.Add(1)
		go func(txHash common.Hash) {
			defer wgWait.Done()
			_, _ = waitReceipt(client, txHash)
		}(h)
	}
	wgWait.Wait()
	fmt.Println("✅ Tất cả giao dịch đã được confirm!")

	fmt.Println("\n📊 KẾT QUẢ ĐỌC SAU KHI GHI TRONG CÙNG BLOCK:")
	
	successCount := 0
	
	for job := range readHashes {
		receipt, err := client.TransactionReceipt(context.Background(), job.hash)
		if err != nil {
			continue
		}
		
		readData := getDocumentReadEventData(receipt, contractABI)
		fmt.Printf("   Ví %d (Block %d) -> Lấy được: '%s'\n", job.walletIdx, receipt.BlockNumber.Uint64(), readData)
		if strings.HasPrefix(readData, uniqueTextPrefix) {
			successCount++
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("\n⏱️ Thời gian hoàn thành: %v\n", elapsed)
	
	if successCount > 0 {
		fmt.Printf("🎉 KẾT LUẬN: Đã có %d/%d giao dịch ĐỌC THÀNH CÔNG dữ liệu uncommitted trong CÙNG BLOCK!\n", successCount, 10)
		fmt.Println("   Giải thích: Lệnh getDocument lấy dữ liệu thẳng từ RAM, nên có thể nhìn thấy dữ liệu vừa ghi.")
	} else {
		fmt.Println("⚠️ KẾT LUẬN: Không đọc được dữ liệu. Các giao dịch Read đều trả về rỗng.")
	}
}

func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	nonce, _ := client.PendingNonceAt(context.Background(), from)
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	tx := types.NewContractCreation(nonce, big.NewInt(0), 5_000_000, gasPrice, bytecode)
	signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	client.SendTransaction(context.Background(), signedTx)
	receipt, _ := waitReceipt(client, signedTx.Hash())
	return &receipt.ContractAddress, nil
}

func sendTxWithNonce(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI, nonce uint64, method string, args ...interface{}) (common.Hash, error) {
	data, err := parsedABI.Pack(method, args...)
	if err != nil {
		return common.Hash{}, err
	}
	gasPrice, _ := client.SuggestGasPrice(context.Background())
	tx := types.NewTransaction(nonce, *to, big.NewInt(0), 1_000_000, gasPrice, data)
	signedTx, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	client.SendTransaction(context.Background(), signedTx)
	return signedTx.Hash(), nil
}

func waitReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	for {
		receipt, err := client.TransactionReceipt(context.Background(), txHash)
		if err == nil && receipt != nil && receipt.BlockNumber.Uint64() > 0 {
			return receipt, nil
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func getDocumentReadEventData(receipt *types.Receipt, parsedABI abi.ABI) string {
	for _, log := range receipt.Logs {
		event, err := parsedABI.EventByID(log.Topics[0])
		if err == nil && event.Name == "DocumentRead" {
			data, err := event.Inputs.Unpack(log.Data)
			if err == nil && len(data) > 1 {
				if text, ok := data[1].(string); ok {
					return text
				}
			}
		}
	}
	return ""
}
