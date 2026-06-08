#!/bin/bash

set -euo pipefail

# Tự động xuất traceback cho debugging
export GOTRACEBACK=all
export RUST_BACKTRACE=full

# Xóa cờ dừng cũ nếu có
rm -f /tmp/MTN_CHAIN_ERROR_STOP
pkill -f "test-rpc/test-history" || true
pkill -f "block_hash_checker" || true
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
export DEPLOY_MODE="single"
BATCH_SIZE=""

# Nhận tham số truyền vào từ command line (VD: ./auto_test.sh --steps "2,4,5" --mode multi)
while [[ "$#" -gt 0 ]]; do
    case $1 in
        --step|--steps) STEPS_TO_RUN="$2"; shift ;;
        --mode) DEPLOY_MODE="$2"; shift ;;
        --batch) BATCH_SIZE="$2"; shift ;;
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
    LATEST_NODE0_LOG=$(find "$METANODE_DIR/consensus/metanode/logs/node_0" -type f -name "*.log" -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -n 1 | cut -d' ' -f2-)
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
    if [ -f /tmp/MTN_CHAIN_ERROR_STOP ]; then
        echo -e "\n🛑 PHÁT HIỆN CỜ DỪNG HỆ THỐNG (/tmp/MTN_CHAIN_ERROR_STOP):"
        cat /tmp/MTN_CHAIN_ERROR_STOP
        echo -e "\n"
        exit 1
    fi
    local step_name="$1"
    shift
    local log_file="/tmp/auto_test_current_step.log"
    "$@" 2>&1 | tee "$log_file"
    local status=${PIPESTATUS[0]}
    if [ $status -ne 0 ]; then
        handle_error "$step_name" "$log_file"
    fi
    if [ -f /tmp/MTN_CHAIN_ERROR_STOP ]; then
        echo -e "\n🛑 PHÁT HIỆN CỜ DỪNG HỆ THỐNG (/tmp/MTN_CHAIN_ERROR_STOP) SAU BƯỚC $step_name:"
        cat /tmp/MTN_CHAIN_ERROR_STOP
        echo -e "\n"
        exit 1
    fi
}

echo "=================================================="
if [ -n "$STEPS_TO_RUN" ]; then
    echo "🚀 BẮT ĐẦU AUTO TEST PIPELINE (CHỈ CHẠY CÁC BƯỚC: $STEPS_TO_RUN)"
else
    echo "🚀 BẮT ĐẦU AUTO TEST PIPELINE TỪ ĐẦU (ALL STEPS)..."
fi
echo "💡 Parameter hiện tại: MODE=$DEPLOY_MODE | STEPS_TO_RUN=${STEPS_TO_RUN:-ALL} | BATCH_SIZE=${BATCH_SIZE:-Default}"
echo "💡 Usage: ./auto_test.sh [--step|--steps \"2,4,5\"] [--mode single|multi] [--batch 10|500]"
echo "=================================================="

# ----------------------------------------------------
# BƯỚC 1, 2, 2.5: Setup Chain (BTRFS, Genesis, Deploy, RPC Proxy)
# ----------------------------------------------------
if should_run 1 || should_run 2; then
    echo ""
    echo "📌 BƯỚC 1 & 2: Chạy setup_chain.sh (bao gồm BTRFS, Genesis, Deploy và bật RPC Proxy)..."
    export DEPLOY_MODE="$DEPLOY_MODE"
    bash "$TOOL_TEST_DIR/scripts/setup/setup_chain.sh"
    
    if [ $? -ne 0 ]; then
        echo "❌ Lỗi khi chạy setup_chain.sh"
        exit 1
    fi
fi


# BẬT GIÁM SÁT LỆCH HASH & LỊCH SỬ STATE (CHẠY NGẦM)
# ----------------------------------------------------
echo ""
echo "📌 BẬT GIÁM SÁT LỆCH HASH NGẦM (block_hash_checker)..."
(
    cd "$TOOL_TEST_DIR/block/block_hash_checker"
    if [ "$DEPLOY_MODE" == "single" ]; then
        go run main.go --watch --interval 200ms > block_hash_checker_auto.log 2>&1
    else
        go run main.go --watch --interval 5s --config config-m-nodes.json > block_hash_checker_auto.log 2>&1
    fi
    if grep -q "bị lệch hash" block_hash_checker_auto.log; then
        echo -e "\n\n🚨 Phân tích từ log: Phát hiện blocks bị lệch hash!"
        echo -e "🚨🚨🚨 PHÁT HIỆN LỆCH HASH! ĐANG TIẾN HÀNH DỪNG AUTO TEST PIPELINE! 🚨🚨🚨\n\n"
        kill -TERM -$$ 2>/dev/null || kill -TERM $$
    fi
) &
CHECKER_PID=$!

echo "📌 BẬT GIÁM SÁT LỊCH SỬ STATE NGẦM (test-history)..."
(
    cd "$TOOL_TEST_DIR/test-simple/test-rpc/test-history"
    if [ "$DEPLOY_MODE" == "multi" ]; then
        go run main.go -config=config-mutil.json -wait 5 -loop > history_checker_auto.log 2>&1
    else
        go run main.go -config=config-local.json -wait 5 -loop > history_checker_auto.log 2>&1
    fi
) &
HISTORY_PID=$!

echo "📌 BẬT GIÁM SÁT SỰ SỐNG CỦA NODE NGẦM (Node Health Checker)..."
(
    while true; do
        sleep 10
        if [ "$DEPLOY_MODE" == "multi" ]; then
            RPC_JSON_PATH="/tmp/rpc_nodes.json"
            if [ -f "$RPC_JSON_PATH" ]; then
                # Đọc các URL RPC từ file JSON
                urls=$(grep -oE 'http://[^"]+' "$RPC_JSON_PATH" 2>/dev/null || true)
                for url in $urls; do
                    if ! curl -s -m 2 "$url" >/dev/null 2>&1; then
                        echo -e "\n\n🚨🚨🚨 PHÁT HIỆN NODE CHẾT TẠI $url! ĐANG TIẾN HÀNH DỪNG AUTO TEST PIPELINE! 🚨🚨🚨\n\n"
                        echo "Node HTTP Server ($url) không phản hồi. Có thể process đã bị crash!" > /tmp/MTN_CHAIN_ERROR_STOP
                        kill -TERM -$$ 2>/dev/null || kill -TERM $$
                        exit 1
                    fi
                done
            fi
        else
            # Chế độ single (local mode)
            PORTS=(8757 10747 10749 10750 10748)
            for port in "${PORTS[@]}"; do
                if ! curl -s -m 2 "http://127.0.0.1:$port" >/dev/null 2>&1; then
                    echo -e "\n\n🚨🚨🚨 PHÁT HIỆN NODE CHẾT TẠI CỔNG $port! ĐANG TIẾN HÀNH DỪNG AUTO TEST PIPELINE! 🚨🚨🚨\n\n"
                    echo "Node HTTP Server (cổng $port) không phản hồi. Có thể process đã bị crash!" > /tmp/MTN_CHAIN_ERROR_STOP
                    kill -TERM -$$ 2>/dev/null || kill -TERM $$
                    exit 1
                fi
            done
        fi
    done
) &
HEALTH_PID=$!

trap "disown $CHECKER_PID $HISTORY_PID $HEALTH_PID 2>/dev/null; kill -9 $CHECKER_PID $HISTORY_PID $HEALTH_PID 2>/dev/null; pkill -f 'go run main.go --watch' || true; pkill -f 'exe/main --watch' || true; pkill -f 'test-rpc/test-history' || true; pkill -f 'config-local.json.*-loop' || true; pkill -f 'config-local.json.*-wait' || true" EXIT


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
# BƯỚC 7: Test History RPC (Đang chạy ngầm)
# ----------------------------------------------------
if should_run 7; then
    echo ""
    echo "📌 BƯỚC 7: Test History RPC (Đang chạy ngầm)..."
    if [ -f /tmp/MTN_CHAIN_ERROR_STOP ]; then
        echo -e "\n🛑 PHÁT HIỆN LỖI LỊCH SỬ STATE TỪ BẢN CHẠY NGẦM:"
        cat /tmp/MTN_CHAIN_ERROR_STOP
        echo -e "\n"
        exit 1
    fi
    echo "  -> Giám sát lịch sử state đã được bật ở chế độ chạy ngầm."
fi
# ----------------------------------------------------
# BƯỚC 8: Load Test TPS
# ----------------------------------------------------
if should_run 8; then
    echo ""
    echo "📌 BƯỚC 8: Load Test TPS (20,000 txs)..."
    cd "$TOOL_TEST_DIR/test_tps/tps_blast_cc"
    if [ "$DEPLOY_MODE" == "single" ]; then
        run_and_capture "Load Test TPS (Bước 8) [Single]" go run main.go --count 20000 --parallel_native=true --rounds 2000 --load_balance=false --batch="${BATCH_SIZE:-10}" --amount 1
    else
        run_and_capture "Load Test TPS (Bước 8) [Multi]" go run main.go --count 20000 --parallel_native=true --rounds 30 --load_balance=false --batch="${BATCH_SIZE:-300}" --amount 1
    fi
fi


echo ""
echo "=================================================="
echo "🎉 AUTO TEST PIPELINE COMPLETED SUCCESSFULLY!"
echo "=================================================="