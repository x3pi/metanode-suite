package main

import (
	"context"
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

func main() {
	blockNum := flag.Int64("block", -1, "Block number to check")
	node1Flag := flag.String("node1", "m0", "Tên node 1 (VD: m0) hoặc RPC URL")
	node2Flag := flag.String("node2", "m3", "Tên node 2 (VD: m3) hoặc RPC URL")
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

	fmt.Printf("Đang so sánh Block %d giữa:\n  Node 1: %s (%s)\n  Node 2: %s (%s)\n", *blockNum, *node1Flag, url1, *node2Flag, url2)

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

	if block1.ReceiptHash() != block2.ReceiptHash() {
		fmt.Printf("❌ Block %d có ReceiptsRoot LỆCH NHAU:\n   Node1: %s\n   Node2: %s\n", *blockNum, block1.ReceiptHash().Hex(), block2.ReceiptHash().Hex())
		fmt.Println("Đang so sánh chi tiết từng Receipt...")
	} else {
		fmt.Printf("✅ Block %d có ReceiptsRoot GIỐNG NHAU: %s\n", *blockNum, block1.ReceiptHash().Hex())
		return
	}

	txs1 := block1.Transactions()
	txs2 := block2.Transactions()
	if len(txs1) != len(txs2) {
		log.Fatalf("Số lượng transaction không khớp! Node1: %d, Node2: %d", len(txs1), len(txs2))
	}

	for i, tx1 := range txs1 {
		tx2 := txs2[i]
		if tx1.Hash() != tx2.Hash() {
			log.Fatalf("Transaction hash tại index %d không khớp! Node1: %s, Node2: %s", i, tx1.Hash().Hex(), tx2.Hash().Hex())
		}

		rcp1, err := client1.TransactionReceipt(context.Background(), tx1.Hash())
		if err != nil {
			log.Fatalf("Lỗi lấy receipt từ node1 cho tx %s: %v", tx1.Hash().Hex(), err)
		}
		rcp2, err := client2.TransactionReceipt(context.Background(), tx2.Hash())
		if err != nil {
			log.Fatalf("Lỗi lấy receipt từ node2 cho tx %s: %v", tx2.Hash().Hex(), err)
		}

		j1, _ := json.Marshal(rcp1)
		j2, _ := json.Marshal(rcp2)

		if string(j1) != string(j2) {
			fmt.Printf("\n🔴 TÌM THẤY RECEIPT KHÁC NHAU TẠI INDEX %d (Tx: %s)\n", i, tx1.Hash().Hex())
			
			var raw1, raw2 map[string]interface{}
			json.Unmarshal(j1, &raw1)
			json.Unmarshal(j2, &raw2)

			fmt.Println("--- Các trường khác nhau ---")
			allKeys := make(map[string]bool)
			for k := range raw1 {
				allKeys[k] = true
			}
			for k := range raw2 {
				allKeys[k] = true
			}
			for k := range allKeys {
				v1, ok1 := raw1[k]
				v2, ok2 := raw2[k]
				if !ok1 {
					fmt.Printf("- %s: %s KHÔNG CÓ, %s = %v\n", k, *node1Flag, *node2Flag, v2)
					continue
				}
				if !ok2 {
					fmt.Printf("- %s: %s = %v, %s KHÔNG CÓ\n", k, *node1Flag, v1, *node2Flag)
					continue
				}
				v1Json, _ := json.Marshal(v1)
				v2Json, _ := json.Marshal(v2)
				if string(v1Json) != string(v2Json) {
					fmt.Printf("- %s:\n    %s: %s\n    %s: %s\n", k, *node1Flag, string(v1Json), *node2Flag, string(v2Json))
				}
			}
		}
	}
	fmt.Println("Hoàn tất so sánh.")
}