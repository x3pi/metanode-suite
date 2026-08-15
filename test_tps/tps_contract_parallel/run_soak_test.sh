#!/bin/bash

# ==========================================
# TỰ ĐỘNG CHẠY TRONG TMUX NẾU CHƯA CÓ
# ==========================================
if [ -z "$TMUX" ] && [ "$1" != "--internal-run" ]; then
    SESSION_NAME="soak_test_parallel_$(date +%H%M%S)"
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
export TELEGRAM_BOT_TOKEN="8230176859:AAGoZ_78xzb1q4rgJJ5SYLxRhZBYBTSz_xo"
export TELEGRAM_CHAT_ID="-1003867050625"
export MTN_TELE_ALERT="true"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" &> /dev/null && pwd)"
TEST_DIR="$SCRIPT_DIR"
LOG_DIR="${TEST_DIR}/logs/$(date +%Y-%m-%d)"
mkdir -p "$LOG_DIR"
LOG_FILE="${LOG_DIR}/soak_test_parallel_$(date +%H%M%S).log"

# Hàm gửi tin nhắn Telegram
send_telegram() {
    local message="$1"
    curl -s -X POST "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage" \
        -d chat_id="${TELEGRAM_CHAT_ID}" \
        -d text="${message}" \
        -d parse_mode="HTML" > /dev/null
}

MY_IP=$(hostname -I | awk '{print $1}')

echo "=========================================="
echo "Bắt đầu Soak Test Parallel Contract."
echo "Log được ghi tại: $LOG_FILE"
echo "=========================================="

TEST_TITLE="PARALLEL CONTRACT"
TEST_DESC="Bắt đầu bài test thực thi Contract song song ngầm qua đêm."
# Để Test sức chịu đựng của thuật toán khi Conflict cực gắt (Dễ ra bug nhất):
GO_FLAGS="-config=config-multi.json -count=10000 -rounds=1000000 -num-contracts=2000 -load_balance -check-state"


send_telegram "🚀 <b>[SOAK TEST $TEST_TITLE START]</b> $TEST_DESC
🖥 <b>Máy chủ:</b> <code>$MY_IP</code>
🕒 <b>Bắt đầu lúc:</b> $(date)"

# Di chuyển vào thư mục test
cd "$TEST_DIR" || { echo "Không tìm thấy thư mục test!"; exit 1; }

# ==========================================
# CHẠY SOAK TEST
# ==========================================
rm -f .intentional_stop
go run main.go $GO_FLAGS > "$LOG_FILE" 2>&1

EXIT_CODE=$?

# ==========================================
# XỬ LÝ KẾT QUẢ VÀ GỬI THÔNG BÁO
# ==========================================
if [ $EXIT_CODE -ne 0 ]; then
    if [ -f .intentional_stop ]; then
        echo "🛑 Soak test đã được người dùng dừng chủ động."
        send_telegram "🛑 <b>[SOAK TEST $TEST_TITLE ĐÃ DỪNG]</b> Bài test đã được dừng chủ động!
🖥 <b>Máy chủ:</b> <code>$MY_IP</code>
🕒 <b>Thời gian dừng:</b> $(date)
📁 <b>File log:</b> <code>$LOG_FILE</code>"
        rm -f .intentional_stop
    else
        echo "❌ Soak test thất bại với mã lỗi $EXIT_CODE"
        
        # Lấy 20 dòng log cuối cùng để xem nguyên nhân lỗi
        TAIL_LOGS=$(tail -n 20 "$LOG_FILE")
        
        send_telegram "🚨 <b>[LỖI SOAK TEST $TEST_TITLE]</b> Bài test đã bị lỗi hoặc dừng đột ngột!
🖥 <b>Máy chủ:</b> <code>$MY_IP</code>
⚠️ <b>Mã lỗi (Exit Code):</b> <code>$EXIT_CODE</code>
🕒 <b>Thời gian dừng:</b> $(date)

📋 <b>20 dòng log cuối cùng:</b>
<pre><code>${TAIL_LOGS}</code></pre>

🔍 <b>Yêu cầu kiểm tra:</b> Hãy lên server đọc toàn bộ file log tại:
<code>$LOG_FILE</code>"
    fi
else
    echo "✅ Soak test hoàn thành thành công!"
    send_telegram "✅ <b>[SOAK TEST $TEST_TITLE THÀNH CÔNG]</b> Bài test đã chạy hết số vòng mà không gặp lỗi!
🖥 <b>Máy chủ:</b> <code>$MY_IP</code>
🕒 <b>Kết thúc lúc:</b> $(date)
📁 <b>File log:</b> <code>$LOG_FILE</code>"
fi
