package main

import (
	"context"
	"crypto/ecdsa"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/config"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

//go:embed contract/evm_recovery.json
var abiJSON string

//go:embed contract/bytecode.hex
var bytecodeHex string

const defaultContractFile = "/tmp/evm_recovery_contract.json"

type ContractEntry struct {
	ContractAddress string `json:"contract_address"`
	DeployedAt      string `json:"deployed_at"`
	ChainID         int64  `json:"chain_id"`
	Round           int    `json:"round,omitempty"`
	InitKey         string `json:"init_key,omitempty"`
	InitValue       string `json:"init_value,omitempty"`
}

type ContractStore struct {
	ContractAddress string          `json:"contract_address"` // Contract mới nhất (tương thích ngược)
	DeployedAt      string          `json:"deployed_at"`
	ChainID         int64           `json:"chain_id"`
	History         []ContractEntry `json:"history"` // Danh sách hợp đồng qua các round (tối đa 100)
}

type ExpectedEvent struct {
	Name     string
	Contains []string
}

const maxTrackedContracts = 100
const receiptWaitIterations = 240
const receiptWaitInterval = 500 * time.Millisecond

func dialClient(urlStr string) (*ethclient.Client, error) {
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
		return nil, err
	}
	return ethclient.NewClient(rpcClient), nil
}

func resolveNodeURL(cfg *config.Config, targetNode string, explicitURL string) (string, error) {
	if explicitURL != "" {
		return explicitURL, nil
	}
	if targetNode != "" {
		clean := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(targetNode, "m"), "node"))
		for k, v := range cfg.RPCNodes {
			cleanK := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(k, "m"), "node"))
			if cleanK == clean || strings.EqualFold(k, targetNode) {
				return v, nil
			}
		}
		for k, v := range cfg.SyncNodes {
			cleanK := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(k, "m"), "node"))
			if cleanK == clean || strings.EqualFold(k, targetNode) {
				return v, nil
			}
		}
		return "", fmt.Errorf("không tìm thấy URL cho target-node '%s'", targetNode)
	}
	if cfg.RPCUrl != "" {
		return cfg.RPCUrl, nil
	}
	for _, v := range cfg.RPCNodes {
		return v, nil
	}
	return "", fmt.Errorf("không có RPC URL nào trong cấu hình")
}

func saveContractAddress(filePath string, entry ContractEntry) error {
	var store ContractStore
	data, err := os.ReadFile(filePath)
	if err == nil {
		_ = json.Unmarshal(data, &store)
	}

	store.ContractAddress = entry.ContractAddress
	store.DeployedAt = entry.DeployedAt
	store.ChainID = entry.ChainID

	// Khi bắt đầu Vòng 1: reset hoàn toàn lịch sử cũ để không bị sót contract từ chain cũ đã reset
	if entry.Round <= 1 {
		store.History = []ContractEntry{entry}
	} else {
		alreadyExists := false
		for i, e := range store.History {
			if strings.EqualFold(e.ContractAddress, entry.ContractAddress) {
				store.History[i] = entry
				alreadyExists = true
				break
			}
		}
		if !alreadyExists {
			store.History = append(store.History, entry)
		}
	}

	// Giới hạn tối đa 100 contract: giữ contract cũ nhất và 99 contract mới nhất
	if len(store.History) > maxTrackedContracts {
		oldest := store.History[0]
		recentCount := maxTrackedContracts - 1
		recent := store.History[len(store.History)-recentCount:]
		store.History = append([]ContractEntry{oldest}, recent...)
	}

	bytes, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, bytes, 0644)
}

func loadContractAddresses(filePath string) ([]ContractEntry, error) {
	bytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("lỗi đọc file contract '%s': %w", filePath, err)
	}
	var store ContractStore
	if err := json.Unmarshal(bytes, &store); err != nil {
		var single ContractEntry
		if err2 := json.Unmarshal(bytes, &single); err2 == nil && common.IsHexAddress(single.ContractAddress) {
			return []ContractEntry{single}, nil
		}
		return nil, fmt.Errorf("lỗi parse file contract '%s': %w", filePath, err)
	}

	if len(store.History) == 0 && common.IsHexAddress(store.ContractAddress) {
		store.History = append(store.History, ContractEntry{
			ContractAddress: store.ContractAddress,
			DeployedAt:      store.DeployedAt,
			ChainID:         store.ChainID,
		})
	}

	var valid []ContractEntry
	for _, e := range store.History {
		if common.IsHexAddress(e.ContractAddress) {
			valid = append(valid, e)
		}
	}

	if len(valid) == 0 {
		return nil, fmt.Errorf("không tìm thấy địa chỉ contract hợp lệ trong file '%s'", filePath)
	}
	return valid, nil
}

func waitReceiptAndPoll(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	ctx := context.Background()
	for i := 0; i < receiptWaitIterations; i++ {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			return receipt, nil
		}
		time.Sleep(receiptWaitInterval)
	}
	waited := time.Duration(receiptWaitIterations) * receiptWaitInterval
	return nil, fmt.Errorf("timeout (%s) chờ receipt cho tx %s", waited, txHash.Hex())
}

func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (common.Address, error) {
	ctx := context.Background()
	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return common.Address{}, fmt.Errorf("lỗi lấy nonce: %v", err)
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		gasPrice = big.NewInt(100000)
	}
	gasLimit := uint64(4000000)

	tx := types.NewContractCreation(nonce, big.NewInt(0), gasLimit, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return common.Address{}, fmt.Errorf("lỗi ký tx deploy: %v", err)
	}

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		return common.Address{}, fmt.Errorf("lỗi gửi transaction deploy: %v", err)
	}
	fmt.Printf("   🚀 Đã gửi Tx Deploy EVM Contract. Hash: %s\n", signedTx.Hash().Hex())

	receipt, err := waitReceiptAndPoll(client, signedTx.Hash())
	if err != nil {
		return common.Address{}, err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return common.Address{}, fmt.Errorf("tx deploy bị Revert (gasUsed: %d)", receipt.GasUsed)
	}
	return receipt.ContractAddress, nil
}

func executeSendWithVerification(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, contractAddr common.Address, contractABI abi.ABI, method string, args []interface{}, expectedEvents []ExpectedEvent) (*types.Receipt, error) {
	var data []byte
	var err error
	if len(args) > 0 {
		data, err = contractABI.Pack(method, args...)
	} else {
		data, err = contractABI.Pack(method)
	}
	if err != nil {
		return nil, fmt.Errorf("lỗi pack method %s: %v", method, err)
	}

	ctx := context.Background()
	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, fmt.Errorf("lỗi lấy nonce: %v", err)
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		gasPrice = big.NewInt(100000)
	}
	gasLimit := uint64(4000000)

	tx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return nil, fmt.Errorf("lỗi ký tx: %v", err)
	}

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		return nil, fmt.Errorf("lỗi gửi transaction %s: %v", method, err)
	}
	fmt.Printf("   🚀 Đã gửi Tx (%s). Hash: %s\n", method, signedTx.Hash().Hex())

	receipt, err := waitReceiptAndPoll(client, signedTx.Hash())
	if err != nil {
		return nil, err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return nil, fmt.Errorf("tx %s bị Revert", method)
	}

	if len(expectedEvents) > 0 {
		verifiedEvents := make(map[int]bool)
		for _, vLog := range receipt.Logs {
			if len(vLog.Topics) == 0 {
				continue
			}
			event, err := contractABI.EventByID(vLog.Topics[0])
			if err != nil {
				continue
			}

			logStrBuilder := strings.Builder{}
			for _, topic := range vLog.Topics {
				logStrBuilder.WriteString(topic.Hex() + " ")
			}
			if len(vLog.Data) > 0 {
				unpacked, err := event.Inputs.NonIndexed().Unpack(vLog.Data)
				if err == nil {
					for _, unp := range unpacked {
						logStrBuilder.WriteString(fmt.Sprintf("%v ", unp))
					}
				}
			}
			fullLogStr := logStrBuilder.String()

			for eIdx, expected := range expectedEvents {
				if expected.Name == event.Name {
					allMatch := true
					for _, text := range expected.Contains {
						if !strings.Contains(fullLogStr, text) {
							allMatch = false
							break
						}
					}
					if allMatch {
						verifiedEvents[eIdx] = true
					}
				}
			}
		}

		for eIdx, expected := range expectedEvents {
			if !verifiedEvents[eIdx] {
				return nil, fmt.Errorf("không tìm thấy Event '%s' có chứa: %v trong receipt tx %s", expected.Name, expected.Contains, method)
			}
		}
	}
	return receipt, nil
}

func executeCallWithVerification(client *ethclient.Client, from common.Address, contractAddr common.Address, contractABI abi.ABI, method string, args []interface{}, expectedSubstrings []string) (string, error) {
	var data []byte
	var err error
	if len(args) > 0 {
		data, err = contractABI.Pack(method, args...)
	} else {
		data, err = contractABI.Pack(method)
	}
	if err != nil {
		return "", fmt.Errorf("lỗi pack method %s: %v", method, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := client.CallContract(ctx, ethereum.CallMsg{
		From: from,
		To:   &contractAddr,
		Data: data,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("lỗi eth_call %s: %v", method, err)
	}
	if len(res) == 0 {
		return "", fmt.Errorf("lỗi toàn vẹn dữ liệu: hợp đồng %s trả về kết quả rỗng (0x) khi gọi %s - node không đọc được state sau khi restart/khôi phục snapshot", contractAddr.Hex(), method)
	}

	outputs, err := contractABI.Unpack(method, res)
	if err != nil {
		return "", fmt.Errorf("lỗi unpack output %s: %v", method, err)
	}

	outputStr := fmt.Sprintf("%+v", outputs)
	for _, exp := range expectedSubstrings {
		if !strings.Contains(outputStr, exp) {
			return outputStr, fmt.Errorf("kết quả trả về từ %s thiếu '%s'. Kết quả nhận được: %s", method, exp, outputStr)
		}
	}
	return outputStr, nil
}

func main() {
	modeFlag := flag.String("mode", "setup", "Chế độ: setup | verify-node | verify-cluster | write-state | write-doc")
	configFlag := flag.String("config", "../config.json", "Đường dẫn file config.json")
	targetNodeFlag := flag.String("target-node", "", "ID node cần tương tác (ví dụ: 0, 1, 2, 4)")
	rpcURLFlag := flag.String("rpc-url", "", "Explicit RPC URL (nếu không dùng config/target-node)")
	contractFileFlag := flag.String("contract-file", defaultContractFile, "Đường dẫn lưu/đọc file contract address")
	contractAddrFlag := flag.String("contract-addr", "", "Chỉ định trực tiếp địa chỉ contract đã deploy")
	forceDeployFlag := flag.Bool("force-deploy", false, "Bắt buộc deploy contract mới bỏ qua contract cũ")
	roundFlag := flag.Int("round", 0, "Số thứ tự round (nếu > 0 sẽ gắn tag và tự động deploy contract mới mỗi round)")
	flag.Parse()

	cfg, err := config.LoadConfig(*configFlag)
	if err != nil {
		log.Fatalf("❌ Lỗi load config: %v", err)
	}

	contractABI, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	bytecode := common.FromHex(strings.TrimSpace(bytecodeHex))
	if len(bytecode) == 0 {
		log.Fatalf("❌ Lỗi bytecode: rỗng")
	}

	if len(cfg.PrivateKeys) == 0 {
		log.Fatalf("❌ Không tìm thấy private key trong config")
	}
	pk, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		log.Fatalf("❌ Lỗi parse private key: %v", err)
	}
	fromAddress := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

	switch *modeFlag {
	case "setup":
		rpcURL, err := resolveNodeURL(cfg, *targetNodeFlag, *rpcURLFlag)
		if err != nil {
			log.Fatalf("❌ %v", err)
		}
		client, err := dialClient(rpcURL)
		if err != nil {
			log.Fatalf("❌ Lỗi kết nối RPC %s: %v", rpcURL, err)
		}

		// Kiểm tra nếu ĐÃ CÓ contract cũ và không bắt buộc deploy mới
		if !*forceDeployFlag && *roundFlag == 0 {
			entries, err := loadContractAddresses(*contractFileFlag)
			if err == nil && len(entries) > 0 {
				existingAddr := common.HexToAddress(entries[len(entries)-1].ContractAddress)
				fmt.Println("==================================================")
				fmt.Printf("🔍 [EVM CHECK EXISTING] Đã tìm thấy contract cũ: %s\n", existingAddr.Hex())
				fmt.Printf("   Đang kiểm tra đọc lại dữ liệu trên RPC %s...\n", rpcURL)
				out, err := executeCallWithVerification(client, fromAddress, existingAddr, contractABI, "getSummary", nil, nil)
				if err == nil {
					fmt.Printf("   ✅ [DỮ LIỆU EVM CŨ NGUYÊN VẸN] Summary: %s\n", out)
					fmt.Println("   👉 Tái sử dụng contract cũ. (Dùng --force-deploy hoặc --round=N để deploy mới).")
					fmt.Println("==================================================")
					return
				}
				fmt.Printf("   ⚠️ Contract cũ không phản hồi hoặc state chưa khởi tạo: %v\n", err)
				fmt.Println("   👉 Sẽ tiến hành deploy mới và khởi tạo lại state...")
			}
		}

		roundInfo := ""
		if *roundFlag > 0 {
			roundInfo = fmt.Sprintf(" (Round %d)", *roundFlag)
		}
		fmt.Println("==================================================")
		fmt.Printf("📦 [EVM SETUP] Deploy Contract & Khởi tạo State trên: %s%s\n", rpcURL, roundInfo)
		fmt.Println("==================================================")

		contractAddr, err := deployContract(client, pk, cfg.ChainID, fromAddress, bytecode)
		if err != nil {
			log.Fatalf("❌ Deploy contract EVM thất bại: %v", err)
		}
		fmt.Printf("   ✅ DEPLOY EVM CONTRACT THÀNH CÔNG: %s\n", contractAddr.Hex())

		initKey := fmt.Sprintf("round_%d_key", *roundFlag)
		if *roundFlag == 0 {
			initKey = "evm_state_init_key"
		}
		initVal := fmt.Sprintf("evm_state_val_round_%d_ts_%d", *roundFlag, time.Now().Unix())

		entry := ContractEntry{
			ContractAddress: contractAddr.Hex(),
			DeployedAt:      time.Now().Format(time.RFC3339),
			ChainID:         cfg.ChainID,
			Round:           *roundFlag,
			InitKey:         initKey,
			InitValue:       initVal,
		}

		if err := saveContractAddress(*contractFileFlag, entry); err != nil {
			log.Fatalf("❌ Lỗi lưu địa chỉ hợp đồng: %v", err)
		}
		trackedList, _ := loadContractAddresses(*contractFileFlag)
		fmt.Printf("   💾 Đã lưu vào lịch sử: %s (Đang theo dõi: %d/%d contracts)\n", *contractFileFlag, len(trackedList), maxTrackedContracts)

		// Nạp dữ liệu state ban đầu qua setRoundState
		fmt.Printf("▶️  Chạy setRoundState (round=%d, key=%s, val=%s)...\n", *roundFlag, initKey, initVal)
		expectedEvents := []ExpectedEvent{
			{Name: "StateUpdated", Contains: []string{initKey, initVal}},
		}
		_, err = executeSendWithVerification(client, pk, cfg.ChainID, fromAddress, contractAddr, contractABI, "setRoundState", []interface{}{big.NewInt(int64(*roundFlag)), initKey, initVal}, expectedEvents)
		if err != nil {
			log.Fatalf("❌ setRoundState thất bại: %v", err)
		}
		fmt.Println("   ✅ [EVM State Set] Đã khởi tạo state thành công!")

		// Kiểm tra đọc lại dữ liệu vừa ghi qua eth_call
		fmt.Printf("▶️  Chạy eth_call getState('%s')...\n", initKey)
		out, err := executeCallWithVerification(client, fromAddress, contractAddr, contractABI, "getState", []interface{}{initKey}, []string{initVal})
		if err != nil {
			log.Fatalf("❌ Đọc lại state ban đầu thất bại: %v", err)
		}
		fmt.Printf("   ✅ [EVM ReadBack Verified] Giá trị: %s\n", out)

		summaryOut, err := executeCallWithVerification(client, fromAddress, contractAddr, contractABI, "getSummary", nil, []string{initKey, initVal})
		if err != nil {
			log.Fatalf("❌ Đọc getSummary thất bại: %v", err)
		}
		fmt.Printf("   ✅ [EVM Summary Verified]: %s\n", summaryOut)
		fmt.Println("🎉 [EVM SETUP HOÀN TẤT] State EVM đã được nạp sẵn sàng vào blockchain!")

	case "verify-node":
		rpcURL, err := resolveNodeURL(cfg, *targetNodeFlag, *rpcURLFlag)
		if err != nil {
			log.Fatalf("❌ %v", err)
		}
		var targetContracts []ContractEntry
		if *contractAddrFlag != "" {
			targetContracts = []ContractEntry{{ContractAddress: *contractAddrFlag}}
		} else {
			entries, err := loadContractAddresses(*contractFileFlag)
			if err != nil {
				log.Fatalf("❌ %v", err)
			}
			targetContracts = entries
		}

		client, err := dialClient(rpcURL)
		if err != nil {
			log.Fatalf("❌ Lỗi kết nối RPC %s: %v", rpcURL, err)
		}

		fmt.Println("==================================================")
		fmt.Printf("🔍 [EVM VERIFY NODE] Kiểm tra toàn vẹn State EVM trên Node %s (%s)\n", *targetNodeFlag, rpcURL)
		fmt.Printf("   📋 Tổng số hợp đồng cần xác minh: %d (Tối đa %d, gồm cũ nhất & mới nhất)\n", len(targetContracts), maxTrackedContracts)
		fmt.Println("==================================================")

		for idx, entry := range targetContracts {
			contractAddr := common.HexToAddress(entry.ContractAddress)
			roundLabel := ""
			if entry.Round > 0 {
				roundLabel = fmt.Sprintf(" (Round %d)", entry.Round)
			}
			fmt.Printf("▶️  [%d/%d] Kiểm tra Contract %s%s...\n", idx+1, len(targetContracts), contractAddr.Hex(), roundLabel)

			// 1. Kiểm tra getSummary
			summaryOut, err := executeCallWithVerification(client, fromAddress, contractAddr, contractABI, "getSummary", nil, nil)
			if err != nil {
				log.Fatalf("❌ [LỖI TOÀN VẸN DỮ LIỆU EVM TRÊN NODE %s] Contract %s%s getSummary thất bại: %v", *targetNodeFlag, contractAddr.Hex(), roundLabel, err)
			}
			fmt.Printf("      ✅ Summary: %s\n", summaryOut)

			// 2. Nếu có lưu InitKey/InitValue, xác thực chính xác giá trị
			if entry.InitKey != "" && entry.InitValue != "" {
				valOut, err := executeCallWithVerification(client, fromAddress, contractAddr, contractABI, "getState", []interface{}{entry.InitKey}, []string{entry.InitValue})
				if err != nil {
					log.Fatalf("❌ [LỖI TOÀN VẸN DỮ LIỆU EVM TRÊN NODE %s] Contract %s%s getState('%s') thất bại: %v", *targetNodeFlag, contractAddr.Hex(), roundLabel, entry.InitKey, err)
				}
				fmt.Printf("      ✅ Key '%s' nguyên vẹn: %s\n", entry.InitKey, valOut)
			}
		}
		fmt.Printf("🏆 [EVM PASS] Node %s hoàn toàn nguyên vẹn toàn bộ %d contracts EVM!\n", *targetNodeFlag, len(targetContracts))

	case "write-state", "write-doc":
		rpcURL, err := resolveNodeURL(cfg, *targetNodeFlag, *rpcURLFlag)
		if err != nil {
			log.Fatalf("❌ %v", err)
		}
		var contractAddr common.Address
		var targetEntry ContractEntry
		if *contractAddrFlag != "" {
			contractAddr = common.HexToAddress(*contractAddrFlag)
		} else {
			entries, err := loadContractAddresses(*contractFileFlag)
			if err != nil {
				log.Fatalf("❌ %v", err)
			}
			// Ưu tiên ghi vào contract mới nhất
			targetEntry = entries[len(entries)-1]
			contractAddr = common.HexToAddress(targetEntry.ContractAddress)
		}

		client, err := dialClient(rpcURL)
		if err != nil {
			log.Fatalf("❌ Lỗi kết nối RPC %s: %v", rpcURL, err)
		}

		fmt.Println("==================================================")
		fmt.Printf("✍️  [EVM WRITE TEST] Bơm giao dịch cập nhật State qua: Node %s (%s)\n", *targetNodeFlag, rpcURL)
		fmt.Printf("   📌 Contract: %s (Mới nhất)\n", contractAddr.Hex())
		fmt.Println("==================================================")

		postKey := fmt.Sprintf("node_%s_post_restore", *targetNodeFlag)
		postVal := fmt.Sprintf("restored_node_%s_written_at_%d", *targetNodeFlag, time.Now().Unix())

		expectedEvents := []ExpectedEvent{
			{Name: "StateUpdated", Contains: []string{postKey, postVal}},
		}
		_, err = executeSendWithVerification(client, pk, cfg.ChainID, fromAddress, contractAddr, contractABI, "updateState", []interface{}{postKey, postVal}, expectedEvents)
		if err != nil {
			log.Fatalf("❌ [LỖI GHI EVM QUA NODE %s] Giao dịch thất bại: %v", *targetNodeFlag, err)
		}
		fmt.Printf("✅ [EVM WRITE PASS] Giao dịch cập nhật State qua Node %s đã được xác nhận vào block thành công!\n", *targetNodeFlag)

		// Kiểm tra đọc lại ngay trên node đó
		readOut, err := executeCallWithVerification(client, fromAddress, contractAddr, contractABI, "getState", []interface{}{postKey}, []string{postVal})
		if err != nil {
			log.Fatalf("❌ [LỖI ĐỌC LẠI EVM QUA NODE %s] Đọc lại giá trị vừa ghi thất bại: %v", *targetNodeFlag, err)
		}
		fmt.Printf("   🔍 Đọc lại trực tiếp trên Node %s thành công: %s\n", *targetNodeFlag, readOut)

	case "verify-cluster":
		var targetContracts []ContractEntry
		if *contractAddrFlag != "" {
			targetContracts = []ContractEntry{{ContractAddress: *contractAddrFlag}}
		} else {
			entries, err := loadContractAddresses(*contractFileFlag)
			if err != nil {
				log.Fatalf("❌ %v", err)
			}
			targetContracts = entries
		}

		allNodes := make(map[string]string)
		for k, v := range cfg.RPCNodes {
			allNodes[k] = v
		}
		for k, v := range cfg.SyncNodes {
			allNodes[k] = v
		}

		fmt.Println("==================================================")
		fmt.Printf("🌐 [EVM VERIFY CLUSTER] Đối chiếu %d contracts trên toàn bộ %d nodes\n", len(targetContracts), len(allNodes))
		fmt.Printf("   📋 Danh sách theo dõi: tối đa %d contracts (gồm cũ nhất & mới nhất)\n", maxTrackedContracts)
		fmt.Println("==================================================")

		for idx, entry := range targetContracts {
			contractAddr := common.HexToAddress(entry.ContractAddress)
			roundLabel := ""
			if entry.Round > 0 {
				roundLabel = fmt.Sprintf(" (Round %d)", entry.Round)
			}
			fmt.Printf("▶️  [%d/%d] Đối chiếu Contract %s%s trên toàn cụm...\n", idx+1, len(targetContracts), contractAddr.Hex(), roundLabel)

			refSummary := ""
			refNode := ""

			for name, urlStr := range allNodes {
				c, err := dialClient(urlStr)
				if err != nil {
					log.Fatalf("❌ Không thể kết nối tới Node %s (%s): %v", name, urlStr, err)
				}
				out, err := executeCallWithVerification(c, fromAddress, contractAddr, contractABI, "getSummary", nil, nil)
				if err != nil {
					log.Fatalf("❌ [LỆCH DỮ LIỆU EVM / FORK] Node %s (%s) contract %s getSummary trả về sai: %v", name, urlStr, contractAddr.Hex(), err)
				}

				if refSummary == "" {
					refSummary = out
					refNode = name
				} else if out != refSummary {
					log.Fatalf("🚨 [FORK PHÁT HIỆN] Lệch state EVM contract %s giữa Node %s và Node %s!\n   • %s: %s\n   • %s: %s", contractAddr.Hex(), refNode, name, refNode, refSummary, name, out)
				}

				// Nếu có InitKey, kiểm tra thêm getState
				if entry.InitKey != "" && entry.InitValue != "" {
					valOut, err := executeCallWithVerification(c, fromAddress, contractAddr, contractABI, "getState", []interface{}{entry.InitKey}, []string{entry.InitValue})
					if err != nil {
						log.Fatalf("❌ [LỆCH DỮ LIỆU EVM / FORK] Node %s (%s) contract %s getState('%s') sai: %v", name, urlStr, contractAddr.Hex(), entry.InitKey, err)
					}
					_ = valOut
				}
			}
			fmt.Printf("      ✅ Khớp dữ liệu trên toàn bộ %d nodes! (Summary: %s)\n", len(allNodes), refSummary)
		}
		fmt.Printf("🏆 [100%% ZERO-FORK CONFIRMED] Dữ liệu State EVM của toàn bộ %d contracts đồng nhất hoàn hảo trên %d nodes trong cụm!\n", len(targetContracts), len(allNodes))

	default:
		log.Fatalf("❌ Mode '%s' không hợp lệ. Chỉ hỗ trợ: setup | verify-node | verify-cluster | write-state | write-doc", *modeFlag)
	}
}
