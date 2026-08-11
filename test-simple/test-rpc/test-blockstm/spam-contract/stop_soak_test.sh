#!/bin/bash

# Lấy IP của máy hiện tại để hiển thị khi có nhiều máy chạy song song
MY_IP=$(hostname -I | awk '{print $1}')

# Tên session tmux đã đặt
SESSION_NAME="soaktest"

echo "=========================================="
echo "🛑 [Máy $MY_IP] Đang dừng Soak Test..."

# 1. Tắt session tmux
if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
    tmux kill-session -t "$SESSION_NAME"
    echo "✅ [Máy $MY_IP] Đã đóng tmux session: $SESSION_NAME"
else
    echo "ℹ️  [Máy $MY_IP] Không tìm thấy tmux session nào tên '$SESSION_NAME' (Coil như đã dừng)"
fi

# 2. Đảm bảo kill sạch sẽ tiến trình go run main.go xả tải đang chạy (để tránh rác bộ nhớ)
touch .intentional_stop
if pkill -f "main.go -config=../config.json -num=10000 -rounds="; then
    echo "✅ [Máy $MY_IP] Đã kill tiến trình go run đang xả tải!"
else
    echo "ℹ️  [Máy $MY_IP] Không có tiến trình xả tải nào đang chạy (Coi như đã dừng)."
fi

echo "=========================================="
echo "✅ [Máy $MY_IP] Đã dừng toàn bộ quá trình Soak Test!"
#!/bin/bash

# Lấy IP của máy hiện tại để hiển thị khi có nhiều máy chạy song song
MY_IP=$(hostname -I | awk '{print $1}')

# Tên session tmux đã đặt
SESSION_NAME="soaktest"

echo "=========================================="
echo "🛑 [Máy $MY_IP] Đang dừng Soak Test..."

# 1. Tắt session tmux
if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
    tmux kill-session -t "$SESSION_NAME"
    echo "✅ [Máy $MY_IP] Đã đóng tmux session: $SESSION_NAME"
else
    echo "ℹ️  [Máy $MY_IP] Không tìm thấy tmux session nào tên '$SESSION_NAME' (Coil như đã dừng)"
fi

# 2. Đảm bảo kill sạch sẽ tiến trình go run main.go xả tải đang chạy (để tránh rác bộ nhớ)
touch .intentional_stop
if pkill -f "main.go -config=../config.json -num=10000 -rounds="; then
    echo "✅ [Máy $MY_IP] Đã kill tiến trình go run đang xả tải!"
else
    echo "ℹ️  [Máy $MY_IP] Không có tiến trình xả tải nào đang chạy (Coi như đã dừng)."
fi

echo "=========================================="
echo "✅ [Máy $MY_IP] Đã dừng toàn bộ quá trình Soak Test!"
