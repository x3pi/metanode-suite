package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
)

type EthBlockResponse struct {
	Result *struct {
		Hash       string `json:"hash"`
		ParentHash string `json:"parentHash"`
		Number     string `json:"number"`
		StateRoot  string `json:"stateRoot"`
	} `json:"result"`
}

func getBlock(nodeURL string, blockNum uint64) (*EthBlockResponse, error) {
	reqBody := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["0x%x", false],"id":1}`, blockNum)
	resp, err := http.Post(nodeURL, "application/json", bytes.NewBuffer([]byte(reqBody)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var ethResp EthBlockResponse
	if err := json.Unmarshal(body, &ethResp); err != nil {
		return nil, err
	}
	return &ethResp, nil
}

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: go run check_parent_hashes.go <nodeURL> <startBlockNum> <count> [targetHash]")
		return
	}
	nodeURL := os.Args[1]
	startBlockNum, _ := strconv.ParseUint(os.Args[2], 10, 64)
	count, _ := strconv.ParseUint(os.Args[3], 10, 64)
	targetHash := ""
	if len(os.Args) >= 5 {
		targetHash = os.Args[4]
	}

	fmt.Printf("Tracing backward from block %d on %s for %d blocks\n\n", startBlockNum, nodeURL, count)
	if targetHash != "" {
		fmt.Printf("🎯 Target Hash to find: %s\n\n", targetHash)
	}
	fmt.Printf("%-10s | %-66s | %-66s | %-66s\n", "Block", "Hash", "ParentHash", "StateRoot")
	fmt.Printf("------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------\n")

	for i := uint64(0); i < count; i++ {
		currentNum := startBlockNum - i
		if currentNum == 0 {
			break
		}

		res, err := getBlock(nodeURL, currentNum)
		if err != nil {
			fmt.Printf("%-10d | ERROR: %v\n", currentNum, err)
			continue
		}

		if res.Result == nil {
			fmt.Printf("%-10d | NOT FOUND (Result is nil)\n", currentNum)
			continue
		}

		matchStr := ""
		if targetHash != "" {
			if res.Result.Hash == targetHash {
				matchStr = " <=== 🚨 MATCHED HASH!"
			} else if res.Result.ParentHash == targetHash {
				matchStr = " <=== 🚨 MATCHED PARENT!"
			}
		}

		fmt.Printf("%-10d | %-66s | %-66s | %-66s%s\n", currentNum, res.Result.Hash, res.Result.ParentHash, res.Result.StateRoot, matchStr)
	}
}
