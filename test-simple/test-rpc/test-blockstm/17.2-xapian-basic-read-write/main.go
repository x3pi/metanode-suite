/*
 * BÀI TEST: 17.2-xapian-basic-read-write
 * MÔ TẢ   : Kiểm thử chu trình cơ bản Xapian DB (Setup -> ReadBack -> Update -> QuerySearch -> View):
 *           1. Deploy Contract TestDbXapianV2.
 *           2. runStep1_Setup: Tạo DB và khởi tạo 3 document mẫu (Iphone, Samsung, Macbook).
 *           3. runStep2_ReadBack: Đọc ngược lại dữ liệu doc 0 từ Xapian (Event: Read_Data).
 *           4. runStep3_UpdateDoc: Cập nhật document 0 (Iphone 13 Pro -> Iphone 13 Pro UPDATED).
 *           5. runStep5b_QuerySearch: Tìm kiếm từ khóa "iphone" qua Xapian (Event: Search_Item).
 *           6. runStep5c_GetData_View: Gọi eth_call đọc struct ProductData hoàn chỉnh từ Xapian.
 * KỲ VỌNG : Tự động parse và verify toàn bộ Event logs & Receipt data. Báo lỗi chi tiết nếu sai lệch.
 */
package main

import (
	"context"
	"crypto/ecdsa"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"tool-test/test-simple/test-rpc/test-blockstm/config"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

//go:embed contract/xapian.json
var abiJSON string

//go:embed contract/bytecode.hex
var bytecodeHex string

type ExpectedEvent struct {
	Name     string
	Contains []string
}

func main() {
	configFlag := flag.String("config", "../config.json", "Đường dẫn file cấu hình config.json")
	chainFlag := flag.String("chain", "", "Tùy chọn chain mục tiêu (ví dụ: 101, 102, chain_a)")
	flag.Parse()

	if *chainFlag != "" {
		os.Setenv("TARGET_CHAIN", *chainFlag)
	}

	configPath := *configFlag
	if flag.NArg() > 0 && !strings.HasPrefix(flag.Arg(0), "-") {
		configPath = flag.Arg(0)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi load config: %v", err)
	}

	client, err := ethclient.Dial(cfg.RPCUrl)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC: %v", err)
	}

	contractABI, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	bytecode := common.FromHex(strings.TrimSpace(bytecodeHex))
	if len(bytecode) == 0 {
		log.Fatalf("❌ Lỗi load bytecode: bytecode rỗng")
	}

	if len(cfg.PrivateKeys) == 0 {
		log.Fatalf("❌ Không có private key trong config")
	}

	pk, err := crypto.HexToECDSA(cfg.PrivateKeys[0])
	if err != nil {
		log.Fatalf("❌ Lỗi parse private key: %v", err)
	}
	fromAddress := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

	fmt.Println("==================================================")
	fmt.Println("🚀 BÀI TEST: 17.2-xapian-basic-read-write")
	fmt.Printf("   - RPC URL: %s (ChainID: %d)\n", cfg.RPCUrl, cfg.ChainID)
	fmt.Printf("   - Submitter: %s\n", fromAddress.Hex())
	fmt.Println("==================================================")

	// ==========================================
	// TASK 1: DEPLOY CONTRACT
	// ==========================================
	fmt.Println("\n--- THỰC THI TASK 1: DEPLOY CONTRACT ---")
	fmt.Println("   📝 Đang sử dụng raw InputData Hex...")
	fmt.Println("▶️  Chạy eth_sendRawTransaction (DEPLOY CONTRACT)...")

	contractAddr, err := deployAndLog(client, pk, cfg.ChainID, fromAddress, bytecode)
	if err != nil {
		log.Fatalf("\n❌ Dừng pipeline tại Task 1 do lỗi: %v", err)
	}

	// ==========================================
	// TASK 2: RUN STEP 1 - SETUP
	// ==========================================
	fmt.Println("\n--- THỰC THI TASK 2: runStep1_Setup ---")
	expectedSetupEvents := []ExpectedEvent{
		{
			Name:     "Setup_DbCreated",
			Contains: []string{"products_test_v1_version1"},
		},
		{
			Name:     "Setup_DocCreated",
			Contains: []string{"Iphone 13 Pro"},
		},
		{
			Name:     "Setup_DocCreated",
			Contains: []string{"Samsung Galaxy S22"},
		},
		{
			Name:     "Setup_DocCreated",
			Contains: []string{"Macbook Pro 14"},
		},
	}

	err = executeSendWithVerification(client, pk, cfg.ChainID, fromAddress, contractAddr, contractABI, "runStep1_Setup", nil, expectedSetupEvents)
	if err != nil {
		log.Fatalf("\n❌ Dừng pipeline tại Task 2 do lỗi: %v", err)
	}

	// ==========================================
	// TASK 3: RUN STEP 2 - READ BACK
	// ==========================================
	fmt.Println("\n--- THỰC THI TASK 3: runStep2_ReadBack ---")
	expectedReadBackEvents := []ExpectedEvent{
		{
			Name:     "Read_Data",
			Contains: []string{"Iphone 13 Pro", "electronics"},
		},
	}

	err = executeSendWithVerification(client, pk, cfg.ChainID, fromAddress, contractAddr, contractABI, "runStep2_ReadBack", nil, expectedReadBackEvents)
	if err != nil {
		log.Fatalf("\n❌ Dừng pipeline tại Task 3 do lỗi: %v", err)
	}

	// ==========================================
	// TASK 4: RUN STEP 3 - UPDATE DOC
	// ==========================================
	fmt.Println("\n--- THỰC THI TASK 4: runStep3_UpdateDoc ---")
	expectedUpdateEvents := []ExpectedEvent{
		{
			Name:     "Update_SetData",
			Contains: []string{"true"},
		},
	}

	err = executeSendWithVerification(client, pk, cfg.ChainID, fromAddress, contractAddr, contractABI, "runStep3_UpdateDoc", nil, expectedUpdateEvents)
	if err != nil {
		log.Fatalf("\n❌ Dừng pipeline tại Task 4 do lỗi: %v", err)
	}

	// ==========================================
	// TASK 5: RUN STEP 5B - QUERY SEARCH
	// ==========================================
	fmt.Println("\n--- THỰC THI TASK 5: runStep5b_QuerySearch ---")
	expectedSearchEvents := []ExpectedEvent{
		{
			Name:     "Search_Item",
			Contains: []string{"Iphone 13 Pro UPDATED", "electronics"},
		},
	}

	err = executeSendWithVerification(client, pk, cfg.ChainID, fromAddress, contractAddr, contractABI, "runStep5b_QuerySearch", []interface{}{"iphone"}, expectedSearchEvents)
	if err != nil {
		log.Fatalf("\n❌ Dừng pipeline tại Task 5 do lỗi: %v", err)
	}

	// ==========================================
	// TASK 6: RUN STEP 5C - GET DATA VIEW
	// ==========================================
	fmt.Println("\n--- THỰC THI TASK 6: runStep5c_GetData_View ---")
	err = executeCallWithVerification(client, fromAddress, contractAddr, contractABI, "runStep5c_GetData_View", []interface{}{big.NewInt(0)}, []string{"Iphone 13 Pro", "electronics", "apple"})
	if err != nil {
		log.Fatalf("\n❌ Dừng pipeline tại Task 6 do lỗi: %v", err)
	}

	fmt.Println("\n==================================================")
	fmt.Println("📊 BẢNG TỔNG HỢP KẾT QUẢ THỰC THI:")
	fmt.Println("==================================================")
	fmt.Println("  ✅ Task 1: DEPLOY -> THÀNH CÔNG")
	fmt.Println("  ✅ Task 2: runStep1_Setup -> THÀNH CÔNG (Pass verify 4 events: Setup_DbCreated & 3 Setup_DocCreated)")
	fmt.Println("  ✅ Task 3: runStep2_ReadBack -> THÀNH CÔNG (Pass 1 verify event 'Read_Data')")
	fmt.Println("  ✅ Task 4: runStep3_UpdateDoc -> THÀNH CÔNG (Pass 1 verify event 'Update_SetData')")
	fmt.Println("  ✅ Task 5: runStep5b_QuerySearch -> THÀNH CÔNG (Pass 1 verify event 'Search_Item')")
	fmt.Println("  ✅ Task 6: runStep5c_GetData_View -> THÀNH CÔNG (Pass verify output ProductData)")
	fmt.Println("==================================================")
	fmt.Println("🎉 HOÀN TẤT THỰC THI! TẤT CẢ CÁC BƯỚC ĐỀU HỢP LỆ VÀ ĐẠT KỲ VỌNG!")
	fmt.Println("==================================================")
}

func deployAndLog(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (common.Address, error) {
	ctx := context.Background()
	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return common.Address{}, fmt.Errorf("Lỗi lấy nonce: %v", err)
	}

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		gasPrice = big.NewInt(100000)
	}

	gasLimit := uint64(3000000)

	fmt.Printf("   📝 CHI TIẾT TX DEPLOY:\n")
	fmt.Printf("      - From: %s\n", from.Hex())
	fmt.Printf("      - Nonce: %d\n", nonce)
	fmt.Printf("      - Gas Price: %s wei\n", gasPrice.String())
	fmt.Printf("      - Gas Limit: %d\n", gasLimit)
	fmt.Printf("      - Type: Contract Creation (To: 0x0)\n")
	fmt.Printf("      - Bytecode Length: %d bytes\n", len(bytecode))

	tx := types.NewContractCreation(nonce, big.NewInt(0), gasLimit, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return common.Address{}, fmt.Errorf("Lỗi ký tx deploy: %v", err)
	}

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		return common.Address{}, fmt.Errorf("Lỗi gửi transaction Deploy: %v", err)
	}

	fmt.Printf("   🚀 Đã gửi Tx Deploy. Hash: %s\n", signedTx.Hash().Hex())
	fmt.Printf("   ⏳ Đang đợi mạng Mining (Polling Receipt) ")

	receipt, err := waitReceiptAndPoll(client, signedTx.Hash())
	if err != nil {
		return common.Address{}, err
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return common.Address{}, fmt.Errorf("Tx Deploy bị Revert")
	}

	fmt.Printf("   ✅ DEPLOY THÀNH CÔNG! (Gas used: %d)\n", receipt.GasUsed)
	fmt.Printf("   📌 CONTRACT ADDRESS MỚI TẠO: %s\n", receipt.ContractAddress.Hex())
	return receipt.ContractAddress, nil
}

func executeSendWithVerification(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, contractAddr common.Address, contractABI abi.ABI, method string, args []interface{}, expectedEvents []ExpectedEvent) error {
	fmt.Printf("▶️  Chạy eth_sendRawTransaction (WRITE/SEND) cho hàm %s...\n", method)

	var data []byte
	var err error
	if len(args) > 0 {
		data, err = contractABI.Pack(method, args...)
	} else {
		data, err = contractABI.Pack(method)
	}
	if err != nil {
		return fmt.Errorf("Lỗi pack method %s: %v", method, err)
	}

	ctx := context.Background()
	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return fmt.Errorf("Lỗi lấy nonce: %v", err)
	}

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		gasPrice = big.NewInt(100000)
	}

	gasLimit := uint64(3000000)

	tx := types.NewTransaction(nonce, contractAddr, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return fmt.Errorf("Lỗi ký tx: %v", err)
	}

	fmt.Printf("   📝 CHI TIẾT TX GỬI ĐI (%s):\n", method)
	fmt.Printf("      - From: %s\n", from.Hex())
	fmt.Printf("      - Hash: %s\n", signedTx.Hash().Hex())
	fmt.Printf("      - Nonce: %d\n", nonce)
	fmt.Printf("      - Gas Price: %s wei\n", gasPrice.String())
	fmt.Printf("      - Gas Limit: %d\n", gasLimit)
	fmt.Printf("      - To Contract: %s\n", contractAddr.Hex())
	fmt.Printf("      - Data Length: %d bytes\n", len(data))

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		return fmt.Errorf("Lỗi gửi transaction %s: %v", method, err)
	}

	fmt.Printf("   🚀 Đã gửi Tx. Hash: %s\n", signedTx.Hash().Hex())
	fmt.Printf("   ⏳ Đang đợi mạng Mining (Polling Receipt) ")

	receipt, err := waitReceiptAndPoll(client, signedTx.Hash())
	if err != nil {
		return err
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("Tx %s bị Revert", method)
	}

	fmt.Printf("   ✅ Tx THÀNH CÔNG (Gas used: %d)\n", receipt.GasUsed)

	if len(receipt.Logs) > 0 {
		fmt.Printf("   📝 SỰ KIỆN (EVENTS):\n")
		verifiedEvents := make(map[int]bool)

		for i, vLog := range receipt.Logs {
			if len(vLog.Topics) == 0 {
				continue
			}
			event, err := contractABI.EventByID(vLog.Topics[0])
			if err != nil {
				fmt.Printf("      - Log [%d]: Topic0=%s (Không tìm thấy trong ABI)\n", i, vLog.Topics[0].Hex())
				continue
			}

			logStrBuilder := strings.Builder{}
			fmt.Printf("      - Log [%d] Event: %s\n", i, event.Name)
			for j, topic := range vLog.Topics {
				fmt.Printf("         + Topic[%d]: %s\n", j, topic.Hex())
				logStrBuilder.WriteString(topic.Hex() + " ")
			}

			if len(vLog.Data) > 0 {
				unpacked, err := event.Inputs.NonIndexed().Unpack(vLog.Data)
				if err == nil {
					fmt.Printf("         + Data: ")
					dataStrBuilder := strings.Builder{}
					for k, unp := range unpacked {
						strVal := fmt.Sprintf("%v", unp)
						fmt.Print(strVal)
						dataStrBuilder.WriteString(strVal)
						if k < len(unpacked)-1 {
							fmt.Print(", ")
							dataStrBuilder.WriteString(", ")
						}
					}
					fmt.Println()
					logStrBuilder.WriteString(dataStrBuilder.String())
				} else {
					fmt.Printf("         + Lỗi đọc Data: %v\n", err)
				}
			}

			logFullStr := string(vLog.Data) + logStrBuilder.String()

			for eIdx, expected := range expectedEvents {
				if expected.Name == event.Name {
					allMatch := true
					for _, text := range expected.Contains {
						if !strings.Contains(logFullStr, text) {
							allMatch = false
							break
						}
					}
					if allMatch {
						verifiedEvents[eIdx] = true
						fmt.Printf("         ✅ [Verified] Khớp điều kiện cho event '%s'\n", expected.Name)
					}
				}
			}
		}

		for eIdx, expected := range expectedEvents {
			if !verifiedEvents[eIdx] {
				return fmt.Errorf("Không tìm thấy Event '%s' có chứa đúng các thông số: %v", expected.Name, expected.Contains)
			}
		}
	}

	return nil
}

func executeCallWithVerification(client *ethclient.Client, from common.Address, contractAddr common.Address, contractABI abi.ABI, method string, args []interface{}, expectedOutput []string) error {
	fmt.Printf("▶️  Chạy thử eth_call (READ/SIMULATE) cho hàm %s...\n", method)

	data, err := contractABI.Pack(method, args...)
	if err != nil {
		return fmt.Errorf("Lỗi pack method %s: %v", method, err)
	}

	startTime := time.Now()
	res, err := client.CallContract(context.Background(), ethereum.CallMsg{
		From: from,
		To:   &contractAddr,
		Data: data,
	}, nil)
	latency := time.Since(startTime)
	fmt.Printf("   ⏱️  Thời gian phản hồi RPC: %v\n", latency)

	if err != nil {
		return fmt.Errorf("Lỗi eth_call %s: %v", method, err)
	}

	outputs, err := contractABI.Unpack(method, res)
	if err != nil {
		return fmt.Errorf("Lỗi unpack output: %v", err)
	}

	fmt.Printf("   ✅ KẾT QUẢ ĐỌC: %+v\n", outputs)

	outputStr := fmt.Sprintf("%+v", outputs)
	for _, expected := range expectedOutput {
		if !strings.Contains(outputStr, expected) {
			return fmt.Errorf("Kết quả trả về không chứa '%s'. Result: %s", expected, outputStr)
		}
	}
	fmt.Println("   ✅ [Verified] Khớp điều kiện kết quả đầu ra")
	return nil
}

func waitReceiptAndPoll(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	ctx := context.Background()
	for i := 0; i < 60; i++ {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			fmt.Println()
			return receipt, nil
		}
		fmt.Print(".")
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Println()
	return nil, fmt.Errorf("timeout chờ receipt cho tx %s", txHash.Hex())
}
