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
elif [[ "$TYPE" != "spam" && "$TYPE" != "recovery" && "$TYPE" != "snapshot" ]]; then
    echo "❌ Lỗi: Loại monitor không hợp lệ. Chỉ chấp nhận: spam, recovery, snapshot."
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
else
    MONITOR_NAME="ci_snapshot_monitor"
    TEST_SCRIPT="test-snapshot.sh"
    LOGS_DIR_NAME="snapshot_test_logs"
fi

CI_MONITOR="$SCRIPT_DIR/${MONITOR_NAME}.py"
CI_LOG="$SCRIPT_DIR/${MONITOR_NAME}.log"
AUTO_TEST_LOGS="$SCRIPT_DIR/${LOGS_DIR_NAME}"

cleanup_all_processes() {
    echo ""
    echo "🔪 Dọn dẹp TOÀN BỘ tiến trình cũ đang chạy ngầm..."

    # 1. Kill tất cả Python monitors
    for m in "ci_monitor.py" "ci_recovery_monitor.py" "ci_snapshot_monitor.py"; do
        PIDS_M=$(pgrep -f "$m" 2>/dev/null)
        if [ -n "$PIDS_M" ]; then
            echo "   → Tìm thấy $m (PIDs: $PIDS_M). Đang kill..."
            pkill -f "$m" 2>/dev/null
            sleep 0.5
            pkill -9 -f "$m" 2>/dev/null || true
        fi
    done

    # 2. Kill tất cả Shell test runners
    for t in "auto_test.sh" "test-node-recovery-gap.sh" "test-snapshot.sh"; do
        PIDS_T=$(pgrep -f "$t" 2>/dev/null)
        if [ -n "$PIDS_T" ]; then
            echo "   → Tìm thấy $t (PIDs: $PIDS_T). Đang kill..."
            pkill -f "$t" 2>/dev/null
            sleep 0.5
            pkill -9 -f "$t" 2>/dev/null || true
        fi
    done

    # 3. Kill các tiến trình phụ trợ
    for proc in "block_hash_checker" "rpc-tcp-simple" "tps_blast_cc"; do
        if pgrep -f "$proc" >/dev/null; then
            echo "   → Tìm thấy $proc. Đang kill..."
            pkill -f "$proc" 2>/dev/null
            sleep 0.5
            pkill -9 -f "$proc" 2>/dev/null || true
        fi
    done

    # 4. Kill các session tmux liên quan đến cluster
    echo "   → Tìm và tắt các tmux sessions go-master, metanode, rpc-proxy..."
    for session in $(tmux list-sessions -F '#S' 2>/dev/null | grep -E '^(go-master-|metanode-|rpc-proxy)'); do
        echo "     - Tắt tmux session: $session"
        tmux kill-session -t "$session" 2>/dev/null || true
    done

    # 5. Tắt triệt để các tiến trình Go/Rust
    echo "   → Tắt triệt để các tiến trình simple_chain, metanode, rpc-client..."
    pkill -9 -f "simple_chain" 2>/dev/null || true
    pkill -9 -f "metanode" 2>/dev/null || true
    pkill -9 -f "rpc-client" 2>/dev/null || true

    # 6. Giải phóng port của cluster
    echo "   → Giải phóng các port của cluster (8545, 8757, 10747-10750)..."
    for port in 8545 8757 10747 10748 10749 10750; do
        PIDS_PORT=$(lsof -t -i :$port 2>/dev/null)
        if [ -z "$PIDS_PORT" ]; then
            PIDS_PORT=$(ss -tlnp 2>/dev/null | grep -E ":$port " | grep -oP 'pid=\K[0-9]+' | sort -u)
        fi
        if [ -n "$PIDS_PORT" ]; then
            for p in $PIDS_PORT; do
                echo "     - Port $port đang bị giữ bởi PID $p. Đang kill..."
                kill -9 "$p" 2>/dev/null || true
            done
        fi
    done

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

# Đợi 2 giây để port cũ đóng hoàn toàn
sleep 2

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
