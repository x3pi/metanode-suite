package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/rpc"
)

func main() {
	rpcURL := "http://139.59.243.85:8545"
	targetAddress := strings.ToLower("0xbc92a827b94fc293eed47a1d797cdc54ebac9d6b")

	client, err := rpc.Dial(rpcURL)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC: %v", err)
	}

	var latest string
	err = client.CallContext(context.Background(), &latest, "eth_blockNumber")
	if err != nil {
		log.Fatalf("❌ Lỗi lấy block mới nhất: %v", err)
	}

	var latestBlock uint64
	fmt.Sscanf(latest, "0x%x", &latestBlock)
	startBlock := uint64(0)

	fmt.Printf("🔍 Đang quét từ block %d lùi về %d cho giao dịch liên quan đến %s\n", latestBlock, startBlock, targetAddress)

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 50)

	type TxInfo struct {
		Block uint64
		Nonce uint64
		Hash  string
		From  string
		To    string
		Value string
		Gas   string
	}
	var results []TxInfo
	var mu sync.Mutex

	for i := latestBlock; ; i-- {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(blockNum uint64) {
			defer wg.Done()
			defer func() { <-semaphore }()

			var block struct {
				Transactions []map[string]interface{} `json:"transactions"`
			}
			err := client.CallContext(context.Background(), &block, "eth_getBlockByNumber", fmt.Sprintf("0x%x", blockNum), true)
			if err != nil {
				return
			}

			for _, tx := range block.Transactions {
				nonceHex, ok := tx["nonce"].(string)
				if !ok {
					continue
				}

				fromHex, _ := tx["from"].(string)
				toHex, _ := tx["to"].(string)

				if strings.ToLower(fromHex) == targetAddress {
					var nonce uint64
					fmt.Sscanf(nonceHex, "0x%x", &nonce)

					mu.Lock()
					results = append(results, TxInfo{
						Block: blockNum,
						Nonce: nonce,
						Hash:  tx["hash"].(string),
						From:  fromHex,
						To:    toHex,
						Value: tx["value"].(string),
						Gas:   tx["gas"].(string),
					})
					mu.Unlock()
					fmt.Printf("✅ Đã tìm thấy Tx: Nonce %d tại Block %d (Hash: %s, From: %s, To: %s)\n", nonce, blockNum, tx["hash"], fromHex, toHex)
				}
			}
		}(i)

		if i <= startBlock {
			break
		}
	}

	wg.Wait()

	fmt.Println("\n==============================")
	fmt.Printf("🚀 TÌM THẤY %d GIAO DỊCH!\n", len(results))
	for _, tx := range results {
		fmt.Printf("📌 Nonce: %d | Block: %d | Hash: %s\n", tx.Nonce, tx.Block, tx.Hash)
	}
	fmt.Println("==============================")
}
