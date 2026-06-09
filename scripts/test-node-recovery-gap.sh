#!/bin/bash
set -euo pipefail

# Giới hạn tài nguyên biên dịch để tránh OOM trong môi trường tài nguyên hạn chế
export GOMAXPROCS=2
export CARGO_BUILD_JOBS=2

# Unset exported cd function to avoid environment conflicts
unset -f cd 2>/dev/null || true


# Khởi tạo giá trị mặc định
TARGET_NODE_FIXED=""
TEST_ALL_ONLY=false
POSITIONAL_ARGS=()

# Lặp qua tất cả tham số để parse flag
while [[ "$#" -gt 0 ]]; do
    case $1 in
        --all-only) TEST_ALL_ONLY=true ;;
        --target-node) TARGET_NODE_FIXED="$2"; shift ;;
        --mode) export DEPLOY_MODE="$2"; shift ;;
        -*) echo "Unknown option: $1" ;;
        *) POSITIONAL_ARGS+=("$1") ;;
    esac
    shift || true
done

# Restore positional arguments
set -- "${POSITIONAL_ARGS[@]:-}"

# Tham số (mặc định: node=1, gap=1, loop=20000)
# Nếu TARGET_NODE_FIXED được truyền qua flag, nó sẽ ghi đè $1
NODE_ID=${TARGET_NODE_FIXED:-${1:-1}}
GAP_EPOCH=${2:-1}
LOOP_COUNT=${3:-20000}

# Chế độ triển khai
DEPLOY_MODE="${DEPLOY_MODE:-single}"

# Đường dẫn động để chạy được trên nhiều máy
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"
ORCH_SCRIPT="$ROOT_DIR/metanode/consensus/metanode/scripts/mtn-orchestrator.sh"
SYSTEMD_DEPLOY_SCRIPT="$ROOT_DIR/metanode/consensus/metanode/scripts/node/deploy_systemd_cluster.sh"
SYSTEMD_ENV="deploy-muti-node.env"
RPC_TCP_SCRIPT="$SCRIPT_DIR/rpc-tcp-simple.sh"
HASH_CHECKER_DIR="$(dirname "$SCRIPT_DIR")/block/block_hash_checker"

HISTORY_CONFIG="config-local.json"
HASH_CONFIG_ARG=""
TPS_CONFIG_ARG=""
if [ "${DEPLOY_MODE:-single}" == "multi" ]; then
    HISTORY_CONFIG="config-mutil.json"
    HASH_CONFIG_ARG="--config config-m-nodes.json"
    TPS_CONFIG_ARG="--config config-multi.json"
fi

# Hàm gọi orchestrator tùy theo chế độ
run_orch() {
    local cmd=$1
    local node=${2:-}
    if [ "$DEPLOY_MODE" == "multi" ]; then
        local base_cmd="$SYSTEMD_DEPLOY_SCRIPT --env $SYSTEMD_ENV"
        case "$cmd" in
            "start") $base_cmd --start --keep-data ;;
            "stop") $base_cmd --stop ;;
            "start-node") $base_cmd --start --keep-data --only-node "$node" ;;
            "stop-node") $base_cmd --stop --only-node "$node" ;;
            *) echo "Unknown multi mode orch cmd: $cmd"; exit 1 ;;
        esac
    else
        if [ -n "$node" ]; then
            $ORCH_SCRIPT "$cmd" "$node"
        else
            $ORCH_SCRIPT "$cmd"
        fi
    fi
}

# Hàm kiểm tra và giải phóng các cổng bị chiếm giữ
wait_for_ports_to_release() {
    local ports=("$@")
    echo "🔍 Đang kiểm tra và chờ giải phóng các cổng: ${ports[*]}..."
    local start_time=$(date +%s)
    local timeout=15
    
    while true; do
        local busy_ports=()
        for port in "${ports[@]}"; do
            if ss -tlnp 2>/dev/null | grep -qE ":$port\s"; then
                busy_ports+=("$port")
            fi
        done
        
        if [ ${#busy_ports[@]} -eq 0 ]; then
            echo "✅ Tất cả các cổng đã được giải phóng hoàn toàn!"
            return 0
        fi
        
        local current_time=$(date +%s)
        local elapsed=$((current_time - start_time))
        if [ $elapsed -ge $timeout ]; then
            echo "⚠️ Cảnh báo: Các cổng vẫn bị chiếm giữ sau ${timeout}s: ${busy_ports[*]}"
            for port in "${busy_ports[@]}"; do
                local pids=$(ss -tlnp 2>/dev/null | grep -E ":$port " | grep -oP 'pid=\K[0-9]+' | sort -u || true)
                if [ -n "$pids" ]; then
                    for p in $pids; do
                        echo "  → Force killing PID $p occupying port $port..."
                        kill -9 "$p" 2>/dev/null || true
                    done
                fi
            done
            sleep 1
            return 0
        fi
        
        echo "  → Các cổng đang bị giữ: ${busy_ports[*]}. Chờ giải phóng..."
        sleep 0.5
    done
}

# Dọn dẹp tiến trình giám sát hoặc spam cũ đang chạy ngầm để tránh xung đột
echo "🧹 Đang dọn dẹp các tiến trình nền cũ..."
pkill -f "go run main.go --watch" || true
pkill -f "exe/main --watch" || true
pkill -f "rpc-tcp-simple.sh" || true
pkill -f "test-rpc.*main.go" || true
pkill -f "test-tcp.*main-no-none.go" || true
pkill -f "main.go --count 20000" || true

# Đảm bảo giải phóng các cổng của metanode trước khi chạy test
wait_for_ports_to_release 8545 8757 10747 10748 10749 10750 9100 9101 9102 9103 9104 19200 19201 19202 19203 19204 8547 8548 8549 8550

# Xóa cờ lỗi cũ trước khi chạy
rm -f /tmp/MTN_CHAIN_ERROR_STOP /tmp/pending_check_*.json 2>/dev/null || true
rm -f /tmp/MTN_INTEGRITY_FAILED 2>/dev/null || sudo rm -f /tmp/MTN_INTEGRITY_FAILED 2>/dev/null || true

# Xác định port RPC của Node 0 theo DEPLOY_MODE
# single mode: 8757 (direct port của node 0)
# multi mode: đọc từ /tmp/rpc_nodes.json, nếu không có thì fallback 8545
if [ "${DEPLOY_MODE:-single}" == "multi" ]; then
    if [ -f /tmp/rpc_nodes.json ]; then
        EPOCH_RPC=$(jq -r '.nodes.m0 // empty' /tmp/rpc_nodes.json 2>/dev/null || echo "http://127.0.0.1:8545")
    else
        EPOCH_RPC="http://127.0.0.1:8545"
    fi
else
    EPOCH_RPC="http://127.0.0.1:8757"
fi

# Xác định danh sách RESTORE_NODES tham gia test
if [ "${DEPLOY_MODE:-single}" == "multi" ]; then
    CONFIG_JSON="$(dirname "$(dirname "$SCRIPT_DIR")")/metanode-suite/test_tps/tps_blast_cc/config-multi.json"
    if [ -f "$CONFIG_JSON" ]; then
        CONFIG_IDS=($(jq -r 'keys[]' "$CONFIG_JSON" | grep -E '^rpc_[0-9]+$' | sed 's/rpc_//' | sort -n))
    else
        CONFIG_IDS=(0 1 2 3)
    fi
    if [ -f "/tmp/rpc_nodes.json" ]; then
        ACTIVE_IDS=($(jq -r '.nodes | keys[]' /tmp/rpc_nodes.json | sed 's/m//' | sort -n))
    else
        ACTIVE_IDS=(0 1 2 3 4)
    fi
    RESTORE_NODES=()
    for id in "${ACTIVE_IDS[@]}"; do
        for cid in "${CONFIG_IDS[@]}"; do
            if [ "$id" = "$cid" ]; then
                RESTORE_NODES+=("$id")
                break
            fi
        done
    done
    if [ ${#RESTORE_NODES[@]} -eq 0 ]; then
        RESTORE_NODES=("${ACTIVE_IDS[@]}")
    fi
else
    RESTORE_NODES=(1 2 3 4)
fi
echo "📋 Danh sách các node tham gia test recovery: ${RESTORE_NODES[*]}"

# Hàm lấy epoch hiện tại từ m0 (Node 0 luôn chạy)
get_current_epoch() {
    # Gọi RPC và lấy epoch dưới dạng hex
    hex_epoch=$(curl -s --max-time 2 -X POST "$EPOCH_RPC" \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest",false],"id":1}' \
        | grep -oP '"epoch":"\K(0x[0-9a-fA-F]+)' || echo "")
    
    if [ -z "$hex_epoch" ] || [ "$hex_epoch" = "0x0" ] || [ "$hex_epoch" = "0x" ]; then
        echo "0"
    else
        # Chuyển Hex -> Decimal an toàn
        printf "%d\n" "$hex_epoch" 2>/dev/null || echo "0"
    fi
}

# Hàm chờ mạng thiết lập đồng thuận bằng cách kiểm tra tất cả các node có cùng chiều cao block
wait_for_consensus() {
    echo "⏳ Đang kiểm tra chiều cao block trên tất cả các node..."
    # multi mode: đọc URLs từ /tmp/rpc_nodes.json; single mode: dùng direct ports cố định
    local rpc_urls=()
    if [ "${DEPLOY_MODE:-single}" == "multi" ] && [ -f /tmp/rpc_nodes.json ]; then
        while IFS= read -r url; do
            rpc_urls+=("$url")
        done < <(jq -r '.nodes | to_entries | sort_by(.key) | .[].value' /tmp/rpc_nodes.json)
    else
        local ports=(8757 10747 10749 10750 10748)
        for p in "${ports[@]}"; do rpc_urls+=("http://127.0.0.1:$p"); done
    fi
    
    # Thử tối đa 120 giây (120 lần thử, mỗi lần cách nhau 1s)
    for ((r=1; r<=120; r++)); do
        local all_same=true
        local first_block=""
        local blocks_info=""
        
        for i in "${!rpc_urls[@]}"; do
            local url="${rpc_urls[$i]}"
            local block_hex
            block_hex=$(curl -s --max-time 1 -X POST "$url" \
                -H "Content-Type: application/json" \
                -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
                | grep -oP '"result":"\K(0x[0-9a-fA-F]+)' || echo "")
            
            local current_block=-1
            if [ -n "$block_hex" ] && [ "$block_hex" != "0x" ]; then
                current_block=$(printf "%d\n" "$block_hex" 2>/dev/null || echo "-1")
            fi
            
            blocks_info+="m$i=$current_block "
            
            if [ "$current_block" -eq -1 ]; then
                all_same=false
            else
                if [ -z "$first_block" ]; then
                    first_block=$current_block
                elif [ "$current_block" -ne "$first_block" ]; then
                    all_same=false
                fi
            fi
        done
        
        if [ "$all_same" = true ] && [ -n "$first_block" ]; then
            echo "✅ Đồng thuận xác nhận! Tất cả 5 node đều ở cùng chiều cao block: $first_block"
            return 0
        fi
        
        echo "   ... [Lần $r] Trạng thái block: $blocks_info. Đang chờ đồng bộ..."
        sleep 1
    done
    echo "❌ LỖI: Các node không đồng bộ được chiều cao block với nhau sau 120 giây. Trạng thái cuối: $blocks_info. Dừng script để kiểm tra mạng!"
    return 1
}

# Hàm chờ và kiểm tra trạng thái khởi động chính xác của từng node (không dựa vào timeout 2s mù)
wait_for_node_startup() {
    local node_id=$1
    local log_file="$ROOT_DIR/metanode/consensus/metanode/logs/node_${node_id}/go-master-stdout.log"
    echo "🔍 Đang kiểm tra trạng thái khởi động chính xác của Node ${node_id}..."
    
    # Thăm dò tối đa 50 lần, mỗi lần cách nhau 100ms (tổng tối đa 5 giây)
    for ((r=1; r<=50; r++)); do
        # 1. Nếu sentinel file báo lỗi integrity xuất hiện, báo lỗi ngay lập tức
        if [ -f /tmp/MTN_INTEGRITY_FAILED ]; then
            echo "❌ [ERROR] Phát hiện lỗi integrity check của Node ${node_id}!"
            return 1
        fi
        
        # 2. Nếu tmux session biến mất, nghĩa là process đã crash trước cả khi ghi log hoặc tmux bị lỗi
        if ! tmux ls 2>/dev/null | grep -q "go-master-${node_id}"; then
            echo "❌ [ERROR] Tmux session go-master-${node_id} không tồn tại!"
            return 1
        fi
        
        # 3. Nếu log file tồn tại, kiểm tra xem có dấu hiệu khởi động thành công hoặc lỗi nghiêm trọng không
        if [ -f "$log_file" ]; then
            if grep -E -q "Consensus core started|Starting consensus|Starting peer synchronization" "$log_file"; then
                echo "✅ [SUCCESS] Node ${node_id} đã khởi động thành công sau $((r * 100))ms!"
                return 0
            fi
            if grep -E -q "CRITICAL ERROR|CRITICAL: DATA INTEGRITY CHECK FAILED" "$log_file"; then
                echo "❌ [ERROR] Node ${node_id} báo lỗi CRITICAL trong log!"
                return 1
            fi
        fi
        
        # 4. Kiểm tra xem tiến trình thực sự có chạy không
        if ! pgrep -f "simple_chain.*config-master-node${node_id}" >/dev/null; then
            # Đợi thêm một tí xem có ghi log lỗi gì không
            sleep 0.1
            if ! pgrep -f "simple_chain.*config-master-node${node_id}" >/dev/null; then
                echo "❌ [ERROR] Tiến trình simple_chain cho Node ${node_id} không chạy!"
                return 1
            fi
        fi
        
        sleep 0.1
    done
    
    echo "⚠️ Cảnh báo: Hết thời gian chờ 5s nhưng chưa xác định được trạng thái Node ${node_id}"
    return 0
}



# Hàm dọn dẹp tiến trình spam
stop_spam() {
    echo "🛑 Dừng các tiến trình spam giao dịch và giám sát..."
    pkill -f "rpc-tcp-simple.sh" || true
    pkill -f "test-rpc.*main.go" || true
    pkill -f "test-tcp.*main-no-none.go" || true
    pkill -f "tps_blast_cc.*main.go" || true
    pkill -f "main.go --count" || true
    pkill -f "go run main.go --watch" || true
    pkill -f "exe/main --watch" || true
    
    # Chờ động các tiến trình trên thực sự dừng hẳn (tối đa 5 giây, thăm dò mỗi 100ms)
    local elapsed=0
    while [ $elapsed -lt 50 ]; do
        if ! pgrep -f "rpc-tcp-simple.sh|test-rpc.*main.go|test-tcp.*main-no-none.go|tps_blast_cc.*main.go|main.go --count|go run main.go --watch|exe/main --watch" >/dev/null 2>&1; then
            break
        fi
        sleep 0.1
        elapsed=$((elapsed + 1))
    done
}

# Đảm bảo dọn dẹp khi script bị ngắt
cleanup() {
    err=$?
    trap - EXIT INT TERM
    echo -e "\n[CLEANUP] Đang thoát test với mã lỗi $err..."
    stop_spam
    if [ $err -eq 0 ]; then
        rm -f /tmp/pending_check_*.json /tmp/MTN_INTEGRITY_FAILED /tmp/MTN_EXCLUDE_NODES
    else
        echo "💡 [DEBUG] Giữ lại file checkpoint /tmp/pending_check_*.json để debug offline!"
    fi
    [ -n "${MONITOR_PID:-}" ] && kill "$MONITOR_PID" 2>/dev/null || true
    exit $err
}
trap cleanup EXIT INT TERM

# Theo dõi cờ lỗi từ Hash Checker ngầm & Giám sát sự sống của Node
monitor_error_flag() {
    local loop_count=0
    local consecutive_failures=0
    while true; do
        if [ -f /tmp/MTN_CHAIN_ERROR_STOP ]; then
            echo -e "\n\n🛑 PHÁT HIỆN LỖI NGHIÊM TRỌNG: /tmp/MTN_CHAIN_ERROR_STOP đã được tạo!"
            echo "Nội dung lỗi:"
            cat /tmp/MTN_CHAIN_ERROR_STOP
            echo -e "\n🛑 Dừng toàn bộ bài test ngay lập tức..."
            # Dọn dẹp và kill toàn bộ Process Group của script này
            stop_spam
            kill -TERM -$$
            exit 1
        fi
        
        # Kiểm tra sự sống của các node mỗi 2 giây (10 chu kỳ của sleep 0.2s)
        if [ $((loop_count % 10)) -eq 0 ]; then
            local check_failed=false
            local error_msg=""
            
            if [ "${DEPLOY_MODE:-single}" == "multi" ]; then
                if [ -f /tmp/rpc_nodes.json ]; then
                    while read -r node_key node_url; do
                        local node_id="${node_key#m}"
                        local is_excluded=false
                        if [ -f /tmp/MTN_EXCLUDE_NODES ]; then
                            if grep -qE "(^|,)${node_id}(,|$)" /tmp/MTN_EXCLUDE_NODES; then
                                is_excluded=true
                            fi
                        fi
                        if [ "$is_excluded" = false ]; then
                            if ! curl -s -m 2 "$node_url" >/dev/null 2>&1; then
                                check_failed=true
                                error_msg="Node HTTP Server ($node_key: $node_url) không phản hồi. Có thể process đã bị crash!"
                                break
                            fi
                        fi
                    done < <(jq -r '.nodes | to_entries[] | "\(.key) \(.value)"' /tmp/rpc_nodes.json 2>/dev/null || true)
                fi
            else
                local ports=(8757 10747 10749 10750 10748)
                for node_id in 0 1 2 3 4; do
                    local is_excluded=false
                    if [ -f /tmp/MTN_EXCLUDE_NODES ]; then
                        if grep -qE "(^|,)${node_id}(,|$)" /tmp/MTN_EXCLUDE_NODES; then
                            is_excluded=true
                        fi
                    fi
                    if [ "$is_excluded" = false ]; then
                        local port="${ports[$node_id]}"
                        if ! curl -s -m 2 "http://127.0.0.1:$port" >/dev/null 2>&1; then
                            check_failed=true
                            error_msg="Node HTTP Server (Node $node_id tại cổng $port) không phản hồi. Có thể process đã bị crash!"
                            break
                        fi
                    fi
                done
            fi
            
            if [ "$check_failed" = true ]; then
                consecutive_failures=$((consecutive_failures + 1))
                if [ "$consecutive_failures" -ge 3 ]; then
                    echo "$error_msg" > /tmp/MTN_CHAIN_ERROR_STOP
                fi
            else
                consecutive_failures=0
            fi
        fi

        loop_count=$((loop_count + 1))
        sleep 0.2
    done
}

MONITOR_PID=""

# Hàm phân tích lỗi tự động
analyze_mismatch() {
    target="${1:-unknown}"
    log_file="$HASH_CHECKER_DIR/hash_mismatch_alert.log"
    echo -e "\n📊 [PHÂN TÍCH NHANH LỖI RECOVERY] (Đang test Node $target)"
    echo "--------------------------------------------------------"
    if [ ! -f "$log_file" ]; then
        echo "✅ Không tìm thấy file báo lỗi. Mạng hoàn toàn đồng bộ!"
        echo "--------------------------------------------------------"
        return
    fi
    
    mismatches=$(grep -c "⚠️  Block" "$log_file" || true)
    echo "🚨 Phân tích từ log: Phát hiện $mismatches blocks bị lệch hash!"
    
    # Phân tích nguyên nhân
    if grep -q "gei=" "$log_file" || grep -q "epoch=" "$log_file"; then
        echo "⚠️ Lỗi gán sai Context (GEI/Epoch): Node có dấu hiệu lấy Epoch/GEI của thời điểm hiện tại đắp ngược vào Block quá khứ."
    fi
    
    if grep -q "time=" "$log_file" || grep -q "miner=" "$log_file"; then
        echo "⚠️ Lỗi mạo danh Block: Node có dấu hiệu tự tạo block rỗng (khác thời gian, khác miner) thay vì tải về Block gốc của mạng."
    fi
 
    echo ""
    echo "🔎 [TRÍCH XUẤT NHANH MẪU LỖI ĐẦU TIÊN]"
    grep -A 6 "⚠️  Block" "$log_file" | head -n 7
    echo "--------------------------------------------------------"
    echo "💡 KHUYẾN NGHỊ: Hãy kiểm tra logic import_block qua FFI hoặc cơ chế P2P Sync."
    echo "   Block tải về phải được giữ nguyên vẹn Header lịch sử (không tự generate lại Timestamp, GEI, Epoch)."
    echo "--------------------------------------------------------"
 
    if [ "$mismatches" -ge 100 ]; then
        echo -e "\n🛑 LỖI NGHIÊM TRỌNG (Đang test Node $target): Phát hiện >= 100 block bị lệch hash! Dừng script ngay lập tức để kiểm tra!"
        exit 1
    elif [ "$mismatches" -gt 0 ]; then
        echo -e "\n🛑 LỖI (Đang test Node $target): Phát hiện $mismatches block bị lệch hash! Dừng script để kiểm tra!"
        exit 1
    fi
}
 
echo "========================================================="
echo "🧪 TEST RECOVERY NODE (Node: $NODE_ID, Gap: $GAP_EPOCH epochs, Loop: $LOOP_COUNT lần)"
echo "========================================================="
 
echo "🔄 Đang chạy Simple Test (Bao gồm Setup và Test Cơ Bản)..."
bash "$SCRIPT_DIR/simple_test.sh" --mode "${DEPLOY_MODE:-single}"
echo "✅ Khởi tạo và Simple Test hoàn tất!"

# Khởi chạy monitor sau khi cụm node đã khởi tạo xong để tránh race condition
monitor_error_flag &
MONITOR_PID=$!

for ((loop=1; loop<=LOOP_COUNT; loop++)); do
    echo -e "\n\n🔄 VÒNG LẶP TEST THỨ $loop / $LOOP_COUNT"
    echo "========================================================="

    # Xác định TARGET_NODE
    if [ "$TEST_ALL_ONLY" = true ]; then
        TARGET_NODE="all"
        echo "ℹ️ Chế độ TEST_ALL_ONLY: Bỏ qua luân phiên, ép test tắt toàn bộ mạng."
    elif [ -n "$TARGET_NODE_FIXED" ]; then
        TARGET_NODE="$TARGET_NODE_FIXED"
    else
        # Luân phiên theo danh sách RESTORE_NODES
        NUM_RESTORE=${#RESTORE_NODES[@]}
        IDX=$(( (loop - 1) % NUM_RESTORE ))
        TARGET_NODE=${RESTORE_NODES[$IDX]}
    fi

    echo -e "\n[1/8] ⏳ Kiểm tra điều kiện bắt đầu (Yêu cầu Epoch >= 1)..."
    while true; do
        CURRENT_EPOCH=$(get_current_epoch)
        if [ "$CURRENT_EPOCH" -ge 1 ]; then
            echo "✅ Mạng đã đạt Epoch $CURRENT_EPOCH. Đủ điều kiện bắt đầu test!"
            break
        fi
        echo "   ... Hiện tại: Epoch $CURRENT_EPOCH. Đang chờ mạng lên ít nhất Epoch 1..."
        sleep 5
    done

    if [ "$TARGET_NODE" == "all" ]; then
        echo "📥 Lưu trạng thái lịch sử trước khi dừng toàn bộ mạng (chọn node 0 làm chuẩn)..."
        (
            cd "$ROOT_DIR/metanode-suite/test-simple/test-rpc/test-history"
            go run main.go -config $HISTORY_CONFIG -action save -file /tmp/pending_check_all.json
        )

        echo -e "\n[2/8] 🛑 Dừng toàn bộ mạng..."
        echo "0,1,2,3,4" > /tmp/MTN_EXCLUDE_NODES
        run_orch stop

        echo -e "\n[3/8] 🚀 Bỏ qua tạo gap giao dịch vì mạng đã dừng hoàn toàn."
        echo -e "\n[4/8] ⏳ Đợi 10 giây trước khi bật lại toàn mạng..."
        sleep 10

        echo -e "\n[5/8] 🔄 Khởi động lại toàn bộ mạng..."
        echo "🗑️ Đang dọn dẹp log cũ của toàn bộ node..."
        rm -f "$ROOT_DIR/metanode/consensus/metanode/logs"/node_*/*.log
        run_orch start
        
        echo "⏳ Đang kiểm tra trạng thái khởi động chính xác của toàn mạng..."
        for n in 0 1 2 3 4; do
            if ! wait_for_node_startup "$n"; then
                if [ -f /tmp/MTN_INTEGRITY_FAILED ]; then
                    echo -e "\n🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨"
                    echo "❌ [DATA INTEGRITY FAILURE] (Đang test mục tiêu: $TARGET_NODE)"
                    echo "   Node $n KHÔNG THỂ KHỞI ĐỘNG do DỮ LIỆU BỊ HỎNG!"
                    echo ""
                    echo "   📋 Chi tiết lỗi từ startup integrity check:"
                    cat /tmp/MTN_INTEGRITY_FAILED
                    echo ""
                    echo "   🔧 HƯỚNG DẪN KHẮC PHỤC:"
                    echo "   1. Restore từ snapshot mới nhất:"
                    if [ "$DEPLOY_MODE" == "multi" ]; then
                        echo "      ./deploy_systemd_cluster.sh --env $SYSTEMD_ENV --restore-node $n"
                    else
                        echo "      ./mtn-orchestrator.sh restore-node $n"
                        echo "   2. Hoặc re-sync từ các node khác:"
                        echo "      ./mtn-orchestrator.sh resync-node $n"
                    fi
                    echo "🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨"
                    echo "DATA_INTEGRITY_FAILED: Node $n dữ liệu bị hỏng, cần restore snapshot" > /tmp/MTN_CHAIN_ERROR_STOP
                    rm -f /tmp/MTN_INTEGRITY_FAILED
                    exit 1
                fi
                echo -e "\n❌ [FATAL ERROR] (Đang test mục tiêu: $TARGET_NODE): Node $n đã Crash ngay khi vừa khởi động!"
                echo "   👉 Vui lòng xem log: metanode/consensus/metanode/logs/node_$n/go-master-stdout.log"
                exit 1
            fi
        done
        # Chờ mạng phục hồi đồng thuận hoàn toàn bằng active polling thay vì sleep 15s cứng
        wait_for_consensus
        rm -f /tmp/MTN_EXCLUDE_NODES
    else
        echo "📥 Lưu trạng thái lịch sử trước khi dừng node $TARGET_NODE..."
        (
            cd "$ROOT_DIR/metanode-suite/test-simple/test-rpc/test-history"
            go run main.go -config $HISTORY_CONFIG -action save -file /tmp/pending_check_${TARGET_NODE}.json
        )
        echo -e "\n[2/8] 🛑 Dừng node $TARGET_NODE..."
        echo "$TARGET_NODE" > /tmp/MTN_EXCLUDE_NODES
        run_orch stop-node $TARGET_NODE

        echo -e "\n[3/8] 🚀 Bắn giao dịch ngầm (Tạo Gap)..."
        cd "$ROOT_DIR/metanode-suite/test_tps/tps_blast_cc"
        SPAM_NODE=0
        if [ "$TARGET_NODE" = "0" ]; then
            SPAM_NODE=1
        fi
        go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10 --target-node $SPAM_NODE $TPS_CONFIG_ARG > blast_gap.log 2>&1 &
        PID_GAP=$!

        START_EPOCH=$(get_current_epoch)
        TARGET_EPOCH=$((START_EPOCH + GAP_EPOCH))
        echo -e "\n[4/8] ⏳ Đang ở Epoch $START_EPOCH. Chờ mạng chạy đến Epoch $TARGET_EPOCH..."

        while true; do
            CURRENT_EPOCH=$(get_current_epoch)
            if [ "$CURRENT_EPOCH" -ge "$TARGET_EPOCH" ]; then
                echo "✅ Đã đạt Epoch $CURRENT_EPOCH. (Gap đã tạo xong)"
                break
            fi
            echo "   ... Hiện tại: $CURRENT_EPOCH / $TARGET_EPOCH"
            sleep 0.5
        done

        # Dừng spam để node bắt kịp nhanh hơn, hoặc để nguyên tùy kịch bản. Ở đây tạm dừng spam trước khi restart.
        stop_spam
        wait $PID_GAP 2>/dev/null || true

        echo -e "\n[5/8] 🔄 Khởi động lại node $TARGET_NODE..."
        echo "🗑️ Đang dọn dẹp log cũ của node $TARGET_NODE..."
        rm -f "$ROOT_DIR/metanode/consensus/metanode/logs/node_$TARGET_NODE"/*.log
        run_orch start-node $TARGET_NODE
        
        echo "⏳ Đang kiểm tra trạng thái khởi động chính xác của Node $TARGET_NODE..."
        if ! wait_for_node_startup "$TARGET_NODE"; then
            # ═══════════════════════════════════════════════════════════
            # DATA INTEGRITY DETECTION (May 2026):
            # Check if the node exited due to data integrity failure
            # ═══════════════════════════════════════════════════════════
            if [ -f /tmp/MTN_INTEGRITY_FAILED ]; then
                echo -e "\n🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨"
                echo "❌ [DATA INTEGRITY FAILURE] (Đang test mục tiêu: $TARGET_NODE)"
                echo "   Node $TARGET_NODE KHÔNG THỂ KHỞI ĐỘNG do DỮ LIỆU BỊ HỎNG!"
                echo ""
                echo "   📋 Chi tiết lỗi từ startup integrity check:"
                cat /tmp/MTN_INTEGRITY_FAILED
                echo ""
                echo "   🔧 HƯỚNG DẪN KHẮC PHỤC:"
                echo "   1. Restore từ snapshot mới nhất:"
                if [ "$DEPLOY_MODE" == "multi" ]; then
                    echo "      ./deploy_systemd_cluster.sh --env $SYSTEMD_ENV --restore-node $TARGET_NODE"
                else
                    echo "      ./mtn-orchestrator.sh restore-node $TARGET_NODE"
                    echo "   2. Hoặc re-sync từ các node khác:"
                    echo "      ./mtn-orchestrator.sh resync-node $TARGET_NODE"
                fi
                echo "🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨🚨"
                # Ghi vào file sentinel dừng toàn bộ test
                echo "DATA_INTEGRITY_FAILED: Node $TARGET_NODE dữ liệu bị hỏng, cần restore snapshot" > /tmp/MTN_CHAIN_ERROR_STOP
                rm -f /tmp/MTN_INTEGRITY_FAILED
                exit 1
            fi
            echo -e "\n❌ [FATAL ERROR] (Đang test mục tiêu: $TARGET_NODE): Node $TARGET_NODE đã Crash ngay khi vừa khởi động!"
            echo "   👉 Vui lòng xem log: metanode/consensus/metanode/logs/node_$TARGET_NODE/go-master-stdout.log"
            exit 1
        fi
    fi

# Hàm chờ target_node đồng bộ tới block cao nhất (so với Node 0)
wait_for_sync_to_highest_block() {
    local target_node=$1
    echo "⏳ Đang chờ Node $target_node đồng bộ tới block cao nhất của mạng (Node 0 làm chuẩn)..."
    
    # Lấy RPC URL của target node:
    # - multi mode: đọc từ /tmp/rpc_nodes.json (key m<N>)
    # - single mode: dùng direct port cố định
    local target_rpc
    if [ "${DEPLOY_MODE:-single}" == "multi" ] && [ -f /tmp/rpc_nodes.json ]; then
        target_rpc=$(jq -r ".nodes.m${target_node} // empty" /tmp/rpc_nodes.json 2>/dev/null || echo "")
        if [ -z "$target_rpc" ]; then
            target_rpc="$EPOCH_RPC"
        fi
    else
        local direct_port=8757
        if [ "$target_node" == "1" ]; then direct_port=10747; fi
        if [ "$target_node" == "2" ]; then direct_port=10749; fi
        if [ "$target_node" == "3" ]; then direct_port=10750; fi
        if [ "$target_node" == "4" ]; then direct_port=10748; fi
        target_rpc="http://127.0.0.1:$direct_port"
    fi
    # RPC của Node 0 (chuẩn tham chiếu)
    local ref_rpc
    if [ "${DEPLOY_MODE:-single}" == "multi" ] && [ -f /tmp/rpc_nodes.json ]; then
        ref_rpc=$(jq -r '.nodes.m0 // empty' /tmp/rpc_nodes.json 2>/dev/null || echo "$EPOCH_RPC")
    else
        ref_rpc="http://127.0.0.1:8757"
    fi
    
    local max_attempts=600 # 10 phút tối đa
    local attempt=0
    
    while [ $attempt -lt $max_attempts ]; do
        # Lấy block hiện tại của Node 0 (tham chiếu)
        local block_hex_0=$(curl -s --max-time 1 -X POST "$ref_rpc" \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
            | grep -oP '"result":"\K(0x[0-9a-fA-F]+)' || echo "")
            
        local block_0=0
        if [ -n "$block_hex_0" ] && [ "$block_hex_0" != "0x" ]; then
            block_0=$(printf "%d\n" "$block_hex_0" 2>/dev/null || echo "0")
        fi
        
        # Lấy block hiện tại của Target Node
        local block_hex_target=$(curl -s --max-time 1 -X POST "$target_rpc" \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
            | grep -oP '"result":"\K(0x[0-9a-fA-F]+)' || echo "")
            
        local block_target=0
        if [ -n "$block_hex_target" ] && [ "$block_hex_target" != "0x" ]; then
            block_target=$(printf "%d\n" "$block_hex_target" 2>/dev/null || echo "0")
        fi
        
        if [ "$block_target" -ge "$block_0" ] && [ "$block_0" -gt 0 ]; then
            echo "✅ Node $target_node đã đồng bộ tới block cao nhất (Node 0: $block_0, Node $target_node: $block_target)"
            break
        fi
        
        echo "   ... [Lần $attempt] Node 0: $block_0, Node $target_node: $block_target. Đang chờ đồng bộ..."
        sleep 1
        attempt=$((attempt + 1))
    done
    
    if [ $attempt -eq $max_attempts ]; then
        echo "⚠️ Cảnh báo: Đã đạt giới hạn tối đa chờ đồng bộ block của Node $target_node"
    fi
}

    # 1. Chạy xác minh trạng thái lịch sử trước bằng active polling (cực kỳ nhanh và chính xác)
    if [ "$TARGET_NODE" != "all" ]; then
        wait_for_sync_to_highest_block "$TARGET_NODE"
        rm -f /tmp/MTN_EXCLUDE_NODES
        
        echo "📤 Xác minh trạng thái lịch sử trên node $TARGET_NODE..."
        (
            cd "$ROOT_DIR/metanode-suite/test-simple/test-rpc/test-history"
            go run main.go -config $HISTORY_CONFIG -action verify -file /tmp/pending_check_${TARGET_NODE}.json -target-node $TARGET_NODE
        )

        # echo "🔎 Gọi API meta_verifyHistoricalRoot để kiểm chứng tính toàn vẹn của State Trie trên Node $TARGET_NODE..."
        # if [ "$TARGET_NODE" = "0" ]; then TARGET_PORT=8757; fi
        # if [ "$TARGET_NODE" = "1" ]; then TARGET_PORT=10747; fi
        # if [ "$TARGET_NODE" = "2" ]; then TARGET_PORT=10749; fi
        # if [ "$TARGET_NODE" = "3" ]; then TARGET_PORT=10750; fi
        # if [ "$TARGET_NODE" = "4" ]; then TARGET_PORT=10748; fi

        # VERIFY_RES=$(curl -s -X POST http://127.0.0.1:$TARGET_PORT \
        #     -H "Content-Type: application/json" \
        #     -d '{"jsonrpc":"2.0","method":"meta_verifyHistoricalRoot","params":["latest"],"id":1}')
        
        # if echo "$VERIFY_RES" | grep -q '"match":true'; then
        #     echo "   ✅ VerifyHistoricalRoot thành công: StateRoot hoàn toàn khớp với Database sau khi Sync Gap!"
        # else
        #     echo "❌ LỖI NGHIÊM TRỌNG (Đang test Node $TARGET_NODE): StateRoot không khớp (MISMATCH) sau khi Sync Gap!"
        #     echo "   Kết quả API: $VERIFY_RES"
        #     echo "DATA_INTEGRITY_FAILED: Node $TARGET_NODE bị lỗi State Root Mismatch sau khi sync gap" > /tmp/MTN_CHAIN_ERROR_STOP
        #     exit 1
        # fi
    else
        echo "📤 Xác minh trạng thái lịch sử trên mạng (node 0)..."
        (
            cd "$ROOT_DIR/metanode-suite/test-simple/test-rpc/test-history"
            go run main.go -config $HISTORY_CONFIG -action verify -file /tmp/pending_check_all.json -target-node 0
        )
    fi

    # 2. Kiểm tra Hash Checker sau khi xác minh lịch sử thành công
    echo -e "\n[6/8] 👁️ Kiểm tra Hash Checker sau khi Node hồi phục (Timeout 30s)..."
    cd $HASH_CHECKER_DIR
    rm -f hash_mismatch_alert.log
    timeout 30s go run main.go --watch --interval 5s $HASH_CONFIG_ARG || true
    analyze_mismatch "$TARGET_NODE"
    echo "✅ Nếu không có Alert văng ra, Node đã đồng bộ Block và Hash thành công!"

    echo -e "\n[7/8] 🚀 Bắn giao dịch trở lại (Stress Test sau hồi phục)..."
    cd "$ROOT_DIR/metanode-suite/test_tps/tps_blast_cc"
    SPAM_NODE_1=$TARGET_NODE
    SPAM_NODE_2=0
    if [ "$TARGET_NODE" = "all" ]; then
        SPAM_NODE_1=0
        SPAM_NODE_2=1
    elif [ "$TARGET_NODE" = "0" ]; then
        SPAM_NODE_2=1
    fi
    
    echo "👉 Bắn giao dịch lên Node vừa hồi phục ($SPAM_NODE_1) (Chạy tuần tự)..."
    # Chạy tuần tự để tránh lỗi Nonce collision. In ra màn hình bằng lệnh tee để dễ theo dõi.
    go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10 --target-node $SPAM_NODE_1 $TPS_CONFIG_ARG 2>&1 | tee blast_recovered_node.log
    
    # Check lỗi nếu lệnh fail
    if [ ${PIPESTATUS[0]} -ne 0 ]; then
        echo "❌ [ERROR] Lỗi khi chạy tps_blast_cc tuần tự!"
        exit 1
    fi
    
    echo "👉 Đổi sang bắn giao dịch qua node khác ($SPAM_NODE_2) (Chạy ngầm)..."
    go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10 --target-node $SPAM_NODE_2 $TPS_CONFIG_ARG > blast_other_node.log 2>&1 &
    PID_OTH=$!

    echo -e "\n[8/8] 👁️ Kiểm tra Hash Checker khi mạng đang chịu tải (Timeout 40s)..."
    cd $HASH_CHECKER_DIR
    rm -f hash_mismatch_alert.log
    timeout 40s go run main.go --watch --interval 5s $HASH_CONFIG_ARG || true
    analyze_mismatch "$TARGET_NODE"

    echo "⏳ Đang chờ tiến trình bắn giao dịch ngầm ($PID_OTH) hoàn tất..."
    wait $PID_OTH
    if [ $? -ne 0 ]; then
        echo "❌ [ERROR] Tiến trình bắn giao dịch ngầm thất bại!"
        exit 1
    fi

    stop_spam

    # Kiểm tra log xem có lỗi không
    if grep -q "❌ \[ERROR\]" "$ROOT_DIR/metanode-suite/test_tps/tps_blast_cc/blast_recovered_node.log" 2>/dev/null || grep -q "panic" "$ROOT_DIR/metanode-suite/test_tps/tps_blast_cc/blast_recovered_node.log" 2>/dev/null; then
        echo "❌ LỖI: TPS blast lên Node $TARGET_NODE gặp lỗi!"
        echo "--- LOG CHI TIẾT ---"
        cat "$ROOT_DIR/metanode-suite/test_tps/tps_blast_cc/blast_recovered_node.log"
        exit 1
    fi
    if grep -q "❌ \[ERROR\]" "$ROOT_DIR/metanode-suite/test_tps/tps_blast_cc/blast_other_node.log" 2>/dev/null || grep -q "panic" "$ROOT_DIR/metanode-suite/test_tps/tps_blast_cc/blast_other_node.log" 2>/dev/null; then
        echo "❌ LỖI: TPS blast lên Node $SPAM_NODE gặp lỗi!"
        echo "--- LOG CHI TIẾT ---"
        cat "$ROOT_DIR/metanode-suite/test_tps/tps_blast_cc/blast_other_node.log"
        exit 1
    fi
    echo "✅ Toàn bộ tiến trình TPS blast hoàn tất ổn định!"
done

echo -e "\n🎉 HOÀN TẤT TOÀN BỘ CÁC VÒNG LẶP TEST NODE RECOVERY!"
