package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

func main() {
	client, err := ethclient.Dial("http://localhost:8545")
	if err != nil {
		log.Fatalf("Failed to connect to the Ethereum client: %v", err)
	}
	defer client.Close()

	// Đọc ABI file cục bộ để dự án hoạt động độc lập
	abiBytes, err := os.ReadFile("accountAbi.json")
	if err != nil {
		log.Fatalf("Failed to read ABI file: %v", err)
	}
	abiJson := string(abiBytes)

	parsedABI, err := abi.JSON(strings.NewReader(abiJson))
	if err != nil {
		log.Fatalf("Failed to parse contract ABI: %v", err)
	}

	contractAddress := common.HexToAddress("0x00000000000000000000000000000000D844bb55")

	// 2. Test Get Config
	fmt.Println("--- Testing getConfig ---")
	callData, err := parsedABI.Pack("getConfig")
	if err != nil {
		log.Fatalf("Failed to pack getConfig data: %v", err)
	}

	msg := ethereum.CallMsg{
		To:   &contractAddress,
		Data: callData,
	}

	res, err := client.CallContract(context.Background(), msg, nil)
	if err != nil {
		log.Printf("CallContract failed: %v", err)
	} else {
		var out []interface{}
		out, err = parsedABI.Unpack("getConfig", res)
		if err != nil {
			log.Printf("Failed to unpack getConfig response: %v", err)
		} else {
			fmt.Printf("Extra Account: %v\n", out[0].(*big.Int))
			fmt.Printf("Free Gas Min Balance: %v\n", out[1].(*big.Int))
			fmt.Printf("Reward Amount: %v\n", out[2].(*big.Int))
			fmt.Printf("Disable Free Gas: %v\n", out[3].(bool))
		}
	}

	// 3. Test Set Functions
	fmt.Println("\n--- Testing Setters ---")

	// Khởi tạo rpc client trực tiếp để bắt response result tuỳ chỉnh
	rpcClient, err := rpc.Dial("http://localhost:8545")
	if err != nil {
		log.Fatalf("Failed to connect rpc: %v", err)
	}
	// Đọc config.json để lấy private key thay vì hardcode
	configBytes, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Failed to read config.json: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(configBytes, &cfg); err != nil {
		log.Fatalf("Failed to parse config.json: %v", err)
	}
	ownerPrivKeyHex := cfg["eth_private_key"].(string)

	ownerPrivKey, err := crypto.HexToECDSA(ownerPrivKeyHex)

	if err == nil {
		chainID := big.NewInt(991) // ChainID từ cấu hình

		// Hàm helper gọi và in kết quả
		testSetFunc := func(name string, args ...interface{}) {
			fmt.Printf(">> Calling %s...\n", name)
			setData, err := parsedABI.Pack(name, args...)
			if err != nil {
				log.Printf("Failed to pack %s: %v", name, err)
				return
			}

			// Tạo tx dummy, chỉ quan tâm To và Data (vì ta intercept, nhưng vẫn cần đủ cấu trúc để ethclient ko báo lỗi định dạng)
			tx := types.NewTransaction(0, contractAddress, big.NewInt(0), 100000, big.NewInt(1000000000), setData)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), ownerPrivKey)
			if err != nil {
				log.Printf("Failed to sign %s: %v", name, err)
				return
			}

			txData, _ := signedTx.MarshalBinary()

			var result interface{}
			err = rpcClient.CallContext(context.Background(), &result, "eth_sendRawTransaction", hexutil.Encode(txData))
			if err != nil {
				log.Printf("❌ %s failed: %v\n", name, err)
			} else {
				log.Printf("✅ %s succeeded! Returned value: %v\n", name, result)
			}
		}

		extraAccVal := big.NewInt(10000000000000000)
		testSetFunc("setExtraAccount", extraAccVal)

		freeGasVal := big.NewInt(20000000000000000)
		testSetFunc("setFreeGasMinBalance", freeGasVal)

		rewardVal := big.NewInt(0)
		testSetFunc("setRewardAmount", rewardVal)

		testSetFunc("setDisableFreeGas", false)
	} else {
		fmt.Println("⚠️ Vui lòng cập nhật biến ownerPrivKeyHex bằng private key hợp lệ để test send transaction.")
	}
}
