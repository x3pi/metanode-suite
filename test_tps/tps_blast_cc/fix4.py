import re

with open('/home/abc/chain-n/metanode-suite/test_tps/tps_blast_cc/main.go', 'r') as f:
    content = f.read()

start_marker = "if trace {\n\t\t\t\t// --- IN BLOCK TRACES REPORT ---"
# Let's find the `## 📊 BENCHMARK SUMMARY`
end_marker = "## 📊 BENCHMARK SUMMARY"

start_idx = content.find("if trace {")
# let's be more precise
search_start = content.find("if blockCount > 0 {")
if search_start != -1:
    start_idx = content.find("if trace {", search_start)
    if start_idx != -1:
        end_idx = content.find('sb.WriteString(fmt.Sprintf("\\n## 📊 BENCHMARK SUMMARY\\n"))', start_idx)
        if end_idx != -1:
            new_block = """if trace {
--- IN BLOCK TRACES REPORT ---
g(fmt.Sprintf("\\n  📝 BLOCK PERFORMANCE TRACES (Blocks %d to %d) [PURE RUST]\\n", startBlock+1, endBlock))
g(fmt.Sprintf("  %-8s | %-6s | %-20s | %-20s | %-20s\\n",
"TXs", "BlockSTM EVM (ms)", "BlockSTM Commit (ms)", "Total Execution (ms)"))
g(fmt.Sprintf("  %s\\n", strings.Repeat("-", 85)))

tracesErr != nil {
g(fmt.Sprintf("  ❌ Could not fetch block traces: %v\\n", tracesErr))
else {
totalEVM, totalCommit, totalTotal float64

_, t := range traces {
+= float64(t.EvmExecutionDurationUs)
+= float64(t.CommitDurationUs)
+= float64(t.TotalExecutionUs)

g(fmt.Sprintf("  %-8d | %-6d | %-20.2f | %-20.2f | %-20.2f\\n",
umber, t.TxCount,
DurationUs)/1000.0,
Us)/1000.0,
Us)/1000.0))
len(traces) > 0 {
 := float64(len(traces))
g(fmt.Sprintf("\\n  🔍 BOTTLENECK ANALYSIS (Average per Block)\\n"))
g(fmt.Sprintf("  %s\\n", strings.Repeat("-", 75)))
baseMs float64 = totalTotal / n / 1000.0
baseMs == 0 {
= 1
:= totalCommit / n / 1000.0
:= totalEVM / n / 1000.0

g(fmt.Sprintf("  🔴 BlockSTM Commit : %8.1f ms (%5.1f%%) | Ghi trạng thái NomtDB\\n", avgCommit, (avgCommit/baseMs)*100.0))
g(fmt.Sprintf("  🔴 BlockSTM EVM    : %8.1f ms (%5.1f%%) | Thực thi EVM song song\\n", avgEVM, (avgEVM/baseMs)*100.0))
g(fmt.Sprintf("  %s\\n", strings.Repeat("-", 75)))
g(fmt.Sprintf("  💡 Gợi ý tối ưu:\\n"))
avgCommit > avgEVM {
g(fmt.Sprintf("     - Commit cao (%.1fms). Nút thắt tại ổ cứng NomtDB.\\n", avgCommit))
g(fmt.Sprintf("     -> Tối ưu batch ghi hoặc chuyển sang SSD NVMe.\\n"))
else {
g(fmt.Sprintf("     - EVM chậm (%.1fms). Nút thắt tại VM/CPU.\\n", avgEVM))
g(fmt.Sprintf("     -> Tăng luồng BlockSTM EVM.\\n"))
Log warning if block times out
totalTxInBlocks < len(allTxs) {
g(fmt.Sprintf("\\n  ⚠️ Cảnh báo: %d TXs bị rớt (Không thấy trong block).\\n", len(allTxs)-totalTxInBlocks))
           content = content[:start_idx] + new_block + content[end_idx:]

with open('/home/abc/chain-n/metanode-suite/test_tps/tps_blast_cc/main.go', 'w') as f:
    f.write(content)

