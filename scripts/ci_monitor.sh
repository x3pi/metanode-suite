#!/bin/bash
# =============================================================
# ci_monitor.sh — Quản lý và Khởi động các CI Monitor của Metanode
#
# Chức năng:
#   1. Kill TẤT CẢ các loại monitor và test scripts cũ đang chạy ngầm
#      (ci_monitor.py, ci_recovery_monitor.py, ci_snapshot_monitor.py,
#       auto_test.sh, test-node-recovery-gap.sh, test-snapshot.sh)
#   2. Dọn dẹp triệt để các tiến trình cluster và giải phóng port
#   3. Khởi động loại Monitor được chỉ định (mặc định: spam)
#
# Cách dùng:
#   ./ci_monitor.sh --type spam [--clean-logs] [--mode single]
#   ./ci_monitor.sh --type recovery [--clean-logs]
#   ./ci_monitor.sh --type snapshot [--clean-logs]
# =============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# ─── Đảm bảo duy nhất một instance ci_monitor.sh hoạt động ───
MY_PID=$$
OTHER_PIDS=$(pgrep -f "ci_monitor.sh" | grep -v "^$MY_PID$" || true)
if [ -n "$OTHER_PIDS" ]; then
    echo "⚠️ Phát hiện phiên bản ci_monitor.sh khác đang chạy (PIDs: $OTHER_PIDS). Tiến hành tắt tiến trình cũ..."
    for pid in $OTHER_PIDS; do
        kill -9 "$pid" 2>/dev/null || true
    done
    sleep 1
fi

# Mặc định các thông số
TYPE="spam"
CLEAN_LOGS=false
CI_ARGS=()

# Parse arguments
DO_STOP=false
while [[ "$#" -gt 0 ]]; do
    case $1 in
        --type)
            TYPE="$2"
            shift
            ;;
        --clean-logs)
            CLEAN_LOGS=true
            ;;
        stop|--stop|kill|--kill)
            DO_STOP=true
            ;;
        *)
            CI_ARGS+=("$1")
            ;;
    esac
    shift
done

if [ "$DO_STOP" = true ]; then
    TYPE="all" # Skip type validation when stopping
elif [[ "$TYPE" != "spam" && "$TYPE" != "recovery" && "$TYPE" != "snapshot" && "$TYPE" != "spam_xapian" ]]; then
    echo "❌ Lỗi: Loại monitor không hợp lệ. Chỉ chấp nhận: spam, recovery, snapshot, spam_xapian."
    exit 1
fi

# Thiết lập file tương ứng với từng loại
if [ "$TYPE" = "spam" ]; then
    MONITOR_NAME="ci_monitor"
    TEST_SCRIPT="auto_test.sh"
    LOGS_DIR_NAME="auto_test_logs"
    # Default mode cho spam nếu không truyền gì
    if [ ${#CI_ARGS[@]} -eq 0 ]; then
        CI_ARGS=("--mode" "single")
    fi
elif [ "$TYPE" = "recovery" ]; then
    MONITOR_NAME="ci_recovery_monitor"
    TEST_SCRIPT="test-node-recovery-gap.sh"
    LOGS_DIR_NAME="recovery_test_logs"
elif [ "$TYPE" = "spam_xapian" ]; then
    MONITOR_NAME="ci_spam_xapian_monitor"
    TEST_SCRIPT="test-spam-xapian.sh"
    LOGS_DIR_NAME="spam_xapian_logs"
else
    MONITOR_NAME="ci_snapshot_monitor"
    TEST_SCRIPT="test-snapshot.sh"
    LOGS_DIR_NAME="snapshot_test_logs"
fi

CI_MONITOR="$SCRIPT_DIR/${MONITOR_NAME}.py"
CI_LOG="$SCRIPT_DIR/${MONITOR_NAME}.log"
AUTO_TEST_LOGS="$SCRIPT_DIR/${LOGS_DIR_NAME}"

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

cleanup_all_processes() {
    echo ""
    echo "🔪 Dọn dẹp TOÀN BỘ tiến trình cũ đang chạy ngầm..."

    # 1. Kill tất cả Python monitors
    for m in "ci_monitor.py" "ci_recovery_monitor.py" "ci_snapshot_monitor.py" "ci_spam_xapian_monitor.py"; do
        local pat="[${m:0:1}]${m:1}"
        PIDS_M=$(pgrep -f "$pat" 2>/dev/null)
        if [ -n "$PIDS_M" ]; then
            echo "   → Tìm thấy $m (PIDs: $PIDS_M). Đang kill..."
            pkill -f "$pat" 2>/dev/null
            sleep 0.5
            pkill -9 -f "$pat" 2>/dev/null || true
        fi
    done

    # 2. Kill tất cả Shell test runners
    for t in "auto_test.sh" "test-node-recovery-gap.sh" "test-snapshot.sh" "test-spam-xapian.sh" "run_spam.sh"; do
        local pat="[${t:0:1}]${t:1}"
        PIDS_T=$(pgrep -f "$pat" 2>/dev/null)
        if [ -n "$PIDS_T" ]; then
            echo "   → Tìm thấy $t (PIDs: $PIDS_T). Đang kill..."
            pkill -f "$pat" 2>/dev/null
            sleep 0.5
            pkill -9 -f "$pat" 2>/dev/null || true
        fi
    done

    # 3. Kill các tiến trình phụ trợ
    for proc in "block_hash_checker" "rpc-tcp-simple" "tps_blast_cc" "tx_sender" "spam_xapian_test"; do
        local pat="[${proc:0:1}]${proc:1}"
        if pgrep -f "$pat" >/dev/null; then
            echo "   → Tìm thấy $proc. Đang kill..."
            pkill -f "$pat" 2>/dev/null
            sleep 0.5
            pkill -9 -f "$pat" 2>/dev/null || true
        fi
    done

    # 3.1. Kill các tool chạy qua go run (watch/loop)
    pkill -9 -f "main.go -loop" 2>/dev/null || true
    pkill -9 -f "main.go --watch" 2>/dev/null || true

    # 3.5. Dừng Nginx nếu đang chạy và chiếm cổng
    echo "   → Tắt dịch vụ Nginx (giải quyết lỗi 502 Bad Gateway)..."
    pkill -9 nginx 2>/dev/null || true

    # 4. Kill các session tmux liên quan đến cluster
    echo "   → Tìm và tắt các tmux sessions go-master, metanode, rpc-proxy..."
    for session in $(tmux list-sessions -F '#S' 2>/dev/null | grep -E '^(go-master-|metanode-|rpc-proxy)'); do
        echo "     - Tắt tmux session: $session"
        tmux kill-session -t "$session" 2>/dev/null || true
    done

    # 5. Tắt triệt để các tiến trình Go/Rust và các RPC proxy server
    echo "   → Tắt triệt để các tiến trình simple_chain, metanode, rpc-client, config-rpc..."
    pkill -9 -f "[s]imple_chain" 2>/dev/null || true
    pkill -9 -f "[m]etanode" 2>/dev/null || true
    pkill -9 -f "[r]pc-client" 2>/dev/null || true
    pkill -9 -f "config-rpc-node" 2>/dev/null || true
    pkill -9 -f "config-client-tcp" 2>/dev/null || true

    # 6. Giải phóng port của cluster
    echo "   → Giải phóng các port của cụm cluster..."
    wait_for_ports_to_release 8545 8757 10747 10748 10749 10750 9100 9101 9102 9103 9104 19200 19201 19202 19203 19204 8547 8548 8549 8550

    # Xóa cờ lỗi dừng test cũ để tránh nhận diện nhầm
    rm -f /tmp/MTN_CHAIN_ERROR_STOP

    echo "   ✅ Đã dọn sạch toàn bộ tiến trình và giải phóng các port."
}

if [ "$DO_STOP" = true ]; then
    echo "======================================================="
    echo "🛑 YÊU CẦU DỪNG TẤT CẢ TIẾN TRÌNH"
    echo "======================================================="
    cleanup_all_processes
    exit 0
fi

echo "======================================================="
echo "🧹 KHỞI ĐỘNG HỆ THỐNG GIÁM SÁT CI: [${TYPE^^}]"
echo "======================================================="

# ─── Bước 1: Kill TẤT CẢ tiến trình cũ để tránh đè cổng ───────
echo ""
echo "🔪 [1/3] Đang dọn dẹp hệ thống..."
cleanup_all_processes

# ─── Bước 2: Xóa logs cũ theo từng loại ─────────────────────────
echo ""
if [ "$CLEAN_LOGS" = true ]; then
    echo "🗑️  [2/3] Xóa logs cũ (--clean-logs) của loại [${TYPE}]..."

    if [ -f "$CI_LOG" ]; then
        rm -f "$CI_LOG"
        echo "   → Đã xóa: ${MONITOR_NAME}.log"
    fi

    if [ -d "$AUTO_TEST_LOGS" ]; then
        # Giữ lại 1 file log mới nhất, xóa các file cũ
        LOG_FILES=($(ls -t "$AUTO_TEST_LOGS"/*.log 2>/dev/null))
        FILE_COUNT=${#LOG_FILES[@]}
        
        if [ "$FILE_COUNT" -gt 1 ]; then
            FILES_TO_DELETE=("${LOG_FILES[@]:1}")
            rm -f "${FILES_TO_DELETE[@]}"
            echo "   → Đã xóa $((${FILE_COUNT}-1)) files cũ trong ${LOGS_DIR_NAME}/, giữ lại 1 file mới nhất."
        elif [ "$FILE_COUNT" -eq 1 ]; then
            echo "   → Chỉ có 1 file log trong ${LOGS_DIR_NAME}/, đã giữ lại."
        else
            echo "   → Thư mục ${LOGS_DIR_NAME}/ trống."
        fi
    fi

    if [ -f "$SCRIPT_DIR/error_report.txt" ]; then
        rm -f "$SCRIPT_DIR/error_report.txt"
        echo "   → Đã xóa: error_report.txt"
    fi

    echo "   ✅ Đã dọn sạch logs."
else
    echo "📁 [2/3] Giữ lại logs cũ (mặc định)."
fi

# ─── Bước 3: Khởi động Monitor mới ────────────────────────
echo ""
echo "🚀 [3/3] Khởi động ${MONITOR_NAME}.py mới..."
echo "   → Args: ${CI_ARGS[*]}"
echo "   → Log:  $CI_LOG"

# Ghi lại log file mới nhất hiện tại trước khi khởi chạy
PREV_LATEST=""
if [ -d "$AUTO_TEST_LOGS" ]; then
    PREV_LATEST=$(ls -t "$AUTO_TEST_LOGS"/*.log 2>/dev/null | head -n 1)
fi

# Đợi các port cũ giải phóng hoàn toàn
wait_for_ports_to_release 8545 8757 10747 10748 10749 10750 9100 9101 9102 9103 9104 19200 19201 19202 19203 19204 8547 8548 8549 8550


export MTN_TELE_ALERT=true
nohup "$CI_MONITOR" "${CI_ARGS[@]}" > "$CI_LOG" 2>&1 &
NEW_PID=$!

# Đợi 1 giây rồi kiểm tra xem process có sống không
sleep 1
if kill -0 "$NEW_PID" 2>/dev/null; then
    echo "   ✅ Đã khởi động thành công! (PID: $NEW_PID)"
else
    echo "   ❌ Process đã chết ngay sau khi khởi động! Kiểm tra log:"
    cat "$CI_LOG" 2>/dev/null
    exit 1
fi

# Chờ tối đa 5 giây để Python monitor thực hiện git ls-remote/pull và tạo log file mới
LATEST_LOG_FILE=""
for i in {1..50}; do
    LATEST_LOG_FILE=$(ls -t "$AUTO_TEST_LOGS"/*.log 2>/dev/null | head -n 1)
    if [ -n "$LATEST_LOG_FILE" ] && [ "$LATEST_LOG_FILE" != "$PREV_LATEST" ]; then
        break
    fi
    sleep 0.1
done

# Fallback nếu không tìm thấy log file mới hoặc chưa tạo kịp
if [ -z "$LATEST_LOG_FILE" ]; then
    LATEST_LOG_FILE=$(ls -t "$AUTO_TEST_LOGS"/*.log 2>/dev/null | head -n 1)
fi

echo ""
echo "======================================================="
echo "📋 THÔNG TIN TIỆN ÍCH"
echo "======================================================="
echo "  Xem log realtime:  tail -f $CI_LOG"
if [ -n "$LATEST_LOG_FILE" ]; then
    echo "  Xem log test:      tail -f $LATEST_LOG_FILE"
else
    echo "  Xem log test:      tail -f $AUTO_TEST_LOGS/*.log"
fi
echo "  Kill CI Monitor:   pkill -f ${MONITOR_NAME}.py"
echo "======================================================="
