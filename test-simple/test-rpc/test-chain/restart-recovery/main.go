package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
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
	Name      string
	URL       string
	Client    *ethclient.Client
	RPCClient *rpc.Client
	Online    bool
}

type ConsensusReadyResult struct {
	Ready bool   `json:"ready"`
	Note  string `json:"note"`
}

func dialClient(urlStr string) (*ethclient.Client, *rpc.Client, error) {
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 50,
			IdleConnTimeout:     30 * time.Second,
		},
	}
	rpcClient, err := rpc.DialHTTPWithClient(urlStr, httpClient)
	if err != nil {
		return nil, nil, err
	}
	return ethclient.NewClient(rpcClient), rpcClient, nil
}

func (n *NodeClient) CheckConsensusReady(timeout time.Duration) (*ConsensusReadyResult, error) {
	if n.RPCClient == nil {
		return nil, fmt.Errorf("rpc client is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var res ConsensusReadyResult
	err := n.RPCClient.CallContext(ctx, &res, "eth_consensusReady")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func sendTelegramAlert(text string) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if token == "" || chatID == "" {
		invPaths := []string{
			"../../../../metanode/deploy/ansible/inventory.yml",
			"../../metanode/deploy/ansible/inventory.yml",
			"/home/abc/nhat/con-chain-v2/metanode/deploy/ansible/inventory.yml",
		}
		for _, p := range invPaths {
			data, err := os.ReadFile(p)
			if err == nil {
				lines := strings.Split(string(data), "\n")
				for _, l := range lines {
					trimmed := strings.TrimSpace(l)
					if strings.HasPrefix(trimmed, "bot_token:") && token == "" {
						token = strings.Trim(strings.TrimPrefix(trimmed, "bot_token:"), ` "'`)
					}
					if strings.HasPrefix(trimmed, "chat_id:") && chatID == "" {
						chatID = strings.Trim(strings.TrimPrefix(trimmed, "chat_id:"), ` "'`)
					}
				}
				if token != "" && chatID != "" {
					break
				}
			}
		}
	}
	if token == "" {
		token = "8230176859:AAG2MuF6RI3hRPm9H8_TctSSANkwwrEEdIc"
	}
	if chatID == "" {
		chatID = "-1003867050625"
	}

	data := url.Values{}
	data.Set("chat_id", chatID)
	data.Set("parse_mode", "HTML")
	data.Set("text", text)

	req, err := http.NewRequest("POST", fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token), strings.NewReader(data.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func alertNodeNotReady(nodeName string, ready bool, note string) {
	jsonObj := map[string]interface{}{
		"ready": ready,
		"note":  note,
	}
	jsonBytes, _ := json.MarshalIndent(jsonObj, "", "  ")
	msg := fmt.Sprintf(`⚠️ <b>[METANODE TEST CẢNH BÁO] Node Chưa Sẵn Sàng Xử Lý Giao Dịch!</b>
• <b>Target Node:</b> Node %s
• <b>Phản hồi eth_consensusReady:</b>
<pre>%s</pre>`, nodeName, string(jsonBytes))

	sendTelegramAlert(msg)
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

type NodeProbeResult struct {
	Name           string
	URL            string
	Online         bool
	Stopped        bool
	Block          uint64
	ConsensusReady bool
	ConsensusNote  string
	Err            error
}

func probeCluster(nodes []*NodeClient, stoppedNode, excludeNodes string) (results []NodeProbeResult, crashed []string, minBlock uint64, maxBlock uint64, isSynced bool) {
	minBlock = ^uint64(0)
	maxBlock = 0
	aliveCount := 0

	for _, n := range nodes {
		isStopped := isNodeExcluded(n.Name, stoppedNode, excludeNodes)
		if isStopped {
			results = append(results, NodeProbeResult{
				Name:           n.Name,
				URL:            n.URL,
				Stopped:        true,
				ConsensusReady: true,
			})
			continue
		}

		c, rpcC, dErr := dialClient(n.URL)
		online := false
		var bNum uint64
		consensusReady := true
		consensusNote := ""
		if dErr == nil {
			cCtx, cCancel := context.WithTimeout(context.Background(), 2*time.Second)
			bNum, dErr = c.BlockNumber(cCtx)
			cCancel()
			if dErr == nil {
				online = true
				var res ConsensusReadyResult
				rCtx, rCancel := context.WithTimeout(context.Background(), 2*time.Second)
				rErr := rpcC.CallContext(rCtx, &res, "eth_consensusReady")
				rCancel()
				if rErr == nil {
					consensusReady = res.Ready
					consensusNote = res.Note
				}
			}
		}

		res := NodeProbeResult{
			Name:           n.Name,
			URL:            n.URL,
			Online:         online,
			Block:          bNum,
			ConsensusReady: consensusReady,
			ConsensusNote:  consensusNote,
			Err:            dErr,
		}
		results = append(results, res)

		if online {
			aliveCount++
			if bNum < minBlock {
				minBlock = bNum
			}
			if bNum > maxBlock {
				maxBlock = bNum
			}
		} else {
			crashed = append(crashed, fmt.Sprintf("%s (%s)", n.Name, n.URL))
		}
	}

	if aliveCount <= 1 {
		minBlock = maxBlock
		isSynced = true
	} else {
		isSynced = (minBlock == maxBlock)
	}
	return
}

func printClusterProbe(results []NodeProbeResult, maxBlock uint64) {
	for _, res := range results {
		if res.Stopped {
			fmt.Printf("   • Node %s (%s): ⚪ STOPPED (Đang cố ý TẮT theo kịch bản test - KHÔNG CÓ LỖI)\n", res.Name, res.URL)
		} else if res.Online {
			consensusStr := ""
			if !res.ConsensusReady {
				consensusStr = fmt.Sprintf(" ⚠️ [CONSENSUS NOT READY: %s]", res.ConsensusNote)
			}
			if res.Block < maxBlock {
				fmt.Printf("   • Node %s (%s): 🟢 ALIVE (Block %d) ⚠️ TỤT %d BLOCK so với cụm (Max Block %d)%s\n",
					res.Name, res.URL, res.Block, maxBlock-res.Block, maxBlock, consensusStr)
			} else {
				fmt.Printf("   • Node %s (%s): 🟢 ALIVE (Block %d)%s\n", res.Name, res.URL, res.Block, consensusStr)
			}
		} else {
			fmt.Printf("   • Node %s (%s): 🔴 DEAD / CRASH! (Lỗi: %v)\n", res.Name, res.URL, res.Err)
		}
	}
}

func reportDesyncFailure(reason string, stoppedNode, excludeNodes string, results []NodeProbeResult, minBlock, maxBlock uint64, elapsed time.Duration) {
	fmt.Println("\n==================================================================")
	fmt.Printf("❌ [LỖI ĐỒNG BỘ CHIỀU CAO CLUSTER / BLOCK HEIGHT DESYNC]\n")
	fmt.Printf("   • Nguyên nhân: %s\n", reason)
	exc := stoppedNode
	if exc == "" {
		exc = excludeNodes
	}
	if exc != "" {
		fmt.Printf("   • Kịch bản test: Đang DỪNG node [%s], gửi giao dịch lên các node sống.\n", exc)
	}
	if elapsed > 0 {
		fmt.Printf("   • Thời gian các node sống bị lệch chiều cao: %v\n", elapsed.Round(time.Second))
	}
	fmt.Printf("   • Danh sách Block hiện tại của toàn bộ các node:\n")
	var laggingNodes []string
	for _, res := range results {
		if res.Stopped {
			fmt.Printf("     - Node %s (%s): ⚪ STOPPED (Đang cố ý TẮT theo kịch bản test - KHÔNG CÓ LỖI)\n", res.Name, res.URL)
		} else if res.Online {
			if res.Block < maxBlock {
				fmt.Printf("     - Node %s (%s): 🟢 ALIVE (Block %d) ⚠️ TỤT %d BLOCK so với cụm (Max Block %d)\n",
					res.Name, res.URL, res.Block, maxBlock-res.Block, maxBlock)
				laggingNodes = append(laggingNodes, fmt.Sprintf("Node %s (Block %d < %d)", res.Name, res.Block, maxBlock))
			} else {
				fmt.Printf("     - Node %s (%s): 🟢 ALIVE (Block %d)\n", res.Name, res.URL, res.Block)
			}
		} else {
			fmt.Printf("     - Node %s (%s): 🔴 DEAD / CRASH! (Lỗi: %v)\n", res.Name, res.URL, res.Err)
		}
	}
	if len(laggingNodes) > 0 {
		fmt.Printf("   => Phát hiện %s không đồng bộ chiều cao với các node còn lại!\n", strings.Join(laggingNodes, ", "))
	}
	fmt.Println("==================================================================")
	log.Fatalf("❌ BÀI TEST THẤT BẠI: Các node sống không đồng bộ cùng chiều cao block!")
}

func main() {
	configPath := flag.String("config", "../config.json", "Đường dẫn file config.json")
	txCount := flag.Int("count", 15, "Số lượng giao dịch cần gửi và xác nhận")
	checkFork := flag.Bool("check-fork", true, "Kiểm tra Block Hash & StateRoot giữa các node để đảm bảo không fork")
	requireAllAlive := flag.Bool("require-all-alive", true, "Bắt buộc toàn bộ các node phải đang sống sau đợt test")
	stoppedNode := flag.String("stopped-node", "", "Tên hoặc ID của node đang cố ý bị dừng (ví dụ: '0', 'm0', hoặc '1,4'). Node này được phép offline, các node còn lại bắt buộc phải sống")
	excludeNodes := flag.String("exclude-nodes", "", "Danh sách node ngoại lệ bỏ qua kiểm tra sống (ví dụ: '4' hoặc '1,4')")
	maxDesyncTimeouts := flag.Int("max-desync-timeouts", 6, "Số giao dịch timeout tối đa khi phát hiện các node sống bị lệch chiều cao trước khi báo lỗi")
	maxDesyncDuration := flag.Duration("max-desync-duration", 4*time.Minute, "Thời gian tối đa cho phép các node sống bị lệch chiều cao (desync) trước khi báo lỗi chi tiết")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi load config: %v", err)
	}

	if len(cfg.PrivateKeys) == 0 {
		log.Fatalf("❌ Không có private keys trong config")
	}

	// 1. Khởi tạo danh sách toàn bộ các node RPC (Gộp cả RPCNodes Validator và SyncNodes để kiểm tra toàn bộ 5 node)
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
		c, rpcC, err := dialClient(url)
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
			Name:      name,
			URL:       url,
			Client:    c,
			RPCClient: rpcC,
			Online:    online,
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

	// txSendNodes: Ưu tiên gửi qua các Validator RPC nodes đang online (LOẠI BỎ SyncOnly nodes như m4)
	var txSendNodes []*NodeClient
	for _, n := range activeNodes {
		// Bỏ qua nếu là SyncOnly node (ví dụ: m4) vì node này không propose block
		if _, isSync := cfg.SyncNodes[n.Name]; isSync {
			continue
		}
		if _, isVal := cfg.RPCNodes[n.Name]; isVal || len(cfg.RPCNodes) == 0 {
			txSendNodes = append(txSendNodes, n)
		}
	}
	if len(txSendNodes) == 0 {
		txSendNodes = activeNodes
	}

	fmt.Println("==========================================================")
	fmt.Printf("🚀 METANODE RESTART & RECOVERY TEST\n")
	fmt.Printf("   • Tổng số node cấu hình: %d (Validator + SyncOnly)\n", len(nodes))
	excStr := *stoppedNode
	if excStr == "" {
		excStr = *excludeNodes
	}
	if excStr != "" {
		fmt.Printf("   • Node ngoại lệ (TẮT/SNAPSHOT): [%s] (ngoại lệ kiểm tra sống/chết)\n", excStr)
	}
	fmt.Printf("   • Số node đang ONLINE  : %d (", len(activeNodes))
	for i, n := range activeNodes {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Printf("%s: %s", n.Name, n.URL)
	}
	fmt.Printf(")\n")
	fmt.Printf("   • Số TXs mục tiêu      : %d\n", *txCount)
	fmt.Println("==========================================================")

	if len(txSendNodes) == 0 {
		log.Fatalf("❌ Không có node nào online để gửi giao dịch!")
	}

	// Chờ các Validator node trong txSendNodes thực sự sẵn sàng consensus (tối đa 120s) trước khi gửi
	fmt.Printf("⏳ Đang kiểm tra tính sẵn sàng (Consensus Ready) của các Validator gửi giao dịch...\n")
	for _, n := range txSendNodes {
		waitStart := time.Now()
		isReady := false
		var lastRes *ConsensusReadyResult
		for time.Since(waitStart) < 120*time.Second {
			cRes, cErr := n.CheckConsensusReady(2 * time.Second)
			if cErr == nil && cRes.Ready {
				isReady = true
				break
			}
			lastRes = cRes
			time.Sleep(2 * time.Second)
		}

		if isReady {
			fmt.Printf("   ✅ Node %s (%s): Consensus đã SẴN SÀNG (Healthy)!\n", n.Name, n.URL)
		} else if lastRes != nil {
			fmt.Printf("   ⚠️ Cảnh báo: Node %s (%s) consensus VẪN CHƯA sẵn sàng sau 120s:\n", n.Name, n.URL)
			resBytes, _ := json.MarshalIndent(lastRes, "      ", "  ")
			fmt.Println("      " + string(resBytes))
			alertNodeNotReady(n.Name, lastRes.Ready, lastRes.Note)
		}
	}

	// 2. Chuẩn bị tài khoản gửi tiền và lấy nonce ban đầu an toàn
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
			var n uint64
			var nErr error
			gotNonce := false
			// Chỉ query nonce từ các activeNodes (đang sống và KHÔNG bị stopped)
			for _, an := range activeNodes {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				n, nErr = an.Client.PendingNonceAt(ctx, addr)
				cancel()
				if nErr == nil {
					gotNonce = true
					break
				}
			}
			if gotNonce {
				accounts = append(accounts, &AccountState{
					Key:     pk,
					Address: addr,
					Nonce:   n,
				})
			}
		}
	}
	if len(accounts) == 0 {
		log.Fatalf("❌ Không thể lấy nonce từ bất kỳ node online nào trong: %v", txSendNodes)
	}

	// 3. Gửi giao dịch có kiểm soát và xác nhận rõ ràng
	var txNames []string
	for _, n := range txSendNodes {
		txNames = append(txNames, n.Name)
	}
	excLabel := *stoppedNode
	if excLabel == "" {
		excLabel = *excludeNodes
	}
	if excLabel != "" {
		fmt.Printf("⏳ [NGOẠI LỆ TẮT/SNAPSHOT: %s] Gửi %d giao dịch phân phối đều qua %d Validator ĐANG SỐNG: [%s] (sử dụng %d ví)...\n",
			excLabel, *txCount, len(txSendNodes), strings.Join(txNames, ", "), len(accounts))
		fmt.Printf("   ℹ️  (Node [%s] được bỏ qua theo kịch bản test - KHÔNG gửi giao dịch tới node này)\n", excLabel)
	} else {
		fmt.Printf("⏳ Bắt đầu gửi %d giao dịch phân phối đều qua %d Validator đang sống: [%s] (sử dụng %d ví)...\n",
			*txCount, len(txSendNodes), strings.Join(txNames, ", "), len(accounts))
	}
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

	// Giới hạn concurrency để không nghẽn RPC
	sem := make(chan struct{}, 5)

	for i := 0; i < *txCount; i++ {
		wg.Add(1)
		go func(txIdx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Cấp phát nonce an toàn, độc quyền theo từng tài khoản
			acct := accounts[txIdx%len(accounts)]
			acct.Mu.Lock()
			nonce := acct.Nonce
			acct.Nonce++
			acct.Mu.Unlock()

			// Chọn node theo round-robin (chỉ trong txSendNodes - Validator RPC)
			targetNode := txSendNodes[txIdx%len(txSendNodes)]

			// Amount phân biệt để mỗi tx hash luôn là duy nhất
			amount := big.NewInt(int64(1000 + txIdx))
			gasLimit := uint64(21000)
			gasPrice := big.NewInt(1000000000) // 1 Gwei

			tx := types.NewTransaction(nonce, receiverAddr, amount, gasLimit, gasPrice, nil)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), acct.Key)
			if err != nil {
				fmt.Printf("   [TX %d/%d] ⚠️ Lỗi ký tx: %v\n", txIdx+1, *txCount, err)
				return
			}

			ctxSend, cancelSend := context.WithTimeout(context.Background(), 4*time.Second)
			err = targetNode.Client.SendTransaction(ctxSend, signedTx)
			cancelSend()

			if err != nil {
				fmt.Printf("   [TX %d/%d] ⚠️ Lỗi gửi tx qua %s (ví %s, nonce %d): %v\n",
					txIdx+1, *txCount, targetNode.Name, acct.Address.Hex()[:10], nonce, err)
				return
			}

			rec := &TxRecord{
				Index:    txIdx + 1,
				NodeName: targetNode.Name,
				TxHash:   signedTx.Hash(),
				Client:   targetNode.Client,
			}

			recordsMu.Lock()
			records = append(records, rec)
			recordsMu.Unlock()
		}(i)
	}

	wg.Wait()
	fmt.Printf("📤 Đã gửi %d/%d giao dịch thành công vào mempool các node sống: [%s]. Đang chờ xác nhận trong Block...\n",
		len(records), *txCount, strings.Join(txNames, ", "))

	// 4. Chờ Receipt xác nhận trong Block (tối đa 45s để cụm kịp đồng thuận khi thiếu validator)
	confirmedCount := 0
	timedOutCount := 0
	var desyncFirstDetected *time.Time
	lastProbeTime := time.Now()

	for _, rec := range records {
		timeoutStart := time.Now()
		for {
			now := time.Now()

			// Định kỳ mỗi 10 giây: kiểm tra trạng thái chiều cao các node để phát hiện desync kéo dài
			if now.Sub(lastProbeTime) >= 10*time.Second {
				lastProbeTime = now
				results, crashed, minH, maxH, isSynced := probeCluster(nodes, *stoppedNode, *excludeNodes)
				if len(crashed) > 0 {
					printClusterProbe(results, maxH)
					log.Fatalf("❌ PHÁT HIỆN CÓ %d NODE BỊ CRASH / KHÔNG PHẢN HỒI KHI CHỜ RECEIPT: %s! BÀI TEST THẤT BẠI NGAY LẬP TỨC!",
						len(crashed), strings.Join(crashed, ", "))
				}
				if !isSynced {
					if desyncFirstDetected == nil {
						probeNow := time.Now()
						desyncFirstDetected = &probeNow
					} else if now.Sub(*desyncFirstDetected) >= *maxDesyncDuration {
						elapsed := now.Sub(*desyncFirstDetected)
						reportDesyncFailure(
							fmt.Sprintf("Các node sống KHÔNG ĐỒNG BỘ cùng chiều cao liên tục trong hơn %v (đã trôi qua %.1fs)",
								*maxDesyncDuration, elapsed.Seconds()),
							*stoppedNode, *excludeNodes, results, minH, maxH, elapsed,
						)
					}
				} else {
					desyncFirstDetected = nil
				}
			}

			if time.Since(timeoutStart) > 45*time.Second {
				timedOutCount++
				fmt.Printf("   [TX %d/%d qua %s] ⚠️ Timeout (45s) chờ receipt cho hash: %s (Tổng timeout: %d/%d)\n",
					rec.Index, *txCount, rec.NodeName, rec.TxHash.Hex(), timedOutCount, *maxDesyncTimeouts)

				// 🚨 KIỂM TRA SỨC KHỎE TOÀN BỘ CÁC NODE NGAY LẬP TỨC KHI BỊ TIMEOUT RECEIPT
				fmt.Printf("   🔍 [HEALTH PROBE] Giao dịch %s gửi qua %s bị timeout! Đang kiểm tra trạng thái cụm...\n",
					rec.TxHash.Hex()[:14]+"...", rec.NodeName)

				results, crashed, minH, maxH, isSynced := probeCluster(nodes, *stoppedNode, *excludeNodes)
				printClusterProbe(results, maxH)

				if len(crashed) > 0 {
					log.Fatalf("❌ PHÁT HIỆN CÓ %d NODE BỊ CRASH / KHÔNG PHẢN HỒI KHI CHỜ RECEIPT: %s! BÀI TEST THẤT BẠI NGAY LẬP TỨC!",
						len(crashed), strings.Join(crashed, ", "))
				}

				if !isSynced {
					if desyncFirstDetected == nil {
						probeNow := time.Now()
						desyncFirstDetected = &probeNow
					}
				} else {
					desyncFirstDetected = nil
				}

				// Điều kiện 1: Đã có đủ số giao dịch timeout (mặc định 4) mà các node sống không đồng bộ cùng chiều cao
				if timedOutCount >= *maxDesyncTimeouts && !isSynced {
					elapsed := time.Duration(0)
					if desyncFirstDetected != nil {
						elapsed = time.Since(*desyncFirstDetected)
					}
					reportDesyncFailure(
						fmt.Sprintf("Đã có %d giao dịch bị timeout và phát hiện các node sống KHÔNG ĐỒNG BỘ cùng chiều cao (Min Block: %d, Max Block: %d)",
							timedOutCount, minH, maxH),
						*stoppedNode, *excludeNodes, results, minH, maxH, elapsed,
					)
				}

				// Điều kiện 2: Đã quá thời gian maxDesyncDuration (mặc định 4 phút) mà các node sống vẫn không đồng bộ chiều cao
				if desyncFirstDetected != nil && time.Since(*desyncFirstDetected) >= *maxDesyncDuration {
					elapsed := time.Since(*desyncFirstDetected)
					reportDesyncFailure(
						fmt.Sprintf("Các node sống KHÔNG ĐỒNG BỘ cùng chiều cao liên tục trong hơn %v (đã trôi qua %.1fs)",
							*maxDesyncDuration, elapsed.Seconds()),
						*stoppedNode, *excludeNodes, results, minH, maxH, elapsed,
					)
				}

				break
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			receipt, rErr := rec.Client.TransactionReceipt(ctx, rec.TxHash)
			cancel()

			// Trong Metanode, giao dịch native transfer được xác nhận khi có receipt và BlockNumber > 0
			if rErr == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
				rec.BlockNum = receipt.BlockNumber.Uint64()
				rec.Status = true
				confirmedCount++
				fmt.Printf("   ✅ [TX %d/%d] Đã confirm tại Block #%d qua %s (Hash: %s)\n",
					rec.Index, *txCount, rec.BlockNum, rec.NodeName, rec.TxHash.Hex()[:14]+"...")
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	if *txCount > 0 {
		fmt.Printf("\n📊 KẾT QUẢ GIAO DỊCH:\n")
		fmt.Printf("   • Tổng số giao dịch đã gửi: %d\n", len(records))
		fmt.Printf("   • Số giao dịch ĐÃ XÁC NHẬN : %d (%.1f%%)\n", confirmedCount, float64(confirmedCount)/float64(*txCount)*100)

		if confirmedCount == 0 {
			log.Fatalf("❌ Không có giao dịch nào được xác nhận vào block! Bài test thất bại!")
		}
	}

	// 5. KIỂM TRA SỨC KHỎE TẤT CẢ CÁC NODE (ĐẢM BẢO TẤT CẢ CÒN SỐNG NGOẠI TRỪ NODE CỐ Ý DỪNG)
	fmt.Println("\n📡 [CLUSTER HEALTH CHECK] Kiểm tra trạng thái sống/chết của các node...")
	var deadNodes []string
	var aliveNodes []*NodeClient
	nodeHeights := make(map[string]uint64)

	for _, n := range nodes {
		isExcluded := isNodeExcluded(n.Name, *stoppedNode, *excludeNodes)

		c, rpcC, err := dialClient(n.URL)
		online := false
		var bNum uint64
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			bNum, err = c.BlockNumber(ctx)
			cancel()
			if err == nil {
				online = true
				n.Client = c
				n.RPCClient = rpcC
				n.Online = true
			}
		}

		if isExcluded {
			if online {
				fmt.Printf("   • Node %s (%s): ⚪ EXCLUDED / VẪN ONLINE (Block %d - Bỏ qua theo kịch bản test)\n", n.Name, n.URL, bNum)
			} else {
				fmt.Printf("   • Node %s (%s): ⚪ STOPPED/SNAPSHOT (Đã tắt hoặc Snapshot theo kịch bản test)\n", n.Name, n.URL)
			}
			continue
		}

		if online {
			aliveNodes = append(aliveNodes, n)
			nodeHeights[n.Name] = bNum
			fmt.Printf("   • Node %s (%s): 🟢 ALIVE (Block %d)\n", n.Name, n.URL, bNum)
		} else {
			deadNodes = append(deadNodes, fmt.Sprintf("%s (%s)", n.Name, n.URL))
			fmt.Printf("   • Node %s (%s): 🔴 DEAD / MẤT KẾT NỐI!\n", n.Name, n.URL)
		}
	}

	if len(deadNodes) > 0 {
		if *requireAllAlive {
			log.Fatalf("❌ PHÁT HIỆN CÓ %d NODE BỊ CHẾT NGOÀI DỰ KIẾN: %s! BÀI TEST THẤT BẠI!", len(deadNodes), strings.Join(deadNodes, ", "))
		} else {
			fmt.Printf("⚠️ Cảnh báo: Có %d node bị chết ngoài dự kiến: %s\n", len(deadNodes), strings.Join(deadNodes, ", "))
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

	// 6. KIỂM TRA ZERO-FORK (SO SÁNH BLOCK HASH & STATE ROOT)
	if *checkFork && len(aliveNodes) >= 2 {
		if *stoppedNode != "" {
			fmt.Printf("\n🔍 [ZERO-FORK VERIFICATION] Kiểm tra đối chiếu Block Hash & StateRoot giữa %d node sống (ngoại trừ node %s đã dừng)...\n",
				len(aliveNodes), *stoppedNode)
		} else {
			fmt.Println("\n🔍 [ZERO-FORK VERIFICATION] Kiểm tra đối chiếu Block Hash & StateRoot giữa các node sống...")
		}

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
