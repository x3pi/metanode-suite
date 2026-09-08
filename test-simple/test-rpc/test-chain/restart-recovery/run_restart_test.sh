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

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --nodes) SPECIFIED_NODES="$2"; shift ;;
        --nodes=*) SPECIFIED_NODES="${1#*=}" ;;
        --count) TX_COUNT="$2"; shift ;;
        --count=*) TX_COUNT="${1#*=}" ;;
        --duration|--duration=*) shift ;; # Tương thích ngược
        -h|--help)
            echo "Cách dùng: $0 [OPTIONS]"
            echo "  --nodes <0,1,2>        Chỉ định danh sách node cần test (Mặc định: tự động phát hiện node online)"
            echo "  --count <15>           Số lượng giao dịch gửi và xác nhận mỗi chặng"
            exit 0
            ;;
        *) echo "Tham số không hợp lệ: $1"; exit 1 ;;
    esac
    shift
done

echo "=========================================================="
echo "🚀 BẮT ĐẦU BÀI TEST: CLUSTER RESTART & ZERO-FORK RECOVERY"
echo "📂 Thư mục: ${SCRIPT_DIR}"
echo "⚙️  Ansible: ${ANSIBLE_DIR}/ansible_deploy.sh"
echo "🔢 Số TX kiểm chứng mỗi chặng: ${TX_COUNT}"
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

round=1
for node_id in "${ACTIVE_NODES[@]}"; do
    echo -e "\n👉 [CHẶNG 2.${round}] TẮT & BẬT LẠI NODE ${node_id}..."
    
    echo "🔴 Tắt Node ${node_id}..."
    "${ANSIBLE_DIR}/ansible_deploy.sh" --stop --only-node "${node_id}"
    sleep 3

    echo "🟢 Bật lại Node ${node_id}..."
    "${ANSIBLE_DIR}/ansible_deploy.sh" --restart --only-node "${node_id}"

    wait_node_online "${node_id}"

    echo "⚡ [Node ${node_id} vừa thức dậy] Bơm ${TX_COUNT} giao dịch load-balance, kiểm tra sống/chết và Zero-Fork..."
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
"${ANSIBLE_DIR}/ansible_deploy.sh" --stop
echo "⏳ Chờ 5s đảm bảo mọi process đã dừng hẳn..."
sleep 5

echo "🚀 Khởi động lại toàn bộ cụm node..."
"${ANSIBLE_DIR}/ansible_deploy.sh" --restart

wait_all_nodes_online

echo "⚡ [Cả cụm vừa thức dậy] Bơm ${TX_COUNT} giao dịch load-balance, kiểm tra sống/chết và Zero-Fork..."
go run main.go -count "${TX_COUNT}" -check-fork -require-all-alive=true

# ------------------------------------------------------------------------------
# BƯỚC 4: KIỂM TRA SỨC KHỎE TẤT CẢ CÁC NODE SAU TOÀN BỘ BÀI TEST
# ------------------------------------------------------------------------------
echo -e "\n=========================================================="
echo "📡 [BƯỚC 4] KIỂM TRA SỨC KHỎE TẤT CẢ CÁC NODE SAU TOÀN BỘ BÀI TEST"
echo "=========================================================="

python3 -c "
import json, urllib.request, sys

try:
    with open('${CONFIG_PATH}', 'r') as f:
        c = json.load(f)
    rpc_nodes = c.get('rpc_nodes', {})
    dead_nodes = []
    print(f'🔍 Kiểm tra trạng thái {len(rpc_nodes)} node cấu hình:')
    for name, url in rpc_nodes.items():
        try:
            req = urllib.request.Request(url, data=b'{\"jsonrpc\":\"2.0\",\"method\":\"eth_blockNumber\",\"params\":[],\"id\":1}', headers={'Content-Type': 'application/json'})
            with urllib.request.urlopen(req, timeout=3) as resp:
                data = json.loads(resp.read().decode())
                blk = int(data.get('result', '0x0'), 16)
                print(f'   • Node {name} ({url}): 🟢 ALIVE (Block {blk})')
        except Exception as e:
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

echo -e "\n=========================================================="
echo "🏆 HOÀN THÀNH XUẤT SẮC BÀI TEST RECOVERY & ZERO-FORK!"
echo "   • Đã thử nghiệm luân phiên trên ${#ACTIVE_NODES[@]} nodes: [ ${ACTIVE_NODES[*]} ]"
echo "   • Đã restart toàn bộ cụm và xác nhận đồng thuận tiếp tục hoạt động"
echo "   • Đảm bảo TẤT CẢ các node ĐỀU ĐANG CÒN SỐNG (Đã kiểm tra độc lập)"
echo "   • Đảm bảo 100% Zero-Fork, block hash và state root đồng nhất hoàn toàn!"
echo "=========================================================="
