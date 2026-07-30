package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"encoding/json"
	"flag"
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

type Config struct {
	RPCUrl              string
	DeployerPrivateKey  string
	FileContractAddress string
}

func main() {
	// Parse CLI flags
	action := flag.String("action", "deploy", "Action to perform: deploy, add, remove")
	registryAddrFlag := flag.String("registry", "", "Address of the deployed registry proxy (for add/remove)")
	fileAddrFlag := flag.String("file", "", "Address of the file contract to add/remove")
	flag.Parse()

	// Load environment variables
	err := godotenv.Load(".env")
	if err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}

	config := &Config{
		RPCUrl:              getEnv("RPC_URL", "http://192.168.1.234:8545"),
		DeployerPrivateKey:  getEnv("PRIVATE_KEY", ""),
		FileContractAddress: getEnv("FILE_CONTRACT_ADDRESS", ""),
	}

	if config.DeployerPrivateKey == "" {
		log.Fatal("PRIVATE_KEY is required in .env file")
	}

	if *action == "deploy" && config.FileContractAddress == "" {
		log.Fatal("FILE_CONTRACT_ADDRESS is required in .env file for deployment")
	}

	if (*action == "add" || *action == "remove") && (*registryAddrFlag == "" || *fileAddrFlag == "") {
		log.Fatal("For add/remove actions, you must provide -registry and -file flags")
	}

	var rpcClient *rpc.Client
	ctx := context.Background()
	parsedURL, err := url.Parse(config.RPCUrl)
	if err != nil {
		log.Fatalf("Failed to parse RPC URL: %v", err)
	}

	switch parsedURL.Scheme {
	case "https":
		transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		httpClient := &http.Client{Transport: transport}
		rpcClient, err = rpc.DialHTTPWithClient(config.RPCUrl, httpClient)
	case "wss":
		dialer := *websocket.DefaultDialer
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		rpcClient, err = rpc.DialWebsocketWithDialer(ctx, config.RPCUrl, "", dialer)
	default:
		rpcClient, err = rpc.DialContext(ctx, config.RPCUrl)
	}

	if err != nil {
		log.Fatalf("Failed to connect to RPC: %v", err)
	}
	client := ethclient.NewClient(rpcClient)

	// Get deployer account
	isDeploy := *action == "deploy"
	deployerAuth, err := getDeployerAuth(client, config.DeployerPrivateKey, isDeploy)
	if err != nil {
		log.Fatalf("Failed to get deployer auth: %v", err)
	}

	log.Printf("📍 Deployer Address: %s", deployerAuth.From.Hex())

	// Read ABI
	abiData, err := os.ReadFile("registryAbi.json")
	if err != nil {
		log.Fatalf("Failed to read ABI file: %v", err)
	}
	parsedABI, err := abi.JSON(strings.NewReader(string(abiData)))
	if err != nil {
		log.Fatalf("Failed to parse ABI: %v", err)
	}

	if *action == "deploy" {
		log.Println("\n[1/3] Deploying Registry Implementation contract...")
		implementationAddress, err := deployRegistryImplementation(client, deployerAuth, &parsedABI)
		if err != nil {
			log.Fatalf("Failed to deploy Registry implementation: %v", err)
		}
		log.Printf("✅ Registry Implementation deployed at: %s", implementationAddress.Hex())

		log.Println("\n[2/3] Deploying Registry Proxy contract...")
		// Tăng nonce
		deployerAuth.Nonce = big.NewInt(0).Add(deployerAuth.Nonce, big.NewInt(1))
		proxyAddress, err := deployRegistryProxy(client, deployerAuth, implementationAddress, &parsedABI)
		if err != nil {
			log.Fatalf("Failed to deploy Registry Proxy: %v", err)
		}
		log.Printf("✅ Registry Proxy deployed at: %s", proxyAddress.Hex())

		log.Println("\n[3/3] Registering File contract...")
		registryContract := bind.NewBoundContract(proxyAddress, parsedABI, client, client, client)
		fileAddr := common.HexToAddress(config.FileContractAddress)

		deployerAuth.Nonce = big.NewInt(0).Add(deployerAuth.Nonce, big.NewInt(1))
		if err := interactWithRegistry(client, deployerAuth, registryContract, "registerContract", fileAddr); err != nil {
			log.Fatalf("❌ Failed to register contract: %v", err)
		}
		log.Printf("✅ Successfully registered File contract: %s", fileAddr.Hex())

		saveDeploymentInfo(map[string]interface{}{
			"registryImplementation": implementationAddress.Hex(),
			"registryProxy":          proxyAddress.Hex(),
			"fileContract":           fileAddr.Hex(),
			"deployer":               deployerAuth.From.Hex(),
			"rpcUrl":                 config.RPCUrl,
			"timestamp":              time.Now().Format(time.RFC3339),
		})
	} else if *action == "add" || *action == "remove" {
		registryAddr := common.HexToAddress(*registryAddrFlag)
		fileAddr := common.HexToAddress(*fileAddrFlag)
		registryContract := bind.NewBoundContract(registryAddr, parsedABI, client, client, client)

		method := "registerContract"
		if *action == "remove" {
			method = "deregisterContract"
		}

		log.Printf("🔄 Executing %s for file contract %s on registry %s...", method, fileAddr.Hex(), registryAddr.Hex())

		if err := interactWithRegistry(client, deployerAuth, registryContract, method, fileAddr); err != nil {
			log.Fatalf("❌ Failed to %s: %v", method, err)
		}
		log.Printf("✅ Successfully executed %s!", method)
	} else {
		log.Fatalf("Unknown action: %s", *action)
	}
}

func deployRegistryImplementation(client *ethclient.Client, auth *bind.TransactOpts, parsedABI *abi.ABI) (common.Address, error) {
	bytecodeData, err := os.ReadFile("byteCode/registryByteCode.json")
	if err != nil {
		return common.Address{}, err
	}
	type BytecodeFile struct {
		Registry string `json:"registry"`
	}
	var bytecodeFile BytecodeFile
	json.Unmarshal(bytecodeData, &bytecodeFile)
	bytecode := common.FromHex(strings.TrimPrefix(bytecodeFile.Registry, "0x"))

	address, tx, _, err := bind.DeployContract(auth, *parsedABI, bytecode, client)
	if err != nil {
		return common.Address{}, err
	}
	receipt, err := waitForTransaction(client, tx.Hash(), "Registry Implementation")
	if err != nil {
		return common.Address{}, err
	}
	log.Printf("⛽ Gas used: %d", receipt.GasUsed)
	return address, nil
}

func deployRegistryProxy(client *ethclient.Client, auth *bind.TransactOpts, implementationAddress common.Address, parsedImplABI *abi.ABI) (common.Address, error) {
	bytecodeData, err := os.ReadFile("byteCode/registryByteCode.json")
	if err != nil {
		return common.Address{}, err
	}
	type BytecodeFile struct {
		RegistryProxy string `json:"registryProxy"`
	}
	var bytecodeFile BytecodeFile
	json.Unmarshal(bytecodeData, &bytecodeFile)
	proxyBytecode := common.FromHex(strings.TrimPrefix(bytecodeFile.RegistryProxy, "0x"))

	proxyABI := `[{"inputs":[{"internalType":"address","name":"_implementation","type":"address"},{"internalType":"bytes","name":"_data","type":"bytes"}],"stateMutability":"nonpayable","type":"constructor"}]`
	parsedProxyABI, _ := abi.JSON(strings.NewReader(proxyABI))

	initData, err := parsedImplABI.Pack("initialize", auth.From)
	if err != nil {
		return common.Address{}, err
	}
	constructorArgs, _ := parsedProxyABI.Pack("", implementationAddress, initData)
	fullBytecode := append(proxyBytecode, constructorArgs...)

	tx := types.NewContractCreation(
		auth.Nonce.Uint64(),
		auth.Value,
		auth.GasLimit,
		auth.GasPrice,
		fullBytecode,
	)

	signedTx, err := auth.Signer(auth.From, tx)
	if err != nil {
		return common.Address{}, err
	}
	err = client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		return common.Address{}, err
	}
	receipt, err := waitForTransaction(client, signedTx.Hash(), "RegistryProxy")
	if err != nil {
		return common.Address{}, err
	}
	log.Printf("⛽ Gas used: %d", receipt.GasUsed)
	return receipt.ContractAddress, nil
}

func interactWithRegistry(client *ethclient.Client, auth *bind.TransactOpts, contract *bind.BoundContract, method string, fileContractAddress common.Address) error {
	tx, err := contract.Transact(auth, method, fileContractAddress)
	if err != nil {
		return err
	}
	log.Printf("📤 %s transaction sent: %s", method, tx.Hash().Hex())
	receipt, err := waitForTransaction(client, tx.Hash(), method)
	if err != nil {
		return err
	}
	log.Printf("⛽ Gas used: %d", receipt.GasUsed)
	return nil
}

func getDeployerAuth(client *ethclient.Client, privateKeyHex string, isDeploy bool) (*bind.TransactOpts, error) {
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKeyHex, "0x"))
	if err != nil {
		return nil, err
	}
	publicKeyECDSA := privateKey.Public().(*ecdsa.PublicKey)
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
	nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
	if err != nil {
		return nil, err
	}
	if isDeploy && nonce != 1 {
		return nil, fmt.Errorf("yêu cầu deploy thất bại: nonce phải là 1 (đang là %d)", nonce)
	}
	chainID, _ := client.ChainID(context.Background())
	auth, _ := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(0)
	auth.GasLimit = uint64(30000000)
	return auth, nil
}

func waitForTransaction(client *ethclient.Client, txHash common.Hash, contractName string) (*types.Receipt, error) {
	log.Printf("⏳ Waiting for %s transaction to be mined...", contractName)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil {
			if receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
				if receipt.Status == 1 {
					return receipt, nil
				}
				
				// Lấy raw JSON receipt để đọc field revertReason
				var raw map[string]interface{}
				errRaw := client.Client().CallContext(ctx, &raw, "eth_getTransactionReceipt", txHash)
				if errRaw == nil && raw != nil {
					if reason, ok := raw["revertReason"].(string); ok {
						return nil, fmt.Errorf("transaction failed with revert reason: %s", reason)
					}
				}
				
				return nil, fmt.Errorf("transaction failed with status %d", receipt.Status)
			}
		} else if err != ethereum.NotFound {
			return nil, fmt.Errorf("system error checking receipt: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for transaction")
		case <-time.After(2 * time.Second):
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

func saveDeploymentInfo(info map[string]interface{}) {
	data, _ := json.MarshalIndent(info, "", "  ")
	filename := fmt.Sprintf("deployment_%s.json", time.Now().Format("20060102_150405"))
	os.WriteFile(filename, data, 0644)
	log.Printf("\n💾 Deployment info saved to: %s", filename)
}
