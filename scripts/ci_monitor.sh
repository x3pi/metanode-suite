#!/bin/bash
# =============================================================
# start_ci.sh — Khởi động CI Monitor sạch sẽ
#
# Chức năng:
#   1. Kill TẤT CẢ tiến trình ci_monitor.py + auto_test.sh cũ
#   2. Xóa toàn bộ logs cũ (ci_monitor.log + auto_test_logs/)
#   3. Khởi động ci_monitor.py mới trong nền (nohup)
#
# Cách dùng:
#   ./start_ci.sh                  # Chạy mặc định (mode single)
#   ./start_ci.sh --mode single    # Tường minh chỉ định mode
#   ./ci_monitor.sh                  # Chạy mặc định (giữ lại logs cũ)
#   ./ci_monitor.sh --mode single    # Tường minh chỉ định mode
#   ./ci_monitor.sh --clean-logs     # Xóa logs cũ khi khởi động
# =============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CI_MONITOR="$SCRIPT_DIR/ci_monitor.py"
CI_LOG="$SCRIPT_DIR/ci_monitor.log"
AUTO_TEST_LOGS="$SCRIPT_DIR/auto_test_logs"
CLEAN_LOGS=false
CI_ARGS=()

# Parse arguments
for arg in "$@"; do
    if [ "$arg" = "--clean-logs" ]; then
        CLEAN_LOGS=true
    else
        CI_ARGS+=("$arg")
    fi
done

# Default mode nếu không truyền gì
if [ ${#CI_ARGS[@]} -eq 0 ]; then
    CI_ARGS=("--mode" "single")
fi

echo "======================================================="
echo "🧹 KHỞI ĐỘNG LẠI CI MONITOR (Clean Start)"
echo "======================================================="

# ─── Bước 1: Kill tất cả tiến trình cũ ───────────────────────
echo ""
echo "🔪 [1/3] Dọn dẹp tiến trình cũ..."

# Kill ci_monitor.py
PIDS_MONITOR=$(pgrep -f "ci_monitor.py" 2>/dev/null)
if [ -n "$PIDS_MONITOR" ]; then
    echo "   → Tìm thấy ci_monitor.py (PIDs: $PIDS_MONITOR). Đang kill..."
    pkill -f "ci_monitor.py" 2>/dev/null
    sleep 1
    # Force kill nếu vẫn còn sống
    PIDS_REMAIN=$(pgrep -f "ci_monitor.py" 2>/dev/null)
    if [ -n "$PIDS_REMAIN" ]; then
        echo "   → Tiến trình ngoan cố, dùng SIGKILL..."
        pkill -9 -f "ci_monitor.py" 2>/dev/null
    fi
else
    echo "   → Không có ci_monitor.py nào đang chạy."
fi

# Kill auto_test.sh (tiến trình con mà ci_monitor spawn ra)
PIDS_TEST=$(pgrep -f "auto_test.sh" 2>/dev/null)
if [ -n "$PIDS_TEST" ]; then
    echo "   → Tìm thấy auto_test.sh (PIDs: $PIDS_TEST). Đang kill..."
    pkill -f "auto_test.sh" 2>/dev/null
    sleep 1
    pkill -9 -f "auto_test.sh" 2>/dev/null
else
    echo "   → Không có auto_test.sh nào đang chạy."
fi

# Kill block_hash_checker (ci_monitor cũng spawn cái này)
PIDS_BHC=$(pgrep -f "block_hash_checker" 2>/dev/null)
if [ -n "$PIDS_BHC" ]; then
    echo "   → Tìm thấy block_hash_checker (PIDs: $PIDS_BHC). Đang kill..."
    pkill -f "block_hash_checker" 2>/dev/null
fi

# Kill all master nodes and legacy metanode tmux sessions
echo "   → Tìm và tắt các tmux sessions go-master, metanode, rpc-proxy..."
for session in $(tmux list-sessions -F '#S' 2>/dev/null | grep -E '^(go-master-|metanode-|rpc-proxy)'); do
    echo "     - Tắt tmux session: $session"
    tmux kill-session -t "$session" 2>/dev/null || true
done

# Force terminate any simple_chain or metanode processes directly
echo "   → Tắt triệt để các tiến trình simple_chain, metanode, rpc-client..."
pkill -9 -f "simple_chain" 2>/dev/null || true
pkill -9 -f "metanode" 2>/dev/null || true
pkill -9 -f "rpc-client" 2>/dev/null || true

# Force free ports
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


# ─── Bước 2: Xóa logs cũ ─────────────────────────────────────
echo ""
if [ "$CLEAN_LOGS" = true ]; then
    echo "🗑️  [2/3] Xóa logs cũ (--clean-logs)..."

    if [ -f "$CI_LOG" ]; then
        rm -f "$CI_LOG"
        echo "   → Đã xóa: ci_monitor.log"
    fi

    if [ -d "$AUTO_TEST_LOGS" ]; then
        # Keep the newest log file, delete the rest
        LOG_FILES=($(ls -t "$AUTO_TEST_LOGS"/*.log 2>/dev/null))
        FILE_COUNT=${#LOG_FILES[@]}
        
        if [ "$FILE_COUNT" -gt 1 ]; then
            FILES_TO_DELETE=("${LOG_FILES[@]:1}")
            rm -f "${FILES_TO_DELETE[@]}"
            echo "   → Đã xóa $((${FILE_COUNT}-1)) files cũ trong auto_test_logs/, giữ lại 1 file mới nhất."
        elif [ "$FILE_COUNT" -eq 1 ]; then
            echo "   → Chỉ có 1 file log trong auto_test_logs/, đã giữ lại."
        else
            echo "   → Thư mục auto_test_logs/ trống."
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

# ─── Bước 3: Khởi động ci_monitor mới ────────────────────────
echo ""
echo "🚀 [3/3] Khởi động ci_monitor.py mới..."
echo "   → Args: ${CI_ARGS[*]}"
echo "   → Log:  $CI_LOG"

# Đợi 2 giây để các port cũ đóng hoàn toàn
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

echo ""
echo "======================================================="
echo "📋 THÔNG TIN TIỆN ÍCH"
echo "======================================================="
echo "  Xem log realtime:  tail -f $CI_LOG"
echo "  Xem log test:      ls $AUTO_TEST_LOGS/"
echo "  Kill CI Monitor:   pkill -f ci_monitor.py"
echo "======================================================="
