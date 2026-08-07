#!/bin/bash

# Tên session tmux đã đặt
SESSION_NAME="soaktest"

echo "=========================================="
echo "🛑 Đang dừng Soak Test..."

# 1. Tắt session tmux
if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
    tmux kill-session -t "$SESSION_NAME"
    echo "✅ Đã đóng tmux session: $SESSION_NAME"
else
    echo "⚠️ Không tìm thấy tmux session nào tên '$SESSION_NAME'"
fi

# 2. Đảm bảo kill sạch sẽ tiến trình go run main.go xả tải đang chạy (để tránh rác bộ nhớ)
if pkill -f "main.go -config=../config.json -num=10000 -rounds=10000"; then
    echo "✅ Đã kill tiến trình go run đang xả tải!"
else
    echo "⚠️ Không có tiến trình xả tải nào đang chạy."
fi

echo "=========================================="
echo "Đã dừng toàn bộ quá trình Soak Test!"
