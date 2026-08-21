/*
 * BÀI TEST: 32-xapian-parallel-stress
 * MÔ TẢ   : Stress test khả năng xử lý song song các truy vấn Xapian (GetData_View & QuerySearch_View)
 *           bằng Golang Goroutines Worker Pool trong thời gian 1 phút (hoặc tùy chỉnh qua flag).
 * GỌI     : Đa luồng Goroutine liên tục gửi eth_call, đồng thời GIẢI MÃ (UNPACK) và XÁC THỰC (VERIFY)
 *           100% dữ liệu trả về (tên sản phẩm, giá, thương hiệu, kết quả tìm kiếm).
 * KỲ VỌNG : 100% truy vấn thành công và dữ liệu trả về chính xác tuyệt đối, không có lỗi (errorReqs = 0).
 */
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type Config struct {
	RPCUrl      string            `json:"rpc_url"`
	RPCNodes    map[string]string `json:"rpc_nodes"`
	PrivateKeys []string          `json:"private_keys"`
	PrivateKey  string            `json:"private_key"`
	ChainID     int64             `json:"chain_id"`
}

type ExpectedEvent struct {
	Name     string   `json:"name"`
	Contains []string `json:"contains"`
}

type DataPayload struct {
	Contract       string          `json:"contract"`
	AbiPath        string          `json:"abi_path"`
	Action         string          `json:"action"` // "deploy", "send", "call"
	Method         string          `json:"method"`
	Args           []interface{}   `json:"args"`
	InputData      string          `json:"input_data"`
	ExpectedEvents []ExpectedEvent `json:"expected_events"`
	ExpectedOutput []string        `json:"expected_output"`
}

func loadConfig(path string) (*Config, error) {
	candidates := []string{
		path,
		"../config.json",
		"../../config.json",
		"/home/abc/nhat/consensus-chain/metanode-suite/config.json",
	}

	var raw []byte
	var err error
	for _, p := range candidates {
		if p == "" {
			continue
		}
		raw, err = os.ReadFile(p)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("không thể đọc file config: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("lỗi parse JSON config: %v", err)
	}

	if cfg.PrivateKey == "" && len(cfg.PrivateKeys) > 0 {
		cfg.PrivateKey = cfg.PrivateKeys[0]
	}

	return &cfg, nil
}

func loadABI() (abi.ABI, error) {
	abiPaths := []string{
		"../../test_read_wire_xapian/abi/xapian.json",
		"../test_read_wire_xapian/abi/xapian.json",
		"/home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test_read_wire_xapian/abi/xapian.json",
	}
	for _, p := range abiPaths {
		raw, err := os.ReadFile(p)
		if err == nil {
			parsed, err := abi.JSON(strings.NewReader(string(raw)))
			if err == nil {
				return parsed, nil
			}
		}
	}
	return abi.ABI{}, fmt.Errorf("không thể tìm hoặc parse file abi xapian.json")
}

func loadSetupData() ([]DataPayload, error) {
	dataPaths := []string{
		"../../test_read_wire_xapian/data-xapian-v2.json",
		"../test_read_wire_xapian/data-xapian-v2.json",
		"/home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test_read_wire_xapian/data-xapian-v2.json",
	}
	for _, p := range dataPaths {
		raw, err := os.ReadFile(p)
		if err == nil {
			var d []DataPayload
			if err := json.Unmarshal(raw, &d); err == nil {
				return d, nil
			}
		}
	}
	return nil, fmt.Errorf("không thể tìm hoặc parse file data-xapian-v2.json")
}

func convertToType(t abi.Type, val interface{}) (interface{}, error) {
	strVal := fmt.Sprintf("%v", val)
	switch t.T {
	case abi.IntTy, abi.UintTy:
		n, ok := new(big.Int).SetString(strVal, 10)
		if !ok {
			return nil, fmt.Errorf("không thể parse số: %s", strVal)
		}
		if t.T == abi.UintTy {
			switch t.Size {
			case 8:
				return uint8(n.Uint64()), nil
			case 16:
				return uint16(n.Uint64()), nil
			case 32:
				return uint32(n.Uint64()), nil
			case 64:
				return uint64(n.Uint64()), nil
			}
		} else {
			switch t.Size {
			case 8:
				return int8(n.Int64()), nil
			case 16:
				return int16(n.Int64()), nil
			case 32:
				return int32(n.Int64()), nil
			case 64:
				return int64(n.Int64()), nil
			}
		}
		return n, nil
	case abi.StringTy:
		return strVal, nil
	case abi.AddressTy:
		return common.HexToAddress(strVal), nil
	case abi.BoolTy:
		if strVal == "true" || strVal == "1" {
			return true, nil
		}
		return false, nil
	}
	return val, nil
}

func prepareArgs(method abi.Method, jsonArgs []interface{}) []interface{} {
	var packedArgs []interface{}
	for i, input := range method.Inputs {
		rawVal := jsonArgs[i]
		val, err := convertToType(input.Type, rawVal)
		if err != nil {
			log.Fatalf("❌ Lỗi chuyển đổi tham số [%d] (%s): %v", i, input.Type.String(), err)
		}
		packedArgs = append(packedArgs, val)
	}
	return packedArgs
}

// deployAndSetupContract tự động chạy toàn bộ pipeline của data-xapian-v2.json
func deployAndSetupContract(client *ethclient.Client, cfg *Config, contractAbi abi.ABI) (common.Address, error) {
	fmt.Println("\n📦 [AUTO DEPLOY] Đang Deploy Contract Xapian Products & Setup Dữ liệu...")

	pk, err := crypto.HexToECDSA(cfg.PrivateKey)
	if err != nil {
		return common.Address{}, fmt.Errorf("private key không hợp lệ: %v", err)
	}
	fromAddr := crypto.PubkeyToAddress(pk.PublicKey)

	tasks, err := loadSetupData()
	if err != nil {
		return common.Address{}, err
	}

	var contractAddr common.Address

	for idx, task := range tasks {
		action := strings.ToLower(task.Action)

		if action == "deploy" {
			if !strings.HasPrefix(task.InputData, "0x") {
				task.InputData = "0x" + task.InputData
			}
			bytecode, err := hexutil.Decode(task.InputData)
			if err != nil {
				return common.Address{}, fmt.Errorf("lỗi giải mã bytecode deploy: %v", err)
			}

			nonce, err := client.PendingNonceAt(context.Background(), fromAddr)
			if err != nil {
				return common.Address{}, fmt.Errorf("lỗi lấy nonce deploy: %v", err)
			}
			gasPrice, _ := client.SuggestGasPrice(context.Background())
			if gasPrice == nil || gasPrice.Sign() == 0 {
				gasPrice = big.NewInt(100000)
			}

			gasLimit, err := client.EstimateGas(context.Background(), ethereum.CallMsg{
				From: fromAddr,
				Data: bytecode,
			})
			if err != nil {
				gasLimit = 5000000
			} else {
				gasLimit += 500000
			}

			tx := types.NewContractCreation(nonce, big.NewInt(0), gasLimit, gasPrice, bytecode)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)
			if err != nil {
				return common.Address{}, fmt.Errorf("lỗi ký deploy tx: %v", err)
			}

			if err := client.SendTransaction(context.Background(), signedTx); err != nil {
				return common.Address{}, fmt.Errorf("lỗi gửi deploy tx: %v", err)
			}
			fmt.Printf("   🚀 Task %d [DEPLOY]: Tx %s (Nonce %d) -> Chờ mined...", idx+1, signedTx.Hash().Hex(), nonce)

			for {
				rcpt, err := client.TransactionReceipt(context.Background(), signedTx.Hash())
				if err == nil && rcpt != nil && rcpt.BlockNumber != nil && rcpt.BlockNumber.Uint64() > 0 {
					if rcpt.Status != 1 {
						return common.Address{}, fmt.Errorf("deploy tx bị revert")
					}
					contractAddr = rcpt.ContractAddress
					fmt.Printf(" ✅ Thành công! Block: %d | Contract: %s\n", rcpt.BlockNumber.Uint64(), contractAddr.Hex())
					break
				}
				time.Sleep(400 * time.Millisecond)
			}
		} else if action == "send" || action == "write" {
			method, ok := contractAbi.Methods[task.Method]
			if !ok {
				return common.Address{}, fmt.Errorf("không tìm thấy method %s trong ABI", task.Method)
			}
			parsedArgs := prepareArgs(method, task.Args)
			payloadData, err := contractAbi.Pack(task.Method, parsedArgs...)
			if err != nil {
				return common.Address{}, fmt.Errorf("lỗi pack %s: %v", task.Method, err)
			}

			nonce, err := client.PendingNonceAt(context.Background(), fromAddr)
			if err != nil {
				return common.Address{}, fmt.Errorf("lỗi lấy nonce cho %s: %v", task.Method, err)
			}
			gasPrice, _ := client.SuggestGasPrice(context.Background())
			if gasPrice == nil || gasPrice.Sign() == 0 {
				gasPrice = big.NewInt(100000)
			}

			gasLimit, err := client.EstimateGas(context.Background(), ethereum.CallMsg{
				From: fromAddr,
				To:   &contractAddr,
				Data: payloadData,
			})
			if err != nil {
				gasLimit = 3000000
			} else {
				gasLimit += 100000
			}

			tx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), gasLimit, gasPrice, payloadData)
			signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(cfg.ChainID)), pk)
			if err != nil {
				return common.Address{}, fmt.Errorf("lỗi ký tx %s: %v", task.Method, err)
			}

			if err := client.SendTransaction(context.Background(), signedTx); err != nil {
				return common.Address{}, fmt.Errorf("lỗi gửi tx %s: %v", task.Method, err)
			}
			fmt.Printf("   ▶️ Task %d [%s]: Tx %s (Nonce %d) -> Chờ mined...", idx+1, task.Method, signedTx.Hash().Hex(), nonce)

			for {
				rcpt, err := client.TransactionReceipt(context.Background(), signedTx.Hash())
				if err == nil && rcpt != nil && rcpt.BlockNumber != nil && rcpt.BlockNumber.Uint64() > 0 {
					if rcpt.Status != 1 {
						return common.Address{}, fmt.Errorf("task %s bị revert", task.Method)
					}
					fmt.Printf(" ✅ OK (Gas: %d)\n", rcpt.GasUsed)

					// Xác thực Event Logs từ Receipt nếu có cấu hình expected_events
					if len(task.ExpectedEvents) > 0 {
						verifiedMap := make(map[int]bool)
						for _, vLog := range rcpt.Logs {
							if len(vLog.Topics) == 0 {
								continue
							}
							event, err := contractAbi.EventByID(vLog.Topics[0])
							if err != nil {
								continue
							}
							logDataStr := string(vLog.Data)
							if len(vLog.Data) > 0 {
								unpacked, err := event.Inputs.NonIndexed().Unpack(vLog.Data)
								if err == nil {
									logDataStr += fmt.Sprintf(" %v", unpacked)
								}
							}
							for eIdx, expected := range task.ExpectedEvents {
								if expected.Name == event.Name {
									allMatch := true
									for _, kw := range expected.Contains {
										if !strings.Contains(logDataStr, kw) {
											allMatch = false
											break
										}
									}
									if allMatch {
										verifiedMap[eIdx] = true
										fmt.Printf("      📋 [Verified Event Log] '%s' -> Khớp: %v\n", expected.Name, expected.Contains)
									}
								}
							}
						}
						for eIdx, expected := range task.ExpectedEvents {
							if !verifiedMap[eIdx] {
								return common.Address{}, fmt.Errorf("không tìm thấy hoặc không khớp Event Log '%s' (%v)", expected.Name, expected.Contains)
							}
						}
					}
					break
				}
				time.Sleep(400 * time.Millisecond)
			}
		} else if action == "call" || action == "read" {
			method, ok := contractAbi.Methods[task.Method]
			if !ok {
				return common.Address{}, fmt.Errorf("không tìm thấy method %s trong ABI", task.Method)
			}
			parsedArgs := prepareArgs(method, task.Args)
			payloadData, err := contractAbi.Pack(task.Method, parsedArgs...)
			if err != nil {
				return common.Address{}, fmt.Errorf("lỗi pack call %s: %v", task.Method, err)
			}

			msg := ethereum.CallMsg{
				To:   &contractAddr,
				Data: payloadData,
			}
			res, err := client.CallContract(context.Background(), msg, nil)
			if err != nil || len(res) == 0 {
				return common.Address{}, fmt.Errorf("call verification %s thất bại: %v", task.Method, err)
			}
			fmt.Printf("   🔍 Task %d [%s]: eth_call kiểm tra ban đầu -> ✅ OK\n", idx+1, task.Method)
		}
	}

	fmt.Println("🎉 Khởi tạo dữ liệu Xapian Database hoàn tất 100%!")
	return contractAddr, nil
}

type QueryCase struct {
	Method          string
	ArgDescription  string
	Payload         []byte
	ExpectedOutputs []string
}

func main() {
	configPath := flag.String("config", "../config.json", "Đường dẫn file config.json")
	duration := flag.Duration("duration", 1*time.Minute, "Thời gian chạy stress test (mặc định 1m, ví dụ: 60s, 2m)")
	workers := flag.Int("workers", 50, "Số lượng luồng song song (Goroutines)")
	mode := flag.String("mode", "mixed", "Chế độ test: mixed (cả hai), getdata (chỉ đọc doc), search (chỉ tìm kiếm)")
	delayMs := flag.Int("delay", 2, "Độ trễ nghỉ giữa 2 request mỗi goroutine (ms, mặc định 2ms để tránh nghẽn I/O log server)")
	contractHex := flag.String("contract", "", "Địa chỉ Contract Xapian (nếu bỏ trống sẽ tự động deploy & setup)")
	flag.Parse()

	fmt.Println("==========================================================")
	fmt.Println("⚡ BÀI TEST 32: XAPIAN HIGH-CONCURRENCY PARALLEL STRESS TEST")
	fmt.Println("==========================================================")
	fmt.Printf("⏱️  Thời lượng test : %v\n", *duration)
	fmt.Printf("👥 Số luồng song song: %d Goroutines\n", *workers)
	fmt.Printf("🎯 Chế độ truy vấn  : %s\n", *mode)
	fmt.Printf("⏱️  Pacing Delay     : %d ms/goroutine\n", *delayMs)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc config: %v", err)
	}
	fmt.Printf("🔗 RPC Endpoint      : %s\n", cfg.RPCUrl)

	contractAbi, err := loadABI()
	if err != nil {
		log.Fatalf("❌ Lỗi nạp ABI: %v", err)
	}

	// Tạo HTTP Client tối ưu Connection Pool (Keep-Alive) và Timeout an toàn (30s)
	httpTransport := &http.Transport{
		MaxIdleConns:        3000,
		MaxIdleConnsPerHost: 1000,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}
	httpClient := &http.Client{
		Transport: httpTransport,
		Timeout:   30 * time.Second,
	}

	rpcUrls := []string{cfg.RPCUrl}
	if len(cfg.RPCNodes) > 0 {
		rpcUrls = nil
		for _, u := range cfg.RPCNodes {
			if u != "" {
				rpcUrls = append(rpcUrls, u)
			}
		}
		if len(rpcUrls) == 0 {
			rpcUrls = []string{cfg.RPCUrl}
		}
	}

	var clients []*ethclient.Client
	for _, u := range rpcUrls {
		rpcClient, err := rpc.DialHTTPWithClient(u, httpClient)
		if err != nil {
			log.Fatalf("❌ Lỗi kết nối RPC (%s): %v", u, err)
		}
		clients = append(clients, ethclient.NewClient(rpcClient))
	}
	primaryClient := clients[0]
	fmt.Printf("🔗 Đang phân phối tải trên %d RPC Node: %s\n", len(rpcUrls), strings.Join(rpcUrls, ", "))

	// Lấy hoặc Deploy Contract
	var contractAddr common.Address
	if *contractHex != "" {
		contractAddr = common.HexToAddress(*contractHex)
		fmt.Printf("📌 Sử dụng Contract đã chỉ định: %s\n", contractAddr.Hex())
	} else {
		deployedAddr, err := deployAndSetupContract(primaryClient, cfg, contractAbi)
		if err != nil {
			log.Fatalf("❌ Setup Xapian Contract thất bại: %v", err)
		}
		contractAddr = deployedAddr
	}

	// Chuẩn bị các test cases cùng KỲ VỌNG DỮ LIỆU ĐẦU RA (Ground-Truth Verification)
	var searchCases []QueryCase
	searchTerms := []struct {
		term     string
		expected []string
	}{
		{"iphone", []string{"+1 +1"}},
		{"samsung", []string{"+1 +1"}},
		{"macbook", []string{"+1 +1"}},
	}
	for _, item := range searchTerms {
		data, err := contractAbi.Pack("runStep5b_QuerySearch_View", item.term)
		if err != nil {
			log.Fatalf("❌ Pack ABI QuerySearch_View lỗi: %v", err)
		}
		searchCases = append(searchCases, QueryCase{
			Method:          "runStep5b_QuerySearch_View",
			ArgDescription:  item.term,
			Payload:         data,
			ExpectedOutputs: item.expected,
		})
	}

	var getDataCases []QueryCase
	docTerms := []struct {
		id       int64
		expected []string
	}{
		{0, []string{"Iphone 13 Pro UPDATED", "electronics", "apple", "89999", "84999", "false", "Flash sale da ket thuc"}},
		{1, []string{"Samsung Galaxy S22", "electronics", "samsung", "79900"}},
		{2, []string{"Macbook Pro 14", "electronics", "apple", "199900"}},
	}
	for _, item := range docTerms {
		data, err := contractAbi.Pack("runStep5c_GetData_View", big.NewInt(item.id))
		if err != nil {
			log.Fatalf("❌ Pack ABI GetData_View lỗi: %v", err)
		}
		getDataCases = append(getDataCases, QueryCase{
			Method:          "runStep5c_GetData_View",
			ArgDescription:  fmt.Sprintf("docId=%d", item.id),
			Payload:         data,
			ExpectedOutputs: item.expected,
		})
	}

	// Thống kê Metrics Atomic
	var totalReqs uint64
	var successReqs uint64
	var errorReqs uint64
	var verifiedDataReqs uint64

	var latenciesMu sync.Mutex
	latencies := make([]time.Duration, 0, 50000)

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	// Lắng nghe tín hiệu Ctrl+C để dừng sớm và in thống kê
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n🛑 Nhận tín hiệu dừng từ người dùng! Đang tổng hợp báo cáo...")
		cancel()
	}()

	fmt.Println("\n==========================================================")
	fmt.Printf("🚀 BẮT ĐẦU XẢ BÃO TRUY VẤN & XÁC THỰC DỮ LIỆU VỚI %d GOROUTINES...\n", *workers)
	fmt.Println("==========================================================")
	fmt.Println("📋 CHI TIẾT CÁC HÀM TRUY VẤN ĐANG CHẠY SONG SONG:")
	fmt.Println("   1. [GET DATA]     runStep5c_GetData_View(docId=0) -> Xác thực: 'Iphone 13 Pro UPDATED', 'electronics', 'apple', '89999', '84999', 'false', 'Flash sale da ket thuc'")
	fmt.Println("   2. [GET DATA]     runStep5c_GetData_View(docId=1) -> Xác thực: 'Samsung Galaxy S22', 'electronics', 'samsung', '79900'")
	fmt.Println("   3. [GET DATA]     runStep5c_GetData_View(docId=2) -> Xác thực: 'Macbook Pro 14', 'electronics', 'apple', '199900'")
	fmt.Println("   4. [SEARCH QUERY] runStep5b_QuerySearch_View('iphone')  -> Xác thực kết quả: [+1 +1] (1 doc khớp)")
	fmt.Println("   5. [SEARCH QUERY] runStep5b_QuerySearch_View('samsung') -> Xác thực kết quả: [+1 +1] (1 doc khớp)")
	fmt.Println("   6. [SEARCH QUERY] runStep5b_QuerySearch_View('macbook') -> Xác thực kết quả: [+1 +1] (1 doc khớp)")
	fmt.Println("----------------------------------------------------------")

	startTime := time.Now()
	var wg sync.WaitGroup

	// Kích hoạt N worker song song
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			client := clients[workerID%len(clients)]
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID*1000)))

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Chọn kiểu truy vấn theo mode
				var qCase QueryCase
				isSearch := false
				if *mode == "search" {
					isSearch = true
				} else if *mode == "getdata" {
					isSearch = false
				} else {
					// mixed: random 50/50
					isSearch = rng.Intn(2) == 0
				}

				if isSearch {
					qCase = searchCases[rng.Intn(len(searchCases))]
				} else {
					qCase = getDataCases[rng.Intn(len(getDataCases))]
				}

				msg := ethereum.CallMsg{
					To:   &contractAddr,
					Data: qCase.Payload,
				}

				reqStart := time.Now()
				resBytes, err := client.CallContract(context.Background(), msg, nil)
				lat := time.Since(reqStart)

				atomic.AddUint64(&totalReqs, 1)

				if err != nil || len(resBytes) == 0 {
					errCount := atomic.AddUint64(&errorReqs, 1)
					if errCount == 1 {
						fmt.Printf("\n🚨 [PHÁT HIỆN LỖI RPC - DỪNG BÀI TEST NGAY LẬP TỨC]: %v (resBytes len=%d)\n", err, len(resBytes))
						cancel()
					}
					return
				} else {
					// GIẢI MÃ VÀ XÁC THỰC DỮ LIỆU ĐẦU RA 100%
					unpacked, unpackErr := contractAbi.Unpack(qCase.Method, resBytes)
					if unpackErr != nil {
						errCount := atomic.AddUint64(&errorReqs, 1)
						if errCount == 1 {
							fmt.Printf("\n🚨 [LỖI UNPACK ABI - DỪNG BÀI TEST NGAY LẬP TỨC - %s]: %v\n", qCase.Method, unpackErr)
							cancel()
						}
						return
					} else {
						outputStr := fmt.Sprintf("%+v", unpacked)
						allMatch := true
						for _, expected := range qCase.ExpectedOutputs {
							if !strings.Contains(outputStr, expected) {
								allMatch = false
								errCount := atomic.AddUint64(&errorReqs, 1)
								if errCount == 1 {
									fmt.Printf("\n🚨 [LỖI DỮ LIỆU KHÔNG KHỚP - DỪNG BÀI TEST NGAY LẬP TỨC]:\n   Method: %s (%s)\n   Kỳ vọng chứa: '%s'\n   Nhận được   : '%s'\n",
										qCase.Method, qCase.ArgDescription, expected, outputStr)
									cancel()
								}
								return
							}
						}
						if allMatch {
							atomic.AddUint64(&successReqs, 1)
							atomic.AddUint64(&verifiedDataReqs, 1)
						}
					}
				}

				// Lưu mẫu latency để vẽ thống kê
				latenciesMu.Lock()
				if len(latencies) < 100000 {
					latencies = append(latencies, lat)
				}
				latenciesMu.Unlock()

				if atomic.LoadUint64(&errorReqs) > 0 {
					return
				}

				if *delayMs > 0 {
					time.Sleep(time.Duration(*delayMs) * time.Millisecond)
				}
			}
		}(w)
	}

	// Goroutine in tiến độ realtime mỗi 3 giây
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	var lastTotal uint64
	var lastTime = startTime

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				curTotal := atomic.LoadUint64(&totalReqs)
				curVerified := atomic.LoadUint64(&verifiedDataReqs)
				curErr := atomic.LoadUint64(&errorReqs)

				elapsed := t.Sub(startTime)
				intervalSec := t.Sub(lastTime).Seconds()
				instantRPS := float64(curTotal-lastTotal) / intervalSec

				lastTotal = curTotal
				lastTime = t

				fmt.Printf("⏳ [%4.1fs / %v] Total: %6d | RPS: %6.1f req/s | Success & Verified: %6d | Errors: %d\n",
					elapsed.Seconds(), *duration, curTotal, instantRPS, curVerified, curErr)
			}
		}
	}()

	// Chờ tất cả worker hoàn tất
	wg.Wait()
	totalElapsed := time.Since(startTime)

	// Tính toán thống kê Latency
	latenciesMu.Lock()
	sortedLatencies := make([]time.Duration, len(latencies))
	copy(sortedLatencies, latencies)
	latenciesMu.Unlock()

	sort.Slice(sortedLatencies, func(i, j int) bool {
		return sortedLatencies[i] < sortedLatencies[j]
	})

	var minLat, maxLat, avgLat, p50Lat, p95Lat, p99Lat time.Duration
	if len(sortedLatencies) > 0 {
		minLat = sortedLatencies[0]
		maxLat = sortedLatencies[len(sortedLatencies)-1]
		var totalDur time.Duration
		for _, l := range sortedLatencies {
			totalDur += l
		}
		avgLat = totalDur / time.Duration(len(sortedLatencies))
		p50Lat = sortedLatencies[int(float64(len(sortedLatencies))*0.50)]
		p95Lat = sortedLatencies[int(float64(len(sortedLatencies))*0.95)]
		p99Lat = sortedLatencies[int(float64(len(sortedLatencies))*0.99)]
	}

	totalRPS := float64(totalReqs) / totalElapsed.Seconds()
	successRate := 0.0
	if totalReqs > 0 {
		successRate = float64(successReqs) / float64(totalReqs) * 100.0
	}

	fmt.Println("\n==========================================================")
	fmt.Println("📊 BÁO CÁO TỔNG KẾT STRESS TEST XAPIAN PARALLEL CONCURRENCY")
	fmt.Println("==========================================================")
	fmt.Printf("⏱️  Thời gian thực thi      : %.2f giây (Kỳ vọng: %v)\n", totalElapsed.Seconds(), *duration)
	fmt.Printf("👥 Số Goroutine song song   : %d workers (Delay: %d ms)\n", *workers, *delayMs)
	fmt.Printf("📨 Tổng số request gửi      : %d\n", totalReqs)
	fmt.Printf("✅ Request thành công & đúng: %d (%.2f%%)\n", successReqs, successRate)
	fmt.Printf("🔍 Đã xác thực dữ liệu 100%%: %d requests\n", verifiedDataReqs)
	fmt.Printf("❌ Số request thất bại      : %d\n", errorReqs)
	fmt.Printf("⚡ Tốc độ trung bình        : %.2f RPS (Requests/Second)\n", totalRPS)
	fmt.Println("----------------------------------------------------------")
	fmt.Println("📈 PHÂN BỔ ĐỘ TRỄ (LATENCY):")
	fmt.Printf("   - Nhanh nhất (Min)       : %v\n", minLat)
	fmt.Printf("   - Trung bình (Avg)       : %v\n", avgLat)
	fmt.Printf("   - Median (p50)           : %v\n", p50Lat)
	fmt.Printf("   - 95th Percentile        : %v\n", p95Lat)
	fmt.Printf("   - 99th Percentile        : %v\n", p99Lat)
	fmt.Printf("   - Chậm nhất (Max)        : %v\n", maxLat)
	fmt.Println("==========================================================")

	// BẮT BUỘC: Nếu có bất kỳ request nào fail hoặc dữ liệu sai -> Báo lỗi và exit 1
	if errorReqs == 0 && totalReqs > 0 {
		fmt.Println("🎉 TEST PASSED: Xapian Engine và Validator RPC xử lý song song và TRẢ DỮ LIỆU CHÍNH XÁC 100%!")
	} else {
		fmt.Printf("❌ TEST FAILED: Có %d/%d request bị lỗi hoặc trả dữ liệu sai lệch!\n", errorReqs, totalReqs)
		os.Exit(1)
	}
}
