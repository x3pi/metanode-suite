package client

import (
	"encoding/binary"
	"fmt"
	"math/big"
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
		return transaction.Hash(), nil
	}

	return common.Hash{}, fmt.Errorf("unexpected command: %s", cmd)
}
