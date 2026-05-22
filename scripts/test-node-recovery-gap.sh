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

# Dọn dẹp tiến trình giám sát hoặc spam cũ đang chạy ngầm để tránh xung đột
echo "🧹 Đang dọn dẹp các tiến trình nền cũ..."
pkill -f "go run main.go --watch" || true
pkill -f "exe/main --watch" || true
pkill -f "rpc-tcp-simple.sh" || true
pkill -f "test-rpc.*main.go" || true
pkill -f "test-tcp.*main-no-none.go" || true
pkill -f "main.go --count 20000" || true

# Xóa cờ lỗi cũ trước khi chạy
rm -f /tmp/MTN_CHAIN_ERROR_STOP /tmp/pending_check_*.json

# Hàm lấy epoch hiện tại từ m0 (Node 0 luôn chạy)
get_current_epoch() {
    # Gọi RPC và lấy epoch dưới dạng hex
    hex_epoch=$(curl -s --max-time 2 -X POST http://127.0.0.1:8757 \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest",false],"id":1}' \
        | grep -oP '"epoch":"\K(0x[0-9a-fA-F]+)' || echo "0x0")
    
    if [ -z "$hex_epoch" ] || [ "$hex_epoch" = "0x0" ]; then
        echo "0"
    else
        # Chuyển Hex -> Decimal
        printf "%d\n" "$hex_epoch"
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
        
        if [ -n "$block_hex" ]; then
            local current_block
            current_block=$(printf "%d\n" "$block_hex")
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
        sleep 2
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
        sleep 0.5
    done

    if [ "$TARGET_NODE" == "all" ]; then
        echo -e "\n[2/8] 🛑 Dừng toàn bộ mạng..."
        $ORCH_SCRIPT stop

        echo -e "\n[3/8] 🚀 Bỏ qua tạo gap vì mạng đã dừng hoàn toàn."
        echo -e "\n[4/8] ⏳ Bỏ qua chờ epoch gap."

        echo -e "\n[5/8] 🔄 Khởi động lại toàn bộ mạng..."
        $ORCH_SCRIPT start
        
        echo "⏳ Đang kiểm tra nhanh xem có Node nào bị crash ngay khi khởi động không..."
        crash_detected=0
        for ((i=1; i<=20; i++)); do
            for n in 0 1 2 3 4; do
                if ! tmux ls 2>/dev/null | grep -q "go-master-$n"; then
                    crash_detected=1
                    break 2
                fi
            done
            sleep 0.1
        done

        for n in 0 1 2 3 4; do
            if ! tmux ls 2>/dev/null | grep -q "go-master-$n"; then
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
                echo -e "\n❌ [FATAL ERROR] (Đang test mục tiêu: $TARGET_NODE): Node $n đã Crash (Panic) ngay khi vừa khởi động!"
                echo "   👉 Tiến trình go-master-$n không tồn tại trong tmux."
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
        
        echo "⏳ Đang kiểm tra nhanh xem Node $TARGET_NODE có bị crash ngay khi khởi động không..."
        crash_detected=0
        for ((i=1; i<=20; i++)); do
            if ! tmux ls 2>/dev/null | grep -q "go-master-$TARGET_NODE"; then
                crash_detected=1
                break
            fi
            sleep 0.1
        done

        if ! tmux ls 2>/dev/null | grep -q "go-master-$TARGET_NODE"; then
            # ═══════════════════════════════════════════════════════════
            # DATA INTEGRITY DETECTION (May 2026):
            # Check if the node exited due to data integrity failure
            # (exit code 78) vs regular crash (panic, OOM).
            # The Go startup_integrity_check writes /tmp/MTN_INTEGRITY_FAILED
            # with detailed error info when data is corrupted.
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
            echo -e "\n❌ [FATAL ERROR] (Đang test mục tiêu: $TARGET_NODE): Node $TARGET_NODE đã Crash (Panic) ngay khi vừa khởi động!"
            echo "   👉 Tiến trình go-master-$TARGET_NODE không tồn tại trong tmux."
            echo "   👉 Vui lòng xem log: metanode/consensus/metanode/logs/node_$TARGET_NODE/go-master-stdout.log"
            exit 1
        fi
    fi

    # 1. Chạy xác minh trạng thái lịch sử trước bằng active polling (cực kỳ nhanh và chính xác)
    if [ "$TARGET_NODE" != "all" ]; then
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
