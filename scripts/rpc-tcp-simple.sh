#!/bin/bash

# Đường dẫn gốc (Tự động nhận diện theo thư mục hiện tại)
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
BASE_DIR="$SCRIPT_DIR/../test-simple"

# Hàm thực thi 2 test
run_tests() {
    echo "=================================================="
    echo "🚀 CHẠY TEST RPC (Target: $RPC_URL)..."
    echo "=================================================="
    cd "$BASE_DIR/test-rpc" || { echo "❌ Không tìm thấy thư mục test-rpc"; exit 1; }
    go run main.go -config=config-local.json -data=data.json -url="$RPC_URL"
    if [ $? -ne 0 ]; then
        echo "❌ TEST RPC THẤT BẠI! Dừng chương trình."
        exit 1
    fi
    
    echo ""
    echo "=================================================="
    echo "🚀 CHẠY TEST TCP (Target: $TCP_URL)..."
    echo "=================================================="
    cd "$BASE_DIR/test-tcp/caller-tcp" || { echo "❌ Không tìm thấy thư mục test-tcp/caller-tcp"; exit 1; }
    go run main-no-none.go -config=config-local.json -data=data.json -url="$TCP_URL"
    if [ $? -ne 0 ]; then
        echo "❌ TEST TCP THẤT BẠI! Dừng chương trình."
        exit 1
    fi
    echo ""
}

# Parse arguments
LOOP_MODE=false
NODE_ID="0"
RPC_URL_OVERRIDE=""
TCP_URL_OVERRIDE=""

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --loop) LOOP_MODE=true; shift ;;
        --node) NODE_ID="$2"; shift 2 ;;
        --rpc-url) RPC_URL_OVERRIDE="$2"; shift 2 ;;
        --tcp-url) TCP_URL_OVERRIDE="$2"; shift 2 ;;
        *) echo "Unknown parameter passed: $1"; exit 1 ;;
    esac
done

# Áp dụng cấu hình tương ứng với Node ID
case $NODE_ID in
    0)
        RPC_URL="http://127.0.0.1:8545"
        TCP_URL="127.0.0.1:4201"
        ;;
    1)
        RPC_URL="http://127.0.0.1:8547"
        TCP_URL="127.0.0.1:6201"
        ;;
    2)
        RPC_URL="http://127.0.0.1:8548"
        TCP_URL="127.0.0.1:6211"
        ;;
    3)
        RPC_URL="http://127.0.0.1:8549"
        TCP_URL="127.0.0.1:6221"
        ;;
    4)
        RPC_URL="http://127.0.0.1:8550"
        TCP_URL="127.0.0.1:6241"
        ;;
    *)
        echo "❌ Node ID không hợp lệ: $NODE_ID. Chọn từ 0 đến 4."
        exit 1
        ;;
esac

# Ưu tiên cấu hình override trực tiếp nếu có
if [ -n "$RPC_URL_OVERRIDE" ]; then
    RPC_URL="$RPC_URL_OVERRIDE"
fi
if [ -n "$TCP_URL_OVERRIDE" ]; then
    TCP_URL="$TCP_URL_OVERRIDE"
fi

echo "📌 Cấu hình chạy test:"
echo "   - Node Target: Node $NODE_ID"
echo "   - RPC URL:     $RPC_URL"
echo "   - TCP Host:    $TCP_URL"
echo ""

if [ "$LOOP_MODE" = true ]; then
    echo "🔄 CHẾ ĐỘ LẶP VÒNG LẶP ĐƯỢC BẬT (Nhấn Ctrl+C để dừng)"
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
    echo "✅ ĐÃ CHẠY XONG. Các tùy chọn mở rộng: ./rpc-tcp-simple.sh [--loop] [--node 0-4] [--rpc-url http://...] [--tcp-url host:port]"
fi
