#!/bin/bash

# ==========================================
# TỰ ĐỘNG CHẠY TRONG TMUX NẾU CHƯA CÓ
# ==========================================
if [ -z "$TMUX" ] && [ "$1" != "--internal-run" ]; then
    SESSION_NAME="soak_test_$(date +%H%M%S)"
    echo "🚀 Tự động tạo tmux session ngầm: $SESSION_NAME"
    tmux new-session -d -s "$SESSION_NAME" "bash \"$0\" --internal-run"
    echo "✅ Script đang chạy ngầm an toàn. Bạn có thể đóng terminal thoải mái."
    echo "💡 Để vào xem trực tiếp tiến trình, gõ lệnh:"
    echo "   tmux attach -t $SESSION_NAME"
    exit 0
fi


# ==========================================
# CẤU HÌNH TELEGRAM & THƯ MỤC
# ==========================================
TELEGRAM_BOT_TOKEN="8230176859:AAGoZ_78xzb1q4rgJJ5SYLxRhZBYBTSz_xo"
TELEGRAM_CHAT_ID="-1003867050625"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" &> /dev/null && pwd)"
TEST_DIR="$SCRIPT_DIR"
LOG_FILE="${TEST_DIR}/soak_test_$(date +%Y%m%d_%H%M%S).log"

# Hàm gửi tin nhắn Telegram
send_telegram() {
    local message="$1"
    curl -s -X POST "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage" \
        -d chat_id="${TELEGRAM_CHAT_ID}" \
        -d text="${message}" \
        -d parse_mode="HTML" > /dev/null
}

echo "=========================================="
echo "Bắt đầu Soak Test."
echo "Log được ghi tại: $LOG_FILE"
echo "=========================================="

send_telegram "🚀 <b>[SOAK TEST START]</b> Bắt đầu bài test Block-STM xả tải 10k TPS liên tục (cùng 1 contract).
🕒 <b>Bắt đầu lúc:</b> $(date)"

# Di chuyển vào thư mục test
cd "$TEST_DIR" || { echo "Không tìm thấy thư mục test!"; exit 1; }

# ==========================================
# CHẠY SOAK TEST
# Ở đây tôi đặt rounds=10000 (tức là bơm 100 triệu giao dịch)
# ==========================================
go run main.go -config=../config.json -num=10000 -rounds=100000 -wait-method=block -multi -xapian > "$LOG_FILE" 2>&1

EXIT_CODE=$?

# ==========================================
# XỬ LÝ KẾT QUẢ VÀ GỬI THÔNG BÁO
# ==========================================
if [ $EXIT_CODE -ne 0 ]; then
    echo "❌ Soak test thất bại với mã lỗi $EXIT_CODE"
    
    # Lấy 20 dòng log cuối cùng để xem nguyên nhân lỗi
    TAIL_LOGS=$(tail -n 20 "$LOG_FILE")
    
    send_telegram "🚨 <b>[LỖI SOAK TEST]</b> Bài test spam xả tải đã bị dừng đột ngột!
⚠️ <b>Mã lỗi (Exit Code):</b> <code>$EXIT_CODE</code>
🕒 <b>Thời gian dừng:</b> $(date)

📋 <b>20 dòng log cuối cùng:</b>
<pre><code>${TAIL_LOGS}</code></pre>

🔍 <b>Yêu cầu kiểm tra:</b> Hãy lên server đọc toàn bộ file log tại:
<code>$LOG_FILE</code>"

else
    echo "✅ Soak test hoàn thành thành công!"
    send_telegram "✅ <b>[SOAK TEST THÀNH CÔNG]</b> Bài test đã chạy hết 10000 vòng xả tải mà không gặp lỗi ngắt kết nối nào!
🕒 <b>Kết thúc lúc:</b> $(date)
📁 <b>File log:</b> <code>$LOG_FILE</code>"
fi
