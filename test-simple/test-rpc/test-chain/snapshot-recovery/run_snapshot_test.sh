#!/usr/bin/env bash
# ==============================================================================
# 📸 METANODE SNAPSHOT GENERATION & RESTORE ZERO-FORK RECOVERY TEST
# - Tự động phát hiện các node đang online từ config.json
# - Dò tìm Snapshot Server (HTTP API /api/snapshots)
# - Hỗ trợ chạy nhiều vòng lặp (--loop <N>): Mỗi vòng đợi sinh bản snapshot MỚI
#   và luân phiên khôi phục một Node khác nhau
# - Sử dụng Ansible (--restore-node) để xóa và khôi phục node đích từ snapshot
# - Chờ node đích khởi động lại và đồng bộ catch-up block height với cụm
# - Bơm giao dịch kiểm chứng qua node vừa khôi phục và toàn bộ cụm
# - Đối chiếu 100% Zero-Fork (Block Hash & State Root)
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

SPECIFIED_TARGET_NODE=""
SNAPSHOT_URL=""
TX_COUNT=15
LOOP_COUNT=1
SLEEP_BETWEEN_ROUNDS=10

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --target-node) SPECIFIED_TARGET_NODE="$2"; shift ;;
        --target-node=*) SPECIFIED_TARGET_NODE="${1#*=}" ;;
        --snapshot-url) SNAPSHOT_URL="$2"; shift ;;
        --snapshot-url=*) SNAPSHOT_URL="${1#*=}" ;;
        --count) TX_COUNT="$2"; shift ;;
        --count=*) TX_COUNT="${1#*=}" ;;
        --loop|--loops) LOOP_COUNT="$2"; shift ;;
        --loop=*|--loops=*) LOOP_COUNT="${1#*=}" ;;
        --sleep-between) SLEEP_BETWEEN_ROUNDS="$2"; shift ;;
        --sleep-between=*) SLEEP_BETWEEN_ROUNDS="${1#*=}" ;;
        -h|--help)
            echo "Cách dùng: $0 [OPTIONS]"
            echo "  --target-node <id>     Node ID cần thực hiện khôi phục snapshot (vd: 1, 2)"
            echo "  --snapshot-url <url>   URL của Snapshot Server (vd: http://192.168.1.234:8600)"
            echo "  --count <15>           Số lượng giao dịch kiểm chứng mỗi chặng"
            echo "  --loop <N>             Số vòng lặp test (mỗi vòng chờ snapshot mới và khôi phục luân phiên node)"
            echo "  --sleep-between <sec>  Thời gian nghỉ giữa các vòng (mặc định 10s)"
            exit 0
            ;;
        *) echo "Tham số không hợp lệ: $1"; exit 1 ;;
    esac
    shift
done

echo "=========================================================="
echo "🚀 BẮT ĐẦU BÀI TEST: SNAPSHOT GENERATION & ANSIBLE RESTORE"
echo "📂 Thư mục: ${SCRIPT_DIR}"
echo "⚙️  Ansible: ${ANSIBLE_DIR}/ansible_deploy.sh"
echo "🔢 Số vòng lặp (Loops): ${LOOP_COUNT}"
echo "🔢 Số TX kiểm chứng: ${TX_COUNT}"
echo "=========================================================="

cd "${SCRIPT_DIR}"

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

detect_snapshot_url() {
    python3 -c "
import json, urllib.request, urllib.parse, sys

try:
    with open('${CONFIG_PATH}', 'r') as f:
        c = json.load(f)
    # Ưu tiên kiểm tra sync_nodes (node chuyên tạo snapshot) trước, sau đó tới rpc_nodes
    check_order = []
    for k, u in c.get('sync_nodes', {}).items():
        check_order.append((k, u))
    for k, u in c.get('rpc_nodes', {}).items():
        check_order.append((k, u))

    for k, rpc_url in check_order:
        node_id = k.replace('m', '').replace('node', '')
        parsed = urllib.parse.urlparse(rpc_url)
        ip = parsed.hostname
        ports = [8600 + int(node_id) if node_id.isdigit() else 8600, 8604, 8600, 8700]
        for p in ports:
            url = f'http://{ip}:{p}'
            try:
                with urllib.request.urlopen(f'{url}/api/snapshots', timeout=2) as resp:
                    if resp.status == 200:
                        print(url)
                        sys.exit(0)
            except Exception:
                pass
    sys.exit(1)
except Exception:
    sys.exit(1)
"
}

detect_snapshot_nodes() {
    python3 -c "
import json, sys
try:
    with open('${CONFIG_PATH}', 'r') as f:
        c = json.load(f)
    sync_nodes = c.get('sync_nodes', {})
    snap_ids = [k.replace('m', '').replace('node', '') for k in sync_nodes.keys()]
    print(' '.join(sorted(snap_ids, key=lambda x: int(x) if x.isdigit() else x)))
except Exception:
    print('4')
"
}

detect_validator_nodes() {
    python3 -c "
import json, urllib.request, sys
try:
    with open('${CONFIG_PATH}', 'r') as f:
        c = json.load(f)
    rpc_nodes = c.get('rpc_nodes', {})
    sync_nodes = c.get('sync_nodes', {})
    sync_ids = {k.replace('m', '').replace('node', '') for k in sync_nodes.keys()}
    val_nodes = []
    for k, url in rpc_nodes.items():
        nid = k.replace('m', '').replace('node', '')
        if nid in sync_ids:
            continue
        try:
            req = urllib.request.Request(url, data=b'{\"jsonrpc\":\"2.0\",\"method\":\"eth_blockNumber\",\"params\":[],\"id\":1}', headers={'Content-Type': 'application/json'})
            with urllib.request.urlopen(req, timeout=2) as resp:
                if resp.status == 200:
                    val_nodes.append(nid)
        except Exception:
            pass
    print(' '.join(sorted(val_nodes, key=lambda x: int(x) if x.isdigit() else x)))
except Exception:
    print('0 1 2 3')
"
}

get_latest_snapshot_block() {
    python3 -c "
import urllib.request, json, sys
try:
    with urllib.request.urlopen('${SNAPSHOT_URL}/api/snapshots', timeout=3) as resp:
        if resp.status == 200:
            data = json.loads(resp.read().decode())
            if data and len(data) > 0:
                print(int(data[0].get('block_number', 0)))
                sys.exit(0)
    print(0)
except Exception:
    print(0)
"
}

get_latest_snapshot_desc() {
    python3 -c "
import urllib.request, json, sys
try:
    with urllib.request.urlopen('${SNAPSHOT_URL}/api/snapshots', timeout=3) as resp:
        if resp.status == 200:
            data = json.loads(resp.read().decode())
            if data and len(data) > 0:
                print(f\"{data[0].get('snapshot_name')} (Block #{data[0].get('block_number')}, Epoch {data[0].get('epoch')})\")
                sys.exit(0)
    print('')
except Exception:
    print('')
"
}

wait_node_online() {
    local target="$1"
    local max_wait=90
    local waited=0
    echo -n "⏳ Đang chờ Node ${target} online trở lại sau khi restore (tối đa ${max_wait}s)... "

    while [ $waited -lt $max_wait ]; do
        local online_now=$(detect_online_nodes)
        for n in $online_now; do
            if [ "$n" == "$target" ]; then
                echo "✅ Node ${target} đã ONLINE (sau ${waited}s)!"
                return 0
            fi
        done
        sleep 2
        waited=$((waited + 2))
    done

    echo "❌ HẾT THỜI GIAN CHỜ: Node ${target} không phản hồi RPC sau ${max_wait}s!"
    return 1
}

# CONSENSUS READINESS: node_consensus_ready / wait_node_consensus_ready -- see
# lib/consensus_readiness.sh for why this exists (eth_blockNumber answering
# doesn't mean the Rust consensus layer accepts proposals yet). Extracted out of
# here into the shared lib (was a near-duplicate of restart-recovery's copy,
# commit 829c9fc) so future test scripts source it instead of copy-pasting again.
# Requires CONFIG_PATH (set above) to already point at this test's config.json.
source "${SCRIPT_DIR}/../lib/consensus_readiness.sh"

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

# Gộp toàn bộ các node trong mạng (cả Validator Nodes và SyncOnly Nodes)
all_nodes = dict(cfg.get("rpc_nodes", {}))
all_nodes.update(cfg.get("sync_nodes", {}))

# Loại trừ node đang tắt hoặc đang restore snapshot (nếu có chỉ định)
if exclude_node:
    clean_exc = exclude_node.lower().replace("m", "").replace("node", "")
    for k in list(all_nodes.keys()):
        clean_k = k.lower().replace("m", "").replace("node", "")
        if clean_k == clean_exc or k == exclude_node:
            del all_nodes[k]

print("\n==========================================================")
print(f"🔍 [KIỂM TRA ĐỒNG BỘ CHIỀU CAO & ZERO-FORK] {stage_label}")
if exclude_node:
    print(f"ℹ️  Đang kiểm tra {len(all_nodes)} node (loại trừ Node {exclude_node} đang tắt/restore snapshot)")
else:
    print(f"ℹ️  Đang kiểm tra TOÀN BỘ {len(all_nodes)} node trong cụm (Validator + SyncOnly)")
print(f"⏳ Đang chờ các node đạt chiều cao block bằng nhau (tối đa {timeout_sec}s / 4 phút)...")
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

# Đảm bảo dọn dẹp cờ ignore node khi script kết thúc
trap 'rm -f /tmp/monitors_ignore_nodes 2>/dev/null || true' EXIT

echo "🔍 Đang tự động quét phát hiện các node đang hoạt động..."
DETECTED=$(detect_online_nodes || true)
if [ -z "$DETECTED" ]; then
    echo "❌ Không phát hiện được node nào online từ ${CONFIG_PATH}!"
    exit 1
fi
read -r -a ACTIVE_NODES <<< "$DETECTED"
echo "📋 Danh sách các node đang online: [ ${ACTIVE_NODES[*]} ] (Tổng: ${#ACTIVE_NODES[@]} nodes)"

SNAPSHOT_NODES_STR=$(detect_snapshot_nodes || echo "4")
VALIDATOR_NODES_STR=$(detect_validator_nodes || echo "0 1 2 3")
read -r -a SNAPSHOT_NODES <<< "$SNAPSHOT_NODES_STR"
read -r -a VALIDATOR_NODES <<< "$VALIDATOR_NODES_STR"
echo "📸 Danh sách Node tạo Snapshot (SyncOnly): [ ${SNAPSHOT_NODES[*]} ]"
echo "🛡️  Danh sách Node Validator (Hợp lệ):       [ ${VALIDATOR_NODES[*]} ]"

# Dò tìm Snapshot Server nếu chưa chỉ định
if [ -z "$SNAPSHOT_URL" ]; then
    echo "🔍 Đang dò tìm Snapshot Server từ cụm node..."
    FOUND_URL=$(detect_snapshot_url || true)
    if [ -n "$FOUND_URL" ]; then
        SNAPSHOT_URL="$FOUND_URL"
        echo "✅ Phát hiện Snapshot Server hoạt động tại: ${SNAPSHOT_URL}"
    else
        SYNC_IP=$(python3 -c "import json, urllib.parse; c = json.load(open('${CONFIG_PATH}')); syncs = c.get('sync_nodes', {}); print(urllib.parse.urlparse(list(syncs.values())[0]).hostname if syncs else '')" 2>/dev/null || echo "")
        if [ -n "$SYNC_IP" ]; then
            SNAPSHOT_URL="http://${SYNC_IP}:8604"
        else
            FIRST_IP=$(python3 -c "import json, urllib.parse; c = json.load(open('${CONFIG_PATH}')); print(urllib.parse.urlparse(list(c.get('rpc_nodes', {}).values())[0]).hostname)" 2>/dev/null || echo "192.168.1.234")
            SNAPSHOT_URL="http://${FIRST_IP}:8600"
        fi
        echo "ℹ️  Chưa thấy snapshot server phản hồi, fallback về URL cấu hình: ${SNAPSHOT_URL}"
    fi
fi

# Kiểm tra nếu người dùng truyền target-node là node tạo snapshot: CHẶN NGAY LẬP TỨC
if [ -n "$SPECIFIED_TARGET_NODE" ]; then
    CLEAN_TARGET=$(echo "$SPECIFIED_TARGET_NODE" | sed 's/m//' | sed 's/node//')
    for sn in "${SNAPSHOT_NODES[@]}"; do
        if [ "$CLEAN_TARGET" == "$sn" ]; then
            echo -e "\n❌ [LỖI AN TOÀN] Node ${SPECIFIED_TARGET_NODE} là Node tạo Snapshot (SyncOnly)!"
            echo "   ⚠️ Node tạo snapshot KHÔNG ĐƯỢC PHÉP tự khôi phục chính nó."
            echo "   👉 Vui lòng chỉ chọn các Node Validator để test khôi phục snapshot: [ ${VALIDATOR_NODES[*]} ]"
            exit 1
        fi
    done
fi

# Lọc danh sách candidate nodes: CHỈ CHỌN CÁC VALIDATOR NODES (LOẠI TRỪ SNAPSHOT / SYNCONLY NODE)
CANDIDATE_NODES=()
for n in "${ACTIVE_NODES[@]}"; do
    is_val=false
    for v in "${VALIDATOR_NODES[@]}"; do
        if [ "$n" == "$v" ]; then
            is_val=true
            break
        fi
    done
    if [ "$is_val" == "true" ]; then
        CANDIDATE_NODES+=("$n")
    fi
done

if [ ${#CANDIDATE_NODES[@]} -eq 0 ]; then
    echo "❌ Không tìm thấy Node Validator nào đang online để thực hiện bài test restore snapshot!"
    exit 1
fi
echo "🎯 Danh sách Candidate Nodes (CHỈ Validator Nodes, loại trừ Snapshot Node): [ ${CANDIDATE_NODES[*]} ]"

LAST_SNAPSHOT_BLOCK=0

for round_idx in $(seq 1 $LOOP_COUNT); do
    echo -e "\n=========================================================="
    echo "🔄 [VÒNG LẶP ${round_idx}/${LOOP_COUNT}] BẮT ĐẦU CHU KỲ TEST SNAPSHOT RECOVERY"
    echo "=========================================================="

    # Xác định Target Node cho vòng này
    if [ -n "$SPECIFIED_TARGET_NODE" ]; then
        TARGET_NODE="$SPECIFIED_TARGET_NODE"
    else
        cand_idx=$(( (round_idx - 1) % ${#CANDIDATE_NODES[@]} ))
        TARGET_NODE="${CANDIDATE_NODES[$cand_idx]}"
    fi
    echo "🎯 [Vòng ${round_idx}] Mục tiêu khôi phục dữ liệu snapshot: Node ${TARGET_NODE}"

    # KIỂM CHỨNG ĐẦU ROUND: Đảm bảo toàn bộ node có chiều cao bằng nhau & 100% Zero-Fork trước khi test
    verify_equal_height_and_zero_fork "ĐẦU VÒNG ${round_idx}/${LOOP_COUNT} (Trước khi tạo Snapshot & Restore)" 240

    # BƯỚC 1: ĐẢM BẢO CÓ BẢN SNAPSHOT MỚI
    echo -e "\n📸 [BƯỚC 1/5] Chờ bản Snapshot mới trên server (Yêu cầu Block > ${LAST_SNAPSHOT_BLOCK})..."
    CUR_BLOCK=$(get_latest_snapshot_block)
    if [ "$CUR_BLOCK" -le "$LAST_SNAPSHOT_BLOCK" ]; then
        echo "⏳ Snapshot hiện tại (Block #${CUR_BLOCK}) chưa mới hơn mốc Block #${LAST_SNAPSHOT_BLOCK}."
        echo "   Bắt đầu bơm giao dịch định kỳ để chuỗi tiến tới mốc Snapshot tiếp theo..."
        MAX_WAIT_ROUNDS=40
        for w_round in $(seq 1 $MAX_WAIT_ROUNDS); do
            echo "   👉 [Đợt $w_round/$MAX_WAIT_ROUNDS] Bơm 15 giao dịch kích block & chờ snapshot mới..."
            go run main.go -count 15 -check-fork=false -require-all-alive=false || true
            sleep 4
            CUR_BLOCK=$(get_latest_snapshot_block)
            if [ "$CUR_BLOCK" -gt "$LAST_SNAPSHOT_BLOCK" ]; then
                echo "   🎉 Snapshot mới đã được sinh ra thành công tại Block #${CUR_BLOCK}!"
                break
            fi
        done
    fi

    if [ "$CUR_BLOCK" -le "$LAST_SNAPSHOT_BLOCK" ]; then
        echo "⚠️ Cảnh báo: Chưa xuất hiện bản snapshot mới hơn sau thời gian chờ. Dùng bản hiện có tại Block #${CUR_BLOCK}..."
    else
        LAST_SNAPSHOT_BLOCK="$CUR_BLOCK"
    fi

    SNAP_INFO=$(get_latest_snapshot_desc || true)
    echo "✅ [Vòng ${round_idx}] Bản Snapshot sử dụng: ${SNAP_INFO}"
    go run main.go -snapshot-url "${SNAPSHOT_URL}" -count 0 || true

    # BƯỚC 2: KHÔI PHỤC DỮ LIỆU NODE ĐÍCH BẰNG ANSIBLE
    echo -e "\n=========================================================="
    echo "🔄 [BƯỚC 2/5] KHÔI PHỤC NODE ${TARGET_NODE} TỪ SNAPSHOT ${SNAPSHOT_URL} BẰNG ANSIBLE"
    echo "=========================================================="
    echo "🔕 Tạm thời bỏ qua giám sát Node ${TARGET_NODE} trong thời gian khôi phục snapshot..."
    mkdir -p /tmp
    echo "${TARGET_NODE}" > /tmp/monitors_ignore_nodes

    echo "🚀 Thực thi ansible_deploy.sh với --restore-node ${TARGET_NODE}..."
    "${ANSIBLE_DIR}/ansible_deploy.sh" --only-node "${TARGET_NODE}" --restore-node "${TARGET_NODE}" --snapshot-url "${SNAPSHOT_URL}"

    # BƯỚC 3: CHỜ NODE ĐÍCH ONLINE VÀ ĐỒNG BỘ CATCH-UP
    echo -e "\n=========================================================="
    echo "⏳ [BƯỚC 3/5] CHỜ NODE ${TARGET_NODE} ONLINE VÀ ĐỒNG BỘ CATCH-UP VỚI CỤM"
    echo "=========================================================="
    wait_node_online "${TARGET_NODE}"

    echo "🔔 Bật lại giám sát cho Node ${TARGET_NODE} sau khi đã online..."
    rm -f /tmp/monitors_ignore_nodes 2>/dev/null || true

    echo "📡 Kiểm tra đồng bộ catch-up của Node ${TARGET_NODE}..."
    go run main.go -wait-sync-node "${TARGET_NODE}" -max-lag 5 -count 0

    wait_node_consensus_ready "${TARGET_NODE}"

    # BƯỚC 4: BƠM GIAO DỊCH QUA CHÍNH NODE VỪA KHÔI PHỤC
    echo -e "\n=========================================================="
    echo "⚡ [BƯỚC 4/5] BƠM GIAO DỊCH TRỰC TIẾP QUA NODE ${TARGET_NODE} VỪA KHÔI PHỤC"
    echo "=========================================================="
    go run main.go -target-node "${TARGET_NODE}" -count "${TX_COUNT}" -check-fork=false

    # BƯỚC 5: KIỂM CHỨNG TOÀN DIỆN TOÀN CỤM & 100% ZERO-FORK
    echo -e "\n=========================================================="
    echo "🏆 [BƯỚC 5/5] KIỂM TRA SỨC KHỎE CẢ CỤM & XÁC NHẬN 100% ZERO-FORK"
    echo "=========================================================="
    go run main.go -count "${TX_COUNT}" -check-fork=true -require-all-alive=true

    # KIỂM CHỨNG CUỐI ROUND: Đảm bảo toàn bộ node (bao gồm node vừa khôi phục) đã hội tụ bằng nhau & Zero-Fork
    verify_equal_height_and_zero_fork "CUỐI VÒNG ${round_idx}/${LOOP_COUNT} (Sau khi khôi phục Node ${TARGET_NODE})" 240

    echo -e "\n🎉 [VÒNG ${round_idx}/${LOOP_COUNT}] HOÀN THÀNH XUẤT SẮC: Node ${TARGET_NODE} đã phục hồi và Zero-Fork!"

    if [ "$round_idx" -lt "$LOOP_COUNT" ]; then
        echo "⏸ Nghỉ ${SLEEP_BETWEEN_ROUNDS}s trước khi bước sang vòng tiếp theo..."
        sleep "$SLEEP_BETWEEN_ROUNDS"
    fi
done

echo -e "\n=========================================================="
echo "🎉 TẤT CẢ ${LOOP_COUNT} VÒNG TEST SNAPSHOT RECOVERY ĐÃ HOÀN TẤT THÀNH CÔNG!"
echo "   • Toàn bộ các node được kiểm tra khôi phục từ các bản snapshot mới"
echo "   • Các node đều sync catch-up chuẩn xác và xử lý giao dịch ổn định"
echo "   • Toàn bộ cụm node đạt chuẩn 100% Zero-Fork (Hash & StateRoot đồng nhất)!"
echo "=========================================================="
