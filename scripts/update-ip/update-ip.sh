#!/bin/bash
set -e

# Path to the source of node IPs
RPC_NODES_FILE="/tmp/rpc_nodes.json"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUITE_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"



if [ ! -f "$RPC_NODES_FILE" ]; then
    echo "Error: $RPC_NODES_FILE not found." >&2
    exit 1
fi


echo "Reading IPs and Proxy Ports from $RPC_NODES_FILE..."

# 1. Update $SUITE_DIR/test_tps/tps_blast_cc/config-multi.json
FILE1="$SUITE_DIR/test_tps/tps_blast_cc/config-multi.json"
if [ -f "$FILE1" ]; then
    echo "Updating $FILE1 using Validator Nodes..."
    
    new_parent=$(jq -r '((.tcp_nodes.m0 // "") // "")' "$RPC_NODES_FILE")
    new_rpc_0=$(jq -r '((.nodes.m0 // "") // "") | sub("^https?://"; "")' "$RPC_NODES_FILE")

    role_1=$(jq -r '((.roles.m1 // "validator"))' "$RPC_NODES_FILE")
    role_2=$(jq -r '((.roles.m2 // "validator"))' "$RPC_NODES_FILE")
    role_3=$(jq -r '((.roles.m3 // "validator"))' "$RPC_NODES_FILE")

    new_conn_1=""
    new_rpc_1=""
    if [ "$role_1" != "synconly" ]; then
        new_conn_1=$(jq -r '((.tcp_nodes.m1 // "") // "")' "$RPC_NODES_FILE")
        new_rpc_1=$(jq -r '((.nodes.m1 // "") // "") | sub("^https?://"; "")' "$RPC_NODES_FILE")
    fi

    new_conn_2=""
    new_rpc_2=""
    if [ "$role_2" != "synconly" ]; then
        new_conn_2=$(jq -r '((.tcp_nodes.m2 // "") // "")' "$RPC_NODES_FILE")
        new_rpc_2=$(jq -r '((.nodes.m2 // "") // "") | sub("^https?://"; "")' "$RPC_NODES_FILE")
    fi

    new_conn_3=""
    new_rpc_3=""
    if [ "$role_3" != "synconly" ]; then
        new_conn_3=$(jq -r '((.tcp_nodes.m3 // "") // "")' "$RPC_NODES_FILE")
        new_rpc_3=$(jq -r '((.nodes.m3 // "") // "") | sub("^https?://"; "")' "$RPC_NODES_FILE")
    fi

    jq --arg p "$new_parent" \
       --arg r0 "$new_rpc_0" \
       --arg c1 "$new_conn_1" \
       --arg r1 "$new_rpc_1" \
       --arg c2 "$new_conn_2" \
       --arg r2 "$new_rpc_2" \
       --arg c3 "$new_conn_3" \
       --arg r3 "$new_rpc_3" \
       '.parent_connection_address = $p | .rpc_0 = $r0 | .connection_node_1 = $c1 | .rpc_1 = $r1 | .connection_node_2 = $c2 | .rpc_2 = $r2 | .connection_node_3 = $c3 | .rpc_3 = $r3 | with_entries(select(.value != ""))' \
       "$FILE1" > "${FILE1}.tmp" && mv "${FILE1}.tmp" "$FILE1"
else
    echo "Warning: $FILE1 not found." >&2
fi

# 2. Update $SUITE_DIR/test-simple/test-rpc/test-history/config-mutil.json
FILE2="$SUITE_DIR/test-simple/test-rpc/test-history/config-mutil.json"
if [ -f "$FILE2" ]; then
    echo "Updating $FILE2 using Nodes (including sync nodes)..."
    
    new_rpc_url=$(jq -r '((.nodes.m0 // "") // "")' "$RPC_NODES_FILE")
    new_url_0=$(jq -r '((.nodes.m0 // "") // "")' "$RPC_NODES_FILE")
    new_url_1=$(jq -r '((.nodes.m1 // "") // "")' "$RPC_NODES_FILE")
    new_url_2=$(jq -r '((.nodes.m2 // "") // "")' "$RPC_NODES_FILE")
    new_url_3=$(jq -r '((.nodes.m3 // "") // "")' "$RPC_NODES_FILE")
    new_url_4=$(jq -r '((.nodes.m4 // "") // "")' "$RPC_NODES_FILE")

    jq --arg r "$new_rpc_url" \
       --arg u0 "$new_url_0" \
       --arg u1 "$new_url_1" \
       --arg u2 "$new_url_2" \
       --arg u3 "$new_url_3" \
       --arg u4 "$new_url_4" \
       '.rpc_url = $r | .rpc_urls = [$u0, $u1, $u2, $u3, $u4] | .rpc_urls |= map(select(. != ""))' \
       "$FILE2" > "${FILE2}.tmp" && mv "${FILE2}.tmp" "$FILE2"
else
    echo "Warning: $FILE2 not found." >&2
fi

# 3. Update $SUITE_DIR/block/block_hash_checker/config-m-nodes.json
FILE3="$SUITE_DIR/block/block_hash_checker/config-m-nodes.json"
if [ -f "$FILE3" ]; then
    echo "Updating $FILE3 using Nodes (including sync nodes)..."
    
    new_m0=$(jq -r '((.nodes.m0 // "") // "")' "$RPC_NODES_FILE")
    new_m1=$(jq -r '((.nodes.m1 // "") // "")' "$RPC_NODES_FILE")
    new_m2=$(jq -r '((.nodes.m2 // "") // "")' "$RPC_NODES_FILE")
    new_m3=$(jq -r '((.nodes.m3 // "") // "")' "$RPC_NODES_FILE")
    new_m4=$(jq -r '((.nodes.m4 // "") // "")' "$RPC_NODES_FILE")

    jq --arg m0 "$new_m0" \
       --arg m1 "$new_m1" \
       --arg m2 "$new_m2" \
       --arg m3 "$new_m3" \
       --arg m4 "$new_m4" \
       '.nodes = {m4: $m4, m3: $m3, m2: $m2, m1: $m1, m0: $m0} | .nodes |= with_entries(select(.value != ""))' \
       "$FILE3" > "${FILE3}.tmp" && mv "${FILE3}.tmp" "$FILE3"
else
    echo "Warning: $FILE3 not found." >&2
fi

echo "Done updating configs to use Node endpoints."

# 4. Update $SUITE_DIR/test-simple/test-rpc/spam_xapian/config-m-node.json
FILE4="$SUITE_DIR/test-simple/test-rpc/spam_xapian/config-m-node.json"
if [ -f "$FILE4" ]; then
    echo "Updating $FILE4 using Validator RPC Nodes..."
    
    new_rpc_url=$(jq -r '((.nodes.m0 // "") // "")' "$RPC_NODES_FILE")
    
    jq --arg r "$new_rpc_url" \
       --slurpfile rpc "$RPC_NODES_FILE" \
       '.rpc_url = $r | .rpc_urls = [$rpc[0].nodes | to_entries[] | select($rpc[0].roles[.key] != "synconly") | .value]' \
       "$FILE4" > "${FILE4}.tmp" && mv "${FILE4}.tmp" "$FILE4"
else
    echo "Warning: $FILE4 not found." >&2
fi

# 5. Update $SUITE_DIR/register_bls/tcp/config.json
FILE5="$SUITE_DIR/register_bls/tcp/config.json"
if [ -f "$FILE5" ]; then
    echo "Updating $FILE5 using TCP nodes and RPC Endpoints..."

    new_parent_0=$(jq -r '((.tcp_nodes.m0 // "") // "")' "$RPC_NODES_FILE")
    new_parent_1=$(jq -r '((.tcp_nodes.m1 // "") // "")' "$RPC_NODES_FILE")
    new_parent_2=$(jq -r '((.tcp_nodes.m2 // "") // "")' "$RPC_NODES_FILE")
    new_parent_3=$(jq -r '((.tcp_nodes.m3 // "") // "")' "$RPC_NODES_FILE")
    new_parent_4=$(jq -r '((.tcp_nodes.m4 // "") // "")' "$RPC_NODES_FILE")

    new_rpc_0=$(jq -r '((.nodes.m0 // "") // "")' "$RPC_NODES_FILE")
    new_rpc_1=$(jq -r '((.nodes.m1 // "") // "")' "$RPC_NODES_FILE")
    new_rpc_2=$(jq -r '((.nodes.m2 // "") // "")' "$RPC_NODES_FILE")
    new_rpc_3=$(jq -r '((.nodes.m3 // "") // "")' "$RPC_NODES_FILE")
    new_rpc_4=$(jq -r '((.nodes.m4 // "") // "")' "$RPC_NODES_FILE")

    jq --arg p0 "$new_parent_0" --arg p1 "$new_parent_1" --arg p2 "$new_parent_2" --arg p3 "$new_parent_3" --arg p4 "$new_parent_4" \
       --arg r0 "$new_rpc_0" --arg r1 "$new_rpc_1" --arg r2 "$new_rpc_2" --arg r3 "$new_rpc_3" --arg r4 "$new_rpc_4" \
       '.parent_connection_address = [$p0, $p1, $p2, $p3, $p4] | .parent_connection_address |= map(select(. != "")) | .rpc_endpoints = [$r0, $r1, $r2, $r3, $r4] | .rpc_endpoints |= map(select(. != ""))' \
       "$FILE5" > "${FILE5}.tmp" && mv "${FILE5}.tmp" "$FILE5"
else
    echo "Warning: $FILE5 not found." >&2
fi

# 6. Update $SUITE_DIR/test-simple/test-rpc/test-blockstm/config.json
FILE6="$SUITE_DIR/test-simple/test-rpc/test-blockstm/config.json"
if [ -f "$FILE6" ]; then
    echo "Updating $FILE6 using RPC Nodes..."
    
    new_rpc_url=$(jq -r '((.nodes.m0 // "") // "")' "$RPC_NODES_FILE")
    
    jq --arg r "$new_rpc_url" \
       --slurpfile rpc "$RPC_NODES_FILE" \
       'del(.all_nodes, .roles) | .rpc_url = (if $r != "" then $r else .rpc_url end) | .rpc_nodes = ($rpc[0].nodes | with_entries(select($rpc[0].roles[.key] != "synconly"))) | .sync_nodes = ($rpc[0].nodes | with_entries(select($rpc[0].roles[.key] == "synconly")))' \
       "$FILE6" > "${FILE6}.tmp" && mv "${FILE6}.tmp" "$FILE6"

    # Cập nhật thông tin Private Chains từ /tmp/private_chains.json (nếu có)
    PRIV_CHAINS_FILE="/tmp/private_chains.json"
    if [ -f "$PRIV_CHAINS_FILE" ]; then
        echo "Updating Private Chains RPC in $FILE6 from $PRIV_CHAINS_FILE..."
        python3 -c "
import json
with open('$FILE6') as f:
    cfg = json.load(f)
with open('$PRIV_CHAINS_FILE') as f:
    p_data = json.load(f)

nodes = p_data.get('nodes', {})
chain_nodes = p_data.get('chain_nodes', {})
if 'private_chains' not in cfg:
    cfg['private_chains'] = {}

name_map = {'101': 'chain_a', '102': 'chain_b', '103': 'chain_c', '104': 'chain_d'}
for cid, rpc_url in nodes.items():
    c_name = name_map.get(str(cid), f'chain_{cid}')
    c_info = chain_nodes.get(str(cid), {})
    rpc_nodes_map = c_info.get('rpc_nodes', {'m0': rpc_url})

    if c_name in cfg['private_chains']:
        cfg['private_chains'][c_name]['rpc_url'] = rpc_url
        cfg['private_chains'][c_name]['rpc_nodes'] = rpc_nodes_map
    else:
        cfg['private_chains'][c_name] = {
            'chain_id': int(cid),
            'rpc_url': rpc_url,
            'rpc_nodes': rpc_nodes_map,
            'private_keys': []
        }

with open('$FILE6', 'w') as f:
    json.dump(cfg, f, indent=2)
print('✅ Updated private_chains in $FILE6')
"
    fi
else
    echo "Warning: $FILE6 not found." >&2
fi

# 7. Update $SUITE_DIR/test_tps/tps_contract/config-multi.json
FILE7="$SUITE_DIR/test_tps/tps_contract/config-multi.json"
if [ -f "$FILE7" ]; then
    echo "Updating $FILE7 using Validator Nodes..."
    
    new_parent=$(jq -r '((.tcp_nodes.m0 // "") // "")' "$RPC_NODES_FILE")
    new_rpc_0=$(jq -r '((.nodes.m0 // "") // "") | sub("^https?://"; "")' "$RPC_NODES_FILE")

    role_1=$(jq -r '((.roles.m1 // "validator"))' "$RPC_NODES_FILE")
    role_2=$(jq -r '((.roles.m2 // "validator"))' "$RPC_NODES_FILE")
    role_3=$(jq -r '((.roles.m3 // "validator"))' "$RPC_NODES_FILE")

    new_conn_1=""
    new_rpc_1=""
    if [ "$role_1" != "synconly" ]; then
        new_conn_1=$(jq -r '((.tcp_nodes.m1 // "") // "")' "$RPC_NODES_FILE")
        new_rpc_1=$(jq -r '((.nodes.m1 // "") // "") | sub("^https?://"; "")' "$RPC_NODES_FILE")
    fi

    new_conn_2=""
    new_rpc_2=""
    if [ "$role_2" != "synconly" ]; then
        new_conn_2=$(jq -r '((.tcp_nodes.m2 // "") // "")' "$RPC_NODES_FILE")
        new_rpc_2=$(jq -r '((.nodes.m2 // "") // "") | sub("^https?://"; "")' "$RPC_NODES_FILE")
    fi

    new_conn_3=""
    new_rpc_3=""
    if [ "$role_3" != "synconly" ]; then
        new_conn_3=$(jq -r '((.tcp_nodes.m3 // "") // "")' "$RPC_NODES_FILE")
        new_rpc_3=$(jq -r '((.nodes.m3 // "") // "") | sub("^https?://"; "")' "$RPC_NODES_FILE")
    fi

    jq --arg p "$new_parent" \
       --arg r0 "$new_rpc_0" \
       --arg c1 "$new_conn_1" \
       --arg r1 "$new_rpc_1" \
       --arg c2 "$new_conn_2" \
       --arg r2 "$new_rpc_2" \
       --arg c3 "$new_conn_3" \
       --arg r3 "$new_rpc_3" \
       '.parent_connection_address = $p | .rpc_0 = $r0 | .connection_node_1 = $c1 | .rpc_1 = $r1 | .connection_node_2 = $c2 | .rpc_2 = $r2 | .connection_node_3 = $c3 | .rpc_3 = $r3 | with_entries(select(.value != ""))' \
       "$FILE7" > "${FILE7}.tmp" && mv "${FILE7}.tmp" "$FILE7"
else
    echo "Warning: $FILE7 not found." >&2
fi

# 8. Update $SUITE_DIR/test_tps/tps_contract_parallel/config-multi.json
FILE8="$SUITE_DIR/test_tps/tps_contract_parallel/config-multi.json"
if [ -f "$FILE8" ]; then
    echo "Updating $FILE8 using Validator Nodes..."
    
    new_parent=$(jq -r '((.tcp_nodes.m0 // "") // "")' "$RPC_NODES_FILE")
    new_rpc_0=$(jq -r '((.nodes.m0 // "") // "") | sub("^https?://"; "")' "$RPC_NODES_FILE")

    role_1=$(jq -r '((.roles.m1 // "validator"))' "$RPC_NODES_FILE")
    role_2=$(jq -r '((.roles.m2 // "validator"))' "$RPC_NODES_FILE")
    role_3=$(jq -r '((.roles.m3 // "validator"))' "$RPC_NODES_FILE")

    new_conn_1=""
    new_rpc_1=""
    if [ "$role_1" != "synconly" ]; then
        new_conn_1=$(jq -r '((.tcp_nodes.m1 // "") // "")' "$RPC_NODES_FILE")
        new_rpc_1=$(jq -r '((.nodes.m1 // "") // "") | sub("^https?://"; "")' "$RPC_NODES_FILE")
    fi

    new_conn_2=""
    new_rpc_2=""
    if [ "$role_2" != "synconly" ]; then
        new_conn_2=$(jq -r '((.tcp_nodes.m2 // "") // "")' "$RPC_NODES_FILE")
        new_rpc_2=$(jq -r '((.nodes.m2 // "") // "") | sub("^https?://"; "")' "$RPC_NODES_FILE")
    fi

    new_conn_3=""
    new_rpc_3=""
    if [ "$role_3" != "synconly" ]; then
        new_conn_3=$(jq -r '((.tcp_nodes.m3 // "") // "")' "$RPC_NODES_FILE")
        new_rpc_3=$(jq -r '((.nodes.m3 // "") // "") | sub("^https?://"; "")' "$RPC_NODES_FILE")
    fi

    jq --arg p "$new_parent" \
       --arg r0 "$new_rpc_0" \
       --arg c1 "$new_conn_1" \
       --arg r1 "$new_rpc_1" \
       --arg c2 "$new_conn_2" \
       --arg r2 "$new_rpc_2" \
       --arg c3 "$new_conn_3" \
       --arg r3 "$new_rpc_3" \
       '.parent_connection_address = $p | .rpc_0 = $r0 | .connection_node_1 = $c1 | .rpc_1 = $r1 | .connection_node_2 = $c2 | .rpc_2 = $r2 | .connection_node_3 = $c3 | .rpc_3 = $r3 | with_entries(select(.value != ""))' \
       "$FILE8" > "${FILE8}.tmp" && mv "${FILE8}.tmp" "$FILE8"
else
    echo "Warning: $FILE8 not found." >&2
fi
