package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/ethclient"
)

type ConfigNode struct {
	Nodes map[string]string `json:"nodes"`
}

func loadConfig() (*ConfigNode, error) {
	paths := []string{"config-node.json", "../config-node.json", "../../config-node.json"}
	var configData []byte
	var err error
	for _, p := range paths {
		configData, err = ioutil.ReadFile(p)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("không tìm thấy file config-node.json")
	}
	var config ConfigNode
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func resolveNodeURL(input string, config *ConfigNode) string {
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		return input
	}
	if url, ok := config.Nodes[input]; ok {
		return url
	}
	log.Fatalf("Không tìm thấy URL cho node '%s' trong config-node.json", input)
	return ""
}

// formatLogData parses hex data and formats potential uint256 values as decimal for easy debugging
func formatLogData(data []byte) string {
	hexStr := hex.EncodeToString(data)
	if len(data) >= 32 {
		val := new(big.Int).SetBytes(data[:32])
		return fmt.Sprintf("0x%s (uint256=%s)", hexStr, val.String())
	}
	return "0x" + hexStr
}

func main() {
	blockNum := flag.Int64("block", -1, "Block number to check")
	node1Flag := flag.String("node1", "m0", "Tên node 1 (VD: m0) hoặc RPC URL")
	node2Flag := flag.String("node2", "m2", "Tên node 2 (VD: m2) hoặc RPC URL")
	flag.Parse()

	if *blockNum < 0 {
		log.Fatalf("Vui lòng truyền -block=<number>")
	}

	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Lỗi đọc config: %v", err)
	}

	url1 := resolveNodeURL(*node1Flag, config)
	url2 := resolveNodeURL(*node2Flag, config)

	fmt.Printf("🔍 Đang so sánh Block %d giữa:\n  • Node 1: %s (%s)\n  • Node 2: %s (%s)\n\n", *blockNum, *node1Flag, url1, *node2Flag, url2)

	client1, err := ethclient.Dial(url1)
	if err != nil {
		log.Fatalf("Lỗi kết nối node1: %v", err)
	}

	client2, err := ethclient.Dial(url2)
	if err != nil {
		log.Fatalf("Lỗi kết nối node2: %v", err)
	}

	block1, err := client1.BlockByNumber(context.Background(), big.NewInt(*blockNum))
	if err != nil {
		log.Fatalf("Lỗi lấy block %d từ node1: %v", *blockNum, err)
	}
	block2, err := client2.BlockByNumber(context.Background(), big.NewInt(*blockNum))
	if err != nil {
		log.Fatalf("Lỗi lấy block %d từ node2: %v", *blockNum, err)
	}

	// 1. So sánh Header cơ bản
	fmt.Println("📊 [BLOCK HEADER CHECK]")
	fmt.Printf("   • Block Number      : %d\n", *blockNum)
	fmt.Printf("   • StateRoot Match   : %v (m0: %s | m2: %s)\n", block1.Root() == block2.Root(), block1.Root().Hex(), block2.Root().Hex())
	fmt.Printf("   • TxRoot Match      : %v (m0: %s | m2: %s)\n", block1.TxHash() == block2.TxHash(), block1.TxHash().Hex(), block2.TxHash().Hex())
	fmt.Printf("   • ReceiptsRoot Match: %v\n", block1.ReceiptHash() == block2.ReceiptHash())
	fmt.Printf("     - %s: %s\n", *node1Flag, block1.ReceiptHash().Hex())
	fmt.Printf("     - %s: %s\n", *node2Flag, block2.ReceiptHash().Hex())

	if block1.ReceiptHash() == block2.ReceiptHash() {
		fmt.Printf("\n✅ Block %d có ReceiptsRoot GIỐNG NHAU 100%%. Không có lệch!\n", *blockNum)
		return
	}

	fmt.Println("\n🚨 Phát hiện LỆCH ReceiptsRoot! Tiến hành phân tích sâu từng Receipt...")

	txs1 := block1.Transactions()
	txs2 := block2.Transactions()
	if len(txs1) != len(txs2) {
		log.Fatalf("❌ Số lượng transaction không khớp! Node1: %d, Node2: %d", len(txs1), len(txs2))
	}

	mismatchCount := 0
	firstMismatchIdx := -1

	for i, tx1 := range txs1 {
		tx2 := txs2[i]
		if tx1.Hash() != tx2.Hash() {
			log.Fatalf("❌ Transaction hash tại index %d không khớp! Node1: %s, Node2: %s", i, tx1.Hash().Hex(), tx2.Hash().Hex())
		}

		rcp1, err := client1.TransactionReceipt(context.Background(), tx1.Hash())
		if err != nil {
			log.Fatalf("Lỗi lấy receipt từ node1 cho tx %s: %v", tx1.Hash().Hex(), err)
		}
		rcp2, err := client2.TransactionReceipt(context.Background(), tx2.Hash())
		if err != nil {
			log.Fatalf("Lỗi lấy receipt từ node2 cho tx %s: %v", tx2.Hash().Hex(), err)
		}

		// So sánh các trường thực thi nội tại (BỎ QUA blockHash vì blockHash bị ảnh hưởng bởi receiptsRoot)
		var diffs []string

		if rcp1.Status != rcp2.Status {
			diffs = append(diffs, fmt.Sprintf("Status: %s=%d, %s=%d", *node1Flag, rcp1.Status, *node2Flag, rcp2.Status))
		}
		if rcp1.GasUsed != rcp2.GasUsed {
			diffs = append(diffs, fmt.Sprintf("GasUsed: %s=%d, %s=%d", *node1Flag, rcp1.GasUsed, *node2Flag, rcp2.GasUsed))
		}
		if rcp1.CumulativeGasUsed != rcp2.CumulativeGasUsed {
			diffs = append(diffs, fmt.Sprintf("CumulativeGasUsed: %s=%d, %s=%d", *node1Flag, rcp1.CumulativeGasUsed, *node2Flag, rcp2.CumulativeGasUsed))
		}
		if len(rcp1.Logs) != len(rcp2.Logs) {
			diffs = append(diffs, fmt.Sprintf("LogsCount: %s=%d, %s=%d", *node1Flag, len(rcp1.Logs), *node2Flag, len(rcp2.Logs)))
		} else {
			for lIdx, l1 := range rcp1.Logs {
				l2 := rcp2.Logs[lIdx]
				if l1.Address != l2.Address {
					diffs = append(diffs, fmt.Sprintf("Log[%d].Address: %s=%s, %s=%s", lIdx, *node1Flag, l1.Address.Hex(), *node2Flag, l2.Address.Hex()))
				}
				if len(l1.Topics) != len(l2.Topics) {
					diffs = append(diffs, fmt.Sprintf("Log[%d].TopicsCount: %s=%d, %s=%d", lIdx, *node1Flag, len(l1.Topics), *node2Flag, len(l2.Topics)))
				} else {
					for tIdx, t1 := range l1.Topics {
						t2 := l2.Topics[tIdx]
						if t1 != t2 {
							diffs = append(diffs, fmt.Sprintf("Log[%d].Topic[%d]: %s=%s, %s=%s", lIdx, tIdx, *node1Flag, t1.Hex(), *node2Flag, t2.Hex()))
						}
					}
				}
				if string(l1.Data) != string(l2.Data) {
					diffs = append(diffs, fmt.Sprintf("Log[%d].Data:\n      %s: %s\n      %s: %s", lIdx, *node1Flag, formatLogData(l1.Data), *node2Flag, formatLogData(l2.Data)))
				}
			}
		}

		if len(diffs) > 0 {
			mismatchCount++
			if firstMismatchIdx == -1 {
				firstMismatchIdx = i
			}
			fmt.Printf("\n🔴 [RECEIPT MISMATCH #%d] Tx Index: %d | Hash: %s\n", mismatchCount, i, tx1.Hash().Hex())
			for _, d := range diffs {
				fmt.Printf("   ⚠️  %s\n", d)
			}
		}
	}

	fmt.Println("\n========================================================")
	fmt.Printf("📊 [TỔNG KẾT SO SÁNH BLOCK %d]\n", *blockNum)
	fmt.Printf("   • Tổng số transactions : %d\n", len(txs1))
	fmt.Printf("   • Số receipts bị lệch  : %d / %d\n", mismatchCount, len(txs1))
	if firstMismatchIdx != -1 {
		fmt.Printf("   • Mismatch đầu tiên tại: Index %d (Tx: %s)\n", firstMismatchIdx, txs1[firstMismatchIdx].Hash().Hex())
	}
	fmt.Println("========================================================")
}