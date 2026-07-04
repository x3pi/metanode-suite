import re

with open('/home/abc/chain-n/metanode-suite/test_tps/tps_blast_cc/main.go', 'r') as f:
    content = f.read()

# Replace block from `if len(traces) > 0 {` until the end of the `if trace` block
start_marker = "if len(traces) > 0 {"
end_marker = "}\n\t\t\t}\n\n\t\t}\n\n\t\t// Log warning if block times out"

start_idx = content.find(start_marker)
if start_idx != -1:
    end_idx = content.find(end_marker, start_idx)
    if end_idx != -1:
        new_block = """if len(traces) > 0 {
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
       content = content[:start_idx] + new_block + content[end_idx:]

with open('/home/abc/chain-n/metanode-suite/test_tps/tps_blast_cc/main.go', 'w') as f:
    f.write(content)

print("done")
