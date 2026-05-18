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
#   ./start_ci.sh --keep-logs      # Giữ lại logs cũ, không xóa
# =============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CI_MONITOR="$SCRIPT_DIR/ci_monitor.py"
CI_LOG="$SCRIPT_DIR/ci_monitor.log"
AUTO_TEST_LOGS="$SCRIPT_DIR/auto_test_logs"
KEEP_LOGS=false
CI_ARGS=()

# Parse arguments
for arg in "$@"; do
    if [ "$arg" = "--keep-logs" ]; then
        KEEP_LOGS=true
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

echo "   ✅ Đã dọn sạch tiến trình."

# ─── Bước 2: Xóa logs cũ ─────────────────────────────────────
echo ""
if [ "$KEEP_LOGS" = true ]; then
    echo "📁 [2/3] Giữ lại logs cũ (--keep-logs)."
else
    echo "🗑️  [2/3] Xóa logs cũ..."

    if [ -f "$CI_LOG" ]; then
        rm -f "$CI_LOG"
        echo "   → Đã xóa: ci_monitor.log"
    fi

    if [ -d "$AUTO_TEST_LOGS" ]; then
        FILE_COUNT=$(find "$AUTO_TEST_LOGS" -type f | wc -l)
        rm -rf "$AUTO_TEST_LOGS"
        echo "   → Đã xóa thư mục auto_test_logs/ ($FILE_COUNT files)"
    fi

    if [ -f "$SCRIPT_DIR/error_report.txt" ]; then
        rm -f "$SCRIPT_DIR/error_report.txt"
        echo "   → Đã xóa: error_report.txt"
    fi

    echo "   ✅ Đã dọn sạch logs."
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
