#!/bin/bash
# run_snapshot_loop.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=================================================="
echo "🔄 BẮT ĐẦU VÒNG LẶP TEST - DEBUG - FIX SNAPSHOT"
echo "=================================================="

# Dọn dẹp các tiến trình cũ trước khi chạy
pkill -f "ci_snapshot_monitor.py" || true
sleep 1

# Run ci_monitor.sh với type snapshot và --no-listen
./ci_monitor.sh --type snapshot --no-listen --clean-logs

# Chờ 5 giây để Python monitor khởi tạo xong
sleep 5

# Tìm PID của ci_snapshot_monitor.py
MONITOR_PID=$(pgrep -f "ci_snapshot_monitor.py" | grep -v "$$" | head -n 1)

if [ -z "$MONITOR_PID" ]; then
    echo "❌ Không tìm thấy tiến trình ci_snapshot_monitor.py!"
    # Check if there is an error report
    if [ -f "$SCRIPT_DIR/error_report.txt" ]; then
        cat "$SCRIPT_DIR/error_report.txt"
    fi
    exit 1
fi

echo "📡 Đang monitor PID: $MONITOR_PID"
echo "📄 Thư mục Logs: $SCRIPT_DIR/snapshot_test_logs"

# Lấy log file mới nhất để theo dõi
LATEST_LOG=$(ls -t "$SCRIPT_DIR/snapshot_test_logs"/*.log 2>/dev/null | head -n 1)
echo "📄 Log file: $LATEST_LOG"

# Loop checking if the monitor process is still alive
CHECK_COUNT=0
while kill -0 "$MONITOR_PID" 2>/dev/null; do
    sleep 15
    CHECK_COUNT=$((CHECK_COUNT + 1))
    
    # Mỗi 1 phút in ra dòng status hoặc 5 dòng log mới nhất để giữ terminal hoạt động
    if [ $((CHECK_COUNT % 4)) -eq 0 ]; then
        echo "⏰ [$(date '+%H:%M:%S')] Monitor vẫn đang chạy..."
        LATEST_LOG_CURRENT=$(ls -t "$SCRIPT_DIR/snapshot_test_logs"/*.log 2>/dev/null | head -n 1)
        if [ -n "$LATEST_LOG_CURRENT" ] && [ -f "$LATEST_LOG_CURRENT" ]; then
            LATEST_LOG="$LATEST_LOG_CURRENT"
            echo "--- Log 5 dòng cuối ($LATEST_LOG) ---"
            tail -n 5 "$LATEST_LOG"
            echo "-----------------------"
        fi
    fi
done

echo "🛑 Tiến trình ci_snapshot_monitor.py đã dừng!"

# Kiểm tra xem có lỗi trong file báo lỗi dừng khẩn cấp không
if [ -f /tmp/MTN_CHAIN_ERROR_STOP ]; then
    echo "🚨 LỖI PHÁT HIỆN TỪ /tmp/MTN_CHAIN_ERROR_STOP:"
    cat /tmp/MTN_CHAIN_ERROR_STOP
    echo ""
    exit 1
fi

# Hoặc kiểm tra error_report.txt
if [ -f "$SCRIPT_DIR/error_report.txt" ]; then
    echo "🚨 LỖI PHÁT HIỆN TỪ error_report.txt:"
    cat "$SCRIPT_DIR/error_report.txt"
    echo ""
    exit 1
fi

# Hoặc xem log
if [ -n "$LATEST_LOG" ] && [ -f "$LATEST_LOG" ]; then
    if grep -qE "FATAL|ERROR|Lỗi|LỆCH|exit status 1" "$LATEST_LOG"; then
        echo "🚨 PHÁT HIỆN TỪ KHÓA LỖI TRONG LOG FILE:"
        tail -n 50 "$LATEST_LOG"
        exit 1
    fi
fi

echo "✅ Test hoàn tất không phát hiện lỗi."
