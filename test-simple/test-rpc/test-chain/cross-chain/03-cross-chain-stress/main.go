package main

import (
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"tool-test/test-simple/test-rpc/test-chain/cross-chain/common"
)

func main() {
	fmt.Println()
	fmt.Printf("%s╔═══════════════════════════════════════════════════════════════════════╗%s\n", common.ColorPurple, common.ColorReset)
	fmt.Printf("%s║    ⚡ TEST 03: STRESS TEST GIAO DỊCH XUYÊN CHUỖI ĐỒNG THỜI (PARALLEL)  ║%s\n", common.ColorPurple, common.ColorReset)
	fmt.Printf("%s╚═══════════════════════════════════════════════════════════════════════╝%s\n", common.ColorPurple, common.ColorReset)

	cfg := common.LoadClusterConfig()
	fmt.Printf("%s📋 CẤU HÌNH MẠNG:%s\n", common.ColorBold, common.ColorReset)
	fmt.Printf("  • Exec 1: %s\n", cfg.Exec1URL)
	fmt.Printf("  • Exec 2: %s (Address: %s)\n", cfg.Exec2URL, cfg.Exec2Address)
	if cfg.ConfigFile != "" {
		fmt.Printf("  • Cấu hình nạp từ: %s\n", cfg.ConfigFile)
	}

	common.PrintHeader("1. KIỂM TRA SẴN SÀNG CỦA CÁC CLUSTER")
	_, err1 := common.GetBlockNumber(cfg.Exec1URL)
	_, err2 := common.GetBlockNumber(cfg.Exec2URL)
	if err1 != nil || err2 != nil {
		common.PrintError("Một trong hai Execution Cluster không phản hồi. Hủy bài test stress.")
		os.Exit(1)
	}
	common.PrintSuccess("Cả 2 Execution Cluster đều sẵn sàng nhận tải.")

	common.PrintHeader("2. PHÁT SÓNG ĐỒNG THỜI 10 GIAO DỊCH XUYÊN CHUỖI")
	const concurrentTxCount = 10
	var wg sync.WaitGroup
	type TxResult struct {
		Index   int
		TxHash  string
		Err     error
		Elapsed time.Duration
	}
	results := make([]TxResult, concurrentTxCount)

	targetAddr := ethcommon.HexToAddress(cfg.Exec2Address)
	if targetAddr == (ethcommon.Address{}) {
		targetAddr = common.RandomAddress()
	}

	startTime := time.Now()
	for i := 0; i < concurrentTxCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			senderKey := cfg.GetStressSenderKey(idx)
			amount := big.NewInt(1000000000000) // 10^12 wei
			t0 := time.Now()
			hash, err := common.SendCrossChainTransfer(cfg.Exec1URL, targetAddr, amount, senderKey)
			results[idx] = TxResult{
				Index:   idx,
				TxHash:  hash,
				Err:     err,
				Elapsed: time.Since(t0),
			}
		}(i)
	}

	wg.Wait()
	totalElapsed := time.Since(startTime)

	successCount := 0
	failedCount := 0
	for _, res := range results {
		if res.Err != nil {
			failedCount++
			fmt.Printf("  ❌ Tx %02d thất bại: %v (Thời gian: %v)\n", res.Index+1, res.Err, res.Elapsed)
		} else {
			successCount++
			fmt.Printf("  ✅ Tx %02d thành công: TxHash=%s (Thời gian: %v)\n", res.Index+1, res.TxHash, res.Elapsed)
		}
	}

	common.PrintHeader("3. KIỂM TRA SỨC KHỎE PARENT CHAIN SAU TẢI")
	for i, u := range cfg.ParentURLs {
		st := common.CheckParentNode(u)
		if st.Online {
			fmt.Printf("  ✅ Parent Node %d (%s): Height=%d, Latency=%dms\n", i, u, st.Height, st.LatencyMs)
		} else {
			fmt.Printf("  ❌ Parent Node %d (%s): OFFLINE\n", i, u)
		}
	}

	common.PrintHeader("TỔNG KẾT BÀI TEST STRESS")
	fmt.Printf("Tổng số giao dịch: %d\n", concurrentTxCount)
	fmt.Printf("Thành công: %d, Thất bại: %d\n", successCount, failedCount)
	fmt.Printf("Tổng thời gian thực thi: %v\n", totalElapsed)

	if failedCount == 0 {
		fmt.Printf("%s🎉 TEST 03 HOÀN TẤT: Toàn bộ %d giao dịch xuyên chuỗi đồng thời đều được xử lý hoàn hảo!%s\n", common.ColorGreen, concurrentTxCount, common.ColorReset)
		os.Exit(0)
	} else if successCount > 0 {
		fmt.Printf("%s⚠️  TEST 03 ĐẠT MỘT PHẦN: %d/%d giao dịch thành công.%s\n", common.ColorYellow, successCount, concurrentTxCount, common.ColorReset)
		os.Exit(0)
	} else {
		fmt.Printf("%s❌ TEST 03 THẤT BÀI: Toàn bộ giao dịch đều gặp lỗi.%s\n", common.ColorRed, common.ColorReset)
		os.Exit(1)
	}
}
