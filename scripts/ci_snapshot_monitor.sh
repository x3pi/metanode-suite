#!/bin/bash
# =============================================================
# ci_snapshot_monitor.sh — Khởi động Snapshot Monitor sạch sẽ
# =============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CI_MONITOR="$SCRIPT_DIR/ci_snapshot_monitor.py"
CI_LOG="$SCRIPT_DIR/ci_snapshot_monitor.log"
AUTO_TEST_LOGS="$SCRIPT_DIR/snapshot_test_logs"
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
echo "🧹 KHỞI ĐỘNG LẠI SNAPSHOT MONITOR (Clean Start)"
echo "======================================================="

# ─── Bước 1: Kill tất cả tiến trình cũ ───────────────────────
echo ""
echo "🔪 [1/3] Dọn dẹp tiến trình cũ..."

PIDS_MONITOR=$(pgrep -f "ci_snapshot_monitor.py" 2>/dev/null)
if [ -n "$PIDS_MONITOR" ]; then
    echo "   → Tìm thấy ci_snapshot_monitor.py (PIDs: $PIDS_MONITOR). Đang kill..."
    pkill -f "ci_snapshot_monitor.py" 2>/dev/null
    sleep 1
    PIDS_REMAIN=$(pgrep -f "ci_snapshot_monitor.py" 2>/dev/null)
    if [ -n "$PIDS_REMAIN" ]; then
        echo "   → Tiến trình ngoan cố, dùng SIGKILL..."
        pkill -9 -f "ci_snapshot_monitor.py" 2>/dev/null
    fi
else
    echo "   → Không có ci_snapshot_monitor.py nào đang chạy."
fi

# Kill test-snapshot.sh
PIDS_TEST=$(pgrep -f "test-snapshot.sh" 2>/dev/null)
if [ -n "$PIDS_TEST" ]; then
    echo "   → Tìm thấy test-snapshot.sh. Đang kill..."
    pkill -f "test-snapshot.sh" 2>/dev/null
    sleep 1
    pkill -9 -f "test-snapshot.sh" 2>/dev/null
else
    echo "   → Không có test-snapshot.sh nào đang chạy."
fi

# Kill các tiến trình phụ trợ
for proc in "block_hash_checker" "rpc-tcp-simple" "tps_blast_cc"; do
    if pgrep -f "$proc" >/dev/null; then
        echo "   → Tìm thấy $proc. Đang kill..."
        pkill -f "$proc" 2>/dev/null
    fi
done

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
        echo "   → Đã xóa: ci_snapshot_monitor.log"
    fi

    if [ -d "$AUTO_TEST_LOGS" ]; then
        LOG_FILES=($(ls -t "$AUTO_TEST_LOGS"/*.log 2>/dev/null))
        FILE_COUNT=${#LOG_FILES[@]}
        
        if [ "$FILE_COUNT" -gt 1 ]; then
            FILES_TO_DELETE=("${LOG_FILES[@]:1}")
            rm -f "${FILES_TO_DELETE[@]}"
            echo "   → Đã xóa $((${FILE_COUNT}-1)) files cũ trong snapshot_test_logs/, giữ lại 1 file mới nhất."
        elif [ "$FILE_COUNT" -eq 1 ]; then
            echo "   → Chỉ có 1 file log trong snapshot_test_logs/, đã giữ lại."
        else
            echo "   → Thư mục snapshot_test_logs/ trống."
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
echo "🚀 [3/3] Khởi động ci_snapshot_monitor.py mới..."
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
echo "  Kill CI Monitor:   pkill -f ci_snapshot_monitor.py"
echo "======================================================="
