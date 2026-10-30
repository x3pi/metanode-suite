package common

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// RandomAddress generates a random 20-byte Ethereum-compatible address
func RandomAddress() common.Address {
	var b [20]byte
	_, _ = rand.Read(b[:])
	return common.BytesToAddress(b[:])
}

var defaultHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
}

// JSONRPCRequest represents a standard JSON-RPC 2.0 payload
type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

// JSONRPCResponse represents a standard JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	ID int `json:"id"`
}

// CallJSONRPC sends a JSON-RPC request to the specified endpoint
func CallJSONRPC(endpoint, method string, params []interface{}) (json.RawMessage, error) {
	reqBody := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request failed: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("new request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post to %s failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response (%s) failed: %w", string(respBytes), err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error [%d]: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// CheckParentNode queries the Parent Chain's /status endpoint to evaluate its health
func CheckParentNode(url string) ParentNodeStatus {
	st := ParentNodeStatus{
		URL: url,
	}
	cleanURL := strings.TrimRight(url, "/")

	start := time.Now()
	resp, err := defaultHTTPClient.Get(cleanURL + "/status")
	st.LatencyMs = time.Since(start).Milliseconds()

	if err != nil {
		st.Online = false
		st.Error = err.Error()
		return st
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		st.Online = false
		st.Error = fmt.Sprintf("status code %d", resp.StatusCode)
		return st
	}

	var payload struct {
		Height       uint64 `json:"height"`
		LastBlock    uint64 `json:"last_block"`
		Syncing      bool   `json:"syncing"`
		ForkDetected bool   `json:"fork_detected"`
		StateRoot    string `json:"state_root"`
		ChainID      uint64 `json:"chain_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		st.Online = false
		st.Error = fmt.Sprintf("decode status: %v", err)
		return st
	}

	st.Online = true
	st.Height = payload.Height
	if payload.LastBlock > 0 || st.Height == 0 {
		st.Height = payload.LastBlock
	}
	st.Syncing = payload.Syncing
	st.ForkDetected = payload.ForkDetected
	st.StateRoot = payload.StateRoot
	st.ChainID = payload.ChainID
	return st
}

// GetAccountBalance queries mtn_getAccountState on an Execution Cluster
func GetAccountBalance(endpoint string, addr common.Address) (*big.Int, error) {
	raw, err := CallJSONRPC(endpoint, "mtn_getAccountState", []interface{}{addr.Hex(), "latest"})
	if err != nil {
		// Fallback to standard eth_getBalance if mtn_getAccountState is not supported
		if rawEth, errEth := CallJSONRPC(endpoint, "eth_getBalance", []interface{}{addr.Hex(), "latest"}); errEth == nil {
			var hexStr string
			if json.Unmarshal(rawEth, &hexStr) == nil {
				return hexutil.DecodeBig(hexStr)
			}
		}
		return nil, err
	}
	var state struct {
		Balance string `json:"balance"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("unmarshal account state: %w", err)
	}
	if state.Balance == "" {
		return big.NewInt(0), nil
	}
	val, ok := new(big.Int).SetString(state.Balance, 10)
	if !ok {
		if strings.HasPrefix(state.Balance, "0x") {
			v, err := hexutil.DecodeBig(state.Balance)
			if err == nil {
				return v, nil
			}
		}
		return nil, fmt.Errorf("cannot parse balance string: %s", state.Balance)
	}
	return val, nil
}

// SendCrossChainTransfer executes mtn_sendCrossChainTransfer on an Execution Cluster
// Supports optional senderKeyHex override to avoid nonce contention or supply funded keys
func SendCrossChainTransfer(sourceEndpoint string, targetAddr common.Address, amount *big.Int, senderKeyHex ...string) (string, error) {
	amountHex := hexutil.EncodeBig(amount)
	params := []interface{}{targetAddr.Hex(), amountHex}
	if len(senderKeyHex) > 0 && strings.TrimSpace(senderKeyHex[0]) != "" {
		params = append(params, strings.TrimPrefix(strings.TrimSpace(senderKeyHex[0]), "0x"))
	}
	if len(senderKeyHex) > 1 && strings.TrimSpace(senderKeyHex[1]) != "" {
		params = append(params, strings.TrimPrefix(strings.TrimSpace(senderKeyHex[1]), "0x"))
	}
	raw, err := CallJSONRPC(sourceEndpoint, "mtn_sendCrossChainTransfer", params)
	if err != nil {
		return "", err
	}
	var txHash string
	if err := json.Unmarshal(raw, &txHash); err != nil {
		var resMap map[string]interface{}
		if errMap := json.Unmarshal(raw, &resMap); errMap == nil {
			if h, ok := resMap["txHash"].(string); ok {
				return h, nil
			}
			if h, ok := resMap["result"].(string); ok {
				return h, nil
			}
		}
		return string(raw), nil
	}
	return txHash, nil
}

// GetConservation queries mtn_getConservation on an Execution Cluster
func GetConservation(endpoint string) (map[string]interface{}, error) {
	raw, err := CallJSONRPC(endpoint, "mtn_getConservation", []interface{}{})
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshal conservation: %w", err)
	}
	return result, nil
}

// GetBlockNumber queries eth_blockNumber on an Execution Cluster or Parent Chain
func GetBlockNumber(endpoint string) (uint64, error) {
	raw, err := CallJSONRPC(endpoint, "eth_blockNumber", []interface{}{})
	if err != nil {
		return 0, err
	}
	var hexStr string
	if err := json.Unmarshal(raw, &hexStr); err != nil {
		return 0, fmt.Errorf("unmarshal blockNumber: %w", err)
	}
	return hexutil.DecodeUint64(hexStr)
}

// GetNonce queries eth_getTransactionCount for an address
func GetNonce(endpoint string, addr common.Address) (uint64, error) {
	raw, err := CallJSONRPC(endpoint, "eth_getTransactionCount", []interface{}{addr.Hex(), "pending"})
	if err != nil {
		return 0, err
	}
	var hexStr string
	if err := json.Unmarshal(raw, &hexStr); err != nil {
		return 0, fmt.Errorf("unmarshal nonce: %w", err)
	}
	return hexutil.DecodeUint64(hexStr)
}

// DeployContract signs and deploys a smart contract to the target endpoint
func DeployContract(endpoint string, privKeyHex string, bytecodeHex string) (common.Address, common.Hash, error) {
	privKey, err := crypto.HexToECDSA(strings.TrimPrefix(privKeyHex, "0x"))
	if err != nil {
		return common.Address{}, common.Hash{}, fmt.Errorf("invalid private key: %w", err)
	}
	sender := crypto.PubkeyToAddress(privKey.PublicKey)

	nonce, err := GetNonce(endpoint, sender)
	if err != nil {
		return common.Address{}, common.Hash{}, fmt.Errorf("get nonce failed: %w", err)
	}

	cleanedBytecode := strings.TrimSpace(bytecodeHex)
	if !strings.HasPrefix(cleanedBytecode, "0x") {
		cleanedBytecode = "0x" + cleanedBytecode
	}
	rawBytecode, err := hexutil.Decode(cleanedBytecode)
	if err != nil {
		return common.Address{}, common.Hash{}, fmt.Errorf("invalid bytecode hex: %w", err)
	}

	chainID := big.NewInt(991)
	gasLimit := uint64(3000000)
	gasPrice := big.NewInt(1000000000) // 1 gwei

	tx := types.NewContractCreation(nonce, big.NewInt(0), gasLimit, gasPrice, rawBytecode)
	signer := types.NewCancunSigner(chainID)
	signedTx, err := types.SignTx(tx, signer, privKey)
	if err != nil {
		return common.Address{}, common.Hash{}, fmt.Errorf("sign tx failed: %w", err)
	}

	rawBytes, err := signedTx.MarshalBinary()
	if err != nil {
		return common.Address{}, common.Hash{}, fmt.Errorf("marshal binary failed: %w", err)
	}

	res, err := CallJSONRPC(endpoint, "eth_sendRawTransaction", []interface{}{hexutil.Encode(rawBytes)})
	if err != nil {
		return common.Address{}, common.Hash{}, fmt.Errorf("send raw tx failed: %w", err)
	}

	var txHashStr string
	_ = json.Unmarshal(res, &txHashStr)
	contractAddr := crypto.CreateAddress(sender, nonce)
	return contractAddr, signedTx.Hash(), nil
}

// CallContract signs and broadcasts a transaction invoking a contract function
func CallContract(endpoint string, privKeyHex string, contractAddr common.Address, callData []byte) (common.Hash, error) {
	privKey, err := crypto.HexToECDSA(strings.TrimPrefix(privKeyHex, "0x"))
	if err != nil {
		return common.Hash{}, fmt.Errorf("invalid private key: %w", err)
	}
	sender := crypto.PubkeyToAddress(privKey.PublicKey)

	nonce, err := GetNonce(endpoint, sender)
	if err != nil {
		return common.Hash{}, fmt.Errorf("get nonce failed: %w", err)
	}

	chainID := big.NewInt(991)
	gasLimit := uint64(500000)
	gasPrice := big.NewInt(1000000000) // 1 gwei

	tx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), gasLimit, gasPrice, callData)
	signer := types.NewCancunSigner(chainID)
	signedTx, err := types.SignTx(tx, signer, privKey)
	if err != nil {
		return common.Hash{}, fmt.Errorf("sign tx failed: %w", err)
	}

	rawBytes, err := signedTx.MarshalBinary()
	if err != nil {
		return common.Hash{}, fmt.Errorf("marshal binary failed: %w", err)
	}

	res, err := CallJSONRPC(endpoint, "eth_sendRawTransaction", []interface{}{hexutil.Encode(rawBytes)})
	if err != nil {
		return common.Hash{}, fmt.Errorf("send raw tx failed: %w", err)
	}

	var txHashStr string
	_ = json.Unmarshal(res, &txHashStr)
	return signedTx.Hash(), nil
}

// EthCall performs a read-only eth_call against the target contract
func EthCall(endpoint string, contractAddr common.Address, callData []byte) (string, error) {
	callObj := map[string]string{
		"to":   contractAddr.Hex(),
		"data": hexutil.Encode(callData),
	}
	res, err := CallJSONRPC(endpoint, "eth_call", []interface{}{callObj, "latest"})
	if err != nil {
		return "", err
	}
	var out string
	_ = json.Unmarshal(res, &out)
	return out, nil
}

// WaitForReceipt polls eth_getTransactionReceipt until confirmed or timed out
func WaitForReceipt(ctx context.Context, endpoint string, txHash common.Hash, timeout time.Duration) (map[string]interface{}, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		res, err := CallJSONRPC(endpoint, "eth_getTransactionReceipt", []interface{}{txHash.Hex()})
		if err == nil && len(res) > 0 && string(res) != "null" {
			var receipt map[string]interface{}
			if err := json.Unmarshal(res, &receipt); err == nil && receipt != nil {
				return receipt, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("timeout waiting for receipt %s", txHash.Hex())
}
