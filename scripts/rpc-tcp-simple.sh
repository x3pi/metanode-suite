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
target = sys.argv[2].strip()

try:
    with open(cfg_file, "r") as f:
        data = json.load(f)
except Exception:
    sys.exit(1)

num_str = target.replace("m", "") if target.startswith("m") and target[1:].isdigit() else ("" if not target.isdigit() else target)
candidates = [target]
if num_str:
    candidates.extend([f"m{num_str}", num_str, f"node_{num_str}", f"node-{num_str}"])

rpc = ""
tcp = ""

def search_dict(d, keys):
    if not isinstance(d, dict):
        return ""
    for k in keys:
        if k in d and d[k]:
            return str(d[k])
    return ""

rpc_map = data.get("rpc_nodes") or {}
tcp_map = data.get("tcp_nodes") or {}
nodes_map = data.get("nodes") or {}

rpc = search_dict(rpc_map, candidates) or search_dict(nodes_map, candidates)
tcp = search_dict(tcp_map, candidates)

# Tra cứu mờ (fuzzy match) cho cluster names (vd: target="1" khớp "exec1_replica1" hoặc "cluster_1")
if not rpc or not tcp:
    for k in (rpc_map.keys() if isinstance(rpc_map, dict) else []):
        if target == k or target in k or (num_str and (f"replica{num_str}" in k or f"r{num_str}" in k)):
            if not rpc:
                rpc = str(rpc_map[k])
            if not tcp and isinstance(tcp_map, dict) and k in tcp_map:
                tcp = str(tcp_map[k])
            break

# Fallback rpc_<num> / connection_node_<num>
if num_str:
    if not rpc and f"rpc_{num_str}" in data:
        rpc = str(data[f"rpc_{num_str}"])
    if not tcp and f"connection_node_{num_str}" in data:
        tcp = str(data[f"connection_node_{num_str}"])

# Fallback đặc biệt cho Node 0
if num_str == "0":
    if not rpc and "rpc_url" in data:
        rpc = str(data["rpc_url"])
    if not tcp and "parent_connection_address" in data:
        tcp = str(data["parent_connection_address"])

# Tra cứu trong private_chains nếu có cấu trúc multi-chain
if (not rpc or not tcp) and "private_chains" in data and isinstance(data["private_chains"], dict):
    for ch_name, ch_data in data["private_chains"].items():
        if isinstance(ch_data, dict):
            p_rpc = ch_data.get("rpc_nodes") or {}
            p_tcp = ch_data.get("tcp_nodes") or {}
            if not rpc:
                rpc = search_dict(p_rpc, candidates)
            if not tcp:
                tcp = search_dict(p_tcp, candidates)
            if not rpc and "rpc_url" in ch_data and (num_str == "0" or target == ch_name):
                rpc = str(ch_data["rpc_url"])
            if not tcp and "tcp_node" in ch_data and (num_str == "0" or target == ch_name):
                tcp = str(ch_data["tcp_node"])
            if rpc and tcp:
                break

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

candidates = []

def add_keys(d):
    if isinstance(d, dict):
        for k in d.keys():
            if k not in candidates:
                candidates.append(k)

add_keys(data.get("rpc_nodes"))
add_keys(data.get("tcp_nodes"))
add_keys(data.get("nodes"))

for k in data.keys():
    if k.startswith("rpc_") and k[4:].isdigit():
        node_name = f"m{k[4:]}"
        if node_name not in candidates:
            candidates.append(node_name)

if "private_chains" in data and isinstance(data["private_chains"], dict):
    for ch_name, ch_data in data["private_chains"].items():
        if isinstance(ch_data, dict):
            add_keys(ch_data.get("rpc_nodes"))

def sort_key(x):
    clean = x.replace("m", "")
    if clean.isdigit():
        return (0, int(clean))
    return (1, x)

candidates.sort(key=sort_key)
print(" ".join(candidates))
' "$cfg" 2>/dev/null
}

# Parse arguments
LOOP_MODE=false
MULTI_MODE=false
NODE_ID=""
RPC_URL_OVERRIDE=""
TCP_URL_OVERRIDE=""
CONFIG_FILE="${RPC_NODES_FILE:-${CONFIG_FILE:-}}"
EXPLICIT_CONFIG=false

if [ -n "$CONFIG_FILE" ]; then
    EXPLICIT_CONFIG=true
fi

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --loop) LOOP_MODE=true; shift ;;
        --multi) MULTI_MODE=true; shift ;;
        --node) NODE_ID="$2"; shift 2 ;;
        --node=*) NODE_ID="${1#*=}"; shift ;;
        --rpc-url) RPC_URL_OVERRIDE="$2"; shift 2 ;;
        --rpc-url=*) RPC_URL_OVERRIDE="${1#*=}"; shift ;;
        --tcp-url) TCP_URL_OVERRIDE="$2"; shift 2 ;;
        --tcp-url=*) TCP_URL_OVERRIDE="${1#*=}"; shift ;;
        --config|-c|--json|-f|--file|--rpc-nodes-file) CONFIG_FILE="$2"; EXPLICIT_CONFIG=true; shift 2 ;;
        --config=*|--json=*|--file=*|--rpc-nodes-file=*) CONFIG_FILE="${1#*=}"; EXPLICIT_CONFIG=true; shift ;;
        -h|--help)
            echo "Usage: ./rpc-tcp-simple.sh [JSON_FILE] [OPTIONS]"
            echo ""
            echo "⚡ Chỉ định file cấu hình JSON linh hoạt:"
            echo "  [JSON_FILE]                 Đường dẫn file JSON (vd: /tmp/rpc_nodes.chain_2.json)"
            echo "  --config, -c, --json FILE   Chỉ định file JSON chứa danh sách RPC/TCP nodes"
            echo "  --rpc-nodes-file FILE       Tương đương --config (chuẩn của ansible_deploy/deploy_clusters)"
            echo "  Biến môi trường:            RPC_NODES_FILE=/tmp/... ./rpc-tcp-simple.sh"
            echo ""
            echo "🎯 Tùy chọn thực thi:"
            echo "  --node ID                   Chỉ test 1 node cụ thể (vd: 0, 5, exec1_replica1)."
            echo "                              Nếu không truyền, tự động chọn node đầu tiên có trong file JSON."
            echo "  --multi                     Chạy test lần lượt qua TẤT CẢ các node có trong file JSON."
            echo "  --loop                      Chạy lặp vô hạn (stress test/polling)."
            echo "  --rpc-url URL               Ghi đè thủ công RPC URL (vd: http://127.0.0.1:8545)."
            echo "  --tcp-url HOST:PORT         Ghi đè thủ công TCP Host:Port (vd: 127.0.0.1:4200)."
            exit 0
            ;;
        *.json)
            CONFIG_FILE="$1"
            EXPLICIT_CONFIG=true
            shift
            ;;
        *)
            if [ -f "$1" ]; then
                CONFIG_FILE="$1"
                EXPLICIT_CONFIG=true
                shift
            else
                echo "❌ Tham số không hợp lệ: $1 (chạy với --help để xem hướng dẫn)"
                exit 1
            fi
            ;;
    esac
done

# Hàm xác định file cấu hình thực tế
get_target_config() {
    if [ "$EXPLICIT_CONFIG" = true ]; then
        if [ ! -f "$CONFIG_FILE" ]; then
            echo "❌ Không tìm thấy file cấu hình được chỉ định: $CONFIG_FILE" >&2
            return 1
        fi
        echo "$CONFIG_FILE"
        return 0
    fi

    # Auto fallback
    if [ -n "$CONFIG_FILE" ] && [ -f "$CONFIG_FILE" ]; then
        echo "$CONFIG_FILE"
    elif [ -f "$DEFAULT_CONFIG_FILE" ]; then
        echo "$DEFAULT_CONFIG_FILE"
    elif [ -f "/tmp/rpc_nodes.json" ]; then
        echo "/tmp/rpc_nodes.json"
    else
        echo ""
    fi
    return 0
}

run_single() {
    local target_cfg
    target_cfg=$(get_target_config)
    if [ $? -ne 0 ]; then
        exit 1
    fi

    # Tự động chọn Node ID nếu người dùng không truyền --node
    if [ -z "$NODE_ID" ]; then
        if [ -n "$target_cfg" ] && [ -f "$target_cfg" ]; then
            local available_nodes
            available_nodes=$(get_configured_nodes "$target_cfg")
            if [ -n "$available_nodes" ]; then
                if echo "$available_nodes" | grep -qw -E "(0|m0)"; then
                    NODE_ID="0"
                else
                    local first_node
                    first_node=$(echo "$available_nodes" | awk '{print $1}')
                    if [[ "$first_node" =~ ^m[0-9]+$ ]]; then
                        NODE_ID="${first_node#m}"
                    else
                        NODE_ID="$first_node"
                    fi
                    echo "ℹ️ Không truyền --node, tự động chọn Node đầu tiên có sẵn trong ${target_cfg}: Node $NODE_ID"
                fi
            fi
        fi
        [ -z "$NODE_ID" ] && NODE_ID="0"
    fi

    local resolved=""
    local used_cfg=""

    if [ -n "$target_cfg" ] && [ -f "$target_cfg" ]; then
        resolved=$(resolve_node_endpoints "$NODE_ID" "$target_cfg")
        if [ $? -eq 0 ] && [ -n "$resolved" ]; then
            used_cfg="$target_cfg"
        fi
    fi

    # Chỉ fallback sang /tmp/rpc_nodes.json nếu người dùng KHÔNG chỉ định file riêng
    if [ -z "$resolved" ] && [ "$EXPLICIT_CONFIG" != true ] && [ -f "/tmp/rpc_nodes.json" ] && [ "$target_cfg" != "/tmp/rpc_nodes.json" ]; then
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
        if [ "$EXPLICIT_CONFIG" = true ]; then
            echo "❌ Không tìm thấy thông tin Node $NODE_ID trong file cấu hình: $CONFIG_FILE"
            exit 1
        fi
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
    local target_cfg
    target_cfg=$(get_target_config)
    if [ $? -ne 0 ] || [ -z "$target_cfg" ] || [ ! -f "$target_cfg" ]; then
        echo "❌ Không tìm thấy file cấu hình hợp lệ để chạy chế độ multi"
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
    echo "✅ ĐÃ CHẠY XONG. Các tùy chọn mở rộng: ./rpc-tcp-simple.sh [path/file.json] [--config path/config.json] [--loop] [--multi] [--node ID]"
fi
