#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUITE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TOOL_TEST_DIR="$SUITE_DIR"
RPC_NODES_FILE="/tmp/rpc_nodes.json"

# Defaults
START_STEP=1

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --start-step) START_STEP="$2"; shift ;;
        *) echo "Unknown parameter passed: $1"; exit 1 ;;
    esac
    shift
done

should_run() {
    local step=$1
    if [ "$step" -ge "$START_STEP" ]; then
        return 0
    else
        return 1
    fi
}

run_and_capture() {
    local name="$1"
    shift
    echo "▶️ Đang chạy: $name..."
    "$@"
}

# ----------------------------------------------------
if should_run 1; then
    echo ""
    echo "📌 BƯỚC 1: Run rpc-tcp-simple.sh --multi"
    cd "$SCRIPT_DIR"
    run_and_capture "RPC TCP Simple" ./rpc-tcp-simple.sh --multi
fi

# ----------------------------------------------------
if should_run 2; then
    echo ""
    echo "📌 BƯỚC 2: Test History"
    cd "$TOOL_TEST_DIR/test-simple/test-rpc/test-history"
    run_and_capture "Test History" go run main.go -config config-mutil.json -stop-on-error
fi

# ----------------------------------------------------
if should_run 5; then
    echo ""
    echo "📌 BƯỚC 5: Test Send Native Coin..."
    
    if [ ! -f "$RPC_NODES_FILE" ]; then
        echo "❌ Lỗi: Không tìm thấy $RPC_NODES_FILE" >&2
        exit 1
    fi

    # Lấy RPC URL từ node m0
    # Giả sử cấu trúc có chứa rpc_proxies.m0 (có http:// sẵn)
    # Cần đảm bảo jq đã cài đặt
    RPC_URL=$(jq -r '.rpc_proxies.m0' "$RPC_NODES_FILE")
    
    if [ -z "$RPC_URL" ] || [ "$RPC_URL" == "null" ]; then
        echo "❌ Lỗi: Không thể lấy rpc_url từ $RPC_NODES_FILE" >&2
        exit 1
    fi

    # send-native/main.go load config từ ../config-local.json
    CONFIG_FILE="$TOOL_TEST_DIR/test-simple/test-rpc/config-local.json"
    
    echo "⚙️  Tạo $CONFIG_FILE với RPC URL: $RPC_URL"
    cat <<EOF > "$CONFIG_FILE"
{
  "rpc_url": "$RPC_URL",
  "private_key": "28f0ad246c39b9b5a32692e4f9364e29c3df3cdd6ca6d88fcb40e9dc6bc6c511",
  "chain_id": 991
}
EOF

    cd "$TOOL_TEST_DIR/test-simple/test-rpc/send-native"
    run_and_capture "Send Native (Bước 5)" go run main.go
fi

echo ""
echo "✅ HOÀN TẤT TOÀN BỘ SCRIPT TEST CƠ BẢN!"
