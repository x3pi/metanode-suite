#!/bin/bash
set -euo pipefail

# Giới hạn tài nguyên biên dịch để tránh OOM trong môi trường tài nguyên hạn chế
export GOMAXPROCS=2
export CARGO_BUILD_JOBS=2

# Unset exported cd function to avoid environment conflicts
unset -f cd 2>/dev/null || true


# Tham số (mặc định: node=1, gap=3, loop=1)
NODE_ID=${1:-1}
GAP_EPOCH=${2:-1}
LOOP_COUNT=${3:-20000}

# Đường dẫn động để chạy được trên nhiều máy
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"
ORCH_SCRIPT="$ROOT_DIR/metanode/consensus/metanode/scripts/mtn-orchestrator.sh"
RPC_TCP_SCRIPT="$SCRIPT_DIR/rpc-tcp-simple.sh"
HASH_CHECKER_DIR="$(dirname "$SCRIPT_DIR")/block/block_hash_checker"

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
rm -f /tmp/MTN_CHAIN_ERROR_STOP /tmp/pending_check_*.json

# Hàm lấy epoch hiện tại từ m0 (Node 0 luôn chạy)
get_current_epoch() {
    # Gọi RPC và lấy epoch dưới dạng hex
    hex_epoch=$(curl -s --max-time 2 -X POST http://127.0.0.1:8757 \
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

# Hàm chờ mạng thiết lập đồng thuận và bắt đầu tăng trưởng block
wait_for_consensus() {
    echo "⏳ Đang chờ hệ thống mạng thiết lập lại đồng thuận và tiến triển chiều cao block..."
    local last_block=""
    # Thử tối đa 60 giây (300 lần thử, mỗi lần cách nhau 200ms)
    for ((r=1; r<=300; r++)); do
        local block_hex
        block_hex=$(curl -s --max-time 1 -X POST http://127.0.0.1:8757 \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
            | grep -oP '"result":"\K(0x[0-9a-fA-F]+)' || echo "")
        
        if [ -n "$block_hex" ] && [ "$block_hex" != "0x" ]; then
            local current_block
            current_block=$(printf "%d\n" "$block_hex" 2>/dev/null || echo "0")
            if [ -n "$last_block" ] && [ "$current_block" -gt "$last_block" ]; then
                echo "✅ Đồng thuận đã phục hồi! Chiều cao block đang tăng trưởng: $last_block -> $current_block"
                return 0
            fi
            last_block=$current_block
        fi
        sleep 0.2
    done
    echo "⚠️ Cảnh báo: Đồng thuận chưa phục hồi rõ rệt sau 60 giây, tiếp tục chạy..."
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
            if grep -E -q "All checks passed|NOMT account_state not initialized yet|Indexing process initiated|Consensus core started|Starting consensus|Starting peer synchronization" "$log_file"; then
                echo "✅ [SUCCESS] Node ${node_id} đã khởi động và vượt qua integrity check thành công sau $((r * 100))ms!"
                return 0
            fi
            if grep -E -q "CRITICAL ERROR|CRITICAL:" "$log_file"; then
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
        rm -f /tmp/pending_check_*.json
    else
        echo "💡 [DEBUG] Giữ lại file checkpoint /tmp/pending_check_*.json để debug offline!"
    fi
    kill ${MONITOR_PID:-0} 2>/dev/null || true
    exit $err
}
trap cleanup EXIT INT TERM

# Theo dõi cờ lỗi từ Hash Checker ngầm
monitor_error_flag() {
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
        sleep 0.2
    done
}
monitor_error_flag &
MONITOR_PID=$!

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
bash "$SCRIPT_DIR/simple_test.sh"
echo "✅ Khởi tạo và Simple Test hoàn tất!"

for ((loop=1; loop<=LOOP_COUNT; loop++)); do
    echo -e "\n\n🔄 VÒNG LẶP TEST THỨ $loop / $LOOP_COUNT"
    echo "========================================================="

    # Xác định TARGET_NODE luân phiên
    MOD=$(( (loop - 1) % 5 ))
    if [ $MOD -eq 0 ]; then
        TARGET_NODE=1
    elif [ $MOD -eq 1 ]; then
        TARGET_NODE=2
    elif [ $MOD -eq 2 ]; then
        TARGET_NODE=3
    elif [ $MOD -eq 3 ]; then
        TARGET_NODE=4
    else
        TARGET_NODE="all"
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
        echo -e "\n[2/8] 🛑 Dừng toàn bộ mạng..."
        $ORCH_SCRIPT stop

        echo -e "\n[3/8] 🚀 Bỏ qua tạo gap vì mạng đã dừng hoàn toàn."
        echo -e "\n[4/8] ⏳ Bỏ qua chờ epoch gap."

        echo -e "\n[5/8] 🔄 Khởi động lại toàn bộ mạng..."
        $ORCH_SCRIPT start
        
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
                    echo "      ./mtn-orchestrator.sh restore-node $n"
                    echo "   2. Hoặc re-sync từ các node khác:"
                    echo "      ./mtn-orchestrator.sh resync-node $n"
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
    else
        echo "📥 Lưu trạng thái lịch sử trước khi dừng node $TARGET_NODE..."
        (
            cd "$ROOT_DIR/metanode-suite/test-simple/test-rpc/test-history"
            go run main.go -config config-local.json -action save -file /tmp/pending_check_${TARGET_NODE}.json
        )
        echo -e "\n[2/8] 🛑 Dừng node $TARGET_NODE..."
        $ORCH_SCRIPT stop-node $TARGET_NODE

        echo -e "\n[3/8] 🚀 Bắn giao dịch ngầm (Tạo Gap)..."
        cd "$ROOT_DIR/metanode-suite/test_tps/tps_blast_cc"
        SPAM_NODE=0
        if [ "$TARGET_NODE" = "0" ]; then
            SPAM_NODE=1
        fi
        go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10 --target-node $SPAM_NODE > blast_gap.log 2>&1 &
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
        $ORCH_SCRIPT start-node $TARGET_NODE
        
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
                echo "      ./mtn-orchestrator.sh restore-node $TARGET_NODE"
                echo "   2. Hoặc re-sync từ các node khác:"
                echo "      ./mtn-orchestrator.sh resync-node $TARGET_NODE"
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
    
    # Lấy RPC Port trực tiếp của target node để tránh phụ thuộc vào RPC Proxy
    local direct_port=8757
    if [ "$target_node" == "1" ]; then direct_port=10747; fi
    if [ "$target_node" == "2" ]; then direct_port=10749; fi
    if [ "$target_node" == "3" ]; then direct_port=10750; fi
    if [ "$target_node" == "4" ]; then direct_port=10748; fi
    
    local max_attempts=600 # 10 phút tối đa
    local attempt=0
    
    while [ $attempt -lt $max_attempts ]; do
        # Lấy block hiện tại của Node 0
        local block_hex_0=$(curl -s --max-time 1 -X POST http://127.0.0.1:8757 \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
            | grep -oP '"result":"\K(0x[0-9a-fA-F]+)' || echo "")
            
        local block_0=0
        if [ -n "$block_hex_0" ] && [ "$block_hex_0" != "0x" ]; then
            block_0=$(printf "%d\n" "$block_hex_0" 2>/dev/null || echo "0")
        fi
        
        # Lấy block hiện tại của Target Node
        local block_hex_target=$(curl -s --max-time 1 -X POST http://127.0.0.1:$direct_port \
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
        
        echo "📤 Xác minh trạng thái lịch sử trên node $TARGET_NODE..."
        (
            cd "$ROOT_DIR/metanode-suite/test-simple/test-rpc/test-history"
            go run main.go -config config-local.json -action verify -file /tmp/pending_check_${TARGET_NODE}.json -target-node $TARGET_NODE
        )
    fi

    # 2. Kiểm tra Hash Checker sau khi xác minh lịch sử thành công
    echo -e "\n[6/8] 👁️ Kiểm tra Hash Checker sau khi Node hồi phục (Timeout 30s)..."
    cd $HASH_CHECKER_DIR
    rm -f hash_mismatch_alert.log
    timeout 30s go run main.go --watch --interval 5s --check-last 100 --nodes "m0=http://127.0.0.1:8757,m1=http://127.0.0.1:10747,m2=http://127.0.0.1:10749,m3=http://127.0.0.1:10750,m4=http://127.0.0.1:10748" || true
    analyze_mismatch "$TARGET_NODE"
    echo "✅ Nếu không có Alert văng ra, Node đã đồng bộ Block và Hash thành công!"

    echo -e "\n[7/8] 🚀 Bắn giao dịch trở lại (Stress Test sau hồi phục)..."
    cd "$ROOT_DIR/metanode-suite/test_tps/tps_blast_cc"
    SPAM_NODE=0
    if [ "$TARGET_NODE" = "0" ]; then
        SPAM_NODE=1
    fi
    echo "👉 Bắn giao dịch lên Node vừa hồi phục ($TARGET_NODE)..."
    go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10 --target-node $TARGET_NODE > blast_recovered_node.log 2>&1 &
    PID_REC=$!
    
    echo "👉 Đổi sang bắn giao dịch qua node khác ($SPAM_NODE)..."
    go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10 --target-node $SPAM_NODE > blast_other_node.log 2>&1 &
    PID_OTH=$!

    echo -e "\n[8/8] 👁️ Kiểm tra Hash Checker khi mạng đang chịu tải (Timeout 40s)..."
    cd $HASH_CHECKER_DIR
    rm -f hash_mismatch_alert.log
    timeout 40s go run main.go --watch --interval 5s --check-last 100 --nodes "m0=http://127.0.0.1:8757,m1=http://127.0.0.1:10747,m2=http://127.0.0.1:10749,m3=http://127.0.0.1:10750,m4=http://127.0.0.1:10748" || true
    analyze_mismatch "$TARGET_NODE"

    stop_spam
    wait $PID_REC 2>/dev/null || true
    wait $PID_OTH 2>/dev/null || true

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
