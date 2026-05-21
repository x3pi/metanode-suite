#!/bin/bash

# Tự động xuất traceback cho debugging
export GOTRACEBACK=all
export RUST_BACKTRACE=full

# Xóa cờ dừng cũ nếu có
rm -f /tmp/MTN_CHAIN_ERROR_STOP

# Tự động lấy thư mục gốc của project tool-test
TOOL_TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# Thư mục gốc của metanode (cùng cấp với tool-test)
METANODE_DIR="$(cd "$TOOL_TEST_DIR/../metanode" && pwd)"

# Suy ra thư mục script mtn-consensus
METANODE_SCRIPT_DIR="$METANODE_DIR/consensus/metanode/scripts/node"

# Thư mục chứa rpc proxy
RPC_CLIENT_DIR="$METANODE_DIR/execution/cmd/rpc/cmd/rpc-client"

# Cấu hình chế độ deploy (mặc định là single)
DEPLOY_MODE="single"

# ----------------------------------------------------
# HÀM XỬ LÝ LỖI & TẠO REPORT
# ----------------------------------------------------
handle_error() {
    local step_name="$1"
    local log_file="$2"
    echo ""
    echo "❌ Lỗi xảy ra tại: $step_name. Đang thu thập log và tạo báo cáo..."
    
    local report_file="$TOOL_TEST_DIR/scripts/error_report.txt"
    echo "==================================================" > "$report_file"
    echo "🛑 ERROR REPORT: $step_name" >> "$report_file"
    echo "⏰ Time: $(date)" >> "$report_file"
    echo "==================================================" >> "$report_file"
    
    echo -e "\n[1] COMMAND OUTPUT (Last 100 lines):" >> "$report_file"
    echo "--------------------------------------------------" >> "$report_file"
    if [ -f "$log_file" ]; then
        tail -n 100 "$log_file" >> "$report_file"
    else
        echo "No command output log found." >> "$report_file"
    fi
    
    echo -e "\n[2] RPC PROXY LATEST LOG (Last 50 lines):" >> "$report_file"
    echo "--------------------------------------------------" >> "$report_file"
    LATEST_RPC_LOG=$(find "$RPC_CLIENT_DIR" -maxdepth 3 -path "*/node0_data/logs/*.log" -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -n 1 | cut -d' ' -f2-)
    if [ -n "$LATEST_RPC_LOG" ]; then
        echo "  (Log file: $LATEST_RPC_LOG)" >> "$report_file"
        tail -n 50 "$LATEST_RPC_LOG" >> "$report_file"
    else
        echo "No RPC Proxy log found (searched in $RPC_CLIENT_DIR/node0_data/logs/)." >> "$report_file"
    fi

    echo -e "\n[3] METANODE 0 LATEST LOG (Last 100 lines):" >> "$report_file"
    echo "--------------------------------------------------" >> "$report_file"
    LATEST_NODE0_LOG=$(find "$METANODE_DIR/consensus/metanode/logs/node_0" -type f -name "*.log" -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -n 1 | cut -d' ' -f2-)
    if [ -n "$LATEST_NODE0_LOG" ]; then
        tail -n 100 "$LATEST_NODE0_LOG" >> "$report_file"
    else
        echo "No Node 0 log found." >> "$report_file"
    fi

    echo "==================================================" >> "$report_file"
    echo "✅ Đã lưu báo cáo lỗi chi tiết tại: $report_file"
    exit 1
}

run_and_capture() {
    local step_name="$1"
    shift
    local log_file="/tmp/auto_test_current_step.log"
    "$@" 2>&1 | tee "$log_file"
    local status=${PIPESTATUS[0]}
    if [ $status -ne 0 ]; then
        handle_error "$step_name" "$log_file"
    fi
}

# ----------------------------------------------------
# INIT: Kiểm tra và mount phân vùng BTRFS LVM
# ----------------------------------------------------
echo "📌 Đang kiểm tra mount point BTRFS cho database..."
if ! mountpoint -q "$METANODE_DIR/execution/cmd/simple_chain/sample" 2>/dev/null; then
    echo "  -> Phân vùng chưa được mount, đang tìm migrate-to-btrfs-lvm.sh..."
    if [ -f "$METANODE_DIR/execution/cmd/simple_chain/migrate-to-btrfs-lvm.sh" ]; then
        echo "  -> Đang chạy migrate-to-btrfs-lvm.sh (có thể yêu cầu sudo)..."
        bash "$METANODE_DIR/execution/cmd/simple_chain/migrate-to-btrfs-lvm.sh"
        if [ $? -ne 0 ]; then
            echo "❌ Lỗi khi chạy migrate-to-btrfs-lvm.sh!"
            exit 1
        fi
    else
        echo "  -> File migrate-to-btrfs-lvm.sh không tồn tại. Bỏ qua bước mount BTRFS."
    fi
else
    echo "  -> Phân vùng BTRFS đã được mount sẵn."
fi

# ----------------------------------------------------
# BƯỚC 1: Xóa genesis cũ và tạo file genesis mới
# ----------------------------------------------------
echo ""
echo "📌 BƯỚC 1: Prepare Genesis & Gen Spam Keys..."
cd "$METANODE_DIR/execution/cmd/simple_chain"
echo "  -> Xóa genesis.json và copy từ genesis-main.json..."
rm -f genesis.json
cp genesis-main.json genesis.json

cd "$TOOL_TEST_DIR/test_tps/gen_spam_keys"
echo "  -> Chạy Gen Spam Keys (count 50000)..."
run_and_capture "Gen Spam Keys (Bước 1)" go run main.go --count 50000 --genesis-in "$METANODE_DIR/execution/cmd/simple_chain/genesis-main.json" --genesis-out "$METANODE_DIR/execution/cmd/simple_chain/genesis.json"

# ----------------------------------------------------
# BƯỚC 2: Triển khai Cụm
# ----------------------------------------------------
echo ""
echo "📌 BƯỚC 2: Triển khai cụm Cluster (deploy_cluster.sh)..."
if [ "$DEPLOY_MODE" == "single" ]; then
    cd "$METANODE_SCRIPT_DIR/.."
    run_and_capture "Deploy Cluster Mạng Lớn (Bước 2)" ./mtn-orchestrator.sh restart --fresh --build-all
else
    cd "$METANODE_SCRIPT_DIR"
    run_and_capture "Deploy Cluster Single (Bước 2)" ./deploy_cluster.sh --env deploy-3machines.env --all
fi

# Đợi 1 chút để các HTTP server start up hoàn toàn
sleep 5

# ----------------------------------------------------
# BƯỚC 2.5: Bật RPC Proxy (Cả 5 node)
# ----------------------------------------------------
echo ""
echo "📌 BƯỚC 2.5: Kiểm tra và bật RPC Proxy cho cả 5 node..."
cd "$RPC_CLIENT_DIR"

# Luôn khởi tạo lại TLS cert/key mới để đảm bảo tính đồng bộ khớp khóa
echo "  -> Khởi tạo lại TLS cert/key..."
rm -f certificate.pem private.key certificate.csr
openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout private.key -out certificate.pem -subj "/CN=localhost" 2>/dev/null

# Dọn dẹp session cũ nếu có
tmux kill-session -t rpc-proxy 2>/dev/null || true

# Khởi động từng RPC Proxy cho 5 node
declare -A NODE_PORTS=( [0]=8545 [1]=8547 [2]=8548 [3]=8549 [4]=8550 )

for node_id in 0 1 2 3 4; do
    port=${NODE_PORTS[$node_id]}
    echo "  -> Đang kiểm tra RPC Proxy Node $node_id ở port $port..."
    if ! curl -s http://127.0.0.1:$port > /dev/null; then
        echo "     -> RPC Proxy Node $node_id chưa bật, đang khởi động..."
        tmux kill-session -t rpc-proxy-$node_id 2>/dev/null || true
        tmux new-session -d -s rpc-proxy-$node_id "go run main.go --config config-rpc-node$node_id.json --tcp-config config-client-tcp-node$node_id.json"
        
        # Đợi khởi động
        for i in {1..15}; do
            if curl -s http://127.0.0.1:$port -m 1 > /dev/null; then
                break
            fi
            sleep 1
        done

        if ! curl -s http://127.0.0.1:$port -m 2 > /dev/null; then
            echo "     ❌ Khởi động RPC Proxy Node $node_id thất bại!"
            exit 1
        else
            echo "     ✅ RPC Proxy Node $node_id đã khởi động thành công."
        fi
    else
        echo "     ✅ RPC Proxy Node $node_id đã hoạt động."
    fi
done
