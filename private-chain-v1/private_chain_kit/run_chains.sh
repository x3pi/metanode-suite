#!/usr/bin/env bash
# ╔══════════════════════════════════════════════════════════════════════════════╗
# ║  🚀 METANODE PRIVATE CHAIN KIT — ALL-IN-ONE CHAIN RUNNER                     ║
# ║                                                                              ║
# ║  Quản lý tự động các Private Chains (Chain 101, Chain 102, ...):             ║
# ║  - Hỗ trợ chạy toàn bộ Chain hoặc từng Node riêng lẻ (--node=X)              ║
# ║  - Hỗ trợ đa validator (đọc validators từ inventory.yml)                    ║
# ║  - Chạy giữ nguyên dữ liệu (Keep Data / Start)                               ║
# ║  - Xóa dữ liệu DB & logs chạy lại từ block 0 (Clean Data)                    ║
# ║  - Xóa toàn bộ & sinh mới keys/configs (Reset All)                           ║
# ║  - Tự động cập nhật BLS keys vào gateway_register.json                       ║
# ║  - Tách riêng Submitter Key cho từng chain (chống xung đột Nonce)            ║
# ╚══════════════════════════════════════════════════════════════════════════════╝

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Màu sắc hiển thị
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Cấu hình mặc định
ACTION="start"
TARGET_CHAIN="all"
TARGET_NODE="all"
ROOT_ANCHOR_RPC="http://192.168.1.234:10746"
ROOT_ANCHOR_RPC_OVERRIDDEN=""
GATEWAY_JSON="$SCRIPT_DIR/gateway_register.json"
DEV_KEYS_FILE="$SCRIPT_DIR/private_dev_keys.json"
GENESIS_FILE="$SCRIPT_DIR/genesis.json"

# Đường dẫn file inventory cấu hình trực tiếp trong thư mục này
INVENTORY_FILE="$SCRIPT_DIR/inventory.yml"

# Danh sách cấu hình các chuỗi mặc định (Chain ID, RPC Port, Port Offset, Dedicated Submitter Key, Validators)
# Tránh xung đột Nonce với Relayer (Relayer dùng Wallet 11: d3d8157f...)
declare -A CHAIN_RPCS=( ["101"]="8546" ["102"]="8556" ["103"]="8566" ["104"]="8576" )
declare -A CHAIN_OFFSETS=( ["101"]="10" ["102"]="20" ["103"]="30" ["104"]="40" )
declare -A CHAIN_VALIDATORS=( ["101"]="1" ["102"]="1" ["103"]="1" ["104"]="1" )
# Submitter keys bắt buộc cấu hình từ inventory.yml (node_submitter_keys hoặc node_submitter_key)
# Tuyệt đối không hardcode trong script
declare -A CHAIN_SUBMITTER_KEYS=()


ALL_CHAINS=("101" "102")

print_banner() {
    echo -e "${CYAN}══════════════════════════════════════════════════════════════════════════════${NC}"
    echo -e "${BOLD}${MAGENTA}🚀 METANODE PRIVATE CHAIN KIT — CHAIN MANAGER${NC}"
    echo -e "${CYAN}══════════════════════════════════════════════════════════════════════════════${NC}"
}

usage() {
    print_banner
    echo -e "${BOLD}Cách sử dụng:${NC} $0 [OPTIONS]"
    echo ""
    echo -e "${BOLD}⚡ Các Hành Động Chính (Actions):${NC}"
    echo -e "  ${GREEN}--start${NC}             (Mặc định) Chạy node giữ nguyên dữ liệu (nếu chưa có config thì tự sinh)"
    echo -e "  ${YELLOW}--clean-data, --clean${NC} Xóa database & logs (giữ nguyên keys & config), chạy lại từ block 0"
    echo -e "  ${RED}--reset-all, --reset${NC}  Reset lại từ block 0 (tự động giữ keys nếu đã có để không phải đăng ký lại)"
    echo -e "  ${RED}--force-keygen${NC}       Buộc xóa sạch và sinh mới bộ keys từ đầu (kèm --reset-all)"
    echo -e "  ${CYAN}--stop${NC}              Dừng các node an toàn (Graceful SIGTERM -> fallback SIGKILL)"
    echo -e "  ${CYAN}--restart${NC}           Dừng và khởi động lại các node (giữ nguyên dữ liệu)"
    echo -e "  ${CYAN}--status${NC}            Kiểm tra trạng thái hoạt động (PID, port, block number)"
    echo -e "  ${CYAN}--update-gateway${NC}    Chỉ cập nhật gateway_register.json từ config hiện tại (không chạy node)"
    echo -e "  ${CYAN}--register${NC}          Đăng ký danh bạ các chain lên Gateway Contract (Root Anchor)"
    echo -e "  ${CYAN}--relayer${NC}           Khởi chạy Cross-Chain Relayer Daemon"
    echo ""
    echo -e "${BOLD}🎯 Tùy Chọn Cấu Hình (Configuration Options):${NC}"
    echo -e "  ${CYAN}-i, --inventory=FILE${NC} Đường dẫn file cấu hình inventory.yml (mặc định: ./inventory.yml)"
    echo -e "  ${CYAN}--chain=ID, -c=ID${NC}   Chỉ áp dụng lệnh cho 1 Chain cụ thể (ví dụ: --chain=101). Mặc định: all"
    echo -e "  ${CYAN}--node=ID, -n=ID${NC}    Chỉ áp dụng lệnh cho 1 Node cụ thể (ví dụ: --node=1). Mặc định: all"
    echo -e "  ${CYAN}--root-rpc=URL${NC}      Địa chỉ Root Anchor RPC (mặc định: đọc từ inventory.yml)"
    echo ""
    echo -e "${BOLD}💡 Ví Dụ Thực Thi:${NC}"
    echo -e "  ${GREEN}$0${NC}                                    # Khởi động tất cả các chain & validators"
    echo -e "  ${GREEN}$0 --stop${NC}                             # Dừng tất cả các Private Chain & nodes"
    echo -e "  ${GREEN}$0 --chain=101 --stop${NC}                 # Dừng toàn bộ các node của Chain 101"
    echo -e "  ${GREEN}$0 --chain=101 --node=1 --stop${NC}        # Dừng RIÊNG Node 1 của Chain 101"
    echo -e "  ${GREEN}$0 --chain=101 --node=1 --start${NC}       # Khởi động RIÊNG Node 1 của Chain 101"
    echo -e "  ${GREEN}$0 --chain=101 --node=1 --restart${NC}     # Khởi động lại RIÊNG Node 1 của Chain 101"
    echo -e "  ${GREEN}$0 --chain=101 --clean-data${NC}           # Xóa DB và chạy lại từ block 0 cho Chain 101"
    echo -e "  ${GREEN}$0 --reset-all${NC}                        # Xóa trắng toàn bộ, sinh key mới theo inventory"
    echo -e "  ${GREEN}$0 --status${NC}                           # Kiểm tra chi tiết trạng thái từng node của các chain"
    exit 0
}

# Phân tích tham số dòng lệnh
while [[ $# -gt 0 ]]; do
    case "$1" in
        --start)
            ACTION="start"
            shift
            ;;
        --clean-data|--clean)
            ACTION="clean_data"
            shift
            ;;
        --reset-all|--reset)
            ACTION="reset_all"
            shift
            ;;
        --force-keygen|--new-keys)
            FORCE_KEYGEN=true
            shift
            ;;
        --stop)
            ACTION="stop"
            shift
            ;;
        --restart)
            ACTION="restart"
            shift
            ;;
        --status)
            ACTION="status"
            shift
            ;;
        --update-gateway|--update-config)
            ACTION="update_gateway"
            shift
            ;;
        --register)
            ACTION="register"
            shift
            ;;
        --relayer)
            ACTION="relayer"
            shift
            ;;
        -i|--inventory)
            INVENTORY_FILE="$2"
            shift 2
            ;;
        --inventory=*)
            INVENTORY_FILE="${1#*=}"
            shift
            ;;
        --chain=*|-c=*)
            TARGET_CHAIN="${1#*=}"
            shift
            ;;
        -c)
            TARGET_CHAIN="$2"
            shift 2
            ;;
        --node=*|-n=*)
            TARGET_NODE="${1#*=}"
            shift
            ;;
        -n)
            TARGET_NODE="$2"
            shift 2
            ;;
        --root-rpc=*)
            ROOT_ANCHOR_RPC="${1#*=}"
            ROOT_ANCHOR_RPC_OVERRIDDEN="true"
            shift
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo -e "${RED}❌ Tùy chọn không hợp lệ: $1${NC}"
            usage
            ;;
    esac
done

# Nạp động cấu hình từ Inventory (nếu tồn tại)
if [ -n "$INVENTORY_FILE" ] && [ -f "$INVENTORY_FILE" ]; then
    INV_EXPORTS=$(python3 -c "
import yaml, sys
try:
    with open('$INVENTORY_FILE') as f:
        data = yaml.safe_load(f) or {}
    hosts = data.get('all', {}).get('children', {}).get('private_chains', {}).get('hosts', {}) or {}
    all_vars = data.get('all', {}).get('vars', {}) or {}
    chains = []
    for host_name, host_cfg in hosts.items():
        if not isinstance(host_cfg, dict):
            continue
        cid = str(host_cfg.get('chain_id', ''))
        if cid and cid != 'None':
            chains.append(cid)
            rpc = str(host_cfg.get('rpc_port', 8546))
            offset = str(host_cfg.get('port_offset', 10))
            val_c = str(host_cfg.get('validators', 1))

            # Hỗ trợ node_submitter_keys (list) hoặc node_submitter_key (str / comma-separated)
            sub_k_raw = host_cfg.get('node_submitter_keys')
            if sub_k_raw is None:
                sub_k_raw = host_cfg.get('node_submitter_key')

            if isinstance(sub_k_raw, list):
                sub_k = ','.join(str(k).strip() for k in sub_k_raw if k)
            elif sub_k_raw:
                sub_k = str(sub_k_raw).strip()
            else:
                sub_k = ''

            # Fallback về root_anchor_submitter_key trong all.vars nếu host không khai báo riêng
            if not sub_k and all_vars.get('root_anchor_submitter_key'):
                sub_k = str(all_vars.get('root_anchor_submitter_key')).strip()

            print(f'CHAIN_RPCS[\"{cid}\"]=\"{rpc}\"')
            print(f'CHAIN_OFFSETS[\"{cid}\"]=\"{offset}\"')
            print(f'CHAIN_VALIDATORS[\"{cid}\"]=\"{val_c}\"')
            if sub_k:
                print(f'CHAIN_SUBMITTER_KEYS[\"{cid}\"]=\"{sub_k}\"')
    if chains:
        print('ALL_CHAINS=(' + ' '.join(f'\"{c}\"' for c in chains) + ')')
    root_rpc = all_vars.get('root_anchor_rpc')
    if root_rpc:
        print(f'ROOT_ANCHOR_RPC_DEFAULT=\"{root_rpc}\"')
    sub_key = all_vars.get('root_anchor_submitter_key')
    if sub_key:
        print(f'ROOT_ANCHOR_SUBMITTER_KEY=\"{sub_key}\"')
except Exception as e:
    sys.stderr.write(f'Lỗi đọc inventory: {e}\\\\n')
")
    if [ -n "$INV_EXPORTS" ]; then
        eval "$INV_EXPORTS"
        if [ -z "$ROOT_ANCHOR_RPC_OVERRIDDEN" ] && [ -n "$ROOT_ANCHOR_RPC_DEFAULT" ]; then
            ROOT_ANCHOR_RPC="$ROOT_ANCHOR_RPC_DEFAULT"
        fi
    fi
fi

# Xác định danh sách chain mục tiêu
CHAINS_TO_PROCESS=()
if [ "$TARGET_CHAIN" == "all" ]; then
    CHAINS_TO_PROCESS=("${ALL_CHAINS[@]}")
else
    CHAINS_TO_PROCESS=("$TARGET_CHAIN")
fi

# ==============================================================================
# HÀM HỖ TRỢ: LẤY DANH SÁCH CÁC NODE CẦN XỬ LÝ CỦA 1 CHAIN
# ==============================================================================
get_chain_nodes() {
    local cid="$1"
    local cdir="$SCRIPT_DIR/chain-$cid"
    local val_count="${CHAIN_VALIDATORS[$cid]:-1}"
    local nodes=()

    if [ "$TARGET_NODE" != "all" ]; then
        nodes=("$TARGET_NODE")
    else
        # Tự động quét các thư mục node-* đang có
        if [ -d "$cdir" ]; then
            for nd in "$cdir"/node-*; do
                if [ -d "$nd" ]; then
                    local n_base=$(basename "$nd")
                    local nid="${n_base#node-}"
                    if [[ "$nid" =~ ^[0-9]+$ ]]; then
                        nodes+=("$nid")
                    fi
                fi
            done
        fi
        # Nếu thư mục chưa có, dùng theo cấu hình validators (0..val_count-1)
        if [ ${#nodes[@]} -eq 0 ]; then
            for ((i=0; i<val_count; i++)); do
                nodes+=("$i")
            done
        fi
    fi
    echo "${nodes[@]}"
}

# ==============================================================================
# HÀM HỖ TRỢ: DỪNG NODE CỦA 1 CHAIN (TOÀN BỘ HOẶC TỪNG NODE)
# ==============================================================================
stop_chain_node() {
    local cid="$1"
    local cdir="$SCRIPT_DIR/chain-$cid"
    local base_rpc="${CHAIN_RPCS[$cid]:-8546}"
    local nodes=($(get_chain_nodes "$cid"))

    if [ "$TARGET_NODE" == "all" ]; then
        echo -e "🛑 Dừng tất cả các node của Private Chain ${BOLD}${cid}${NC}..."
    else
        echo -e "🛑 Dừng Node ${BOLD}${TARGET_NODE}${NC} của Private Chain ${BOLD}${cid}${NC}..."
    fi

    for nid in "${nodes[@]}"; do
        local ndir="$cdir/node-$nid"
        local pid_file="$ndir/node-$nid.pid"
        local rpc_p=$((base_rpc + nid))

        local pid=$(cat "$pid_file" 2>/dev/null || echo "")
        if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
            pid=$(pgrep -f "chain-$cid/node-$nid/config.json" 2>/dev/null | head -n 1 || echo "")
        fi

        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            echo -e "  → Dừng Node-$nid (PID $pid)..."
            kill -15 "$pid" 2>/dev/null || true
            for i in $(seq 1 10); do
                if ! kill -0 "$pid" 2>/dev/null; then
                    break
                fi
                sleep 0.5
            done
            if kill -0 "$pid" 2>/dev/null; then
                echo -e "  → Node-$nid chưa dừng, buộc dừng bằng SIGKILL..."
                kill -9 "$pid" 2>/dev/null || true
            fi
            rm -f "$pid_file" "$ndir/logs/node-$nid.pid" 2>/dev/null || true
        fi

        # Quét dọn fallback theo process config & port riêng của node
        pkill -9 -f "chain-$cid/node-$nid/config.json" 2>/dev/null || true
        fuser -k "${rpc_p}/tcp" 2>/dev/null || true
        echo -e "  ${GREEN}✓${NC} Node-$nid (Chain $cid, RPC: $rpc_p) đã dừng hoàn toàn."
    done

    # Nếu dừng toàn bộ chain, quét dọn thêm fallback toàn chain
    if [ "$TARGET_NODE" == "all" ]; then
        pkill -9 -f "chain-$cid/node-" 2>/dev/null || true
        echo -e "  ${GREEN}✓${NC} Toàn bộ Chain $cid đã dừng."
    fi
}

# ==============================================================================
# HÀM HỖ TRỢ: XÓA DỮ LIỆU DATABASE (TOÀN BỘ HOẶC TỪNG NODE)
# ==============================================================================
clean_chain_data() {
    local cid="$1"
    local cdir="$SCRIPT_DIR/chain-$cid"
    local nodes=($(get_chain_nodes "$cid"))

    if [ "$TARGET_NODE" == "all" ]; then
        echo -e "🧹 Đang dọn sạch database & logs của toàn bộ Chain ${BOLD}${cid}${NC} (giữ nguyên Keys)..."
    else
        echo -e "🧹 Đang dọn sạch database & logs của Node ${BOLD}${TARGET_NODE}${NC} (Chain $cid)..."
    fi

    stop_chain_node "$cid"

    for nid in "${nodes[@]}"; do
        local ndir="$cdir/node-$nid"
        rm -rf "$ndir/data"/* "$ndir/logs"/*
        mkdir -p "$ndir/data/execution/db" \
                 "$ndir/data/execution/backup" \
                 "$ndir/data/execution/snapshots" \
                 "$ndir/data/consensus" \
                 "$ndir/logs"
        echo -e "  ${GREEN}✓${NC} Đã xóa sạch dữ liệu cũ của Node-$nid (Chain $cid)."
    done
}

# ==============================================================================
# HÀM HỖ TRỢ: SINH MỚI CẤU HÌNH & BỘ KHÓA CHO 1 CHAIN (GEN_SINGLE_CHAIN)
# ==============================================================================
generate_chain_config() {
    local cid="$1"
    local cdir="$SCRIPT_DIR/chain-$cid"
    local rpc_port="${CHAIN_RPCS[$cid]:-8546}"
    local p_offset="${CHAIN_OFFSETS[$cid]:-10}"
    local sub_key="${CHAIN_SUBMITTER_KEYS[$cid]:-""}"
    local val_count="${CHAIN_VALIDATORS[$cid]:-1}"

    # Bắt buộc phải có submitter key, nếu không cấu hình là báo lỗi dừng script
    if [ -z "$sub_key" ]; then
        echo -e "${RED}❌ LỖI: Chain $cid chưa được cấu hình submitter key trong inventory.yml!${NC}"
        if [ "$val_count" -gt 1 ]; then
            echo -e "${YELLOW}   Chain có ${val_count} validator nodes. Vui lòng khai báo 'node_submitter_keys' (danh sách ${val_count} keys riêng biệt) trong host 'chain_${cid}' của inventory.yml.${NC}"
        else
            echo -e "${YELLOW}   Vui lòng khai báo 'node_submitter_key' trong host 'chain_${cid}' của inventory.yml.${NC}"
        fi
        exit 1
    fi

    # Kiểm tra số lượng keys nếu có nhiều validator
    IFS=',' read -ra k_arr <<< "$sub_key"
    if [ "${#k_arr[@]}" -lt "$val_count" ]; then
        echo -e "${RED}❌ LỖI: Chain $cid cấu hình ${val_count} validators nhưng chỉ có ${#k_arr[@]} submitter key!${NC}"
        echo -e "${YELLOW}   Mỗi validator node PHẢI có 1 submitter key riêng để tránh xung đột Nonce trên Root Anchor.${NC}"
        echo -e "${YELLOW}   Vui lòng khai báo đủ ${val_count} keys trong 'node_submitter_keys' của host 'chain_${cid}' trong inventory.yml.${NC}"
        exit 1
    fi

    # Kiểm tra xem bộ khóa & cấu hình đã tồn tại đầy đủ chưa
    local all_keys_exist=true
    if [ ! -d "$cdir" ]; then
        all_keys_exist=false
    else
        for ((i=0; i<val_count; i++)); do
            if [ ! -f "$cdir/node-$i/keys/protocol_key.json" ] || [ ! -f "$cdir/node-$i/config.json" ]; then
                all_keys_exist=false
                break
            fi
        done
    fi

    if [ "$all_keys_exist" = true ] && [ "$FORCE_KEYGEN" != true ]; then
        echo -e "🔑 Phát hiện bộ khóa và cấu hình đã tồn tại cho Chain ${BOLD}${cid}${NC}."
        echo -e "   ${GREEN}➔ GIỮ NGUYÊN KEYS & CONFIG (tránh mất công đăng ký lại), chỉ xóa sạch database & logs...${NC}"
        clean_chain_data "$cid"
        return
    fi

    echo -e "🔨 Đang sinh cấu hình & bộ khóa mới cho Chain ${BOLD}${cid}${NC} (${val_count} validators)..."
    stop_chain_node "$cid"
    rm -rf "$cdir"

    local cmd_args=(
        "gen_single_chain.py"
        "--chain-id" "$cid"
        "--ip" "127.0.0.1"
        "--rpc-port" "$rpc_port"
        "--port-offset" "$p_offset"
        "--validators" "$val_count"
        "--root-anchor-rpc" "$ROOT_ANCHOR_RPC"
        "--output-dir" "./chain-$cid"
    )

    if [ -n "$sub_key" ]; then
        cmd_args+=("--root-anchor-submitter-key" "$sub_key")
    fi

    if [ -f "$GENESIS_FILE" ]; then
        cmd_args+=("--genesis-template" "$GENESIS_FILE")
    fi

    if [ -f "$DEV_KEYS_FILE" ]; then
        cmd_args+=("--dev-keys-file" "$DEV_KEYS_FILE")
    fi

    python3 "${cmd_args[@]}" >/dev/null 2>&1
    echo -e "  ${GREEN}✓${NC} Đã tạo xong cấu hình Chain $cid (${val_count} validators) tại ./chain-$cid"
}

# ==============================================================================
# HÀM HỖ TRỢ: KHỞI ĐỘNG NODE (TOÀN BỘ HOẶC TỪNG NODE)
# ==============================================================================
start_chain_node() {
    local cid="$1"
    local cdir="$SCRIPT_DIR/chain-$cid"
    local base_rpc="${CHAIN_RPCS[$cid]:-8546}"
    local bin_path="$SCRIPT_DIR/bin/simple_chain"

    if [ ! -f "$bin_path" ]; then
        if [ -d "$SCRIPT_DIR/../../execution/cmd/simple_chain" ]; then
            echo -e "🔨 Đang biên dịch binary simple_chain..."
            (cd "$SCRIPT_DIR/../../execution/cmd/simple_chain" && go build -o "$bin_path" .)
        else
            echo -e "${RED}❌ LỖI: Không tìm thấy binary $bin_path!${NC}"
            exit 1
        fi
    fi

    if [ ! -d "$cdir" ] || [ ! -f "$cdir/node-0/config.json" ]; then
        echo -e "⚠️  Chưa có cấu hình cho Chain $cid, tự động sinh mới..."
        generate_chain_config "$cid"
    fi

    local nodes=($(get_chain_nodes "$cid"))

    if [ "$TARGET_NODE" == "all" ]; then
        echo -e "🚀 Khởi động tất cả các node của Chain ${BOLD}${cid}${NC}..."
    else
        echo -e "🚀 Khởi động Node ${BOLD}${TARGET_NODE}${NC} của Chain ${BOLD}${cid}${NC}..."
    fi

    for nid in "${nodes[@]}"; do
        local ndir="$cdir/node-$nid"
        local cfg="$ndir/config.json"
        local pid_file="$ndir/node-$nid.pid"
        local rpc_p=$((base_rpc + nid))

        if [ ! -f "$cfg" ]; then
            echo -e "  ${RED}❌ Không tìm thấy config cho Node-$nid tại $cfg${NC}"
            continue
        fi

        local existing_pid=$(cat "$pid_file" 2>/dev/null || echo "")
        if [ -n "$existing_pid" ] && kill -0 "$existing_pid" 2>/dev/null; then
            echo -e "  ${YELLOW}⚠️  Node-$nid (Chain $cid) đang chạy (PID: $existing_pid, RPC: http://127.0.0.1:$rpc_p)${NC}"
            continue
        fi

        mkdir -p "$ndir/logs"
        (
            cd "$ndir"
            nohup "$bin_path" --config "$cfg" > "logs/node-$nid.log" 2>&1 &
            echo $! > "node-$nid.pid"
        )

        sleep 1
        local new_pid=$(cat "$pid_file" 2>/dev/null || echo "")
        if [ -n "$new_pid" ] && kill -0 "$new_pid" 2>/dev/null; then
            echo -e "  ${GREEN}✅ Node-$nid (Chain $cid) đã khởi chạy thành công! (PID: $new_pid, RPC: http://127.0.0.1:$rpc_p)${NC}"
        else
            echo -e "  ${RED}❌ Node-$nid (Chain $cid) khởi động thất bại. Kiểm tra log: $ndir/logs/node-$nid.log${NC}"
            tail -n 15 "$ndir/logs/node-$nid.log" 2>/dev/null || true
        fi
    done
}

# ==============================================================================
# HÀM HỖ TRỢ: TỰ ĐỘNG CẬP NHẬT GATEWAY_REGISTER.JSON
# ==============================================================================
update_gateway_register_json() {
    echo -e "\n📝 Đang tự động cập nhật ${BOLD}gateway_register.json${NC}..."
    python3 - "$SCRIPT_DIR" "$INVENTORY_FILE" "${ALL_CHAINS[@]}" << 'EOF'
import json, os, sys, yaml

script_dir = sys.argv[1] if len(sys.argv) > 1 else os.getcwd()
inv_file = sys.argv[2] if len(sys.argv) > 2 and sys.argv[2] else ""
chains_to_update = [int(c) for c in sys.argv[3:] if c.isdigit()]

gw_path = os.path.join(script_dir, "gateway_register.json")

# Nạp file cấu hình gateway_register.json hiện tại
gw_data = {}
if os.path.exists(gw_path):
    try:
        with open(gw_path) as f:
            gw_data = json.load(f)
    except Exception:
        gw_data = {}

if not gw_data:
    gw_data = {
        "root_anchor_rpc": "http://192.168.1.234:10746",
        "submitter_key": "d3d8157f2571153bcb664233f998a82b9b475fe509f92caf65ca2461bae7f1a9",
        "genesis_supply": "400000000000000000000000000",
        "per_chain_allocation": "100000000000000000000000000",
        "fund_genesis": True,
        "chains": []
    }

# Đọc cấu hình bổ sung từ inventory (nếu có)
rpc_map = {}
if inv_file and os.path.exists(inv_file):
    try:
        with open(inv_file) as f:
            idata = yaml.safe_load(f) or {}
        all_vars = idata.get("all", {}).get("vars", {}) or {}
        if all_vars.get("root_anchor_rpc"):
            gw_data["root_anchor_rpc"] = all_vars["root_anchor_rpc"]
        if all_vars.get("root_anchor_submitter_key"):
            gw_data["submitter_key"] = all_vars["root_anchor_submitter_key"]
        if all_vars.get("genesis_supply"):
            gw_data["genesis_supply"] = str(all_vars["genesis_supply"])
        if all_vars.get("per_chain_allocation"):
            gw_data["per_chain_allocation"] = str(all_vars["per_chain_allocation"])
        
        hosts = idata.get("all", {}).get("children", {}).get("private_chains", {}).get("hosts", {}) or {}
        for hname, hcfg in hosts.items():
            if isinstance(hcfg, dict) and "chain_id" in hcfg:
                cid = int(hcfg["chain_id"])
                rpc_map[cid] = int(hcfg.get("rpc_port", 8546 + (cid - 101)))
    except Exception:
        pass

if not chains_to_update:
    chains_to_update = list(rpc_map.keys()) if rpc_map else [101, 102]

chains_list = gw_data.get("chains", [])
existing_chain_map = {c["chain_id"]: c for c in chains_list if "chain_id" in c}
updated_chains = []

for cid in chains_to_update:
    cdir = os.path.join(script_dir, f"chain-{cid}")
    rpc_p = rpc_map.get(cid, 8546 + (cid - 101) if cid >= 101 else 8546)
    rpc_url = f"http://127.0.0.1:{rpc_p}"

    chain_entry = existing_chain_map.get(cid, {
        "chain_id": cid,
        "rpc_url": rpc_url,
        "quorum_threshold": 6667,
        "validators": []
    })
    chain_entry["rpc_url"] = rpc_url

    node_dirs = []
    if os.path.exists(cdir):
        candidates = [d for d in os.listdir(cdir) if d.startswith("node-") and os.path.isdir(os.path.join(cdir, d))]
        node_dirs = sorted(candidates, key=lambda x: int(x.split("-")[1]) if x.split("-")[1].isdigit() else 0)

    validators_list = []
    for nd in node_dirs:
        nid_str = nd.replace("node-", "")
        nid = int(nid_str) if nid_str.isdigit() else 0
        cfg_candidates = [
            os.path.join(cdir, nd, "config.json"),
            os.path.join(cdir, nd, "keys", "authority_key.json")
        ]
        bls_priv = ""
        for cp in cfg_candidates:
            if os.path.exists(cp):
                try:
                    with open(cp) as cf:
                        cd = json.load(cf)
                        bls_priv = cd.get("Databases", {}).get("BLSPrivateKey", "") or cd.get("private_key_hex", "") or cd.get("private_key", "")
                        if bls_priv:
                            break
                except Exception:
                    pass
        if bls_priv:
            validators_list.append({
                "name": nd,
                "node_id": nid,
                "bls_private_key": bls_priv,
                "stake": 1000
            })

    if validators_list:
        chain_entry["validators"] = validators_list

    updated_chains.append(chain_entry)

gw_data["chains"] = updated_chains

with open(gw_path, "w") as f:
    json.dump(gw_data, f, indent=2)

chain_ids_str = ", ".join(str(c["chain_id"]) for c in updated_chains)
total_validators = sum(len(c.get("validators", [])) for c in updated_chains)
print(f"  \033[32m✓\033[0m Đã cập nhật xong BLS keys cho {len(updated_chains)} chain [{chain_ids_str}] ({total_validators} validators) vào: gateway_register.json")
EOF
}

# ==============================================================================
# HÀM HỖ TRỢ: KIỂM TRA TRẠNG THÁI (STATUS)
# ==============================================================================
check_status() {
    print_banner
    echo -e "${BOLD}📊 TRẠNG THÁI CÁC PRIVATE CHAIN & VALIDATOR NODES:${NC}\n"
    for cid in "${CHAINS_TO_PROCESS[@]}"; do
        local cdir="$SCRIPT_DIR/chain-$cid"
        local base_rpc="${CHAIN_RPCS[$cid]:-8546}"
        local nodes=($(get_chain_nodes "$cid"))

        echo -e "🔹 ${BOLD}Chain $cid:${NC}"
        for nid in "${nodes[@]}"; do
            local ndir="$cdir/node-$nid"
            local pid=$(cat "$ndir/node-$nid.pid" 2>/dev/null || echo "")
            if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
                pid=$(pgrep -f "chain-$cid/node-$nid/config.json" 2>/dev/null | head -n 1 || echo "")
                if [ -n "$pid" ]; then
                    echo "$pid" > "$ndir/node-$nid.pid" 2>/dev/null || true
                fi
            fi
            local rpc_port=$((base_rpc + nid))
            local is_running="${RED}❌ Đang tắt${NC}"

            if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
                is_running="${GREEN}🟢 Đang chạy (PID: $pid)${NC}"
            fi

            # Kiểm tra block number qua RPC
            local block_hex=$(curl -s --connect-timeout 1 "http://127.0.0.1:${rpc_port}" \
                -H "Content-Type: application/json" \
                -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
                | grep -o '"result":"[^"]*"' | cut -d'"' -f4 || echo "")

            local block_dec="N/A"
            if [ -n "$block_hex" ]; then
                block_dec=$((block_hex))
            fi

            echo -e "   → ${BOLD}Node-$nid${NC}: $is_running | RPC: http://127.0.0.1:${rpc_port} | Block: ${BOLD}${block_dec}${NC} ($block_hex)"
        done
        echo ""
    done
}

# ==============================================================================
# THỰC THI CHÍNH DỰA THEO ACTION
# ==============================================================================
print_banner
if [ "$TARGET_NODE" == "all" ]; then
    echo -e "🎯 Mục tiêu Chain: ${BOLD}${TARGET_CHAIN}${NC} | Node: ${BOLD}Tất cả (all)${NC} | Hành động: ${BOLD}${ACTION}${NC}\n"
else
    echo -e "🎯 Mục tiêu Chain: ${BOLD}${TARGET_CHAIN}${NC} | Node: ${BOLD}Node-${TARGET_NODE}${NC} | Hành động: ${BOLD}${ACTION}${NC}\n"
fi

case "$ACTION" in
    stop)
        for cid in "${CHAINS_TO_PROCESS[@]}"; do
            stop_chain_node "$cid"
        done
        ;;
    
    clean_data)
        for cid in "${CHAINS_TO_PROCESS[@]}"; do
            clean_chain_data "$cid"
            start_chain_node "$cid"
        done
        update_gateway_register_json
        echo -e "\n🏛️  Tự động cập nhật danh bạ liên chuỗi (Gateway Registry)..."
        "$SCRIPT_DIR/bin/register_chains" --config "$GATEWAY_JSON" || true
        ;;
    
    reset_all)
        for cid in "${CHAINS_TO_PROCESS[@]}"; do
            generate_chain_config "$cid"
            start_chain_node "$cid"
        done
        update_gateway_register_json
        echo -e "\n🏛️  Tự động cập nhật danh bạ liên chuỗi (Gateway Registry)..."
        "$SCRIPT_DIR/bin/register_chains" --config "$GATEWAY_JSON" || true
        ;;

    restart)
        for cid in "${CHAINS_TO_PROCESS[@]}"; do
            stop_chain_node "$cid"
            start_chain_node "$cid"
        done
        update_gateway_register_json
        ;;

    start)
        for cid in "${CHAINS_TO_PROCESS[@]}"; do
            start_chain_node "$cid"
        done
        update_gateway_register_json
        ;;

    update_gateway)
        update_gateway_register_json
        ;;

    status)
        check_status
        ;;

    register)
        update_gateway_register_json
        echo -e "\n🏛️  Đăng ký các chain lên Gateway Contract của Root Anchor..."
        "$SCRIPT_DIR/bin/register_chains" --config "$GATEWAY_JSON"
        ;;

    relayer)
        update_gateway_register_json
        echo -e "\n🌉 Khởi chạy Cross-Chain Relayer Daemon..."
        "$SCRIPT_DIR/bin/cross_chain_relayer" --config "$GATEWAY_JSON"
        ;;
esac

echo -e "\n${GREEN}✨ Hoàn tất thành công!${NC}"
