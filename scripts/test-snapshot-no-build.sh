#!/bin/bash

# Mặc định cấu hình
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
    echo "  --loops <num>       Số vòng lặp test (mặc định: $LOOPS)"
    echo "  --tps-rounds <num>  Số round chạy TPS blast mỗi vòng (mặc định: $TPS_ROUNDS)"
    echo "  --tps-count <num>   Số lượng giao dịch TPS mỗi round (mặc định: $TPS_COUNT)"
    echo "  --target-node <num> Cố định test trên 1 Node (vd: 2). Nếu không có, tự động xoay vòng."
    echo "  -h, --help          Hiển thị trợ giúp này"
    echo ""
    echo "Ví dụ:"
    echo "  $0 --loops 5 --tps-rounds 2"
    echo "========================================================="
}

# Parse các tham số truyền vào
TARGET_NODE=""
while [[ "$#" -gt 0 ]]; do
    case $1 in
        --loops) LOOPS="$2"; shift ;;
        --tps-rounds) TPS_ROUNDS="$2"; shift ;;
        --tps-count) TPS_COUNT="$2"; shift ;;
        --target-node) TARGET_NODE="$2"; shift ;;
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

HISTORY_CONFIG="config-local.json"
HASH_CONFIG_ARG=""
if [ "${DEPLOY_MODE:-single}" == "multi" ]; then
    HISTORY_CONFIG="config-mutil.json"
    HASH_CONFIG_ARG="--config config-m-nodes.json"
fi

# Xóa cờ lỗi cũ trước khi chạy
rm -f /tmp/MTN_CHAIN_ERROR_STOP /tmp/pending_check_*.json

# Theo dõi cờ lỗi từ Hash Checker ngầm & Giám sát sự sống của Node
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
        
        # Kiểm tra sự sống của các node
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
                            echo "Node HTTP Server ($node_key: $node_url) không phản hồi. Có thể process đã bị crash!" > /tmp/MTN_CHAIN_ERROR_STOP
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
                        echo "Node HTTP Server (Node $node_id tại cổng $port) không phản hồi. Có thể process đã bị crash!" > /tmp/MTN_CHAIN_ERROR_STOP
                        break
                    fi
                fi
            done
        fi
        
        sleep 2
    done
}
monitor_error_flag &
MONITOR_PID=$!

# Xử lý tín hiệu Ctrl+C (SIGINT) để kill sạch các tiến trình chạy ngầm
trap 'echo -e "\n🛑 Đang dọn dẹp tiến trình..."; rm -f /tmp/pending_check_*.json /tmp/MTN_EXCLUDE_NODES; kill -9 $CHECKER_PID $TPS_PID $MONITOR_PID 2>/dev/null || true; pkill -P $$ 2>/dev/null || true; exit 1' SIGINT SIGTERM

echo "====================================================================="
echo "🚀 BƯỚC 0: CHẠY SIMPLE TEST ĐỂ KHỞI TẠO VÀ TEST MẠNG CƠ BẢN"
echo "====================================================================="
cd "$SCRIPTS_DIR" || exit 1

for ((i=1; i<=LOOPS; i++)); do
    # Xác định Node luân phiên (0, 1, 2, 3), trừ node 4 (dùng để tải snapshot)
    if [ -n "$TARGET_NODE" ]; then
        NODE_ID=$TARGET_NODE
    else
        NODE_ID=$(( (i - 1) % 4 ))
    fi

    echo ""
    echo "====================================================================="
    echo "🔄 BẮT ĐẦU VÒNG LẶP TEST SNAPSHOT: $i / $LOOPS (Node: $NODE_ID)"
    echo "====================================================================="

    # 1. Khởi động block_hash_checker chạy ngầm
    echo "👉 Bước 1: Khởi động block_hash_checker chạy ngầm..."
    cd "$CHECKER_DIR" || exit 1
    go run main.go --watch --interval 5s --lag-threshold 1000 $HASH_CONFIG_ARG > "hash_checker_loop_${i}.log" 2>&1 &
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
            echo "❌ LỖI NGHIÊM TRỌNG (Đang test Node $NODE_ID): block_hash_checker đã bị kill (khả năng phát hiện lệch hash)!"
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
        echo "❌ LỖI (Đang test Node $NODE_ID): Tiến trình TPS blast thất bại (exit code $TPS_EXIT_CODE)"
        kill -9 $CHECKER_PID 2>/dev/null
        exit 1
    fi
    echo "   ✅ Chạy TPS hoàn tất."

    # 3. Restore Node
    echo "👉 Bước 3: Restore Node $NODE_ID từ snapshot của Node 4..."
    echo "$NODE_ID" > /tmp/MTN_EXCLUDE_NODES
    echo "📥 Lưu trạng thái lịch sử trước khi restore Node $NODE_ID..."
    (
        cd "$TOOL_TEST_DIR/test-simple/test-rpc/test-history"
        go run main.go -config $HISTORY_CONFIG -action save -file "/tmp/pending_check_${NODE_ID}.json"
    )
    cd "$METANODE_SCRIPT_DIR" || exit 1
    # Thực hiện restore node tùy theo mode
    if [ "${DEPLOY_MODE:-single}" == "multi" ]; then
        ./deploy_systemd_cluster.sh --env deploy-muti-node.env --restore-node "$NODE_ID"
    else
        # Tự động gửi phím "y" liên tục để vượt qua các prompt xác nhận của restore_node.sh
        yes | ./restore_node.sh "$NODE_ID" "" 4
    fi
    RESTORE_EXIT_CODE=$?
    if [ $RESTORE_EXIT_CODE -ne 0 ]; then
        echo "❌ LỖI (Đang test Node $NODE_ID): Thao tác restore_node.sh thất bại!"
        kill -9 $CHECKER_PID 2>/dev/null
        exit 1
    fi
    echo "   ✅ Restore Node $NODE_ID hoàn tất."

    # 4. Chờ các node đồng bộ block (dựa theo log CHÊNH)
    echo "👉 Bước 4: Chờ các node đồng bộ block (theo dõi 'CHÊNH' trong log)..."
    MAX_WAIT=60  # Tối đa 60 lần * 5s = 300 s
    PREV_DIFF=9999999
    STALL_COUNT=0
    SYNCED=false

    for ((w=1; w<=MAX_WAIT; w++)); do
        # Lấy dòng log mới nhất có chứa "Heights:"
        LAST_LINE=$(grep "Heights:" "$CHECKER_DIR/hash_checker_loop_${i}.log" | tail -n 1)
        
        if [[ "$LAST_LINE" == *"Heights:"* ]]; then
            NODE_HEIGHT_STR=$(echo "$LAST_LINE" | grep -o "m${NODE_ID}=[0-9]*" || echo "")
            NODE_HEIGHT=${NODE_HEIGHT_STR#*=}
            
            if [ -z "$NODE_HEIGHT" ] || [ "$NODE_HEIGHT" == "ERR" ]; then
                echo "   ⏳ Đang chờ Node $NODE_ID phản hồi..."
            else
                MAX_HEIGHT=$(echo "$LAST_LINE" | grep -o "m[0-4]=[0-9]*" | cut -d= -f2 | sort -nr | head -n1)
                
                NODE_DIFF=$(( MAX_HEIGHT - NODE_HEIGHT ))
                if [ "$NODE_DIFF" -le 10 ]; then
                    echo "   ✅ Node $NODE_ID đã đồng bộ đủ gần nhau (chênh lệch: $NODE_DIFF blocks)!"
                    SYNCED=true
                    break
                else
                    CUR_DIFF=$NODE_DIFF
                    echo "   ⏳ Đang chờ Node $NODE_ID đồng bộ... Chênh lệch: $CUR_DIFF blocks (Max: $MAX_HEIGHT, Node: $NODE_HEIGHT)"
                    if [ "$CUR_DIFF" -lt "$PREV_DIFF" ]; then
                        PREV_DIFF=$CUR_DIFF
                        STALL_COUNT=0
                    elif [ "$CUR_DIFF" -eq "$PREV_DIFF" ]; then
                        STALL_COUNT=$((STALL_COUNT + 1))
                        if [ $STALL_COUNT -ge 12 ]; then # 12 lần liên tiếp (~60s) không giảm
                            echo "❌ LỖI (Đang test Node $NODE_ID): Tiến trình đồng bộ của Node $NODE_ID bị kẹt ở mức chênh $CUR_DIFF blocks trong 60s!"
                            kill -9 $CHECKER_PID 2>/dev/null
                            exit 1
                        fi
                    else
                        PREV_DIFF=$CUR_DIFF
                        STALL_COUNT=0
                    fi
                fi
            fi
        fi
        sleep 5
    done

    if [ "$SYNCED" = false ]; then
        echo "❌ LỖI (Đang test Node $NODE_ID): Quá thời gian chờ đồng bộ (300 giây)!"
        kill -9 $CHECKER_PID 2>/dev/null
        exit 1
    fi
    
    echo "   👉 Chờ thêm 5s sau khi đồng bộ để hệ thống thật sự lưu state..."
    sleep 5
    rm -f /tmp/MTN_EXCLUDE_NODES
    
    if ! kill -0 $CHECKER_PID 2>/dev/null; then
        echo "❌ LỖI (Đang test Node $NODE_ID): block_hash_checker đã báo lệch hash và thoát sau khi bơm TPS!"
        echo "   Xem log tại: $CHECKER_DIR/hash_checker_loop_${i}.log"
        tail -n 20 "$CHECKER_DIR/hash_checker_loop_${i}.log"
        exit 1
    fi

    echo "📤 Xác minh trạng thái lịch sử trên node $NODE_ID..."
    (
        cd "$TOOL_TEST_DIR/test-simple/test-rpc/test-history"
        go run main.go -config $HISTORY_CONFIG -action verify -file "/tmp/pending_check_${NODE_ID}.json" -target-node "$NODE_ID"
    )

    # echo "🔎 Gọi API meta_verifyHistoricalRoot để kiểm chứng tính toàn vẹn của State Trie trên Node $NODE_ID..."
    # if [ "$NODE_ID" = "0" ]; then TARGET_PORT=8757; fi
    # if [ "$NODE_ID" = "1" ]; then TARGET_PORT=10747; fi
    # if [ "$NODE_ID" = "2" ]; then TARGET_PORT=10749; fi
    # if [ "$NODE_ID" = "3" ]; then TARGET_PORT=10750; fi
    # if [ "$NODE_ID" = "4" ]; then TARGET_PORT=10748; fi

    # VERIFY_RES=$(curl -s -X POST http://127.0.0.1:$TARGET_PORT \
    #     -H "Content-Type: application/json" \
    #     -d '{"jsonrpc":"2.0","method":"meta_verifyHistoricalRoot","params":["latest"],"id":1}')
    
    # if echo "$VERIFY_RES" | grep -q '"match":true'; then
    #     echo "   ✅ VerifyHistoricalRoot thành công: StateRoot hoàn toàn khớp với Database sau khi khôi phục!"
    # else
    #     echo "❌ LỖI NGHIÊM TRỌNG (Đang test Node $NODE_ID): StateRoot không khớp (MISMATCH) sau khi khôi phục Snapshot!"
    #     echo "   Kết quả API: $VERIFY_RES"
    #     kill -9 $CHECKER_PID 2>/dev/null
    #     exit 1
    # fi

    # 5. Chạy test giao dịch
    echo "👉 Bước 5: Chạy giao dịch kiểm tra trên node vừa khôi phục..."
    echo "   ⏳ Đợi 10s cho node $NODE_ID ổn định sau khi restore..."
    sleep 10
    
    SPAM_NODE=0
    if [ "$NODE_ID" = "0" ]; then
        SPAM_NODE=1
    fi
    
    cd "$TPS_DIR" || exit 1
    echo "👉 Chạy giao dịch lên chính node vừa khôi phục ($NODE_ID)..."
    go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10 --target-node $NODE_ID > blast_restore_node.log 2>&1
    TPS_REC_EXIT=$?
    if [ $TPS_REC_EXIT -ne 0 ]; then
        echo "❌ LỖI (Đang test Node $NODE_ID): TPS blast lên Node $NODE_ID thất bại (exit code $TPS_REC_EXIT)!"
        echo "--- LOG CHI TIẾT ---"
        cat blast_restore_node.log
        kill -9 $CHECKER_PID 2>/dev/null
        exit 1
    fi
    
    echo "👉 Đổi sang chạy giao dịch qua node khác ($SPAM_NODE)..."
    go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10 --target-node $SPAM_NODE > blast_other_node.log 2>&1
    TPS_OTH_EXIT=$?
    if [ $TPS_OTH_EXIT -ne 0 ]; then
        echo "❌ LỖI (Đang test Node $NODE_ID): TPS blast lên Node $SPAM_NODE thất bại (exit code $TPS_OTH_EXIT)!"
        echo "--- LOG CHI TIẾT ---"
        cat blast_other_node.log
        kill -9 $CHECKER_PID 2>/dev/null
        exit 1
    fi
    echo "   ✅ Các giao dịch kiểm tra hoàn tất thành công!"

    # 6. Chờ để kiểm tra hash cuối cùng
    echo "👉 Bước 6: Chờ 10s để block_hash_checker xác minh các node có cùng hash block không..."
    sleep 10

    if ! kill -0 $CHECKER_PID 2>/dev/null; then
        echo "❌ LỖI (Đang test Node $NODE_ID): block_hash_checker phát hiện lệch hash sau khi chạy rpc-tcp-simple.sh!"
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
