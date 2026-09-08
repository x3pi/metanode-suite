#!/bin/bash

# Đường dẫn gốc (Tự động nhận diện theo thư mục hiện tại)
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
BASE_DIR="$SCRIPT_DIR/../test-simple"
DEFAULT_CONFIG_FILE="$SCRIPT_DIR/../configs/config.json"

# Hàm thực thi 2 test (RPC & TCP)
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

# Hàm phân giải RPC_URL và TCP_URL của node từ file json
resolve_node_endpoints() {
    local target_node="$1"
    local cfg="$2"

    if [ ! -f "$cfg" ]; then
        return 1
    fi

    python3 -c '
import json, sys

cfg_file = sys.argv[1]
node = sys.argv[2]
key = f"m{node}" if node.isdigit() else node
num_str = node.replace("m", "")

try:
    with open(cfg_file, "r") as f:
        data = json.load(f)
except Exception:
    sys.exit(1)

rpc = ""
tcp = ""

# 1. Tra cứu trong rpc_nodes / tcp_nodes
if "rpc_nodes" in data and isinstance(data["rpc_nodes"], dict):
    rpc = data["rpc_nodes"].get(key, "") or data["rpc_nodes"].get(node, "")

if "tcp_nodes" in data and isinstance(data["tcp_nodes"], dict):
    tcp = data["tcp_nodes"].get(key, "") or data["tcp_nodes"].get(node, "")

# 2. Tra cứu trong nodes (nếu là format của /tmp/rpc_nodes.json)
if not rpc and "nodes" in data and isinstance(data["nodes"], dict):
    rpc = data["nodes"].get(key, "") or data["nodes"].get(node, "")

# 3. Tra cứu fallback rpc_<num> / connection_node_<num>
if not rpc and f"rpc_{num_str}" in data:
    rpc = data[f"rpc_{num_str}"]

if not tcp and f"connection_node_{num_str}" in data:
    tcp = data[f"connection_node_{num_str}"]

# 4. Fallback đặc biệt cho Node 0
if num_str == "0":
    if not rpc and "rpc_url" in data:
        rpc = data["rpc_url"]
    if not tcp and "parent_connection_address" in data:
        tcp = data["parent_connection_address"]

# Chuẩn hóa RPC URL (thêm http:// nếu chưa có schema)
if rpc and not rpc.startswith("http://") and not rpc.startswith("https://"):
    rpc = f"http://{rpc}"

if rpc or tcp:
    print(f"{rpc}|{tcp}")
    sys.exit(0)
else:
    sys.exit(2)
' "$cfg" "$target_node" 2>/dev/null
}

# Hàm lấy danh sách node có trong file cấu hình
get_configured_nodes() {
    local cfg="$1"
    if [ ! -f "$cfg" ]; then
        return 1
    fi

    python3 -c '
import json, sys

try:
    with open(sys.argv[1], "r") as f:
        data = json.load(f)
except Exception:
    sys.exit(1)

nodes = set()
if "rpc_nodes" in data and isinstance(data["rpc_nodes"], dict):
    nodes.update(data["rpc_nodes"].keys())
if "tcp_nodes" in data and isinstance(data["tcp_nodes"], dict):
    nodes.update(data["tcp_nodes"].keys())
if "nodes" in data and isinstance(data["nodes"], dict):
    nodes.update(data["nodes"].keys())

for k in data.keys():
    if k.startswith("rpc_") and k[4:].isdigit():
        nodes.add(f"m{k[4:]}")

sorted_nodes = sorted(list(nodes), key=lambda x: int(x.replace("m", "")) if x.replace("m", "").isdigit() else x)
print(" ".join(sorted_nodes))
' "$cfg" 2>/dev/null
}

# Parse arguments
LOOP_MODE=false
MULTI_MODE=false
NODE_ID=""
RPC_URL_OVERRIDE=""
TCP_URL_OVERRIDE=""
CONFIG_FILE="$DEFAULT_CONFIG_FILE"

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --loop) LOOP_MODE=true; shift ;;
        --multi) MULTI_MODE=true; shift ;;
        --node) NODE_ID="$2"; shift 2 ;;
        --rpc-url) RPC_URL_OVERRIDE="$2"; shift 2 ;;
        --tcp-url) TCP_URL_OVERRIDE="$2"; shift 2 ;;
        --config) CONFIG_FILE="$2"; shift 2 ;;
        *) echo "Unknown parameter passed: $1"; exit 1 ;;
    esac
done

run_single() {
    # Default cho single mode
    if [ -z "$NODE_ID" ]; then
        NODE_ID="0"
    fi

    # Thử resolve từ CONFIG_FILE hoặc fallback /tmp/rpc_nodes.json
    local resolved=""
    local used_cfg=""

    if [ -f "$CONFIG_FILE" ]; then
        resolved=$(resolve_node_endpoints "$NODE_ID" "$CONFIG_FILE")
        if [ $? -eq 0 ] && [ -n "$resolved" ]; then
            used_cfg="$CONFIG_FILE"
        fi
    fi

    if [ -z "$resolved" ] && [ -f "/tmp/rpc_nodes.json" ]; then
        resolved=$(resolve_node_endpoints "$NODE_ID" "/tmp/rpc_nodes.json")
        if [ $? -eq 0 ] && [ -n "$resolved" ]; then
            used_cfg="/tmp/rpc_nodes.json"
        fi
    fi

    if [ -n "$resolved" ]; then
        export RPC_URL=$(echo "$resolved" | cut -d'|' -f1)
        export TCP_URL=$(echo "$resolved" | cut -d'|' -f2)
        echo "📖 Đã nạp cấu hình Node $NODE_ID từ file: $used_cfg"
    else
        echo "⚠️ Không tìm thấy cấu hình Node $NODE_ID trong file config. Sử dụng fallback mặc định (Localhost)."
        case $NODE_ID in
            0) export RPC_URL="http://127.0.0.1:8545"; export TCP_URL="127.0.0.1:4200" ;;
            1) export RPC_URL="http://127.0.0.1:8547"; export TCP_URL="127.0.0.1:6201" ;;
            2) export RPC_URL="http://127.0.0.1:8548"; export TCP_URL="127.0.0.1:6211" ;;
            3) export RPC_URL="http://127.0.0.1:8549"; export TCP_URL="127.0.0.1:6221" ;;
            4) export RPC_URL="http://127.0.0.1:8550"; export TCP_URL="127.0.0.1:6241" ;;
            *)
                echo "❌ Node ID không hợp lệ: $NODE_ID."
                exit 1
                ;;
        esac
    fi

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
    local target_cfg="$CONFIG_FILE"
    if [ ! -f "$target_cfg" ] && [ -f "/tmp/rpc_nodes.json" ]; then
        target_cfg="/tmp/rpc_nodes.json"
    fi

    if [ ! -f "$target_cfg" ]; then
        echo "❌ Không tìm thấy file cấu hình: $CONFIG_FILE hoặc /tmp/rpc_nodes.json"
        exit 1
    fi
    
    echo "📌 Cấu hình chạy test (Multi Mode từ $target_cfg):"
    
    local node_keys=""
    if [ -n "$NODE_ID" ]; then
        if [[ "$NODE_ID" =~ ^[0-9]+$ ]]; then
            node_keys="m${NODE_ID}"
        else
            node_keys="$NODE_ID"
        fi
    else
        node_keys=$(get_configured_nodes "$target_cfg")
        if [ -z "$node_keys" ]; then
            echo "❌ Không trích xuất được danh sách node từ $target_cfg"
            exit 1
        fi
    fi
    
    for key in $node_keys; do
        local resolved
        resolved=$(resolve_node_endpoints "$key" "$target_cfg")
        if [ $? -ne 0 ] || [ -z "$resolved" ]; then
            echo "⚠️ Bỏ qua $key do không tìm thấy endpoint trong $target_cfg"
            continue
        fi

        export RPC_URL=$(echo "$resolved" | cut -d'|' -f1)
        export TCP_URL=$(echo "$resolved" | cut -d'|' -f2)
        
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
    echo "✅ ĐÃ CHẠY XONG. Các tùy chọn mở rộng: ./rpc-tcp-simple.sh [--config path/config.json] [--loop] [--multi] [--node 0-4] [--rpc-url http://...] [--tcp-url host:port]"
fi
