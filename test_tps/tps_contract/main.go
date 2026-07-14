//go:build ignore

package main

import (
	"path/filepath"
	"sort"

	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bufio"

	"context"
	"crypto/ecdsa"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"tool-test/pkg/bls"
	"tool-test/pkg/client-tcp/command"
	c_config "tool-test/pkg/client-tcp/config"
	p_common "tool-test/pkg/common"
	"tool-test/pkg/logger"
	pb "tool-test/pkg/proto"
	"tool-test/pkg/transaction"
	"tool-test/test_tps/tps_blast_cc/rpc"
)

// AccountInfo from generated_keys.json
type AccountInfo struct {
	Index      int    `json:"index"`
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
}

// ABI definitions
const bytecodeHex = "6080604052348015600e575f5ffd5b506102a18061001c5f395ff3fe608060405234801561000f575f5ffd5b506004361061003f575f3560e01c80633ccc05221461004357806354fe9fd714610073578063cc8a55e2146100a3575b5f5ffd5b61005d600480360381019061005891906101ba565b6100bf565b60405161006a91906101fd565b60405180910390f35b61008d600480360381019061008891906101ba565b610104565b60405161009a91906101fd565b60405180910390f35b6100bd60048036038101906100b89190610240565b610118565b005b5f5f5f8373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f20549050919050565b5f602052805f5260405f205f915090505481565b805f5f3373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f208190555050565b5f5ffd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f61018982610160565b9050919050565b6101998161017f565b81146101a3575f5ffd5b50565b5f813590506101b481610190565b92915050565b5f602082840312156101cf576101ce61015c565b5b5f6101dc848285016101a6565b91505092915050565b5f819050919050565b6101f7816101e5565b82525050565b5f6020820190506102105f8301846101ee565b92915050565b61021f816101e5565b8114610229575f5ffd5b50565b5f8135905061023a81610216565b92915050565b5f602082840312156102555761025461015c565b5b5f6102628482850161022c565b9150509291505056fea264697066735822122060ccde915bc46853e9554ef38a5243f689e643d9cdfc22887eeb4f938129cf1864736f6c63430008230033"
const abiString = `[{"inputs":[{"internalType":"address","name":"user","type":"address"}],"name":"getValue","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"val","type":"uint256"}],"name":"updateState","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"values","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`

func waitReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	for {
		receipt, err := client.TransactionReceipt(context.Background(), txHash)
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			return receipt, nil
		}
		if err != nil && err.Error() != "not found" {
			return nil, err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	nonce, err := client.PendingNonceAt(context.Background(), from)
	if err != nil {
		return nil, err
	}
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil {
		gasPrice = big.NewInt(1000000000)
	}
	gasLimit, err := client.EstimateGas(context.Background(), ethereum.CallMsg{From: from, Data: bytecode})
	if err != nil {
		gasLimit = 5000000
	} else {
		gasLimit += 50000
	}
	tx := types.NewContractCreation(nonce, big.NewInt(0), gasLimit, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return nil, err
	}
	if err := client.SendTransaction(context.Background(), signedTx); err != nil {
		return nil, err
	}
	receipt, err := waitReceipt(client, signedTx.Hash())
	if err != nil {
		return nil, err
	}
	if receipt.Status != 1 {
		return nil, fmt.Errorf("deploy reverted")
	}
	return &receipt.ContractAddress, nil
}

func ts() string {
	return time.Now().Format("15:04:05.000")
}

var txHashMap sync.Map // Map[string]string -> maps TxHashHex (lowercase) to SenderAddress (hex)

// rawWriter wraps a raw TCP connection (same as tps_blast)
type rawWriter struct {
	conn         net.Conn
	writer       *bufio.Writer
	addr         string
	version      string
	toAddrHex    string
	rpcPool      []*rpc.RPCClient     // injected for nonce divergence check
	nonceChecker func(addrHex string) // callback khi invalid nonce xảy ra
}

func newRawWriter(addr, version, toAddrHex string) (*rawWriter, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	rw := &rawWriter{
		conn:      conn,
		writer:    bufio.NewWriterSize(conn, 4*1024*1024),
		addr:      addr,
		version:   version,
		toAddrHex: toAddrHex,
	}

	go func() {
		reader := bufio.NewReader(conn)
		for {
			lengthBuf := make([]byte, 8)
			if _, err := io.ReadFull(reader, lengthBuf); err != nil {
				return
			}
			msgLen := binary.LittleEndian.Uint64(lengthBuf)
			if msgLen > 10*1024*1024 {
				return
			}
			msgBuf := make([]byte, msgLen)
			if _, err := io.ReadFull(reader, msgBuf); err != nil {
				return
			}
			var msg pb.Message
			if err := proto.Unmarshal(msgBuf, &msg); err == nil && msg.Header != nil {
				if msg.Header.Command == "TransactionError" {
					var txErr pb.TransactionHashWithError
					if proto.Unmarshal(msg.Body, &txErr) == nil {
						txHashHex := common.BytesToHash(txErr.Hash).Hex()
						senderAddr := "unknown"
						if val, ok := txHashMap.Load(strings.ToLower(txHashHex)); ok {
							senderAddr = val.(string)
						}
						fmt.Printf("\n[%s] ❌ SERVER REJECTED TX: %s (Sender: %s) | Node: %s | Code: %d | Msg: %s\n",
							ts(), txHashHex, senderAddr, rw.addr, txErr.Code, txErr.Description)
						// Nếu lỗi invalid nonce → trigger cross-check nonce divergence
						if strings.Contains(strings.ToLower(txErr.Description), "invalid nonce") {
							if rw.nonceChecker != nil {
								// Không block goroutine đọc — chạy check bất đồng bộ
								go rw.nonceChecker(txHashHex)
							}
						}
					}
				} else if msg.Header.Command != "Receipt" {
					fmt.Printf("\n[%s] 📩 SERVER: %s\n", ts(), msg.Header.Command)
				}
			}
		}
	}()

	return rw, nil
}

func (rw *rawWriter) sendRaw(cmd string, body []byte) error {
	toAddr := common.HexToAddress(rw.toAddrHex)
	msgProto := &pb.Message{
		Header: &pb.Header{
			Command:   cmd,
			Version:   rw.version,
			ToAddress: toAddr.Bytes(),
			ID:        uuid.New().String(),
		},
		Body: body,
	}
	b, err := proto.Marshal(msgProto)
	if err != nil {
		return err
	}
	lengthBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(lengthBuf, uint64(len(b)))
	rw.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := rw.writer.Write(lengthBuf); err != nil {
		return err
	}
	if _, err := rw.writer.Write(b); err != nil {
		return err
	}
	return nil
}

func (rw *rawWriter) flush() error { return rw.writer.Flush() }
func (rw *rawWriter) close() {
	if rw.conn != nil {
		rw.conn.Close()
	}
}

// checkNonceDivergence fetch nonce của 1 sample address từ tất cả RPC nodes
// để phát hiện node nào đang bị lệch state. Chỉ gọi khi xảy ra invalid nonce.
func checkNonceDivergence(rpcPool []*rpc.RPCClient, sampleAddr string, triggerInfo string) {
	if len(rpcPool) < 2 {
		// Chỉ 1 node → không có gì để so sánh
		return
	}

	type nodeNonce struct {
		endpoint string
		nonce    int64
		err      error
	}

	results := make([]nodeNonce, len(rpcPool))
	var wg sync.WaitGroup
	for i, rc := range rpcPool {
		wg.Add(1)
		go func(i int, rc *rpc.RPCClient) {
			defer wg.Done()
			as, err := rc.GetAccountState(sampleAddr)
			if err != nil {
				results[i] = nodeNonce{endpoint: rc.Endpoint, nonce: -1, err: err}
				return
			}
			if as == nil {
				results[i] = nodeNonce{endpoint: rc.Endpoint, nonce: -1, err: fmt.Errorf("nil state")}
				return
			}
			results[i] = nodeNonce{endpoint: rc.Endpoint, nonce: int64(as.Nonce)}
		}(i, rc)
	}
	wg.Wait()

	// Tìm nonce chuẩn (majority voting)
	nonceCount := make(map[int64]int)
	for _, r := range results {
		if r.err == nil {
			nonceCount[r.nonce]++
		}
	}
	majorityNonce := int64(-1)
	maxVotes := 0
	for n, cnt := range nonceCount {
		if cnt > maxVotes {
			maxVotes = cnt
			majorityNonce = n
		}
	}

	// In bảng so sánh
	fmt.Printf("\n╔══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  🔍 NONCE DIVERGENCE CHECK (triggered by: invalid nonce)\n")
	fmt.Printf("║  📋 Sample addr: %s\n", sampleAddr)
	fmt.Printf("║  ℹ️  Trigger: %s\n", triggerInfo)
	fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n")

	hasDivergence := false
	for i, r := range results {
		if r.err != nil {
			fmt.Printf("║  Node[%d] %-35s  nonce=ERROR (%v)\n", i, r.endpoint, r.err)
			continue
		}
		status := "✅ OK"
		if r.nonce != majorityNonce {
			status = fmt.Sprintf("⚠️  LỆCH! (majority=%d)", majorityNonce)
			hasDivergence = true
		}
		fmt.Printf("║  Node[%d] %-35s  nonce=%-6d  %s\n", i, r.endpoint, r.nonce, status)
	}

	if hasDivergence {
		fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n")
		fmt.Printf("║  ⚠️  PHÁT HIỆN LỆCH NONCE GIỮA CÁC NODE! Majority nonce=%d\n", majorityNonce)
		fmt.Printf("║     → Nguyên nhân: sub-node bị replication lag hoặc chết.\n")
		fmt.Printf("║     → Kiểm tra logs: /consensus/metanode/logs/node_*/\n")
	} else {
		fmt.Printf("║  ✅ Tất cả node đồng thuận nonce=%d. Có thể do race condition.\n", majorityNonce)
	}
	fmt.Printf("╚══════════════════════════════════════════════════════════════╝\n")
}

func logErrorToFile(msg string) {
	f, err := os.OpenFile("blast_cc_errors.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	ts := time.Now().Format("2006-01-02 15:04:05")
	f.WriteString(fmt.Sprintf("[%s] %s\n", ts, msg))
}

func getSystemIPInfo() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "Unknown"
	}

	var localIPs []string
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					localIPs = append(localIPs, ipnet.IP.String())
				}
			}
		}
	}
	localIPStr := "Unknown"
	if len(localIPs) > 0 {
		localIPStr = strings.Join(localIPs, ", ")
	}

	publicIP := "Unknown"
	client := http.Client{
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get("https://api.ipify.org")
	if err == nil {
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			publicIP = strings.TrimSpace(string(body))
		}
	}

	if publicIP != "Unknown" {
		return fmt.Sprintf("%s (Static/Private IP: %s, Public IP: %s)", hostname, localIPStr, publicIP)
	}
	return fmt.Sprintf("%s (Static/Private IP: %s)", hostname, localIPStr)
}

func sendTelegramAlert(message string, testName string) {
	if os.Getenv("MTN_TELE_ALERT") != "true" {
		return
	}
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := "-1003867050625"
	ipInfo := getSystemIPInfo()

	fullMessage := fmt.Sprintf("⚠️  *[%s]* CẢNH BÁO !\n\n*Server:* `%s`\n\n%s", testName, ipInfo, message)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]string{
		"chat_id":    chatID,
		"text":       fullMessage,
		"parse_mode": "Markdown",
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		fmt.Printf("⚠️ Lỗi gửi Telegram alert: %v\n", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Printf("⚠️ Lỗi gửi Telegram alert, status code: %d\n", resp.StatusCode)
	}
}

func main() {
	os.MkdirAll("reports", 0755)
	reportFilename := fmt.Sprintf("reports/tps_report_%s.md", time.Now().Format("20060102_150405"))
	cleanupReports()
	var (
		configPath  string
		keysFile    string
		count       int
		batchSize   int
		sleepMs     int
		nodeAddr    string
		rpcAddr     string
		waitSecs    int
		recipient   string
		destId      int
		amountWei   string
		numRounds   int
		loadBalance bool
		verify      bool
		epochWait   int
		targetNode  int
		trace       bool
		tpsTarget   int
	)

	flag.StringVar(&configPath, "config", "./config.json", "Client config")
	flag.StringVar(&keysFile, "keys", "../gen_spam_keys/generated_keys.json", "Generated keys JSON")
	flag.IntVar(&count, "count", 10000, "Number of lockAndBridge TXs")
	flag.IntVar(&batchSize, "batch", 500, "Batch size")
	flag.IntVar(&sleepMs, "sleep", 0, "Sleep between batches (ms)")
	flag.StringVar(&nodeAddr, "node", "", "Override node TCP address")
	flag.StringVar(&rpcAddr, "rpc", "", "RPC URL for verification")
	flag.IntVar(&waitSecs, "wait", 600, "Max seconds to wait for chain processing")
	flag.StringVar(&recipient, "recipient", "0xbF2b4B9b9dFB6d23F7F0FC46981c2eC89f94A9F2", "Recipient address")
	flag.IntVar(&destId, "dest", 2, "Destination chain ID")
	flag.StringVar(&amountWei, "amount", "100", "Amount in wei (default: 1 ETH)")
	flag.IntVar(&numRounds, "rounds", 1, "Number of benchmark rounds")
	flag.BoolVar(&loadBalance, "load_balance", false, "Round-robin transactions across all connection_node_* in config")
	flag.BoolVar(&verify, "verify", false, "After each round, check recipient balance to confirm TXs landed")
	flag.IntVar(&epochWait, "epoch-wait", 300, "Max seconds to wait for epoch transition (0 = disable epoch wait)")
	flag.IntVar(&targetNode, "target-node", 0, "Target node index (0 to 3) to send transactions to")
	flag.BoolVar(&trace, "trace", true, "Enable fetching block traces at the end of the round")
	flag.IntVar(&tpsTarget, "tps-target", 0, "Target TPS for paced injection (0 = disable pacing)")
	flag.Parse()

	logger.SetConfig(&logger.LoggerConfig{Flag: 0})

	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println("  🔥 TPS BLAST — Parallel Smart Contract Calls")
	fmt.Println("═══════════════════════════════════════════════════")

	// Load config
	configIface, err := c_config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	config := configIface.(*c_config.ClientConfig)

	// Override config based on target-node if specified
	if raw, err := os.ReadFile(configPath); err == nil {
		var rawCfg map[string]interface{}
		if json.Unmarshal(raw, &rawCfg) == nil {
			tcpKey := "parent_connection_address"
			if targetNode > 0 {
				tcpKey = fmt.Sprintf("connection_node_%d", targetNode)
			}
			if v, ok := rawCfg[tcpKey].(string); ok && v != "" {
				config.ParentConnectionAddress = v
				fmt.Printf("🎯 [CONFIG OVERRIDE] Target Node: %d (TCP: %s)\n", targetNode, v)
			}
			if !loadBalance {
				rpcKey := fmt.Sprintf("rpc_%d", targetNode)
				if v, ok := rawCfg[rpcKey].(string); ok && v != "" {
					rpcAddr = v
					fmt.Printf("🎯 [CONFIG OVERRIDE] Target RPC: %s\n", v)
				}
			}
		}
	}

	var pKey p_common.PrivateKey
	copy(pKey[:], config.PrivateKey())

	chainId := config.ChainId

	// Load keys
	keysData, err := os.ReadFile(keysFile)
	if err != nil {
		log.Fatalf("Cannot read keys file %s: %v", keysFile, err)
	}
	var accounts []AccountInfo
	if err := json.Unmarshal(keysData, &accounts); err != nil {
		log.Fatalf("Cannot parse keys file: %v", err)
	}
	fmt.Printf("  📋 Loaded %d accounts from %s\n", len(accounts), keysFile)

	var toSend []AccountInfo
	if count > len(accounts) {
		toSend = make([]AccountInfo, 0, count)
		for i := 0; i < count; i++ {
			toSend = append(toSend, accounts[i%len(accounts)])
		}
	} else {
		toSend = accounts[:count]
	}

	fmt.Printf("  📊 TXs to send: %d\n", len(toSend))
	fmt.Printf("  📍 Recipient: %s\n", recipient)
	fmt.Printf("  🆔 DestinationId: %d\n", destId)
	fmt.Printf("  💰 Amount: %s wei\n", amountWei)

	// ── Build RPC pool: rpc_1, rpc_2, rpc_3 từ config (load balance nonce fetch) ──
	// Đọc raw config để lấy rpc_1/rpc_2/rpc_3
	var rpcPool []*rpc.RPCClient
	if raw, err := os.ReadFile(configPath); err == nil {
		var rawCfg map[string]interface{}
		if json.Unmarshal(raw, &rawCfg) == nil {
			// Thêm rpc_0, rpc_1, rpc_2, rpc_3, ... theo thứ tự
			for i := 0; i <= 10; i++ {
				// Nếu load_balance = false, chỉ sử dụng rpc_<targetNode>
				if !loadBalance && i != targetNode {
					continue
				}

				key := fmt.Sprintf("rpc_%d", i)
				if v, ok := rawCfg[key].(string); ok && v != "" {
					url := v
					if !strings.HasPrefix(url, "http") {
						url = "http://" + url
					}
					rpcPool = append(rpcPool, rpc.NewRPCClient(url))

					if !loadBalance {
						fmt.Printf("  🌐 Chế độ Single Node IP (RPC): %s\n", url)
					} else {
						fmt.Printf("  🌐 RPC pool [%d]: %s\n", i, url)
					}
				}
			}
		}
	}

	// Fallback: nếu không có rpc_* trong config, dùng rpcAddr như cũ
	if len(rpcPool) == 0 {
		if rpcAddr == "" {
			targetAddr := config.GetParentConnectionAddress()
			if nodeAddr != "" {
				targetAddr = nodeAddr
			}
			configHost := targetAddr
			if idx := strings.LastIndex(configHost, ":"); idx >= 0 {
				configHost = configHost[:idx]
			}
			rpcAddr = configHost + ":8757"
		}
		rpcUrl := rpcAddr
		if !strings.HasPrefix(rpcUrl, "http") {
			rpcUrl = "http://" + rpcUrl
		}
		rpcPool = append(rpcPool, rpc.NewRPCClient(rpcUrl))
		fmt.Printf("  🌐 RPC pool [fallback]: %s\n", rpcUrl)
	}

	// rpcClient dùng cho block polling (luôn dùng pool[0])
	rpcClient := rpcPool[0]

	// Round-robin counter cho nonce fetching
	var rpcPoolIdx int64

	// fetchNonce: luôn dùng rcPool[0] (tức là node master/local) để lấy nonce
	// Việc dùng round-robin rpcPool (poolSize > 1) sẽ gây ra lỗi "invalid nonce" vì
	// các sub-node thường bị lag 1 chút (replication lag). Khi lấy state từ sub-node bị lag,
	// nonce trả về sẽ là nonce cũ.
	fetchNonce := func(addr string, expectedNonce int64) (uint64, error) {
		poolSize := len(rpcPool)

		// Pick node once — all retries stay on the same node
		// This ensures consistency: the node we ask is the node we wait for
		idx := atomic.AddInt64(&rpcPoolIdx, 1) % int64(poolSize)
		rc := rpcPool[idx]

		var lastErr error
		for retry := 0; retry <= 60; retry++ {
			as, err := rc.GetAccountState(addr)
			if err != nil {
				lastErr = err
				if retry < 60 {
					time.Sleep(1 * time.Second)
					continue
				}
				return 0, err
			}
			if as == nil {
				lastErr = fmt.Errorf("node[%d] returned nil state", idx)
				if retry < 60 {
					time.Sleep(1 * time.Second)
					continue
				}
				return 0, lastErr
			}

			fetchedNonce := int64(as.Nonce)
			if expectedNonce >= 0 && fetchedNonce < expectedNonce {
				lastErr = fmt.Errorf("node[%d] returned stale nonce %d < expected %d", idx, fetchedNonce, expectedNonce)
				if retry < 60 {
					time.Sleep(1 * time.Second)
					continue
				}
				// All retries exhausted -> Return error
				return 0, lastErr
			}

			// Log 5 cái đầu để debug
			count := atomic.LoadInt64(&rpcPoolIdx)
			if count <= 5 {
				fmt.Printf("      DEBUG: Account %s => Nonce %d (from %s)\n", addr, as.Nonce, rc.Endpoint)
			}

			return uint64(as.Nonce), nil
		}
		return 0, lastErr
	}

	// Fetch nonce for ALL accounts concurrently (load-balanced conditional)
	fmt.Printf("  🔍 Fetching nonces for %d accounts (pool size: %d RPC nodes)...\n", len(toSend), len(rpcPool))
	nonceMap := make(map[string]uint64) // address -> nonce
	var nonceMu sync.Mutex
	var nonceWg sync.WaitGroup
	nonceCh := make(chan int, len(toSend))
	nonceWorkers := 50
	var nonceFetched int64
	var nonceErrors int64

	// Collect sample errors for summary
	var nonceErrMu sync.Mutex
	nonceErrSamples := make(map[string]int) // error string -> count
	const maxLoggedErrors = 10
	var loggedErrors int64

	for w := 0; w < nonceWorkers; w++ {
		nonceWg.Add(1)
		go func() {
			defer nonceWg.Done()
			for idx := range nonceCh {
				if atomic.LoadInt64(&nonceErrors) > 0 {
					continue // fast-fail if another worker failed
				}
				acc := toSend[idx]
				nonce, err := fetchNonce(acc.Address, -1)
				if err == nil {
					nonceMu.Lock()
					nonceMap[acc.Address] = nonce
					nonceMu.Unlock()
					atomic.AddInt64(&nonceFetched, 1)
				} else {
					errStr := err.Error()
					// Log first N errors immediately to stdout (clear of progress line)
					if atomic.AddInt64(&loggedErrors, 1) <= maxLoggedErrors {
						fmt.Printf("\n    ❌ [NONCE ERROR] addr=%s err=%v\n", acc.Address, err)
					}
					logErrorToFile(fmt.Sprintf("[Round 1] [NONCE ERROR] addr=%s err=%v", acc.Address, err))
					// Collect unique error types for summary
					nonceErrMu.Lock()
					nonceErrSamples[errStr]++
					nonceErrMu.Unlock()
					atomic.AddInt64(&nonceErrors, 1)
				}
				done := atomic.LoadInt64(&nonceFetched) + atomic.LoadInt64(&nonceErrors)
				if done%100 == 0 || done == int64(len(toSend)) {
					fmt.Printf("\r    ⏳ Progress: %d/%d nonces fetched (errors: %d)... ", done, len(toSend), atomic.LoadInt64(&nonceErrors))
				}
			}
		}()
	}
	for i := range toSend {
		nonceCh <- i
	}
	close(nonceCh)
	nonceWg.Wait()
	fmt.Printf("\n  ✅ Nonces fetched: %d ok, %d errors\n", nonceFetched, nonceErrors)
	if nonceErrors > 0 {
		var activeRPCEndpoints []string
		for _, rc := range rpcPool {
			activeRPCEndpoints = append(activeRPCEndpoints, rc.Endpoint)
		}
		errMsg := fmt.Sprintf("Thất bại khi lấy nonce từ các RPC Node (%s). Số lỗi: %d. Không thể tiếp tục!", strings.Join(activeRPCEndpoints, ", "), nonceErrors)
		fmt.Printf("\n❌ [ERROR] Đang gửi giao dịch tới Node TCP: %s, RPC: %v nhưng gặp lỗi: %s\n", config.ParentConnectionAddress, activeRPCEndpoints, errMsg)
		logErrorToFile(errMsg)
		os.Exit(1)
	}

	amount, _ := new(big.Int).SetString(amountWei, 10)
	_ = amount

	// Prepare ABI
	parsedABI, err := abi.JSON(strings.NewReader(abiString))
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	callData, err := parsedABI.Pack("updateState", big.NewInt(1))
	if err != nil {
		log.Fatalf("❌ Lỗi pack ABI: %v", err)
	}

	bytecode, err := hexutil.Decode("0x" + bytecodeHex)
	if err != nil {
		log.Fatalf("❌ Lỗi decode bytecode hex: %v", err)
	}

	// Deploy contract using first account
	ethClient, err := ethclient.Dial(rpcPool[0].Endpoint)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC: %v", err)
	}

	pk0Bytes, _ := hex.DecodeString(accounts[0].PrivateKey)
	pk0, _ := crypto.ToECDSA(pk0Bytes)
	from0 := crypto.PubkeyToAddress(pk0.PublicKey)

	fmt.Println("  🚀 Deploying contract with Account 0 via RPC...")
	contractAddr, err := deployContract(ethClient, pk0, int64(chainId), from0, bytecode)
	if err != nil {
		log.Fatalf("❌ Deploy thất bại: %v", err)
	}
	fmt.Printf("  📌 Contract deployed at: %s\n\n", contractAddr.Hex())

	// Force nonce refresh for account 0 since it just deployed a contract
	nonceMap[accounts[0].Address] = nonceMap[accounts[0].Address] + 1

	// Pre-build all TXs
	txTypeName := "Smart Contract Call"
	fmt.Printf("\n📦 Pre-building %d %s TXs...\n", len(toSend), txTypeName)
	buildStart := time.Now()

	type rawTx struct {
		bytes  []byte
		addr   string
		txHash common.Hash
		target common.Address
		amount *big.Int
	}
	var allTxs []rawTx
	var buildErrors int
	for _, acc := range toSend {
		privKeyBytes, err := hex.DecodeString(acc.PrivateKey)
		if err != nil {
			buildErrors++
			continue
		}
		ecdsaKey, err := crypto.ToECDSA(privKeyBytes)
		if err != nil {
			buildErrors++
			continue
		}
		fromAddr := crypto.PubkeyToAddress(ecdsaKey.PublicKey)

		var targetContract common.Address
		var bCallData []byte

		// Generate a unique dummy address so each sender sends to an untouched recipient
		// This makes verification perfectly isolated and guarantees the balance must equal txAmount.
		targetContract = *contractAddr
		bCallData = callData
		txAmount := big.NewInt(0) // Contract call amount is 0

		// Get nonce for this account
		nonce, ok := nonceMap[acc.Address]
		nonceMap[acc.Address] = nonce + 1 // Increment for duplicate uses
		if !ok {
			// This happens when fetchNonce failed for this address earlier
			if buildErrors < 5 {
				fmt.Printf("  ⚠️  [BUILD SKIP] addr=%s — nonce not found (fetch failed earlier), skipping TX build\n", acc.Address)
			} else if buildErrors == 5 {
				fmt.Printf("  ⚠️  [BUILD SKIP] ... (further skipped addresses suppressed)\n")
			}
			buildErrors++
			continue
		}

		// txAmount := amount (handled above)

		internalTx := transaction.NewTransaction(
			fromAddr,
			targetContract,
			txAmount,
			1000000, // maxGas
			1000000, // maxGasPrice
			0,       // maxPriorityFee
			bCallData,
			[][]byte{},
			common.Hash{},
			common.Hash{},
			nonce,
			chainId,
		)

		// Sign with BLS key
		var accPKey p_common.PrivateKey
		copy(accPKey[:], privKeyBytes)
		internalTx.SetSign(accPKey)

		bTx, err := internalTx.Marshal()
		if err != nil {
			buildErrors++
			continue
		}

		txHash := internalTx.ToEthTransaction().Hash()
		txHashMap.Store(strings.ToLower(txHash.Hex()), acc.Address)
		txHashMap.Store(strings.ToLower(internalTx.Hash().Hex()), acc.Address)

		rawHash := crypto.Keccak256Hash(bTx)
		txHashMap.Store(strings.ToLower(rawHash.Hex()), acc.Address)

		allTxs = append(allTxs, rawTx{
			bytes:  bTx,
			addr:   acc.Address,
			txHash: txHash,
			target: targetContract,
			amount: txAmount,
		})
	}

	buildDuration := time.Since(buildStart)
	fmt.Printf("  ✅ Built %d TXs in %s (%.0f tx/s), %d errors\n",
		len(allTxs), buildDuration.Round(time.Millisecond),
		float64(len(allTxs))/buildDuration.Seconds(), buildErrors)

	// if buildErrors > 0 {
	// 	log.Fatalf("❌ DỪNG CHƯƠNG TRÌNH: Quá trình build giao dịch gặp %d lỗi! Vui lòng kiểm tra log để debug.", buildErrors)
	// }
	// if len(allTxs) == 0 {
	// 	log.Fatalf("❌ DỪNG CHƯƠNG TRÌNH: Không có giao dịch nào được build thành công!")
	// }

	targetAddresses := []string{config.GetParentConnectionAddress()}
	if !loadBalance {
		fmt.Printf("\n  📡 Chế độ Single Node IP (TCP): %s\n", config.GetParentConnectionAddress())
	}

	if nodeAddr != "" {
		targetAddresses = strings.Split(nodeAddr, ",")
	} else if loadBalance {
		// Read raw config for extra load-balancer nodes only if load_balance flag is true
		if raw, err := os.ReadFile(configPath); err == nil {
			var rawCfg map[string]interface{}
			if err := json.Unmarshal(raw, &rawCfg); err == nil {
				for k, v := range rawCfg {
					if strings.HasPrefix(k, "connection_node_") {
						if strV, ok := v.(string); ok {
							targetAddresses = append(targetAddresses, strV)
						}
					}
				}
			}
		}
	}

	toAddrHex := config.ParentAddress
	version := config.Version()

	randomPrivKey, _ := crypto.GenerateKey()
	clientAddress := crypto.PubkeyToAddress(randomPrivKey.PublicKey)
	_ = bls.NewKeyPair(config.PrivateKey()) // keep import

	// Throttle: chỉ check divergence tối đa 1 lần mỗi 5 giây
	var lastNonceCheckMs atomic.Int64

	// nonceCheckerFn được inject vào mỗi rawWriter.
	// triggerInfo = txHashHex của TX bị reject (để log)
	nonceCheckerFn := func(triggerInfo string) {
		if len(rpcPool) < 2 || len(toSend) == 0 {
			return
		}
		nowMs := time.Now().UnixMilli()
		last := lastNonceCheckMs.Load()
		// throttle 5000ms
		if nowMs-last < 5000 {
			return
		}
		if !lastNonceCheckMs.CompareAndSwap(last, nowMs) {
			return // goroutine khác đã giằng lấy quyền check
		}
		// lấy sample = addr đầu tiên trong toSend
		sampleAddr := toSend[0].Address
		checkNonceDivergence(rpcPool, sampleAddr, triggerInfo)
	}

	reconnectNode := func(targetAddr string) *rawWriter {
		for attempt := 1; attempt <= 30; attempt++ {
			fmt.Printf("[%s]   🔌 Connecting to %s (attempt %d)...\n", ts(), targetAddr, attempt)
			rw, err := newRawWriter(targetAddr, version, toAddrHex)
			if err != nil {
				time.Sleep(1 * time.Second)
				continue
			}
			// Inject rpcPool + nonceChecker vào rawWriter
			rw.rpcPool = rpcPool
			rw.nonceChecker = nonceCheckerFn
			initMsg := &pb.InitConnection{
				Address: clientAddress.Bytes(),
				Type:    config.NodeType(),
				Replace: true,
			}
			initBody, _ := proto.Marshal(initMsg)
			if err := rw.sendRaw(command.InitConnection, initBody); err != nil {
				rw.close()
				time.Sleep(1 * time.Second)
				continue
			}
			if err := rw.flush(); err != nil {
				rw.close()
				time.Sleep(1 * time.Second)
				continue
			}
			fmt.Printf("[%s]   ✅ Connected to %s and InitConnection sent\n", ts(), targetAddr)
			return rw
		}
		var activeRPCEndpoints []string
		for _, rc := range rpcPool {
			activeRPCEndpoints = append(activeRPCEndpoints, rc.Endpoint)
		}
		fmt.Printf("\n[%s] ❌ [ERROR] Đang kết nối tới Node TCP: %s, RPC: %v nhưng gặp lỗi: Không thể kết nối sau 30 lần thử (Node bị hỏng/tắt). Dừng chương trình ngay lập tức!\n", ts(), targetAddr, activeRPCEndpoints)
		os.Exit(1)
		return nil
	}

	type activeClient struct {
		addr string
		rw   *rawWriter
	}

	connectAll := func() []*activeClient {
		var clients []*activeClient
		for _, addr := range targetAddresses {
			if rw := reconnectNode(addr); rw != nil {
				clients = append(clients, &activeClient{addr: addr, rw: rw})
			}
		}
		if len(clients) == 0 {
			fmt.Println("  ❌ Failed to connect to any node.")
			os.Exit(1)
		}
		return clients
	}

	var allRoundTPS []float64

	type RoundSummary struct {
		Round         int     `json:"round"`
		StartBlock    uint64  `json:"startBlock"`
		EndBlock      uint64  `json:"endBlock"`
		BlockCount    int     `json:"blockCount"`
		TxCount       int     `json:"txCount"`
		MaxTxInBlock  int     `json:"maxTxInBlock"`
		TPS           float64 `json:"tps"`
		ProcessingSec float64 `json:"processingSec"`
	}
	var roundSummaries []RoundSummary

	clients := connectAll()
	defer func() {
		for _, c := range clients {
			if c.rw != nil {
				c.rw.close()
			}
		}
	}()

	for round := 1; round <= numRounds; round++ {
		if numRounds > 1 {
			fmt.Printf("\n╔═══════════════════════════════════════════════════╗\n")
			fmt.Printf("║  🔄 ROUND %d / %d\n", round, numRounds)
			fmt.Printf("╚═══════════════════════════════════════════════════╝\n")
		}

		// ── Re-fetch nonces + rebuild TXs for rounds > 1 ──
		if round > 1 {
			// Wait for chain to fully process previous round before re-fetching nonces
			fmt.Printf("  ⏳ Waiting 20s for chain to finalize previous round...\n")
			time.Sleep(20 * time.Second)
			fmt.Printf("  🔍 Re-fetching nonces for %d accounts (pool: %d nodes)...\n", len(toSend), len(rpcPool))
			oldNonceMap := nonceMap
			nonceMap = make(map[string]uint64)
			var refetchOk, refetchErr int64
			var refetchMu sync.Mutex
			var refetchWg sync.WaitGroup
			refetchCh := make(chan int, len(toSend))
			atomic.StoreInt64(&loggedErrors, 0)
			for w := 0; w < 50; w++ {
				refetchWg.Add(1)
				go func() {
					defer refetchWg.Done()
					for idx := range refetchCh {
						if atomic.LoadInt64(&refetchErr) > 0 {
							continue // fast-fail if another worker failed
						}
						acc := toSend[idx]
						exp := int64(-1)
						if oldN, ok := oldNonceMap[acc.Address]; ok {
							exp = int64(oldN)
						}
						nonce, err := fetchNonce(acc.Address, exp)
						if err == nil {
							refetchMu.Lock()
							nonceMap[acc.Address] = nonce
							refetchMu.Unlock()
							atomic.AddInt64(&refetchOk, 1)
						} else {
							atomic.AddInt64(&refetchErr, 1)
							prevTxHash := "unknown"
							for _, tx := range allTxs {
								if strings.EqualFold(tx.addr, acc.Address) {
									prevTxHash = tx.txHash.Hex()
									break
								}
							}
							if atomic.AddInt64(&loggedErrors, 1) <= maxLoggedErrors {
								fmt.Printf("\n    ❌ [RE-FETCH NONCE ERROR] addr=%s err=%v | PrevTxHash: %s\n", acc.Address, err, prevTxHash)
							}
							logErrorToFile(fmt.Sprintf("[Round %d] [RE-FETCH NONCE ERROR] addr=%s err=%v | PrevTxHash: %s", round, acc.Address, err, prevTxHash))
						}
						done := atomic.LoadInt64(&refetchOk) + atomic.LoadInt64(&refetchErr)
						if done%2000 == 0 || done == int64(len(toSend)) {
							fmt.Printf("\r    Fetched %d/%d nonces (errors: %d)   ", done, len(toSend), atomic.LoadInt64(&refetchErr))
						}
					}
				}()
			}
			for i := range toSend {
				refetchCh <- i
			}
			close(refetchCh)
			refetchWg.Wait()
			fmt.Printf("\n  ✅ Nonces re-fetched: %d ok, %d errors\n", refetchOk, refetchErr)

			if refetchErr > 0 {
				var activeRPCEndpoints []string
				for _, rc := range rpcPool {
					activeRPCEndpoints = append(activeRPCEndpoints, rc.Endpoint)
				}
				errMsg := fmt.Sprintf("Thất bại khi lấy lại nonce từ RPC (%s). Số lỗi: %d. Không thể tiếp tục Round %d!", strings.Join(activeRPCEndpoints, ", "), refetchErr, round)
				fmt.Printf("\n❌ [ERROR] %s\n", errMsg)
				logErrorToFile(errMsg)
				os.Exit(1)
			}

			// Rebuild all TXs with new nonces
			fmt.Printf("\n📦 Re-building %d %s TXs...\n", len(toSend), txTypeName)
			rebuildStart := time.Now()
			allTxs = nil
			var rebuildErrors int
			for _, acc := range toSend {
				privKeyBytes, err := hex.DecodeString(acc.PrivateKey)
				if err != nil {
					rebuildErrors++
					continue
				}
				ecdsaKey, err := crypto.ToECDSA(privKeyBytes)
				if err != nil {
					rebuildErrors++
					continue
				}
				fromAddr := crypto.PubkeyToAddress(ecdsaKey.PublicKey)

				var targetContract common.Address
				var bCallData []byte

				targetContract = *contractAddr
				bCallData = callData
				txAmount := big.NewInt(0) // Contract call amount is 0

				nonce, ok := nonceMap[acc.Address]
				nonceMap[acc.Address] = nonce + 1 // Increment for duplicate uses
				if !ok {
					if rebuildErrors < 5 {
						fmt.Printf("  ⚠️  [REBUILD SKIP] addr=%s — nonce not found (fetch failed earlier)\n", acc.Address)
					} else if rebuildErrors == 5 {
						fmt.Printf("  ⚠️  [REBUILD SKIP] ... (further skipped addresses suppressed)\n")
					}
					rebuildErrors++
					continue
				}

				// txAmount := amount (handled above)

				internalTx := transaction.NewTransaction(
					fromAddr, targetContract, txAmount,
					1000000, 1000000, 0,
					bCallData, [][]byte{},
					common.Hash{}, common.Hash{},
					nonce, chainId,
				)
				var accPKey p_common.PrivateKey
				copy(accPKey[:], privKeyBytes)
				internalTx.SetSign(accPKey)
				bTx, err := internalTx.Marshal()
				if err != nil {
					rebuildErrors++
					continue
				}
				txHash := internalTx.ToEthTransaction().Hash()
				txHashMap.Store(strings.ToLower(txHash.Hex()), acc.Address)
				txHashMap.Store(strings.ToLower(internalTx.Hash().Hex()), acc.Address)
				rawHash := crypto.Keccak256Hash(bTx)
				txHashMap.Store(strings.ToLower(rawHash.Hex()), acc.Address)

				allTxs = append(allTxs, rawTx{
					bytes:  bTx,
					addr:   acc.Address,
					txHash: txHash,
					target: targetContract,
					amount: txAmount,
				})
			}
			rebuildDuration := time.Since(rebuildStart)
			fmt.Printf("  ✅ Re-built %d TXs in %s (%.0f tx/s), %d errors\n",
				len(allTxs), rebuildDuration.Round(time.Millisecond),
				float64(len(allTxs))/rebuildDuration.Seconds(), rebuildErrors)

			if rebuildErrors > 0 {
				fmt.Printf("  ⚠️ CẢNH BÁO: Quá trình re-build giao dịch gặp %d lỗi! Bỏ qua và tiếp tục...\n", rebuildErrors)
			}
			if len(allTxs) == 0 {
				log.Fatalf("❌ DỪNG CHƯƠNG TRÌNH: Không có giao dịch nào được re-build thành công!")
			}
		}

		startBlock, _ := rpcClient.GetBlockNumber()
		startEpochBeforeBlast := uint64(0)
		if blk, err := rpcClient.GetBlockByNumber(startBlock); err == nil && blk != nil {
			startEpochBeforeBlast = blk.Epoch
		}
		fmt.Printf("\n  🏁 Starting block: %d | Epoch: %d | Time: %s\n", startBlock, startEpochBeforeBlast, time.Now().Format("15:04:05.000"))

		// Batch and blast
		fmt.Printf("\n🔥 BLASTING %d %s TXs across %d nodes via SendTransactions...\n", len(allTxs), txTypeName, len(clients))
		fmt.Printf("   Epoch: %d | Start Time: %s | Batch size: %d, Sleep: %dms\n", startEpochBeforeBlast, time.Now().Format("15:04:05.000"), batchSize, sleepMs)

		var batchedMsgs [][]byte
		for i := 0; i < len(allTxs); i += batchSize {
			end := i + batchSize
			if end > len(allTxs) {
				end = len(allTxs)
			}
			var pbTxs []*pb.Transaction
			for j := i; j < end; j++ {
				txProto := &pb.Transaction{}
				if err := proto.Unmarshal(allTxs[j].bytes, txProto); err == nil {
					pbTxs = append(pbTxs, txProto)
				}
			}
			batchProto := &pb.Transactions{Transactions: pbTxs}
			batchBytes, err := proto.Marshal(batchProto)
			if err == nil {
				batchedMsgs = append(batchedMsgs, batchBytes)
			}
		}

		blastStart := time.Now()

		var wg sync.WaitGroup
		var totalSent int64

		for clientIdx, c := range clients {
			wg.Add(1)
			go func(cIdx int, client *activeClient) {
				defer wg.Done()
				for i := cIdx; i < len(batchedMsgs); i += len(clients) {
					batchBytes := batchedMsgs[i]

					// Check for STOP flag from block_hash_checker
					if _, err := os.Stat("/tmp/MTN_CHAIN_ERROR_STOP"); err == nil {
						reason, _ := os.ReadFile("/tmp/MTN_CHAIN_ERROR_STOP")
						errMsg := fmt.Sprintf("\n🛑 FATAL: Phát hiện lỗi chuỗi từ block_hash_checker: %s\n   -> DỪNG BLASTING NGAY LẬP TỨC!", string(reason))
						fmt.Println(errMsg)
						logErrorToFile(fmt.Sprintf("[Round %d] %s", round, errMsg))
						os.Exit(1)
					}

					if client.rw == nil {
						client.rw = reconnectNode(client.addr)
						if client.rw == nil {
							fmt.Printf("\n[%s]   ❌ Skipping batch %d due to reconnect failure on %s\n", ts(), i, client.addr)
							continue
						}
					}

					err := client.rw.sendRaw(command.SendTransactions, batchBytes)
					if err != nil {
						var activeRPCEndpoints []string
						for _, rc := range rpcPool {
							activeRPCEndpoints = append(activeRPCEndpoints, rc.Endpoint)
						}
						fmt.Printf("\n[%s] ⚠️  Gặp lỗi ghi (write error) lên Node TCP: %s tại Batch %d: %v — Đang kết nối lại...\n", ts(), client.addr, i, err)
						client.rw.close()
						client.rw = reconnectNode(client.addr)
						if client.rw != nil {
							errRetry := client.rw.sendRaw(command.SendTransactions, batchBytes)
							if errRetry != nil {
								fmt.Printf("\n[%s] ❌ [ERROR] Đang gửi giao dịch tới Node TCP: %s, RPC: %v nhưng gặp lỗi: Gửi Batch %d thất bại sau khi reconnect. Chi tiết: %v. Dừng chương trình!\n", ts(), client.addr, activeRPCEndpoints, i, errRetry)
								os.Exit(1)
							}
						} else {
							fmt.Printf("\n[%s] ❌ [ERROR] Đang gửi giao dịch tới Node TCP: %s, RPC: %v nhưng gặp lỗi: Kết nối lại thất bại tại Batch %d. Dừng chương trình!\n", ts(), client.addr, activeRPCEndpoints, i)
							os.Exit(1)
						}
					}

					if client.rw != nil {
						client.rw.flush()
					}

					currentSent := atomic.AddInt64(&totalSent, int64(batchSize))
					if currentSent > int64(len(allTxs)) {
						currentSent = int64(len(allTxs))
					}

					if currentSent%int64(batchSize*10) == 0 || currentSent >= int64(len(allTxs)) {
						elapsed := time.Since(blastStart)
						rate := float64(currentSent) / elapsed.Seconds()
						fmt.Printf("\r[%s]   📤 [%d/%d] %.0f tx/s | elapsed %s   ",
							ts(), currentSent, len(allTxs), rate, elapsed.Round(time.Millisecond))
					}

					if currentSent < int64(len(allTxs)) {
						if tpsTarget > 0 {
							// Calculate exact time we SHOULD have spent by now to maintain tpsTarget
							expectedElapsedSecs := float64(currentSent) / float64(tpsTarget)
							actualElapsedSecs := time.Since(blastStart).Seconds()
							if expectedElapsedSecs > actualElapsedSecs {
								pacingSleep := time.Duration((expectedElapsedSecs - actualElapsedSecs) * float64(time.Second))
								time.Sleep(pacingSleep)
							}
						} else if sleepMs > 0 {
							time.Sleep(time.Duration(sleepMs) * time.Millisecond)
						}
					}
				}
			}(clientIdx, c)
		}

		wg.Wait()

		for _, c := range clients {
			if c.rw != nil {
				c.rw.flush()
			}
		}

		blastDuration := time.Since(blastStart)
		injectionTPS := float64(len(allTxs)) / blastDuration.Seconds()
		fmt.Printf("\n\n  📤 Injected: %d TXs in %s | End Time: %s\n", len(allTxs), blastDuration.Round(time.Millisecond), time.Now().Format("15:04:05.000"))
		fmt.Printf("  🚀 Injection TPS: %.0f tx/s\n", injectionTPS)

		// ================= Poll for TX completion =================
		maxWait := time.Duration(waitSecs) * time.Second
		pollInterval := 5 * time.Millisecond

		// Build map of expected tx hashes to correctly count only our TXs
		expectedTxHashes := make(map[string]bool)
		hashMapping := make(map[string][]string)
		for _, tx := range allTxs {
			ethHashLower := strings.ToLower(tx.txHash.Hex())
			expectedTxHashes[ethHashLower] = true

			internalTx := &transaction.Transaction{}
			if err := internalTx.Unmarshal(tx.bytes); err == nil {
				pbHash := internalTx.Hash()
				pbHashLower := strings.ToLower(pbHash.Hex())
				expectedTxHashes[pbHashLower] = true

				rawHash := crypto.Keccak256Hash(tx.bytes)
				rawHashLower := strings.ToLower(rawHash.Hex())
				expectedTxHashes[rawHashLower] = true

				hashMapping[ethHashLower] = []string{pbHashLower, rawHashLower}
				hashMapping[pbHashLower] = []string{ethHashLower, rawHashLower}
				hashMapping[rawHashLower] = []string{ethHashLower, pbHashLower}
			}
		}

		var processingDuration time.Duration
		lastBlockNum := startBlock
		totalTxsInBlocks := uint64(0)
		seenAnyTx := false
		var firstTxBlockTime time.Time
		var lastTxBlockTime time.Time

		epochWaitStart := time.Now()
		startEpoch := startEpochBeforeBlast
		epochStartSet := true // startEpoch đã được thiết lập hợp lệ (dù = 0)
		epochTransitioned := false
		var processStart time.Time
		var timeoutStartEpoch uint64
		var currentMonitorBlock uint64 // block hiện tại để log vào cảnh báo
		lastProgressTime := time.Now()
		if epochWait <= 0 {
			epochTransitioned = true
			processStart = time.Now()
			lastProgressTime = time.Now()
			timeoutStartEpoch = startEpochBeforeBlast
		}
		_ = epochStartSet

		if epochWait > 0 {
			fmt.Printf("\n⏳ [%s] Bắt đầu quét block & chờ chuyển đổi epoch (tối đa %d giây). Epoch hiện tại: %d\n", time.Now().Format("15:04:05.000"), epochWait, startEpoch)
		} else {
			fmt.Printf("\n⏳ [%s] Bắt đầu quét block (timeout %s)...\n", time.Now().Format("15:04:05.000"), maxWait)
		}

		var lastEpochAlertTime time.Time
		var lastAlertTime time.Time // 2-minute stall alert for TX confirmation phase

		for {
			// Check for STOP flag from block_hash_checker
			if _, err := os.Stat("/tmp/MTN_CHAIN_ERROR_STOP"); err == nil {
				reason, _ := os.ReadFile("/tmp/MTN_CHAIN_ERROR_STOP")
				errMsg := fmt.Sprintf("\n🛑 FATAL: Phát hiện lỗi chuỗi từ block_hash_checker: %s\n   -> DỪNG POLLING NGAY LẬP TỨC!", string(reason))
				fmt.Println(errMsg)
				logErrorToFile(fmt.Sprintf("[Round %d] %s", round, errMsg))
				os.Exit(1)
			}

			// Check epoch transition timeout
			if !epochTransitioned && epochWait > 0 {
				if time.Since(epochWaitStart) > time.Duration(epochWait)*time.Second {
					if time.Since(lastEpochAlertTime) >= 60*time.Second {
						elapsedSec := int(time.Since(epochWaitStart).Seconds())
						pendingEpoch := len(expectedTxHashes)
						var activeTCPs []string
						for _, cl := range clients {
							activeTCPs = append(activeTCPs, cl.addr)
						}
						// ⏰ icon phân biệt: epoch timeout (khác ⚠️ stall)
						errMsg := fmt.Sprintf("\n⏰ EPOCH TIMEOUT: Quá %d giây không có epoch mới! (Đã chờ %d giây) (startEpoch: %d) | Đang ở block: %d | Pending TXs: %d | Nodes: %v | Time: %s",
							epochWait, elapsedSec, startEpoch, currentMonitorBlock, pendingEpoch, activeTCPs, time.Now().Format("15:04:05.000"))
						fmt.Println(errMsg)
						logErrorToFile(fmt.Sprintf("[Round %d] %s", round, errMsg))

						var missingTxs []string
						count := 0
						for idx, tx := range allTxs {
							txHashLower := strings.ToLower(tx.txHash.Hex())
							if expectedTxHashes[txHashLower] {
								clientAddr := "Unknown"
								if len(clients) > 0 && batchSize > 0 {
									clientAddr = clients[(idx/batchSize)%len(clients)].addr
								}
								missingTxs = append(missingTxs, fmt.Sprintf("%s (Node: %s)", tx.txHash.Hex(), clientAddr))
								count++
								if count >= 5 {
									break
								}
							}
						}

						teleMsg := fmt.Sprintf("⏰ Quá %d giây không có epoch mới! (Đã chờ %d giây) (startEpoch: %d) | Đang ở block: %d | Pending TXs: %d | Nodes: %v\n5 giao dịch chưa được đưa vào:\n%s",
							epochWait, elapsedSec, startEpoch, currentMonitorBlock, pendingEpoch, activeTCPs, strings.Join(missingTxs, "\n"))
						sendTelegramAlert(teleMsg, "tps_blast_cc")
						lastEpochAlertTime = time.Now()
					}
					// Bỏ break để chờ mãi và 1 phút báo 1 lần
				}
			}

			// Check TX confirmation timeout + periodic stall alert every 2 minutes
			if epochTransitioned && !processStart.IsZero() {
				stalledFor := time.Since(lastProgressTime)
				if stalledFor > maxWait {
					break
				}
				// Every 2 minutes without new confirmed TXs, print alert
				if stalledFor > 2*time.Minute && time.Since(lastAlertTime) >= 2*time.Minute {
					pendingCount := len(expectedTxHashes)
					var sample []string
					for idx, tx := range allTxs {
						txHashLower := strings.ToLower(tx.txHash.Hex())
						if expectedTxHashes[txHashLower] {
							clientAddr := "Unknown"
							if len(clients) > 0 && batchSize > 0 {
								clientAddr = clients[(idx/batchSize)%len(clients)].addr
							}
							sample = append(sample, fmt.Sprintf("%s (Node: %s)", tx.txHash.Hex(), clientAddr))
							if len(sample) >= 5 {
								break
							}
						}
					}
					totalElapsed := time.Since(processStart).Round(time.Second)
					var activeTCPs []string
					for _, cl := range clients {
						activeTCPs = append(activeTCPs, cl.addr)
					}
					aMsg := fmt.Sprintf("\n⚠️  [%s] STALL ALERT: %d giao dịch chưa phản hồi (Nodes: %v)\n   Tổng thời gian chờ kể từ epoch: %s | Không tiến triển (stalled): %s | Timeout còn lại: %s\n   5 giao dịch minh họa:\n   %s\n",
						time.Now().Format("15:04:05.000"),
						pendingCount,
						activeTCPs,
						totalElapsed,
						stalledFor.Round(time.Second),
						(maxWait - stalledFor).Round(time.Second),
						strings.Join(sample, "\n   "))
					fmt.Print(aMsg)
					teleMsg := fmt.Sprintf("%d giao dịch chưa phản hồi (Nodes: %v)\nTổng thời gian chờ kể từ epoch: %s | Không tiến triển: %s | Timeout còn lại: %s\n5 giao dịch minh họa:\n%s",
						pendingCount, activeTCPs, totalElapsed, stalledFor.Round(time.Second), (maxWait - stalledFor).Round(time.Second), strings.Join(sample, "\n"))
					sendTelegramAlert(teleMsg, "tps_blast_cc")
					lastAlertTime = time.Now()
				}
			}

			time.Sleep(pollInterval)

			currentBlockNum, err := rpcClient.GetBlockNumber()
			if err != nil {
				continue
			}
			currentMonitorBlock = currentBlockNum // cập nhật để log vào cảnh báo

			newTxs := uint64(0)
			nextLastBlockNum := lastBlockNum
			if currentBlockNum > lastBlockNum {
				maxBlocksToQuery := uint64(20)
				toBlockNum := currentBlockNum
				if toBlockNum > lastBlockNum+maxBlocksToQuery {
					toBlockNum = lastBlockNum + maxBlocksToQuery
				}
				numBlocks := toBlockNum - lastBlockNum

				type blockResult struct {
					bn  uint64
					blk *rpc.Block
					err error
				}
				ch := make(chan blockResult, numBlocks)
				var wg sync.WaitGroup

				for bn := lastBlockNum + 1; bn <= toBlockNum; bn++ {
					wg.Add(1)
					go func(blockNum uint64) {
						defer wg.Done()
						blk, err := rpcClient.GetBlockByNumber(blockNum)
						if err != nil {
							fmt.Printf("DEBUG: Block %d failed to fetch: %v\n", blockNum, err)
						} else {
							if blk == nil {
								fmt.Printf("DEBUG: Block %d is nil\n", blockNum)
							}
						}
						ch <- blockResult{bn: blockNum, blk: blk, err: err}
					}(bn)
				}
				wg.Wait()
				close(ch)

				results := make([]blockResult, 0, numBlocks)
				for res := range ch {
					results = append(results, res)
				}
				sort.Slice(results, func(i, j int) bool {
					return results[i].bn < results[j].bn
				})

				for _, res := range results {
					if res.err != nil || res.blk == nil {
						break
					}
					blk := res.blk
					bn := res.bn

					// FIX: chỉ gán startEpoch từ block nếu chưa được thiết lập hợp lệ
					if !epochStartSet {
						startEpoch = blk.Epoch
						epochStartSet = true
					}
					if epochWait > 0 && !epochTransitioned && blk.Epoch > startEpoch {
						fmt.Printf("\n  ✅ Đã chuyển sang epoch mới: %d → %d (tại block %d). Bắt đầu đếm giờ timeout chờ TX... | Time: %s\n", startEpoch, blk.Epoch, bn, time.Now().Format("15:04:05.000"))
						epochTransitioned = true
						processStart = time.Now()
						lastProgressTime = time.Now()
						timeoutStartEpoch = blk.Epoch
					}

					newTxsCount := uint64(0)
					for _, txHash := range blk.Transactions {
						txHashLower := strings.ToLower(txHash)
						if expectedTxHashes[txHashLower] {
							newTxsCount++
							delete(expectedTxHashes, txHashLower)
							if otherHashes, exists := hashMapping[txHashLower]; exists {
								for _, otherHash := range otherHashes {
									delete(expectedTxHashes, otherHash)
								}
							}
						}
					}

					if newTxsCount > 0 {
						blkTime := time.UnixMilli(int64(blk.Timestamp))
						if !seenAnyTx {
							firstTxBlockTime = blkTime
							seenAnyTx = true
						}
						lastTxBlockTime = blkTime
					}
					newTxs += newTxsCount
					nextLastBlockNum = bn
				}
			}

			if newTxs > 0 {
				lastProgressTime = time.Now()
			}
			totalTxsInBlocks += newTxs
			lastBlockNum = nextLastBlockNum

			pct := float64(totalTxsInBlocks) / float64(len(allTxs)) * 100
			if pct > 100 {
				pct = 100
			}

			var elapsedStr string
			if epochTransitioned && !processStart.IsZero() {
				elapsedStr = fmt.Sprintf("Timeout: %s/%s", time.Since(lastProgressTime).Round(time.Millisecond), maxWait)
			} else if epochWait > 0 {
				elapsedStr = fmt.Sprintf("Wait Epoch: %s/%ds", time.Since(epochWaitStart).Round(time.Millisecond), epochWait)
			} else {
				elapsedStr = fmt.Sprintf("Elapsed: %s", time.Since(epochWaitStart).Round(time.Millisecond))
			}

			fmt.Printf("\r[%s]   📡 [%s] Block: %d | TXs in blocks: %d/%d (%.0f%%) | +%d new   ",
				ts(), elapsedStr, currentBlockNum, totalTxsInBlocks, len(allTxs), pct, newTxs)

			// Stop immediately when all TXs confirmed
			if totalTxsInBlocks >= uint64(len(allTxs)) {
				if !processStart.IsZero() {
					processingDuration = time.Since(processStart)
				} else {
					processingDuration = time.Since(epochWaitStart)
				}
				fmt.Printf("\n  ✅ All %d/%d TXs confirmed in blocks | End Time: %s\n", totalTxsInBlocks, len(allTxs), time.Now().Format("15:04:05.000"))
				break
			}
		}

		if processingDuration == 0 {
			if !processStart.IsZero() {
				processingDuration = time.Since(processStart)
			} else {
				processingDuration = time.Since(epochWaitStart)
			}

			// Get current block number + epoch at timeout
			currentEpoch := uint64(0)
			currentBlockAtTimeout := uint64(0)
			if bn, err := rpcClient.GetBlockNumber(); err == nil {
				currentBlockAtTimeout = bn
				if blk, err := rpcClient.GetBlockByNumber(bn); err == nil && blk != nil {
					currentEpoch = blk.Epoch
				}
			}

			epochInfo := fmt.Sprintf(" (Block hiện tại: %d | Epoch ban đầu: %d | Epoch tính giờ: %d | Epoch hiện tại: %d)",
				currentBlockAtTimeout, startEpochBeforeBlast, timeoutStartEpoch, currentEpoch)

			if !seenAnyTx {
				errMsg := fmt.Sprintf("TIMEOUT: Hết %s chờ mà KHÔNG có TX nào vào block! Kiểm tra: (1) Node có đang chạy? (2) TX có bị reject ở mempool? (3) Chain có bị kẹt không?%s", maxWait, epochInfo)
				fmt.Printf("\n  ❌ %s\n", errMsg)
				logErrorToFile(fmt.Sprintf("[Round %d] %s", round, errMsg))
			} else {
				errMsg := fmt.Sprintf("TIMEOUT: Hết %s chờ, chỉ %d/%d TXs vào block.%s", maxWait, totalTxsInBlocks, len(allTxs), epochInfo)
				fmt.Printf("\n  ❌ %s\n", errMsg)
				logErrorToFile(fmt.Sprintf("[Round %d] %s", round, errMsg))
			}
		}

		if totalTxsInBlocks < uint64(len(allTxs)) {
			// Find and print stuck transactions
			fmt.Printf("\n🔍 [DIAGNOSTIC] Danh sách 10 giao dịch đầu tiên bị kẹt (không có receipt):\n")
			stuckCount := 0
			for idx, tx := range allTxs {
				ethHashLower := strings.ToLower(tx.txHash.Hex())
				if expectedTxHashes[ethHashLower] {
					pbHash := common.Hash{}
					internalTx := &transaction.Transaction{}
					if err := internalTx.Unmarshal(tx.bytes); err == nil {
						pbHash = internalTx.Hash()
					}

					clientAddr := "Unknown"
					if len(clients) > 0 && batchSize > 0 {
						clientAddr = clients[(idx/batchSize)%len(clients)].addr
					}

					fmt.Printf("   - TX #%d | EthHash: %s | PbHash: %s | Account: %s | Node: %s\n",
						stuckCount+1, tx.txHash.Hex(), pbHash.Hex(), tx.addr, clientAddr)

					stuckCount++
					if stuckCount >= 10 {
						break
					}
				}
			}

			var activeRPCEndpoints []string
			for _, rc := range rpcPool {
				activeRPCEndpoints = append(activeRPCEndpoints, rc.Endpoint)
			}
			var activeTCPs []string
			for _, cl := range clients {
				activeTCPs = append(activeTCPs, cl.addr)
			}
			errMsg := fmt.Sprintf("Không thể xử lý hết tất cả giao dịch (%d/%d) trên các node TCP: %v và RPC: %v!", totalTxsInBlocks, len(allTxs), activeTCPs, activeRPCEndpoints)
			fmt.Printf("\n❌ [ERROR] Đang gửi giao dịch tới Node TCP: %v, RPC: %v nhưng gặp lỗi: %s\n", activeTCPs, activeRPCEndpoints, errMsg)
			logErrorToFile(fmt.Sprintf("[Round %d] %s", round, errMsg))
			os.Exit(1)
		}

		endBlock, _ := rpcClient.GetBlockNumber()
		endEpoch := uint64(0)
		if blk, err := rpcClient.GetBlockByNumber(endBlock); err == nil && blk != nil {
			endEpoch = blk.Epoch
		}

		// Block statistics
		blockCount := 0
		maxTxInBlock := 0
		totalTxInBlocks := 0

		var prevTimestamp uint64 = uint64(blastStart.UnixMilli())

		var blockDetails strings.Builder
		if !trace {
			blockDetails.WriteString(fmt.Sprintf("\n  📝 CHI TIẾT TỪNG BLOCK (Từ block %d đến %d)\n", startBlock+1, endBlock))
			blockDetails.WriteString(fmt.Sprintf("  %-10s | %-15s | %-15s\n", "Block", "Số Giao Dịch", "Khoảng cách Block"))
			blockDetails.WriteString(fmt.Sprintf("  %s\n", strings.Repeat("-", 48)))
		}

		for b := startBlock + 1; b <= endBlock; b++ {
			blkInfo, err := rpcClient.GetBlockByNumber(b)
			if err == nil && blkInfo != nil {
				blockCount++
				txCount := len(blkInfo.Transactions)
				totalTxInBlocks += txCount
				if txCount > maxTxInBlock {
					maxTxInBlock = txCount
				}

				if !trace {
					durationStr := "N/A"
					if prevTimestamp > 0 && blkInfo.Timestamp > prevTimestamp {
						duration := blkInfo.Timestamp - prevTimestamp
						durationStr = fmt.Sprintf("%d ms", duration)
					} else if prevTimestamp > 0 && blkInfo.Timestamp == prevTimestamp {
						durationStr = "0 ms"
					}
					blockDetails.WriteString(fmt.Sprintf("  %-10d | %-15d | %-15s\n", b, txCount, durationStr))
				}
				prevTimestamp = blkInfo.Timestamp
			}
		}

		totalDuration := time.Since(blastStart)
		processingTPS := float64(totalTxsInBlocks) / totalDuration.Seconds()

		var onChainDuration time.Duration
		var onChainTPS float64
		isSingleBlock := false
		if !firstTxBlockTime.IsZero() && !lastTxBlockTime.IsZero() {
			if firstTxBlockTime.Equal(lastTxBlockTime) {
				isSingleBlock = true
				traces, err := rpcClient.GetBlockTraces(endBlock, endBlock)
				if err == nil && len(traces) > 0 {
					t := traces[0]
					if t.TotalExecutionUs > 0 {
						onChainDuration = time.Duration(t.TotalExecutionUs) * time.Microsecond
						txCountForTps := t.TxCount
						if txCountForTps <= 0 {
							txCountForTps = int(totalTxsInBlocks)
						}
						onChainTPS = float64(txCountForTps) / onChainDuration.Seconds()
					}
				}
			} else {
				onChainDuration = lastTxBlockTime.Sub(firstTxBlockTime)
				if onChainDuration > 0 {
					onChainTPS = float64(totalTxsInBlocks) / onChainDuration.Seconds()
				}
			}
		}
		allRoundTPS = append(allRoundTPS, processingTPS)

		if startEpochBeforeBlast != 0 && endEpoch != 0 && startEpochBeforeBlast != endEpoch {
			fmt.Printf("🔄 Phát hiện chuyển đổi Epoch (%d -> %d) trong Round %d. Giao dịch có thể bị chậm, bỏ qua cảnh báo TPS.\n", startEpochBeforeBlast, endEpoch, round)
		} else {
			if len(allRoundTPS) >= 4 { // Đủ 3 round trước + 1 round hiện tại
				var sum float64
				count := 0
				for i := len(allRoundTPS) - 2; i >= 0 && count < 10; i-- {
					sum += allRoundTPS[i]
					count++
				}
				avgTps := sum / float64(count)

				if processingTPS < avgTps*0.6 {
					dropPercent := (avgTps - processingTPS) / avgTps * 100
					msg := fmt.Sprintf("🚨 Cảnh báo TPS giảm bất thường trong TPS Blast CC!\nRound: %d\nTPS hiện tại: %.2f\nTPS trung bình trước đó: %.2f\n📉 Mức giảm: %.2f%%\nThời gian round: %s\n📦 Block: %d ➡️ %d\n⏱️ Epoch: %d ➡️ %d", round, processingTPS, avgTps, dropPercent, totalDuration.Round(time.Millisecond), startBlock, endBlock, startEpochBeforeBlast, endEpoch)
					fmt.Println(msg)
					sendTelegramAlert(msg, "TPS BLAST CC")
				}
			}
		}

		roundSummaries = append(roundSummaries, RoundSummary{
			Round:         round,
			StartBlock:    startBlock,
			EndBlock:      endBlock,
			BlockCount:    blockCount,
			TxCount:       totalTxInBlocks,
			MaxTxInBlock:  maxTxInBlock,
			TPS:           processingTPS,
			ProcessingSec: totalDuration.Seconds(),
		})

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("\n\n═══════════════════════════════════════════════════\n"))
		sb.WriteString(fmt.Sprintf("  📊 ROUND %d RESULTS\n", round))
		sb.WriteString(fmt.Sprintf("═══════════════════════════════════════════════════\n"))
		sb.WriteString(fmt.Sprintf("  🧱 Start Block:          %d\n", startBlock+1))
		sb.WriteString(fmt.Sprintf("  🧱 End Block:            %d\n", endBlock))
		sb.WriteString(fmt.Sprintf("  📤 Total TXs sent:       %d\n", len(allTxs)))
		sb.WriteString(fmt.Sprintf("  🚀 Injection TPS:        %.0f tx/s\n", injectionTPS))
		sb.WriteString(fmt.Sprintf("  ⏱️  Injection time:       %s\n", blastDuration.Round(time.Millisecond)))
		sb.WriteString(fmt.Sprintf("  ─────────────────────────────────────────────────\n"))
		sb.WriteString(fmt.Sprintf("  📥 TX in blocks:         %d\n", totalTxsInBlocks))
		sb.WriteString(fmt.Sprintf("  📊 End-to-End TPS:       ~%.0f tx/s\n", processingTPS))
		sb.WriteString(fmt.Sprintf("  ⏱️  End-to-End time:      %s\n", totalDuration.Round(time.Millisecond)))

		var traces []rpc.BlockTrace
		var tracesErr error
		var totalOnChainExecTime time.Duration
		if trace {
			traces, tracesErr = rpcClient.GetBlockTraces(startBlock+1, endBlock)
			if tracesErr == nil {
				var totalRealUs float64
				for _, t := range traces {
					realTotalUs := float64(0) + float64(0) + float64(t.EvmExecutionDurationUs) + float64(t.TotalExecutionUs)
					totalRealUs += realTotalUs
				}
				totalOnChainExecTime = time.Duration(totalRealUs) * time.Microsecond
			}
		}

		waitAndNetworkDelay := totalDuration - blastDuration - totalOnChainExecTime
		if waitAndNetworkDelay < 0 {
			waitAndNetworkDelay = 0
		}

		sb.WriteString(fmt.Sprintf("  ─────────────────────────────────────────────────\n"))
		sb.WriteString(fmt.Sprintf("  🔍 END-TO-END TIME BREAKDOWN:\n"))
		sb.WriteString(fmt.Sprintf("     1️⃣  Client TX Injection:     %s\n", blastDuration.Round(time.Millisecond)))
		sb.WriteString(fmt.Sprintf("     2️⃣  On-Chain Execution:      %s (Sum of block traces)\n", totalOnChainExecTime.Round(time.Millisecond)))
		sb.WriteString(fmt.Sprintf("     3️⃣  Mempool, Sync & Polling: %s (Wait & Networking)\n", waitAndNetworkDelay.Round(time.Millisecond)))
		sb.WriteString(fmt.Sprintf("  ─────────────────────────────────────────────────\n"))

		if isSingleBlock && onChainDuration > 0 {
			sb.WriteString(fmt.Sprintf("  📊 On-Chain Engine TPS:  ~%.0f tx/s (Single Block Trace Execution)\n", onChainTPS))
			sb.WriteString(fmt.Sprintf("  ⏱️  On-Chain Commit time: %s (Block Execution Trace)\n", onChainDuration.Round(time.Millisecond)))
		} else if onChainDuration > 0 {
			sb.WriteString(fmt.Sprintf("  📊 On-Chain Engine TPS:  ~%.0f tx/s (First ➡️ Last block commit)\n", onChainTPS))
			sb.WriteString(fmt.Sprintf("  ⏱️  On-Chain Commit time: %s\n", onChainDuration.Round(time.Millisecond)))
		} else {
			sb.WriteString(fmt.Sprintf("  📊 On-Chain Engine TPS:  N/A (All TXs confirmed in a single block, no trace available)\n"))
		}
		sb.WriteString(fmt.Sprintf("  ─────────────────────────────────────────────────\n"))
		sb.WriteString(fmt.Sprintf("  📦 BLOCK STATISTICS (Blocks %d to %d)\n", startBlock+1, endBlock))
		sb.WriteString(fmt.Sprintf("  🧊 Total Blocks:         %d\n", blockCount))
		sb.WriteString(fmt.Sprintf("  📥 Total TXs in blocks:  %d\n", totalTxInBlocks))
		sb.WriteString(fmt.Sprintf("  📈 Max TXs in a block:   %d\n", maxTxInBlock))
		if blockCount > 0 {
			sb.WriteString(fmt.Sprintf("  📉 Avg TXs per block:    %.1f\n", float64(totalTxInBlocks)/float64(blockCount)))

			if trace {
				// --- IN BLOCK TRACES REPORT ---
				sb.WriteString(fmt.Sprintf("\n  📝 BLOCK PERFORMANCE TRACES (Blocks %d to %d)\n", startBlock+1, endBlock))
				sb.WriteString(fmt.Sprintf("  %-8s | %-6s | %-10s | %-10s | %-10s | %-8s | %-11s | %-10s | %-10s | %-10s | %-10s | %-10s | %-10s | %-10s | %-10s\n",
					"Block", "TXs", "WaitGo", "WaitRust", "Consensus", "RustFFI", "ClientBatch", "ProcessTX", "CalcRoots", "BlockData", "Mapping", "CommitMem", "SaveDB", "Total", "GCPause"))
				sb.WriteString(fmt.Sprintf("  %s\n", strings.Repeat("-", 190)))

				if tracesErr != nil {
					sb.WriteString(fmt.Sprintf("  ❌ Could not fetch block traces: %v\n", tracesErr))
				} else {
					var totalWaitGo, totalWaitRust, totalConsensus, totalRustFFI, totalClientBatch float64
					var totalProcessTX, totalCalcRoots, totalBlockData, totalMapping, totalCommitMem, totalSaveDB, totalTotal, totalGCPause float64

					for _, t := range traces {
						// Calculate real total including all phases + wait times (End-to-End Node Latency)
						realTotalUs := float64(0) +
							float64(0) +
							float64(t.EvmExecutionDurationUs) +
							float64(t.TotalExecutionUs)

						totalWaitGo += float64(0)
						totalWaitRust += float64(0)
						totalConsensus += float64(0)
						totalRustFFI += float64(0)
						totalClientBatch += float64(0)
						totalProcessTX += float64(t.EvmExecutionDurationUs)
						totalCalcRoots += float64(0)
						totalBlockData += float64(0)
						totalMapping += float64(0)
						totalCommitMem += float64(0)
						totalSaveDB += float64(t.CommitDurationUs)
						totalTotal += float64(t.TotalExecutionUs)
						totalGCPause += float64(0)

						sb.WriteString(fmt.Sprintf("  %-8d | %-6d | %-8.1fms | %-8.1fms | %-8.1fms | %-6.1fms | %-9.1fms | %-8.1fms | %-8.1fms | %-8.2fms | %-8.2fms | %-8.1fms | %-8.1fms | %-8.1fms | %-8.1fms\n",
							t.BlockNumber, t.TxCount,
							float64(0)/1000.0,
							float64(0)/1000.0,
							float64(0)/1000.0,
							float64(0)/1000.0,
							float64(0)/1000.0,
							float64(t.EvmExecutionDurationUs)/1000.0,
							float64(0)/1000.0, // calc roots
							float64(0)/1000.0,
							float64(0)/1000.0,
							float64(0)/1000.0,
							float64(t.CommitDurationUs)/1000.0,
							realTotalUs/1000.0,
							float64(0)/1000.0))
					}

					if len(traces) > 0 {
						n := float64(len(traces))
						sb.WriteString(fmt.Sprintf("\n  🔍 BOTTLENECK ANALYSIS (Average per Block)\n"))
						sb.WriteString(fmt.Sprintf("  %s\n", strings.Repeat("-", 75)))

						type stat struct {
							name  string
							avgMs float64
							desc  string
						}
						stats := []stat{
							{"ProcessTX", totalProcessTX / n / 1000.0, "Thực thi EVM & cập nhật State"},
							{"WaitGo", totalWaitGo / n / 1000.0, "Mempool Go / Xử lý RPC nghẽn"},
							{"WaitRust", totalWaitRust / n / 1000.0, "Đồng thuận P2P / Rust Core"},
							{"Consensus", totalConsensus / n / 1000.0, "Rust xử lý thuật toán đồng thuận"},
							{"SaveDB", totalSaveDB / n / 1000.0, "Ghi dữ liệu Disk I/O"},
							{"CommitMem", totalCommitMem / n / 1000.0, "Lưu State Cache (Memory)"},
							{"CalcRoots", totalCalcRoots / n / 1000.0, "Tính toán Merkle Roots"},
							{"GCPause", totalGCPause / n / 1000.0, "Dừng dọn rác Golang (STW)"},
							{"ClientBatch", totalClientBatch / n / 1000.0, "Hàng đợi chờ Execution Go"},
						}

						sort.Slice(stats, func(i, j int) bool {
							return stats[i].avgMs > stats[j].avgMs
						})

						var baseMs float64 = (totalWaitGo + totalWaitRust + totalTotal) / n / 1000.0
						if baseMs == 0 {
							baseMs = 1
						}

						for i, s := range stats {
							if i >= 4 && s.avgMs < 5.0 {
								break // only show top bottlenecks
							}
							percent := (s.avgMs / baseMs) * 100.0
							if percent > 100.0 {
								percent = 100.0
							}
							icon := "🟢"
							if s.avgMs > 200 {
								icon = "🔴"
							} else if s.avgMs > 50 {
								icon = "🟡"
							}
							sb.WriteString(fmt.Sprintf("  %s %-12s : %8.1f ms (%5.1f%%) | %s\n", icon, s.name, s.avgMs, percent, s.desc))
						}
						sb.WriteString(fmt.Sprintf("  %s\n", strings.Repeat("-", 75)))

						top := stats[0]
						sb.WriteString(fmt.Sprintf("  💡 Gợi ý tối ưu:\n"))
						if top.name == "ProcessTX" && top.avgMs > 500 {
							sb.WriteString(fmt.Sprintf("     - ProcessTX quá cao (%.1fms). Nút thắt tại CPU xử lý EVM.\n", top.avgMs))
							sb.WriteString(fmt.Sprintf("     -> Thử giảm 'Max TXs per block' hoặc nâng cấp CPU.\n"))
						} else if top.name == "WaitRust" && top.avgMs > 1000 {
							sb.WriteString(fmt.Sprintf("     - WaitRust rất cao (%.1fms). Mạng P2P hoặc đồng thuận lõi đang nghẽn.\n", top.avgMs))
							sb.WriteString(fmt.Sprintf("     -> Kiểm tra network latency giữa các node hoặc logs của Rust.\n"))
						} else if top.name == "SaveDB" && top.avgMs > 100 {
							sb.WriteString(fmt.Sprintf("     - SaveDB cao (%.1fms). Nút thắt tại I/O ổ cứng.\n", top.avgMs))
							sb.WriteString(fmt.Sprintf("     -> Chuyển sang SSD NVMe hoặc tối ưu cấu hình PebbleDB.\n"))
						} else if top.name == "GCPause" && top.avgMs > 50 {
							sb.WriteString(fmt.Sprintf("     - GCPause cao (%.1fms). Golang đang tốn nhiều thời gian dọn rác.\n", top.avgMs))
							sb.WriteString(fmt.Sprintf("     -> Kiểm tra memory leak hoặc cấu hình lại GOGC.\n"))
						} else if top.name == "WaitGo" && top.avgMs > 500 {
							sb.WriteString(fmt.Sprintf("     - WaitGo cao (%.1fms). TX bị nghẽn ở Mempool trước khi vào đồng thuận.\n", top.avgMs))
							sb.WriteString(fmt.Sprintf("     -> Tối ưu rpc server hoặc Mempool parsing.\n"))
						} else {
							sb.WriteString(fmt.Sprintf("     - Hệ thống đang chạy khá mượt mà. Nút thắt chính hiện tại: %s.\n", top.name))
						}
					}
				}
			} else {
				sb.WriteString(blockDetails.String())
			}
		}

		fmt.Print(sb.String())

		// Auto-save to tps_round_results.md
		f, err := os.OpenFile(reportFilename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			f.WriteString(sb.String())
			f.Close()
			fmt.Printf("  💾 [Saved results to %s]\n", reportFilename)
		} else {
			fmt.Printf("  ⚠️ Could not save results to file: %v\n", err)
		}

		// ── Verify: Kiểm tra cụ thể Balance và Receipt ──────────────────
		if verify {
			fmt.Printf("\n  🔎 Chờ 2s để các node đồng bộ state...\n")
			time.Sleep(2 * time.Second)

			fmt.Printf("  🔎 Verifying %d transactions (Balance & Receipt)...\n", len(allTxs))
			var verifiedCount int64
			var failedCount int64

			// BƯỚC 1: Quét nhanh Balance cho toàn bộ TX (Pass 1)
			var pass1Wg sync.WaitGroup
			pass1Ch := make(chan int, len(allTxs))

			// Hàng đợi cho những TX trượt bước 1
			var pass2Txs []int
			var pass2Mu sync.Mutex

			for w := 0; w < 100; w++ {
				pass1Wg.Add(1)
				go func() {
					defer pass1Wg.Done()
					for idx := range pass1Ch {
						tx := allTxs[idx]
						rc := rpcPool[idx%len(rpcPool)]

						as, err := rc.GetAccountState(tx.target.Hex())
						if err == nil && as != nil && as.Balance.Cmp(tx.amount) >= 0 {
							atomic.AddInt64(&verifiedCount, 1)
						} else {
							pass2Mu.Lock()
							pass2Txs = append(pass2Txs, idx)
							pass2Mu.Unlock()
						}

						done := atomic.LoadInt64(&verifiedCount) + int64(len(pass2Txs))
						if done%1000 == 0 || done == int64(len(allTxs)) {
							fmt.Printf("\r    [Pass 1] Checked Balance: %d/%d (Need Receipts: %d)   ", done, len(allTxs), len(pass2Txs))
						}
					}
				}()
			}

			for i := range allTxs {
				pass1Ch <- i
			}
			close(pass1Ch)
			pass1Wg.Wait()

			// BƯỚC 2: Nếu có TX chưa check được Balance, chờ 1 cục 5s rồi gặt mạng hỏi Receipt (Pass 2)
			if len(pass2Txs) > 0 {
				fmt.Printf("\n  ⏳ Mạng lag/Balance chưa lên, rớt lại %d TXs. Ngủ 5s trước khi quét Receipt...\n", len(pass2Txs))
				time.Sleep(5 * time.Second)

				var pass2Wg sync.WaitGroup
				pass2Ch := make(chan int, len(pass2Txs))

				for w := 0; w < 100; w++ {
					pass2Wg.Add(1)
					go func() {
						defer pass2Wg.Done()
						for idx := range pass2Ch {
							tx := allTxs[idx]
							rc := rpcPool[idx%len(rpcPool)]

							// Kiểm tra Balance lại một lần nữa sau khi đã ngủ 5s (chắc cú 100%)
							as, _ := rc.GetAccountState(tx.target.Hex())
							if as != nil && as.Balance.Cmp(tx.amount) >= 0 {
								// Tiền đã nổi sau 5s chờ
								atomic.AddInt64(&verifiedCount, 1)
							} else {
								// Nếu vẫn chưa up Balance (do RPC node bị delay cache), đành tin chuẩn xác vào Receipt
								receipt, rErr := rc.GetReceipt(tx.txHash.Hex())
								if rErr == nil && receipt != nil {
									status := ""
									if s, ok := receipt["status"].(string); ok {
										status = s
									} else if st, ok := receipt["Status"].(string); ok {
										status = st
									}

									if status != "" && status != "0x0" && status != "FAILED" { // Lọc bớt status thất bại nếu có
										atomic.AddInt64(&verifiedCount, 1)
									} else {
										atomic.AddInt64(&failedCount, 1)
									}
								} else {
									atomic.AddInt64(&failedCount, 1)
								}
							}

							done2 := atomic.LoadInt64(&verifiedCount) + atomic.LoadInt64(&failedCount)
							if done2%1000 == 0 || done2 == int64(len(allTxs)) {
								fmt.Printf("\r    [Pass 2] Fetching Receipts: %d/%d completed            ", done2, len(allTxs))
							}
						}
					}()
				}

				for _, idx := range pass2Txs {
					pass2Ch <- idx
				}
				close(pass2Ch)
				pass2Wg.Wait()
			}

			fmt.Printf("\n  ✅ Kết quả: %d TXs xác nhận OK, %d TXs Lỗi\n", verifiedCount, failedCount)
		}
	} // end round loop

	// ── Benchmark Summary ──────────────────────────────
	if numRounds > 1 {
		var minTPS, maxTPS, sumTPS float64
		minTPS = allRoundTPS[0]
		maxTPS = allRoundTPS[0]
		for _, t := range allRoundTPS {
			sumTPS += t
			if t < minTPS {
				minTPS = t
			}
			if t > maxTPS {
				maxTPS = t
			}
		}
		avgTPS := sumTPS / float64(len(allRoundTPS))

		var summaryBuilder strings.Builder
		summaryBuilder.WriteString("\n## 📊 BENCHMARK SUMMARY\n\n")
		summaryBuilder.WriteString(fmt.Sprintf("- **Rounds**: %d\n", numRounds))
		summaryBuilder.WriteString(fmt.Sprintf("- **TXs per round**: %d\n\n", len(allTxs)))
		summaryBuilder.WriteString("| Round | TPS |\n|---|---|\n")
		for i, t := range allRoundTPS {
			summaryBuilder.WriteString(fmt.Sprintf("| %d | ~%.0f tx/s |\n", i+1, t))
		}
		summaryBuilder.WriteString("\n")
		summaryBuilder.WriteString(fmt.Sprintf("- **Min TPS**: ~%.0f tx/s\n", minTPS))
		summaryBuilder.WriteString(fmt.Sprintf("- **Max TPS**: ~%.0f tx/s\n", maxTPS))
		summaryBuilder.WriteString(fmt.Sprintf("- **Avg TPS**: ~%.0f tx/s\n", avgTPS))

		fmt.Println("\n╔═══════════════════════════════════════════════════╗")
		fmt.Println("║  📊 BENCHMARK SUMMARY")
		fmt.Println("╠═══════════════════════════════════════════════════╣")
		fmt.Printf("║  🔄 Rounds         : %d\n", numRounds)
		fmt.Printf("║  📤 TXs per round  : %d\n", len(allTxs))
		fmt.Println("║  ─────────────────────────────────────────────────")
		for i, t := range allRoundTPS {
			fmt.Printf("║  Round %-2d TPS      : ~%.0f tx/s\n", i+1, t)
		}
		fmt.Println("║  ─────────────────────────────────────────────────")
		fmt.Printf("║  📉 Min TPS        : ~%.0f tx/s\n", minTPS)
		fmt.Printf("║  📈 Max TPS        : ~%.0f tx/s\n", maxTPS)
		fmt.Printf("║  📊 Avg TPS        : ~%.0f tx/s\n", avgTPS)
		fmt.Println("╚═══════════════════════════════════════════════════╝")

		// Append to report filename
		f, err := os.OpenFile(reportFilename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			f.WriteString(summaryBuilder.String())
			f.Close()
			fmt.Printf("💾 Final summary appended to %s\n", reportFilename)
		}
	}

	// ── Export JSON cho matching-tps ─────────────────────────
	type BlastResult struct {
		RoundTPS []float64 `json:"roundTPS"`
	}
	bResult := BlastResult{RoundTPS: allRoundTPS}
	bData, _ := json.MarshalIndent(bResult, "", "  ")
	os.WriteFile("blast_cc_results.json", bData, 0644)
}

func cleanupReports() {
	files, err := os.ReadDir("reports")
	if err != nil {
		return
	}
	var mdFiles []os.DirEntry
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
			mdFiles = append(mdFiles, f)
		}
	}
	sort.Slice(mdFiles, func(i, j int) bool {
		infoI, errI := mdFiles[i].Info()
		infoJ, errJ := mdFiles[j].Info()
		if errI != nil || errJ != nil {
			return false
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})

	if len(mdFiles) > 5 {
		for _, f := range mdFiles[5:] {
			os.Remove(filepath.Join("reports", f.Name()))
		}
	}
}
