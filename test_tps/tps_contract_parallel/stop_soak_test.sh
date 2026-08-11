#!/bin/bash

MY_IP=$(hostname -I | awk '{print $1}')
SESSION_PREFIX="soak_test_parallel_"

echo "=========================================="
echo "🛑 [Máy $MY_IP] Đang dừng Soak Test Parallel..."

# 1. Tắt session tmux
for session in $(tmux ls 2>/dev/null | grep "$SESSION_PREFIX" | awk -F: '{print $1}'); do
    tmux kill-session -t "$session"
    echo "✅ [Máy $MY_IP] Đã đóng tmux session: $session"
done

# 2. Kill tiến trình
touch .intentional_stop
if pkill -f "tps_contract_parallel.*main.go"; then
    echo "✅ [Máy $MY_IP] Đã kill tiến trình go run đang chạy test!"
else
    echo "ℹ️  [Máy $MY_IP] Không có tiến trình test nào đang chạy (Coi như đã dừng)."
fi

echo "=========================================="
echo "✅ [Máy $MY_IP] Đã dừng toàn bộ quá trình Soak Test!"
