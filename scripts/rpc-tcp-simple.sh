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
        echo "❌ TEST RPC THẤT BẠI TẠI NODE: $RPC_URL ! Dừng chương trình."
        exit 1
    fi
    
    echo ""
    echo "=================================================="
    echo "🚀 CHẠY TEST TCP (Target: $TCP_URL)..."
    echo "=================================================="
    cd "$BASE_DIR/test-tcp/caller-tcp" || { echo "❌ Không tìm thấy thư mục test-tcp/caller-tcp"; exit 1; }
    go run main-no-none.go -config=config-local.json -data=data.json -url="$TCP_URL"
    if [ $? -ne 0 ]; then
        echo "❌ TEST TCP THẤT BẠI TẠI NODE: $TCP_URL ! Dừng chương trình."
        exit 1
    fi
    echo ""
}

# Parse arguments
LOOP_MODE=false
MULTI_MODE=false
NODE_ID=""
RPC_URL_OVERRIDE=""
TCP_URL_OVERRIDE=""

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --loop) LOOP_MODE=true; shift ;;
        --multi) MULTI_MODE=true; shift ;;
        --node) NODE_ID="$2"; shift 2 ;;
        --rpc-url) RPC_URL_OVERRIDE="$2"; shift 2 ;;
        --tcp-url) TCP_URL_OVERRIDE="$2"; shift 2 ;;
        *) echo "Unknown parameter passed: $1"; exit 1 ;;
    esac
done

run_single() {
    # Default cho single mode
    if [ -z "$NODE_ID" ]; then
        NODE_ID="0"
    fi

    # Áp dụng cấu hình tương ứng với Node ID
    case $NODE_ID in
        0)
            export RPC_URL="http://127.0.0.1:8545"
            export TCP_URL="127.0.0.1:4200"
            ;;
        1)
            export RPC_URL="http://127.0.0.1:8547"
            export TCP_URL="127.0.0.1:6201"
            ;;
        2)
            export RPC_URL="http://127.0.0.1:8548"
            export TCP_URL="127.0.0.1:6211"
            ;;
        3)
            export RPC_URL="http://127.0.0.1:8549"
            export TCP_URL="127.0.0.1:6221"
            ;;
        4)
            export RPC_URL="http://127.0.0.1:8550"
            export TCP_URL="127.0.0.1:6241"
            ;;
        *)
            echo "❌ Node ID không hợp lệ: $NODE_ID. Chọn từ 0 đến 4."
            exit 1
            ;;
    esac

    # Ưu tiên cấu hình override trực tiếp nếu có
    if [ -n "$RPC_URL_OVERRIDE" ]; then
        export RPC_URL="$RPC_URL_OVERRIDE"
    fi
    if [ -n "$TCP_URL_OVERRIDE" ]; then
        export TCP_URL="$TCP_URL_OVERRIDE"
    fi

    echo "📌 Cấu hình chạy test (Single Mode):"
    echo "   - Node Target: Node $NODE_ID"
    echo "   - RPC URL:     $RPC_URL"
    echo "   - TCP Host:    $TCP_URL"
    echo ""

    run_tests
}

run_multi() {
    if [ ! -f "/tmp/rpc_nodes.json" ]; then
        echo "❌ Không tìm thấy file /tmp/rpc_nodes.json. Vui lòng chạy deploy để tạo file này."
        exit 1
    fi
    
    echo "📌 Cấu hình chạy test (Multi Mode từ /tmp/rpc_nodes.json):"
    
    if [ -n "$NODE_ID" ]; then
        if [[ "$NODE_ID" =~ ^[0-9]+$ ]]; then
            TARGET_KEY="m${NODE_ID}"
        else
            TARGET_KEY="$NODE_ID"
        fi
        
        CHECK_EXISTS=$(jq -r ".rpc_proxies[\"$TARGET_KEY\"]" /tmp/rpc_nodes.json)
        if [ "$CHECK_EXISTS" = "null" ] || [ -z "$CHECK_EXISTS" ]; then
            echo "❌ Node $TARGET_KEY không tồn tại trong /tmp/rpc_nodes.json"
            exit 1
        fi
        NODE_KEYS="$TARGET_KEY"
    else
        NODE_KEYS=$(jq -r '.rpc_proxies | keys[]' /tmp/rpc_nodes.json | sort)
    fi
    
    for key in $NODE_KEYS; do
        export RPC_URL=$(jq -r ".rpc_proxies[\"$key\"]" /tmp/rpc_nodes.json)
        export TCP_URL=$(jq -r ".tcp_nodes[\"$key\"]" /tmp/rpc_nodes.json)
        
        # Ưu tiên cấu hình override trực tiếp nếu có
        if [ -n "$RPC_URL_OVERRIDE" ]; then
            export RPC_URL="$RPC_URL_OVERRIDE"
        fi
        if [ -n "$TCP_URL_OVERRIDE" ]; then
            export TCP_URL="$TCP_URL_OVERRIDE"
        fi
        
        echo "=================================================="
        echo "📌 Đang chuẩn bị test Node: $key"
        echo "   - RPC URL: $RPC_URL"
        echo "   - TCP Host: $TCP_URL"
        
        run_tests
    done
}

if [ "$LOOP_MODE" = true ]; then
    echo "🔄 CHẾ ĐỘ LẶP VÒNG LẶP ĐƯỢC BẬT (Nhấn Ctrl+C để dừng)"
    count=1
    while true; do
        echo "▶️  BẮT ĐẦU VÒNG LẶP THỨ $count"
        if [ "$MULTI_MODE" = true ]; then
            run_multi
        else
            run_single
        fi
        count=$((count + 1))
        echo "⏳ Đợi 2s trước khi bắt đầu vòng tiếp theo..."
        sleep 2
    done
else
    echo "▶️  CHẾ ĐỘ CHẠY 1 LẦN"
    if [ "$MULTI_MODE" = true ]; then
        run_multi
    else
        run_single
    fi
    echo "✅ ĐÃ CHẠY XONG. Các tùy chọn mở rộng: ./rpc-tcp-simple.sh [--loop] [--multi] [--node 0-4] [--rpc-url http://...] [--tcp-url host:port]"
fi
