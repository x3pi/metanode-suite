#!/bin/bash
set -euo pipefail

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

# Xóa cờ lỗi cũ trước khi chạy
rm -f /tmp/MTN_CHAIN_ERROR_STOP

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

# Hàm lấy epoch hiện tại từ m0 (Node 0 luôn chạy)
get_current_epoch() {
    # Gọi RPC và lấy epoch dưới dạng hex
    local hex_epoch=$(curl -s --max-time 2 -X POST http://127.0.0.1:8757 \
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



# Hàm dọn dẹp tiến trình spam
stop_spam() {
    echo "🛑 Dừng các tiến trình spam giao dịch..."
    pkill -f "rpc-tcp-simple.sh" || true
    pkill -f "test-rpc.*main.go" || true
    pkill -f "test-tcp.*main-no-none.go" || true
    pkill -f "main.go --count 20000" || true
    sleep 2
}

# Đảm bảo dọn dẹp khi script bị ngắt
cleanup() {
    local err=$?
    trap - EXIT INT TERM
    echo -e "\n[CLEANUP] Đang thoát test với mã lỗi $err..."
    stop_spam
    kill $MONITOR_PID 2>/dev/null || true
    exit $err
}
trap cleanup EXIT INT TERM

# Hàm phân tích lỗi tự động
analyze_mismatch() {
    local log_file="$HASH_CHECKER_DIR/hash_mismatch_alert.log"
    echo -e "\n📊 [PHÂN TÍCH NHANH LỖI RECOVERY]"
    echo "--------------------------------------------------------"
    if [ ! -f "$log_file" ]; then
        echo "✅ Không tìm thấy file báo lỗi. Mạng hoàn toàn đồng bộ!"
        echo "--------------------------------------------------------"
        return
    fi
    
    local mismatches=$(grep -c "⚠️  Block" "$log_file" || true)
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
        echo -e "\n🛑 LỖI NGHIÊM TRỌNG: Phát hiện >= 100 block bị lệch hash! Dừng script ngay lập tức để kiểm tra!"
        exit 1
    elif [ "$mismatches" -gt 0 ]; then
        echo -e "\n🛑 LỖI: Phát hiện $mismatches block bị lệch hash! Dừng script để kiểm tra!"
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

    echo -e "\n[2/8] 🛑 Dừng node $NODE_ID..."
    $ORCH_SCRIPT stop-node $NODE_ID

    echo -e "\n[3/8] 🚀 Bắn giao dịch ngầm (Tạo Gap)..."
    cd "$ROOT_DIR/tool-test/test_tps/tps_blast_cc"
    go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10 > /dev/null 2>&1 &

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
        sleep 10
    done

    # Dừng spam để node bắt kịp nhanh hơn, hoặc để nguyên tùy kịch bản. Ở đây tạm dừng spam trước khi restart.
    stop_spam

    echo -e "\n[5/8] 🔄 Khởi động lại node $NODE_ID..."
    $ORCH_SCRIPT start-node $NODE_ID
    
    echo "⏳ Chờ 5s để xác nhận tiến trình Node không bị crash..."
    sleep 5
    if ! tmux ls 2>/dev/null | grep -q "go-master-$NODE_ID"; then
        echo -e "\n❌ [FATAL ERROR]: Node $NODE_ID đã Crash (Panic) ngay khi vừa khởi động!"
        echo "   👉 Tiến trình go-master-$NODE_ID không tồn tại trong tmux."
        echo "   👉 Vui lòng xem log: metanode/consensus/metanode/logs/node_$NODE_ID/go-master-stdout.log"
        exit 1
    fi
echo "⏳ Đợi 10s cho node khởi động và xin Recovery State..."
sleep 10



echo -e "\n[6/8] 👁️ Kiểm tra Hash Checker sau khi Node hồi phục (Timeout 30s)..."
    cd $HASH_CHECKER_DIR
    rm -f hash_mismatch_alert.log
timeout 30s go run main.go --watch --interval 5s --check-last 100 --nodes "m0=http://127.0.0.1:8757,m1=http://127.0.0.1:10747,m2=http://127.0.0.1:10749,m3=http://127.0.0.1:10750,m4=http://127.0.0.1:10748" || true
    analyze_mismatch
    echo "✅ Nếu không có Alert văng ra, Node đã đồng bộ Block và Hash thành công!"

    echo -e "\n[7/8] 🚀 Bắn giao dịch trở lại (Stress Test sau hồi phục)..."
    cd "$ROOT_DIR/tool-test/test_tps/tps_blast_cc"
    go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10 > /dev/null 2>&1 &

    echo -e "\n[8/8] 👁️ Kiểm tra Hash Checker khi mạng đang chịu tải (Timeout 40s)..."
    cd $HASH_CHECKER_DIR
    rm -f hash_mismatch_alert.log
    timeout 40s go run main.go --watch --interval 5s --check-last 100 --nodes "m0=http://127.0.0.1:8757,m1=http://127.0.0.1:10747,m2=http://127.0.0.1:10749,m3=http://127.0.0.1:10750,m4=http://127.0.0.1:10748" || true
    analyze_mismatch

    stop_spam


done

echo -e "\n🎉 HOÀN TẤT TOÀN BỘ CÁC VÒNG LẶP TEST NODE RECOVERY!"
