#!/usr/bin/env bash
# ==============================================================================
# consensus_readiness.sh — shared "is this node actually ready for a transaction"
# check, meant to be `source`d by test scripts (not executed directly).
#
# WHY THIS EXISTS (2026-09-09): eth_blockNumber answering only means the Go RPC
# server is up -- it says nothing about whether the node's Rust consensus layer
# is actually in a phase that accepts new proposals. A node that just restarted
# (restart-recovery) or was just restored from a snapshot (snapshot-recovery)
# can answer eth_blockNumber within seconds, well before consensus finishes
# bootstrapping/rejoining/catching-up -- sending a transaction in that window
# gives a silent "0/N confirmed" with no error to debug from. eth_consensusReady
# (metanode repo, commit 22e0fb35) exposes exactly this signal.
#
# This was independently duplicated into restart-recovery/run_restart_test.sh
# (commit 829c9fc) and snapshot-recovery/run_snapshot_test.sh (commit c53f280)
# as near-identical inline copies before being extracted here -- if a future
# test script needs the same wait, source this instead of copy-pasting a third
# time, and any future fix to the check itself (e.g. the RPC method's shape
# changing) only needs to happen in one place.
#
# REQUIRES: the sourcing script must already have CONFIG_PATH set to its
# config.json (the one holding an "rpc_nodes" map of node-name -> RPC URL)
# before calling any function below.
#
# Usage:
#   source "${SCRIPT_DIR}/../lib/consensus_readiness.sh"
#   wait_node_consensus_ready "1"          # single node, e.g. after --restore-node 1
#   wait_all_nodes_consensus_ready "0 1 2" # whole cluster, e.g. after --restart
# ==============================================================================

# node_consensus_ready TARGET_NODE
# Returns 0 (success) if the node's consensus layer reports ready, 1 otherwise
# (including any network/parse error -- fail closed, never assume ready on doubt).
node_consensus_ready() {
    local target_node="$1"
    python3 -c "
import json, urllib.request, sys

try:
    with open('${CONFIG_PATH}', 'r') as f:
        c = json.load(f)
    # Nếu node thuộc sync_nodes (SyncOnly node), không tham gia propose block nên consensusReady không áp dụng
    sync_nodes = c.get('sync_nodes', {})
    for k in sync_nodes:
        if k.replace('m', '').replace('node', '') == '${target_node}':
            sys.exit(0)

    rpc_nodes = c.get('rpc_nodes', {})
    url = None
    for k, v in rpc_nodes.items():
        if k.replace('m', '').replace('node', '') == '${target_node}':
            url = v
            break
    if url is None:
        sys.exit(1)
    req = urllib.request.Request(url, data=b'{\"jsonrpc\":\"2.0\",\"method\":\"eth_consensusReady\",\"params\":[],\"id\":1}', headers={'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=3) as resp:
        body = json.loads(resp.read())
        sys.exit(0 if body.get('result', {}).get('ready') else 1)
except Exception:
    sys.exit(1)
"
}

# get_node_consensus_info TARGET_NODE
# Returns formatted JSON string from eth_consensusReady RPC
get_node_consensus_info() {
    local target_node="$1"
    python3 -c "
import json, urllib.request, sys

try:
    with open('${CONFIG_PATH}', 'r') as f:
        c = json.load(f)
    rpc_nodes = c.get('rpc_nodes', {})
    url = None
    for k, v in rpc_nodes.items():
        if k.replace('m', '').replace('node', '') == '${target_node}':
            url = v
            break
    if url is None:
        print(json.dumps({'ready': False, 'note': 'Node URL not found in config'}, indent=2))
        sys.exit(0)
    req = urllib.request.Request(url, data=b'{\"jsonrpc\":\"2.0\",\"method\":\"eth_consensusReady\",\"params\":[],\"id\":1}', headers={'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=3) as resp:
        body = json.loads(resp.read())
        res = body.get('result', {})
        print(json.dumps(res, indent=2, ensure_ascii=False))
except Exception as e:
    print(json.dumps({'ready': False, 'note': f'Error querying RPC: {e}'}, indent=2))
"
}

# send_telegram_readiness_alert TARGET_NODE JSON_BODY WAITED_SECS
send_telegram_readiness_alert() {
    local target_node="$1"
    local json_body="$2"
    local waited="$3"

    local token="${TELEGRAM_BOT_TOKEN:-}"
    local chat_id="${TELEGRAM_CHAT_ID:-}"

    local inv_path="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../../../../metanode/deploy/ansible/inventory.yml"
    if [ -z "$token" ] && [ -f "$inv_path" ]; then
        token=$(grep -E '^\s*bot_token:' "$inv_path" | head -n 1 | awk '{print $2}' | tr -d '"' | tr -d "'")
        chat_id=$(grep -E '^\s*chat_id:' "$inv_path" | head -n 1 | awk '{print $2}' | tr -d '"' | tr -d "'")
    fi

    token="${token:-"8230176859:AAG2MuF6RI3hRPm9H8_TctSSANkwwrEEdIc"}"
    chat_id="${chat_id:-"-1003867050625"}"

    if [ -z "$token" ] || [ -z "$chat_id" ]; then
        return
    fi

    local msg="⚠️ <b>[METANODE CẢNH BÁO] Node Chưa Sẵn Sàng Xử Lý Giao Dịch!</b>
• <b>Target Node:</b> Node ${target_node}
• <b>Thời gian chờ:</b> ${waited}s
• <b>Phản hồi eth_consensusReady:</b>
<pre>${json_body}</pre>"

    curl -s -X POST "https://api.telegram.org/bot${token}/sendMessage" \
        -d chat_id="${chat_id}" \
        -d parse_mode="HTML" \
        --data-urlencode text="${msg}" >/dev/null 2>&1 || true
}

# wait_node_consensus_ready TARGET_NODE [MAX_WAIT_SECONDS=120]
# Polls node_consensus_ready every 2s until it succeeds or MAX_WAIT_SECONDS elapses.
# Does NOT hard-fail the caller on timeout (returns 1, but doesn't `exit`/`set -e`
# out) -- callers decide whether "still not ready, proceeding anyway" is fatal for
# their scenario. A tx sent right after a timeout is exactly the failure mode this
# exists to catch, so the warning is loud on purpose.
wait_node_consensus_ready() {
    local target_node="$1"
    local max_wait="${2:-120}"
    local waited=0
    echo -n "⏳ Đang chờ Node ${target_node} sẵn sàng xử lý giao dịch (consensus ready, tối đa ${max_wait}s)... "

    while [ "$waited" -lt "$max_wait" ]; do
        if node_consensus_ready "${target_node}"; then
            echo "✅ Node ${target_node} đã SẴN SÀNG (sau ${waited}s)!"
            return 0
        fi
        sleep 2
        waited=$((waited + 2))
    done

    echo "⚠️ Cảnh báo: Node ${target_node} vẫn CHƯA sẵn sàng xử lý giao dịch sau ${max_wait}s (RPC online nhưng consensus chưa Healthy) -- gửi tx bây giờ có thể không confirm được. Tiếp tục thử gửi..."
    local info_json
    info_json=$(get_node_consensus_info "${target_node}")
    echo "📋 Phản hồi từ RPC eth_consensusReady:"
    echo "${info_json}"
    send_telegram_readiness_alert "${target_node}" "${info_json}" "${max_wait}"
    echo "📨 Đã gửi cảnh báo Node chưa sẵn sàng qua Telegram."
    return 1
}

# wait_all_nodes_consensus_ready "NODE1 NODE2 ..." [MAX_WAIT_SECONDS=60]
# Same as wait_node_consensus_ready, but for a whole space-separated list of node
# ids at once (e.g. after a full-cluster --restart) -- returns success only once
# every listed node reports ready.
wait_all_nodes_consensus_ready() {
    local nodes_str="$1"
    local max_wait="${2:-60}"
    local waited=0
    echo "⏳ Đang chờ TẤT CẢ các node [ ${nodes_str} ] sẵn sàng xử lý giao dịch (consensus ready)..."

    while [ "$waited" -lt "$max_wait" ]; do
        local all_ready=true
        for target in $nodes_str; do
            if ! node_consensus_ready "${target}"; then
                all_ready=false
                break
            fi
        done

        if [ "$all_ready" == "true" ]; then
            echo "✅ Toàn bộ các node [ ${nodes_str} ] đều SẴN SÀNG xử lý giao dịch (sau ${waited}s)!"
            return 0
        fi
        sleep 2
        waited=$((waited + 2))
    done

    echo "⚠️ Cảnh báo: Có node chưa sẵn sàng xử lý giao dịch sau ${max_wait}s -- gửi tx bây giờ có thể timeout không rõ lý do. Tiếp tục thử gửi..."
    for target in $nodes_str; do
        if ! node_consensus_ready "${target}"; then
            local info_json
            info_json=$(get_node_consensus_info "${target}")
            echo "📋 Phản hồi Node ${target} eth_consensusReady:"
            echo "${info_json}"
            send_telegram_readiness_alert "${target}" "${info_json}" "${max_wait}"
        fi
    done
    echo "📨 Đã gửi cảnh báo các Node chưa sẵn sàng qua Telegram."
    return 1
}
