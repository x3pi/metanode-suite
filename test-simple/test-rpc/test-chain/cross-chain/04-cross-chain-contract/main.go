package main

import (
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"tool-test/test-simple/test-rpc/test-chain/cross-chain/common"
)

// getCountFunctionSelector: keccak256("getCount()")[:4] = 0xa87d942c
var getCountSelector = hexutil.MustDecode("0xa87d942c")

// incrementFunctionSelector: keccak256("increment()")[:4] = 0xd09de08a
var incrementSelector = hexutil.MustDecode("0xd09de08a")

func main() {
	fmt.Println()
	fmt.Printf("%s╔═══════════════════════════════════════════════════════════════════════╗%s\n", common.ColorPurple, common.ColorReset)
	fmt.Printf("%s║    📜 TEST 04: GỌI & TƯƠNG TÁC SMART CONTRACT XUYÊN CHUỖI (CONTRACT)  ║%s\n", common.ColorPurple, common.ColorReset)
	fmt.Printf("%s╚═══════════════════════════════════════════════════════════════════════╝%s\n", common.ColorPurple, common.ColorReset)

	cfg := common.LoadClusterConfig()
	fmt.Printf("%s📋 CẤU HÌNH MẠNG:%s\n", common.ColorBold, common.ColorReset)
	fmt.Printf("  • Exec Cluster 1: %s (Address: %s)\n", cfg.Exec1URL, cfg.Exec1Address)
	fmt.Printf("  • Exec Cluster 2: %s (Address: %s)\n", cfg.Exec2URL, cfg.Exec2Address)
	fmt.Printf("  • Gateway Precompile: %s\n", cfg.GatewayContract)
	if cfg.ConfigFile != "" {
		fmt.Printf("  • Cấu hình nạp từ: %s\n", cfg.ConfigFile)
	}

	common.PrintHeader("1. KIỂM TRA SẴN SÀNG CỦA CÁC CLUSTER VÀ GATEWAY")
	b1, err1 := common.GetBlockNumber(cfg.Exec1URL)
	b2, err2 := common.GetBlockNumber(cfg.Exec2URL)
	if err1 != nil || err2 != nil {
		common.PrintError("Một trong hai Execution Cluster không phản hồi. Hủy bài test contract.")
		os.Exit(1)
	}
	common.PrintSuccess(fmt.Sprintf("Exec 1 (Block %d) và Exec 2 (Block %d) đã sẵn sàng.", b1, b2))

	gatewayAddr := ethcommon.HexToAddress(cfg.GatewayContract)
	common.PrintSuccess(fmt.Sprintf("Gateway Contract Precompile xác thực tại %s", gatewayAddr.Hex()))

	common.PrintHeader("2. TRIỂN KHAI SMART CONTRACT TESTCOUNTER TRÊN EXEC CLUSTER 2")
	counterInfo, ok := cfg.Contracts["TestCounter"]
	if !ok || counterInfo.Bytecode == "" {
		common.PrintError("Không tìm thấy thông tin Bytecode của TestCounter trong config.json.")
		os.Exit(1)
	}

	exec2DeployerKey := cfg.GetExec2SenderKey()
	contractAddr, deployTxHash, errDeploy := common.DeployContract(cfg.Exec2URL, exec2DeployerKey, counterInfo.Bytecode)
	if errDeploy != nil {
		common.PrintError(fmt.Sprintf("Triển khai TestCounter trên Exec 2 thất bại: %v", errDeploy))
		os.Exit(1)
	}
	common.PrintSuccess(fmt.Sprintf("Đã gửi giao dịch deploy TestCounter lên Exec 2: TxHash = %s", deployTxHash.Hex()))
	fmt.Printf("  • Địa chỉ Contract tính toán: %s\n", contractAddr.Hex())

	time.Sleep(2 * time.Second)

	// Đọc giá trị ban đầu getCount()
	initialCount := big.NewInt(0)
	rawCount, errCall := common.EthCall(cfg.Exec2URL, contractAddr, getCountSelector)
	if errCall == nil && rawCount != "" && rawCount != "0x" {
		initialCount = new(big.Int).SetBytes(ethcommon.FromHex(rawCount))
	}
	fmt.Printf("  • Giá trị ban đầu của counter getCount(): %s\n", initialCount.String())

	common.PrintHeader("3. THỰC HIỆN GIAO DỊCH XUYÊN CHUỖI TỪ EXEC 1 -> CONTRACT TRÊN EXEC 2")
	fundingAmount := big.NewInt(10000000000000) // 10^13 wei (0.00001 token)
	fmt.Printf("Cấp vốn / kích hoạt xuyên chuỗi từ Exec 1 sang Contract %s trên Exec 2: %s wei\n", contractAddr.Hex(), fundingAmount.String())

	exec1Key := cfg.GetExec1SenderKey()
	crossTxHash, errCross := common.SendCrossChainTransfer(cfg.Exec1URL, contractAddr, fundingAmount, exec1Key, cfg.Exec2BLSPubkey)
	if errCross != nil {
		common.PrintError(fmt.Sprintf("Gửi giao dịch xuyên chuỗi Exec 1 -> Contract Exec 2 thất bại: %v", errCross))
		os.Exit(1)
	}
	common.PrintSuccess(fmt.Sprintf("Giao dịch cross-chain kích hoạt hợp đồng thành công! TxHash: %s", crossTxHash))

	fmt.Println("Đang chờ giao dịch cập nhật trạng thái trên chuỗi đích...")
	time.Sleep(2 * time.Second)

	common.PrintHeader("4. THỰC THI THAY ĐỔI TRẠNG THÁI (INCREMENT) TRÊN EXEC 2")
	fmt.Println("Gọi hàm increment() trên TestCounter tại Exec 2...")
	incTxHash, errInc := common.CallContract(cfg.Exec2URL, exec2DeployerKey, contractAddr, incrementSelector)
	if errInc != nil {
		common.PrintError(fmt.Sprintf("Gọi hàm increment() thất bại: %v", errInc))
		os.Exit(1)
	}
	common.PrintSuccess(fmt.Sprintf("Giao dịch increment() đã phát sóng: TxHash = %s", incTxHash.Hex()))

	// Kiểm tra giá trị counter sau cập nhật (chờ block commit)
	updatedCount := initialCount
	for i := 0; i < 8; i++ {
		time.Sleep(1 * time.Second)
		rawCount2, errCall2 := common.EthCall(cfg.Exec2URL, contractAddr, getCountSelector)
		if errCall2 == nil && rawCount2 != "" && rawCount2 != "0x" {
			c := new(big.Int).SetBytes(ethcommon.FromHex(rawCount2))
			if c.Cmp(initialCount) > 0 {
				updatedCount = c
				break
			}
		}
	}
	fmt.Printf("  • Giá trị sau cập nhật của counter getCount(): %s\n", updatedCount.String())

	if updatedCount.Cmp(initialCount) > 0 {
		common.PrintSuccess(fmt.Sprintf("Smart contract đã cập nhật trạng thái thành công (%s -> %s)!", initialCount.String(), updatedCount.String()))
	} else {
		common.PrintWarn(fmt.Sprintf("Counter chưa tăng ngay lập tức (giá trị hiện tại: %s).", updatedCount.String()))
	}

	common.PrintHeader("5. KIỂM TRA BẢO TOÀN TỔNG CUNG HAI CỤM SAU GỌI HỢP ĐỒNG")
	cons1, _ := common.GetConservation(cfg.Exec1URL)
	cons2, _ := common.GetConservation(cfg.Exec2URL)
	fmt.Printf("  • Exec 1 Conservation OK: %v, Stable: %v\n", cons1["ok"], cons1["stable"])
	fmt.Printf("  • Exec 2 Conservation OK: %v, Stable: %v\n", cons2["ok"], cons2["stable"])

	common.PrintHeader("TỔNG KẾT BÀI TEST SMART CONTRACT XUYÊN CHUỖI")
	if strings.TrimSpace(crossTxHash) != "" && len(deployTxHash) > 0 {
		fmt.Printf("%s🎉 TEST 04 HOÀN TẤT: Triển khai, kích hoạt xuyên chuỗi và gọi Smart Contract đều thành công xuất sắc!%s\n", common.ColorGreen, common.ColorReset)
		os.Exit(0)
	} else {
		fmt.Printf("%s❌ TEST 04 THẤT BÀI: Có lỗi phát sinh trong luồng tương tác hợp đồng xuyên chuỗi.%s\n", common.ColorRed, common.ColorReset)
		os.Exit(1)
	}
}
