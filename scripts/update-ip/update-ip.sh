#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUITE_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
METANODE_DIR="$(cd "$SUITE_DIR/../metanode" 2>/dev/null && pwd || echo "/home/abc/nhat/con-chain-v2/metanode")"

RPC_NODES_FILE="/tmp/rpc_nodes.json"
PRIV_CHAINS_FILE="/tmp/private_chains.json"

TARGET_CHAIN_ARG="${TARGET_CHAIN:-${CHAIN:-""}}"

while [[ $# -gt 0 ]]; do
    case "$1" in
        -c|--chain)
            TARGET_CHAIN_ARG="$2"
            shift 2
            ;;
        --chain=*)
            TARGET_CHAIN_ARG="${1#*=}"
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS] [CHAIN_ID_OR_NAME]"
            echo "  Updates test configuration files for Public Chain or a specific Private Chain."
            echo ""
            echo "Options:"
            echo "  -c, --chain <id|name>   Target specific chain (e.g., 101, 102, chain_a, public, 991)"
            echo "  -h, --help              Show this help message"
            exit 0
            ;;
        *)
            if [ -z "$TARGET_CHAIN_ARG" ]; then
                TARGET_CHAIN_ARG="$1"
                shift
            else
                shift
            fi
            ;;
    esac
done

# ------------------------------------------------------------------------------
# 1. TỰ ĐỘNG ĐỒNG BỘ /tmp/private_chains.json TỪ inventory.yml CỦA ANSIBLE PRIVATE CHAINS
# ------------------------------------------------------------------------------
PRIV_INVENTORY=""
for p in "$METANODE_DIR/deploy/ansible_private_chains/inventory.yml" \
         "$SUITE_DIR/../metanode/deploy/ansible_private_chains/inventory.yml" \
         "/opt/metanode/deploy/ansible_private_chains/inventory.yml"; do
    if [ -f "$p" ]; then
        PRIV_INVENTORY="$p"
        break
    fi
done

if [ -n "$PRIV_INVENTORY" ]; then
    python3 -c "
import os, json, yaml

inv_path = '$PRIV_INVENTORY'
try:
    with open(inv_path) as f:
        data = yaml.safe_load(f)
    global_vars = data.get('all', {}).get('vars', {}) or {}
    root_rpc = global_vars.get('root_anchor_rpc', 'http://127.0.0.1:10746')
    hosts = data.get('all', {}).get('children', {}).get('private_chains', {}).get('hosts', {}) or {}

    out_simple = {
        'root_anchor': root_rpc,
        'nodes': {},
        'tcp_nodes': {},
        'chain_nodes': {}
    }

    for host_key, h in sorted(hosts.items()):
        if not isinstance(h, dict) or 'chain_id' not in h:
            continue
        cid = int(h['chain_id'])
        cid_str = str(cid)
        ip = h.get('ansible_host', '127.0.0.1')
        rpc_port = int(h.get('rpc_port', 8546))
        p_offset = int(h.get('port_offset', 10))
        num_vals = int(h.get('validators', 1))

        out_simple['nodes'][cid_str] = f'http://{ip}:{rpc_port}'
        out_simple['tcp_nodes'][cid_str] = f'{ip}:{4200 + p_offset}'

        c_rpc_nodes = {}
        c_tcp_nodes = {}
        for v in range(num_vals):
            c_rpc_nodes[f'm{v}'] = f'http://{ip}:{rpc_port + v}'
            c_tcp_nodes[f'm{v}'] = f'{ip}:{4200 + p_offset + v}'

        out_simple['chain_nodes'][cid_str] = {
            'validators': num_vals,
            'rpc_url': f'http://{ip}:{rpc_port}',
            'rpc_nodes': c_rpc_nodes,
            'tcp_nodes': c_tcp_nodes
        }

    with open('$PRIV_CHAINS_FILE', 'w') as f:
        json.dump(out_simple, f, indent=2)
    print('✅ Đã đồng bộ $PRIV_CHAINS_FILE từ inventory.yml')
except Exception as e:
    print('⚠️ Cảnh báo khi đồng bộ inventory.yml:', e)
"
fi

# ------------------------------------------------------------------------------
# 2. XÁC ĐỊNH CHẾ ĐỘ CHẠY: PUBLIC CHAIN HAY PRIVATE CHAIN
# ------------------------------------------------------------------------------
USE_PRIVATE_CHAIN=false
TARGET_CHAIN_ID=""
TARGET_CHAIN_NAME=""

if [ -n "$TARGET_CHAIN_ARG" ] && [ "$TARGET_CHAIN_ARG" != "public" ] && [ "$TARGET_CHAIN_ARG" != "root" ] && [ "$TARGET_CHAIN_ARG" != "991" ] && [ "$TARGET_CHAIN_ARG" != "default" ]; then
    USE_PRIVATE_CHAIN=true
elif [ ! -f "$RPC_NODES_FILE" ] && [ -f "$PRIV_CHAINS_FILE" ]; then
    # Không có Public chain, tự động chọn Private chain đầu tiên
    FIRST_CID=$(python3 -c "
import json
with open('$PRIV_CHAINS_FILE') as f:
    d = json.load(f)
nodes = d.get('nodes', {})
if nodes:
    print(list(nodes.keys())[0])
" 2>/dev/null || true)
    if [ -n "$FIRST_CID" ]; then
        USE_PRIVATE_CHAIN=true
        TARGET_CHAIN_ARG="$FIRST_CID"
        echo "ℹ️  Không tìm thấy $RPC_NODES_FILE, tự động chọn Private Chain $FIRST_CID"
    fi
fi

if [ "$USE_PRIVATE_CHAIN" = true ]; then
    echo "=========================================================="
    echo "🔗 ĐANG CẤU HÌNH TEST SUITE CHO PRIVATE CHAIN: $TARGET_CHAIN_ARG"
    echo "=========================================================="

    if [ ! -f "$PRIV_CHAINS_FILE" ]; then
        echo "Error: $PRIV_CHAINS_FILE not found." >&2
        exit 1
    fi

    # Trích xuất thông tin node cho Private Chain được chọn
    EVAL_INFO=$(python3 -c "
import json, sys

with open('$PRIV_CHAINS_FILE') as f:
    p_data = json.load(f)

nodes = p_data.get('nodes', {})
chain_nodes = p_data.get('chain_nodes', {})
target = '$TARGET_CHAIN_ARG'.strip().lower()

name_map = {'101': 'chain_a', '102': 'chain_b', '103': 'chain_c', '104': 'chain_d'}

matched_cid = None
for cid_str in nodes.keys():
    c_name = name_map.get(cid_str, f'chain_{cid_str}')
    if target == cid_str or target == c_name or target == f'chain_{cid_str}':
        matched_cid = cid_str
        break

if not matched_cid:
    print(f'Error: Không tìm thấy private chain \"$TARGET_CHAIN_ARG\" trong $PRIV_CHAINS_FILE', file=sys.stderr)
    print(f'Các chain khả dụng: {list(nodes.keys())}', file=sys.stderr)
    sys.exit(1)

c_info = chain_nodes.get(matched_cid, {})
rpc_map = c_info.get('rpc_nodes', {})
tcp_map = c_info.get('tcp_nodes', {})
c_name = name_map.get(matched_cid, f'chain_{matched_cid}')
base_rpc = nodes.get(matched_cid, '')

res = {
    'cid': int(matched_cid),
    'name': c_name,
    'rpc_url': base_rpc,
    'rpc_nodes': rpc_map,
    'tcp_nodes': tcp_map
}
print(json.dumps(res))
")

    TARGET_CID=$(echo "$EVAL_INFO" | jq -r '.cid')
    TARGET_NAME=$(echo "$EVAL_INFO" | jq -r '.name')
    TARGET_RPC_URL=$(echo "$EVAL_INFO" | jq -r '.rpc_url')
    
    # Lấy các node m0..m4
    P_M0_RPC=$(echo "$EVAL_INFO" | jq -r '(.rpc_nodes.m0 // .rpc_url // "")')
    P_M1_RPC=$(echo "$EVAL_INFO" | jq -r '(.rpc_nodes.m1 // "")')
    P_M2_RPC=$(echo "$EVAL_INFO" | jq -r '(.rpc_nodes.m2 // "")')
    P_M3_RPC=$(echo "$EVAL_INFO" | jq -r '(.rpc_nodes.m3 // "")')
    P_M4_RPC=$(echo "$EVAL_INFO" | jq -r '(.rpc_nodes.m4 // "")')

    P_M0_RPC_CLEAN=$(echo "$P_M0_RPC" | sed -E 's#^https?://##')
    P_M1_RPC_CLEAN=$(echo "$P_M1_RPC" | sed -E 's#^https?://##')
    P_M2_RPC_CLEAN=$(echo "$P_M2_RPC" | sed -E 's#^https?://##')
    P_M3_RPC_CLEAN=$(echo "$P_M3_RPC" | sed -E 's#^https?://##')

    P_M0_TCP=$(echo "$EVAL_INFO" | jq -r '(.tcp_nodes.m0 // "")')
    P_M1_TCP=$(echo "$EVAL_INFO" | jq -r '(.tcp_nodes.m1 // "")')
    P_M2_TCP=$(echo "$EVAL_INFO" | jq -r '(.tcp_nodes.m2 // "")')
    P_M3_TCP=$(echo "$EVAL_INFO" | jq -r '(.tcp_nodes.m3 // "")')
    P_M4_TCP=$(echo "$EVAL_INFO" | jq -r '(.tcp_nodes.m4 // "")')

    echo "   - Chain ID: $TARGET_CID ($TARGET_NAME)"
    echo "   - RPC m0:   $P_M0_RPC (TCP: $P_M0_TCP)"
    echo "   - RPC m1:   $P_M1_RPC (TCP: $P_M1_TCP)"
    echo "   - RPC m2:   $P_M2_RPC (TCP: $P_M2_TCP)"
    echo "   - RPC m3:   $P_M3_RPC (TCP: $P_M3_TCP)"

    # 1. Update tps_blast_cc
    FILE1="$SUITE_DIR/test_tps/tps_blast_cc/config-multi.json"
    if [ -f "$FILE1" ]; then
        echo "Updating $FILE1 using Private Chain $TARGET_CID..."
        jq --arg p "$P_M0_TCP" --arg r0 "$P_M0_RPC_CLEAN" \
           --arg c1 "$P_M1_TCP" --arg r1 "$P_M1_RPC_CLEAN" \
           --arg c2 "$P_M2_TCP" --arg r2 "$P_M2_RPC_CLEAN" \
           --arg c3 "$P_M3_TCP" --arg r3 "$P_M3_RPC_CLEAN" \
           --argjson cid "$TARGET_CID" \
           '.chain_id = $cid | .parent_connection_address = $p | .rpc_0 = $r0 | .connection_node_1 = $c1 | .rpc_1 = $r1 | .connection_node_2 = $c2 | .rpc_2 = $r2 | .connection_node_3 = $c3 | .rpc_3 = $r3 | with_entries(select(.value != ""))' \
           "$FILE1" > "${FILE1}.tmp" && mv "${FILE1}.tmp" "$FILE1"
    fi

    # 2. Update test-history
    FILE2="$SUITE_DIR/test-simple/test-rpc/test-history/config-mutil.json"
    if [ -f "$FILE2" ]; then
        echo "Updating $FILE2 using Private Chain $TARGET_CID..."
        jq --arg r "$P_M0_RPC" \
           --arg u0 "$P_M0_RPC" --arg u1 "$P_M1_RPC" --arg u2 "$P_M2_RPC" --arg u3 "$P_M3_RPC" --arg u4 "$P_M4_RPC" \
           --argjson cid "$TARGET_CID" \
           '.chain_id = $cid | .rpc_url = $r | .rpc_urls = [$u0, $u1, $u2, $u3, $u4] | .rpc_urls |= map(select(. != ""))' \
           "$FILE2" > "${FILE2}.tmp" && mv "${FILE2}.tmp" "$FILE2"
    fi

    # 3. Update block_hash_checker
    FILE3="$SUITE_DIR/block/block_hash_checker/config-m-nodes.json"
    if [ -f "$FILE3" ]; then
        echo "Updating $FILE3 using Private Chain $TARGET_CID..."
        jq --arg m0 "$P_M0_RPC" --arg m1 "$P_M1_RPC" --arg m2 "$P_M2_RPC" --arg m3 "$P_M3_RPC" --arg m4 "$P_M4_RPC" \
           '.nodes = {m4: $m4, m3: $m3, m2: $m2, m1: $m1, m0: $m0} | .nodes |= with_entries(select(.value != ""))' \
           "$FILE3" > "${FILE3}.tmp" && mv "${FILE3}.tmp" "$FILE3"
    fi

    # 4. Update spam_xapian
    FILE4="$SUITE_DIR/test-simple/test-rpc/spam_xapian/config-m-node.json"
    if [ -f "$FILE4" ]; then
        echo "Updating $FILE4 using Private Chain $TARGET_CID..."
        jq --arg r "$P_M0_RPC" \
           --arg u0 "$P_M0_RPC" --arg u1 "$P_M1_RPC" --arg u2 "$P_M2_RPC" --arg u3 "$P_M3_RPC" --arg u4 "$P_M4_RPC" \
           --argjson cid "$TARGET_CID" \
           '.chain_id = $cid | .rpc_url = $r | .rpc_urls = [$u0, $u1, $u2, $u3, $u4] | .rpc_urls |= map(select(. != ""))' \
           "$FILE4" > "${FILE4}.tmp" && mv "${FILE4}.tmp" "$FILE4"
    fi

    # 5. Update register_bls
    FILE5="$SUITE_DIR/register_bls/tcp/config.json"
    if [ -f "$FILE5" ]; then
        echo "Updating $FILE5 using Private Chain $TARGET_CID..."
        jq --arg p0 "$P_M0_TCP" --arg p1 "$P_M1_TCP" --arg p2 "$P_M2_TCP" --arg p3 "$P_M3_TCP" --arg p4 "$P_M4_TCP" \
           --arg r0 "$P_M0_RPC" --arg r1 "$P_M1_RPC" --arg r2 "$P_M2_RPC" --arg r3 "$P_M3_RPC" --arg r4 "$P_M4_RPC" \
           '.parent_connection_address = [$p0, $p1, $p2, $p3, $p4] | .parent_connection_address |= map(select(. != "")) | .rpc_endpoints = [$r0, $r1, $r2, $r3, $r4] | .rpc_endpoints |= map(select(. != ""))' \
           "$FILE5" > "${FILE5}.tmp" && mv "${FILE5}.tmp" "$FILE5"
    fi

    # 6. Update test-blockstm
    FILE6="$SUITE_DIR/test-simple/test-rpc/test-blockstm/config.json"
    if [ -f "$FILE6" ]; then
        echo "Updating $FILE6 using Private Chain $TARGET_CID..."
        python3 -c "
import json
with open('$FILE6') as f:
    cfg = json.load(f)
with open('$PRIV_CHAINS_FILE') as f:
    p_data = json.load(f)

cid_str = '$TARGET_CID'
chain_nodes = p_data.get('chain_nodes', {})
nodes = p_data.get('nodes', {})
c_info = chain_nodes.get(cid_str, {})
rpc_nodes_map = c_info.get('rpc_nodes', {'m0': '$P_M0_RPC'})

cfg['target_chain'] = '$TARGET_NAME'
cfg['chain_id'] = int(cid_str)
cfg['rpc_url'] = '$P_M0_RPC'
cfg['rpc_nodes'] = rpc_nodes_map

name_map = {'101': 'chain_a', '102': 'chain_b', '103': 'chain_c', '104': 'chain_d'}
if 'private_chains' not in cfg:
    cfg['private_chains'] = {}

for cid, rpc_url in nodes.items():
    c_name = name_map.get(str(cid), f'chain_{cid}')
    cur_info = chain_nodes.get(str(cid), {})
    cur_rpc_map = cur_info.get('rpc_nodes', {'m0': rpc_url})

    if c_name in cfg['private_chains']:
        cfg['private_chains'][c_name]['chain_id'] = int(cid)
        cfg['private_chains'][c_name]['rpc_url'] = rpc_url
        cfg['private_chains'][c_name]['rpc_nodes'] = cur_rpc_map
    else:
        cfg['private_chains'][c_name] = {
            'chain_id': int(cid),
            'rpc_url': rpc_url,
            'rpc_nodes': cur_rpc_map,
            'private_keys': []
        }

with open('$FILE6', 'w') as f:
    json.dump(cfg, f, indent=2)
print('✅ Updated private_chains and target_chain in $FILE6')
"
    fi

    # 7. Update tps_contract
    FILE7="$SUITE_DIR/test_tps/tps_contract/config-multi.json"
    if [ -f "$FILE7" ]; then
        echo "Updating $FILE7 using Private Chain $TARGET_CID..."
        jq --arg p "$P_M0_TCP" --arg r0 "$P_M0_RPC_CLEAN" \
           --arg c1 "$P_M1_TCP" --arg r1 "$P_M1_RPC_CLEAN" \
           --arg c2 "$P_M2_TCP" --arg r2 "$P_M2_RPC_CLEAN" \
           --arg c3 "$P_M3_TCP" --arg r3 "$P_M3_RPC_CLEAN" \
           --argjson cid "$TARGET_CID" \
           '.chain_id = $cid | .parent_connection_address = $p | .rpc_0 = $r0 | .connection_node_1 = $c1 | .rpc_1 = $r1 | .connection_node_2 = $c2 | .rpc_2 = $r2 | .connection_node_3 = $c3 | .rpc_3 = $r3 | with_entries(select(.value != ""))' \
           "$FILE7" > "${FILE7}.tmp" && mv "${FILE7}.tmp" "$FILE7"
    fi

    # 8. Update tps_contract_parallel
    FILE8="$SUITE_DIR/test_tps/tps_contract_parallel/config-multi.json"
    if [ -f "$FILE8" ]; then
        echo "Updating $FILE8 using Private Chain $TARGET_CID..."
        jq --arg p "$P_M0_TCP" --arg r0 "$P_M0_RPC_CLEAN" \
           --arg c1 "$P_M1_TCP" --arg r1 "$P_M1_RPC_CLEAN" \
           --arg c2 "$P_M2_TCP" --arg r2 "$P_M2_RPC_CLEAN" \
           --arg c3 "$P_M3_TCP" --arg r3 "$P_M3_RPC_CLEAN" \
           --argjson cid "$TARGET_CID" \
           '.chain_id = $cid | .parent_connection_address = $p | .rpc_0 = $r0 | .connection_node_1 = $c1 | .rpc_1 = $r1 | .connection_node_2 = $c2 | .rpc_2 = $r2 | .connection_node_3 = $c3 | .rpc_3 = $r3 | with_entries(select(.value != ""))' \
           "$FILE8" > "${FILE8}.tmp" && mv "${FILE8}.tmp" "$FILE8"
    fi

else
    # --------------------------------------------------------------------------
    # CHẾ ĐỘ PUBLIC CHAIN (CHAIN 991 - ROOT ANCHOR)
    # --------------------------------------------------------------------------
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
           '.chain_id = 991 | .parent_connection_address = $p | .rpc_0 = $r0 | .connection_node_1 = $c1 | .rpc_1 = $r1 | .connection_node_2 = $c2 | .rpc_2 = $r2 | .connection_node_3 = $c3 | .rpc_3 = $r3 | with_entries(select(.value != ""))' \
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
           '.chain_id = 991 | .rpc_url = $r | .rpc_urls = [$u0, $u1, $u2, $u3, $u4] | .rpc_urls |= map(select(. != ""))' \
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
           '.chain_id = 991 | .rpc_url = $r | .rpc_urls = [$rpc[0].nodes | to_entries[] | select($rpc[0].roles[.key] != "synconly") | .value]' \
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
           'del(.all_nodes, .roles) | .target_chain = "" | .chain_id = 991 | .rpc_url = (if $r != "" then $r else .rpc_url end) | .rpc_nodes = ($rpc[0].nodes | with_entries(select($rpc[0].roles[.key] != "synconly"))) | .sync_nodes = ($rpc[0].nodes | with_entries(select($rpc[0].roles[.key] == "synconly")))' \
           "$FILE6" > "${FILE6}.tmp" && mv "${FILE6}.tmp" "$FILE6"

        # Cập nhật thông tin Private Chains từ /tmp/private_chains.json (nếu có)
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
        cfg['private_chains'][c_name]['chain_id'] = int(cid)
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
           '.chain_id = 991 | .parent_connection_address = $p | .rpc_0 = $r0 | .connection_node_1 = $c1 | .rpc_1 = $r1 | .connection_node_2 = $c2 | .rpc_2 = $r2 | .connection_node_3 = $c3 | .rpc_3 = $r3 | with_entries(select(.value != ""))' \
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
           '.chain_id = 991 | .parent_connection_address = $p | .rpc_0 = $r0 | .connection_node_1 = $c1 | .rpc_1 = $r1 | .connection_node_2 = $c2 | .rpc_2 = $r2 | .connection_node_3 = $c3 | .rpc_3 = $r3 | with_entries(select(.value != ""))' \
           "$FILE8" > "${FILE8}.tmp" && mv "${FILE8}.tmp" "$FILE8"
    else
        echo "Warning: $FILE8 not found." >&2
    fi
fi

chmod +x "$0" 2>/dev/null || true
