package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"tool-test/pkg/logger"
)

// ===== JSON-RPC types =====

var ghostMutex sync.Mutex
var loggedBlocks = make(map[uint64]bool)

func clearLoggedBlocks() {
	ghostMutex.Lock()
	defer ghostMutex.Unlock()
	loggedBlocks = make(map[uint64]bool)
	os.Truncate("ghost_blocks.log", 0)
}

func init() {
	loadLoggedBlocks()
}

func loadLoggedBlocks() {
	ghostMutex.Lock()
	defer ghostMutex.Unlock()

	data, err := os.ReadFile("ghost_blocks.log")
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.Contains(line, "blockNumber=") {
			parts := strings.Split(line, "blockNumber=")
			if len(parts) > 1 {
				numStr := strings.Split(parts[1], " ")[0]
				var num uint64
				fmt.Sscanf(numStr, "%d", &num)
				if num > 0 {
					loggedBlocks[num] = true
				}
			}
		} else if strings.Contains(line, "Ghost block detected: ") {
			parts := strings.Split(line, "Ghost block detected: ")
			if len(parts) > 1 {
				numStr := strings.TrimSpace(parts[1])
				var num uint64
				fmt.Sscanf(numStr, "%d", &num)
				if num > 0 {
					loggedBlocks[num] = true
				}
			}
		}
	}
}

func logBlockEvent(eventType string, blockNum uint64, gei string) {
	ghostMutex.Lock()
	defer ghostMutex.Unlock()

	if loggedBlocks[blockNum] {
		return // Avoid duplicates
	}
	loggedBlocks[blockNum] = true

	geiStr := "unknown"
	if gei != "" {
		geiStr = fmt.Sprintf("%d", parseHexStr(gei))
	}

	// Ghi vào file
	f, err := os.OpenFile("ghost_blocks.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	now := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf("[%s] %s DETECTED: blockNumber=%d gei=%s\n", now, eventType, blockNum, geiStr)
	f.WriteString(msg)

	// In ra terminal hiện tại chỉ khi là NIL_BLOCK
	if strings.Contains(eventType, "NIL_BLOCK") {
		logger.Info("\n🚨 " + msg)
	}
}

// logAnomaly logs chain health anomalies to chain_anomalies.log and prints to terminal.
func logAnomaly(anomalyType string, blockNum uint64, detail string) {
	now := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf("[%s] %s: blockNumber=%d %s\n", now, anomalyType, blockNum, detail)

	// Ghi vào file
	f, err := os.OpenFile("chain_anomalies.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.WriteString(msg)
		f.Close()
	}

	// In ra terminal
	logger.Info("\n🚨 " + msg)

	triggerStopFlag(fmt.Sprintf("%s: blockNumber=%d %s", anomalyType, blockNum, detail))
}

func triggerStopFlag(reason string) {
	err := os.WriteFile("/tmp/MTN_CHAIN_ERROR_STOP", []byte(reason), 0644)
	if err == nil {
		logger.Info("\n🛑 ĐÃ KÍCH HOẠT CỜ DỪNG AUTO_TEST (/tmp/MTN_CHAIN_ERROR_STOP)")
	}
}

func logSystemError(nodeName string, blockNum uint64, errMsg string) {
	now := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf("[%s] 🚨 [SYSTEM ERROR] Lỗi hệ thống: Node %s getBlockByNumber(%d) trả về null hoặc lỗi: %s\n", now, nodeName, blockNum, errMsg)

	// Ghi vào file log
	f, err := os.OpenFile("system_errors.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.WriteString(msg)
		f.Close()
	}

	// In ra terminal hiện tại
	logger.Info("\n" + msg)

	// Nếu tool bị chạy ngầm và giấu log (như trong auto_test.sh dùng > log_file),
	// thì ép in thêm một bản thẳng ra màn hình để báo động!
	if stat, _ := os.Stdout.Stat(); (stat.Mode() & os.ModeCharDevice) == 0 {
		if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
			tty.WriteString("\n[TỪ BACKGROUND PROCESS] " + msg)
			tty.Close()
		}
	}
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
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type blockResult struct {
	Hash             string        `json:"hash"`
	Number           string        `json:"number"`
	ParentHash       string        `json:"parentHash"`
	StateRoot        string        `json:"stateRoot"`
	TransactionsRoot string        `json:"transactionsRoot"`
	ReceiptsRoot     string        `json:"receiptsRoot"`
	Timestamp        string        `json:"timestamp"`
	Miner            string        `json:"miner"`
	GlobalExecIndex  string        `json:"globalExecIndex"`
	Epoch            string        `json:"epoch"`
	Transactions     []interface{} `json:"transactions"`
}

// ===== Block info (parsed from blockResult) =====

type blockInfo struct {
	Hash             string
	ParentHash       string
	StateRoot        string
	TransactionsRoot string
	ReceiptsRoot     string
	Timestamp        string
	Miner            string
	GlobalExecIndex  string
	Epoch            string
	TxCount          int
	SysTxCount       int
	Error            string // non-empty if fetch failed
}

func (b blockInfo) IsError() bool {
	return b.Error != ""
}

// ===== Node info =====

type nodeInfo struct {
	Name string
	URL  string
}

// ===== Mismatch record =====

type mismatch struct {
	BlockNumber uint64
	Blocks      map[string]blockInfo // node name -> block info
}

func main() {
	nodesFlag := flag.String("nodes", "", `Danh sách node, format: "name=url,name2=url2"`)
	fromBlock := flag.Uint64("from", 1, "Block bắt đầu kiểm tra")
	toBlock := flag.Uint64("to", 0, "Block kết thúc (0 = lấy block mới nhất)")
	batchSize := flag.Int("batch", 50, "Số block kiểm tra song song mỗi lần")
	timeout := flag.Duration("timeout", 5*time.Second, "Timeout cho mỗi RPC call")
	watchMode := flag.Bool("watch", false, "Chế độ giám sát liên tục — kiểm tra block mới nhất định kỳ")
	watchInterval := flag.Duration("interval", 10*time.Second, "Khoảng thời gian giữa mỗi lần check (watch mode)")
	checkLast := flag.Int("check-last", 5, "Số block gần nhất cần check mỗi cycle (watch mode)")
	flag.Parse()

	if *nodesFlag == "" {
		fmt.Println("❌ Thiếu --nodes flag")
		fmt.Println()
		fmt.Println("Cách dùng:")
		fmt.Println(`  # Quét 1 lần:`)
		fmt.Println(`  ./block_hash_checker --nodes "master=http://localhost:8747,node4=http://localhost:10748" --from 1 --to 5000`)
		fmt.Println()
		fmt.Println(`  # Giám sát liên tục:`)
		fmt.Println(`  ./block_hash_checker --watch --nodes "master=http://localhost:8747,node4=http://localhost:10748" --interval 10s`)
		os.Exit(1)
	}

	// Parse nodes
	nodes := parseNodes(*nodesFlag)
	if len(nodes) < 2 {
		fmt.Println("❌ Cần ít nhất 2 node để so sánh")
		os.Exit(1)
	}

	fmt.Printf("🔍 Block Hash Checker — So sánh %d nodes\n", len(nodes))
	for _, n := range nodes {
		fmt.Printf("   📡 %s: %s\n", n.Name, n.URL)
	}
	fmt.Println()

	client := &http.Client{Timeout: *timeout}

	// ===== Watch mode =====
	if *watchMode {
		runWatch(client, nodes, *watchInterval, *checkLast)
		return
	}

	// Nếu --to=0, query block mới nhất từ node đầu tiên
	if *toBlock == 0 {
		latest, err := getLatestBlockNumber(client, nodes[0].URL)
		if err != nil {
			fmt.Printf("❌ Không thể lấy block mới nhất từ %s: %v\n", nodes[0].Name, err)
			os.Exit(1)
		}
		*toBlock = latest
		fmt.Printf("📊 Block mới nhất trên %s: %d\n", nodes[0].Name, *toBlock)
	}

	totalBlocks := *toBlock - *fromBlock + 1
	fmt.Printf("📊 Kiểm tra block %d → %d (%d blocks)\n\n", *fromBlock, *toBlock, totalBlocks)

	// ===== Quét block =====
	var allMismatches []mismatch
	var matchCount uint64
	var errorCount uint64
	var skipCount uint64
	startTime := time.Now()

	for batchStart := *fromBlock; batchStart <= *toBlock; batchStart += uint64(*batchSize) {
		batchEnd := batchStart + uint64(*batchSize) - 1
		if batchEnd > *toBlock {
			batchEnd = *toBlock
		}

		batchMismatches, batchMatches, batchErrors, batchSkips, _, _ := checkBatch(client, nodes, batchStart, batchEnd)
		allMismatches = append(allMismatches, batchMismatches...)
		matchCount += batchMatches
		errorCount += batchErrors
		skipCount += batchSkips

		// Progress
		checked := batchEnd - *fromBlock + 1
		elapsed := time.Since(startTime)
		rate := float64(checked) / elapsed.Seconds()
		fmt.Printf("\r⏳ Đã kiểm tra %d/%d blocks (%.0f blocks/s, %d lệch, %d lỗi, %d bỏ qua)   ",
			checked, totalBlocks, rate, len(allMismatches), errorCount, skipCount)
	}
	fmt.Println()
	fmt.Println()

	// ===== Báo cáo =====
	elapsed := time.Since(startTime)

	if len(allMismatches) == 0 {
		fmt.Printf("✅ KẾT QUẢ: Tất cả %d blocks KHỚP giữa %d nodes (%.1fs)\n",
			matchCount, len(nodes), elapsed.Seconds())
	} else {
		fmt.Printf("🚨 KẾT QUẢ: Phát hiện %d blocks LỆCH HASH!\n", len(allMismatches))
		fmt.Printf("   ✅ Khớp: %d | 🚨 Lệch: %d | ❌ Lỗi: %d (%.1fs)\n\n",
			matchCount, len(allMismatches), errorCount, elapsed.Seconds())

		// Chi tiết từng mismatch
		maxShow := 50
		for i, m := range allMismatches {
			if i >= maxShow {
				fmt.Printf("   ... và %d blocks lệch khác (bỏ qua)\n", len(allMismatches)-maxShow)
				break
			}
			printMismatchDetail(m, nodes)
		}

		// Xuất file CSV
		csvFile := fmt.Sprintf("mismatches_%d_%d.csv", *fromBlock, *toBlock)
		if err := writeMismatchCSV(csvFile, nodes, allMismatches); err != nil {
			fmt.Printf("⚠️  Không thể ghi file CSV: %v\n", err)
		} else {
			fmt.Printf("📄 Chi tiết đã ghi vào: %s\n", csvFile)
		}
	}
}

// ===== Parse nodes flag =====

func parseNodes(s string) []nodeInfo {
	var nodes []nodeInfo
	parts := strings.Split(s, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		eqIdx := strings.Index(p, "=")
		if eqIdx < 0 {
			fmt.Printf("⚠️  Bỏ qua node không hợp lệ (thiếu '='): %s\n", p)
			continue
		}
		name := strings.TrimSpace(p[:eqIdx])
		url := strings.TrimSpace(p[eqIdx+1:])
		if name == "" || url == "" {
			continue
		}
		nodes = append(nodes, nodeInfo{Name: name, URL: url})
	}
	return nodes
}

// ===== Check a batch of blocks =====

// prevBlockState tracks sequential state for anomaly detection across consecutive blocks.
type prevBlockState struct {
	Timestamp       uint64
	GEI             uint64
	Epoch           uint64
	StateRoot       string
	StateRootStreak int // number of consecutive blocks with same stateRoot
	BlockNum        uint64
	IsNil           bool
}

func checkBatch(client *http.Client, nodes []nodeInfo, from, to uint64) (mismatches []mismatch, matchCount, errorCount, skipCount uint64, nilBlocks []uint64, emptyBlocks []uint64) {
	type result struct {
		blockNum uint64
		blocks   map[string]blockInfo
		hasError bool
	}

	count := to - from + 1
	results := make([]result, count)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10) // max 10 concurrent

	for i := uint64(0); i < count; i++ {
		wg.Add(1)
		go func(idx uint64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			blockNum := from + idx
			blocks := make(map[string]blockInfo)
			hasErr := false

			for _, node := range nodes {
				bi, err := getBlockInfo(client, node.URL, blockNum)
				if err == nil && bi.Error == "(block không tồn tại)" {
					latest, errLatest := getLatestBlockNumber(client, node.URL)
					if errLatest == nil && blockNum <= latest {
						logger.Info("⏳ Detected transient write lag on node %s for block %d (latest: %d). Retrying...", node.Name, blockNum, latest)
						for retries := 1; retries <= 10; retries++ {
							time.Sleep(500 * time.Millisecond)
							bi, err = getBlockInfo(client, node.URL, blockNum)
							if err == nil && !bi.IsError() {
								logger.Info("✅ Node %s caught up on block %d after %d retries", node.Name, blockNum, retries)
								break
							}
						}
					}
				}
				if err != nil || (bi.IsError() && bi.Error != "(block không tồn tại)") {
					// Retry logic: allow lagging nodes to catch up on async LevelDB writes
					for retries := 1; retries <= 3; retries++ {
						time.Sleep(500 * time.Millisecond)
						bi, err = getBlockInfo(client, node.URL, blockNum)
						if err == nil && (!bi.IsError() || bi.Error == "(block không tồn tại)") {
							break
						}
					}
				}

				if err != nil {
					blocks[node.Name] = blockInfo{Error: fmt.Sprintf("ERROR: %v", err)}
					hasErr = true
				} else {
					blocks[node.Name] = bi
					if bi.IsError() {
						hasErr = true
					}
				}
			}

			results[idx] = result{blockNum: blockNum, blocks: blocks, hasError: hasErr}
		}(i)
	}
	wg.Wait()

	// Build a map of block hash by node name for chain integrity check (parentHash verification)
	// prevBlockHashes[nodeName] = hash of previous block on that node
	prevBlockHashes := make(map[string]string)

	// Track sequential state per node for anomaly detection
	prevState := make(map[string]*prevBlockState)

	for _, r := range results {
		if r.hasError {
			errorCount++
		}

		// === CHECK 1: Compare all fields across nodes ===
		var validBlocks []blockInfo
		var validNames []string
		missingResponseCount := 0

		for _, node := range nodes {
			bi := r.blocks[node.Name]
			if !bi.IsError() {
				validBlocks = append(validBlocks, bi)
				validNames = append(validNames, node.Name)
			} else if bi.Error == "(block không tồn tại)" {
				missingResponseCount++
				// logSystemError(node.Name, r.blockNum, bi.Error)
			}
		}

		if len(validBlocks) < 2 {
			// Nếu tất cả các node phản hồi đều báo block không tồn tại, thì coi là ghost block và bỏ qua.
			// (Không cần đợi các node đang lỗi/sập phản hồi)
			if len(validBlocks) == 0 && missingResponseCount > 0 {
				nilBlocks = append(nilBlocks, r.blockNum)
				logBlockEvent("NIL_BLOCK", r.blockNum, "")
				// Crucial: delete from prevBlockHashes for all nodes since this block is a gap/nil block
				for _, node := range nodes {
					delete(prevBlockHashes, node.Name)
				}
				continue
			}

			skipCount++
			// Still update prevBlockHashes for chain integrity
			for _, node := range nodes {
				bi := r.blocks[node.Name]
				if !bi.IsError() {
					prevBlockHashes[node.Name] = bi.Hash
				} else {
					delete(prevBlockHashes, node.Name)
				}
			}
			continue
		}

		// Compare hash, parentHash, stateRoot, txRoot, receiptsRoot across all valid nodes
		hasMismatch := false
		ref := validBlocks[0]

		if ref.TxCount == 0 && ref.SysTxCount == 0 {
			emptyBlocks = append(emptyBlocks, r.blockNum)
			logBlockEvent("EMPTY_BLOCK", r.blockNum, ref.GlobalExecIndex)
		}

		for i := 1; i < len(validBlocks); i++ {
			b := validBlocks[i]
			if b.Hash != ref.Hash || b.ParentHash != ref.ParentHash ||
				b.StateRoot != ref.StateRoot || b.TransactionsRoot != ref.TransactionsRoot ||
				b.ReceiptsRoot != ref.ReceiptsRoot || b.Timestamp != ref.Timestamp ||
				b.Miner != ref.Miner {
				hasMismatch = true
				break
			}
		}

		// === CHECK 2: Chain integrity — parentHash of block N == hash of block N-1 ===
		// CRITICAL FIX: Save original hashes BEFORE marking errors, to prevent cascade.
		// Otherwise, once a block is marked CHAIN BROKEN (Error set), prevBlockHashes
		// stops updating → every subsequent block is also falsely flagged CHAIN BROKEN.
		originalHashes := make(map[string]string)
		for _, node := range nodes {
			bi := r.blocks[node.Name]
			if !bi.IsError() {
				originalHashes[node.Name] = bi.Hash
			}
		}

		for _, node := range nodes {
			bi := r.blocks[node.Name]
			if bi.IsError() {
				continue
			}
			prevHash, hasPrev := prevBlockHashes[node.Name]
			if hasPrev && bi.ParentHash != prevHash {
				// Chain is broken on this node!
				hasMismatch = true
				// Mark the error in the block info for display
				brokenBi := r.blocks[node.Name]
				brokenBi.Error = fmt.Sprintf("CHAIN BROKEN: parentHash=%s but prev block hash=%s",
					bi.ParentHash[:18]+"...", prevHash[:18]+"...")
				r.blocks[node.Name] = brokenBi
			}
		}

		// Update prevBlockHashes using original (pre-error-marking) hashes
		// This prevents cascading false positives
		for nodeName, hash := range originalHashes {
			prevBlockHashes[nodeName] = hash
		}
		// If a node was missing or returned an error for this block, we MUST remove it from prevBlockHashes.
		// Otherwise, when the next block arrives, it will be incorrectly compared against a stale (N-2) hash
		// and falsely flagged as "CHAIN BROKEN" (e.g. a false positive chain break during heavy load async writes).
		for _, node := range nodes {
			if _, ok := originalHashes[node.Name]; !ok {
				delete(prevBlockHashes, node.Name)
			}
		}

		if hasMismatch {
			mismatches = append(mismatches, mismatch{BlockNumber: r.blockNum, Blocks: r.blocks})
			// Generate detailed mismatch information
			var details []string
			for _, node := range nodes {
				bi := r.blocks[node.Name]
				if bi.IsError() {
					details = append(details, fmt.Sprintf("%s=ERR(%s)", node.Name, bi.Error))
				} else {
					details = append(details, fmt.Sprintf("%s=hash(%s) gei(%d) epoch(%d)", node.Name, safePrefix(bi.Hash, 10), parseHexStr(bi.GlobalExecIndex), parseHexStr(bi.Epoch)))
				}
			}
			triggerStopFlag(fmt.Sprintf("CHAIN_BROKEN_OR_HASH_MISMATCH: blockNumber=%d details: [%s]", r.blockNum, strings.Join(details, " | ")))
		} else {
			matchCount++
		}

		// === CHECK 3–7: Sequential anomaly detection (per node) ===
		for _, node := range nodes {
			bi := r.blocks[node.Name]
			if bi.IsError() {
				// CHECK 4: Block gap — previous was valid but current is NIL
				// if prev, ok := prevState[node.Name]; ok && !prev.IsNil {
				// 	logAnomaly("BLOCK_GAP", r.blockNum,
				// 		fmt.Sprintf("node=%s prev_block=#%d was OK, current block NOT FOUND (Gap in chain!)",
				// 			node.Name, prev.BlockNum))
				// }
				prevState[node.Name] = &prevBlockState{BlockNum: r.blockNum, IsNil: true}
				continue
			}

			curTS := parseHexStr(bi.Timestamp)
			curGEI := parseHexStr(bi.GlobalExecIndex)
			curEpoch := parseHexStr(bi.Epoch)
			curStateRoot := bi.StateRoot
			curTxCount := bi.TxCount + bi.SysTxCount

			if prev, ok := prevState[node.Name]; ok && !prev.IsNil {
				// CHECK 1: Timestamp regression
				if curTS > 0 && prev.Timestamp > 0 && curTS < prev.Timestamp {
					regression := prev.Timestamp - curTS
					if regression > 30 {
						logAnomaly("TIMESTAMP_REGRESSION", r.blockNum,
							fmt.Sprintf("node=%s ts=%d < prev_ts=%d (-%ds — stale DAG commit!)",
								node.Name, curTS, prev.Timestamp, regression))
					}
				}

				// CHECK 2: GEI regression or duplicate
				if curGEI > 0 && prev.GEI > 0 && curGEI <= prev.GEI {
					logAnomaly("GEI_REGRESSION", r.blockNum,
						fmt.Sprintf("node=%s GEI=%d <= prev_GEI=%d (Ghost block or duplicate!)",
							node.Name, curGEI, prev.GEI))
				}

				// CHECK 3: Epoch inconsistency (epoch must never decrease)
				if curEpoch > 0 && prev.Epoch > 0 && curEpoch < prev.Epoch {
					logAnomaly("EPOCH_REGRESSION", r.blockNum,
						fmt.Sprintf("node=%s epoch=%d < prev_epoch=%d (CRITICAL: epoch went backward!)",
							node.Name, curEpoch, prev.Epoch))
				}

				// CHECK 5: StateRoot freeze (same stateRoot across 5+ consecutive non-epoch-boundary blocks with txs)
				if curStateRoot == prev.StateRoot && curEpoch == prev.Epoch {
					newStreak := prev.StateRootStreak + 1
					// LƯU Ý: Chỉ bắt lỗi nếu block thực sự có giao dịch người dùng (bi.TxCount > 0).
					// Bỏ qua giao dịch hệ thống vì nó có thể không làm thay đổi account state.
					if newStreak >= 5 && bi.TxCount > 0 {
						// Fetch receipt details for this frozen block to diagnose WHY stateRoot didn't change
						receiptSummary := fetchReceiptSummary(client, node.URL, r.blockNum)
						detailStr := fmt.Sprintf("\n"+
							"   [VẤN ĐỀ NGHIÊM TRỌNG TRÊN NODE %s]\n"+
							"   - Block này có tổng cộng %d giao dịch (Giao dịch thường: %d, Giao dịch hệ thống: %d).\n"+
							"   - Kết quả chạy: %s\n"+
							"   - TUY NHIÊN, StateRoot (%s...) KHÔNG HỀ THAY ĐỔI trong %d block liên tiếp.\n"+
							"   => KẾT LUẬN: Giao dịch báo thành công (tăng nonce, thu phí) nhưng node đã GẶP LỖI không cập nhật trạng thái (không commit NOMT Trie) vào Database!",
							node.Name, curTxCount, bi.TxCount, bi.SysTxCount, receiptSummary, safePrefix(curStateRoot, 10), newStreak)
						logAnomaly("STATEROOT_FREEZE_LỖI_KHÔNG_LƯU_STATE", r.blockNum, detailStr)
					}
					prevState[node.Name] = &prevBlockState{
						Timestamp: curTS, GEI: curGEI, Epoch: curEpoch,
						StateRoot: curStateRoot, StateRootStreak: newStreak,
						BlockNum: r.blockNum,
					}
				} else {
					prevState[node.Name] = &prevBlockState{
						Timestamp: curTS, GEI: curGEI, Epoch: curEpoch,
						StateRoot: curStateRoot, StateRootStreak: 0,
						BlockNum: r.blockNum,
					}
				}
			} else {
				prevState[node.Name] = &prevBlockState{
					Timestamp: curTS, GEI: curGEI, Epoch: curEpoch,
					StateRoot: curStateRoot, StateRootStreak: 0,
					BlockNum: r.blockNum,
				}
			}
		}
	}

	return
}

// safePrefix returns the first n chars of a string safely.
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// (hash comparison is now inline in checkBatch — compares all fields + chain integrity)

// ===== JSON-RPC calls =====

func getSystemTxsCount(client *http.Client, url string, hexBlock string) (int, error) {
	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  "eth_getSystemTransactionsByBlockNumber",
		Params:  []interface{}{hexBlock},
		ID:      1,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}

	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(data, &rpcResp); err != nil {
		return 0, fmt.Errorf("invalid JSON response: %v", err)
	}

	if rpcResp.Error != nil {
		return 0, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if string(rpcResp.Result) == "null" {
		return 0, nil
	}

	var txs []interface{}
	if err := json.Unmarshal(rpcResp.Result, &txs); err != nil {
		return 0, fmt.Errorf("cannot parse system txs: %v", err)
	}

	return len(txs), nil
}

func getBlockInfo(client *http.Client, url string, blockNum uint64) (blockInfo, error) {
	hexBlock := fmt.Sprintf("0x%x", blockNum)
	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  "eth_getBlockByNumber",
		Params:  []interface{}{hexBlock, false},
		ID:      1,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return blockInfo{}, err
	}

	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return blockInfo{}, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return blockInfo{}, err
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(data, &rpcResp); err != nil {
		return blockInfo{}, fmt.Errorf("invalid JSON response: %v", err)
	}

	if rpcResp.Error != nil {
		return blockInfo{}, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if string(rpcResp.Result) == "null" {
		return blockInfo{Error: "(block không tồn tại)"}, nil
	}

	var block blockResult
	if err := json.Unmarshal(rpcResp.Result, &block); err != nil {
		return blockInfo{}, fmt.Errorf("cannot parse block result: %v", err)
	}

	txCount := len(block.Transactions)
	sysTxCount := 0

	if txCount == 0 {
		count, err := getSystemTxsCount(client, url, hexBlock)
		if err == nil {
			sysTxCount = count
		}
	}

	return blockInfo{
		Hash:             block.Hash,
		ParentHash:       block.ParentHash,
		StateRoot:        block.StateRoot,
		TransactionsRoot: block.TransactionsRoot,
		ReceiptsRoot:     block.ReceiptsRoot,
		Timestamp:        block.Timestamp,
		Miner:            block.Miner,
		GlobalExecIndex:  block.GlobalExecIndex,
		Epoch:            block.Epoch,
		TxCount:          txCount,
		SysTxCount:       sysTxCount,
	}, nil
}

// ===== Receipt diagnostics for STATEROOT_FREEZE =====

// receiptResult represents a parsed transaction receipt from JSON-RPC
type receiptResult struct {
	TransactionHash string `json:"transactionHash"`
	Status          string `json:"status"` // "0x1" = success, "0x0" = failure
	From            string `json:"from"`
	To              string `json:"to"`
	GasUsed         string `json:"gasUsed"`
	BlockNumber     string `json:"blockNumber"`
}

// txObjectResult represents a full transaction object from eth_getBlockByNumber(true)
type txObjectResult struct {
	Hash  string `json:"hash"`
	From  string `json:"from"`
	To    string `json:"to"`
	Nonce string `json:"nonce"`
	Value string `json:"value"`
}

// fetchReceiptSummary fetches block with full TXs and their receipts,
// returning a diagnostic summary string like "[receipts: 5✅ / 0❌ / 0❓ | samples: 0x1234...→n=3:ok, ...]"
func fetchReceiptSummary(client *http.Client, nodeURL string, blockNum uint64) string {
	hexBlock := fmt.Sprintf("0x%x", blockNum)

	// Step 1: Fetch block with full transaction objects
	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  "eth_getBlockByNumber",
		Params:  []interface{}{hexBlock, true}, // true = full tx objects
		ID:      1,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "[receipts: fetch_error]"
	}

	resp, err := client.Post(nodeURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "[receipts: rpc_error]"
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "[receipts: read_error]"
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(data, &rpcResp); err != nil {
		return "[receipts: parse_error]"
	}

	if string(rpcResp.Result) == "null" {
		return "[receipts: block_null]"
	}

	// Parse block with full tx objects
	var fullBlock struct {
		Transactions []txObjectResult `json:"transactions"`
	}
	if err := json.Unmarshal(rpcResp.Result, &fullBlock); err != nil {
		return "[receipts: tx_parse_error]"
	}

	txs := fullBlock.Transactions
	if len(txs) == 0 {
		return "[receipts: 0 txs in block body]"
	}

	// Step 2: Fetch receipt for each TX (parallel, max 10 concurrent)
	type receiptInfo struct {
		TxHash  string
		From    string
		Nonce   uint64
		Status  string // "ok", "fail", "missing"
		GasUsed uint64
	}

	results := make([]receiptInfo, len(txs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	for i, tx := range txs {
		wg.Add(1)
		go func(idx int, txObj txObjectResult) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			nonce := parseHexStr(txObj.Nonce)
			ri := receiptInfo{
				TxHash: txObj.Hash,
				From:   txObj.From,
				Nonce:  nonce,
				Status: "missing",
			}

			rcp, err := getTransactionReceipt(client, nodeURL, txObj.Hash)
			if err == nil && rcp != nil {
				ri.GasUsed = parseHexStr(rcp.GasUsed)
				if rcp.Status == "0x1" {
					ri.Status = "ok"
				} else {
					ri.Status = "fail"
				}
			}

			results[idx] = ri
		}(i, tx)
	}
	wg.Wait()

	// Step 3: Summarize
	okCount := 0
	failCount := 0
	missingCount := 0
	for _, ri := range results {
		switch ri.Status {
		case "ok":
			okCount++
		case "fail":
			failCount++
		default:
			missingCount++
		}
	}

	// Build nonce detail for first 5 TXs
	nonceSamples := make([]string, 0, 5)
	for i, ri := range results {
		if i >= 5 {
			break
		}
		fromShort := ri.From
		if len(fromShort) > 10 {
			fromShort = fromShort[:10] + "..."
		}
		nonceSamples = append(nonceSamples, fmt.Sprintf("%s→n=%d:%s", fromShort, ri.Nonce, ri.Status))
	}

	return fmt.Sprintf("[receipts: %d✅ / %d❌ / %d❓ | samples: %s]",
		okCount, failCount, missingCount, strings.Join(nonceSamples, ", "))
}

// getTransactionReceipt fetches a single TX receipt via JSON-RPC
func getTransactionReceipt(client *http.Client, nodeURL string, txHash string) (*receiptResult, error) {
	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  "eth_getTransactionReceipt",
		Params:  []interface{}{txHash},
		ID:      1,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := client.Post(nodeURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(data, &rpcResp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if string(rpcResp.Result) == "null" {
		return nil, nil // TX exists in block but receipt not found
	}

	var rcp receiptResult
	if err := json.Unmarshal(rpcResp.Result, &rcp); err != nil {
		return nil, fmt.Errorf("cannot parse receipt: %v", err)
	}

	return &rcp, nil
}

func parseHexStr(hexStr string) uint64 {
	if hexStr == "" {
		return 0
	}
	var num uint64
	fmt.Sscanf(hexStr, "0x%x", &num)
	return num
}

func getLatestBlockNumber(client *http.Client, url string) (uint64, error) {
	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  "eth_blockNumber",
		Params:  []interface{}{},
		ID:      1,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}

	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(data, &rpcResp); err != nil {
		return 0, fmt.Errorf("invalid JSON response: %v", err)
	}

	if rpcResp.Error != nil {
		return 0, fmt.Errorf("RPC error: %s", rpcResp.Error.Message)
	}

	var hexStr string
	if err := json.Unmarshal(rpcResp.Result, &hexStr); err != nil {
		return 0, fmt.Errorf("cannot parse block number: %v", err)
	}

	// Parse hex
	var num uint64
	fmt.Sscanf(hexStr, "0x%x", &num)
	return num, nil
}

type peerInfoResp struct {
	Epoch           uint64 `json:"epoch"`
	GlobalExecIndex uint64 `json:"global_exec_index"`
	LastBlockNumber uint64 `json:"last_block_number"`
}

func getPeerInfo(client *http.Client, rpcURL string) (uint64, uint64, error) {
	// rpcURL looks like http://127.0.0.1:8757
	peerURL := strings.TrimRight(rpcURL, "/") + "/peer_info"
	resp, err := client.Get(peerURL)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, 0, fmt.Errorf("bad status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}

	var pInfo peerInfoResp
	if err := json.Unmarshal(data, &pInfo); err != nil {
		return 0, 0, err
	}

	return pInfo.GlobalExecIndex, pInfo.Epoch, nil
}

// ===== Print mismatch detail =====

func printMismatchDetail(m mismatch, nodes []nodeInfo) {
	fmt.Printf("\n⚠\ufe0f  Block %d:\n", m.BlockNumber)

	// Collect valid blocks to show which fields actually differ
	var validBlocks []blockInfo
	var validNames []string
	for _, n := range nodes {
		bi, ok := m.Blocks[n.Name]
		if ok && !bi.IsError() {
			validBlocks = append(validBlocks, bi)
			validNames = append(validNames, n.Name)
		}
	}

	// Determine which fields differ
	hashDiff, parentDiff, stateDiff, txDiff, rcpDiff, timeDiff, minerDiff := false, false, false, false, false, false, false
	if len(validBlocks) >= 2 {
		ref := validBlocks[0]
		for _, b := range validBlocks[1:] {
			if b.Hash != ref.Hash {
				hashDiff = true
			}
			if b.ParentHash != ref.ParentHash {
				parentDiff = true
			}
			if b.StateRoot != ref.StateRoot {
				stateDiff = true
			}
			if b.TransactionsRoot != ref.TransactionsRoot {
				txDiff = true
			}
			if b.ReceiptsRoot != ref.ReceiptsRoot {
				rcpDiff = true
			}
			if b.Timestamp != ref.Timestamp {
				timeDiff = true
			}
			if b.Miner != ref.Miner {
				minerDiff = true
			}
		}
	}

	// Print diff summary
	var diffs []string
	if hashDiff {
		diffs = append(diffs, "hash")
	}
	if parentDiff {
		diffs = append(diffs, "parentHash")
	}
	if stateDiff {
		diffs = append(diffs, "stateRoot")
	}
	if txDiff {
		diffs = append(diffs, "txRoot")
	}
	if rcpDiff {
		diffs = append(diffs, "receiptsRoot")
	}
	if timeDiff {
		diffs = append(diffs, "timestamp")
	}
	if minerDiff {
		diffs = append(diffs, "miner")
	}
	if len(diffs) > 0 {
		fmt.Printf("   ⚠\ufe0f  Fields differ: %s\n", strings.Join(diffs, ", "))
	}

	for _, n := range nodes {
		bi, ok := m.Blocks[n.Name]
		if !ok {
			fmt.Printf("   %-12s (kh\u00f4ng c\u00f3 d\u1eef li\u1ec7u)\n", n.Name+":")
			continue
		}
		if bi.IsError() {
			fmt.Printf("   %-12s %s\n", n.Name+":", bi.Error)
			continue
		}
		line := fmt.Sprintf("   %-12s hash=%s gei=%d epoch=%d", n.Name+":", bi.Hash, parseHexStr(bi.GlobalExecIndex), parseHexStr(bi.Epoch))
		if parentDiff {
			line += fmt.Sprintf("  parent=%s", bi.ParentHash)
		}
		if stateDiff {
			line += fmt.Sprintf("  stateRoot=%s", bi.StateRoot)
		}
		if txDiff {
			line += fmt.Sprintf("  txRoot=%s", bi.TransactionsRoot)
		}
		if rcpDiff {
			line += fmt.Sprintf("  rcpRoot=%s", bi.ReceiptsRoot)
		}
		if timeDiff {
			line += fmt.Sprintf("  time=%s", bi.Timestamp)
		}
		if minerDiff {
			line += fmt.Sprintf("  miner=%s", bi.Miner)
		}
		fmt.Println(line)
	}
	fmt.Println()
}

// ===== CSV export =====

func writeMismatchCSV(filename string, nodes []nodeInfo, mismatches []mismatch) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	// Header
	sortedNames := make([]string, len(nodes))
	for i, n := range nodes {
		sortedNames[i] = n.Name
	}
	sort.Strings(sortedNames)

	header := "block_number"
	for _, name := range sortedNames {
		header += "," + name + "_hash"
		header += "," + name + "_parentHash"
		header += "," + name + "_stateRoot"
		header += "," + name + "_txRoot"
		header += "," + name + "_receiptsRoot"
		header += "," + name + "_gei"
		header += "," + name + "_epoch"
	}
	fmt.Fprintln(f, header)

	// Data
	for _, m := range mismatches {
		line := fmt.Sprintf("%d", m.BlockNumber)
		for _, name := range sortedNames {
			bi, ok := m.Blocks[name]
			if !ok || bi.IsError() {
				errMsg := ""
				if ok {
					errMsg = bi.Error
				}
				line += "," + errMsg + ",,,,,,"
			} else {
				line += "," + bi.Hash
				line += "," + bi.ParentHash
				line += "," + bi.StateRoot
				line += "," + bi.TransactionsRoot
				line += "," + bi.ReceiptsRoot
				line += fmt.Sprintf(",%d,%d", parseHexStr(bi.GlobalExecIndex), parseHexStr(bi.Epoch))
			}
		}
		fmt.Fprintln(f, line)
	}

	return nil
}

// ===== Watch mode =====

const mismatchAlertFile = "hash_mismatch_alert.log"

func runWatch(client *http.Client, nodes []nodeInfo, interval time.Duration, checkLast int) {
	fmt.Printf("👁️  WATCH MODE — kiểm tra %d blocks gần nhất mỗi %v\n", checkLast, interval)
	fmt.Println("   Nhấn Ctrl+C để dừng")
	fmt.Println("   🛑 Tự động DỪNG khi phát hiện lệch hash (ghi vào " + mismatchAlertFile + ")")
	fmt.Println()

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	totalChecks := 0
	totalMismatches := 0
	trackedGhosts := make(map[uint64]bool)
	lastVerifiedBlock := uint64(0)
	nodeWasDead := make(map[string]bool)

	// Run immediately on start
	if watchOnce(client, nodes, checkLast, &totalChecks, &totalMismatches, trackedGhosts, &lastVerifiedBlock, nodeWasDead) {
		fmt.Printf("\n🛑 DỪNG WATCH MODE: Phát hiện lệch hash! Chi tiết đã ghi vào %s\n", mismatchAlertFile)
		fmt.Printf("📊 Tổng kết: %d lần check, %d lệch phát hiện\n", totalChecks, totalMismatches)
		os.Exit(1)
	}

	for {
		select {
		case <-ticker.C:
			if watchOnce(client, nodes, checkLast, &totalChecks, &totalMismatches, trackedGhosts, &lastVerifiedBlock, nodeWasDead) {
				fmt.Printf("\n🛑 DỪNG WATCH MODE: Phát hiện lệch hash! Chi tiết đã ghi vào %s\n", mismatchAlertFile)
				fmt.Printf("📊 Tổng kết: %d lần check, %d lệch phát hiện\n", totalChecks, totalMismatches)
				os.Exit(1)
			}
		case sig := <-sigCh:
			fmt.Printf("\n\n🛑 Nhận signal %v — dừng watch mode\n", sig)
			fmt.Printf("📊 Tổng kết: %d lần check, %d lệch phát hiện\n", totalChecks, totalMismatches)
			return
		}
	}
}

// watchOnce returns true if mismatch detected (caller should stop)
func watchOnce(client *http.Client, nodes []nodeInfo, checkLast int, totalChecks, totalMismatches *int, trackedGhosts map[uint64]bool, lastVerifiedBlock *uint64, nodeWasDead map[string]bool) bool {
	*totalChecks++
	now := time.Now().Format("15:04:05")

	// Query latest block number from each node
	type nodeBlock struct {
		name  string
		block uint64
		gei   uint64
		epoch uint64
		err   error
	}

	var results []nodeBlock
	var minBlock, maxBlock uint64
	minBlock = ^uint64(0)

	var hasNodeError bool
	for _, n := range nodes {
		num, err := getLatestBlockNumber(client, n.URL)

		var gei, epoch uint64
		if err == nil {
			if nodeWasDead[n.Name] {
				fmt.Printf("\n🔄 NODE %s ĐÃ SỐNG LẠI! Tiến hành đặt lại mốc kiểm tra để quét lại từ đầu (block 1)...\n", n.Name)
				nodeWasDead[n.Name] = false
				*lastVerifiedBlock = 0
			}

			// Lấy gei và epoch từ chính block mới nhất thông qua eth_getBlockByNumber
			bi, errBi := getBlockInfo(client, n.URL, num)
			if errBi == nil && !bi.IsError() {
				gei = parseHexStr(bi.GlobalExecIndex)
				epoch = parseHexStr(bi.Epoch)
			} else if bi.IsError() {
				// logSystemError(n.Name, num, bi.Error)
			}
		} else {
			hasNodeError = true
			nodeWasDead[n.Name] = true
		}

		results = append(results, nodeBlock{name: n.Name, block: num, gei: gei, epoch: epoch, err: err})
		if err == nil {
			if num < minBlock {
				minBlock = num
			}
			if num > maxBlock {
				maxBlock = num
			}
		}
	}

	if hasNodeError {
		clearLoggedBlocks()
		for k := range trackedGhosts {
			delete(trackedGhosts, k)
		}
	}

	// Show block heights
	fmt.Printf("[%s] #%d ", now, *totalChecks)
	var heightParts []string
	for _, r := range results {
		if r.err != nil {
			heightParts = append(heightParts, fmt.Sprintf("%s=ERR", r.name))
		} else {
			heightParts = append(heightParts, fmt.Sprintf("%s=%d (gei:%d e:%d)", r.name, r.block, r.gei, r.epoch))
		}
	}
	fmt.Printf("Heights: %s", strings.Join(heightParts, "  "))

	// Block height difference warning
	if maxBlock > 0 && minBlock < ^uint64(0) {
		diff := maxBlock - minBlock
		if diff > 10 {
			fmt.Printf(" ⚠️ CHÊNH %d blocks!", diff)
		}
	}

	// Count how many nodes are actually responding
	respondingNodes := 0
	for _, r := range results {
		if r.err == nil {
			respondingNodes++
		}
	}

	// Check last N blocks — use minBlock as reference (all nodes should have these)
	if minBlock == ^uint64(0) {
		fmt.Println(" ❌ không thể check hash — tất cả node lỗi")
		return false
	}

	if respondingNodes < 2 {
		fmt.Printf(" ⚠️ chỉ %d/%d node phản hồi — KHÔNG THỂ SO SÁNH hash\n", respondingNodes, len(nodes))
		return false
	}

	// Rewind cursor if a node comes back online with a smaller block
	if *lastVerifiedBlock > minBlock {
		fmt.Printf(" ⚠️ Phát hiện node tụt lùi hoặc khởi động lại (minBlock %d < lastVerified %d). Lùi mốc kiểm tra để quét lại...\n", minBlock, *lastVerifiedBlock)
		if minBlock > 0 {
			*lastVerifiedBlock = minBlock - 1
		} else {
			*lastVerifiedBlock = 0
		}
	}

	from := uint64(1)
	if *lastVerifiedBlock > 0 {
		from = *lastVerifiedBlock + 1
	}

	if from > maxBlock {
		fmt.Printf(" ✅ Không có block mới (đã check tới %d)\n", *lastVerifiedBlock)
		// Vẫn in hash của block mới nhất để user theo dõi
		fmt.Printf("   📦 Block %d hashes:\n", maxBlock)
		for _, n := range nodes {
			bi, err := getBlockInfo(client, n.URL, maxBlock)
			if err != nil {
				fmt.Printf("      %-12s ERR: %v\n", n.Name+":", err)
			} else if bi.IsError() {
				fmt.Printf("      %-12s %s\n", n.Name+":", bi.Error)
			} else {
				fmt.Printf("      %-12s hash=%s  stateRoot=%s  gei=%d  epoch=%d\n", n.Name+":", bi.Hash, bi.StateRoot, parseHexStr(bi.GlobalExecIndex), parseHexStr(bi.Epoch))
			}
		}
		return false
	}

	mismatches, matched, _, skipped, nilBlocks, emptyBlocks := checkBatch(client, nodes, from, maxBlock)

	if len(mismatches) == 0 {
		if skipped > 0 {
			fmt.Printf(" ✅ hash khớp %d blocks, ⚠️ %d blocks không đủ node (block %d→%d)\n", matched, skipped, from, maxBlock)
		} else {
			fmt.Printf(" ✅ hash khớp %d blocks (block %d→%d)\n", matched, from, maxBlock)
		}

		if len(nilBlocks) > 0 || len(emptyBlocks) > 0 {
			showN := len(nilBlocks)
			if showN > 5 {
				showN = 5
			}
			showE := len(emptyBlocks)
			if showE > 5 {
				showE = 5
			}

			var parts []string
			if len(nilBlocks) > 0 {
				s := fmt.Sprintf("⚠️ Get bị nil: %d blocks %v", len(nilBlocks), nilBlocks[:showN])
				if len(nilBlocks) > 5 {
					s += "..."
				}
				parts = append(parts, s)
			}
			if len(emptyBlocks) > 0 {
				s := fmt.Sprintf("👻 Rỗng (tx=0, sys_txs=0): %d blocks %v", len(emptyBlocks), emptyBlocks[:showE])
				if len(emptyBlocks) > 5 {
					s += "..."
				}
				parts = append(parts, s)
			}
			fmt.Printf("   %s\n", strings.Join(parts, "  |  "))
		}

		// In hash của block mới nhất (maxBlock) từ mỗi node
		fmt.Printf("   📦 Block %d hashes:\n", maxBlock)
		for _, n := range nodes {
			bi, err := getBlockInfo(client, n.URL, maxBlock)
			if err != nil {
				fmt.Printf("      %-12s ERR: %v\n", n.Name+":", err)
			} else if bi.IsError() {
				fmt.Printf("      %-12s %s\n", n.Name+":", bi.Error)
			} else {
				fmt.Printf("      %-12s hash=%s  stateRoot=%s  gei=%d  epoch=%d\n", n.Name+":", bi.Hash, bi.StateRoot, parseHexStr(bi.GlobalExecIndex), parseHexStr(bi.Epoch))
			}
		}

		*lastVerifiedBlock = minBlock
		return false
	}

	// ═══════════════════════════════════════════════════════════════════
	// MISMATCH DETECTED — write to file + print to console + signal stop
	// ═══════════════════════════════════════════════════════════════════

	// --- TRUY VẤN LÙI ĐỂ TÌM BLOCK LỆCH ĐẦU TIÊN ---
	firstMismatchInBatch := mismatches[0].BlockNumber
	fmt.Printf("\n🔍 Phát hiện lệch hash tại block %d. Đang truy vấn lùi bằng Binary Search để tìm block lệch đầu tiên...\n", firstMismatchInBatch)

	realFirstMismatch := firstMismatchInBatch
	low := uint64(1)
	high := firstMismatchInBatch - 1

	for low <= high {
		mid := low + (high-low)/2
		// Check just this single block
		m, _, _, _, _, _ := checkBatch(client, nodes, mid, mid)
		if len(m) > 0 {
			realFirstMismatch = mid
			high = mid - 1 // Tiếp tục tìm về trước
		} else {
			low = mid + 1 // Mismatch nằm ở phía sau
		}
	}

	if realFirstMismatch < firstMismatchInBatch {
		fmt.Printf("🎯 Đã tìm thấy block lệch đầu tiên thực sự tại: %d\n", realFirstMismatch)
		// Fetch lại chi tiết block này
		m, _, _, _, _, _ := checkBatch(client, nodes, realFirstMismatch, realFirstMismatch)
		if len(m) > 0 {
			// Push vào đầu slice để in ra báo cáo
			mismatches = append(m, mismatches...)
		}
	} else {
		fmt.Printf("🎯 Block %d chính là block lệch đầu tiên trên chuỗi.\n", realFirstMismatch)
	}

	*totalMismatches += len(mismatches)

	// Build alert content for both console and file
	var alertBuf strings.Builder
	alertBuf.WriteString("╔══════════════════════════════════════════════════════════════════╗\n")
	alertBuf.WriteString(fmt.Sprintf("║  🚨 HASH MISMATCH DETECTED — %s                      ║\n", time.Now().Format("2006-01-02 15:04:05")))
	alertBuf.WriteString("╚══════════════════════════════════════════════════════════════════╝\n")
	alertBuf.WriteString(fmt.Sprintf("\nCheck #%d | Blocks checked: %d→%d | Mismatches: %d\n", *totalChecks, from, minBlock, len(mismatches)))
	alertBuf.WriteString("\nNode Heights:\n")
	for _, r := range results {
		if r.err != nil {
			alertBuf.WriteString(fmt.Sprintf("  %-12s ERR: %v\n", r.name+":", r.err))
		} else {
			alertBuf.WriteString(fmt.Sprintf("  %-12s block=%d\n", r.name+":", r.block))
		}
	}
	alertBuf.WriteString("\n─── Mismatch Details ───\n")

	for _, m := range mismatches {
		alertBuf.WriteString(fmt.Sprintf("\n⚠️  Block %d:\n", m.BlockNumber))

		// Find which fields differ
		var diffParent, diffState, diffTx, diffRcp, diffTime, diffMiner, diffGei, diffEpoch bool
		var ref blockInfo
		var refSet bool
		for _, n := range nodes {
			bi, ok := m.Blocks[n.Name]
			if !ok || bi.IsError() || bi.Error != "" {
				continue
			}
			if !refSet {
				ref = bi
				refSet = true
				continue
			}
			if bi.ParentHash != ref.ParentHash {
				diffParent = true
			}
			if bi.StateRoot != ref.StateRoot {
				diffState = true
			}
			if bi.TransactionsRoot != ref.TransactionsRoot {
				diffTx = true
			}
			if bi.ReceiptsRoot != ref.ReceiptsRoot {
				diffRcp = true
			}
			if bi.Timestamp != ref.Timestamp {
				diffTime = true
			}
			if bi.Miner != ref.Miner {
				diffMiner = true
			}
			if bi.GlobalExecIndex != ref.GlobalExecIndex {
				diffGei = true
			}
			if bi.Epoch != ref.Epoch {
				diffEpoch = true
			}
		}

		for _, n := range nodes {
			bi, ok := m.Blocks[n.Name]
			if !ok {
				alertBuf.WriteString(fmt.Sprintf("   %-12s (no data)\n", n.Name+":"))
				continue
			}
			if bi.IsError() {
				alertBuf.WriteString(fmt.Sprintf("   %-12s %s\n", n.Name+":", bi.Error))
				continue
			}

			// Always print hash
			line := fmt.Sprintf("   %-12s hash=%s", n.Name+":", bi.Hash)

			// Only print differing fields
			if diffParent {
				line += fmt.Sprintf("  parent=%s", bi.ParentHash)
			}
			if diffState {
				line += fmt.Sprintf("  stateRoot=%s", bi.StateRoot)
			}
			if diffTx {
				line += fmt.Sprintf("  txRoot=%s", bi.TransactionsRoot)
			}
			if diffRcp {
				line += fmt.Sprintf("  rcpRoot=%s", bi.ReceiptsRoot)
			}
			if diffTime {
				line += fmt.Sprintf("  time=%s", bi.Timestamp)
			}
			if diffMiner {
				line += fmt.Sprintf("  miner=%s", bi.Miner)
			}
			if diffGei {
				line += fmt.Sprintf("  gei=%d", parseHexStr(bi.GlobalExecIndex))
			}
			if diffEpoch {
				line += fmt.Sprintf("  epoch=%d", parseHexStr(bi.Epoch))
			}

			alertBuf.WriteString(line + "\n")
		}
	}

	alertBuf.WriteString("\n─── Summary ───\n")
	alertBuf.WriteString(fmt.Sprintf("Total mismatches: %d\n", *totalMismatches))
	alertBuf.WriteString(fmt.Sprintf("Detected at: %s\n", time.Now().Format("2006-01-02 15:04:05.000")))

	alertContent := alertBuf.String()

	// Print to console
	fmt.Printf("\n")
	fmt.Print(alertContent)

	// Write to file (overwrite)
	if err := os.WriteFile(mismatchAlertFile, []byte(alertContent), 0644); err != nil {
		fmt.Printf("⚠️  Không thể ghi file %s: %v\n", mismatchAlertFile, err)
	} else {
		fmt.Printf("\n📄 Chi tiết đã ghi vào: %s\n", mismatchAlertFile)
	}

	// Also write CSV for detailed analysis
	csvFile := fmt.Sprintf("mismatches_%d_%d.csv", from, minBlock)
	if err := writeMismatchCSV(csvFile, nodes, mismatches); err != nil {
		fmt.Printf("⚠️  Không thể ghi file CSV: %v\n", err)
	} else {
		fmt.Printf("📄 CSV chi tiết: %s\n", csvFile)
	}

	return true // Signal caller to STOP
}
