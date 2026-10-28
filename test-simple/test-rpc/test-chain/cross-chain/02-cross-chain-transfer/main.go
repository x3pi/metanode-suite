package main

import (
	"fmt"
	"math/big"
	"os"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"tool-test/test-simple/test-rpc/test-chain/cross-chain/common"
)

func main() {
	fmt.Println()
	fmt.Printf("%s╔═══════════════════════════════════════════════════════════════════════╗%s\n", common.ColorPurple, common.ColorReset)
	fmt.Printf("%s║       💸 TEST 02: GIAO DỊCH CHUYỂN TIỀN XUYÊN CHUỖI (CROSS-CHAIN)      ║%s\n", common.ColorPurple, common.ColorReset)
	fmt.Printf("%s╚═══════════════════════════════════════════════════════════════════════╝%s\n", common.ColorPurple, common.ColorReset)

	cfg := common.LoadClusterConfig()
	fmt.Printf("%s📋 CẤU HÌNH EXECUTION CLUSTERS:%s\n", common.ColorBold, common.ColorReset)
	fmt.Printf("  • Execution Chain 1 (Source/Dest): %s (Address: %s)\n", cfg.Exec1URL, cfg.Exec1Address)
	fmt.Printf("  • Execution Chain 2 (Source/Dest): %s (Address: %s)\n", cfg.Exec2URL, cfg.Exec2Address)
	if cfg.ConfigFile != "" {
		fmt.Printf("  • Cấu hình nạp từ: %s\n", cfg.ConfigFile)
	}

	common.PrintHeader("1. KIỂM TRA KẾT NỐI ĐẾN CÁC EXECUTION CLUSTERS")
	b1, err1 := common.GetBlockNumber(cfg.Exec1URL)
	if err1 != nil {
		common.PrintError(fmt.Sprintf("Exec Cluster 1 (%s) không phản hồi: %v", cfg.Exec1URL, err1))
	} else {
		common.PrintSuccess(fmt.Sprintf("Exec Cluster 1 (%s) Online - Block Height=%d", cfg.Exec1URL, b1))
	}

	b2, err2 := common.GetBlockNumber(cfg.Exec2URL)
	if err2 != nil {
		common.PrintError(fmt.Sprintf("Exec Cluster 2 (%s) không phản hồi: %v", cfg.Exec2URL, err2))
	} else {
		common.PrintSuccess(fmt.Sprintf("Exec Cluster 2 (%s) Online - Block Height=%d", cfg.Exec2URL, b2))
	}

	if err1 != nil || err2 != nil {
		common.PrintWarn("Một hoặc nhiều Execution Cluster chưa sẵn sàng. Vui lòng kiểm tra cụm trước khi gửi giao dịch.")
		os.Exit(1)
	}

	common.PrintHeader("2. THỰC HIỆN GIAO DỊCH: EXEC 1 -> EXEC 2")
	targetAddr := ethcommon.HexToAddress(cfg.Exec2Address)
	if targetAddr == (ethcommon.Address{}) {
		targetAddr = common.RandomAddress()
	}
	amount := big.NewInt(1000000000000000) // 0.001 token (10^15 wei)
	fmt.Printf("Địa chỉ người nhận mục tiêu trên Exec 2: %s\n", targetAddr.Hex())
	fmt.Printf("Số lượng chuyển: %s wei\n", amount.String())

	initBal, _ := common.GetAccountBalance(cfg.Exec2URL, targetAddr)
	if initBal == nil {
		initBal = big.NewInt(0)
	}
	fmt.Printf("Số dư ban đầu của ví nhận trên Exec 2: %s\n", initBal.String())

	senderKey1 := cfg.GetExec1SenderKey()
	txHash1, err := common.SendCrossChainTransfer(cfg.Exec1URL, targetAddr, amount, senderKey1)
	credited := false
	if err != nil {
		common.PrintError(fmt.Sprintf("Gửi giao dịch xuyên chuỗi Exec 1 -> Exec 2 thất bại: %v", err))
	} else {
		common.PrintSuccess(fmt.Sprintf("Giao dịch đã phát sóng thành công! TxHash: %s", txHash1))
		fmt.Println("Đang chờ xác nhận giao dịch trên chuỗi đích...")

		maxWaitSec := 60
		var finalBal *big.Int
		for i := 0; i < maxWaitSec; i++ {
			time.Sleep(1 * time.Second)
			newBal, errBal := common.GetAccountBalance(cfg.Exec2URL, targetAddr)
			if errBal == nil && newBal != nil {
				finalBal = newBal
				if newBal.Cmp(initBal) > 0 {
					diff := new(big.Int).Sub(newBal, initBal)
					common.PrintSuccess(fmt.Sprintf("Số dư trên chuỗi đích Exec 2 đã tăng thành công sau %ds! (+%s wei, mới: %s)", i+1, diff.String(), newBal.String()))
					credited = true
					break
				}
			}
			if (i+1)%10 == 0 {
				fmt.Printf("   ... đang chờ chứng thực & ghi có trên Exec 2 (%ds/%ds)...\n", i+1, maxWaitSec)
			}
		}
		if !credited {
			lastStr := "chưa đọc được"
			if finalBal != nil {
				lastStr = finalBal.String()
			}
			common.PrintError(fmt.Sprintf("❌ LỖI XÁC MINH: Chuỗi đích Exec 2 KHÔNG nhận được số dư sau %ds! (Ban đầu: %s wei, Sau %ds: %s wei)", maxWaitSec, initBal.String(), maxWaitSec, lastStr))
		}
	}

	common.PrintHeader("3. THỰC HIỆN GIAO DỊCH NGƯỢC LẠI: EXEC 2 -> EXEC 1")
	targetAddr2 := ethcommon.HexToAddress(cfg.Exec1Address)
	if targetAddr2 == (ethcommon.Address{}) {
		targetAddr2 = common.RandomAddress()
	}
	amount2 := big.NewInt(1000000000000000) // 0.001 token (10^15 wei)
	fmt.Printf("Địa chỉ người nhận mục tiêu trên Exec 1: %s\n", targetAddr2.Hex())
	fmt.Printf("Số lượng chuyển: %s wei\n", amount2.String())

	initBal1, _ := common.GetAccountBalance(cfg.Exec1URL, targetAddr2)
	if initBal1 == nil {
		initBal1 = big.NewInt(0)
	}
	fmt.Printf("Số dư ban đầu của ví nhận trên Exec 1: %s\n", initBal1.String())

	senderKey2 := cfg.GetExec2SenderKey()
	txHash2, errRev := common.SendCrossChainTransfer(cfg.Exec2URL, targetAddr2, amount2, senderKey2)
	credited2 := false
	if errRev != nil {
		common.PrintError(fmt.Sprintf("Gửi giao dịch xuyên chuỗi Exec 2 -> Exec 1 thất bại: %v", errRev))
	} else {
		common.PrintSuccess(fmt.Sprintf("Giao dịch chiều ngược phát sóng thành công! TxHash: %s", txHash2))
		fmt.Println("Đang chờ xác nhận giao dịch trên chuỗi đích...")

		maxWaitSec := 60
		var finalBal1 *big.Int
		for i := 0; i < maxWaitSec; i++ {
			time.Sleep(1 * time.Second)
			newBal1, errBal1 := common.GetAccountBalance(cfg.Exec1URL, targetAddr2)
			if errBal1 == nil && newBal1 != nil {
				finalBal1 = newBal1
				if newBal1.Cmp(initBal1) > 0 {
					diff := new(big.Int).Sub(newBal1, initBal1)
					common.PrintSuccess(fmt.Sprintf("Số dư trên chuỗi đích Exec 1 đã tăng thành công sau %ds! (+%s wei, mới: %s)", i+1, diff.String(), newBal1.String()))
					credited2 = true
					break
				}
			}
			if (i+1)%10 == 0 {
				fmt.Printf("   ... đang chờ chứng thực & ghi có trên Exec 1 (%ds/%ds)...\n", i+1, maxWaitSec)
			}
		}
		if !credited2 {
			lastStr1 := "chưa đọc được"
			if finalBal1 != nil {
				lastStr1 = finalBal1.String()
			}
			common.PrintError(fmt.Sprintf("❌ LỖI XÁC MINH: Chuỗi đích Exec 1 KHÔNG nhận được số dư sau %ds! (Ban đầu: %s wei, Sau %ds: %s wei)", maxWaitSec, initBal1.String(), maxWaitSec, lastStr1))
		}
	}

	common.PrintHeader("4. KIỂM TRA BẢO TOÀN TỔNG CUNG (CONSERVATION CHECK)")
	cons1, errC1 := common.GetConservation(cfg.Exec1URL)
	if errC1 != nil {
		common.PrintWarn(fmt.Sprintf("Exec 1 conservation endpoint: %v", errC1))
	} else {
		common.PrintSuccess(fmt.Sprintf("Exec 1 Conservation State: %+v", cons1))
	}

	cons2, errC2 := common.GetConservation(cfg.Exec2URL)
	if errC2 != nil {
		common.PrintWarn(fmt.Sprintf("Exec 2 conservation endpoint: %v", errC2))
	} else {
		common.PrintSuccess(fmt.Sprintf("Exec 2 Conservation State: %+v", cons2))
	}

	common.PrintHeader("TỔNG KẾT BÀI TEST")
	if err == nil && errRev == nil && credited && credited2 {
		fmt.Printf("%s🎉 TEST 02 HOÀN TẤT: Cả hai chiều chuyển tiền xuyên chain đều thực thi và ĐÃ XÁC NHẬN SỐ DƯ ĐÍCH TĂNG THÀNH CÔNG!%s\n", common.ColorGreen, common.ColorReset)
		os.Exit(0)
	} else {
		fmt.Printf("%s❌ TEST 02 THẤT BÀI: Một hoặc cả hai chiều chuyển tiền xuyên chuỗi không cập nhật số dư đích!%s\n", common.ColorRed, common.ColorReset)
		os.Exit(1)
	}
}
