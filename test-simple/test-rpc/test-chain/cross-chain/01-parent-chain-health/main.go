package main

import (
	"fmt"
	"os"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/cross-chain/common"
)

func main() {
	fmt.Println()
	fmt.Printf("%s╔═══════════════════════════════════════════════════════════════════════╗%s\n", common.ColorPurple, common.ColorReset)
	fmt.Printf("%s║       🔍 TEST 01: KIỂM TRA SỨC KHỎE TOÀN DIỆN PARENT CHAIN            ║%s\n", common.ColorPurple, common.ColorReset)
	fmt.Printf("%s╚═══════════════════════════════════════════════════════════════════════╝%s\n", common.ColorPurple, common.ColorReset)

	cfg := common.LoadClusterConfig()
	fmt.Printf("%s📋 CẤU HÌNH PARENT CHAIN PHÁT HIỆN ĐƯỢC:%s\n", common.ColorBold, common.ColorReset)
	fmt.Printf("Tổng số nodes cấu hình: %d\n", len(cfg.ParentURLs))
	for i, u := range cfg.ParentURLs {
		fmt.Printf("  • Node %d: %s\n", i, u)
	}

	common.PrintHeader("1. KIỂM TRA KẾT NỐI VÀ TRẠNG THÁI TỪNG NODE")
	var onlineNodes []common.ParentNodeStatus
	var offlineNodes []common.ParentNodeStatus

	for i, u := range cfg.ParentURLs {
		st := common.CheckParentNode(u)
		if st.Online {
			onlineNodes = append(onlineNodes, st)
			syncStr := fmt.Sprintf("syncing=%t", st.Syncing)
			if st.Syncing {
				syncStr = common.ColorYellow + syncStr + common.ColorReset
			}
			forkStr := fmt.Sprintf("fork=%t", st.ForkDetected)
			if st.ForkDetected {
				forkStr = common.ColorRed + forkStr + common.ColorReset
			}
			fmt.Printf("  ✅ Node %d (%s): Height=%d, %s, %s, Latency=%dms\n",
				i, u, st.Height, syncStr, forkStr, st.LatencyMs)
		} else {
			offlineNodes = append(offlineNodes, st)
			fmt.Printf("  ❌ Node %d (%s): OFFLINE (Lỗi: %s)\n", i, u, st.Error)
		}
	}

	common.PrintHeader("2. KIỂM TRA ĐẠT BFT QUORUM (2f + 1)")
	total := len(cfg.ParentURLs)
	f := (total - 1) / 3
	requiredQuorum := 2*f + 1
	onlineCount := len(onlineNodes)
	fmt.Printf("Tổng node: %d, Số lỗi tối đa f=%d, Quorum tối thiểu cần: %d, Đang hoạt động: %d\n",
		total, f, requiredQuorum, onlineCount)

	if onlineCount >= requiredQuorum {
		common.PrintSuccess(fmt.Sprintf("Parent Chain đạt BFT Quorum hợp lệ (%d/%d online)", onlineCount, total))
	} else {
		common.PrintError(fmt.Sprintf("Parent Chain mất BFT Quorum! (%d/%d online, cần tối thiểu %d)", onlineCount, total, requiredQuorum))
	}

	common.PrintHeader("3. KIỂM TRA ĐỒNG BỘ CHIỀU CAO (HEIGHT DIVERGENCE)")
	if len(onlineNodes) > 1 {
		var minH, maxH uint64 = onlineNodes[0].Height, onlineNodes[0].Height
		for _, n := range onlineNodes {
			if n.Height < minH {
				minH = n.Height
			}
			if n.Height > maxH {
				maxH = n.Height
			}
		}
		divergence := maxH - minH
		if divergence <= 2 {
			common.PrintSuccess(fmt.Sprintf("Chiều cao các node đồng bộ tốt (min=%d, max=%d, độ lệch=%d block)", minH, maxH, divergence))
		} else {
			common.PrintWarn(fmt.Sprintf("Phát hiện lệch block đáng kể giữa các node (min=%d, max=%d, chênh lệch=%d)", minH, maxH, divergence))
		}
	} else if len(onlineNodes) == 1 {
		common.PrintWarn("Chỉ có 1 node online, không thể so sánh độ đồng bộ chiều cao giữa các node.")
	}

	common.PrintHeader("4. KIỂM TRA TIẾN TRÌNH SINH BLOCK (LIVENESS)")
	if len(onlineNodes) > 0 {
		probeNode := onlineNodes[0].URL
		startH := onlineNodes[0].Height
		fmt.Printf("Quan sát node %s từ height %d trong 4 giây...\n", probeNode, startH)
		time.Sleep(4 * time.Second)
		endSt := common.CheckParentNode(probeNode)
		if endSt.Online {
			delta := endSt.Height - startH
			if delta > 0 {
				common.PrintSuccess(fmt.Sprintf("Parent Chain đang sinh block bình thường (tăng +%d block trong 4s, hiện tại=%d)", delta, endSt.Height))
			} else {
				common.PrintWarn(fmt.Sprintf("Chiều cao giữ nguyên ở %d (chain có thể sinh block theo chu kỳ dài hơn 4s hoặc đang idle)", endSt.Height))
			}
		}
	}

	common.PrintHeader("TỔNG KẾT BÀI TEST")
	if len(offlineNodes) == 0 && onlineCount >= requiredQuorum {
		fmt.Printf("%s🎉 TEST 01 HOÀN TẤT: Toàn bộ node Parent Chain hoạt động xuất sắc!%s\n", common.ColorGreen, common.ColorReset)
		os.Exit(0)
	} else if onlineCount >= requiredQuorum {
		fmt.Printf("%s⚠️  TEST 01 ĐẠT VỚI CẢNH BÁO: Parent Chain đạt quorum nhưng có %d node offline.%s\n", common.ColorYellow, len(offlineNodes), common.ColorReset)
		os.Exit(0)
	} else {
		fmt.Printf("%s❌ TEST 01 THẤT BÀI: Parent Chain không đủ quorum hoạt động!%s\n", common.ColorRed, common.ColorReset)
		os.Exit(1)
	}
}
