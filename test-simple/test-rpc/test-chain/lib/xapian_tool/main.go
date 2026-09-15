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

//go:embed contract/xapian.json
var abiJSON string

//go:embed contract/bytecode.hex
var bytecodeHex string

const defaultContractFile = "/tmp/xapian_recovery_contract.json"

type ContractEntry struct {
	ContractAddress string `json:"contract_address"`
	DeployedAt      string `json:"deployed_at"`
	ChainID         int64  `json:"chain_id"`
	Round           int    `json:"round,omitempty"`
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

func saveContractAddress(filePath string, addr common.Address, chainID int64, round int) error {
	var store ContractStore
	data, err := os.ReadFile(filePath)
	if err == nil {
		_ = json.Unmarshal(data, &store)
	}

	newEntry := ContractEntry{
		ContractAddress: addr.Hex(),
		DeployedAt:      time.Now().Format(time.RFC3339),
		ChainID:         chainID,
		Round:           round,
	}

	store.ContractAddress = newEntry.ContractAddress
	store.DeployedAt = newEntry.DeployedAt
	store.ChainID = newEntry.ChainID

	// Khi bắt đầu Vòng 1: reset hoàn toàn lịch sử cũ để không bị sót contract từ chain cũ đã reset
	if round <= 1 {
		store.History = []ContractEntry{newEntry}
	} else {
		alreadyExists := false
		for i, entry := range store.History {
			if strings.EqualFold(entry.ContractAddress, newEntry.ContractAddress) {
				store.History[i] = newEntry
				alreadyExists = true
				break
			}
		}
		if !alreadyExists {
			store.History = append(store.History, newEntry)
		}
	}

	// Giới hạn tối đa 100 contract:
	// Giữ lại contract cũ nhất (History[0]) và 99 contract mới nhất
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

// receiptWaitIterations / receiptWaitInterval: bumped from 90*500ms=45s to
// 240*500ms=120s (2026-09-15) -- reproduced live, CI's "Chaos Rolling Restart"
// step failing this exact wait right after `pre_action: restart_chain` resets
// the chain to a fresh genesis: the search tx genuinely succeeded (confirmed
// via eth_getTransactionReceipt after the fact -- status=0x1, real block/logs)
// but took longer than 45s to land because the freshly-bootstrapped DAG/leader
// rotation hasn't settled yet, matching this project's own established pattern
// of loosening a tolerance that was too tight for a legitimate startup/reset
// condition rather than a real bug (see e.g. the ±5→±50 block-parity fix and
// the 90-180s peer_rpc bind-confirmation note in metanode's own memory).
const receiptWaitIterations = 240
const receiptWaitInterval = 500 * time.Millisecond

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
	fmt.Printf("   🚀 Đã gửi Tx Deploy Xapian. Hash: %s\n", signedTx.Hash().Hex())

	receipt, err := waitReceiptAndPoll(client, signedTx.Hash())
	if err != nil {
		return common.Address{}, err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return common.Address{}, fmt.Errorf("tx deploy bị Revert (gasUsed: %d)", receipt.GasUsed)
	}
	return receipt.ContractAddress, nil
}

func executeSendWithVerification(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, contractAddr common.Address, contractABI abi.ABI, method string, args []interface{}, expectedEvents []ExpectedEvent) error {
	var data []byte
	var err error
	if len(args) > 0 {
		data, err = contractABI.Pack(method, args...)
	} else {
		data, err = contractABI.Pack(method)
	}
	if err != nil {
		return fmt.Errorf("lỗi pack method %s: %v", method, err)
	}

	ctx := context.Background()
	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return fmt.Errorf("lỗi lấy nonce: %v", err)
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		gasPrice = big.NewInt(100000)
	}
	gasLimit := uint64(4000000)

	tx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return fmt.Errorf("lỗi ký tx: %v", err)
	}

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		return fmt.Errorf("lỗi gửi transaction %s: %v", method, err)
	}
	fmt.Printf("   🚀 Đã gửi Tx (%s). Hash: %s\n", method, signedTx.Hash().Hex())

	receipt, err := waitReceiptAndPoll(client, signedTx.Hash())
	if err != nil {
		return err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("tx %s bị Revert", method)
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
				return fmt.Errorf("không tìm thấy Event '%s' có chứa: %v trong receipt tx %s", expected.Name, expected.Contains, method)
			}
		}
	}
	return nil
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
		return "", fmt.Errorf("lỗi toàn vẹn dữ liệu: hợp đồng %s trả về kết quả rỗng (0x) khi gọi %s - giao dịch đã có receipt trước khi dừng nhưng node không đọc được state sau khi restart", contractAddr.Hex(), method)
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
	modeFlag := flag.String("mode", "setup", "Chế độ: setup | verify-node | verify-cluster | write-doc")
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

		// 1. Kiểm tra nếu ĐÃ CÓ contract cũ được lưu và không bắt buộc deploy mới
		if !*forceDeployFlag && *roundFlag == 0 {
			entries, err := loadContractAddresses(*contractFileFlag)
			if err == nil && len(entries) > 0 {
				existingAddr := common.HexToAddress(entries[len(entries)-1].ContractAddress)
				fmt.Println("==================================================")
				fmt.Printf("🔍 [XAPIAN CHECK EXISTING] Đã tìm thấy contract cũ: %s\n", existingAddr.Hex())
				fmt.Printf("   Đang kiểm tra đọc lại dữ liệu trên RPC %s...\n", rpcURL)
				out, err := executeCallWithVerification(client, fromAddress, existingAddr, contractABI, "runStep5c_GetData_View", []interface{}{big.NewInt(0)}, []string{"Iphone 13 Pro", "electronics", "apple"})
				if err == nil {
					fmt.Printf("   ✅ [DỮ LIỆU CŨ NGUYÊN VẸN 100%%] Đọc thành công Doc 0: %s\n", out)
					fmt.Println("   👉 Dữ liệu từ lần chạy trước vẫn còn nguyên vẹn trên blockchain và Xapian DB!")
					fmt.Println("   👉 Tái sử dụng contract cũ. (Dùng --force-deploy hoặc --round=N để deploy mới mỗi round).")
					fmt.Println("==================================================")
					return
				}
				fmt.Printf("   ⚠️ Contract cũ không phản hồi hoặc dữ liệu chưa khởi tạo: %v\n", err)
				fmt.Println("   👉 Sẽ tiến hành deploy mới và khởi tạo lại dữ liệu...")
			}
		}

		roundInfo := ""
		if *roundFlag > 0 {
			roundInfo = fmt.Sprintf(" (Round %d)", *roundFlag)
		}
		fmt.Println("==================================================")
		fmt.Printf("📦 [XAPIAN SETUP] Deploy Contract & Khởi tạo DB trên: %s%s\n", rpcURL, roundInfo)
		fmt.Println("==================================================")

		contractAddr, err := deployContract(client, pk, cfg.ChainID, fromAddress, bytecode)
		if err != nil {
			log.Fatalf("❌ Deploy contract Xapian thất bại: %v", err)
		}
		fmt.Printf("   ✅ DEPLOY XAPIAN CONTRACT THÀNH CÔNG: %s\n", contractAddr.Hex())

		if err := saveContractAddress(*contractFileFlag, contractAddr, cfg.ChainID, *roundFlag); err != nil {
			log.Fatalf("❌ Lỗi lưu địa chỉ hợp đồng: %v", err)
		}
		trackedList, _ := loadContractAddresses(*contractFileFlag)
		fmt.Printf("   💾 Đã lưu vào lịch sử: %s (Đang theo dõi: %d/%d contracts)\n", *contractFileFlag, len(trackedList), maxTrackedContracts)

		// 1. Setup DB & 3 docs
		fmt.Println("▶️  Chạy runStep1_Setup (Tạo DB Xapian và 3 documents mẫu)...")
		expectedSetupEvents := []ExpectedEvent{
			{Name: "Setup_DbCreated", Contains: []string{"products_test_v1_version1"}},
			{Name: "Setup_DocCreated", Contains: []string{"Iphone 13 Pro"}},
			{Name: "Setup_DocCreated", Contains: []string{"Samsung Galaxy S22"}},
			{Name: "Setup_DocCreated", Contains: []string{"Macbook Pro 14"}},
		}
		if err := executeSendWithVerification(client, pk, cfg.ChainID, fromAddress, contractAddr, contractABI, "runStep1_Setup", nil, expectedSetupEvents); err != nil {
			log.Fatalf("❌ runStep1_Setup thất bại: %v", err)
		}
		fmt.Println("   ✅ [Xapian Setup] Đã tạo DB và index 3 documents thành công!")

		// 2. ReadBack doc 0
		fmt.Println("▶️  Chạy runStep2_ReadBack (Kiểm tra đọc dữ liệu doc 0)...")
		expectedReadEvents := []ExpectedEvent{
			{Name: "Read_Data", Contains: []string{"Iphone 13 Pro", "electronics"}},
		}
		if err := executeSendWithVerification(client, pk, cfg.ChainID, fromAddress, contractAddr, contractABI, "runStep2_ReadBack", nil, expectedReadEvents); err != nil {
			log.Fatalf("❌ runStep2_ReadBack thất bại: %v", err)
		}
		fmt.Println("   ✅ [Xapian ReadBack] Đã đọc lại doc 0 thành công!")

		// 3. UpdateDoc doc 0 -> "Iphone 13 Pro UPDATED"
		fmt.Println("▶️  Chạy runStep3_UpdateDoc (Cập nhật doc 0 -> 'Iphone 13 Pro UPDATED')...")
		expectedUpdateEvents := []ExpectedEvent{
			{Name: "Update_SetData", Contains: []string{"true"}},
		}
		if err := executeSendWithVerification(client, pk, cfg.ChainID, fromAddress, contractAddr, contractABI, "runStep3_UpdateDoc", nil, expectedUpdateEvents); err != nil {
			log.Fatalf("❌ runStep3_UpdateDoc thất bại: %v", err)
		}
		fmt.Println("   ✅ [Xapian UpdateDoc] Đã cập nhật doc 0 thành công!")

		// 4. QuerySearch "iphone"
		fmt.Println("▶️  Chạy runStep5b_QuerySearch (Tìm kiếm 'iphone' và đối chiếu Event Search_Item)...")
		expectedSearchEvents := []ExpectedEvent{
			{Name: "Search_Item", Contains: []string{"Iphone 13 Pro UPDATED", "electronics"}},
		}
		if err := executeSendWithVerification(client, pk, cfg.ChainID, fromAddress, contractAddr, contractABI, "runStep5b_QuerySearch", []interface{}{"iphone"}, expectedSearchEvents); err != nil {
			log.Fatalf("❌ runStep5b_QuerySearch thất bại: %v", err)
		}
		fmt.Println("   ✅ [Xapian QuerySearch] Tìm kiếm full-text doc 0 thành công!")

		// 5. Verify initial state via eth_call
		out, err := executeCallWithVerification(client, fromAddress, contractAddr, contractABI, "runStep5c_GetData_View", []interface{}{big.NewInt(0)}, []string{"Iphone 13 Pro UPDATED", "electronics", "apple"})
		if err != nil {
			log.Fatalf("❌ runStep5c_GetData_View khởi tạo thất bại: %v", err)
		}
		fmt.Printf("   ✅ [Xapian Verify View] Doc 0 hợp lệ: %s\n", out)
		fmt.Println("🎉 [XAPIAN SETUP HOÀN TẤT] Dữ liệu Xapian đã được nạp sẵn sàng vào blockchain!")

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
		fmt.Printf("🔍 [XAPIAN VERIFY NODE] Kiểm tra toàn vẹn dữ liệu Xapian DB trên Node %s (%s)\n", *targetNodeFlag, rpcURL)
		fmt.Printf("   📋 Tổng số hợp đồng cần xác minh: %d (Tối đa %d, gồm cũ nhất & mới nhất)\n", len(targetContracts), maxTrackedContracts)
		fmt.Println("==================================================")

		for idx, entry := range targetContracts {
			contractAddr := common.HexToAddress(entry.ContractAddress)
			roundLabel := ""
			if entry.Round > 0 {
				roundLabel = fmt.Sprintf(" (Round %d)", entry.Round)
			}
			fmt.Printf("▶️  [%d/%d] Kiểm tra Contract %s%s...\n", idx+1, len(targetContracts), contractAddr.Hex(), roundLabel)

			// 1. eth_call runStep5c_GetData_View(0)
			out, err := executeCallWithVerification(client, fromAddress, contractAddr, contractABI, "runStep5c_GetData_View", []interface{}{big.NewInt(0)}, []string{"Iphone 13 Pro", "electronics", "apple"})
			if err != nil {
				log.Fatalf("❌ [LỖI TOÀN VẸN DỮ LIỆU XAPIAN TRÊN NODE %s] Contract %s%s đọc thất bại: %v", *targetNodeFlag, contractAddr.Hex(), roundLabel, err)
			}
			fmt.Printf("      ✅ Doc 0 hợp lệ: %s\n", out)

			// Với contract mới nhất: chạy thêm full-text search
			if idx == len(targetContracts)-1 {
				fmt.Println("      🔍 Kiểm tra full-text search runStep5b_QuerySearch('iphone')...")
				expectedSearchEvents := []ExpectedEvent{
					{Name: "Search_Item", Contains: []string{"Iphone 13 Pro", "electronics"}},
				}
				if err := executeSendWithVerification(client, pk, cfg.ChainID, fromAddress, contractAddr, contractABI, "runStep5b_QuerySearch", []interface{}{"iphone"}, expectedSearchEvents); err != nil {
					log.Fatalf("❌ [LỖI TÌM KIẾM XAPIAN TRÊN NODE %s] Contract %s%s tìm kiếm thất bại: %v", *targetNodeFlag, contractAddr.Hex(), roundLabel, err)
				}
				fmt.Println("      ✅ Tìm kiếm full-text Xapian chính xác!")
			}
		}
		fmt.Printf("🏆 [XAPIAN PASS] Node %s hoàn toàn nguyên vẹn toàn bộ %d contracts Xapian!\n", *targetNodeFlag, len(targetContracts))

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
		fmt.Printf("🌐 [XAPIAN VERIFY CLUSTER] Đối chiếu %d contracts trên toàn bộ %d nodes\n", len(targetContracts), len(allNodes))
		fmt.Printf("   📋 Danh sách theo dõi: tối đa %d contracts (gồm cũ nhất & mới nhất)\n", maxTrackedContracts)
		fmt.Println("==================================================")

		for idx, entry := range targetContracts {
			contractAddr := common.HexToAddress(entry.ContractAddress)
			roundLabel := ""
			if entry.Round > 0 {
				roundLabel = fmt.Sprintf(" (Round %d)", entry.Round)
			}
			fmt.Printf("▶️  [%d/%d] Đối chiếu Contract %s%s trên toàn cụm...\n", idx+1, len(targetContracts), contractAddr.Hex(), roundLabel)

			refResult := ""
			refNode := ""

			for name, urlStr := range allNodes {
				c, err := dialClient(urlStr)
				if err != nil {
					log.Fatalf("❌ Không thể kết nối tới Node %s (%s): %v", name, urlStr, err)
				}
				out, err := executeCallWithVerification(c, fromAddress, contractAddr, contractABI, "runStep5c_GetData_View", []interface{}{big.NewInt(0)}, []string{"Iphone 13 Pro", "electronics", "apple"})
				if err != nil {
					log.Fatalf("❌ [LỆCH DỮ LIỆU XAPIAN / FORK] Node %s (%s) contract %s trả về sai: %v", name, urlStr, contractAddr.Hex(), err)
				}

				if refResult == "" {
					refResult = out
					refNode = name
				} else if out != refResult {
					log.Fatalf("🚨 [FORK PHÁT HIỆN] Lệch dữ liệu contract %s giữa Node %s và Node %s!\n   • %s: %s\n   • %s: %s", contractAddr.Hex(), refNode, name, refNode, refResult, name, out)
				}
			}
			fmt.Printf("      ✅ Khớp dữ liệu trên toàn bộ %d nodes!\n", len(allNodes))
		}
		fmt.Printf("🏆 [100%% ZERO-FORK CONFIRMED] Dữ liệu Xapian của toàn bộ %d contracts đồng nhất hoàn hảo trên %d nodes trong cụm!\n", len(targetContracts), len(allNodes))

	case "write-doc":
		rpcURL, err := resolveNodeURL(cfg, *targetNodeFlag, *rpcURLFlag)
		if err != nil {
			log.Fatalf("❌ %v", err)
		}
		var contractAddr common.Address
		if *contractAddrFlag != "" {
			contractAddr = common.HexToAddress(*contractAddrFlag)
		} else {
			entries, err := loadContractAddresses(*contractFileFlag)
			if err != nil {
				log.Fatalf("❌ %v", err)
			}
			// Ưu tiên ghi vào contract mới nhất
			contractAddr = common.HexToAddress(entries[len(entries)-1].ContractAddress)
		}

		client, err := dialClient(rpcURL)
		if err != nil {
			log.Fatalf("❌ Lỗi kết nối RPC %s: %v", rpcURL, err)
		}

		fmt.Println("==================================================")
		fmt.Printf("✍️  [XAPIAN WRITE TEST] Bơm giao dịch ghi Xapian qua: Node %s (%s)\n", *targetNodeFlag, rpcURL)
		fmt.Printf("   📌 Contract: %s (Mới nhất)\n", contractAddr.Hex())
		fmt.Println("==================================================")

		expectedUpdateEvents := []ExpectedEvent{
			{Name: "Update_SetData", Contains: []string{"true"}},
		}
		if err := executeSendWithVerification(client, pk, cfg.ChainID, fromAddress, contractAddr, contractABI, "runStep3_UpdateDoc", nil, expectedUpdateEvents); err != nil {
			log.Fatalf("❌ [LỖI GHI XAPIAN QUA NODE %s] Giao dịch thất bại: %v", *targetNodeFlag, err)
		}
		fmt.Printf("✅ [XAPIAN WRITE PASS] Giao dịch ghi Xapian qua Node %s đã được xác nhận vào block thành công!\n", *targetNodeFlag)

	default:
		log.Fatalf("❌ Mode '%s' không hợp lệ. Chỉ hỗ trợ: setup | verify-node | verify-cluster | write-doc", *modeFlag)
	}
}

