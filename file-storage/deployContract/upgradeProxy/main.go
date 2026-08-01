package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
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

// Config holds deployment configuration
type Config struct {
	RPCUrl             string
	DeployerPrivateKey string
	ProxyAddress       string // Thêm trường ProxyAddress
}

func main() {
	// Load environment variables from parent directory
	err := godotenv.Load("../.env")
	if err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}

	// Initialize deployment configuration
	config := &Config{
		RPCUrl:             getEnv("RPC_URL", "http://192.168.1.234:8545"),
		DeployerPrivateKey: getEnv("PRIVATE_KEY", ""),
		ProxyAddress:       getEnv("PROXY_ADDRESS", ""), // Proxy cần upgrade
	}

	if config.DeployerPrivateKey == "" {
		log.Fatal("PRIVATE_KEY is required in .env file")
	}

	if config.ProxyAddress == "" {
		log.Fatal("PROXY_ADDRESS is required in .env file. Please check deployment_*.json to get proxy address.")
	}

	var rpcClient *rpc.Client
	ctx := context.Background()
	rpcUrl := config.RPCUrl
	parsedURL, err := url.Parse(rpcUrl)
	if err != nil {
		log.Fatalf("Failed to parse RPC URL: %v", err)
	}

	switch parsedURL.Scheme {
	case "https":
		insecureTLSConfig := &tls.Config{InsecureSkipVerify: true}
		transport := &http.Transport{TLSClientConfig: insecureTLSConfig}
		httpClient := &http.Client{Transport: transport}
		rpcClient, err = rpc.DialHTTPWithClient(rpcUrl, httpClient)
	case "wss":
		dialer := *websocket.DefaultDialer
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		rpcClient, err = rpc.DialWebsocketWithDialer(ctx, rpcUrl, "", dialer)
	default:
		log.Printf("Connecting to RPC URL with default settings (scheme: %s).", parsedURL.Scheme)
		rpcClient, err = rpc.DialContext(ctx, rpcUrl)
	}

	if err != nil {
		log.Fatalf("Failed to connect to RPC: %v", err)
	}

	client := ethclient.NewClient(rpcClient)

	// Get deployer account
	deployerAuth, err := getDeployerAuth(client, config.DeployerPrivateKey)
	if err != nil {
		log.Fatalf("Failed to get deployer auth: %v", err)
	}

	log.Printf("🚀 Starting UPGRADE process...")
	log.Printf("📍 Deployer Address (Owner): %s", deployerAuth.From.Hex())
	log.Printf("🌐 RPC URL: %s", config.RPCUrl)
	log.Printf("🛡️ Target Proxy: %s", config.ProxyAddress)
	log.Println("=====================================")

	// Step 1: Deploy NEW Files Implementation contract
	log.Println("\n[1/2] Deploying NEW Files Implementation contract...")
	newImplementationAddress, err := deployFilesImplementation(client, deployerAuth)
	if err != nil {
		log.Fatalf("Failed to deploy NEW Files implementation: %v", err)
	}
	log.Printf("✅ NEW Files Implementation deployed at: %s", newImplementationAddress.Hex())

	// Step 2: Upgrade Proxy to point to New Implementation
	log.Println("\n[2/2] Upgrading Proxy...")
	proxyAddr := common.HexToAddress(config.ProxyAddress)
	
	// Increment nonce
	deployerAuth.Nonce = big.NewInt(0).Add(deployerAuth.Nonce, big.NewInt(1))

	err = upgradeProxy(client, deployerAuth, proxyAddr, newImplementationAddress)
	if err != nil {
		log.Fatalf("❌ Failed to upgrade Proxy: %v", err)
	}
	log.Printf("✅ Proxy %s successfully upgraded to Implementation %s", proxyAddr.Hex(), newImplementationAddress.Hex())

	log.Println("\n=====================================")
	log.Println("🎉 UPGRADE COMPLETED SUCCESSFULLY!")
	log.Println("=====================================")
}

// deployFilesImplementation deploys the Files implementation contract
func deployFilesImplementation(client *ethclient.Client, auth *bind.TransactOpts) (common.Address, error) {
	// Load ABI from fileV2Abi.json
	abiData, err := os.ReadFile("fileV2Abi.json")
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to read ABI file: %w", err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(string(abiData)))
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to parse ABI: %w", err)
	}

	// Load bytecode from byteCodeV2.json
	type BytecodeFile struct {
		File string `json:"file"`
	}

	bytecodeData, err := os.ReadFile("byteCodeV2.json")
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to read bytecode file: %w", err)
	}

	var bytecodeFile BytecodeFile
	if err := json.Unmarshal(bytecodeData, &bytecodeFile); err != nil {
		return common.Address{}, fmt.Errorf("failed to parse bytecode file: %w", err)
	}

	// Parse bytecode (remove 0x prefix if present)
	bytecodeHex := strings.TrimPrefix(bytecodeFile.File, "0x")
	bytecode := common.FromHex(bytecodeHex)

	if len(bytecode) == 0 {
		return common.Address{}, fmt.Errorf("contract bytecode not available")
	}

	// Deploy the implementation contract
	address, tx, _, err := bind.DeployContract(auth, parsedABI, bytecode, client)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to deploy contract: %w", err)
	}

	// Wait for transaction receipt
	receipt, err := waitForTransaction(client, tx.Hash(), "New Files Implementation")
	if err != nil {
		return common.Address{}, fmt.Errorf("transaction failed: %w", err)
	}

	log.Printf("✅ New Implementation contract deployed at: %s", address.Hex())
	log.Printf("⛽ Gas used: %d", receipt.GasUsed)

	return address, nil
}

// upgradeProxy calls upgradeToAndCall on the UUPS Proxy
func upgradeProxy(client *ethclient.Client, auth *bind.TransactOpts, proxyAddress common.Address, newImplementation common.Address) error {
	// Load ABI from fileV2Abi.json to get the UUPS upgradeToAndCall function
	abiData, err := os.ReadFile("fileV2Abi.json")
	if err != nil {
		return fmt.Errorf("failed to read ABI file: %w", err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(string(abiData)))
	if err != nil {
		return fmt.Errorf("failed to parse ABI: %w", err)
	}

	// Create bound contract for proxy
	contract := bind.NewBoundContract(proxyAddress, parsedABI, client, client, client)

	// In OpenZeppelin V5 UUPS, the function is upgradeToAndCall(address newImplementation, bytes data)
	// We pass empty bytes (0x) for data since we don't need to call any init function
	emptyData := []byte{}
	
	log.Printf("Calling upgradeToAndCall...")
	tx, err := contract.Transact(auth, "upgradeToAndCall", newImplementation, emptyData)
	if err != nil {
		return fmt.Errorf("failed to send upgrade transaction: %w", err)
	}

	log.Printf("📤 Upgrade transaction sent: %s", tx.Hash().Hex())

	// Wait for transaction receipt
	receipt, err := waitForTransaction(client, tx.Hash(), "UpgradeToAndCall")
	if err != nil {
		return fmt.Errorf("upgrade transaction failed: %w", err)
	}

	log.Printf("⛽ Gas used: %d", receipt.GasUsed)
	return nil
}

// getDeployerAuth creates a transaction auth from private key
func getDeployerAuth(client *ethclient.Client, privateKeyHex string) (*bind.TransactOpts, error) {
	// Remove 0x prefix if present
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

	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(0)
	auth.GasLimit = uint64(30000000)
	auth.GasPrice = nil // Use gas price suggestion

	return auth, nil
}

// waitForTransaction waits for a transaction to be mined
func waitForTransaction(client *ethclient.Client, txHash common.Hash, contractName string) (*types.Receipt, error) {
	log.Printf("⏳ Waiting for %s transaction to be mined...", contractName)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil {
			if receipt.Status == 1 {
				return receipt, nil
			}
			return nil, fmt.Errorf("transaction failed with status %d", receipt.Status)
		} else if err != ethereum.NotFound {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for transaction")
		case <-time.After(2 * time.Second):
			// Continue waiting
		}
	}
}

// getEnv gets environment variable with default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
