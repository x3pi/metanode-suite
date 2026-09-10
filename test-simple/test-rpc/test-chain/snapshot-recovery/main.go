package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/config"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type NodeClient struct {
	Name   string
	URL    string
	Client *ethclient.Client
	Online bool
}

type SnapshotItem struct {
	SnapshotName string `json:"snapshot_name"`
	Epoch        uint64 `json:"epoch"`
	BlockNumber  uint64 `json:"block_number"`
	CreatedAt    string `json:"created_at"`
	TotalSizeStr string `json:"total_size_str,omitempty"`
}

func dialClient(url string) (*ethclient.Client, error) {
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 50,
			IdleConnTimeout:     30 * time.Second,
		},
	}
	rpcClient, err := rpc.DialHTTPWithClient(url, httpClient)
	if err != nil {
		return nil, err
	}
	return ethclient.NewClient(rpcClient), nil
}

func matchNodeName(name string, filter string) bool {
	if filter == "" {
		return false
	}
	cleanName := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(name, "m"), "node"))
	for _, part := range strings.Split(filter, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		cleanFilter := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(part, "m"), "node"))
		if cleanName == cleanFilter || strings.EqualFold(name, part) {
			return true
		}
	}
	return false
}

func isNodeExcluded(name string, stopped string, exclude string) bool {
	return matchNodeName(name, stopped) || matchNodeName(name, exclude)
}

func querySnapshotAPI(url string) ([]SnapshotItem, error) {
	apiURL := strings.TrimRight(url, "/") + "/api/snapshots"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var items []SnapshotItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func main() {
	configPath := flag.String("config", "../config.json", "Đường dẫn file config.json")
	txCount := flag.Int("count", 15, "Số lượng giao dịch cần gửi và xác nhận")
	checkFork := flag.Bool("check-fork", true, "Kiểm tra Block Hash & StateRoot giữa các node")
	requireAllAlive := flag.Bool("require-all-alive", true, "Bắt buộc các node còn lại phải sống")
	stoppedNode := flag.String("stopped-node", "", "Node dự kiến đang tắt hoặc snapshot (ngoại lệ kiểm tra sống)")
	excludeNodes := flag.String("exclude-nodes", "", "Danh sách node ngoại lệ kiểm tra sống (ví dụ: '1,4')")
	targetNodeFlag := flag.String("target-node", "", "Chỉ định gửi giao dịch qua riêng node này (vd: '1' hoặc 'm1')")
	minBlocks := flag.Int("min-blocks", 0, "Đảm bảo block height của cụm đạt tối thiểu giá trị này (sẽ tự động gửi tx để kích block)")
	snapshotURL := flag.String("snapshot-url", "", "URL Snapshot Server để kiểm tra (vd: http://192.168.1.234:8600)")
	waitSyncNode := flag.String("wait-sync-node", "", "Chờ node này đồng bộ bắt kịp block height của cụm")
	maxLag := flag.Int("max-lag", 5, "Khoảng chênh lệch block tối đa cho phép khi chờ đồng bộ")
	flag.Parse()

	// 1. Kiểm tra Snapshot Server nếu được yêu cầu
	if *snapshotURL != "" {
		fmt.Printf("🔍 Đang truy vấn Snapshot Server tại %s/api/snapshots...\n", strings.TrimRight(*snapshotURL, "/"))
		items, err := querySnapshotAPI(*snapshotURL)
		if err != nil {
			log.Fatalf("❌ Không thể kết nối tới Snapshot Server: %v", err)
		}
		if len(items) == 0 {
			fmt.Println("⚪ Chưa có bản snapshot nào trên Snapshot Server.")
		} else {
			fmt.Printf("📸 Tìm thấy %d bản snapshot sẵn sàng:\n", len(items))
			for idx, item := range items {
				fmt.Printf("   [%d] Name: %s | Epoch: %d | Block: %d\n",
					idx+1, item.SnapshotName, item.Epoch, item.BlockNumber)
			}
		}
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi load config: %v", err)
	}

	if len(cfg.PrivateKeys) == 0 {
		log.Fatalf("❌ Không có private keys trong config")
	}

	// 2. Khởi tạo danh sách các node RPC (Gộp cả RPCNodes và SyncNodes để kiểm tra toàn bộ 5 node cụm)
	urlMap := make(map[string]string)
	for name, url := range cfg.RPCNodes {
		urlMap[name] = url
	}
	for name, url := range cfg.SyncNodes {
		urlMap[name] = url
	}
	if len(urlMap) == 0 && cfg.RPCUrl != "" {
		urlMap["m0"] = cfg.RPCUrl
	}

	var nodeKeys []string
	for k := range urlMap {
		nodeKeys = append(nodeKeys, k)
	}
	sort.Strings(nodeKeys)

	var nodes []*NodeClient
	for _, name := range nodeKeys {
		url := urlMap[name]
		c, err := dialClient(url)
		online := false
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, bErr := c.BlockNumber(ctx)
			cancel()
			if bErr == nil {
				online = true
			}
		}
		nodes = append(nodes, &NodeClient{
			Name:   name,
			URL:    url,
			Client: c,
			Online: online,
		})
	}

	var activeNodes []*NodeClient
	for _, n := range nodes {
		if n.Online {
			if isNodeExcluded(n.Name, *stoppedNode, *excludeNodes) {
				continue
			}
			activeNodes = append(activeNodes, n)
		}
	}

	// txSendNodes: Ưu tiên gửi qua các Validator RPC nodes đang online
	var txSendNodes []*NodeClient
	for _, n := range activeNodes {
		if _, isVal := cfg.RPCNodes[n.Name]; isVal || len(cfg.RPCNodes) == 0 {
			txSendNodes = append(txSendNodes, n)
		}
	}
	if len(txSendNodes) == 0 {
		txSendNodes = activeNodes
	}

	// Lọc target-node nếu có chỉ định (chỉ cho phép Validator nodes, loại trừ Snapshot / SyncOnly nodes)
	if *targetNodeFlag != "" {
		cleanTarget := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(*targetNodeFlag), "m"), "node")
		for syncKey := range cfg.SyncNodes {
			cleanSync := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(syncKey), "m"), "node")
			if cleanTarget == cleanSync || *targetNodeFlag == syncKey {
				log.Fatalf("❌ LỖI AN TOÀN: Node %s là Node Snapshot / SyncOnly! Không được tự khôi phục chính node snapshot, chỉ dùng các node Validator!", *targetNodeFlag)
			}
		}

		var filtered []*NodeClient
		for _, n := range activeNodes {
			if matchNodeName(n.Name, *targetNodeFlag) {
				filtered = append(filtered, n)
			}
		}
		if len(filtered) > 0 {
			txSendNodes = filtered
			fmt.Printf("🎯 Chỉ định gửi giao dịch qua duy nhất Node %s (%s)\n", txSendNodes[0].Name, txSendNodes[0].URL)
		} else {
			log.Fatalf("❌ Node chỉ định %s không online!", *targetNodeFlag)
		}
	}

	if len(txSendNodes) == 0 {
		log.Fatalf("❌ Không có node nào online để thực hiện bài test!")
	}

	// 3. Chuẩn bị tài khoản và Nonce
	type AccountState struct {
		Key     *ecdsa.PrivateKey
		Address common.Address
		Nonce   uint64
		Mu      sync.Mutex
	}

	var accounts []*AccountState
	for _, kStr := range cfg.PrivateKeys {
		pk, err := crypto.HexToECDSA(strings.TrimPrefix(kStr, "0x"))
		if err == nil {
			addr := crypto.PubkeyToAddress(pk.PublicKey)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			n, nErr := txSendNodes[0].Client.PendingNonceAt(ctx, addr)
			cancel()
			if nErr == nil {
				accounts = append(accounts, &AccountState{
					Key:     pk,
					Address: addr,
					Nonce:   n,
				})
			}
		}
	}
	if len(accounts) == 0 {
		log.Fatalf("❌ Không thể khởi tạo tài khoản hợp lệ nào với nonce")
	}

	sendBatch := func(count int) int {
		receiverAddr := common.HexToAddress("0x0000000000000000000000000000000000000088")
		chainID := big.NewInt(cfg.ChainID)

		type TxRecord struct {
			Index    int
			NodeName string
			TxHash   common.Hash
			Client   *ethclient.Client
			BlockNum uint64
			Status   bool
		}

		var records []*TxRecord
		var recordsMu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 5)

		for i := 0; i < count; i++ {
			wg.Add(1)
			go func(txIdx int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				acct := accounts[txIdx%len(accounts)]
				acct.Mu.Lock()
				nonce := acct.Nonce
				acct.Nonce++
				acct.Mu.Unlock()

				targetNode := txSendNodes[txIdx%len(txSendNodes)]
				amount := big.NewInt(int64(1000 + txIdx))
				gasLimit := uint64(21000)
				gasPrice := big.NewInt(1000000000)

				tx := types.NewTransaction(nonce, receiverAddr, amount, gasLimit, gasPrice, nil)
				signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), acct.Key)
				if err != nil {
					fmt.Printf("   [TX %d/%d] ⚠️ Lỗi ký tx: %v\n", txIdx+1, count, err)
					return
				}

				ctxSend, cancelSend := context.WithTimeout(context.Background(), 4*time.Second)
				err = targetNode.Client.SendTransaction(ctxSend, signedTx)
				cancelSend()
				if err != nil {
					fmt.Printf("   [TX %d/%d] ⚠️ Lỗi gửi tx qua %s: %v\n", txIdx+1, count, targetNode.Name, err)
					return
				}

				recordsMu.Lock()
				records = append(records, &TxRecord{
					Index:    txIdx + 1,
					NodeName: targetNode.Name,
					TxHash:   signedTx.Hash(),
					Client:   targetNode.Client,
				})
				recordsMu.Unlock()
			}(i)
		}
		wg.Wait()

		confirmed := 0
		for _, rec := range records {
			timeoutStart := time.Now()
			for {
				if time.Since(timeoutStart) > 20*time.Second {
					break
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				receipt, rErr := rec.Client.TransactionReceipt(ctx, rec.TxHash)
				cancel()

				if rErr == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
					confirmed++
					break
				}
				time.Sleep(300 * time.Millisecond)
			}
		}
		return confirmed
	}

	// 4. Nếu yêu cầu min-blocks: tiếp tục gửi giao dịch cho tới khi block đạt mốc
	if *minBlocks > 0 {
		fmt.Printf("⏳ Đang kiểm tra và kích block để cụm đạt tối thiểu Block #%d...\n", *minBlocks)
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			curBlock, err := activeNodes[0].Client.BlockNumber(ctx)
			cancel()
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}
			fmt.Printf("   • Block hiện tại: #%d (Mục tiêu: #%d)\n", curBlock, *minBlocks)
			if int(curBlock) >= *minBlocks {
				fmt.Printf("   ✅ Đã đạt block mục tiêu #%d!\n", curBlock)
				break
			}
			diff := *minBlocks - int(curBlock)
			toSend := 15
			if diff > 30 {
				toSend = 25
			}
			sendBatch(toSend)
			time.Sleep(1 * time.Second)
		}
	}

	// 5. Nếu có yêu cầu gửi giao dịch kiểm chứng
	if *txCount > 0 {
		fmt.Printf("⏳ Bắt đầu gửi %d giao dịch phân phối qua %d node...\n", *txCount, len(activeNodes))
		confirmed := sendBatch(*txCount)
		fmt.Printf("📊 Kết quả: Đã xác nhận %d/%d giao dịch (%.1f%%)\n", confirmed, *txCount, float64(confirmed)/float64(*txCount)*100)
		if confirmed == 0 {
			log.Fatalf("❌ Không có giao dịch nào được xác nhận vào block!")
		}
	}

	// 6. Nếu có yêu cầu chờ đồng bộ (wait-sync-node)
	if *waitSyncNode != "" {
		fmt.Printf("⏳ Chờ Node %s đồng bộ bắt kịp block height với cụm (chênh lệch <= %d blocks)...\n", *waitSyncNode, *maxLag)
		maxWait := 60
		synced := false
		for w := 0; w < maxWait; w++ {
			var maxH uint64 = 0
			var targetH uint64 = 0
			foundTarget := false

			for _, n := range nodes {
				c, dErr := dialClient(n.URL)
				if dErr == nil {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					bNum, bErr := c.BlockNumber(ctx)
					cancel()
					if bErr == nil {
						if bNum > maxH {
							maxH = bNum
						}
						if matchNodeName(n.Name, *waitSyncNode) {
							targetH = bNum
							foundTarget = true
						}
					}
				}
			}

			if foundTarget && maxH > 0 {
				diff := int64(maxH) - int64(targetH)
				if diff < 0 {
					diff = 0
				}
				fmt.Printf("   [%ds/%ds] Node %s: Block #%d | Cụm Max: Block #%d | Chênh lệch: %d blocks\n",
					w*3, maxWait*3, *waitSyncNode, targetH, maxH, diff)
				if int(diff) <= *maxLag {
					fmt.Printf("   ✅ Node %s đã đồng bộ thành công (Chênh lệch: %d <= %d blocks)!\n", *waitSyncNode, diff, *maxLag)
					synced = true
					break
				}
			} else {
				fmt.Printf("   [%ds/%ds] Đang chờ Node %s phản hồi RPC...\n", w*3, maxWait*3, *waitSyncNode)
			}
			time.Sleep(3 * time.Second)
		}
		if !synced {
			log.Fatalf("❌ Hết thời gian chờ: Node %s không đồng bộ kịp sau %ds!", *waitSyncNode, maxWait*3)
		}
	}

	// 7. Cluster Health Check
	fmt.Println("\n📡 [CLUSTER HEALTH CHECK] Kiểm tra trạng thái sống/chết của các node...")
	var deadNodes []string
	var aliveNodes []*NodeClient
	nodeHeights := make(map[string]uint64)

	for _, n := range nodes {
		isExcluded := isNodeExcluded(n.Name, *stoppedNode, *excludeNodes)
		c, err := dialClient(n.URL)
		online := false
		var bNum uint64
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			bNum, err = c.BlockNumber(ctx)
			cancel()
			if err == nil {
				online = true
				n.Client = c
				n.Online = true
			}
		}

		if isExcluded {
			if online {
				fmt.Printf("   • Node %s (%s): ⚪ EXCLUDED / VẪN ONLINE (Block #%d - Bỏ qua theo kịch bản test)\n", n.Name, n.URL, bNum)
			} else {
				fmt.Printf("   • Node %s (%s): ⚪ STOPPED/SNAPSHOT (Đã tắt hoặc Snapshot theo kịch bản)\n", n.Name, n.URL)
			}
			continue
		}

		if online {
			aliveNodes = append(aliveNodes, n)
			nodeHeights[n.Name] = bNum
			fmt.Printf("   • Node %s (%s): 🟢 ALIVE (Block #%d)\n", n.Name, n.URL, bNum)
		} else {
			fmt.Printf("   • Node %s (%s): 🔴 DEAD / MẤT KẾT NỐI!\n", n.Name, n.URL)
			deadNodes = append(deadNodes, fmt.Sprintf("%s (%s)", n.Name, n.URL))
		}
	}

	if len(deadNodes) > 0 {
		if *requireAllAlive {
			log.Fatalf("❌ PHÁT HIỆN CÓ %d NODE BỊ CHẾT NGOÀI DỰ KIẾN: %s! BÀI TEST THẤT BẠI!", len(deadNodes), strings.Join(deadNodes, ", "))
		} else {
			fmt.Printf("⚠️ Cảnh báo: Có %d node bị chết: %s\n", len(deadNodes), strings.Join(deadNodes, ", "))
		}
	} else {
		excStr := *stoppedNode
		if excStr == "" {
			excStr = *excludeNodes
		}
		if excStr != "" {
			fmt.Printf("✅ TẤT CẢ %d NODE CÒN LẠI (ngoại trừ [%s] đang tắt/snapshot) ĐỀU ĐANG SỐNG VÀ ĐỒNG THUẬN KHỎE MẠNH!\n", len(aliveNodes), excStr)
		} else {
			fmt.Printf("✅ TOÀN BỘ %d NODE ĐỀU ĐANG CÒN SỐNG VÀ ĐỒNG BỘ KHỎE MẠNH!\n", len(nodes))
		}
	}

	// 8. Zero-Fork Verification
	if *checkFork && len(aliveNodes) >= 2 {
		fmt.Printf("\n🔍 [ZERO-FORK VERIFICATION] Đối chiếu Block Hash & StateRoot giữa %d node sống...\n", len(aliveNodes))
		var minHeight uint64 = 0
		first := true
		for _, h := range nodeHeights {
			if first || h < minHeight {
				minHeight = h
				first = false
			}
		}

		if minHeight > 0 {
			startCompare := uint64(1)
			if minHeight > 5 {
				startCompare = minHeight - 4
			}

			forkDetected := false
			for b := startCompare; b <= minHeight; b++ {
				bBig := big.NewInt(int64(b))
				var referenceHash common.Hash
				var referenceRoot common.Hash
				var referenceNode string

				for _, n := range aliveNodes {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					block, bErr := n.Client.BlockByNumber(ctx, bBig)
					cancel()
					if bErr != nil {
						continue
					}
					h := block.Hash()
					root := block.Root()

					if referenceHash == (common.Hash{}) {
						referenceHash = h
						referenceRoot = root
						referenceNode = n.Name
					} else {
						if h != referenceHash {
							forkDetected = true
							fmt.Printf("🚨 FORK DETECTED TẠI BLOCK %d!\n", b)
							fmt.Printf("   - Node %s: Hash=%s\n", referenceNode, referenceHash.Hex())
							fmt.Printf("   - Node %s: Hash=%s\n", n.Name, h.Hex())
						}
						if root != referenceRoot {
							forkDetected = true
							fmt.Printf("🚨 STATE ROOT MISMATCH TẠI BLOCK %d!\n", b)
							fmt.Printf("   - Node %s: StateRoot=%s\n", referenceNode, referenceRoot.Hex())
							fmt.Printf("   - Node %s: StateRoot=%s\n", n.Name, root.Hex())
						}
					}
				}
			}

			if forkDetected {
				log.Fatalf("❌ PHÁT HIỆN FORK GIỮA CÁC NODE! BÀI TEST THẤT BẠI!")
			} else {
				fmt.Printf("🏆 [100%% ZERO-FORK CONFIRMED] Tất cả %d node đồng nhất hoàn hảo từ Block %d đến %d!\n",
					len(aliveNodes), startCompare, minHeight)
			}
		}
	}
}
