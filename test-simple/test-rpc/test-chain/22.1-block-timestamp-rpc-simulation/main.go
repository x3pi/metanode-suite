/*
 * BÀI TEST: 22.1-block-timestamp-rpc-simulation
 * MÔ TẢ   : Regression test cho block.timestamp trong eth_call và eth_estimateGas.
 */
package main

import (
	"context"
	"crypto/ecdsa"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/config"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const simulationDeadlineOffsetSeconds uint64 = 3600
const deploymentGasLimit uint64 = 500_000

//go:embed build/BlockTimestampRPCSimulationTest_BlockTimestampRPCSimulationTest.abi
var abiJSON string

//go:embed build/BlockTimestampRPCSimulationTest_BlockTimestampRPCSimulationTest.bin
var bytecodeHex string

type testOptions struct {
	configPath string
	keysFile   string
}

func RunTest(configPath string) error {
	return runTest(testOptions{configPath: configPath})
}

func runTest(opts testOptions) error {
	if opts.configPath == "" {
		opts.configPath = "../config.json"
	}

	cfg, err := config.LoadConfig(opts.configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	keys, err := loadPrivateKeys(opts.keysFile, cfg.PrivateKeys)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return fmt.Errorf("config does not contain a private key")
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		return fmt.Errorf("connect RPC %s: %w", cfg.RPCUrl, err)
	}
	defer client.Close()

	parsedABI, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return fmt.Errorf("parse contract ABI: %w", err)
	}
	bytecode, err := hexutil.Decode("0x" + strings.TrimSpace(bytecodeHex))
	if err != nil {
		return fmt.Errorf("decode contract bytecode: %w", err)
	}
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(keys[0], "0x"))
	if err != nil {
		return fmt.Errorf("parse deployer key: %w", err)
	}

	ctx := context.Background()
	contractAddress, deployReceipt, err := deployProbe(ctx, client, privateKey, cfg.ChainID, bytecode)
	if err != nil {
		return fmt.Errorf("deploy timestamp probe: %w", err)
	}
	if deployReceipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("probe deployment reverted in block %s", deployReceipt.BlockNumber)
	}

	deployedBlock, err := client.BlockByNumber(ctx, deployReceipt.BlockNumber)
	if err != nil {
		return fmt.Errorf("read deployment block %s: %w", deployReceipt.BlockNumber, err)
	}

	gotTimestamp, err := callTimestamp(ctx, client, contractAddress, parsedABI, deployReceipt.BlockNumber)
	if err != nil {
		return fmt.Errorf("eth_call currentTimestamp at block %d: %w", deployedBlock.NumberU64(), err)
	}
	if gotTimestamp != deployedBlock.Time() {
		return fmt.Errorf(
			"TIMESTAMP REGRESSION: eth_call block.timestamp=%d, but eth_getBlockByNumber(%d).timestamp=%d; expected seconds, got a value likely expressed in milliseconds",
			gotTimestamp, deployedBlock.NumberU64(), deployedBlock.Time(),
		)
	}

	latestBlock, err := client.BlockByNumber(ctx, nil)
	if err != nil {
		return fmt.Errorf("read latest block: %w", err)
	}
	deadline := new(big.Int).SetUint64(latestBlock.Time() + simulationDeadlineOffsetSeconds)
	if err := requireBeforeViaCall(ctx, client, contractAddress, parsedABI, deadline); err != nil {
		return fmt.Errorf("TIMESTAMP REGRESSION: eth_call considers a deadline one hour after latest block (%d) expired: %w", latestBlock.Time(), err)
	}
	if err := requireBeforeViaEstimateGas(ctx, client, contractAddress, parsedABI, deadline); err != nil {
		return fmt.Errorf("TIMESTAMP REGRESSION: eth_estimateGas considers a deadline one hour after latest block (%d) expired: %w", latestBlock.Time(), err)
	}

	legacyTimestamp := latestBlock.Time() * 1000
	fmt.Println("==========================================================")
	fmt.Println("TEST 22.1: RPC simulation block.timestamp")
	fmt.Printf("Probe contract: %s\n", contractAddress.Hex())
	fmt.Printf("Pinned block %d: RPC=%d, eth_call=%d\n", deployedBlock.NumberU64(), deployedBlock.Time(), gotTimestamp)
	fmt.Printf("Latest block %d: timestamp=%d, deadline=%d\n", latestBlock.NumberU64(), latestBlock.Time(), deadline.Uint64())
	fmt.Printf("Legacy faulty value would be %d; it is after deadline: %t\n", legacyTimestamp, legacyTimestamp >= deadline.Uint64())
	fmt.Println("TEST PASSED: eth_call and eth_estimateGas use seconds, matching RPC block timestamp.")
	return nil
}

func main() {
	configPath := flag.String("config", "../config.json", "path to config.json")
	keysFile := flag.String("keys", "", "optional JSON private-key file")
	flag.Parse()

	if err := runTest(testOptions{configPath: *configPath, keysFile: *keysFile}); err != nil {
		log.Fatal(err)
	}
}

func deployProbe(ctx context.Context, client *ethclient.Client, privateKey *ecdsa.PrivateKey, chainID int64, bytecode []byte) (common.Address, *types.Receipt, error) {
	from := crypto.PubkeyToAddress(privateKey.PublicKey)
	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return common.Address{}, nil, err
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		gasPrice = big.NewInt(1_000_000_000)
	}

	// Do not call EstimateGas here: this test must still be able to deploy on a
	// node that has the timestamp bug in its estimation simulation.
	tx := types.NewContractCreation(nonce, big.NewInt(0), deploymentGasLimit, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), privateKey)
	if err != nil {
		return common.Address{}, nil, err
	}
	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return common.Address{}, nil, err
	}
	receipt, err := waitReceipt(client, signedTx.Hash())
	if err != nil {
		return common.Address{}, nil, err
	}
	return receipt.ContractAddress, receipt, nil
}

func callTimestamp(ctx context.Context, client *ethclient.Client, contractAddress common.Address, parsedABI abi.ABI, blockNumber *big.Int) (uint64, error) {
	data, err := parsedABI.Pack("currentTimestamp")
	if err != nil {
		return 0, err
	}
	result, err := client.CallContract(ctx, ethereum.CallMsg{To: &contractAddress, Data: data}, blockNumber)
	if err != nil {
		return 0, err
	}
	outputs, err := parsedABI.Unpack("currentTimestamp", result)
	if err != nil || len(outputs) != 1 {
		return 0, fmt.Errorf("decode currentTimestamp result: %w", err)
	}
	timestamp, ok := outputs[0].(*big.Int)
	if !ok {
		return 0, fmt.Errorf("currentTimestamp returned %T, want *big.Int", outputs[0])
	}
	return timestamp.Uint64(), nil
}

func requireBeforeViaCall(ctx context.Context, client *ethclient.Client, contractAddress common.Address, parsedABI abi.ABI, deadline *big.Int) error {
	data, err := parsedABI.Pack("requireBefore", deadline)
	if err != nil {
		return err
	}
	_, err = client.CallContract(ctx, ethereum.CallMsg{To: &contractAddress, Data: data}, nil)
	return err
}

func requireBeforeViaEstimateGas(ctx context.Context, client *ethclient.Client, contractAddress common.Address, parsedABI abi.ABI, deadline *big.Int) error {
	data, err := parsedABI.Pack("requireBefore", deadline)
	if err != nil {
		return err
	}
	_, err = client.EstimateGas(ctx, ethereum.CallMsg{To: &contractAddress, Data: data})
	return err
}

func waitReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		receipt, err := client.TransactionReceipt(context.Background(), txHash)
		if err == nil && receipt != nil && receipt.BlockNumber != nil {
			return receipt, nil
		}
		if err != nil && !strings.Contains(err.Error(), "not found") {
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("timeout waiting for receipt %s", txHash.Hex())
}

func loadPrivateKeys(keysFilePath string, configKeys []string) ([]string, error) {
	if keysFilePath == "" {
		return configKeys, nil
	}
	raw, err := os.ReadFile(keysFilePath)
	if err != nil {
		return nil, fmt.Errorf("read private-key file: %w", err)
	}
	var keys []string
	if err := json.Unmarshal(raw, &keys); err == nil && len(keys) > 0 {
		return keys, nil
	}
	var keyed []struct {
		PrivateKey string `json:"private_key"`
	}
	if err := json.Unmarshal(raw, &keyed); err != nil {
		return nil, fmt.Errorf("parse private-key file: %w", err)
	}
	for _, entry := range keyed {
		if entry.PrivateKey != "" {
			keys = append(keys, entry.PrivateKey)
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("private-key file contains no keys")
	}
	return keys, nil
}
