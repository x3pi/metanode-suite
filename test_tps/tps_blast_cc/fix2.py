import re

with open('/home/abc/chain-n/metanode-suite/test_tps/tps_blast_cc/main.go', 'r') as f:
    content = f.read()

# Replace block around line 1536
content = content.replace("t.TotalBlockDurationUs", "t.TotalExecutionUs")

# Find the block starting at `if trace {` under `if blockCount > 0 {`
# We'll just replace the entire `if trace { ... }` block inside `if blockCount > 0 {`
# Let's locate `// --- IN BLOCK TRACES REPORT ---`
start_marker = "// --- IN BLOCK TRACES REPORT ---"
end_marker = "}\n\t\t}\n\n\t\t// Log warning if block times out"

start_idx = content.find(start_marker)
if start_idx != -1:
    end_idx = content.find(end_marker, start_idx)
    if end_idx != -1:
        # We replace everything from start_marker to end_marker
        new_block = """// --- IN BLOCK TRACES REPORT ---
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
       # Also need to fix lines 1608: `realTotalUs := float64(t.WaitGoUs) + float64(t.WaitRustUs) + float64(t.ProcessTxsDurationUs) + float64(t.TotalExecutionUs)`
        # to `realTotalUs := float64(t.TotalExecutionUs)`
        # Let's just do a string replace on the whole content for that specific line
        content = content[:start_idx] + new_block + content[end_idx:]

content = content.replace("realTotalUs := float64(t.WaitGoUs) + float64(t.WaitRustUs) + float64(t.ProcessTxsDurationUs) + float64(t.TotalExecutionUs)", "realTotalUs := float64(t.TotalExecutionUs)")
content = content.replace("realTotalUs := float64(t.WaitGoUs) + float64(t.WaitRustUs) + float64(t.ProcessTxsDurationUs) + float64(t.TotalBlockDurationUs)", "realTotalUs := float64(t.TotalExecutionUs)")

with open('/home/abc/chain-n/metanode-suite/test_tps/tps_blast_cc/main.go', 'w') as f:
    f.write(content)

