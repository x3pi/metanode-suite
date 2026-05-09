#!/bin/bash

# Dừng pipeline nếu có bất kỳ lệnh nào bị lỗi
# set -e

# Tự động lấy thư mục gốc của project tool-test
TOOL_TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Thư mục gốc của metanode (cùng cấp với tool-test)
METANODE_DIR="$(cd "$TOOL_TEST_DIR/../metanode" && pwd)"

# Suy ra thư mục script mtn-consensus
METANODE_SCRIPT_DIR="$METANODE_DIR/consensus/metanode/scripts/node"

# Thư mục chứa rpc proxy
RPC_CLIENT_DIR="$METANODE_DIR/execution/cmd/rpc/cmd/rpc-client"

# Cấu hình danh sách các bước cụ thể để chạy (mặc định = chạy tất cả)
STEPS_TO_RUN=""
# Cấu hình chế độ deploy (mặc định là single)
DEPLOY_MODE="single"

# Nhận tham số truyền vào từ command line (VD: ./auto_test.sh --steps "2,4,5" --mode multi)
while [[ "$#" -gt 0 ]]; do
    case $1 in
        --step|--steps) STEPS_TO_RUN="$2"; shift ;;
        --mode) DEPLOY_MODE="$2"; shift ;;
    esac
    shift
done

STEPS_TO_RUN_NORMALIZED=$(echo "$STEPS_TO_RUN" | tr ',' ' ')

# Hàm kiểm tra xem có chạy step hiện tại không
should_run() {
    local step=$1
    if [ -n "$STEPS_TO_RUN" ]; then
        for s in $STEPS_TO_RUN_NORMALIZED; do
            if [ "$s" == "$step" ]; then return 0; fi
        done
        return 1
    else
        # Nếu không truyền --steps, mặc định chạy tất cả
        return 0
    fi
}

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
    # Tìm log trong thư mục node0_data/logs (của riêng node 0)
    LATEST_RPC_LOG=$(find "$RPC_CLIENT_DIR" -maxdepth 3 -path "*/node0_data/logs/*.log" -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -n 1 | cut -d' ' -f2-)
    if [ -n "$LATEST_RPC_LOG" ]; then
        echo "  (Log file: $LATEST_RPC_LOG)" >> "$report_file"
        tail -n 50 "$LATEST_RPC_LOG" >> "$report_file"
    else
        echo "No RPC Proxy log found (searched in $RPC_CLIENT_DIR/node0_data/logs/)." >> "$report_file"
    fi

    echo -e "\n[3] METANODE 0 LATEST LOG (Last 100 lines):" >> "$report_file"
    echo "--------------------------------------------------" >> "$report_file"
    LATEST_NODE0_LOG=$(find "$METANODE_SCRIPT_DIR/logs/node0" -type f -name "*.log" -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -n 1 | cut -d' ' -f2-)
    if [ -n "$LATEST_NODE0_LOG" ]; then
        tail -n 100 "$LATEST_NODE0_LOG" >> "$report_file"
    else
        echo "No Node 0 log found." >> "$report_file"
    fi

    echo "==================================================" >> "$report_file"
    echo "✅ Đã lưu báo cáo lỗi chi tiết tại: $report_file"
    echo "👉 Hãy gửi nội dung file này cho Agent để debug!"
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

echo "=================================================="
if [ -n "$STEPS_TO_RUN" ]; then
    echo "🚀 BẮT ĐẦU AUTO TEST PIPELINE (CHỈ CHẠY CÁC BƯỚC: $STEPS_TO_RUN)"
else
    echo "🚀 BẮT ĐẦU AUTO TEST PIPELINE TỪ ĐẦU (ALL STEPS)..."
fi
echo "💡 Parameter hiện tại: MODE=$DEPLOY_MODE | STEPS_TO_RUN=${STEPS_TO_RUN:-ALL}"
echo "💡 Usage: ./auto_test.sh [--step|--steps \"2,4,5\"] [--mode single|multi]"
echo "=================================================="

# ----------------------------------------------------
# INIT: Kiểm tra và mount phân vùng BTRFS LVM
# ----------------------------------------------------
echo "📌 Đang kiểm tra mount point BTRFS cho database..."
if ! mountpoint -q "$METANODE_DIR/execution/cmd/simple_chain/sample" 2>/dev/null; then
    echo "  -> Phân vùng chưa được mount, đang tiến hành chạy migrate-to-btrfs-lvm.sh (có thể yêu cầu sudo)..."
    # Dùng lệnh chạy bình thường không đưa vào run_and_capture vì sudo sẽ yêu cầu gõ password trực tiếp trên terminal
    bash "$METANODE_DIR/execution/cmd/simple_chain/migrate-to-btrfs-lvm.sh"
    if [ $? -ne 0 ]; then
        echo "❌ Lỗi khi chạy migrate-to-btrfs-lvm.sh!"
        exit 1
    fi
else
    echo "  -> Phân vùng BTRFS đã được mount sẵn."
fi

# ----------------------------------------------------
# BƯỚC 1: Xóa genesis cũ và tạo file genesis mới
# ----------------------------------------------------
# if should_run 1; then
#     echo ""
#     echo "📌 BƯỚC 1: Prepare Genesis & Gen Spam Keys..."
#     cd "$METANODE_DIR/execution/cmd/simple_chain"
#     echo "  -> Xóa genesis.json và copy từ genesis-main.json..."
#     rm -f genesis.json
#     cp genesis-main.json genesis.json

#     cd "$TOOL_TEST_DIR/test_tps/gen_spam_keys"
#     echo "  -> Chạy Gen Spam Keys (count 50000)..."
#     run_and_capture "Gen Spam Keys (Bước 1)" go run main.go --count 50000 --genesis-in "$METANODE_DIR/execution/cmd/simple_chain/genesis-main.json" --genesis-out "$METANODE_DIR/execution/cmd/simple_chain/genesis.json"
# fi

# ----------------------------------------------------
# BƯỚC 2: Triển khai Cụm
# ----------------------------------------------------
if should_run 2; then
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
fi

# ----------------------------------------------------
# BƯỚC 2.5: Bật RPC Proxy
# ----------------------------------------------------
if should_run 2; then
    echo ""
    echo "📌 BƯỚC 2.5: Kiểm tra và bật RPC Proxy..."
    if ! curl -s http://127.0.0.1:8545 > /dev/null; then
        echo "  -> RPC Proxy chưa bật, đang tiến hành khởi động qua tmux session 'rpc-proxy'..."
        cd "$RPC_CLIENT_DIR"
        
        # Tự động tạo TLS Certificate & Key nếu chưa có
        if [ ! -f "certificate.pem" ] || [ ! -f "private.key" ]; then
            echo "  -> Không tìm thấy TLS cert/key, đang tự động khởi tạo..."
            openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout private.key -out certificate.pem -subj "/CN=localhost" 2>/dev/null
        fi

        # Nếu session đã tồn tại thì tắt đi trước khi tạo mới
        tmux kill-session -t rpc-proxy 2>/dev/null || true
        tmux new-session -d -s rpc-proxy 'go run main.go --config config-rpc-node0.json --tcp-config config-client-tcp-node0.json'
        
        echo "  -> Đang chờ RPC proxy khởi động (tối đa 15s)..."
        for i in {1..15}; do
            if curl -s http://127.0.0.1:8545 -m 1 > /dev/null; then
                break
            fi
            sleep 1
        done
        
        # Kiểm tra lại xem đã lên chưa
        if ! curl -s http://127.0.0.1:8545 -m 2 > /dev/null; then
            echo "  ❌ Khởi động RPC Proxy thất bại!"
            echo "  📄 Tmux Pane Output:"
            echo "--------------------------------------------------"
            tmux capture-pane -p -t rpc-proxy || echo "Cannot capture tmux pane"
            echo "--------------------------------------------------"
            
            # Tìm file log mới nhất trong node0_data/logs
            LATEST_LOG=$(find "$RPC_CLIENT_DIR" -maxdepth 3 -path "*/node0_data/logs/*.log" -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -n 1 | cut -d' ' -f2-)
            if [ -n "$LATEST_LOG" ]; then
                echo "  📄 File log: $LATEST_LOG"
                echo "--------------------------------------------------"
                tail -n 30 "$LATEST_LOG"
                echo "--------------------------------------------------"
            else
                echo "  ⚠️ Không tìm thấy file log nào trong $RPC_CLIENT_DIR/node0_data/logs/"
            fi
            exit 1
        else
            echo "  ✅ RPC Proxy đã khởi động thành công ở port 8545."
        fi
    else
        echo "  ✅ RPC Proxy đã hoạt động ở port 8545."
    fi
fi

# ----------------------------------------------------
# BƯỚC 3: Test TCP Caller
# ----------------------------------------------------
if should_run 3; then
    echo ""
    echo "📌 BƯỚC 3: Test TCP RPC (main-no-none.go)..."
    cd "$TOOL_TEST_DIR/test-simple/test-tcp/caller-tcp"
    run_and_capture "Test TCP (Bước 3)" go run main-no-none.go -config=config-local.json -data=data.json
    
fi

# ----------------------------------------------------
# BƯỚC 4: Test HTTP RPC - Xapian V0
# ----------------------------------------------------
if should_run 4; then
    echo ""
    echo "📌 BƯỚC 4: Test Xapian V0..."
    cd "$TOOL_TEST_DIR/test-simple/test-rpc"
    run_and_capture "Test Xapian V0 (Bước 4)" go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-v0.json

fi

# ----------------------------------------------------
# BƯỚC 5: Test Send Native Coin
# ----------------------------------------------------
if should_run 5; then
    echo ""
    echo "📌 BƯỚC 5: Test Send Native Coin..."
    cd "$TOOL_TEST_DIR/test-simple/test-rpc/send-native"
    run_and_capture "Send Native (Bước 5)" go run main.go
fi

# ----------------------------------------------------
# BƯỚC 6: Test HTTP RPC - Xapian V2
# ----------------------------------------------------
if should_run 6; then
    echo ""
    echo "📌 BƯỚC 6: Test Xapian V2..."
    cd "$TOOL_TEST_DIR/test-simple/test-rpc"
    run_and_capture "Test Xapian V2 (Bước 6)" go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-v2.json
fi

# ----------------------------------------------------
# BƯỚC 7: Load Test TPS
# ----------------------------------------------------
if should_run 7; then
    echo ""
    echo "📌 BƯỚC 7: Load Test TPS (20,000 txs)..."
    cd "$TOOL_TEST_DIR/test_tps/tps_blast_cc"
    if [ "$DEPLOY_MODE" == "single" ]; then
        run_and_capture "Load Test TPS (Bước 7) [Single]" go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10
    else
        run_and_capture "Load Test TPS (Bước 7) [Multi]" go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=true --batch=500
    fi
fi

echo ""
echo "=================================================="
echo "🎉 AUTO TEST PIPELINE COMPLETED SUCCESSFULLY!"
echo "=================================================="