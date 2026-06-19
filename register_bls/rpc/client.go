package rpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type RPCClient struct {
	Endpoint string
	client   *http.Client
}

func NewRPCClient(endpoint string) *RPCClient {
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "http://" + endpoint
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxConnsPerHost = 100
	t.MaxIdleConnsPerHost = 100

	return &RPCClient{
		Endpoint: endpoint,
		client: &http.Client{
			Transport: t,
			Timeout:   30 * time.Second,
		},
	}
}

func (c *RPCClient) SetTimeout(d time.Duration) {
	c.client.Timeout = d
}

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *RPCClient) call(method string, params ...interface{}) ([]byte, error) {
	reqBody := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	maxRetries := 5
	baseDelay := 50 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		resp, err := c.client.Post(c.Endpoint, "application/json", bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("rpc request failed: %v", err)
		}

		if resp.StatusCode == 429 {
			resp.Body.Close()
			time.Sleep(baseDelay)
			baseDelay *= 2
			continue
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("rpc request returned status: %d", resp.StatusCode)
		}

		var rpcResp rpcResponse
		if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
			return nil, fmt.Errorf("failed to decode response: %v", err)
		}

		if rpcResp.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
		}

		return rpcResp.Result, nil
	}

	return nil, fmt.Errorf("rpc request exceeded max retries for 429 Too Many Requests")
}

func (c *RPCClient) GetNonce(address string) (uint64, error) {
	result, err := c.call("mtn_getAccountState", address, "pending")
	if err != nil {
		return 0, err
	}
	if string(result) == "null" {
		return 0, nil
	}

	var state struct {
		Nonce int `json:"nonce"`
	}
	if err := json.Unmarshal(result, &state); err != nil {
		// Fallback to eth_getTransactionCount if mtn_getAccountState fails or not available
		res2, err2 := c.call("eth_getTransactionCount", address, "pending")
		if err2 != nil {
			return 0, err2
		}
		var hexStr string
		if err := json.Unmarshal(res2, &hexStr); err == nil {
			hexStr = strings.TrimPrefix(hexStr, "0x")
			n, _ := strconv.ParseUint(hexStr, 16, 64)
			return n, nil
		}
		return 0, fmt.Errorf("failed to get nonce")
	}
	return uint64(state.Nonce), nil
}

func (c *RPCClient) SendRawTransaction(rawTxHex string) (string, error) {
	result, err := c.call("eth_sendRawTransaction", rawTxHex)
	if err != nil {
		return "", err
	}
	var txHash string
	if err := json.Unmarshal(result, &txHash); err != nil {
		return "", fmt.Errorf("failed to unmarshal txHash: %v", err)
	}
	return txHash, nil
}

// Receipt struct
type Receipt struct {
	TransactionHash string `json:"transactionHash"`
	Status          string `json:"status"` // "0x1" or "0x0"
	GasUsed         string `json:"gasUsed"`
}

func (c *RPCClient) GetTransactionReceipt(txHash string) (*Receipt, error) {
	result, err := c.call("eth_getTransactionReceipt", txHash)
	if err != nil {
		return nil, err
	}
	if string(result) == "null" {
		return nil, nil // Pending
	}
	var rcpt Receipt
	if err := json.Unmarshal(result, &rcpt); err != nil {
		return nil, fmt.Errorf("failed to unmarshal receipt: %v", err)
	}
	return &rcpt, nil
}

type CallArgs struct {
To   string `json:"to"`
Data string `json:"data"`
}

func (c *RPCClient) EthCall(to string, data string) ([]byte, error) {
args := CallArgs{
To:   to,
Data: data,
}
result, err := c.call("eth_call", args, "latest")
if err != nil {
return nil, err
}
var hexStr string
if err := json.Unmarshal(result, &hexStr); err != nil {
return nil, err
}
hexStr = strings.TrimPrefix(hexStr, "0x")
return hex.DecodeString(hexStr)
}
