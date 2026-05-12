#!/bin/bash

# Đường dẫn gốc
BASE_DIR="$HOME/nhat/con-chain-v2/tool-test/test-simple"

# Hàm thực thi 2 test
run_tests() {
    echo "=================================================="
    echo "🚀 CHẠY TEST RPC..."
    echo "=================================================="
    cd "$BASE_DIR/test-rpc" || { echo "❌ Không tìm thấy thư mục test-rpc"; exit 1; }
    go run main.go -config=config-local.json -data=data.json
    
    echo ""
    echo "=================================================="
    echo "🚀 CHẠY TEST TCP..."
    echo "=================================================="
    cd "$BASE_DIR/test-tcp/caller-tcp" || { echo "❌ Không tìm thấy thư mục test-tcp/caller-tcp"; exit 1; }
    go run main-no-none.go -config=config-local.json -data=data.json
    echo ""
}

# Kiểm tra arg --loop
LOOP_MODE=false
if [ "$1" == "--loop" ]; then
    LOOP_MODE=true
fi

if [ "$LOOP_MODE" = true ]; then
    echo "🔄 CHẾ ĐỘ LẶP VÔ HẠN ĐƯỢC BẬT (Nhấn Ctrl+C để dừng)"
    count=1
    while true; do
        echo "▶️  BẮT ĐẦU VÒNG LẶP THỨ $count"
        run_tests
        count=$((count + 1))
        echo "⏳ Đợi 2s trước khi bắt đầu vòng tiếp theo..."
        sleep 2
    done
else
    echo "▶️  CHẾ ĐỘ CHẠY 1 LẦN"
    run_tests
    echo "✅ ĐÃ CHẠY XONG. Nếu muốn lặp vô hạn, hãy thêm cờ: ./rpc-tcp-simple.sh --loop"
fi
