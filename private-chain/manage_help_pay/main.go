package main

import (
	"context"
	"encoding/json"
	"encoding/base64"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <add|remove|list> [wallet_address]")
		os.Exit(1)
	}

	action := os.Args[1]
	
	if action != "add" && action != "remove" && action != "list" {
		log.Fatalf("Invalid action. Must be 'add', 'remove', or 'list'.")
	}

	var walletAddress common.Address
	if action == "add" || action == "remove" {
		if len(os.Args) < 3 {
			log.Fatalf("Missing wallet address for action '%s'", action)
		}
		walletAddrStr := os.Args[2]
		if !common.IsHexAddress(walletAddrStr) {
			log.Fatalf("Invalid wallet address: %s", walletAddrStr)
		}
		walletAddress = common.HexToAddress(walletAddrStr)
	}

	// Đọc config.json
	configBytes, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Failed to read config.json: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(configBytes, &cfg); err != nil {
		log.Fatalf("Failed to parse config.json: %v", err)
	}

	httpRpcUrl := cfg["http_rpc"].(string)
	if !strings.HasPrefix(httpRpcUrl, "http") {
		httpRpcUrl = "http://" + httpRpcUrl
	}

	// Kết nối rpc client
	rpcClient, err := rpc.Dial(httpRpcUrl)
	if err != nil {
		log.Fatalf("Failed to connect rpc: %v", err)
	}

	// Lấy admin private key
	adminPrivKeyHex := cfg["eth_private_key"].(string)
	adminPrivKey, err := crypto.HexToECDSA(adminPrivKeyHex)
	if err != nil {
		log.Fatalf("Invalid eth_private_key in config.json: %v", err)
	}

	chainID := big.NewInt(int64(cfg["chain_id"].(float64)))
	contractAddress := common.HexToAddress(cfg["contact_private"].(string))

	// Đọc ABI file
	abiBytes, err := os.ReadFile("../accountAbi.json")
	if err != nil {
		log.Fatalf("Failed to read ../accountAbi.json: %v", err)
	}
	parsedABI, err := abi.JSON(strings.NewReader(string(abiBytes)))
	if err != nil {
		log.Fatalf("Failed to parse contract ABI: %v", err)
	}

	if action == "list" {
		fmt.Printf(">> Calling getAllHelpPayWallets...\n")
		// Call getAllHelpPayWallets (page: 0, pageSize: 1000)
		callData, err := parsedABI.Pack("getAllHelpPayWallets", big.NewInt(0), big.NewInt(1000))
		if err != nil {
			log.Fatalf("Failed to pack getAllHelpPayWallets: %v", err)
		}
		
		msg := map[string]interface{}{
			"to":   contractAddress.Hex(),
			"data": hexutil.Encode(callData),
		}
		
		var result interface{}
		err = rpcClient.CallContext(context.Background(), &result, "eth_call", msg, "latest")
		if err != nil {
			log.Fatalf("❌ Get list failed: %v", err)
		}
		
		// Decode hex result to string if it's hex
		if hexStr, ok := result.(string); ok && strings.HasPrefix(hexStr, "0x") {
			decodedBytes, err := hexutil.Decode(hexStr)
			if err == nil {
				// Parse decoded JSON again to pretty-print
				var jsonObj interface{}
				if err := json.Unmarshal(decodedBytes, &jsonObj); err == nil {
					jsonMap := jsonObj.(map[string]interface{})
					total := jsonMap["total"]
					if total == nil {
						total = 0
					}
					
					// Format base64 fields to Hex address strings
					if wallets, ok := jsonMap["wallets"].([]interface{}); ok {
						for _, w := range wallets {
							if walletMap, ok := w.(map[string]interface{}); ok {
								if b64, ok := walletMap["wallet_address"].(string); ok {
									if decoded, err := base64.StdEncoding.DecodeString(b64); err == nil {
										walletMap["wallet_address"] = common.BytesToAddress(decoded).Hex()
									}
								}
								if b64, ok := walletMap["added_by"].(string); ok {
									if decoded, err := base64.StdEncoding.DecodeString(b64); err == nil {
										walletMap["added_by"] = common.BytesToAddress(decoded).Hex()
									}
								}
							}
						}
					}
					
					jsonBytes, _ := json.MarshalIndent(jsonObj, "", "  ")
					fmt.Printf("✅ Help Pay Wallets (Total: %v):\n%s\n", total, string(jsonBytes))
					return
				}
				fmt.Printf("✅ Help Pay Wallets (Raw String):\n%s\n", string(decodedBytes))
				return
			}
		}

		jsonBytes, _ := json.MarshalIndent(result, "", "  ")
		fmt.Printf("✅ Help Pay Wallets:\n%s\n", string(jsonBytes))
		return
	}

	var methodName string
	if action == "add" {
		methodName = "addHelpPayWallet"
	} else {
		methodName = "removeHelpPayWallet"
	}

	fmt.Printf(">> Calling %s for wallet %s...\n", methodName, walletAddress.Hex())
	
	setData, err := parsedABI.Pack(methodName, walletAddress)
	if err != nil {
		log.Fatalf("Failed to pack %s: %v", methodName, err)
	}

	// Tạo tx
	tx := types.NewTransaction(0, contractAddress, big.NewInt(0), 100000, big.NewInt(1000000000), setData)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), adminPrivKey)
	if err != nil {
		log.Fatalf("Failed to sign tx: %v", err)
	}

	txData, _ := signedTx.MarshalBinary()

	var result interface{}
	err = rpcClient.CallContext(context.Background(), &result, "eth_sendRawTransaction", hexutil.Encode(txData))
	if err != nil {
		log.Fatalf("❌ Transaction failed: %v", err)
	} else {
		fmt.Printf("✅ Transaction succeeded! Result (Tx Hash): %v\n", result)
	}
}
