#!/usr/bin/env bash
# ==============================================================================
# cluster_helpers.sh — shared cluster online/offline/zero-fork helpers, meant to
# be `source`d by test scripts (not executed directly).
#
# WHY THIS EXISTS (2026-09-14): detect_online_nodes / wait_node_online /
# wait_node_offline / wait_all_nodes_online / verify_equal_height_and_zero_fork
# were all originally written inline in restart-recovery/run_restart_test.sh.
# fault-tolerance/run_fault_tolerance_suite.sh needs the exact same primitives
# for its own new scenarios (quorum-boundary, long-catchup) — extracted here
# instead of copy-pasting a second time, matching the precedent already set by
# lib/consensus_readiness.sh ("if a future test script needs the same wait,
# source this instead of copy-pasting again"). run_restart_test.sh itself is
# intentionally NOT changed to source this file — it's a proven, frequently-run
# script and re-plumbing it carries more risk than the duplication it would
# remove; only new scripts are expected to source this one.
#
# REQUIRES: the sourcing script must already have CONFIG_PATH set to its
# config.json (the one holding an "rpc_nodes"/"sync_nodes" map of
# node-name -> RPC URL) before calling any function below.
#
# Usage:
#   source "${SCRIPT_DIR}/../lib/cluster_helpers.sh"
#   detect_online_nodes
#   wait_node_online "2"
#   wait_node_offline "2"
#   wait_all_nodes_online
#   verify_equal_height_and_zero_fork "stage label" 240
# ==============================================================================

detect_online_nodes() {
    python3 -c "
import json, urllib.request, sys

try:
    with open('${CONFIG_PATH}', 'r') as f:
        c = json.load(f)
    nodes_map = dict(c.get('rpc_nodes', {}))
    nodes_map.update(c.get('sync_nodes', {}))
    online = []
    for k, url in nodes_map.items():
        node_id = k.replace('m', '').replace('node', '')
        try:
            req = urllib.request.Request(url, data=b'{\"jsonrpc\":\"2.0\",\"method\":\"eth_blockNumber\",\"params\":[],\"id\":1}', headers={'Content-Type': 'application/json'})
            with urllib.request.urlopen(req, timeout=2) as resp:
                if resp.status == 200:
                    online.append(node_id)
        except Exception:
            pass
    print(' '.join(sorted(online, key=lambda x: int(x) if x.isdigit() else x)))
except Exception as e:
    sys.exit(1)
"
}

# get_node_height TARGET_NODE — prints the block number (decimal) or nothing on error.
get_node_height() {
    local target_node="$1"
    CONFIG_FILE="$CONFIG_PATH" TARGET="$target_node" python3 -c "
import json, urllib.request, os
with open(os.environ['CONFIG_FILE']) as f:
    c = json.load(f)
nodes_map = dict(c.get('rpc_nodes', {}))
nodes_map.update(c.get('sync_nodes', {}))
target = os.environ['TARGET']
url = None
for k, v in nodes_map.items():
    if k.replace('m', '').replace('node', '') == target:
        url = v
        break
if url is None:
    raise SystemExit(1)
req = urllib.request.Request(url, data=b'{\"jsonrpc\":\"2.0\",\"method\":\"eth_blockNumber\",\"params\":[],\"id\":1}', headers={'Content-Type': 'application/json'})
with urllib.request.urlopen(req, timeout=3) as resp:
    res = json.loads(resp.read().decode())
    print(int(res.get('result', '0x0'), 16))
" 2>/dev/null
}

wait_node_online() {
    local target_node="$1"
    local max_wait="${2:-60}"
    local waited=0
    echo -n "⏳ Đang chờ Node ${target_node} online trở lại (tối đa ${max_wait}s)... "

    while [ $waited -lt $max_wait ]; do
        local online_now=$(detect_online_nodes)
        for n in $online_now; do
            if [ "$n" == "$target_node" ]; then
                echo "✅ Node ${target_node} đã ONLINE (sau ${waited}s)!"
                return 0
            fi
        done
        sleep 2
        waited=$((waited + 2))
    done

    echo "❌ HẾT THỜI GIAN CHỜ: Node ${target_node} không phản hồi RPC sau ${max_wait}s!"
    return 1
}

wait_node_offline() {
    local target_node="$1"
    local max_wait="${2:-30}"
    local waited=0
    echo -n "⏳ Đang xác nhận Node ${target_node} đã dừng hoàn toàn (tối đa ${max_wait}s)... "

    while [ $waited -lt $max_wait ]; do
        local online_now=$(detect_online_nodes)
        local is_online=false
        for n in $online_now; do
            if [ "$n" == "$target_node" ]; then
                is_online=true
                break
            fi
        done
        if [ "$is_online" == "false" ]; then
            echo "✅ Node ${target_node} đã DỪNG OFFLINE (sau ${waited}s)!"
            return 0
        fi
        sleep 1
        waited=$((waited + 1))
    done

    echo "⚠️ Cảnh báo: Node ${target_node} vẫn phản hồi RPC sau ${max_wait}s!"
    return 1
}

# wait_nodes_offline "N1 N2 ..." [MAX_WAIT] — like wait_node_offline but for
# several nodes stopped at once (quorum-boundary scenario stops f/f+1 together).
wait_nodes_offline() {
    local nodes_str="$1"
    local max_wait="${2:-30}"
    local waited=0
    echo "⏳ Đang xác nhận các Node [ ${nodes_str} ] đã dừng hoàn toàn (tối đa ${max_wait}s)..."

    while [ $waited -lt $max_wait ]; do
        local online_now=$(detect_online_nodes)
        local any_online=false
        for target in $nodes_str; do
            for n in $online_now; do
                if [ "$n" == "$target" ]; then
                    any_online=true
                    break
                fi
            done
        done
        if [ "$any_online" == "false" ]; then
            echo "✅ Toàn bộ Node [ ${nodes_str} ] đã DỪNG OFFLINE (sau ${waited}s)!"
            return 0
        fi
        sleep 1
        waited=$((waited + 1))
    done

    echo "⚠️ Cảnh báo: Có node trong [ ${nodes_str} ] vẫn phản hồi RPC sau ${max_wait}s!"
    return 1
}

wait_all_nodes_online() {
    local max_wait="${1:-60}"
    local waited=0
    echo "⏳ Đang chờ TẤT CẢ các node [ ${ACTIVE_NODES[*]} ] online..."

    while [ $waited -lt $max_wait ]; do
        local online_now=$(detect_online_nodes)
        local all_up=true
        for target in "${ACTIVE_NODES[@]}"; do
            local found=false
            for n in $online_now; do
                if [ "$n" == "$target" ]; then
                    found=true
                    break
                fi
            done
            if [ "$found" == "false" ]; then
                all_up=false
                break
            fi
        done

        if [ "$all_up" == "true" ]; then
            echo "✅ Toàn bộ các node [ ${ACTIVE_NODES[*]} ] đều đã ONLINE sẵn sàng (sau ${waited}s)!"
            return 0
        fi
        sleep 2
        waited=$((waited + 2))
    done

    echo "❌ HẾT THỜI GIAN CHỜ: Có node chưa online sau ${max_wait}s!"
    return 1
}

# verify_equal_height_and_zero_fork STAGE_LABEL [TIMEOUT_SEC=240] [EXCLUDE_NODE]
# Identical logic to restart-recovery/run_restart_test.sh's own copy — see there
# for the original comment history. Kept as a straight port so both scripts'
# zero-fork verification stays byte-for-byte the same check.
verify_equal_height_and_zero_fork() {
    local stage_label="$1"
    local timeout_sec="${2:-240}"
    local exclude_node="${3:-""}"

    STAGE_LABEL="$stage_label" TIMEOUT_SEC="$timeout_sec" EXCLUDE_NODE="$exclude_node" CONFIG_FILE="$CONFIG_PATH" python3 - << 'EOF'
import json, urllib.request, time, sys, os

config_path = os.environ.get("CONFIG_FILE", "../config.json")
stage_label = os.environ.get("STAGE_LABEL", "Kiểm tra đồng bộ")
timeout_sec = int(os.environ.get("TIMEOUT_SEC", "240"))
exclude_node = os.environ.get("EXCLUDE_NODE", "").strip()

with open(config_path, "r") as f:
    cfg = json.load(f)

all_nodes = dict(cfg.get("rpc_nodes", {}))
all_nodes.update(cfg.get("sync_nodes", {}))

if exclude_node:
    exc_list = [x.strip().lower().replace("m", "").replace("node", "") for x in exclude_node.split(",") if x.strip()]
    for k in list(all_nodes.keys()):
        clean_k = k.lower().replace("m", "").replace("node", "")
        if clean_k in exc_list or k in exc_list:
            del all_nodes[k]

print("\n==========================================================")
print(f"🔍 [KIỂM TRA ĐỒNG BỘ CHIỀU CAO & ZERO-FORK] {stage_label}")
if exclude_node:
    print(f"ℹ️  Đang kiểm tra {len(all_nodes)} node (loại trừ Node [{exclude_node}] đang tắt)")
else:
    print(f"ℹ️  Đang kiểm tra TOÀN BỘ {len(all_nodes)} node trong cụm (Validator + SyncOnly)")
print(f"⏳ Đang chờ tất cả các node đạt chiều cao block bằng nhau (tối đa {timeout_sec}s)...")
print("==========================================================")

if not all_nodes:
    print("❌ Không có node nào cần kiểm tra!")
    sys.exit(1)

start_time = time.time()
equal_height = False
final_heights = {}

# STABILITY REQUIREMENT (2026-09-17): a node still fast-forwarding through a large
# replay/catch-up backlog (e.g. a SyncOnly node right after restart) can momentarily
# report the SAME eth_blockNumber as the rest of the cluster for exactly one 3s sampling
# window purely by coincidence of timing, while still actively climbing and NOT actually
# settled/caught-up -- the old code treated that single lucky sample as proof of
# convergence and moved on immediately, then a write sent right through that node could
# sit unconfirmed until timeout. See restart-recovery/run_restart_test.sh's own copy of
# this fix for the live incident that found this. Fix: require the SAME set of heights to
# hold for STABLE_ROUNDS_REQUIRED consecutive polls (3s apart) before declaring
# convergence.
STABLE_ROUNDS_REQUIRED = 3
stable_rounds = 0
last_stable_vals = None

while time.time() - start_time < timeout_sec:
    elapsed = int(time.time() - start_time)
    heights = {}
    all_ok = True

    for name, url in sorted(all_nodes.items()):
        try:
            req = urllib.request.Request(
                url,
                data=b'{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}',
                headers={"Content-Type": "application/json"}
            )
            with urllib.request.urlopen(req, timeout=3) as resp:
                res = json.loads(resp.read().decode())
                blk = int(res.get("result", "0x0"), 16)
                heights[name] = blk
        except Exception:
            all_ok = False
            heights[name] = None

    final_heights = heights
    status_parts = [f"{k}: #{v if v is not None else 'DEAD'}" for k, v in heights.items()]

    if all_ok and len(heights) == len(all_nodes):
        vals = list(heights.values())
        max_h = max(vals)
        min_h = min(vals)
        # Allow up to 3 blocks delta because continuous empty block production + sequential polling
        if max_h - min_h <= 3:
            stable_rounds += 1
        else:
            stable_rounds = 0
    else:
        stable_rounds = 0

    print(f"   [{elapsed}s/{timeout_sec}s] Chiều cao hiện tại: {' | '.join(status_parts)}"
          + (f"  (ổn định {stable_rounds}/{STABLE_ROUNDS_REQUIRED})" if stable_rounds > 0 else ""))

    if stable_rounds >= STABLE_ROUNDS_REQUIRED:
        equal_height = True
        break

    time.sleep(3)

if not equal_height:
    print(f"\n❌ [LỖI ĐỒNG BỘ TIMEOUT] Sau {timeout_sec}s, các node vẫn CHƯA đạt chiều cao bằng nhau!")
    for k, v in final_heights.items():
        print(f"   • {k}: Block #{v}")
    sys.exit(1)

target_block = min(final_heights.values())
print(f"\n✅ Tất cả {len(all_nodes)} node đã hội tụ đồng bộ (delta <= 3) quanh Block #{target_block}!")
print(f"🔍 Bắt đầu đối chiếu Block Hash & StateRoot tại Block #{target_block}...")

hashes = {}
state_roots = {}
block_hex = hex(target_block)

for name, url in sorted(all_nodes.items()):
    req = urllib.request.Request(
        url,
        data=json.dumps({"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":[block_hex, False],"id":2}).encode(),
        headers={"Content-Type": "application/json"}
    )
    with urllib.request.urlopen(req, timeout=3) as resp:
        data = json.loads(resp.read().decode())
        b_info = data.get("result", {})
        hashes[name] = b_info.get("hash")
        state_roots[name] = b_info.get("stateRoot")

fork_detected = False
ref_name = list(sorted(all_nodes.keys()))[0]
ref_hash = hashes.get(ref_name)
ref_root = state_roots.get(ref_name)

for name in sorted(all_nodes.keys()):
    h = hashes.get(name)
    r = state_roots.get(name)
    if h != ref_hash:
        print(f"🚨 FORK DETECTED! Lệch Block Hash tại Block #{target_block}:")
        print(f"   - {ref_name}: {ref_hash}")
        print(f"   - {name}: {h}")
        fork_detected = True
    if r != ref_root:
        print(f"🚨 FORK DETECTED! Lệch StateRoot tại Block #{target_block}:")
        print(f"   - {ref_name}: {ref_root}")
        print(f"   - {name}: {r}")
        fork_detected = True

if fork_detected:
    print("❌ PHÁT HIỆN FORK GIỮA CÁC NODE!")
    sys.exit(1)

print(f"🏆 [100% ZERO-FORK CONFIRMED] Block #{target_block} đồng nhất hoàn hảo trên toàn bộ {len(all_nodes)} node!")
print(f"   • Block Hash : {ref_hash}")
print(f"   • StateRoot  : {ref_root}\n")
EOF
}
