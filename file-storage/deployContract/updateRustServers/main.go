package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

type Config struct {
	RPCUrl             string
	DeployerPrivateKey string
	ProxyAddress       string
	RustStorageIPs     []string
}

func main() {
	// Load environment variables from parent directory
	err := godotenv.Load("../.env")
	if err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}

	config := &Config{
		RPCUrl:             getEnv("RPC_URL", "http://192.168.1.234:8747"),
		DeployerPrivateKey: getEnv("PRIVATE_KEY", ""),
		ProxyAddress:       getEnv("PROXY_ADDRESS", "0x087cdab97d38a3bfFcDee170739E8C11Af651569"),
		RustStorageIPs:     parseCommaSeparatedString(getEnv("IP_RUST_STORAGE", "192.168.1.230:7081, 192.168.1.230:7082")),
	}

	if config.DeployerPrivateKey == "" {
		log.Fatal("PRIVATE_KEY is required in .env file")
	}
	if config.ProxyAddress == "" {
		log.Fatal("PROXY_ADDRESS is required in .env file")
	}
	if len(config.RustStorageIPs) == 0 {
		log.Fatal("IP_RUST_STORAGE is empty")
	}

	var rpcClient *rpc.Client
	ctx := context.Background()
	parsedURL, err := url.Parse(config.RPCUrl)
	if err != nil {
		log.Fatalf("Failed to parse RPC URL: %v", err)
	}

	switch parsedURL.Scheme {
	case "https":
		insecureTLSConfig := &tls.Config{InsecureSkipVerify: true}
		transport := &http.Transport{TLSClientConfig: insecureTLSConfig}
		httpClient := &http.Client{Transport: transport}
		rpcClient, err = rpc.DialHTTPWithClient(config.RPCUrl, httpClient)
	case "wss":
		dialer := *websocket.DefaultDialer
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		rpcClient, err = rpc.DialWebsocketWithDialer(ctx, config.RPCUrl, "", dialer)
	default:
		log.Printf("Connecting to RPC URL with default settings (scheme: %s).", parsedURL.Scheme)
		rpcClient, err = rpc.DialContext(ctx, config.RPCUrl)
	}

	if err != nil {
		log.Fatalf("Failed to connect to RPC: %v", err)
	}

	client := ethclient.NewClient(rpcClient)

	deployerAuth, err := getDeployerAuth(client, config.DeployerPrivateKey)
	if err != nil {
		log.Fatalf("Failed to get deployer auth: %v", err)
	}

	log.Printf("📍 Caller Address: %s", deployerAuth.From.Hex())
	log.Printf("🌐 RPC URL: %s", config.RPCUrl)
	log.Printf("🎯 Contract Address: %s", config.ProxyAddress)
	log.Printf("🔧 New Rust Server Addresses: %v", config.RustStorageIPs)

	proxyAddr := common.HexToAddress(config.ProxyAddress)

	// Read ABI
	abiData, err := os.ReadFile("../fileAbi.json")
	if err != nil {
		log.Fatalf("Failed to read ABI file: %v", err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(string(abiData)))
	if err != nil {
		log.Fatalf("Failed to parse ABI: %v", err)
	}

	filesContract := bind.NewBoundContract(proxyAddr, parsedABI, client, client, client)

	// Check current Rust servers
	callOpts := &bind.CallOpts{Context: ctx}
	var currentOut []interface{}
	err = filesContract.Call(callOpts, &currentOut, "getRustServerAddresses")
	if err != nil {
		log.Printf("⚠️ Warning: Failed to query current Rust server addresses: %v", err)
	} else if len(currentOut) > 0 {
		currentServers := *abi.ConvertType(currentOut[0], new([]string)).(*[]string)
		log.Printf("📡 Current on-chain Rust servers: %v", currentServers)
	}

	// Update Rust Server Addresses
	log.Println("\n⏳ Sending setRustServerAddresses transaction...")
	tx, err := filesContract.Transact(deployerAuth, "setRustServerAddresses", config.RustStorageIPs)
	if err != nil {
		log.Fatalf("❌ Failed to send transaction: %v", err)
	}
	log.Printf("📤 Transaction sent: %s", tx.Hash().Hex())

	receipt, err := waitForTransaction(client, tx.Hash(), "setRustServerAddresses")
	if err != nil {
		log.Fatalf("❌ Transaction failed: %v", err)
	}

	log.Printf("✅ Transaction mined in block %v! Gas used: %d", receipt.BlockNumber, receipt.GasUsed)

	// Verify update
	var updatedOut []interface{}
	err = filesContract.Call(callOpts, &updatedOut, "getRustServerAddresses")
	if err != nil {
		log.Fatalf("❌ Failed to verify updated Rust server addresses: %v", err)
	}
	updatedServers := *abi.ConvertType(updatedOut[0], new([]string)).(*[]string)

	log.Printf("🎉 Successfully updated Rust server addresses on-chain:")
	log.Printf("👉 %v", updatedServers)
}

func getDeployerAuth(client *ethclient.Client, privateKeyHex string) (*bind.TransactOpts, error) {
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("error casting public key to ECDSA")
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce: %w", err)
	}

	chainID, err := client.ChainID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}

	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to create transactor: %w", err)
	}

	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil || gasPrice == nil || gasPrice.Cmp(big.NewInt(100000)) < 0 {
		gasPrice = big.NewInt(100000)
	}

	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(0)
	auth.GasLimit = uint64(5000000)
	auth.GasPrice = gasPrice

	return auth, nil
}

func waitForTransaction(client *ethclient.Client, txHash common.Hash, name string) (*types.Receipt, error) {
	log.Printf("⏳ Waiting for %s transaction to be mined...", name)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil {
			if receipt.BlockNumber != nil {
				if receipt.Status == 1 {
					return receipt, nil
				}
				return nil, fmt.Errorf("transaction reverted with status 0")
			}
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for transaction %s: %w", txHash.Hex(), ctx.Err())
		case <-time.After(1 * time.Second):
		}
	}
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func parseCommaSeparatedString(input string) []string {
	if input == "" {
		return []string{}
	}
	input = strings.Trim(input, "\"")
	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
