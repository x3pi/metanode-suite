package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	client_tcp "tool-test/pkg/client-tcp"
	tcp_config "tool-test/pkg/client-tcp/config"
	tx_models "tool-test/pkg/client-tcp/models"
	tx_helper "tool-test/pkg/client-tcp/utils/tx_helper"
	pb "tool-test/pkg/proto"
)

// GlobalConfig ánh xạ file configs/config.json
type GlobalConfig struct {
	BlsPrivateKey           string                      `json:"bls_private_key"`
	RpcUrl                  string                      `json:"rpc_url"`
	WsUrl                   string                      `json:"ws_url"`
	TcpNode                 string                      `json:"tcp_node"`
	ParentConnectionAddress string                      `json:"parent_connection_address"`
	ChainId                 int64                       `json:"chain_id"`
	PrivateKeys             []string                    `json:"private_keys"`
	PrivateChains           map[string]ChildChainConfig `json:"private_chains"`
}

type ChildChainConfig struct {
	ChainId       int64             `json:"chain_id"`
	BlsPrivateKey string            `json:"bls_private_key"`
	RpcUrl        string            `json:"rpc_url"`
	WsUrl         string            `json:"ws_url"`
	TcpNodes      map[string]string `json:"tcp_nodes"`
	PrivateKeys   []string          `json:"private_keys"`
}

// ChainParams chứa tham số đầy đủ của 1 chain sau khi phân giải
type ChainParams struct {
	ChainTag  string
	ChainName string
	ChainId   int64
	RpcUrl    string
	WsUrl     string
	TcpNode   string
	BlsKey    string
	User1Key  string
	User2Key  string
	User1Addr common.Address
	User2Addr common.Address
}

// LatencyStats kết quả đo đạc độ trễ
type LatencyStats struct {
	Proto           string
	TotalRounds     int
	Success         int
	Latencies       []int64
	Min             int64
	Max             int64
	Avg             int64
	P50             int64
	P95             int64
	ServerLatencies []int64
	ServerMin       int64
	ServerMax       int64
	ServerAvg       int64
}

func main() {
	chainFlag := flag.String("chain", "parent", "Tên chain: 'parent' (public) hoặc child chain ('chain_a', 'chain_b', v.v.)")
	protoFlag := flag.String("proto", "all", "Giao thức test: 'rpc', 'tcp', hoặc 'all' (chạy cả hai để so sánh)")
	roundsFlag := flag.Int("rounds", 10, "Số lượng vòng Ping-Pong đo latency")
	contractFlag := flag.String("contract", "", "Địa chỉ contract SimpleChat (nếu để trống, tự động deploy hoặc đọc từ cache)")
	redeployFlag := flag.Bool("redeploy", false, "Bắt buộc deploy lại contract mới")
	configFlag := flag.String("config", "../configs/config.json", "Đường dẫn file configs/config.json")
	modeFlag := flag.String("mode", "auto", "Chế độ: 'auto' (tự động test benchmark hoàn chỉnh) hoặc 'chat' (mở console chat tay)")
	userFlag := flag.Int("user", 1, "Chỉ dùng trong mode 'chat': đóng vai User 1 hay User 2 (1 hoặc 2)")

	flag.Parse()

	fmt.Println("═════════════════════════════════════════════════════════════")
	fmt.Println("🚀 METANODE BENCHMARK & DEMO CHAT (RPC + TCP TỰ ĐỘNG)")
	fmt.Println("═════════════════════════════════════════════════════════════")

	// 1. Phân giải cấu hình chain từ configs/config.json
	params, err := loadChainParams(*configFlag, *chainFlag)
	if err != nil {
		log.Fatalf("❌ Lỗi phân giải cấu hình chain: %v", err)
	}

	fmt.Printf("⛓️  Target Chain: %s (ChainID: %d)\n", params.ChainName, params.ChainId)
	fmt.Printf("🌐 RPC Endpoint: %s | WS: %s\n", params.RpcUrl, params.WsUrl)
	fmt.Printf("🔌 TCP Endpoint: %s\n", params.TcpNode)
	fmt.Printf("👤 User 1 (Receiver/Deployer): %s\n", params.User1Addr.Hex())
	fmt.Printf("👤 User 2 (Ping Initiator)  : %s\n", params.User2Addr.Hex())

	// Cập nhật lại user1.json và user2.json để người dùng có thể tham khảo
	_ = saveLocalUserConfigs(params)

	// 2. Load ABI và Bytecode của SimpleChat
	contractAbi, bytecode, err := loadContractArtifacts()
	if err != nil {
		log.Fatalf("❌ Lỗi nạp ABI/Bytecode: %v", err)
	}

	// 3. Xác định hoặc deploy Contract
	contractAddr, err := resolveContract(params, *contractFlag, *redeployFlag, bytecode)
	if err != nil {
		log.Fatalf("❌ Lỗi xác định contract: %v", err)
	}
	fmt.Printf("📝 SimpleChat Contract: %s\n", contractAddr.Hex())
	fmt.Println("─────────────────────────────────────────────────────────────")

	// 4. Nếu chạy chế độ chat tay (interactive mode)
	if *modeFlag == "chat" {
		runInteractiveChat(params, contractAddr, contractAbi, *protoFlag, *userFlag)
		return
	}

	// 5. Chế độ TỰ ĐỘNG BENCHMARK LATENCY (Default)
	var rpcStats, tcpStats *LatencyStats

	targetProto := strings.ToLower(*protoFlag)
	if targetProto == "rpc" || targetProto == "all" {
		fmt.Printf("\n▶️ [1/2] BẮT ĐẦU TEST RPC LATENCY (HTTP RPC + WebSocket Stream)...\n")
		stats, err := runRpcAutoTest(params, contractAddr, contractAbi, *roundsFlag)
		if err != nil {
			fmt.Printf("⚠️ Lỗi chạy test RPC: %v\n", err)
		} else {
			rpcStats = stats
			printStatsTable("RPC (HTTP + WS)", params, rpcStats)
		}
	}

	if targetProto == "tcp" || targetProto == "all" {
		fmt.Printf("\n▶️ [2/2] BẮT ĐẦU TEST TCP LATENCY (Native TCP Socket + Event Stream)...\n")
		stats, err := runTcpAutoTest(params, contractAddr, contractAbi, *roundsFlag)
		if err != nil {
			fmt.Printf("⚠️ Lỗi chạy test TCP: %v\n", err)
		} else {
			tcpStats = stats
			printStatsTable("TCP (Native Client)", params, tcpStats)
		}
	}

	// In bảng so sánh nếu test cả hai
	if rpcStats != nil && tcpStats != nil {
		printComparisonTable(rpcStats, tcpStats)
	}

	fmt.Println("\n🎉 HOÀN TẤT BÀI ĐO LATENCY THÀNH CÔNG!")
}

// -------------------------------------------------------------
// PHẦN 1: QUẢN LÝ CẤU HÌNH & SMART CONTRACT
// -------------------------------------------------------------

func loadChainParams(configPath, targetChain string) (*ChainParams, error) {
	candidates := []string{
		configPath,
		"../configs/config.json",
		"configs/config.json",
		"../../configs/config.json",
		"/home/abc/nhat/con-chain-v2/metanode-suite/configs/config.json",
	}

	var raw []byte
	var err error
	for _, p := range candidates {
		if data, e := os.ReadFile(p); e == nil {
			raw = data
			err = nil
			break
		}
	}
	if raw == nil {
		return nil, fmt.Errorf("không tìm thấy file configs/config.json (đã thử các đường dẫn: %v)", candidates)
	}

	var gCfg GlobalConfig
	if err := json.Unmarshal(raw, &gCfg); err != nil {
		return nil, fmt.Errorf("lỗi parse JSON config: %w", err)
	}

	params := &ChainParams{
		ChainTag: targetChain,
	}

	target := strings.ToLower(targetChain)
	if target == "parent" || target == "public" || target == "" {
		params.ChainName = "Parent Chain (Public)"
		params.ChainId = gCfg.ChainId
		if params.ChainId == 0 {
			params.ChainId = 991
		}
		params.RpcUrl = gCfg.RpcUrl
		params.WsUrl = gCfg.WsUrl
		params.TcpNode = gCfg.TcpNode
		if params.TcpNode == "" {
			params.TcpNode = gCfg.ParentConnectionAddress
		}
		params.BlsKey = gCfg.BlsPrivateKey
		if len(gCfg.PrivateKeys) < 2 {
			return nil, fmt.Errorf("cần ít nhất 2 private_keys trong public chain để test 2 user")
		}
		params.User1Key = gCfg.PrivateKeys[0]
		params.User2Key = gCfg.PrivateKeys[1]
	} else {
		// Child chain (ví dụ chain_a)
		childTag := target
		if childTag == "child" {
			childTag = "chain_a"
		}
		child, ok := gCfg.PrivateChains[childTag]
		if !ok {
			return nil, fmt.Errorf("không tìm thấy child chain '%s' trong private_chains", childTag)
		}
		params.ChainName = fmt.Sprintf("Child Chain (%s)", childTag)
		params.ChainId = child.ChainId
		if params.ChainId == 0 {
			params.ChainId = 991
		}
		params.RpcUrl = child.RpcUrl
		params.WsUrl = child.WsUrl
		if child.TcpNodes != nil {
			params.TcpNode = child.TcpNodes["m0"]
		}
		params.BlsKey = child.BlsPrivateKey
		if len(child.PrivateKeys) < 2 {
			return nil, fmt.Errorf("cần ít nhất 2 private_keys trong child chain '%s' để test 2 user", childTag)
		}
		params.User1Key = child.PrivateKeys[0]
		params.User2Key = child.PrivateKeys[1]
	}

	// Chuẩn hóa endpoints
	if params.RpcUrl != "" && !strings.HasPrefix(params.RpcUrl, "http://") && !strings.HasPrefix(params.RpcUrl, "https://") {
		params.RpcUrl = "http://" + params.RpcUrl
	}
	if params.WsUrl == "" && params.RpcUrl != "" {
		ws := strings.Replace(params.RpcUrl, "http://", "ws://", 1)
		ws = strings.Replace(ws, "https://", "wss://", 1)
		if !strings.HasSuffix(ws, "/ws") {
			ws += "/ws"
		}
		params.WsUrl = ws
	}

	// Tạo User1 Address và User2 Address
	u1Addr, _, err := keyToAccount(params.User1Key)
	if err != nil {
		return nil, fmt.Errorf("lỗi đọc key User 1: %w", err)
	}
	u2Addr, _, err := keyToAccount(params.User2Key)
	if err != nil {
		return nil, fmt.Errorf("lỗi đọc key User 2: %w", err)
	}
	params.User1Addr = u1Addr
	params.User2Addr = u2Addr

	return params, nil
}

func keyToAccount(hexKey string) (common.Address, *ecdsa.PrivateKey, error) {
	clean := strings.TrimPrefix(hexKey, "0x")
	pk, err := crypto.HexToECDSA(clean)
	if err != nil {
		return common.Address{}, nil, err
	}
	pub := pk.Public().(*ecdsa.PublicKey)
	return crypto.PubkeyToAddress(*pub), pk, nil
}

func loadContractArtifacts() (abi.ABI, []byte, error) {
	abiBytes, err := os.ReadFile("SimpleChat.abi")
	if err != nil {
		return abi.ABI{}, nil, fmt.Errorf("không đọc được SimpleChat.abi: %w", err)
	}
	contractAbi, err := abi.JSON(strings.NewReader(string(abiBytes)))
	if err != nil {
		return abi.ABI{}, nil, fmt.Errorf("lỗi parse SimpleChat.abi: %w", err)
	}

	binBytes, err := os.ReadFile("SimpleChat.bin")
	if err != nil {
		return abi.ABI{}, nil, fmt.Errorf("không đọc được SimpleChat.bin: %w", err)
	}
	binHex := strings.TrimSpace(string(binBytes))
	if !strings.HasPrefix(binHex, "0x") {
		binHex = "0x" + binHex
	}
	bytecode, err := hexutil.Decode(binHex)
	if err != nil {
		return abi.ABI{}, nil, fmt.Errorf("lỗi decode bytecode: %w", err)
	}

	return contractAbi, bytecode, nil
}

func resolveContract(params *ChainParams, customContract string, forceRedeploy bool, bytecode []byte) (common.Address, error) {
	if customContract != "" {
		return common.HexToAddress(customContract), nil
	}

	cacheFile := fmt.Sprintf(".contract_%s.txt", sanitizeFilename(params.ChainTag))

	// Kiểm tra cache nếu không force redeploy
	if !forceRedeploy {
		if cachedBytes, err := os.ReadFile(cacheFile); err == nil {
			cachedAddrStr := strings.TrimSpace(string(cachedBytes))
			if common.IsHexAddress(cachedAddrStr) {
				cachedAddr := common.HexToAddress(cachedAddrStr)
				// Verify xem contract có tồn tại trên chain không
				if client, err := ethclient.Dial(params.RpcUrl); err == nil {
					code, _ := client.CodeAt(context.Background(), cachedAddr, nil)
					if len(code) > 0 {
						fmt.Printf("⚡ Tái sử dụng contract đã deploy từ cache: %s\n", cachedAddr.Hex())
						return cachedAddr, nil
					}
				}
			}
		}
	}

	// Deploy contract mới bằng User 1 qua RPC
	fmt.Printf("⏳ Đang tự động deploy contract SimpleChat lên %s qua RPC...\n", params.ChainName)
	client, err := ethclient.Dial(params.RpcUrl)
	if err != nil {
		return common.Address{}, fmt.Errorf("lỗi kết nối RPC deploy: %w", err)
	}
	defer client.Close()

	_, pk1, _ := keyToAccount(params.User1Key)
	nonce, err := client.PendingNonceAt(context.Background(), params.User1Addr)
	if err != nil {
		return common.Address{}, fmt.Errorf("lỗi lấy nonce deploy: %w", err)
	}

	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil || gasPrice == nil || gasPrice.Sign() == 0 {
		gasPrice = big.NewInt(2000000000)
	}

	tx := types.NewContractCreation(nonce, big.NewInt(0), 5000000, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(params.ChainId)), pk1)
	if err != nil {
		return common.Address{}, fmt.Errorf("lỗi ký tx deploy: %w", err)
	}

	if err := client.SendTransaction(context.Background(), signedTx); err != nil {
		return common.Address{}, fmt.Errorf("lỗi gửi tx deploy: %w", err)
	}

	// Chờ receipt
	var deployedAddr common.Address
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		rcpt, err := client.TransactionReceipt(context.Background(), signedTx.Hash())
		if err == nil && rcpt != nil {
			if rcpt.ContractAddress != (common.Address{}) {
				deployedAddr = rcpt.ContractAddress
			} else {
				deployedAddr = crypto.CreateAddress(params.User1Addr, nonce)
			}
			break
		}
	}

	if deployedAddr == (common.Address{}) {
		return common.Address{}, fmt.Errorf("timeout khi chờ receipt deploy contract (tx: %s)", signedTx.Hash().Hex())
	}

	_ = os.WriteFile(cacheFile, []byte(deployedAddr.Hex()), 0644)
	fmt.Printf("🎉 DEPLOY THÀNH CÔNG! Contract Address: %s\n", deployedAddr.Hex())
	return deployedAddr, nil
}

func saveLocalUserConfigs(p *ChainParams) error {
	u1Map := map[string]interface{}{
		"private_key":               p.BlsKey,
		"version":                   "0.0.1.0",
		"parent_connection_address": p.TcpNode,
		"chain_id":                  p.ChainId,
		"nation_id":                 1,
		"parent_connection_type":    "client",
		"parent_address":            p.User1Addr.Hex(),
		"eth_pk":                    p.User1Key,
		"http_rpc":                  p.RpcUrl,
		"websocket_rpc":             p.WsUrl,
		"target_address":            p.User2Addr.Hex(),
	}
	u2Map := map[string]interface{}{
		"private_key":               p.BlsKey,
		"version":                   "0.0.1.0",
		"parent_connection_address": p.TcpNode,
		"chain_id":                  p.ChainId,
		"nation_id":                 1,
		"parent_connection_type":    "client",
		"parent_address":            p.User2Addr.Hex(),
		"eth_pk":                    p.User2Key,
		"http_rpc":                  p.RpcUrl,
		"websocket_rpc":             p.WsUrl,
		"target_address":            p.User1Addr.Hex(),
	}

	b1, _ := json.MarshalIndent(u1Map, "", "  ")
	b2, _ := json.MarshalIndent(u2Map, "", "  ")
	_ = os.WriteFile("user1.json", b1, 0644)
	_ = os.WriteFile("user2.json", b2, 0644)
	return nil
}

func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

// -------------------------------------------------------------
// PHẦN 2: TEST TỰ ĐỘNG LATENCY QUA RPC (HTTP + WS)
// -------------------------------------------------------------

func runRpcAutoTest(p *ChainParams, contractAddr common.Address, contractAbi abi.ABI, rounds int) (*LatencyStats, error) {
	_, pk1, _ := keyToAccount(p.User1Key)
	_, pk2, _ := keyToAccount(p.User2Key)

	// Kết nối client User 1
	cli1, err := ethclient.Dial(p.RpcUrl)
	if err != nil {
		return nil, fmt.Errorf("lỗi kết nối RPC User 1: %w", err)
	}
	defer cli1.Close()

	ws1, err := ethclient.Dial(p.WsUrl)
	if err != nil {
		return nil, fmt.Errorf("lỗi kết nối WebSocket User 1: %w", err)
	}
	defer ws1.Close()

	// Kết nối client User 2
	cli2, err := ethclient.Dial(p.RpcUrl)
	if err != nil {
		return nil, fmt.Errorf("lỗi kết nối RPC User 2: %w", err)
	}
	defer cli2.Close()

	ws2, err := ethclient.Dial(p.WsUrl)
	if err != nil {
		return nil, fmt.Errorf("lỗi kết nối WebSocket User 2: %w", err)
	}
	defer ws2.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. User 1 Subscribe WebSocket để tự động Reply PONG khi nhận PING
	logs1 := make(chan types.Log, 100)
	sub1, err := ws1.SubscribeFilterLogs(ctx, ethereum.FilterQuery{Addresses: []common.Address{contractAddr}}, logs1)
	if err != nil {
		return nil, fmt.Errorf("User 1 không subscribe được WS: %w", err)
	}
	defer sub1.Unsubscribe()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-sub1.Err():
				if err != nil {
					return
				}
			case vLog := <-logs1:
				from, to, text, ts, ok := unpackMessageSentEvent(contractAbi, vLog.Topics, vLog.Data)
				if !ok {
					continue
				}
				// Nếu tin gửi tới User 1 và bắt đầu bằng PING -> Auto Reply PONG
				if to == p.User1Addr && strings.HasPrefix(text, "PING") {
					pongText := strings.Replace(text, "PING", "PONG", 1)
					go func(target common.Address, msg string, timestamp *big.Int) {
						nonce, err := cli1.PendingNonceAt(context.Background(), p.User1Addr)
						if err != nil {
							return
						}
						gasPrice, _ := cli1.SuggestGasPrice(context.Background())
						if gasPrice == nil || gasPrice.Sign() == 0 {
							gasPrice = big.NewInt(2000000000)
						}
						payload, _ := contractAbi.Pack("sendMessage", target, msg, timestamp)
						tx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), 5000000, gasPrice, payload)
						signed, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(p.ChainId)), pk1)
						_ = cli1.SendTransaction(context.Background(), signed)
					}(from, pongText, ts)
				}
			}
		}
	}()

	// 2. User 2 Subscribe WebSocket để nhận PONG và đo RTT
	logs2 := make(chan types.Log, 100)
	sub2, err := ws2.SubscribeFilterLogs(ctx, ethereum.FilterQuery{Addresses: []common.Address{contractAddr}}, logs2)
	if err != nil {
		return nil, fmt.Errorf("User 2 không subscribe được WS: %w", err)
	}
	defer sub2.Unsubscribe()

	pongCh := make(chan int64, 10)
	selfPingCh := make(chan int64, 10)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-sub2.Err():
				if err != nil {
					return
				}
			case vLog := <-logs2:
				from, to, text, ts, ok := unpackMessageSentEvent(contractAbi, vLog.Topics, vLog.Data)
				if !ok {
					continue
				}
				// 1. Khi Block 1 mined (User 2 gửi PING): Event báo cho User 2 biết server đã đưa PING vào block
				if from == p.User2Addr && strings.HasPrefix(text, "PING") {
					selfLat := time.Now().UnixMilli() - ts.Int64()
					select {
					case selfPingCh <- selfLat:
					default:
					}
				}
				// 2. Khi Block 2 mined (User 1 reply PONG): User 2 nhận PONG đo RTT khép kín
				if to == p.User2Addr && strings.HasPrefix(text, "PONG") {
					rtt := time.Now().UnixMilli() - ts.Int64()
					select {
					case pongCh <- rtt:
					default:
					}
				}
			}
		}
	}()

	time.Sleep(1 * time.Second) // Chờ stream kết nối ổn định

	fmt.Printf("   🚀 User 2 bắt đầu gửi %d tin nhắn PING qua RPC...\n", rounds)
	nonce2, err := cli2.PendingNonceAt(ctx, p.User2Addr)
	if err != nil {
		return nil, fmt.Errorf("User 2 lỗi lấy nonce: %w", err)
	}
	gasPrice, _ := cli2.SuggestGasPrice(ctx)
	if gasPrice == nil || gasPrice.Sign() == 0 {
		gasPrice = big.NewInt(2000000000)
	}

	var rttLats []int64
	var serverLats []int64
	for i := 1; i <= rounds; i++ {
		text := fmt.Sprintf("PING #%d latency benchmark payload", i)
		startTime := time.Now()
		ts := big.NewInt(startTime.UnixMilli())

		payload, err := contractAbi.Pack("sendMessage", p.User1Addr, text, ts)
		if err != nil {
			fmt.Printf("   ❌ Lỗi pack tx %d: %v\n", i, err)
			continue
		}

		tx := types.NewTransaction(nonce2, contractAddr, big.NewInt(0), 5000000, gasPrice, payload)
		signed, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(p.ChainId)), pk2)
		if err != nil {
			fmt.Printf("   ❌ Lỗi ký tx %d: %v\n", i, err)
			continue
		}

		tSend := time.Now()
		if err := cli2.SendTransaction(ctx, signed); err != nil {
			fmt.Printf("   ❌ Lỗi gửi tx %d: %v\n", i, err)
			nonce2, _ = cli2.PendingNonceAt(ctx, p.User2Addr)
			continue
		}
		rpcAckMs := time.Since(tSend).Milliseconds()

		var serverMs int64
		select {
		case sLat := <-selfPingCh:
			serverMs = sLat
			serverLats = append(serverLats, sLat)
		case <-time.After(30 * time.Second):
		}

		select {
		case rtt := <-pongCh:
			rttLats = append(rttLats, rtt)
			fmt.Printf("   📤 [RPC Round %d/%d] Gửi PING (Ack: %dms | Server 1-Block: %dms) ➡️ Nhận PONG! RTT: %d ms\n", i, rounds, rpcAckMs, serverMs, rtt)
			nonce2++
		case <-time.After(30 * time.Second):
			fmt.Printf("   📤 [RPC Round %d/%d] ⚠️ Timeout chờ PONG (30s)\n", i, rounds)
			nonce2, _ = cli2.PendingNonceAt(ctx, p.User2Addr)
		}
	}

	stats := calculateStats("RPC", rounds, rttLats, serverLats)
	return &stats, nil
}

func unpackMessageSentEvent(contractAbi abi.ABI, topics []common.Hash, data []byte) (from, to common.Address, message string, timestamp *big.Int, ok bool) {
	if len(topics) < 3 {
		return
	}
	event, exists := contractAbi.Events["MessageSent"]
	if !exists || topics[0] != event.ID {
		return
	}

	from = common.BytesToAddress(topics[1].Bytes())
	to = common.BytesToAddress(topics[2].Bytes())

	decoded := make(map[string]interface{})
	if err := contractAbi.UnpackIntoMap(decoded, "MessageSent", data); err != nil {
		return
	}

	msgVal, hasMsg := decoded["message"]
	tsVal, hasTs := decoded["timestamp"]
	if !hasMsg || !hasTs {
		return
	}

	message, _ = msgVal.(string)
	timestamp, _ = tsVal.(*big.Int)
	if timestamp == nil {
		return
	}
	ok = true
	return
}

// -------------------------------------------------------------
// PHẦN 3: TEST TỰ ĐỘNG LATENCY QUA TCP (NATIVE PROTOCOL)
// -------------------------------------------------------------

func runTcpAutoTest(p *ChainParams, contractAddr common.Address, contractAbi abi.ABI, rounds int) (*LatencyStats, error) {
	cfg1 := &tcp_config.ClientConfig{
		PrivateKey_:             p.BlsKey,
		ParentAddress:           p.User1Addr.Hex(),
		ParentConnectionAddress: p.TcpNode,
		ParentConnectionType:    "client",
		Version_:                "0.0.1.0",
		ChainId:                 uint64(p.ChainId),
		NationId:                1,
	}
	cfg2 := &tcp_config.ClientConfig{
		PrivateKey_:             p.BlsKey,
		ParentAddress:           p.User2Addr.Hex(),
		ParentConnectionAddress: p.TcpNode,
		ParentConnectionType:    "client",
		Version_:                "0.0.1.0",
		ChainId:                 uint64(p.ChainId),
		NationId:                1,
	}

	cli1, err := client_tcp.NewClient(cfg1)
	if err != nil {
		return nil, fmt.Errorf("User 1 lỗi kết nối TCP: %w", err)
	}

	cli2, err := client_tcp.NewClient(cfg2)
	if err != nil {
		return nil, fmt.Errorf("User 2 lỗi kết nối TCP: %w", err)
	}

	time.Sleep(1 * time.Second)

	// User 1 Subscribe TCP Event để Reply PONG
	eventCh1, err := cli1.ParentSubcribes([]common.Address{contractAddr})
	if err != nil {
		return nil, fmt.Errorf("User 1 không subscribe được TCP: %w", err)
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	go func() {
		for {
			select {
			case <-stopCh:
				return
			case evt, ok := <-eventCh1:
				if !ok {
					return
				}
				for _, logItem := range evt.EventLogList() {
					from, to, text, ts, ok := unpackTcpLogItem(contractAbi, logItem.Topics(), logItem.Data())
					if !ok {
						continue
					}
					if to == p.User1Addr && strings.HasPrefix(text, "PING") {
						pongText := strings.Replace(text, "PING", "PONG", 1)
						go func(target common.Address, msg string, timestamp *big.Int) {
							payload, _ := contractAbi.Pack("sendMessage", target, msg, timestamp)
							_, _ = tx_helper.SendTransaction(
								"sendMessage", cli1, cfg1, contractAddr, p.User1Addr, payload,
								&tx_models.TxOptions{MaxGas: 5000000},
							)
						}(from, pongText, ts)
					}
				}
			}
		}
	}()

	// User 2 Subscribe TCP Event để đo RTT
	eventCh2, err := cli2.ParentSubcribes([]common.Address{contractAddr})
	if err != nil {
		return nil, fmt.Errorf("User 2 không subscribe được TCP: %w", err)
	}

	pongCh := make(chan int64, 10)
	go func() {
		for {
			select {
			case <-stopCh:
				return
			case evt, ok := <-eventCh2:
				if !ok {
					return
				}
				for _, logItem := range evt.EventLogList() {
					_, to, text, ts, ok := unpackTcpLogItem(contractAbi, logItem.Topics(), logItem.Data())
					if !ok {
						continue
					}
					if to == p.User2Addr && strings.HasPrefix(text, "PONG") {
						rtt := time.Now().UnixMilli() - ts.Int64()
						select {
						case pongCh <- rtt:
						default:
						}
					}
				}
			}
		}
	}()

	time.Sleep(1 * time.Second)

	fmt.Printf("   🚀 User 2 bắt đầu gửi %d tin nhắn PING qua TCP...\n", rounds)

	var rttLats []int64
	var serverLats []int64
	for i := 1; i <= rounds; i++ {
		text := fmt.Sprintf("PING #%d tcp latency benchmark payload", i)
		startTime := time.Now()
		ts := big.NewInt(startTime.UnixMilli())

		payload, err := contractAbi.Pack("sendMessage", p.User1Addr, text, ts)
		if err != nil {
			fmt.Printf("   ❌ Lỗi pack tx %d: %v\n", i, err)
			continue
		}

		tSend := time.Now()
		receipt, err := tx_helper.SendTransaction(
			"sendMessage", cli2, cfg2, contractAddr, p.User2Addr, payload,
			&tx_models.TxOptions{MaxGas: 5000000},
		)
		serverMs := time.Since(tSend).Milliseconds()
		if err != nil || receipt == nil || (receipt.Status() != pb.RECEIPT_STATUS_RETURNED && receipt.Status() != pb.RECEIPT_STATUS_HALTED) {
			statusStr := "ERROR"
			if receipt != nil {
				statusStr = receipt.Status().String()
			}
			fmt.Printf("   📤 [TCP Round %d/%d] ❌ Send thất bại (%s)\n", i, rounds, statusStr)
			continue
		}
		serverLats = append(serverLats, serverMs)

		select {
		case rtt := <-pongCh:
			rttLats = append(rttLats, rtt)
			fmt.Printf("   📤 [TCP Round %d/%d] Gửi PING (Server Receipt: %dms) ➡️ Nhận PONG! RTT: %d ms\n", i, rounds, serverMs, rtt)
		case <-time.After(30 * time.Second):
			fmt.Printf("   📤 [TCP Round %d/%d] ⚠️ Timeout chờ PONG qua TCP (30s)\n", i, rounds)
		}
	}

	stats := calculateStats("TCP", rounds, rttLats, serverLats)
	return &stats, nil
}

func unpackTcpLogItem(contractAbi abi.ABI, topics []string, dataHex string) (from, to common.Address, message string, timestamp *big.Int, ok bool) {
	if len(topics) < 3 {
		return
	}
	event, exists := contractAbi.Events["MessageSent"]
	if !exists {
		return
	}

	t0 := topics[0]
	if !strings.HasPrefix(t0, "0x") {
		t0 = "0x" + t0
	}
	if t0 != event.ID.Hex() {
		return
	}

	from = common.HexToAddress(topics[1])
	to = common.HexToAddress(topics[2])

	dataBytes := common.FromHex(dataHex)
	decoded := make(map[string]interface{})
	if err := contractAbi.UnpackIntoMap(decoded, "MessageSent", dataBytes); err != nil {
		return
	}

	msgVal, hasMsg := decoded["message"]
	tsVal, hasTs := decoded["timestamp"]
	if !hasMsg || !hasTs {
		return
	}

	message, _ = msgVal.(string)
	timestamp, _ = tsVal.(*big.Int)
	if timestamp == nil {
		return
	}
	ok = true
	return
}

// -------------------------------------------------------------
// PHẦN 4: THỐNG KÊ & HIỂN THỊ KẾT QUẢ
// -------------------------------------------------------------

func calculateStats(proto string, totalRounds int, rttLats, serverLats []int64) LatencyStats {
	stats := LatencyStats{
		Proto:           proto,
		TotalRounds:     totalRounds,
		Success:         len(rttLats),
		Latencies:       rttLats,
		ServerLatencies: serverLats,
	}
	if len(rttLats) > 0 {
		sorted := make([]int64, len(rttLats))
		copy(sorted, rttLats)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

		stats.Min = sorted[0]
		stats.Max = sorted[len(sorted)-1]

		var sum int64
		for _, v := range sorted {
			sum += v
		}
		stats.Avg = sum / int64(len(sorted))
		stats.P50 = sorted[len(sorted)*50/100]
		stats.P95 = sorted[len(sorted)*95/100]
	}

	if len(serverLats) > 0 {
		sorted := make([]int64, len(serverLats))
		copy(sorted, serverLats)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

		stats.ServerMin = sorted[0]
		stats.ServerMax = sorted[len(sorted)-1]

		var sum int64
		for _, v := range sorted {
			sum += v
		}
		stats.ServerAvg = sum / int64(len(sorted))
	}

	return stats
}

func printStatsTable(title string, p *ChainParams, s *LatencyStats) {
	pct := 0
	if s.TotalRounds > 0 {
		pct = s.Success * 100 / s.TotalRounds
	}

	fmt.Printf("\n═════════════════════════════════════════════════════════════\n")
	fmt.Printf("  📊 KẾT QUẢ ĐO ĐỘ TRỄ CHI TIẾT - [%s]\n", title)
	fmt.Printf("═════════════════════════════════════════════════════════════\n")
	fmt.Printf("  ⛓️  Target Chain:         %s\n", p.ChainName)
	fmt.Printf("  📤 Thành công:           %d/%d rounds (%d%%)\n", s.Success, s.TotalRounds, pct)
	fmt.Printf("  ───────────────────────────────────────────────────────────\n")
	if len(s.ServerLatencies) > 0 {
		fmt.Printf("  1️⃣  ĐỘ TRỄ 1-CHIỀU (Client ➡️ Server Mining 1 Block):\n")
		fmt.Printf("     • Trung bình (Average): %d ms\n", s.ServerAvg)
		fmt.Printf("     • Nhanh nhất (Min):     %d ms\n", s.ServerMin)
		fmt.Printf("     • Lâu nhất (Max):       %d ms\n", s.ServerMax)
		fmt.Printf("  ───────────────────────────────────────────────────────────\n")
	}
	fmt.Printf("  2️⃣  ĐỘ TRỄ KHÉP KÍN 2-CHIỀU (Ping-Pong RTT 2 Blocks EVM):\n")
	fmt.Printf("     • Trung bình (Average): %d ms\n", s.Avg)
	fmt.Printf("     • Nhanh nhất (Min):     %d ms\n", s.Min)
	fmt.Printf("     • Lâu nhất (Max):       %d ms\n", s.Max)
	fmt.Printf("     • Phân vị P50 (Median): %d ms\n", s.P50)
	fmt.Printf("     • Phân vị P95:          %d ms\n", s.P95)
	fmt.Printf("═════════════════════════════════════════════════════════════\n")
}

func printComparisonTable(rpcStats, tcpStats *LatencyStats) {
	fmt.Printf("\n═════════════════════════════════════════════════════════════\n")
	fmt.Printf("  ⚖️  BẢNG SO SÁNH ĐỘ TRỄ: RPC (HTTP+WS) vs TCP (NATIVE)\n")
	fmt.Printf("═════════════════════════════════════════════════════════════\n")
	fmt.Printf("  %-24s | %-16s | %-16s\n", "Chỉ số thống kê", "RPC (HTTP + WS)", "TCP (Native)")
	fmt.Printf("  ─────────────────────────┼──────────────────┼─────────────────\n")
	fmt.Printf("  %-24s | %-16s | %-16s\n", "Thành công", fmt.Sprintf("%d/%d", rpcStats.Success, rpcStats.TotalRounds), fmt.Sprintf("%d/%d", tcpStats.Success, tcpStats.TotalRounds))
	fmt.Printf("  %-24s | %-16s | %-16s\n", "Server 1-Block (Avg)", fmt.Sprintf("%d ms", rpcStats.ServerAvg), fmt.Sprintf("%d ms", tcpStats.ServerAvg))
	fmt.Printf("  %-24s | %-16s | %-16s\n", "Ping-Pong RTT (Avg)", fmt.Sprintf("%d ms", rpcStats.Avg), fmt.Sprintf("%d ms", tcpStats.Avg))
	fmt.Printf("  %-24s | %-16s | %-16s\n", "RTT Nhanh nhất (Min)", fmt.Sprintf("%d ms", rpcStats.Min), fmt.Sprintf("%d ms", tcpStats.Min))
	fmt.Printf("  %-24s | %-16s | %-16s\n", "RTT Lâu nhất (Max)", fmt.Sprintf("%d ms", rpcStats.Max), fmt.Sprintf("%d ms", tcpStats.Max))
	fmt.Printf("  %-24s | %-16s | %-16s\n", "RTT Phân vị P50", fmt.Sprintf("%d ms", rpcStats.P50), fmt.Sprintf("%d ms", tcpStats.P50))
	fmt.Printf("  %-24s | %-16s | %-16s\n", "RTT Phân vị P95", fmt.Sprintf("%d ms", rpcStats.P95), fmt.Sprintf("%d ms", tcpStats.P95))
	fmt.Printf("═════════════════════════════════════════════════════════════\n")
}

// -------------------------------------------------------------
// PHẦN 5: CHẾ ĐỘ CHAT TAY THỦ CÔNG (INTERACTIVE)
// -------------------------------------------------------------

func runInteractiveChat(p *ChainParams, contractAddr common.Address, contractAbi abi.ABI, proto string, userRole int) {
	var myAddr, targetAddr common.Address
	var myKeyHex string

	if userRole == 1 {
		myAddr = p.User1Addr
		targetAddr = p.User2Addr
		myKeyHex = p.User1Key
	} else {
		myAddr = p.User2Addr
		targetAddr = p.User1Addr
		myKeyHex = p.User2Key
	}

	fmt.Printf("💬 CHAT MODE KHỞI ĐỘNG (User %d: %s ➡️  Target: %s)\n", userRole, myAddr.Hex()[:10], targetAddr.Hex()[:10])

	if strings.ToLower(proto) == "tcp" {
		cfg := &tcp_config.ClientConfig{
			PrivateKey_:             p.BlsKey,
			ParentAddress:           myAddr.Hex(),
			ParentConnectionAddress: p.TcpNode,
			ParentConnectionType:    "client",
			Version_:                "0.0.1.0",
			ChainId:                 uint64(p.ChainId),
			NationId:                1,
		}
		cli, err := client_tcp.NewClient(cfg)
		if err != nil {
			log.Fatalf("❌ Lỗi TCP: %v", err)
		}
		eventCh, _ := cli.ParentSubcribes([]common.Address{contractAddr})

		go func() {
			for evt := range eventCh {
				for _, logItem := range evt.EventLogList() {
					from, to, text, ts, ok := unpackTcpLogItem(contractAbi, logItem.Topics(), logItem.Data())
					if ok && to == myAddr {
						lat := time.Now().UnixMilli() - ts.Int64()
						fmt.Printf("\n[📥 NHẬN từ %s] (Độ trễ: %d ms): %s\n> ", from.Hex()[:8], lat, text)
					}
				}
			}
		}()

		fmt.Println("💬 Bạn có thể gõ tin nhắn và Enter để gửi qua TCP:")
		scanner := bufio.NewScanner(os.Stdin)
		fmt.Print("> ")
		for scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				fmt.Print("> ")
				continue
			}
			t0 := time.Now()
			ts := big.NewInt(t0.UnixMilli())
			payload, _ := contractAbi.Pack("sendMessage", targetAddr, text, ts)
			_, err := tx_helper.SendTransaction(
				"sendMessage", cli, cfg, contractAddr, myAddr, payload,
				&tx_models.TxOptions{MaxGas: 5000000},
			)
			if err != nil {
				fmt.Printf("❌ Lỗi gửi: %v\n> ", err)
			} else {
				fmt.Printf("   [📤 ĐÃ GỬI] Send latency: %v\n> ", time.Since(t0))
			}
		}
	} else {
		// RPC Mode
		cli, err := ethclient.Dial(p.RpcUrl)
		if err != nil {
			log.Fatalf("❌ Lỗi RPC: %v", err)
		}
		ws, err := ethclient.Dial(p.WsUrl)
		if err != nil {
			log.Fatalf("❌ Lỗi WS: %v", err)
		}
		_, pk, _ := keyToAccount(myKeyHex)

		logs := make(chan types.Log)
		sub, _ := ws.SubscribeFilterLogs(context.Background(), ethereum.FilterQuery{Addresses: []common.Address{contractAddr}}, logs)
		defer sub.Unsubscribe()

		go func() {
			for vLog := range logs {
				from, to, text, ts, ok := unpackMessageSentEvent(contractAbi, vLog.Topics, vLog.Data)
				if ok && to == myAddr {
					lat := time.Now().UnixMilli() - ts.Int64()
					fmt.Printf("\n[📥 NHẬN từ %s] (Độ trễ: %d ms): %s\n> ", from.Hex()[:8], lat, text)
				}
			}
		}()

		fmt.Println("💬 Bạn có thể gõ tin nhắn và Enter để gửi qua RPC:")
		scanner := bufio.NewScanner(os.Stdin)
		fmt.Print("> ")
		for scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				fmt.Print("> ")
				continue
			}
			t0 := time.Now()
			ts := big.NewInt(t0.UnixMilli())
			payload, _ := contractAbi.Pack("sendMessage", targetAddr, text, ts)
			nonce, _ := cli.PendingNonceAt(context.Background(), myAddr)
			gasPrice, _ := cli.SuggestGasPrice(context.Background())
			if gasPrice == nil || gasPrice.Sign() == 0 {
				gasPrice = big.NewInt(2000000000)
			}
			tx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), 5000000, gasPrice, payload)
			signed, _ := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(p.ChainId)), pk)
			err := cli.SendTransaction(context.Background(), signed)
			if err != nil {
				fmt.Printf("❌ Lỗi gửi: %v\n> ", err)
			} else {
				fmt.Printf("   [📤 ĐÃ GỬI] Send latency: %v\n> ", time.Since(t0))
			}
		}
	}
}
