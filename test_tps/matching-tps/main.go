package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"time"
)

type Config struct {
	Batches        []int `json:"batches"`
	Count          int   `json:"count"`
	Rounds         int   `json:"rounds"`
	ParallelNative bool  `json:"parallel_native"`
	LoadBalance    bool  `json:"load_balance"`
}

type BlastResult struct {
	RoundTPS []float64 `json:"roundTPS"`
}

type BenchmarkResult struct {
	Batch    int
	AvgTPS   float64
	MaxTPS   float64
	MinTPS   float64
	RoundTPS []float64
}

func logToFile(filename string, message string) {
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("❌ Lỗi mở file log: %v\n", err)
		return
	}
	defer f.Close()
	f.WriteString(message + "\n")
}

func main() {
	// 1. Đọc config
	configData, err := os.ReadFile("config.json")
	if err != nil {
		fmt.Println("❌ Không thể đọc file config.json! Vui lòng tạo file config.json trước.")
		return
	}

	var cfg Config
	if err := json.Unmarshal(configData, &cfg); err != nil {
		fmt.Println("❌ Lỗi parse config.json:", err)
		return
	}

	logFile := "benchmark.log"
	startMsg := fmt.Sprintf("🚀 [%s] BẮT ĐẦU ĐO TPS VỚI CÁC BATCH KHÁC NHAU...", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Println(startMsg)
	fmt.Println("=========================================================")
	logToFile(logFile, "\n=========================================================")
	logToFile(logFile, startMsg)
	logToFile(logFile, fmt.Sprintf("Cấu hình: Count=%d, Rounds=%d, Batches=%v", cfg.Count, cfg.Rounds, cfg.Batches))

	results := []BenchmarkResult{}

	for _, batch := range cfg.Batches {
		fmt.Printf("\n▶️ ĐANG CHẠY TEST VỚI BATCH = %d...\n", batch)

		parallelArg := fmt.Sprintf("--parallel_native=%t", cfg.ParallelNative)
		loadBalanceArg := fmt.Sprintf("--load_balance=%t", cfg.LoadBalance)
		countArg := fmt.Sprintf("%d", cfg.Count)
		roundsArg := fmt.Sprintf("%d", cfg.Rounds)
		batchArg := fmt.Sprintf("--batch=%d", batch)

		cmd := exec.Command("go", "run", "main.go", "--count", countArg, parallelArg, "--rounds", roundsArg, loadBalanceArg, batchArg, "--amount", "1")
		cmd.Dir = "../tps_blast_cc" // Chạy trong thư mục gốc của tps_blast_cc
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Run()
		if err != nil {
			msg := fmt.Sprintf("❌ [%s] Chạy batch %d thất bại: %v", time.Now().Format("15:04:05"), batch, err)
			fmt.Println(msg)
			logToFile(logFile, msg)
			continue
		}

		// Đọc file kết quả json
		resultPath := "../tps_blast_cc/blast_cc_results.json"
		data, err := os.ReadFile(resultPath)
		if err != nil {
			msg := fmt.Sprintf("❌ [%s] Không thể đọc file kết quả %s: %v", time.Now().Format("15:04:05"), resultPath, err)
			fmt.Println(msg)
			logToFile(logFile, msg)
			continue
		}

		var bResult BlastResult
		err = json.Unmarshal(data, &bResult)
		if err != nil {
			msg := fmt.Sprintf("❌ [%s] Lỗi parse JSON: %v", time.Now().Format("15:04:05"), err)
			fmt.Println(msg)
			logToFile(logFile, msg)
			continue
		}

		// Tính Avg TPS
		if len(bResult.RoundTPS) == 0 {
			msg := fmt.Sprintf("❌ [%s] Không có kết quả TPS nào cho batch %d", time.Now().Format("15:04:05"), batch)
			fmt.Println(msg)
			logToFile(logFile, msg)
			continue
		}

		var sum float64
		var max float64
		var min float64 = -1
		for _, tps := range bResult.RoundTPS {
			sum += tps
			if tps > max {
				max = tps
			}
			if min == -1 || tps < min {
				min = tps
			}
		}
		avg := sum / float64(len(bResult.RoundTPS))

		results = append(results, BenchmarkResult{
			Batch:    batch,
			AvgTPS:   avg,
			MaxTPS:   max,
			MinTPS:   min,
			RoundTPS: bResult.RoundTPS,
		})

		successMsg := fmt.Sprintf("✅ [%s] BATCH %d HOÀN TẤT! Avg TPS: %.0f, Max TPS: %.0f, Min TPS: %.0f\n   👉 Chi tiết các round: %v", time.Now().Format("15:04:05"), batch, avg, max, min, bResult.RoundTPS)
		fmt.Println(successMsg)
		logToFile(logFile, successMsg)
	}

	// Sắp xếp kết quả theo Avg TPS giảm dần
	sort.Slice(results, func(i, j int) bool {
		return results[i].AvgTPS > results[j].AvgTPS
	})

	// Generate summary string
	summary := "\n╔══════════════════════════════════════════════════════════════════════════════════════════╗\n"
	summary += "║                   🏆 KẾT QUẢ SO SÁNH BATCH SIZE (TPS TỪ CAO XUỐNG THẤP)                  ║\n"
	summary += "╠══════════════════════════════════════════════════════════════════════════════════════════╣\n"
	summary += fmt.Sprintf("║ %-10s | %-12s | %-12s | %-12s | %-32s ║\n", "Batch Size", "Avg TPS", "Max TPS", "Min TPS", "Round TPS")
	summary += "║ ───────────┼──────────────┼──────────────┼──────────────┼──────────────────────────────────║\n"
	for _, res := range results {
		roundsStr := fmt.Sprintf("%v", res.RoundTPS)
		if len(roundsStr) > 32 {
			roundsStr = roundsStr[:29] + "..."
		}
		summary += fmt.Sprintf("║ %-10d | %-12.0f | %-12.0f | %-12.0f | %-32s ║\n", res.Batch, res.AvgTPS, res.MaxTPS, res.MinTPS, roundsStr)
	}
	summary += "╚══════════════════════════════════════════════════════════════════════════════════════════╝\n"

	if len(results) > 0 {
		summary += fmt.Sprintf("\n💡 TỐI ƯU NHẤT: Chọn batch = %d (Avg TPS cao nhất: %.0f tx/s)\n", results[0].Batch, results[0].AvgTPS)
	}

	// In ra màn hình và ghi vào log
	fmt.Println(summary)
	logToFile(logFile, summary)
}
