#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUITE_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
METANODE_DIR="$(cd "$SUITE_DIR/../metanode" 2>/dev/null && pwd || echo "/home/abc/nhat/con-chain-v2/metanode")"

RPC_NODES_FILE="/tmp/rpc_nodes.json"

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
# 1. CHỌN FILE ENDPOINT ĐÃ ĐƯỢC SCRIPT DEPLOY CẬP NHẬT
# ------------------------------------------------------------------------------
CLUSTER_INVENTORY=""
PARSE_INV_SCRIPT=""
for p in "$METANODE_DIR/deploy/ansible_clusters/inventory.yml" \
         "$SUITE_DIR/../metanode/deploy/ansible_clusters/inventory.yml" \
         "/opt/metanode/deploy/ansible_clusters/inventory.yml"; do
    if [ -f "$p" ]; then
        CLUSTER_INVENTORY="$p"
        break
    fi
done

for s in "$METANODE_DIR/deploy/ansible_clusters/scripts/parse_inventory.py" \
         "$SUITE_DIR/../metanode/deploy/ansible_clusters/scripts/parse_inventory.py" \
         "/opt/metanode/deploy/ansible_clusters/scripts/parse_inventory.py"; do
    if [ -f "$s" ]; then
        PARSE_INV_SCRIPT="$s"
        break
    fi
done

# Các script deploy mới tự gộp public chain + execution clusters vào
# /tmp/rpc_nodes.json. Chỉ export từ inventory như fallback khi file chung chưa có.
if [ ! -f "$RPC_NODES_FILE" ] && [ -n "$CLUSTER_INVENTORY" ] && [ -n "$PARSE_INV_SCRIPT" ]; then
    python3 "$PARSE_INV_SCRIPT" "$CLUSTER_INVENTORY" export >/dev/null 2>&1 || true
    echo "✅ Đã tạo endpoint fallback từ ansible_clusters/inventory.yml"
fi

CHAIN_ENDPOINTS_FILE="$RPC_NODES_FILE"

# ------------------------------------------------------------------------------
# 2. XÁC ĐỊNH CHẾ ĐỘ CHẠY: PUBLIC CHAIN HAY PRIVATE CHAIN
# ------------------------------------------------------------------------------
USE_PRIVATE_CHAIN=false
TARGET_CHAIN_ID=""
TARGET_CHAIN_NAME=""

if [ -n "$TARGET_CHAIN_ARG" ] && [ "$TARGET_CHAIN_ARG" != "public" ] && [ "$TARGET_CHAIN_ARG" != "root" ] && [ "$TARGET_CHAIN_ARG" != "parent" ] && [ "$TARGET_CHAIN_ARG" != "default" ]; then
    USE_PRIVATE_CHAIN=true
elif [ ! -f "$RPC_NODES_FILE" ]; then
    # Không có Public chain, tự động chọn Private chain đầu tiên
    FIRST_CID=$(python3 -c "
import json
with open('$CHAIN_ENDPOINTS_FILE') as f:
    d = json.load(f)
chains = d.get('private_chains') or d.get('chain_nodes') or d.get('nodes', {})
if chains:
    print(list(chains.keys())[0])
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

    if [ ! -f "$CHAIN_ENDPOINTS_FILE" ]; then
        echo "Error: $RPC_NODES_FILE does not contain private-chain endpoints." >&2
        exit 1
    fi

    # Trích xuất thông tin node cho Private Chain được chọn
    EVAL_INFO=$(python3 -c "
import json, sys

with open('$CHAIN_ENDPOINTS_FILE') as f:
    p_data = json.load(f)

p_chains = p_data.get('private_chains') or p_data.get('chain_nodes', {})
target = '$TARGET_CHAIN_ARG'.strip().lower()

cluster_map = {
    '1': 'chain_a', 'cluster_1': 'chain_a', 'exec1': 'chain_a', '101': 'chain_a', '991': 'chain_a', 'chain_a': 'chain_a',
    '2': 'chain_b', 'cluster_2': 'chain_b', 'exec2': 'chain_b', '102': 'chain_b', 'chain_b': 'chain_b'
}

canonical = cluster_map.get(target, target)
c_info = p_chains.get(canonical) or p_chains.get(target, {})

if not c_info:
    print(f'Error: Không tìm thấy private chain \"$TARGET_CHAIN_ARG\" trong $CHAIN_ENDPOINTS_FILE', file=sys.stderr)
    print(f'Các chain khả dụng: {list(p_chains.keys())}', file=sys.stderr)
    sys.exit(1)

c_name = canonical
rpc_map = c_info.get('rpc_nodes', {})
tcp_map = c_info.get('tcp_nodes', {})
base_rpc = c_info.get('rpc_url', '')
chain_id_val = int(c_info.get('chain_id', 991))

res = {
    'cid': chain_id_val,
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

    # (Các bài test TPS tps_blast_cc, tps_contract, tps_contract_parallel được đồng bộ dùng chung FILE6 ở cuối script)

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


    # 6. Update configs/config.json (Unified Single Source of Truth for RPC & TPS)
    FILE6="$SUITE_DIR/configs/config.json"
    if [ ! -f "$FILE6" ]; then
        FILE6="$SUITE_DIR/test-simple/test-rpc/test-chain/config.json"
    fi
    if [ -f "$FILE6" ]; then
        echo "Updating $FILE6 using Private Chain $TARGET_CID (Unified RPC & TCP)..."
        python3 - "$FILE6" "$CHAIN_ENDPOINTS_FILE" "$TARGET_CID" "$TARGET_NAME" "$P_M0_RPC" "$P_M0_TCP" "$P_M1_TCP" "$P_M1_RPC_CLEAN" "$P_M2_TCP" "$P_M2_RPC_CLEAN" "$P_M3_TCP" "$P_M3_RPC_CLEAN" << 'EOF'
import sys, json

file6 = sys.argv[1]
priv_file = sys.argv[2]
target_cid = sys.argv[3]
target_name = sys.argv[4]
p_m0_rpc = sys.argv[5]
p_m0_tcp = sys.argv[6]
p_m1_tcp = sys.argv[7]
p_m1_rpc_clean = sys.argv[8]
p_m2_tcp = sys.argv[9]
p_m2_rpc_clean = sys.argv[10]
p_m3_tcp = sys.argv[11]
p_m3_rpc_clean = sys.argv[12]

with open(file6) as f:
    cfg = json.load(f)
with open(priv_file) as f:
    p_data = json.load(f)

chain_nodes = p_data.get('chain_nodes', {})
c_info = chain_nodes.get(target_cid, {})
rpc_nodes_map = c_info.get('rpc_nodes', {'m0': p_m0_rpc})
tcp_nodes_map = c_info.get('tcp_nodes', {'m0': p_m0_tcp})

# Khi cấu hình Private Chain: chỉ cập nhật target_chain và private_chains, TUYỆT ĐỐI không đè cấu hình root của Parent Chain
cfg['target_chain'] = target_name


p_clusters = p_data.get('private_chains') or p_data.get('clusters') or chain_nodes or {}

if 'private_chains' not in cfg:
    cfg['private_chains'] = {}

for cid_k, c_val in p_clusters.items():
    c_cid = int(c_val.get('chain_id', 991))
    c_rpc = c_val.get('primary_rpc', c_val.get('rpc_url', ''))
    c_ws = c_val.get('ws_url', c_rpc.replace('http://', 'ws://').replace('https://', 'wss://') + '/ws')
    c_replicas = c_val.get('replicas', {})
    if c_replicas:
        c_rpc_map = {f"m{r.get('index', idx)}": r.get('rpc_url', '') for idx, r in enumerate(c_replicas.values())}
        c_ws_map = {f"m{r.get('index', idx)}": r.get('ws_url', '') for idx, r in enumerate(c_replicas.values())}
        c_tcp_map = {f"m{r.get('index', idx)}": f"{r.get('ip', '127.0.0.1')}:{r.get('p2p_port', 4200)}" for idx, r in enumerate(c_replicas.values())}
    else:
        c_rpc_map = c_val.get('rpc_nodes', {'m0': c_rpc})
        c_ws_map = c_val.get('ws_nodes', {'m0': c_ws})
        c_tcp_map = c_val.get('tcp_nodes', {'m0': '127.0.0.1:4200'})

    aliases = []
    if str(cid_k) in ['1', 'chain_a', 'exec1']:
        aliases = ['chain_a', 'exec1']
    elif str(cid_k) in ['2', 'chain_b', 'exec2']:
        aliases = ['chain_b', 'exec2']
    else:
        aliases = [c_val.get('cluster_name', f'exec{cid_k}')]

    for a_name in aliases:
        prev_keys = cfg.get('private_chains', {}).get(a_name, {}).get('private_keys', [])
        if not prev_keys and a_name == 'exec1':
            prev_keys = cfg.get('private_chains', {}).get('chain_a', {}).get('private_keys', [])
        elif not prev_keys and a_name == 'exec2':
            prev_keys = cfg.get('private_chains', {}).get('chain_b', {}).get('private_keys', [])

        prev_bls_pk = cfg.get('private_chains', {}).get(a_name, {}).get('bls_private_key', '')
        if not prev_bls_pk and a_name == 'exec1':
            prev_bls_pk = cfg.get('private_chains', {}).get('chain_a', {}).get('bls_private_key', '')
        elif not prev_bls_pk and a_name == 'exec2':
            prev_bls_pk = cfg.get('private_chains', {}).get('chain_b', {}).get('bls_private_key', '')
        c_bls_pk = c_val.get('bls_private_key', prev_bls_pk or c_val.get('private_key', ''))

        prev_addr = cfg.get('private_chains', {}).get(a_name, {}).get('address', '')
        if not prev_addr and a_name == 'exec1':
            prev_addr = cfg.get('private_chains', {}).get('chain_a', {}).get('address', '')
        elif not prev_addr and a_name == 'exec2':
            prev_addr = cfg.get('private_chains', {}).get('chain_b', {}).get('address', '')
        c_addr = c_val.get('address', prev_addr)

        prev_pub = cfg.get('private_chains', {}).get(a_name, {}).get('bls_pubkey', '')
        if not prev_pub and a_name == 'exec1':
            prev_pub = cfg.get('private_chains', {}).get('chain_a', {}).get('bls_pubkey', '')
        elif not prev_pub and a_name == 'exec2':
            prev_pub = cfg.get('private_chains', {}).get('chain_b', {}).get('bls_pubkey', '')
        c_bls_pub = c_val.get('bls_pubkey', prev_pub)

        cfg['private_chains'][a_name] = {
            'chain_id': c_cid,
            'bls_private_key': c_bls_pk,
            'address': c_addr,
            'bls_pubkey': c_bls_pub,
            'rpc_url': c_rpc,
            'ws_url': c_ws,
            'rpc_nodes': c_rpc_map,
            'ws_nodes': c_ws_map,
            'tcp_nodes': c_tcp_map,
            'private_keys': prev_keys
        }

for bad in ['chain_chain_a', 'chain_chain_b', 'chain_991', 'chain_cluster_1', 'chain_cluster_2', 'chain_1', 'chain_2', 'chain_101', 'chain_102']:
    cfg.get('private_chains', {}).pop(bad, None)

# Sanitize root node maps: ensure parent_node_* and exec* never pollute root endpoints
for map_key in ['rpc_nodes', 'tcp_nodes', 'ws_nodes', 'state_history_nodes', 'sync_nodes']:
    if map_key in cfg and isinstance(cfg[map_key], dict):
        cfg[map_key] = {k: v for k, v in cfg[map_key].items() if not k.startswith('parent_node_') and not k.startswith('exec')}

with open(file6, 'w') as f:
    json.dump(cfg, f, indent=2)
print(f'✅ Updated private_chains and target_chain in {file6}')
EOF
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

    # (Các bài test TPS tps_blast_cc, tps_contract, tps_contract_parallel được đồng bộ dùng chung FILE6 ở cuối script)

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


    # 6. Update configs/config.json - Unified Single Source of Truth
    FILE6="$SUITE_DIR/configs/config.json"
    if [ ! -f "$FILE6" ]; then
        FILE6="$SUITE_DIR/test-simple/test-rpc/test-chain/config.json"
    fi
    if [ -f "$FILE6" ]; then
        echo "Updating $FILE6 using RPC & TCP Nodes (Unified Single Source of Truth)..."
        
        new_rpc_url=$(jq -r '((.nodes.m0 // (.nodes | to_entries[0].value // "")) // "")' "$RPC_NODES_FILE")
        new_ws_url=$(jq -r '((.ws_nodes.m0 // (.ws_nodes | to_entries[0].value // "")) // "")' "$RPC_NODES_FILE")
        new_parent=$(jq -r '((.tcp_nodes.m0 // (.tcp_nodes | to_entries[0].value // "")) // "")' "$RPC_NODES_FILE")
        new_rpc_0=$(jq -r '((.nodes.m0 // (.nodes | to_entries[0].value // "")) // "") | sub("^https?://"; "")' "$RPC_NODES_FILE")

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

        jq --arg r "$new_rpc_url" \
           --arg ws "$new_ws_url" \
           --arg p "$new_parent" --arg r0 "$new_rpc_0" \
           --arg c1 "$new_conn_1" --arg r1 "$new_rpc_1" \
           --arg c2 "$new_conn_2" --arg r2 "$new_rpc_2" \
           --arg c3 "$new_conn_3" --arg r3 "$new_rpc_3" \
           --slurpfile rpc "$RPC_NODES_FILE" \
           'del(.all_nodes, .roles, .tcp_url) | .target_chain = "" | .chain_id = 991 | .rpc_url = (if $r != "" then $r else .rpc_url end) | .ws_url = (if $ws != "" then $ws else (.rpc_url | sub("^http://"; "ws://") | sub("^https://"; "wss://") + "/ws") end) | .state_history_nodes = (($rpc[0].state_history_nodes // ($rpc[0].rpc_nodes // {})) | with_entries(select(.key | test("^m[0-9]+$")))) | .rpc_nodes = (if ($rpc[0].rpc_nodes != null and ($rpc[0].rpc_nodes | length > 0)) then ($rpc[0].rpc_nodes | with_entries(select(.key | test("^m[0-9]+$")))) else ($rpc[0].nodes | with_entries(select($rpc[0].roles[.key] != "synconly" and (.key | test("^m[0-9]+$"))))) end) | .ws_nodes = (if ($rpc[0].ws_nodes != null and ($rpc[0].ws_nodes | length > 0)) then ($rpc[0].ws_nodes | with_entries(select(.key | test("^m[0-9]+$")))) else (.rpc_nodes | with_entries(.value |= (sub("^http://"; "ws://") | sub("^https://"; "wss://") + "/ws"))) end) | .tcp_nodes = (($rpc[0].tcp_nodes // {}) | with_entries(select(.key | test("^m[0-9]+$")))) | .tcp_node = (if $p != "" then $p else (.tcp_nodes.m0 // (.tcp_nodes | to_entries[0].value // .tcp_node // "")) end) | .sync_nodes = ($rpc[0].nodes | with_entries(select($rpc[0].roles[.key] == "synconly" and (.key | test("^m[0-9]+$"))))) | .parent_connection_address = $p | .rpc_0 = $r0 | (if $c1 != "" then .connection_node_1 = $c1 else del(.connection_node_1) end) | (if $r1 != "" then .rpc_1 = $r1 else del(.rpc_1) end) | (if $c2 != "" then .connection_node_2 = $c2 else del(.connection_node_2) end) | (if $r2 != "" then .rpc_2 = $r2 else del(.rpc_2) end) | (if $c3 != "" then .connection_node_3 = $c3 else del(.connection_node_3) end) | (if $r3 != "" then .rpc_3 = $r3 else del(.rpc_3) end) | .parent_address = (.parent_address // "0xac1137f94f0a4cf8fdc0f4fb6f69a8be98032041") | .parent_connection_type = "client" | .version = "0.0.1.0" | .private_key = (if .private_key != "" and .private_key != null then .private_key else "2b3aa0f620d2d73c046cd93eb64f2eb687a95b22e278500aa251c8c9dda1203b" end)' \
           "$FILE6" > "${FILE6}.tmp" && mv "${FILE6}.tmp" "$FILE6"

        if [ -f "$SUITE_DIR/test-simple/test-rpc/test-chain/config.json" ] && [ ! -L "$SUITE_DIR/test-simple/test-rpc/test-chain/config.json" ]; then
            cp -f "$FILE6" "$SUITE_DIR/test-simple/test-rpc/test-chain/config.json"
        fi

        # Cập nhật thông tin Private Chains từ /tmp/rpc_nodes.json (nếu có)
        if [ -f "$CHAIN_ENDPOINTS_FILE" ]; then
            echo "Updating Private Chains RPC & TCP in $FILE6 from $CHAIN_ENDPOINTS_FILE..."
            python3 - "$FILE6" "$CHAIN_ENDPOINTS_FILE" << 'EOF'
import sys, json

file6 = sys.argv[1]
priv_file = sys.argv[2]

with open(file6) as f:
    cfg = json.load(f)
with open(priv_file) as f:
    p_data = json.load(f)

p_clusters = p_data.get('private_chains') or p_data.get('clusters', {})
if 'private_chains' not in cfg:
    cfg['private_chains'] = {}

for cid_k, c_val in p_clusters.items():
    c_cid = int(c_val.get('chain_id', 991))
    c_rpc = c_val.get('primary_rpc', c_val.get('rpc_url', ''))
    c_ws = c_val.get('ws_url', c_rpc.replace('http://', 'ws://') + '/ws')
    c_replicas = c_val.get('replicas', {})
    if c_replicas:
        c_rpc_map = {f"m{r.get('index', idx)}": r.get('rpc_url', '') for idx, r in enumerate(c_replicas.values())}
        c_ws_map = {f"m{r.get('index', idx)}": r.get('ws_url', '') for idx, r in enumerate(c_replicas.values())}
        c_tcp_map = {f"m{r.get('index', idx)}": f"{r.get('ip', '127.0.0.1')}:{r.get('p2p_port', 4200)}" for idx, r in enumerate(c_replicas.values())}
    else:
        c_rpc_map = c_val.get('rpc_nodes', {'m0': c_rpc})
        c_ws_map = c_val.get('ws_nodes', {'m0': c_ws})
        c_tcp_map = c_val.get('tcp_nodes', {'m0': '127.0.0.1:4200'})

    aliases = []
    if str(cid_k) == '1':
        aliases = ['chain_a', 'exec1']
    elif str(cid_k) == '2':
        aliases = ['chain_b', 'exec2']
    else:
        aliases = [c_val.get('cluster_name', f'exec{cid_k}')]

    for a_name in aliases:
        prev_keys = cfg.get('private_chains', {}).get(a_name, {}).get('private_keys', [])
        if not prev_keys and a_name == 'exec1':
            prev_keys = cfg.get('private_chains', {}).get('chain_a', {}).get('private_keys', [])
        elif not prev_keys and a_name == 'exec2':
            prev_keys = cfg.get('private_chains', {}).get('chain_b', {}).get('private_keys', [])

        prev_bls_pk = cfg.get('private_chains', {}).get(a_name, {}).get('bls_private_key', '')
        if not prev_bls_pk and a_name == 'exec1':
            prev_bls_pk = cfg.get('private_chains', {}).get('chain_a', {}).get('bls_private_key', '')
        elif not prev_bls_pk and a_name == 'exec2':
            prev_bls_pk = cfg.get('private_chains', {}).get('chain_b', {}).get('bls_private_key', '')
        c_bls_pk = c_val.get('bls_private_key', prev_bls_pk or c_val.get('private_key', ''))

        prev_addr = cfg.get('private_chains', {}).get(a_name, {}).get('address', '')
        if not prev_addr and a_name == 'exec1':
            prev_addr = cfg.get('private_chains', {}).get('chain_a', {}).get('address', '')
        elif not prev_addr and a_name == 'exec2':
            prev_addr = cfg.get('private_chains', {}).get('chain_b', {}).get('address', '')
        c_addr = c_val.get('address', prev_addr)

        prev_pub = cfg.get('private_chains', {}).get(a_name, {}).get('bls_pubkey', '')
        if not prev_pub and a_name == 'exec1':
            prev_pub = cfg.get('private_chains', {}).get('chain_a', {}).get('bls_pubkey', '')
        elif not prev_pub and a_name == 'exec2':
            prev_pub = cfg.get('private_chains', {}).get('chain_b', {}).get('bls_pubkey', '')
        c_bls_pub = c_val.get('bls_pubkey', prev_pub)

        cfg['private_chains'][a_name] = {
            'chain_id': c_cid,
            'bls_private_key': c_bls_pk,
            'address': c_addr,
            'bls_pubkey': c_bls_pub,
            'rpc_url': c_rpc,
            'ws_url': c_ws,
            'rpc_nodes': c_rpc_map,
            'ws_nodes': c_ws_map,
            'tcp_nodes': c_tcp_map,
            'private_keys': prev_keys
        }

for bad in ['chain_chain_a', 'chain_chain_b', 'chain_991', 'chain_cluster_1', 'chain_cluster_2', 'chain_1', 'chain_2', 'chain_101', 'chain_102']:
    cfg.get('private_chains', {}).pop(bad, None)

# Sanitize root node maps: ensure parent_node_* and exec* never pollute root endpoints
for map_key in ['rpc_nodes', 'tcp_nodes', 'ws_nodes', 'state_history_nodes', 'sync_nodes']:
    if map_key in cfg and isinstance(cfg[map_key], dict):
        cfg[map_key] = {k: v for k, v in cfg[map_key].items() if not k.startswith('parent_node_') and not k.startswith('exec')}

with open(file6, 'w') as f:
    json.dump(cfg, f, indent=2)
print(f'✅ Updated private_chains in {file6}')
EOF
        fi
    else
        echo "Warning: $FILE6 not found." >&2
    fi
fi

# ==============================================================================
# 7. ĐỒNG BỘ CẤU HÌNH DÙNG CHUNG QUA SYMBOLIC LINKS (SINGLE SOURCE OF TRUTH)
# ==============================================================================
echo "🔗 Đồng bộ cấu hình dùng chung (Single Source of Truth) qua Symbolic Links..."
TARGET_CONFIG="$FILE6"

# 7.1. Root config.json
ln -sf "configs/config.json" "$SUITE_DIR/config.json"

# 7.2. test-simple/test-rpc/test-chain/config.json
mkdir -p "$SUITE_DIR/test-simple/test-rpc/test-chain"
ln -sf "../../../configs/config.json" "$SUITE_DIR/test-simple/test-rpc/test-chain/config.json"

echo "✅ Đã đồng bộ cấu hình dùng chung (Single Source of Truth) tại: $TARGET_CONFIG"

chmod +x "$0" 2>/dev/null || true
