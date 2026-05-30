package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Nodes map[string]string `json:"nodes"`
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

type verifyResult struct {
	TargetBlock   uint64 `json:"targetBlock"`
	ExpectedRoot  string `json:"expectedRoot"`
	RecoveredRoot string `json:"recoveredRoot"`
	Match         bool   `json:"match"`
	FastPath      bool   `json:"fast_path"`
	NumEntries    int    `json:"num_entries"`
}

func main() {
	// Read config
	configData, err := os.ReadFile("config.json")
	if err != nil {
		fmt.Printf("Lỗi đọc config.json: %v\n", err)
		return
	}

	var config Config
	if err := json.Unmarshal(configData, &config); err != nil {
		fmt.Printf("Lỗi parse config.json: %v\n", err)
		return
	}

	// Sort node names for deterministic printing
	var nodeNames []string
	for name := range config.Nodes {
		nodeNames = append(nodeNames, name)
	}
	sort.Strings(nodeNames)

	fmt.Printf("🚀 Khởi động Verify Root Multi-Node Watchdog\n")
	fmt.Printf("Nodes: %v\n\n", nodeNames)

	currentBlock := uint64(1)
	client := &http.Client{Timeout: 10 * time.Second}
	waitForRestart := false

	for {
		// Get highest max block across all nodes
		maxLatest := uint64(0)
		nodesOnline := 0
		for _, name := range nodeNames {
			url := config.Nodes[name]
			latest, err := getLatestBlockNumber(client, url)
			if err == nil {
				nodesOnline++
				if latest > maxLatest {
					maxLatest = latest
				}
			}
		}

		if waitForRestart || nodesOnline == 0 {
			if nodesOnline == len(nodeNames) {
				currentBlock = maxLatest
				waitForRestart = false
				fmt.Printf("\n✅ CÁC NODE ĐÃ ONLINE TRỞ LẠI. RESUME VERIFY TỪ BLOCK MỚI NHẤT: %d\n", currentBlock)
			} else {
				nowStr := time.Now().Format("15:04:05")
				sb := strings.Builder{}
				sb.WriteString(fmt.Sprintf("[%s] Block %-6d | ", nowStr, currentBlock))
				for _, name := range nodeNames {
					url := config.Nodes[name]
					_, err := getLatestBlockNumber(client, url)
					if err != nil {
						sb.WriteString(fmt.Sprintf("%s: ⚠️ ERR | ", name))
					} else {
						sb.WriteString(fmt.Sprintf("%s: ✅ OK | ", name))
					}
				}
				sb.WriteString("❌ đang chờ các node online trở lại...")
				fmt.Println(sb.String())

				// Tự động bật cờ chờ restart nếu tất cả đều chết
				waitForRestart = true

				time.Sleep(2 * time.Second)
				continue
			}
		}

		if maxLatest == 0 || currentBlock > maxLatest {
			// Wait for new blocks
			time.Sleep(5 * time.Second)
			continue
		}

		// Verify current block on all nodes
		sb := strings.Builder{}
		sb.WriteString(fmt.Sprintf("Block %-6d | ", currentBlock))

		allMatched := true
		hasRpcError := false
		var mismatchLogs []string
		var errNodes []string

		for _, name := range nodeNames {
			url := config.Nodes[name]
			res, err := verifyBlock(client, url, currentBlock)
			if err != nil {
				sb.WriteString(fmt.Sprintf("%s: ⚠️ ERR | ", name))
				mismatchLogs = append(mismatchLogs, fmt.Sprintf("%s: RPC Error: %v", name, err))
				errNodes = append(errNodes, name)
				allMatched = false
				hasRpcError = true
			} else if res == nil {
				sb.WriteString(fmt.Sprintf("%s: ⚠️ NULL | ", name))
				mismatchLogs = append(mismatchLogs, fmt.Sprintf("%s: Returned null (block not found?)", name))
				errNodes = append(errNodes, name)
				allMatched = false
			} else if res.Match {
				sb.WriteString(fmt.Sprintf("%s: ✅ OK | ", name))
			} else {
				sb.WriteString(fmt.Sprintf("%s: ❌ LỖI | ", name))
				mismatchLogs = append(mismatchLogs, fmt.Sprintf("%s: MISMATCH! Expected=%s, Recovered=%s (Entries: %d)",
					name, res.ExpectedRoot, res.RecoveredRoot, res.NumEntries))
				allMatched = false
			}
		}

		// Print line
		fmt.Println(sb.String())

		if !allMatched {
			fmt.Printf("\n🚨 PHÁT HIỆN LỖI TẠI BLOCK %d!\n", currentBlock)
			logContent := fmt.Sprintf("=== MISMATCH/ERROR AT BLOCK %d ===\nTime: %s\n", currentBlock, time.Now().Format(time.RFC3339))
			for _, log := range mismatchLogs {
				fmt.Printf("   - %s\n", log)
				logContent += log + "\n"
			}

			// Append to log file
			f, err := os.OpenFile("verify_mismatch.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err == nil {
				f.WriteString(logContent + "\n")
				f.Close()
				fmt.Printf("📄 Đã ghi chi tiết lỗi vào verify_mismatch.log\n")
			}

			if hasRpcError {
				fmt.Printf("🛑 BỎ QUA LỖI KẾT NỐI (Các node lỗi: %s). SẼ CHỜ NODE RESTART VÀ BẮT ĐẦU LẠI TỪ BLOCK MỚI NHẤT...\n", strings.Join(errNodes, ", "))
				waitForRestart = true
			} else {
				fmt.Printf("🛑 DỪNG WATCHDOG ĐỂ KIỂM TRA LỖI MISMATCH.\n")
				os.Exit(1)
			}
		} else {
			// All good, increment block
			currentBlock++
		}
	}
}

func getLatestBlockNumber(client *http.Client, nodeURL string) (uint64, error) {
	reqData := rpcRequest{
		JSONRPC: "2.0",
		Method:  "eth_blockNumber",
		Params:  []interface{}{},
		ID:      1,
	}
	body, err := json.Marshal(reqData)
	if err != nil {
		return 0, err
	}

	resp, err := client.Post(nodeURL, "application/json", bytes.NewReader(body))
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

	if len(hexStr) >= 2 && hexStr[:2] == "0x" {
		hexStr = hexStr[2:]
	}
	num, err := strconv.ParseUint(hexStr, 16, 64)
	return num, err
}

func verifyBlock(client *http.Client, nodeURL string, blockNumber uint64) (*verifyResult, error) {
	reqData := rpcRequest{
		JSONRPC: "2.0",
		Method:  "eth_verifyHistoricalRoot", // API mới của metanode
		Params:  []interface{}{fmt.Sprintf("0x%x", blockNumber)},
		ID:      2,
	}
	body, err := json.Marshal(reqData)
	if err != nil {
		return nil, err
	}

	resp, err := client.Post(nodeURL, "application/json", bytes.NewReader(body))
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

	if string(rpcResp.Result) == "null" {
		return nil, nil
	}

	var vRes verifyResult
	if err := json.Unmarshal(rpcResp.Result, &vRes); err != nil {
		return nil, err
	}

	return &vRes, nil
}
