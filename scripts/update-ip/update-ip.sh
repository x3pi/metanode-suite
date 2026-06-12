#!/bin/bash
set -e

# Path to the source of node IPs
RPC_NODES_FILE="/tmp/rpc_nodes.json"

if [ ! -f "$RPC_NODES_FILE" ]; then
    echo "Error: $RPC_NODES_FILE not found." >&2
    exit 1
fi

echo "Reading IPs and Proxy Ports from $RPC_NODES_FILE..."

# 1. Update /home/abc/nhat/con-chain-v2/metanode-suite/test_tps/tps_blast_cc/config-multi.json
# Note: For config-multi.json, we exclude node 4 as requested.
FILE1="/home/abc/nhat/con-chain-v2/metanode-suite/test_tps/tps_blast_cc/config-multi.json"
if [ -f "$FILE1" ]; then
    echo "Updating $FILE1 using RPC Proxies (excluding node 4)..."
    
    # Lấy parent_port cũ (thường là 4201)
    parent_port=$(jq -r '.parent_connection_address | split(":")[1]' "$FILE1")
    IP_0=$(jq -r '.nodes.m0 | sub("^https?://"; "") | split(":")[0]' "$RPC_NODES_FILE")
    
    new_parent="${IP_0}:${parent_port}"
    new_rpc_0=$(jq -r '.rpc_proxies.m0 | sub("^https?://"; "")' "$RPC_NODES_FILE")
    new_conn_1=$(jq -r '.tcp_nodes.m1' "$RPC_NODES_FILE")
    new_rpc_1=$(jq -r '.rpc_proxies.m1 | sub("^https?://"; "")' "$RPC_NODES_FILE")
    new_conn_2=$(jq -r '.tcp_nodes.m2' "$RPC_NODES_FILE")
    new_rpc_2=$(jq -r '.rpc_proxies.m2 | sub("^https?://"; "")' "$RPC_NODES_FILE")
    new_conn_3=$(jq -r '.tcp_nodes.m3' "$RPC_NODES_FILE")
    new_rpc_3=$(jq -r '.rpc_proxies.m3 | sub("^https?://"; "")' "$RPC_NODES_FILE")

    jq --arg p "$new_parent" \
       --arg r0 "$new_rpc_0" \
       --arg c1 "$new_conn_1" \
       --arg r1 "$new_rpc_1" \
       --arg c2 "$new_conn_2" \
       --arg r2 "$new_rpc_2" \
       --arg c3 "$new_conn_3" \
       --arg r3 "$new_rpc_3" \
       '.parent_connection_address = $p | .rpc_0 = $r0 | .connection_node_1 = $c1 | .rpc_1 = $r1 | .connection_node_2 = $c2 | .rpc_2 = $r2 | .connection_node_3 = $c3 | .rpc_3 = $r3' \
       "$FILE1" > "${FILE1}.tmp" && mv "${FILE1}.tmp" "$FILE1"
else
    echo "Warning: $FILE1 not found." >&2
fi

# 2. Update /home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-history/config-mutil.json
# Note: For test-history, we include node 4.
FILE2="/home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-history/config-mutil.json"
if [ -f "$FILE2" ]; then
    echo "Updating $FILE2 using RPC Proxies (including node 4)..."
    
    new_rpc_url=$(jq -r '.rpc_proxies.m0' "$RPC_NODES_FILE")
    new_url_0=$(jq -r '.rpc_proxies.m0' "$RPC_NODES_FILE")
    new_url_1=$(jq -r '.rpc_proxies.m1' "$RPC_NODES_FILE")
    new_url_2=$(jq -r '.rpc_proxies.m2' "$RPC_NODES_FILE")
    new_url_3=$(jq -r '.rpc_proxies.m3' "$RPC_NODES_FILE")
    new_url_4=$(jq -r '.rpc_proxies.m4' "$RPC_NODES_FILE")

    jq --arg r "$new_rpc_url" \
       --arg u0 "$new_url_0" \
       --arg u1 "$new_url_1" \
       --arg u2 "$new_url_2" \
       --arg u3 "$new_url_3" \
       --arg u4 "$new_url_4" \
       '.rpc_url = $r | .rpc_urls = [$u0, $u1, $u2, $u3, $u4]' \
       "$FILE2" > "${FILE2}.tmp" && mv "${FILE2}.tmp" "$FILE2"
else
    echo "Warning: $FILE2 not found." >&2
fi

# 3. Update /home/abc/nhat/con-chain-v2/metanode-suite/block/block_hash_checker/config-m-nodes.json
# Note: For checkhash, we include node 4 (m4).
FILE3="/home/abc/nhat/con-chain-v2/metanode-suite/block/block_hash_checker/config-m-nodes.json"
if [ -f "$FILE3" ]; then
    echo "Updating $FILE3 using RPC Proxies (including node 4)..."
    
    new_m0=$(jq -r '.rpc_proxies.m0' "$RPC_NODES_FILE")
    new_m1=$(jq -r '.rpc_proxies.m1' "$RPC_NODES_FILE")
    new_m2=$(jq -r '.rpc_proxies.m2' "$RPC_NODES_FILE")
    new_m3=$(jq -r '.rpc_proxies.m3' "$RPC_NODES_FILE")
    new_m4=$(jq -r '.rpc_proxies.m4' "$RPC_NODES_FILE")

    jq --arg m0 "$new_m0" \
       --arg m1 "$new_m1" \
       --arg m2 "$new_m2" \
       --arg m3 "$new_m3" \
       --arg m4 "$new_m4" \
       '.nodes = {m4: $m4, m3: $m3, m2: $m2, m1: $m1, m0: $m0}' \
       "$FILE3" > "${FILE3}.tmp" && mv "${FILE3}.tmp" "$FILE3"
else
    echo "Warning: $FILE3 not found." >&2
fi

echo "Done updating configs to use RPC proxies."
