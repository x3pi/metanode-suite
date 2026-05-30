package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

const rpcURL = "http://127.0.0.1:8545"

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

type verifyResult struct {
	TargetBlock   uint64 `json:"targetBlock"`
	ExpectedRoot  string `json:"expectedRoot"`
	RecoveredRoot string `json:"recoveredRoot"`
	Match         bool   `json:"match"`
	NumEntries    int    `json:"num_entries"`
}

func main() {
	// Lấy block number mới nhất
	latestBlock, err := getLatestBlockNumber()
	if err != nil {
		fmt.Printf("Lỗi khi lấy block number hiện tại: %v\n", err)
		return
	}
	fmt.Printf("Bắt đầu verify từ block 1 đến block %d...\n", latestBlock)

	successCount := 0
	failCount := 0

	for i := uint64(1); i <= latestBlock; i++ {
		fmt.Printf("Đang kiểm tra block %d... ", i)
		res, err := verifyBlock(i)
		if err != nil {
			fmt.Printf("Lỗi RPC: %v\n", err)
			failCount++
			continue
		}

		if res.Match {
			fmt.Printf("✅ OK (Root: %s)\n", res.ExpectedRoot[:10]+"...")
			successCount++
		} else {
			fmt.Printf("❌ SAI LỆCH!\n")
			fmt.Printf("   Expected : %s\n", res.ExpectedRoot)
			fmt.Printf("   Recovered: %s (Entries: %v)\n", res.RecoveredRoot, res.NumEntries)
			failCount++
		}
	}

	fmt.Printf("\n=== HOÀN THÀNH ===\n")
	fmt.Printf("Tổng số block kiểm tra: %d\n", latestBlock)
	fmt.Printf("Thành công: %d\n", successCount)
	fmt.Printf("Thất bại: %d\n", failCount)
}

func getLatestBlockNumber() (uint64, error) {
	reqData := rpcRequest{
		JSONRPC: "2.0",
		Method:  "eth_blockNumber", // API chuẩn lấy block mới nhất
		Params:  []interface{}{},
		ID:      1,
	}
	body, err := json.Marshal(reqData)
	if err != nil {
		return 0, err
	}

	resp, err := http.Post(rpcURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var rpcResp rpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return 0, err
	}
	if rpcResp.Error != nil {
		return 0, fmt.Errorf(rpcResp.Error.Message)
	}

	var hexStr string
	if err := json.Unmarshal(rpcResp.Result, &hexStr); err != nil {
		return 0, err
	}

	// Xử lý chuỗi hex (bỏ "0x" nếu có)
	if len(hexStr) >= 2 && hexStr[:2] == "0x" {
		hexStr = hexStr[2:]
	}
	num, err := strconv.ParseUint(hexStr, 16, 64)
	return num, err
}

func verifyBlock(blockNumber uint64) (*verifyResult, error) {
	reqData := rpcRequest{
		JSONRPC: "2.0",
		Method:  "eth_verifyHistoricalRoot",
		Params:  []interface{}{fmt.Sprintf("0x%x", blockNumber)},
		ID:      2,
	}
	body, err := json.Marshal(reqData)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(rpcURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var rpcResp rpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, err
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf(rpcResp.Error.Message)
	}

	var vRes verifyResult
	if err := json.Unmarshal(rpcResp.Result, &vRes); err != nil {
		return nil, err
	}

	return &vRes, nil
}
