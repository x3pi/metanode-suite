#!/bin/bash
set -e

# Path to the source of node IPs
RPC_NODES_FILE="/tmp/rpc_nodes.json"

if [ ! -f "$RPC_NODES_FILE" ]; then
    echo "Error: $RPC_NODES_FILE not found." >&2
    exit 1
fi

echo "Reading IPs from $RPC_NODES_FILE..."

# Parse IPs for nodes m0, m1, m2, m3, m4 from /tmp/rpc_nodes.json
IP_0=$(jq -r '.nodes.m0 | sub("^https?://"; "") | split(":")[0]' "$RPC_NODES_FILE")
IP_1=$(jq -r '.nodes.m1 | sub("^https?://"; "") | split(":")[0]' "$RPC_NODES_FILE")
IP_2=$(jq -r '.nodes.m2 | sub("^https?://"; "") | split(":")[0]' "$RPC_NODES_FILE")
IP_3=$(jq -r '.nodes.m3 | sub("^https?://"; "") | split(":")[0]' "$RPC_NODES_FILE")
IP_4=$(jq -r '.nodes.m4 | sub("^https?://"; "") | split(":")[0]' "$RPC_NODES_FILE")

echo "Found node IPs:"
echo "  Node 0 (m0): $IP_0"
echo "  Node 1 (m1): $IP_1"
echo "  Node 2 (m2): $IP_2"
echo "  Node 3 (m3): $IP_3"
echo "  Node 4 (m4): $IP_4"

# 1. Update /home/abc/nhat/con-chain-v2/metanode-suite/test_tps/tps_blast_cc/config-multi.json
# Note: For config-multi.json, we exclude node 4 as requested.
FILE1="/home/abc/nhat/con-chain-v2/metanode-suite/test_tps/tps_blast_cc/config-multi.json"
if [ -f "$FILE1" ]; then
    echo "Updating $FILE1 (excluding node 4)..."
    
    parent_port=$(jq -r '.parent_connection_address | split(":")[1]' "$FILE1")
    rpc_0_port=$(jq -r '.rpc_0 | split(":")[1]' "$FILE1")
    conn_1_port=$(jq -r '.connection_node_1 | split(":")[1]' "$FILE1")
    rpc_1_port=$(jq -r '.rpc_1 | split(":")[1]' "$FILE1")
    conn_2_port=$(jq -r '.connection_node_2 | split(":")[1]' "$FILE1")
    rpc_2_port=$(jq -r '.rpc_2 | split(":")[1]' "$FILE1")
    conn_3_port=$(jq -r '.connection_node_3 | split(":")[1]' "$FILE1")
    rpc_3_port=$(jq -r '.rpc_3 | split(":")[1]' "$FILE1")

    new_parent="${IP_0}:${parent_port}"
    new_rpc_0="${IP_0}:${rpc_0_port}"
    new_conn_1="${IP_1}:${conn_1_port}"
    new_rpc_1="${IP_1}:${rpc_1_port}"
    new_conn_2="${IP_2}:${conn_2_port}"
    new_rpc_2="${IP_2}:${rpc_2_port}"
    new_conn_3="${IP_3}:${conn_3_port}"
    new_rpc_3="${IP_3}:${rpc_3_port}"

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
    echo "Updating $FILE2 (including node 4)..."
    
    rpc_url_port=$(jq -r '.rpc_url | sub("^https?://"; "") | split(":")[1]' "$FILE2")
    port_0=$(jq -r '.rpc_urls[0] | sub("^https?://"; "") | split(":")[1] // "8545"' "$FILE2")
    port_1=$(jq -r '.rpc_urls[1] | sub("^https?://"; "") | split(":")[1] // "8547"' "$FILE2")
    port_2=$(jq -r '.rpc_urls[2] | sub("^https?://"; "") | split(":")[1] // "8548"' "$FILE2")
    port_3=$(jq -r '.rpc_urls[3] | sub("^https?://"; "") | split(":")[1] // "8549"' "$FILE2")
    port_4=$(jq -r '.rpc_urls[4] | sub("^https?://"; "") | split(":")[1] // "8550"' "$FILE2")

    new_rpc_url="http://${IP_0}:${rpc_url_port}"
    new_url_0="http://${IP_0}:${port_0}"
    new_url_1="http://${IP_1}:${port_1}"
    new_url_2="http://${IP_2}:${port_2}"
    new_url_3="http://${IP_3}:${port_3}"
    new_url_4="http://${IP_4}:${port_4}"

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
    echo "Updating $FILE3 (including node 4)..."
    
    port_m0=$(jq -r '.nodes.m0 | sub("^https?://"; "") | split(":")[1] // "8757"' "$FILE3")
    port_m1=$(jq -r '.nodes.m1 | sub("^https?://"; "") | split(":")[1] // "10747"' "$FILE3")
    port_m2=$(jq -r '.nodes.m2 | sub("^https?://"; "") | split(":")[1] // "10749"' "$FILE3")
    port_m3=$(jq -r '.nodes.m3 | sub("^https?://"; "") | split(":")[1] // "10750"' "$FILE3")
    port_m4=$(jq -r '.nodes.m4 | sub("^https?://"; "") | split(":")[1] // "10748"' "$FILE3")

    new_m0="http://${IP_0}:${port_m0}"
    new_m1="http://${IP_1}:${port_m1}"
    new_m2="http://${IP_2}:${port_m2}"
    new_m3="http://${IP_3}:${port_m3}"
    new_m4="http://${IP_4}:${port_m4}"

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

echo "Done updating IPs."
