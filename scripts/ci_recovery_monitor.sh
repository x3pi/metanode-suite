#!/bin/bash
# =============================================================
# ci_recovery_monitor.sh — Khởi động Recovery Monitor sạch sẽ
# =============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CI_MONITOR="$SCRIPT_DIR/ci_recovery_monitor.py"
CI_LOG="$SCRIPT_DIR/ci_recovery_monitor.log"
AUTO_TEST_LOGS="$SCRIPT_DIR/recovery_test_logs"
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

echo "======================================================="
echo "🧹 KHỞI ĐỘNG LẠI RECOVERY MONITOR (Clean Start)"
echo "======================================================="

# ─── Bước 1: Kill tất cả tiến trình cũ ───────────────────────
echo ""
echo "🔪 [1/3] Dọn dẹp tiến trình cũ..."

PIDS_MONITOR=$(pgrep -f "ci_recovery_monitor.py" 2>/dev/null)
if [ -n "$PIDS_MONITOR" ]; then
    echo "   → Tìm thấy ci_recovery_monitor.py (PIDs: $PIDS_MONITOR). Đang kill..."
    pkill -f "ci_recovery_monitor.py" 2>/dev/null
    sleep 1
    PIDS_REMAIN=$(pgrep -f "ci_recovery_monitor.py" 2>/dev/null)
    if [ -n "$PIDS_REMAIN" ]; then
        echo "   → Tiến trình ngoan cố, dùng SIGKILL..."
        pkill -9 -f "ci_recovery_monitor.py" 2>/dev/null
    fi
else
    echo "   → Không có ci_recovery_monitor.py nào đang chạy."
fi

# Kill test-node-recovery-gap.sh
PIDS_TEST=$(pgrep -f "test-node-recovery-gap.sh" 2>/dev/null)
if [ -n "$PIDS_TEST" ]; then
    echo "   → Tìm thấy test-node-recovery-gap.sh. Đang kill..."
    pkill -f "test-node-recovery-gap.sh" 2>/dev/null
    sleep 1
    pkill -9 -f "test-node-recovery-gap.sh" 2>/dev/null
else
    echo "   → Không có test-node-recovery-gap.sh nào đang chạy."
fi

# Kill các tiến trình phụ trợ
for proc in "block_hash_checker" "rpc-tcp-simple" "tps_blast_cc"; do
    if pgrep -f "$proc" >/dev/null; then
        echo "   → Tìm thấy $proc. Đang kill..."
        pkill -f "$proc" 2>/dev/null
    fi
done

echo "   ✅ Đã dọn sạch tiến trình."

# ─── Bước 2: Xóa logs cũ ─────────────────────────────────────
echo ""
if [ "$CLEAN_LOGS" = true ]; then
    echo "🗑️  [2/3] Xóa logs cũ (--clean-logs)..."

    if [ -f "$CI_LOG" ]; then
        rm -f "$CI_LOG"
        echo "   → Đã xóa: ci_recovery_monitor.log"
    fi

    if [ -d "$AUTO_TEST_LOGS" ]; then
        LOG_FILES=($(ls -t "$AUTO_TEST_LOGS"/*.log 2>/dev/null))
        FILE_COUNT=${#LOG_FILES[@]}
        
        if [ "$FILE_COUNT" -gt 1 ]; then
            FILES_TO_DELETE=("${LOG_FILES[@]:1}")
            rm -f "${FILES_TO_DELETE[@]}"
            echo "   → Đã xóa $((${FILE_COUNT}-1)) files cũ trong recovery_test_logs/, giữ lại 1 file mới nhất."
        elif [ "$FILE_COUNT" -eq 1 ]; then
            echo "   → Chỉ có 1 file log trong recovery_test_logs/, đã giữ lại."
        else
            echo "   → Thư mục recovery_test_logs/ trống."
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
echo "🚀 [3/3] Khởi động ci_recovery_monitor.py mới..."
echo "   → Args: ${CI_ARGS[*]}"
echo "   → Log:  $CI_LOG"

sleep 2

nohup "$CI_MONITOR" "${CI_ARGS[@]}" > "$CI_LOG" 2>&1 &
NEW_PID=$!

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
echo "  Kill CI Monitor:   pkill -f ci_recovery_monitor.py"
echo "======================================================="
