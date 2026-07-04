import re

with open('/home/abc/chain-n/metanode-suite/test_tps/tps_blast_cc/main.go', 'r') as f:
    content = f.read()

# find:
#                   blkInfo, err := rpcClient.GetBlockByNumber(b)
#                   if err == nil && blkInfo != nil {
# replace with:
#                   blkInfo, err := rpcClient.GetBlockByNumber(b)
#                   fmt.Printf("DEBUG: Block %d, err: %v, blkInfo: %v\\n", b, err, blkInfo != nil)
#                   if err == nil && blkInfo != nil {
#                           fmt.Printf("DEBUG: Block %d has %d txs\\n", b, len(blkInfo.Transactions))

search_str = """\tblkInfo, err := rpcClient.GetBlockByNumber(b)
err == nil && blkInfo != nil {"""
replace_str = """\tblkInfo, err := rpcClient.GetBlockByNumber(b)
tf("DEBUG: Block %d, err: %v, blkInfo!=nil: %v\\n", b, err, blkInfo != nil)
err == nil && blkInfo != nil {
tf("DEBUG: Block %d has %d txs\\n", b, len(blkInfo.Transactions))"""

new_content = content.replace(search_str, replace_str)
with open('/home/abc/chain-n/metanode-suite/test_tps/tps_blast_cc/main.go', 'w') as f:
    f.write(new_content)
