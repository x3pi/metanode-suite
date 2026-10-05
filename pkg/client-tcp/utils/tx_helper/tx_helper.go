package tx_helper

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	clientpkg "tool-test/pkg/client-tcp"
	com_pkg "tool-test/pkg/client-tcp/common"
	c_config "tool-test/pkg/client-tcp/config"
	"tool-test/pkg/client-tcp/models"
	"tool-test/pkg/logger"
	pb "tool-test/pkg/proto"
	mt_transaction "tool-test/pkg/transaction"
	"tool-test/pkg/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	// nonceCache lưu lại nonce mới nhất đã được ghi nhận trên chain cho mỗi address
	nonceCache sync.Map
)

func NormalizeTxOptions(opts *models.TxOptions) models.TxOptions {
	if opts == nil {
		return models.TxOptions{}
	}
	normalized := models.TxOptions{
		Amount:      opts.Amount,
		MaxGas:      opts.MaxGas,
		MaxGasPrice: opts.MaxGasPrice,
		MaxTimeUse:  opts.MaxTimeUse,
	}
	if len(opts.Related) > 0 {
		normalized.Related = append([]common.Address(nil), opts.Related...)
	}
	return normalized
}

func SendReadTransactionWithoutNonce(
	action string,
	cli *clientpkg.Client,
	cfg *c_config.ClientConfig,
	contract common.Address,
	from common.Address,
	input []byte,
	opts *models.TxOptions,
) (types.Receipt, error) {
	if cli == nil || cfg == nil {
		return nil, fmt.Errorf("client and config are required")
	}
	if (contract == common.Address{}) {
		return nil, fmt.Errorf("contract address is required")
	}
	if (from == common.Address{}) {
		return nil, fmt.Errorf("from address is required")
	}

	normalized := NormalizeTxOptions(opts)
	amount := normalized.Amount
	if amount == nil {
		amount = big.NewInt(0)
	}

	related := make([]common.Address, 0, len(normalized.Related)+1)
	if len(normalized.Related) > 0 {
		related = append(related, normalized.Related...)
	}
	related = append(related, cfg.Address())

	maxGas := ChooseOrDefault(normalized.MaxGas, com_pkg.DefaultMaxGas)
	maxGasPrice := ChooseOrDefault(normalized.MaxGasPrice, com_pkg.DefaultMaxGasPrice)
	maxTimeUse := ChooseOrDefault(normalized.MaxTimeUse, com_pkg.DefaultMaxExecution)

	logger.Info("[Client] Payload : %x", input)
	callData := mt_transaction.NewCallData(input)
	payload, err := callData.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal calldata for %s: %w", action, err)
	}
	// Gửi read transaction
	receipt, err := cli.ReadTransactionWithoutNonce(
		from,
		contract,
		amount,
		payload,
		related,
		maxGas,
		maxGasPrice,
		maxTimeUse,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send %s read transaction: %w", action, err)
	}
	if receipt == nil {
		return nil, fmt.Errorf("%s read transaction returned empty receipt", action)
	}
	status := receipt.Status()
	if status != pb.RECEIPT_STATUS_RETURNED && status != pb.RECEIPT_STATUS_HALTED {
		return nil, fmt.Errorf("%s read transaction failed with status %s returned %s \n receipt %v", action, status.String(), string(receipt.Return()), receipt)
	}
	logger.Info("✅ %s completed (status=%s)", action, status.String())
	return receipt, nil
}

func SendEstimateGas(
	action string,
	cli *clientpkg.Client,
	cfg *c_config.ClientConfig,
	contract common.Address,
	from common.Address,
	input []byte,
	opts *models.TxOptions,
) (types.Receipt, error) {
	if cli == nil || cfg == nil {
		return nil, fmt.Errorf("client and config are required")
	}
	if (contract == common.Address{}) {
		return nil, fmt.Errorf("contract address is required")
	}
	if (from == common.Address{}) {
		return nil, fmt.Errorf("from address is required")
	}

	normalized := NormalizeTxOptions(opts)
	amount := normalized.Amount
	if amount == nil {
		amount = big.NewInt(0)
	}

	related := make([]common.Address, 0, len(normalized.Related)+1)
	if len(normalized.Related) > 0 {
		related = append(related, normalized.Related...)
	}
	related = append(related, cfg.Address())

	maxGas := ChooseOrDefault(normalized.MaxGas, com_pkg.DefaultMaxGas)
	maxGasPrice := ChooseOrDefault(normalized.MaxGasPrice, com_pkg.DefaultMaxGasPrice)
	maxTimeUse := ChooseOrDefault(normalized.MaxTimeUse, com_pkg.DefaultMaxExecution)

	callData := mt_transaction.NewCallData(input)
	payload, err := callData.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal calldata for %s: %w", action, err)
	}
	// Gửi estimate gas transaction
	receipt, err := cli.EstimateGas(
		from,
		contract,
		amount,
		payload,
		related,
		maxGas,
		maxGasPrice,
		maxTimeUse,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send %s estimate gas transaction: %w", action, err)
	}
	if receipt == nil {
		return nil, fmt.Errorf("%s estimate gas transaction returned empty receipt", action)
	}
	status := receipt.Status()
	if status != pb.RECEIPT_STATUS_RETURNED && status != pb.RECEIPT_STATUS_HALTED {
		return nil, fmt.Errorf("%s estimate gas transaction failed with status %s returned %s", action, status.String(), string(receipt.Return()))
	}
	logger.Info("✅ %s completed (status=%s)", action, status.String())
	return receipt, nil
}

func SendReadTransaction(
	action string,
	cli *clientpkg.Client,
	cfg *c_config.ClientConfig,
	contract common.Address,
	from common.Address,
	input []byte,
	opts *models.TxOptions,
) (types.Receipt, error) {
	if cli == nil || cfg == nil {
		return nil, fmt.Errorf("client and config are required")
	}
	if (contract == common.Address{}) {
		return nil, fmt.Errorf("contract address is required")
	}
	if (from == common.Address{}) {
		return nil, fmt.Errorf("from address is required")
	}

	normalized := NormalizeTxOptions(opts)
	amount := normalized.Amount
	if amount == nil {
		amount = big.NewInt(0)
	}

	related := make([]common.Address, 0, len(normalized.Related)+1)
	if len(normalized.Related) > 0 {
		related = append(related, normalized.Related...)
	}
	related = append(related, cfg.Address())

	maxGas := ChooseOrDefault(normalized.MaxGas, com_pkg.DefaultMaxGas)
	maxGasPrice := ChooseOrDefault(normalized.MaxGasPrice, com_pkg.DefaultMaxGasPrice)
	maxTimeUse := ChooseOrDefault(normalized.MaxTimeUse, com_pkg.DefaultMaxExecution)

	callData := mt_transaction.NewCallData(input)
	payload, err := callData.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal calldata for %s: %w", action, err)
	}
	// payload := input

	// Gửi read transaction
	receipt, err := cli.ReadTransaction(
		from,
		contract,
		amount,
		payload,
		related,
		maxGas,
		maxGasPrice,
		maxTimeUse,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send %s read transaction: %w", action, err)
	}
	if receipt == nil {
		return nil, fmt.Errorf("%s read transaction returned empty receipt", action)
	}
	status := receipt.Status()
	if status != pb.RECEIPT_STATUS_RETURNED && status != pb.RECEIPT_STATUS_HALTED {
		return nil, fmt.Errorf("%s read transaction failed with status %s returned %s", action, status.String(), string(receipt.Return()))
	}
	logger.Info("✅ %s completed (status=%s)", action, status.String())
	return receipt, nil
}

// SendSecpTransaction signs and sends a Type 0xFF transaction using an ECDSA secp256k1 key.
func SendSecpTransaction(
	action string,
	cli *clientpkg.Client,
	cfg *c_config.ClientConfig,
	privKey *ecdsa.PrivateKey,
	contract common.Address,
	from common.Address,
	input []byte,
	opts *models.TxOptions,
) (types.Receipt, error) {
	if cli == nil || cfg == nil {
		return nil, fmt.Errorf("client and config are required")
	}
	if privKey == nil {
		return nil, fmt.Errorf("ecdsa private key is required")
	}
	if (contract == common.Address{}) && action != "deploy" {
		return nil, fmt.Errorf("contract address is required")
	}
	if (from == common.Address{}) {
		from = crypto.PubkeyToAddress(privKey.PublicKey)
	}

	normalized := NormalizeTxOptions(opts)
	amount := normalized.Amount
	if amount == nil {
		amount = big.NewInt(0)
	}

	maxGas := ChooseOrDefault(normalized.MaxGas, com_pkg.DefaultMaxGas)
	maxGasPrice := ChooseOrDefault(normalized.MaxGasPrice, com_pkg.DefaultMaxGasPrice)

	var payload []byte
	var err error
	if action == "deploy" || contract == (common.Address{}) {
		deployData := mt_transaction.NewDeployData(input, common.Address{})
		payload, err = deployData.Marshal()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal deploydata for %s: %w", action, err)
		}
	} else if len(input) > 0 {
		callData := mt_transaction.NewCallData(input)
		payload, err = callData.Marshal()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal calldata for %s: %w", action, err)
		}
	}

	var cachedNonce uint64
	if val, ok := nonceCache.Load(from); ok {
		cachedNonce = val.(uint64)
	}

	as, err := cli.AccountState(from)
	if err != nil {
		return nil, fmt.Errorf("failed to get account state for %s: %w", from.Hex(), err)
	}

	usedNonce := as.Nonce()
	if cachedNonce != 0 && cachedNonce > usedNonce {
		logger.Info("Node nonce lag (got %d, expected %d), using cached nonce to proceed", usedNonce, cachedNonce)
		usedNonce = cachedNonce
	}

	receipt, tx, err := cli.SendSecpProtoTransactionWithNonce(
		privKey,
		contract,
		amount,
		maxGas,
		maxGasPrice,
		payload,
		usedNonce,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send %s secp transaction: %w", action, err)
	}

	nonceCache.Store(from, usedNonce+1)

	if receipt == nil {
		return nil, fmt.Errorf("%s transaction returned empty receipt (txHash: %s)", action, tx.Hash().Hex())
	}
	status := receipt.Status()
	if status != pb.RECEIPT_STATUS_RETURNED && status != pb.RECEIPT_STATUS_HALTED {
		return receipt, fmt.Errorf("%s transaction failed with status %s (Return: %s)", action, status.String(), string(receipt.Return()))
	}
	return receipt, nil
}

func SendTransaction(
	action string,
	cli *clientpkg.Client,
	cfg *c_config.ClientConfig,
	contract common.Address,
	from common.Address,
	input []byte,
	opts *models.TxOptions,
) (types.Receipt, error) {
	if cli == nil || cfg == nil {
		return nil, fmt.Errorf("client and config are required")
	}

	keyHex := cfg.EthPrivateKey
	if keyHex == "" {
		keyHex = cfg.PrivateKey_
	}
	if keyHex != "" {
		if privKey, errKey := crypto.HexToECDSA(strings.TrimPrefix(keyHex, "0x")); errKey == nil && privKey != nil {
			return SendSecpTransaction(action, cli, cfg, privKey, contract, from, input, opts)
		}
	}

	if (contract == common.Address{}) && action != "deploy" {
		return nil, fmt.Errorf("contract address is required")
	}
	if (from == common.Address{}) {
		return nil, fmt.Errorf("from address is required")
	}

	normalized := NormalizeTxOptions(opts)
	amount := normalized.Amount
	if amount == nil {
		amount = big.NewInt(0)
	}

	related := make([]common.Address, 0, len(normalized.Related)+1)
	if len(normalized.Related) > 0 {
		related = append(related, normalized.Related...)
	}
	related = append(related, cfg.Address())

	maxGas := ChooseOrDefault(normalized.MaxGas, com_pkg.DefaultMaxGas)
	maxGasPrice := ChooseOrDefault(normalized.MaxGasPrice, com_pkg.DefaultMaxGasPrice)
	maxTimeUse := ChooseOrDefault(normalized.MaxTimeUse, com_pkg.DefaultMaxExecution)

	callData := mt_transaction.NewCallData(input)
	payload, err := callData.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal calldata for %s: %w", action, err)
	}
	// payload := input
	// Gửi write transaction
	receipt, err := cli.SendTransactionWithDeviceKey(
		from,
		contract,
		amount,
		payload,
		related,
		maxGas,
		maxGasPrice,
		maxTimeUse,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send %s transaction: %w", action, err)
	}
	if receipt == nil {
		return nil, fmt.Errorf("%s transaction returned empty receipt", action)
	}
	status := receipt.Status()
	if status != pb.RECEIPT_STATUS_RETURNED && status != pb.RECEIPT_STATUS_HALTED {
		return nil, fmt.Errorf("%s transaction failed with status %s (Return: %s)", action, status.String(), string(receipt.Return()))
	}
	return receipt, nil
}

func SendTransactionAsync(
	action string,
	cli *clientpkg.Client,
	cfg *c_config.ClientConfig,
	contract common.Address,
	from common.Address,
	input []byte,
	opts *models.TxOptions,
) (common.Hash, error) {
	if cli == nil || cfg == nil {
		return common.Hash{}, fmt.Errorf("client and config are required")
	}
	if (contract == common.Address{}) && action != "deploy" {
		return common.Hash{}, fmt.Errorf("contract address is required")
	}
	if (from == common.Address{}) {
		return common.Hash{}, fmt.Errorf("from address is required")
	}

	normalized := NormalizeTxOptions(opts)
	amount := normalized.Amount
	if amount == nil {
		amount = big.NewInt(0)
	}

	related := make([]common.Address, 0, len(normalized.Related)+1)
	if len(normalized.Related) > 0 {
		related = append(related, normalized.Related...)
	}
	related = append(related, cfg.Address())

	maxGas := ChooseOrDefault(normalized.MaxGas, com_pkg.DefaultMaxGas)
	maxGasPrice := ChooseOrDefault(normalized.MaxGasPrice, com_pkg.DefaultMaxGasPrice)
	maxTimeUse := ChooseOrDefault(normalized.MaxTimeUse, com_pkg.DefaultMaxExecution)

	callData := mt_transaction.NewCallData(input)
	payload, err := callData.Marshal()
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to marshal calldata for %s: %w", action, err)
	}

	txHash, err := cli.SendTransactionWithDeviceKeyAsync(
		from,
		contract,
		amount,
		payload,
		related,
		maxGas,
		maxGasPrice,
		maxTimeUse,
	)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to send %s transaction async: %w", action, err)
	}
	logger.Info("✅ %s submitted async (txHash=%s)", action, txHash.Hex())
	return txHash, nil
}

func SendTransactionNoneKey(
	action string,
	cli *clientpkg.Client,
	cfg *c_config.ClientConfig,
	contract common.Address,
	from common.Address,
	input []byte,
	opts *models.TxOptions,
) (types.Receipt, error) {
	if cli == nil || cfg == nil {
		return nil, fmt.Errorf("client and config are required")
	}
	if (contract == common.Address{}) && action != "deploy" {
		return nil, fmt.Errorf("contract address is required")
	}
	if (from == common.Address{}) {
		return nil, fmt.Errorf("from address is required")
	}

	normalized := NormalizeTxOptions(opts)
	amount := normalized.Amount
	if amount == nil {
		amount = big.NewInt(0)
	}

	related := make([]common.Address, 0, len(normalized.Related)+1)
	if len(normalized.Related) > 0 {
		related = append(related, normalized.Related...)
	}
	related = append(related, cfg.Address())

	maxGas := ChooseOrDefault(normalized.MaxGas, com_pkg.DefaultMaxGas)
	maxGasPrice := ChooseOrDefault(normalized.MaxGasPrice, com_pkg.DefaultMaxGasPrice)
	maxTimeUse := ChooseOrDefault(normalized.MaxTimeUse, com_pkg.DefaultMaxExecution)

	callData := mt_transaction.NewCallData(input)
	payload, err := callData.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal calldata for %s: %w", action, err)
	}

	// Cơ chế kiểm tra nonce: đảm bảo nonce lấy về phải > cachedNonce
	var cachedNonce uint64
	if val, ok := nonceCache.Load(from); ok {
		cachedNonce = val.(uint64)
	}

	as, err := cli.GetAccountState(from, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to get account state: %w", err)
	}

	usedNonce := as.Nonce()

	// Nếu cache lớn hơn thì dùng cache để bypass node lag
	if cachedNonce != 0 && cachedNonce > usedNonce {
		logger.Info("Node nonce lag (got %d, expected %d), using cached nonce to proceed", usedNonce, cachedNonce)
		usedNonce = cachedNonce
	}

	// Gửi write transaction
	receipt, err := cli.SendTransaction(
		from,
		contract,
		amount,
		payload,
		related,
		maxGas,
		maxGasPrice,
		maxTimeUse,
		usedNonce,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send %s transaction: %w", action, err)
	}

	// Lưu lại cache khi gửi thành công
	nonceCache.Store(from, usedNonce+1)

	if receipt == nil {
		return nil, fmt.Errorf("%s transaction returned empty receipt", action)
	}
	status := receipt.Status()
	if status != pb.RECEIPT_STATUS_RETURNED && status != pb.RECEIPT_STATUS_HALTED {
		return nil, fmt.Errorf("%s transaction failed with status %s (Return: %s)", action, status.String(), string(receipt.Return()))
	}
	return receipt, nil
}

func ChooseOrDefault(value uint64, fallback uint64) uint64 {
	if value == 0 {
		return fallback
	}
	return value
}
