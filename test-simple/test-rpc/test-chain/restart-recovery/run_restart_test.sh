#!/usr/bin/env bash
# ==============================================================================
# 🌐 METANODE CHAOS ROLLING RESTART & ZERO-FORK RECOVERY TEST
# - Tự động phát hiện các node đang online từ config.json
# - Luân phiên tắt/bật từng node trong danh sách active
# - Khi node thức dậy: gửi giao dịch kiểm chứng, xác nhận vào block
# - Sau mỗi đợt: kiểm tra sức khỏe TẤT CẢ các node đảm bảo KHÔNG CÓ NODE NÀO CHẾT
# - Kiểm tra đối chiếu Block Hash & State Root đảm bảo 100% Zero-Fork
# - Cuối cùng tắt/bật toàn bộ cụm node và kiểm chứng toàn diện
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_PATH="${SCRIPT_DIR}/../config.json"

# Tìm thư mục deploy/ansible của metanode
DEFAULT_METANODE_DIR="$(cd "${SCRIPT_DIR}/../../../../../metanode" 2>/dev/null && pwd || true)"
METANODE_DIR="${METANODE_DIR:-"$DEFAULT_METANODE_DIR"}"
ANSIBLE_DIR="${METANODE_DIR}/deploy/ansible"

if [ ! -f "${ANSIBLE_DIR}/ansible_deploy.sh" ]; then
    echo "❌ Không tìm thấy script ansible_deploy.sh tại: ${ANSIBLE_DIR}"
    exit 1
fi

SPECIFIED_NODES=""
TX_COUNT=15
LOOP_COUNT=1          # Mặc định 1 vòng (tương thích ngược CI). 0 hoặc "infinite" là chạy mãi mãi.
DURATION_HOURS=0      # Thời gian chạy tối đa theo giờ (0 = không giới hạn)
SLEEP_BETWEEN_ROUNDS=10 # Thời gian nghỉ giữa các vòng (giây)

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --nodes) SPECIFIED_NODES="$2"; shift ;;
        --nodes=*) SPECIFIED_NODES="${1#*=}" ;;
        --count) TX_COUNT="$2"; shift ;;
        --count=*) TX_COUNT="${1#*=}" ;;
        --loop|--loops) 
            if [ "$2" == "infinite" ] || [ "$2" == "-1" ]; then
                LOOP_COUNT=0
            else
                LOOP_COUNT="$2"
            fi
            shift ;;
        --loop=*|--loops=*)
            val="${1#*=}"
            if [ "$val" == "infinite" ] || [ "$val" == "-1" ]; then
                LOOP_COUNT=0
            else
                LOOP_COUNT="$val"
            fi
            ;;
        --infinite) LOOP_COUNT=0 ;;
        --duration-hours) DURATION_HOURS="$2"; shift ;;
        --duration-hours=*) DURATION_HOURS="${1#*=}" ;;
        --sleep-between) SLEEP_BETWEEN_ROUNDS="$2"; shift ;;
        --sleep-between=*) SLEEP_BETWEEN_ROUNDS="${1#*=}" ;;
        --duration|--duration=*) shift ;; # Tương thích ngược
        -h|--help)
            echo "Cách dùng: $0 [OPTIONS]"
            echo "  --nodes <0,1,2>           Chỉ định danh sách node cần test (Mặc định: tự động phát hiện node online)"
            echo "  --count <15>              Số lượng giao dịch gửi và xác nhận mỗi chặng"
            echo "  --loop <N|infinite>       Số vòng lặp test rolling restart (Mặc định: 1; 0 hoặc 'infinite' = vô tận)"
            echo "  --infinite                Chạy lặp vô tận (tiện dụng để test qua đêm)"
            echo "  --duration-hours <H>      Lặp theo số giờ H > 0, ưu tiên hơn --loop; không cần --infinite; dừng sau vòng hiện tại"
            echo "  --sleep-between <sec>     Thời gian nghỉ giữa các vòng lặp (Mặc định: 10s)"
            exit 0
            ;;
        *) echo "Tham số không hợp lệ: $1"; exit 1 ;;
    esac
    shift
done

# A positive duration selects timed repetition regardless of the loop flags.
if (( $(echo "$DURATION_HOURS > 0" | bc -l 2>/dev/null || [ "$DURATION_HOURS" -gt 0 ] 2>/dev/null || echo 0) )); then
    LOOP_COUNT=0
fi

echo "=========================================================="
echo "🚀 BẮT ĐẦU BÀI TEST: CLUSTER RESTART & ZERO-FORK RECOVERY"
echo "📂 Thư mục: ${SCRIPT_DIR}"
echo "⚙️  Ansible: ${ANSIBLE_DIR}/ansible_deploy.sh"
echo "🔢 Số TX kiểm chứng mỗi chặng: ${TX_COUNT}"
if [ "$LOOP_COUNT" -eq 0 ]; then
    echo "🔁 Chế độ lặp: Không giới hạn số vòng (dừng theo thời lượng nếu có)"
else
    echo "🔁 Chế độ lặp: ${LOOP_COUNT} vòng"
fi
if (( $(echo "$DURATION_HOURS > 0" | bc -l 2>/dev/null || [ "$DURATION_HOURS" -gt 0 ] 2>/dev/null || echo 0) )); then
    echo "⏱️  Giới hạn thời gian: ${DURATION_HOURS} giờ"
fi
echo "=========================================================="

cd "${SCRIPT_DIR}"

trap 'rm -f /tmp/monitors_ignore_nodes 2>/dev/null || true' EXIT

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

if [ -n "$SPECIFIED_NODES" ]; then
    IFS=',' read -r -a ACTIVE_NODES <<< "$SPECIFIED_NODES"
else
    echo "🔍 Đang tự động quét phát hiện các node đang hoạt động..."
    DETECTED=$(detect_online_nodes || true)
    if [ -z "$DETECTED" ]; then
        echo "❌ Không phát hiện được node nào online từ ${CONFIG_PATH}!"
        exit 1
    fi
    read -r -a ACTIVE_NODES <<< "$DETECTED"
fi

echo "📋 Danh sách các node tham gia bài test: [ ${ACTIVE_NODES[*]} ] (Tổng: ${#ACTIVE_NODES[@]} nodes)"

wait_node_online() {
    local target_node="$1"
    local max_wait=60
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
    local max_wait=30
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

wait_all_nodes_online() {
    local max_wait=60
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

# ------------------------------------------------------------------------------
# CONSENSUS READINESS (2026-09-09): "online" above only means eth_blockNumber answers, i.e. the
# HTTP/RPC server is up -- it says nothing about whether the node's Rust consensus layer would
# actually accept/propose a transaction sent right now.
# Note: SyncOnly nodes (e.g. node 4) do not propose blocks in DAG, so they are always ready once online.
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

wait_node_consensus_ready() {
    local target_node="$1"
    local max_wait=60
    local waited=0
    echo -n "⏳ Đang chờ Node ${target_node} sẵn sàng xử lý giao dịch (consensus ready, tối đa ${max_wait}s)... "

    while [ $waited -lt $max_wait ]; do
        if node_consensus_ready "${target_node}"; then
            echo "✅ Node ${target_node} đã SẴN SÀNG (sau ${waited}s)!"
            return 0
        fi
        sleep 2
        waited=$((waited + 2))
    done

    echo "⚠️ Cảnh báo: Node ${target_node} vẫn CHƯA sẵn sàng xử lý giao dịch sau ${max_wait}s (RPC online nhưng consensus chưa Healthy) -- gửi tx bây giờ có thể timeout không rõ lý do. Tiếp tục thử gửi..."
    return 1
}

wait_all_nodes_consensus_ready() {
    local max_wait=60
    local waited=0
    echo "⏳ Đang chờ TẤT CẢ các node [ ${ACTIVE_NODES[*]} ] sẵn sàng xử lý giao dịch (consensus ready)..."

    while [ $waited -lt $max_wait ]; do
        local all_ready=true
        for target in "${ACTIVE_NODES[@]}"; do
            if ! node_consensus_ready "${target}"; then
                all_ready=false
                break
            fi
        done

        if [ "$all_ready" == "true" ]; then
            echo "✅ Toàn bộ các node [ ${ACTIVE_NODES[*]} ] đều SẴN SÀNG xử lý giao dịch (sau ${waited}s)!"
            return 0
        fi
        sleep 2
        waited=$((waited + 2))
    done

    echo "⚠️ Cảnh báo: Có node chưa sẵn sàng xử lý giao dịch sau ${max_wait}s -- gửi tx bây giờ có thể timeout không rõ lý do. Tiếp tục thử gửi..."
    return 1
}

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

# Gộp toàn bộ các node trong cụm (Validator + SyncOnly)
all_nodes = dict(cfg.get("rpc_nodes", {}))
all_nodes.update(cfg.get("sync_nodes", {}))

# Loại trừ node đang tắt hoặc đang snapshot (nếu có chỉ định)
if exclude_node:
    exc_list = [x.strip().lower().replace("m", "").replace("node", "") for x in exclude_node.split(",") if x.strip()]
    for k in list(all_nodes.keys()):
        clean_k = k.lower().replace("m", "").replace("node", "")
        if clean_k in exc_list or k in exc_list:
            del all_nodes[k]

print("\n==========================================================")
print(f"🔍 [KIỂM TRA ĐỒNG BỘ CHIỀU CAO & ZERO-FORK] {stage_label}")
if exclude_node:
    print(f"ℹ️  Đang kiểm tra {len(all_nodes)} node (loại trừ Node [{exclude_node}] đang tắt/snapshot)")
else:
    print(f"ℹ️  Đang kiểm tra TOÀN BỘ {len(all_nodes)} node trong cụm (Validator + SyncOnly)")
print(f"⏳ Đang chờ tất cả các node đạt chiều cao block bằng nhau (tối đa {timeout_sec}s / 4 phút)...")
print("==========================================================")

if not all_nodes:
    print("❌ Không có node nào cần kiểm tra!")
    sys.exit(1)

start_time = time.time()
equal_height = False
final_heights = {}

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
    print(f"   [{elapsed}s/{timeout_sec}s] Chiều cao hiện tại: {' | '.join(status_parts)}")

    if all_ok and len(heights) == len(all_nodes):
        vals = list(heights.values())
        if len(set(vals)) == 1:
            equal_height = True
            break

    time.sleep(3)

if not equal_height:
    print(f"\n❌ [LỖI ĐỒNG BỘ TIMEOUT] Sau {timeout_sec}s (4 phút), các node vẫn CHƯA đạt chiều cao bằng nhau!")
    for k, v in final_heights.items():
        print(f"   • {k}: Block #{v}")
    sys.exit(1)

target_block = list(final_heights.values())[0]
print(f"\n✅ Tất cả {len(all_nodes)} node đã có block number bằng nhau tại: Block #{target_block}!")
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
    print("❌ BÀI TEST THẤT BẠI DO PHÁT HIỆN FORK GIỮA CÁC NODE!")
    sys.exit(1)

print(f"🏆 [100% ZERO-FORK CONFIRMED] Block #{target_block} đồng nhất hoàn hảo trên toàn bộ {len(all_nodes)} node!")
print(f"   • Block Hash : {ref_hash}")
print(f"   • StateRoot  : {ref_root}\n")
EOF
}

START_TIME=$(date +%s)
current_loop=1

while true; do
    echo -e "\n=========================================================="
    if [ "$LOOP_COUNT" -eq 0 ]; then
        echo "🔄 [VÒNG LẶP ${current_loop}] BẮT ĐẦU CHẶNG TEST"
    else
        echo "🔄 [VÒNG LẶP ${current_loop}/${LOOP_COUNT}] BẮT ĐẦU CHẶNG TEST"
    fi
    echo "=========================================================="

    # KIỂM CHỨNG ĐẦU ROUND: Đảm bảo toàn bộ node có chiều cao bằng nhau & 100% Zero-Fork trước khi test
    verify_equal_height_and_zero_fork "ĐẦU VÒNG ${current_loop}/${LOOP_COUNT:-∞} (Trước khi bắt đầu chu kỳ Restart)" 240

    # ------------------------------------------------------------------------------
    # BƯỚC 1: KHỞI ĐỘNG BAN ĐẦU
    # ------------------------------------------------------------------------------
    echo -e "\n----------------------------------------------------------"
    echo "🏁 [BƯỚC 1] Kiểm tra ban đầu: gửi đợt giao dịch warm-up và check sức khỏe cụm..."
    echo "----------------------------------------------------------"
    go run main.go -count "${TX_COUNT}" -check-fork -require-all-alive=true

    # ------------------------------------------------------------------------------
    # BƯỚC 2: ROLLING RESTART TỪNG NODE (LUÂN PHIÊN)
    # ------------------------------------------------------------------------------
    echo -e "\n=========================================================="
    echo "🔄 [BƯỚC 2] BẮT ĐẦU ROLLING RESTART TỪNG NODE (${#ACTIVE_NODES[@]} NODES)"
    echo "=========================================================="

    TOTAL_ACTIVE=${#ACTIVE_NODES[@]}
    round=1
    for node_id in "${ACTIVE_NODES[@]}"; do
        echo -e "\n👉 [CHẶNG 2.${round}] TẮT & KIỂM CHỨNG & BẬT LẠI NODE ${node_id}..."
        
        echo "🔴 1. Tắt Node ${node_id} (Dự kiến tắt phục vụ test chịu lỗi)..."
        echo "${node_id}" > /tmp/monitors_ignore_nodes 2>/dev/null || true
        "${ANSIBLE_DIR}/ansible_deploy.sh" --stop --only-node "${node_id}"
        wait_node_offline "${node_id}" || {
            echo "❌ LỖI: Node ${node_id} không thể dừng hoàn toàn! Dừng bài test."
            exit 1
        }

        # NẾU CỤM CÓ TRÊN 3 NODES: Khi 1 node tắt, cụm còn lại >= 3 nodes (đủ 2f+1 quorum)
        # Kiểm tra gửi giao dịch đến các node còn lại, đảm bảo các node còn lại đều sống và Zero-Fork
        if [ "$TOTAL_ACTIVE" -gt 3 ]; then
            echo "⏳ Chờ 3s để các node còn lại ổn định round consensus sau khi Node ${node_id} dừng..."
            sleep 3
            echo "⚡ [Node ${node_id} ĐANG TẮT - Cụm còn $((TOTAL_ACTIVE - 1)) nodes online]"
            echo "   👉 Gửi ${TX_COUNT} giao dịch phân bổ CHỈ qua các node đang online (loại trừ Node ${node_id})..."
            go run main.go -count "${TX_COUNT}" -check-fork -require-all-alive=true -stopped-node="${node_id}"
        else
            echo "ℹ️  Cụm ban đầu có ${TOTAL_ACTIVE} nodes. Khi dừng node ${node_id} thì chỉ còn $((TOTAL_ACTIVE - 1)) nodes (chưa đủ quorum để tiếp tục commit), bỏ qua bước gửi giao dịch trong lúc dừng."
        fi

        echo "🟢 2. Bật lại Node ${node_id}..."
        "${ANSIBLE_DIR}/ansible_deploy.sh" --restart --only-node "${node_id}"

        wait_node_online "${node_id}"
        wait_node_consensus_ready "${node_id}"
        rm -f /tmp/monitors_ignore_nodes 2>/dev/null || true

        echo "⚡ [Node ${node_id} VỪA THỨC DẬY] Bơm ${TX_COUNT} giao dịch toàn cụm, kiểm tra catch-up sync, sức khỏe toàn bộ node và Zero-Fork..."
        go run main.go -count "${TX_COUNT}" -check-fork -require-all-alive=true

        round=$((round + 1))
    done

    # ------------------------------------------------------------------------------
    # BƯỚC 3: FULL CLUSTER RESTART (TẮT VÀ BẬT TOÀN BỘ CỤM NODE)
    # ------------------------------------------------------------------------------
    echo -e "\n=========================================================="
    echo "🛑 [BƯỚC 3] TẮT & BẬT LẠI TOÀN BỘ CỤM NODE (${ACTIVE_NODES[*]})"
    echo "=========================================================="

    echo "🛑 Dừng toàn bộ cụm node..."
    echo "${ACTIVE_NODES[*]}" > /tmp/monitors_ignore_nodes 2>/dev/null || true
    "${ANSIBLE_DIR}/ansible_deploy.sh" --stop
    echo "⏳ Chờ 5s đảm bảo mọi process đã dừng hẳn..."
    sleep 5

    echo "🚀 Khởi động lại toàn bộ cụm node..."
    "${ANSIBLE_DIR}/ansible_deploy.sh" --restart

    wait_all_nodes_online
    wait_all_nodes_consensus_ready
    rm -f /tmp/monitors_ignore_nodes 2>/dev/null || true

    echo "⚡ [Cả cụm vừa thức dậy] Bơm ${TX_COUNT} giao dịch load-balance, kiểm tra sống/chết và Zero-Fork..."
    go run main.go -count "${TX_COUNT}" -check-fork -require-all-alive=true

    # ------------------------------------------------------------------------------
    # BƯỚC 4: KIỂM TRA SỨC KHỎE TẤT CẢ CÁC NODE SAU VÒNG TEST
    # ------------------------------------------------------------------------------
    echo -e "\n=========================================================="
    echo "📡 [BƯỚC 4] KIỂM TRA SỨC KHỎE TẤT CẢ CÁC NODE (VÒNG ${current_loop})"
    echo "=========================================================="

    python3 -c "
import json, urllib.request, sys, os

try:
    with open('${CONFIG_PATH}', 'r') as f:
        c = json.load(f)
    nodes_map = dict(c.get('rpc_nodes', {}))
    nodes_map.update(c.get('sync_nodes', {}))

    ignore_nodes = []
    if os.path.isfile('/tmp/monitors_ignore_nodes'):
        try:
            with open('/tmp/monitors_ignore_nodes') as ig_f:
                ignore_nodes = [x.strip().replace('m', '').replace('node', '') for x in ig_f.read().split() if x.strip()]
        except Exception:
            pass

    dead_nodes = []
    print(f'🔍 Kiểm tra trạng thái {len(nodes_map)} node cấu hình (Validator + SyncOnly):')
    for name, url in sorted(nodes_map.items()):
        node_id = name.replace('m', '').replace('node', '')
        is_ignored = node_id in ignore_nodes
        try:
            req = urllib.request.Request(url, data=b'{\"jsonrpc\":\"2.0\",\"method\":\"eth_blockNumber\",\"params\":[],\"id\":1}', headers={'Content-Type': 'application/json'})
            with urllib.request.urlopen(req, timeout=3) as resp:
                data = json.loads(resp.read().decode())
                blk = int(data.get('result', '0x0'), 16)
                if is_ignored:
                    print(f'   • Node {name} ({url}): ⚪ STOPPED/SNAPSHOT (Block {blk} - Ngoại lệ test)')
                else:
                    print(f'   • Node {name} ({url}): 🟢 ALIVE (Block {blk})')
        except Exception as e:
            if is_ignored:
                print(f'   • Node {name} ({url}): ⚪ STOPPED/SNAPSHOT (Đang tắt hoặc Snapshot theo kịch bản)')
            else:
                print(f'   • Node {name} ({url}): 🔴 DEAD / MẤT KẾT NỐI ({e})')
                dead_nodes.append(name)
    if dead_nodes:
        print(f'\n❌ PHÁT HIỆN CÓ {len(dead_nodes)} NODE BỊ CHẾT: {dead_nodes}! BÀI TEST THẤT BẠI!')
        sys.exit(1)
    else:
        print(f'\n✅ TOÀN BỘ CÁC NODE ĐỀU ĐANG CÒN SỐNG KHỎE MẠNH!')
except Exception as e:
    print(f'❌ Lỗi khi quét sức khỏe node: {e}')
    sys.exit(1)
"

    # KIỂM CHỨNG CUỐI ROUND: Đảm bảo toàn bộ node đã hội tụ chiều cao bằng nhau & 100% Zero-Fork sau chu kỳ restart
    verify_equal_height_and_zero_fork "CUỐI VÒNG ${current_loop}/${LOOP_COUNT:-∞} (Sau khi hoàn tất toàn bộ chu kỳ Restart)" 240

    # Kiểm tra điều kiện kết thúc vòng lặp
    CURRENT_TIME=$(date +%s)
    ELAPSED_SEC=$((CURRENT_TIME - START_TIME))
    ELAPSED_HOURS=$(echo "scale=2; $ELAPSED_SEC / 3600" | bc 2>/dev/null || echo "0")

    echo -e "\n📊 Đã hoàn thành vòng lặp thứ ${current_loop} (Thời gian đã chạy: ${ELAPSED_SEC}s ~ ${ELAPSED_HOURS}h)"

    if [ "$LOOP_COUNT" -gt 0 ] && [ "$current_loop" -ge "$LOOP_COUNT" ]; then
        echo "🏁 Đã hoàn thành đủ ${LOOP_COUNT} vòng lặp yêu cầu."
        break
    fi

    if (( $(echo "$DURATION_HOURS > 0" | bc -l 2>/dev/null || [ "$DURATION_HOURS" -gt 0 ] 2>/dev/null || echo 0) )); then
        LIMIT_SEC=$(echo "$DURATION_HOURS * 3600" | bc 2>/dev/null || echo $((DURATION_HOURS * 3600)))
        if [ "$ELAPSED_SEC" -ge "$LIMIT_SEC" ]; then
            echo "🏁 Đã đạt thời gian chạy tối đa (${DURATION_HOURS} giờ). Kết thúc bài test qua đêm thành công!"
            break
        fi
    fi

    echo "⏳ Nghỉ ${SLEEP_BETWEEN_ROUNDS}s trước khi bước vào vòng test tiếp theo..."
    sleep "${SLEEP_BETWEEN_ROUNDS}"
    current_loop=$((current_loop + 1))
done

echo -e "\n=========================================================="
echo "🏆 HOÀN THÀNH XUẤT SẮC BÀI TEST RECOVERY & ZERO-FORK!"
echo "   • Tổng số vòng lặp hoàn thành: ${current_loop}"
echo "   • Đã thử nghiệm luân phiên trên ${#ACTIVE_NODES[@]} nodes: [ ${ACTIVE_NODES[*]} ]"
echo "   • Đã restart toàn bộ cụm và xác nhận đồng thuận tiếp tục hoạt động"
echo "   • Đảm bảo TẤT CẢ các node ĐỀU ĐANG CÒN SỐNG (Đã kiểm tra độc lập)"
echo "   • Đảm bảo 100% Zero-Fork, block hash và state root đồng nhất hoàn toàn!"
echo "=========================================================="
