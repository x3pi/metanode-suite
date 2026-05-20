#!/bin/bash

# Mặc định cấu hình
NODE_ID=1
LOOPS=2000
TPS_ROUNDS=1
TPS_COUNT=20000

# Hàm hướng dẫn sử dụng
usage() {
    echo "========================================================="
    echo "🛠️  TOOL TEST SNAPSHOT PIPELINE"
    echo "========================================================="
    echo "Usage: $0 [options]"
    echo ""
    echo "Options:"
    echo "  --node <id>         Chọn node để restore (mặc định: $NODE_ID)"
    echo "  --loops <num>       Số vòng lặp test (mặc định: $LOOPS)"
    echo "  --tps-rounds <num>  Số round chạy TPS blast mỗi vòng (mặc định: $TPS_ROUNDS)"
    echo "  --tps-count <num>   Số lượng giao dịch TPS mỗi round (mặc định: $TPS_COUNT)"
    echo "  -h, --help          Hiển thị trợ giúp này"
    echo ""
    echo "Ví dụ:"
    echo "  $0 --node 1 --loops 5 --tps-rounds 2"
    echo "========================================================="
}

# Parse các tham số truyền vào
while [[ "$#" -gt 0 ]]; do
    case $1 in
        --node) NODE_ID="$2"; shift ;;
        --loops) LOOPS="$2"; shift ;;
        --tps-rounds) TPS_ROUNDS="$2"; shift ;;
        --tps-count) TPS_COUNT="$2"; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "❌ Tham số không hợp lệ: $1"; usage; exit 1 ;;
    esac
    shift
done

# Tự động định vị đường dẫn dựa theo vị trí của file script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOOL_TEST_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

CHECKER_DIR="$TOOL_TEST_DIR/block/block_hash_checker"
TPS_DIR="$TOOL_TEST_DIR/test_tps/tps_blast_cc"
SCRIPTS_DIR="$TOOL_TEST_DIR/scripts"
METANODE_SCRIPT_DIR="$(cd "$TOOL_TEST_DIR/../metanode" && pwd)/consensus/metanode/scripts/node"

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
            kill -9 $CHECKER_PID $TPS_PID 2>/dev/null || true
            pkill -f "rpc-tcp-simple.sh" || true
            pkill -f "main.go --count" || true
            kill -TERM -$$
            exit 1
        fi
        sleep 2
    done
}
monitor_error_flag &
MONITOR_PID=$!

# Xử lý tín hiệu Ctrl+C (SIGINT) để kill sạch các tiến trình chạy ngầm
trap 'echo -e "\n🛑 Đang dọn dẹp tiến trình..."; kill -9 $CHECKER_PID $TPS_PID $MONITOR_PID 2>/dev/null || true; pkill -P $$ 2>/dev/null || true; exit 1' SIGINT SIGTERM

echo "====================================================================="
echo "🚀 BƯỚC 0: CHẠY SIMPLE TEST ĐỂ KHỞI TẠO VÀ TEST MẠNG CƠ BẢN"
echo "====================================================================="
cd "$SCRIPTS_DIR" || exit 1
./simple_test.sh
SIMPLE_EXIT=$?
if [ $SIMPLE_EXIT -ne 0 ]; then
    echo "❌ LỖI: simple_test.sh thất bại! Dừng toàn bộ."
    kill $MONITOR_PID 2>/dev/null || true
    exit 1
fi

for ((i=1; i<=LOOPS; i++)); do
    echo ""
    echo "====================================================================="
    echo "🔄 BẮT ĐẦU VÒNG LẶP TEST SNAPSHOT: $i / $LOOPS (Node: $NODE_ID)"
    echo "====================================================================="

    # 1. Khởi động block_hash_checker chạy ngầm
    echo "👉 Bước 1: Khởi động block_hash_checker chạy ngầm..."
    cd "$CHECKER_DIR" || exit 1
    go run main.go --watch --interval 5s --check-last 100 --nodes "m0=http://127.0.0.1:8757,m1=http://127.0.0.1:10747,m2=http://127.0.0.1:10749,m3=http://127.0.0.1:10750,m4=http://127.0.0.1:10748" > "hash_checker_loop_${i}.log" 2>&1 &
    CHECKER_PID=$!
    echo "   ✅ Đã chạy block_hash_checker với PID $CHECKER_PID"

    # 2. Chạy TPS
    echo "👉 Bước 2: Chạy TPS Blast CC ($TPS_ROUNDS rounds, $TPS_COUNT txs)..."
    cd "$TPS_DIR" || exit 1
    go run main.go --count "$TPS_COUNT" --parallel_native=true --rounds "$TPS_ROUNDS" --load_balance=false --batch=10 &
    TPS_PID=$!

    # Giám sát: nếu checker bị kill (do lệch hash) trong lúc TPS đang chạy thì dừng ngay
    while kill -0 $TPS_PID 2>/dev/null; do
        if ! kill -0 $CHECKER_PID 2>/dev/null; then
            echo ""
            echo "❌ LỖI NGHIÊM TRỌNG: block_hash_checker đã bị kill (khả năng phát hiện lệch hash)!"
            echo "   Xem log chi tiết tại: $CHECKER_DIR/hash_checker_loop_${i}.log"
            echo "--- LOG TÓM TẮT ---"
            tail -n 20 "$CHECKER_DIR/hash_checker_loop_${i}.log"
            echo "-------------------"
            kill -9 $TPS_PID 2>/dev/null
            exit 1
        fi
        sleep 2
    done

    # Lấy exit code của TPS (sau khi TPS chạy xong)
    wait $TPS_PID
    TPS_EXIT_CODE=$?
    if [ $TPS_EXIT_CODE -ne 0 ]; then
        echo "❌ LỖI: Tiến trình TPS blast thất bại (exit code $TPS_EXIT_CODE)"
        kill -9 $CHECKER_PID 2>/dev/null
        exit 1
    fi
    echo "   ✅ Chạy TPS hoàn tất."

    # 3. Chờ các node đồng bộ block (dựa theo log CHÊNH)
    echo "👉 Bước 3: Chờ các node đồng bộ block (theo dõi 'CHÊNH' trong log)..."
    MAX_WAIT=20  # Tối đa 20 lần * 5s = 100 s
    PREV_DIFF=9999999
    STALL_COUNT=0
    SYNCED=false

    for ((w=1; w<=MAX_WAIT; w++)); do
        # Lấy dòng log mới nhất có chứa "Heights:"
        LAST_LINE=$(grep "Heights:" "$CHECKER_DIR/hash_checker_loop_${i}.log" | tail -n 1)
        
        if [[ "$LAST_LINE" =~ CHÊNH\ ([0-9]+)\ blocks! ]]; then
            CUR_DIFF="${BASH_REMATCH[1]}"
            echo "   ⏳ Đang chờ đồng bộ... Chênh lệch: $CUR_DIFF blocks"
            
            if [ "$CUR_DIFF" -lt "$PREV_DIFF" ]; then
                PREV_DIFF=$CUR_DIFF
                STALL_COUNT=0
            elif [ "$CUR_DIFF" -eq "$PREV_DIFF" ]; then
                STALL_COUNT=$((STALL_COUNT + 1))
                if [ $STALL_COUNT -ge 6 ]; then # 6 lần liên tiếp (~30s) không giảm
                    echo "❌ LỖI: Tiến trình đồng bộ bị kẹt ở mức chênh $CUR_DIFF blocks trong 100!"
                    kill -9 $CHECKER_PID 2>/dev/null
                    exit 1
                fi
            else
                PREV_DIFF=$CUR_DIFF
                STALL_COUNT=0
            fi
        else
            # Nếu không tìm thấy chữ CHÊNH, tức là khoảng cách <= 10 blocks (đã đồng bộ)
            echo "   ✅ Các node đã đồng bộ đủ gần nhau!"
            SYNCED=true
            break
        fi
        sleep 5
    done

    if [ "$SYNCED" = false ]; then
        echo "❌ LỖI: Quá thời gian chờ đồng bộ (10 phút)!"
        kill -9 $CHECKER_PID 2>/dev/null
        exit 1
    fi
    
    echo "   👉 Chờ thêm 5s sau khi đồng bộ để hệ thống thật sự lưu state..."
    sleep 5
    
    # Kiểm tra lại checker sau thời gian chờ
    if ! kill -0 $CHECKER_PID 2>/dev/null; then
        echo "❌ LỖI: block_hash_checker đã báo lệch hash và thoát sau khi bơm TPS!"
        echo "   Xem log tại: $CHECKER_DIR/hash_checker_loop_${i}.log"
        tail -n 20 "$CHECKER_DIR/hash_checker_loop_${i}.log"
        exit 1
    fi

    # 4. Restore Node
    echo "👉 Bước 4: Restore Node $NODE_ID từ snapshot của Node 4..."
    cd "$METANODE_SCRIPT_DIR" || exit 1
    # Tự động gửi phím "y" liên tục để vượt qua các prompt xác nhận của restore_node.sh
    yes | ./restore_node.sh "$NODE_ID" "" 4
    RESTORE_EXIT_CODE=$?
    if [ $RESTORE_EXIT_CODE -ne 0 ]; then
        echo "❌ LỖI: Thao tác restore_node.sh thất bại!"
        kill -9 $CHECKER_PID 2>/dev/null
        exit 1
    fi
    echo "   ✅ Restore Node $NODE_ID hoàn tất."

    # 5. Chạy test giao dịch
    echo "👉 Bước 5: Chạy rpc-tcp-simple.sh để kiểm tra giao dịch (trigger block mới)..."
    cd "$SCRIPTS_DIR" || exit 1
    ./rpc-tcp-simple.sh
    RPC_EXIT=$?
    if [ $RPC_EXIT -ne 0 ]; then
        echo "❌ LỖI: rpc-tcp-simple.sh thất bại (exit code $RPC_EXIT). Dừng pipeline!"
        kill -9 $CHECKER_PID 2>/dev/null
        exit 1
    fi

    # 6. Chờ để kiểm tra hash cuối cùng
    echo "👉 Bước 6: Chờ 10s để block_hash_checker xác minh các node có cùng hash block không..."
    sleep 10

    if ! kill -0 $CHECKER_PID 2>/dev/null; then
        echo "❌ LỖI: block_hash_checker phát hiện lệch hash sau khi chạy rpc-tcp-simple.sh!"
        echo "   Xem log tại: $CHECKER_DIR/hash_checker_loop_${i}.log"
        tail -n 20 "$CHECKER_DIR/hash_checker_loop_${i}.log"
        exit 1
    else
        echo "   ✅ Khớp Hash! Không phát hiện lỗi phân nhánh."
    fi

    # 7. Dọn dẹp tiến trình checker cho vòng lặp hiện tại
    echo "👉 Bước 7: Dọn dẹp tiến trình block_hash_checker cho vòng lặp hiện tại..."
    kill -9 $CHECKER_PID 2>/dev/null
    wait $CHECKER_PID 2>/dev/null

    echo "🎉 Hoàn thành vòng lặp $i / $LOOPS thành công!"
done

echo ""
echo "====================================================================="
echo "🎊 TẤT CẢ $LOOPS VÒNG LẶP TEST SNAPSHOT ĐÃ HOÀN TẤT THÀNH CÔNG!"
echo "====================================================================="
kill $MONITOR_PID 2>/dev/null || true
