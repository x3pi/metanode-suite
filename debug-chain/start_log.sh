#!/bin/bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TARGET_IP="139.59.243.85"
TARGET_PORT="8646"
LOG_NAME="${1:-App.log}"
OUTPUT_FILE="${2:-$SCRIPT_DIR/realtime_app.log}"
URL="ws://${TARGET_IP}:${TARGET_PORT}/debug/logs/ws?file=${LOG_NAME}"

echo "=========================================================="
echo " Đang lắng nghe realtime log từ: $URL"
echo " Lưu vào file: $OUTPUT_FILE"
echo " Nhấn [Ctrl+C] hoặc TẮT TERMINAL để DỪNG HOÀN TOÀN"
echo "=========================================================="

cleanup() {
    echo ""
    echo "[!] Đã nhận tín hiệu dừng. Kết thúc theo dõi log."
    exit 0
}
trap cleanup SIGINT SIGTERM SIGHUP

while true; do
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] [WS] Đang kết nối tới $URL..."
    
    # Kiểm tra nhanh kết nối port mạng trước (timeout 3 giây)
    if ! nc -zv -w 3 "$TARGET_IP" "$TARGET_PORT" >/dev/null 2>&1; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] ⚠️ Không thể kết nối tới ${TARGET_IP}:${TARGET_PORT} (Server đang tắt hoặc mất mạng). Đang thử lại sau 3s..."
        sleep 3
        continue
    fi

    echo "[$(date '+%Y-%m-%d %H:%M:%S')] ✅ Đã kết nối TCP thành công! Bắt đầu stream log..."
    # Chạy wscat ở tiền cảnh, vừa in ra terminal vừa ghi tiếp vào file log
    tail -f /dev/null | wscat --no-color -c "$URL" | tee -a "$OUTPUT_FILE"

    echo "[$(date '+%Y-%m-%d %H:%M:%S')] [WS] Mất kết nối! Thử kết nối lại sau 3 giây (Ctrl+C để thoát)..."
    sleep 3
done
