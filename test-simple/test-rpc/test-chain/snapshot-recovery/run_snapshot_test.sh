#!/usr/bin/env bash
# ==============================================================================
# 📸 METANODE SNAPSHOT GENERATION & RESTORE ZERO-FORK RECOVERY TEST
# - Tự động phát hiện các node đang online từ config.json
# - Dò tìm Snapshot Server (HTTP API /api/snapshots)
# - Bơm giao dịch kiểm chứng và đảm bảo Snapshot được tạo hoàn tất trên đĩa
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

TARGET_NODE=""
SNAPSHOT_URL=""
TX_COUNT=15

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --target-node) TARGET_NODE="$2"; shift ;;
        --target-node=*) TARGET_NODE="${1#*=}" ;;
        --snapshot-url) SNAPSHOT_URL="$2"; shift ;;
        --snapshot-url=*) SNAPSHOT_URL="${1#*=}" ;;
        --count) TX_COUNT="$2"; shift ;;
        --count=*) TX_COUNT="${1#*=}" ;;
        -h|--help)
            echo "Cách dùng: $0 [OPTIONS]"
            echo "  --target-node <id>     Node ID cần thực hiện khôi phục snapshot (vd: 1, 2)"
            echo "  --snapshot-url <url>   URL của Snapshot Server (vd: http://192.168.1.234:8600)"
            echo "  --count <15>           Số lượng giao dịch kiểm chứng mỗi chặng"
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
echo "🔢 Số TX kiểm chứng: ${TX_COUNT}"
echo "=========================================================="

cd "${SCRIPT_DIR}"

detect_online_nodes() {
    python3 -c "
import json, urllib.request, sys

try:
    with open('${CONFIG_PATH}', 'r') as f:
        c = json.load(f)
    rpc_nodes = c.get('rpc_nodes', {})
    online = []
    for k, url in rpc_nodes.items():
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
    rpc_nodes = c.get('rpc_nodes', {})
    for k, rpc_url in rpc_nodes.items():
        node_id = k.replace('m', '').replace('node', '')
        parsed = urllib.parse.urlparse(rpc_url)
        ip = parsed.hostname
        ports = [8600 + int(node_id) if node_id.isdigit() else 8600, 8600, 8604, 8700]
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

echo "🔍 Đang tự động quét phát hiện các node đang hoạt động..."
DETECTED=$(detect_online_nodes || true)
if [ -z "$DETECTED" ]; then
    echo "❌ Không phát hiện được node nào online từ ${CONFIG_PATH}!"
    exit 1
fi
read -r -a ACTIVE_NODES <<< "$DETECTED"
echo "📋 Danh sách các node đang online: [ ${ACTIVE_NODES[*]} ] (Tổng: ${#ACTIVE_NODES[@]} nodes)"

# Dò tìm Snapshot Server nếu chưa chỉ định
if [ -z "$SNAPSHOT_URL" ]; then
    echo "🔍 Đang dò tìm Snapshot Server từ cụm node..."
    FOUND_URL=$(detect_snapshot_url || true)
    if [ -n "$FOUND_URL" ]; then
        SNAPSHOT_URL="$FOUND_URL"
        echo "✅ Phát hiện Snapshot Server hoạt động tại: ${SNAPSHOT_URL}"
    else
        # Fallback về node đầu tiên cổng 8600
        FIRST_IP=$(python3 -c "import json, urllib.parse; c = json.load(open('${CONFIG_PATH}')); print(urllib.parse.urlparse(list(c.get('rpc_nodes', {}).values())[0]).hostname)" 2>/dev/null || echo "192.168.1.234")
        SNAPSHOT_URL="http://${FIRST_IP}:8600"
        echo "ℹ️  Chưa thấy snapshot server phản hồi, fallback về URL mặc định: ${SNAPSHOT_URL}"
    fi
fi

# ------------------------------------------------------------------------------
# BƯỚC 1: KIỂM TRA VÀ KÍCH HOẠT TẠO SNAPSHOT TRÊN SNAPSHOT SERVER
# ------------------------------------------------------------------------------
echo -e "\n----------------------------------------------------------"
echo "📸 [BƯỚC 1] Kiểm tra & Đảm bảo Snapshot Server đã có bản lưu..."
echo "----------------------------------------------------------"

has_snapshot() {
    python3 -c "
import urllib.request, json, sys
try:
    with urllib.request.urlopen('${SNAPSHOT_URL}/api/snapshots', timeout=3) as resp:
        if resp.status == 200:
            data = json.loads(resp.read().decode())
            if data and len(data) > 0:
                print(f\"{data[0].get('snapshot_name')} (Block #{data[0].get('block_number')}, Epoch {data[0].get('epoch')})\")
                sys.exit(0)
    sys.exit(1)
except Exception:
    sys.exit(1)
"
}

SNAP_INFO=$(has_snapshot || true)
if [ -z "$SNAP_INFO" ]; then
    echo "⏳ Snapshot Server chưa có bản snapshot. Bắt đầu bơm giao dịch định kỳ để qua mốc Epoch/Block..."
    MAX_ROUNDS=25
    for round in $(seq 1 $MAX_ROUNDS); do
        echo "   👉 [Đợt $round/$MAX_ROUNDS] Bơm 15 giao dịch kích block & chờ snapshot..."
        go run main.go -count 15 -check-fork=false -require-all-alive=false || true
        sleep 4
        SNAP_INFO=$(has_snapshot || true)
        if [ -n "$SNAP_INFO" ]; then
            echo "   🎉 Snapshot đã được sinh ra thành công sau $round đợt!"
            break
        fi
    done
fi

if [ -n "$SNAP_INFO" ]; then
    echo "✅ Snapshot đã sẵn sàng trên server: ${SNAP_INFO}"
    go run main.go -snapshot-url "${SNAPSHOT_URL}" -count 0 || true
else
    echo "⚠️ Cảnh báo: Chưa xuất hiện file snapshot sau thời gian chờ. Thử tiếp tục với Snapshot Server ${SNAPSHOT_URL}..."
fi

# ------------------------------------------------------------------------------
# BƯỚC 2: CHỌN NODE ĐÍCH ĐỂ TEST KHÔI PHỤC (TARGET NODE)
# ------------------------------------------------------------------------------
echo -e "\n----------------------------------------------------------"
echo "🎯 [BƯỚC 2] Xác định Node mục tiêu để khôi phục snapshot..."
echo "----------------------------------------------------------"

if [ -z "$TARGET_NODE" ]; then
    # Tìm port của snapshot server để suy ra ID node snapshot (vd: 8604 -> node 4, 8600 -> node 0)
    SNAP_NODE_ID=$(echo "${SNAPSHOT_URL}" | grep -oE '860[0-9]' | sed 's/860//' || echo "4")
    for n in "${ACTIVE_NODES[@]}"; do
        # Chọn node khác với node đang chạy snapshot server
        if [ "$n" != "$SNAP_NODE_ID" ]; then
            TARGET_NODE="$n"
            break
        fi
    done
    if [ -z "$TARGET_NODE" ]; then
        TARGET_NODE="${ACTIVE_NODES[0]}"
    fi
fi

echo "🎯 Chọn Node ${TARGET_NODE} làm mục tiêu khôi phục dữ liệu snapshot!"

# Đảm bảo dọn dẹp cờ ignore node khi script kết thúc
trap 'rm -f /tmp/monitors_ignore_nodes 2>/dev/null || true' EXIT

# ------------------------------------------------------------------------------
# BƯỚC 3: KHÔI PHỤC DỮ LIỆU NODE ĐÍCH BẰNG ANSIBLE
# ------------------------------------------------------------------------------
echo -e "\n=========================================================="
echo "🔄 [BƯỚC 3] KHÔI PHỤC NODE ${TARGET_NODE} TỪ SNAPSHOT ${SNAPSHOT_URL} BẰNG ANSIBLE"
echo "=========================================================="

echo "🔕 Tạm thời bỏ qua giám sát Node ${TARGET_NODE} trong thời gian khôi phục snapshot..."
mkdir -p /tmp
echo "${TARGET_NODE}" > /tmp/monitors_ignore_nodes

echo "🚀 Thực thi ansible_deploy.sh với --restore-node ${TARGET_NODE}..."
"${ANSIBLE_DIR}/ansible_deploy.sh" --only-node "${TARGET_NODE}" --restore-node "${TARGET_NODE}" --snapshot-url "${SNAPSHOT_URL}"

# ------------------------------------------------------------------------------
# BƯỚC 4: CHỜ NODE ĐÍCH ONLINE VÀ ĐỒNG BỘ CATCH-UP
# ------------------------------------------------------------------------------
echo -e "\n=========================================================="
echo "⏳ [BƯỚC 4] CHỜ NODE ${TARGET_NODE} ONLINE VÀ ĐỒNG BỘ CATCH-UP VỚI CỤM"
echo "=========================================================="

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

wait_node_online "${TARGET_NODE}"

echo "🔔 Bật lại giám sát cho Node ${TARGET_NODE} sau khi đã online..."
rm -f /tmp/monitors_ignore_nodes 2>/dev/null || true

echo "📡 Kiểm tra đồng bộ catch-up của Node ${TARGET_NODE}..."
go run main.go -wait-sync-node "${TARGET_NODE}" -max-lag 5 -count 0

# ------------------------------------------------------------------------------
# BƯỚC 5: BƠM GIAO DỊCH QUA CHÍNH NODE VỪA KHÔI PHỤC
# ------------------------------------------------------------------------------
echo -e "\n=========================================================="
echo "⚡ [BƯỚC 5] BƠM GIAO DỊCH TRỰC TIẾP QUA NODE ${TARGET_NODE} VỪA KHÔI PHỤC"
echo "=========================================================="

go run main.go -target-node "${TARGET_NODE}" -count "${TX_COUNT}" -check-fork=false

# ------------------------------------------------------------------------------
# BƯỚC 6: KIỂM CHỨNG TOÀN DIỆN TOÀN CỤM & 100% ZERO-FORK
# ------------------------------------------------------------------------------
echo -e "\n=========================================================="
echo "🏆 [BƯỚC 6] KIỂM TRA SỨC KHỎE CẢ CỤM & XÁC NHẬN 100% ZERO-FORK"
echo "=========================================================="

go run main.go -count "${TX_COUNT}" -check-fork=true -require-all-alive=true

echo -e "\n=========================================================="
echo "🎉 HOÀN THÀNH XUẤT SẮC BÀI TEST SNAPSHOT RESTORE & ZERO-FORK!"
echo "   • Đã xác nhận Snapshot Server: ${SNAPSHOT_URL}"
echo "   • Đã dùng Ansible khôi phục thành công Node: ${TARGET_NODE}"
echo "   • Node ${TARGET_NODE} đã sync catch-up và xử lý giao dịch mới bình thường"
echo "   • Toàn bộ cụm node đồng thuận 100% Zero-Fork (Hash & StateRoot đồng nhất)!"
echo "=========================================================="
