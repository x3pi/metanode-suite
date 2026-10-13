package client

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"

	"tool-test/pkg/client-tcp/command"
	"tool-test/pkg/logger"
	p_network "tool-test/pkg/network"
	pb "tool-test/pkg/proto"
	"tool-test/pkg/state"
	mt_transaction "tool-test/pkg/transaction"
	"tool-test/pkg/types"
	t_network "tool-test/pkg/types/network"

	"google.golang.org/protobuf/proto"
)

// GetAccountState lấy account state, chờ phản hồi AccountState hoặc TransactionError qua ID-matching
func (client *Client) GetAccountState(address common.Address, timeout time.Duration) (types.AccountState, error) {
	respMsg, err := client.sendChainRequest(command.GetAccountState, address.Bytes(), timeout)
	if err != nil {
		return nil, fmt.Errorf("send error: %w", err)
	}

	cmd := respMsg.Command()
	if cmd == command.TransactionError {
		txErr := &mt_transaction.TransactionHashWithError{}
		if err := txErr.Unmarshal(respMsg.Body()); err != nil {
			return nil, fmt.Errorf("failed to unmarshal transaction error: %w", err)
		}
		return nil, fmt.Errorf("transaction error: %s", txErr.Proto().Description)
	}

	if cmd == command.AccountState {
		accountState := &state.AccountState{}
		if err := accountState.Unmarshal(respMsg.Body()); err != nil {
			return nil, fmt.Errorf("failed to unmarshal account state: %w", err)
		}
		return accountState, nil
	}

	return nil, fmt.Errorf("unexpected command: %s", cmd)
}

// sendChainRequest gửi command trực tiếp lên chain và đợi response theo header ID
func (client *Client) sendChainRequest(cmd string, body []byte, timeout time.Duration) (t_network.Message, error) {
	parentConn := client.clientContext.ConnectionsManager.ParentConnection()
	if parentConn == nil || !parentConn.IsConnect() {
		return nil, fmt.Errorf("parent connection not available")
	}

	id := uuid.New().String()
	respCh := make(chan t_network.Message, 1)

	client.pendingChainRequests.Store(id, respCh)

	msg := p_network.NewMessage(&pb.Message{
		Header: &pb.Header{
			Command: cmd,
			ID:      id,
		},
		Body: body,
	})

	if err := parentConn.SendMessage(msg); err != nil {
		client.pendingChainRequests.Delete(id)
		return nil, fmt.Errorf("failed to send %s: %w", cmd, err)
	}
	select {
	case resp := <-respCh:
		return resp, nil
	case <-time.After(timeout):
		client.pendingChainRequests.Delete(id)
		return nil, fmt.Errorf("timeout waiting for %s (id=%s)", cmd, id)
	}
}

// ChainGetChainId lấy chain ID trực tiếp từ chain (raw uint64)
func (client *Client) ChainGetChainId() (uint64, error) {
	respMsg, err := client.sendChainRequest(command.GetChainId, nil, 60*time.Second)
	if err != nil {
		return 0, err
	}
	resp := respMsg.Body()
	if len(resp) < 8 {
		return 0, fmt.Errorf("invalid chain id response: %d bytes", len(resp))
	}
	chainId := binary.BigEndian.Uint64(resp)
	logger.Info("✅ ChainGetChainId: %d", chainId)
	return chainId, nil
}

// ChainGetNonce lấy nonce trực tiếp từ chain (dùng lệnh GetNonce)
func (client *Client) ChainGetNonce(address common.Address) (uint64, error) {
	respMsg, err := client.sendChainRequest(command.GetNonce, address.Bytes(), 10*time.Second)
	if err != nil {
		return 0, err
	}
	resp := respMsg.Body()
	var nonce uint64
	if len(resp) >= 8 {
		nonce = binary.BigEndian.Uint64(resp)
	}
	return nonce, nil
}

// ChainGetBlockNumber lấy block number trực tiếp từ chain (raw uint64)
func (client *Client) ChainGetBlockNumber() (uint64, error) {
	respMsg, err := client.sendChainRequest(command.GetBlockNumber, nil, 60*time.Second)
	if err != nil {
		return 0, err
	}
	resp := respMsg.Body()
	if len(resp) < 8 {
		return 0, fmt.Errorf("invalid block number response: %d bytes", len(resp))
	}
	bn := binary.BigEndian.Uint64(resp)
	return bn, nil
}

// ChainGetTransactionReceipt lấy receipt trực tiếp từ chain theo txHash
func (client *Client) ChainGetTransactionReceipt(txHash common.Hash) (*pb.GetTransactionReceiptResponse, error) {
	req := &pb.GetTransactionReceiptRequest{
		TransactionHash: txHash.Bytes(),
	}
	requestBytes, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GetTransactionReceiptRequest: %w", err)
	}

	respMsg, err := client.sendChainRequest(command.GetTransactionReceipt, requestBytes, 60*time.Second)
	if err != nil {
		return nil, err
	}

	resp := &pb.GetTransactionReceiptResponse{}
	if err := proto.Unmarshal(respMsg.Body(), resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal GetTransactionReceiptResponse: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("server error: %s", resp.Error)
	}
	return resp, nil
}

// SendRawEthTransaction submits one signed EIP-2718 Ethereum envelope through
// the chain-direct TCP ingress. TransactionSuccess only confirms mempool
// admission; callers must wait for the receipt separately to observe execution.
func (client *Client) SendRawEthTransaction(rawTx []byte) (common.Hash, error) {
	if len(rawTx) == 0 {
		return common.Hash{}, fmt.Errorf("raw Ethereum transaction is empty")
	}

	respMsg, err := client.sendChainRequest(command.SendRawTransaction, rawTx, 60*time.Second)
	if err != nil {
		return common.Hash{}, fmt.Errorf("send raw Ethereum transaction: %w", err)
	}

	switch respMsg.Command() {
	case command.TransactionSuccess:
		if len(respMsg.Body()) != common.HashLength {
			return common.Hash{}, fmt.Errorf("invalid TransactionSuccess hash length: %d", len(respMsg.Body()))
		}
		return common.BytesToHash(respMsg.Body()), nil
	case command.TransactionError:
		txErr := &mt_transaction.TransactionHashWithError{}
		if err := txErr.Unmarshal(respMsg.Body()); err != nil {
			return common.Hash{}, fmt.Errorf("failed to unmarshal transaction error: %w", err)
		}
		return common.Hash{}, fmt.Errorf("transaction rejected: %s", txErr.Proto().Description)
	default:
		return common.Hash{}, fmt.Errorf("unexpected command: %s", respMsg.Command())
	}
}

// ChainGetLogs lấy logs từ chain theo filter criteria
func (client *Client) ChainGetLogs(
	blockHash []byte,
	fromBlock string,
	toBlock string,
	addresses []common.Address,
	topics [][]common.Hash,
) (*pb.GetLogsResponse, error) {
	request := &pb.GetLogsRequest{}
	if len(blockHash) > 0 {
		request.BlockHash = blockHash
	}
	if fromBlock != "" {
		request.FromBlock = []byte(fromBlock)
	}
	if toBlock != "" {
		request.ToBlock = []byte(toBlock)
	}
	if len(addresses) > 0 {
		request.Addresses = make([][]byte, len(addresses))
		for i, addr := range addresses {
			request.Addresses[i] = addr.Bytes()
		}
	}
	if len(topics) > 0 {
		request.Topics = make([]*pb.TopicFilter, len(topics))
		for i, topicList := range topics {
			if len(topicList) > 0 {
				hashes := make([][]byte, len(topicList))
				for j, hash := range topicList {
					hashes[j] = hash.Bytes()
				}
				request.Topics[i] = &pb.TopicFilter{Hashes: hashes}
			}
		}
	}

	requestBytes, err := proto.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GetLogsRequest: %w", err)
	}

	respMsg, err := client.sendChainRequest(command.GetLogs, requestBytes, 60*time.Second)
	if err != nil {
		return nil, err
	}
	response := &pb.GetLogsResponse{}
	if err := proto.Unmarshal(respMsg.Body(), response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal GetLogsResponse: %w", err)
	}
	if response.Error != "" {
		return nil, fmt.Errorf("server error: %s", response.Error)
	}
	return response, nil
}

// SendTransactionWithDeviceKeySync gửi giao dịch và đợi TransactionSuccess.
func (client *Client) SendTransactionWithDeviceKeySync(
	fromAddress common.Address,
	toAddress common.Address,
	pendingUse *big.Int,
	amount *big.Int,
	maxGas uint64,
	maxGasPrice uint64,
	maxTimeUse uint64,
	data []byte,
	relatedAddress [][]byte,
	lastDeviceKey common.Hash,
	newDeviceKey common.Hash,
	nonce uint64,
	deviceKey []byte,
	chainId uint64,
) (common.Hash, error) {
	transaction := mt_transaction.NewTransaction(
		fromAddress,
		toAddress,
		amount,
		maxGas,
		maxGasPrice,
		maxTimeUse,
		data,
		relatedAddress,
		lastDeviceKey,
		newDeviceKey,
		nonce,
		chainId,
	)
	transaction.SetSign(client.clientContext.KeyPair.PrivateKey())

	transactionWithDeviceKey := &pb.TransactionWithDeviceKey{
		Transaction: transaction.Proto().(*pb.Transaction),
		DeviceKey:   deviceKey,
	}

	bTransactionWithDeviceKey, err := proto.Marshal(transactionWithDeviceKey)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to marshal TransactionWithDeviceKey: %w", err)
	}

	respMsg, err := client.sendChainRequest(command.SendTransactionWithDeviceKey, bTransactionWithDeviceKey, 300*time.Second)
	if err != nil {
		return common.Hash{}, err
	}

	cmd := respMsg.Command()
	if cmd == command.TransactionError {
		txErr := &mt_transaction.TransactionHashWithError{}
		if err := txErr.Unmarshal(respMsg.Body()); err != nil {
			return common.Hash{}, fmt.Errorf("failed to unmarshal transaction error: %w", err)
		}
		return common.Hash{}, fmt.Errorf("transaction error: %s", txErr.Proto().Description)
	}

	if cmd == command.TransactionSuccess {
		body := respMsg.Body()
		if len(body) > 0 {
			return common.BytesToHash(body), nil
		}
	}

	return common.Hash{}, fmt.Errorf("unexpected command: %s", cmd)
}

// RegistrationInfo is an alias to pb.RegistrationResult for protobuf-based account gate onboarding.
type RegistrationInfo = pb.RegistrationResult

// ChainGetRegistrationMessage requests 32-byte hash to sign for address onboarding.
func (client *Client) ChainGetRegistrationMessage(addr common.Address) (common.Hash, error) {
	respMsg, err := client.sendChainRequest(command.GetRegistrationMessage, addr.Bytes(), 10*time.Second)
	if err != nil {
		return common.Hash{}, err
	}
	body := respMsg.Body()
	if len(body) != common.HashLength {
		return common.Hash{}, fmt.Errorf("invalid registration message hash length: %d (expected %d)", len(body), common.HashLength)
	}
	return common.BytesToHash(body), nil
}

// ChainGetRegistrationStatus queries current onboarding status for address via protobuf.
func (client *Client) ChainGetRegistrationStatus(addr common.Address) (*pb.RegistrationResult, error) {
	respMsg, err := client.sendChainRequest(command.GetRegistrationStatus, addr.Bytes(), 10*time.Second)
	if err != nil {
		return nil, err
	}
	var info pb.RegistrationResult
	if err := proto.Unmarshal(respMsg.Body(), &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal registration status proto: %w", err)
	}
	return &info, nil
}

// ChainRegisterAccountWait submits account registration via protobuf (RegisterAccountRequest)
// and awaits an asynchronous push notification (RegistrationResult).
// It does NOT use polling; rather, the node pushes the final result back into this connection.
func (client *Client) ChainRegisterAccountWait(addr common.Address, sig []byte, timeout time.Duration) (*pb.RegistrationResult, error) {
	parentConn := client.clientContext.ConnectionsManager.ParentConnection()
	if parentConn == nil || !parentConn.IsConnect() {
		return nil, fmt.Errorf("parent connection not available")
	}

	if len(sig) != 65 {
		return nil, fmt.Errorf("invalid signature length: %d (expected 65 bytes)", len(sig))
	}

	reqProto := &pb.RegisterAccountRequest{
		Address:   addr.Bytes(),
		Signature: sig,
	}
	reqBody, err := proto.Marshal(reqProto)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RegisterAccountRequest: %w", err)
	}

	id := uuid.New().String()
	respCh := make(chan t_network.Message, 2)
	client.pendingChainRequests.Store(id, respCh)
	defer client.pendingChainRequests.Delete(id)

	msg := p_network.NewMessage(&pb.Message{
		Header: &pb.Header{
			Command: command.RegisterAccount,
			ID:      id,
		},
		Body: reqBody,
	})

	if err := parentConn.SendMessage(msg); err != nil {
		return nil, fmt.Errorf("failed to send %s: %w", command.RegisterAccount, err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return nil, fmt.Errorf("timeout (%s) waiting for registration confirmation for %s", timeout, addr.Hex())
		case resp := <-respCh:
			cmd := resp.Command()
			switch cmd {
			case command.RegisterAccountAck:
				var ackRes pb.RegistrationResult
				if err := proto.Unmarshal(resp.Body(), &ackRes); err == nil {
					if ackRes.Status == "FAILED" || ackRes.Status == "REJECTED" {
						return nil, fmt.Errorf("node registration error: %s (%s)", ackRes.Status, ackRes.Reason)
					}
					if ackRes.Status == "CONFIRMED" {
						return &ackRes, nil
					}
				} else {
					ackStr := string(resp.Body())
					if strings.HasPrefix(ackStr, "ERROR") {
						return nil, fmt.Errorf("node rejected registration: %s", ackStr)
					}
					if ackStr == "CONFIRMED" {
						return &pb.RegistrationResult{
							Status:  "CONFIRMED",
							Address: addr.Hex(),
						}, nil
					}
				}
				// "PENDING": continue waiting for push notification (RegistrationResult)
			case command.RegistrationResult:
				var info pb.RegistrationResult
				if err := proto.Unmarshal(resp.Body(), &info); err != nil {
					return nil, fmt.Errorf("failed to unmarshal RegistrationResult proto: %w", err)
				}
				return &info, nil
			default:
				logger.Warn("ChainRegisterAccountWait: unexpected command received: %s", cmd)
			}
		}
	}
}
