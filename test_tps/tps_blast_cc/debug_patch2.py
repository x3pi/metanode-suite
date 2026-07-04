import re

with open('/home/abc/chain-n/metanode-suite/test_tps/tps_blast_cc/main.go', 'r') as f:
    content = f.read()

search_str = """\tblk, err := rpcClient.GetBlockByNumber(blockNum)"""
replace_str = """\tblk, err := rpcClient.GetBlockByNumber(blockNum)
\tif err != nil { fmt.Printf("DEBUG: Block %d failed to fetch: %v\\n", blockNum, err) } else { if blk == nil { fmt.Printf("DEBUG: Block %d is nil\\n", blockNum) } }"""

new_content = content.replace(search_str, replace_str)
with open('/home/abc/chain-n/metanode-suite/test_tps/tps_blast_cc/main.go', 'w') as f:
    f.write(new_content)
