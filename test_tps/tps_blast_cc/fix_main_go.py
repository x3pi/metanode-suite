import re

with open('/home/abc/chain-n/metanode-suite/test_tps/tps_blast_cc/main.go', 'r') as f:
    content = f.read()

# Replace TotalBlockDurationUs with TotalExecutionUs around line 1530-1540
content = content.replace("if t.TotalBlockDurationUs > 0 {", "if t.TotalExecutionUs > 0 {")
content = content.replace("onChainDuration = time.Duration(t.TotalBlockDurationUs) * time.Microsecond", "onChainDuration = time.Duration(t.TotalExecutionUs) * time.Microsecond")

# Replace block from 1646 to end of bottleneck loop with new format
new_block = """--- IN BLOCK TRACES REPORT ---
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
Use regex to find and replace the block
pattern = r"// --- IN BLOCK TRACES REPORT ---.*?if avgCommit > avgEVM {.*?}"
# Since regex for such a large block is tricky, we can slice it
start_idx = content.find("// --- IN BLOCK TRACES REPORT ---")
if start_idx != -1:
    end_pattern = 'sb.WriteString(fmt.Sprintf("\\n## 📊 BENCHMARK SUMMARY\\n"))'
    end_idx = content.find(end_pattern)
    if end_idx != -1:
        # Before replacing, make sure we get exactly what we need
        # wait, the original block has:
        # sb.WriteString(fmt.Sprintf("\n## 📊 BENCHMARK SUMMARY\n"))
        # which is after the trace block
        content = content[:start_idx] + new_block + "\n\n" + content[end_idx:]

with open('/home/abc/chain-n/metanode-suite/test_tps/tps_blast_cc/main.go', 'w') as f:
    f.write(content)

print("done")
