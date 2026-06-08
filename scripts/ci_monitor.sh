#!/bin/bash
# =============================================================
# ci_monitor.sh — Quản lý và Khởi động các CI Monitor của Metanode
#
# Chức năng:
#   1. Kill TẤT CẢ các loại monitor và test scripts cũ đang chạy ngầm
#      (ci_monitor.py, ci_recovery_monitor.py, ci_snapshot_monitor.py,
#       auto_test.sh, test-node-recovery-gap.sh, test-snapshot.sh)
#   2. Dọn dẹp triệt để các tiến trình cluster và giải phóng port
#   3. Khởi động loại Monitor được chỉ định (mặc định: spam)
#
# Cách dùng:
#   ./ci_monitor.sh --type spam [--clean-logs] [--mode single] [--no-start]
#   ./ci_monitor.sh --type recovery [--clean-logs] [--no-start]
#   ./ci_monitor.sh --type snapshot [--clean-logs] [--no-start]
#   ./ci_monitor.sh --type spam_xapian [--clean-logs] [--no-start]
# =============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# ─── Đảm bảo duy nhất một instance ci_monitor.sh hoạt động ───
MY_PID=$$
OTHER_PIDS=$(pgrep -f "ci_monitor.sh" | grep -v "^$MY_PID$" || true)
if [ -n "$OTHER_PIDS" ]; then
    echo "⚠️ Phát hiện phiên bản ci_monitor.sh khác đang chạy (PIDs: $OTHER_PIDS). Tiến hành tắt tiến trình cũ..."
    for pid in $OTHER_PIDS; do
        kill -9 "$pid" 2>/dev/null || true
    done
    sleep 1
fi

# Mặc định các thông số
TYPE="spam"
CLEAN_LOGS=false
NO_START=false
CI_ARGS=()

# Parse arguments
DO_STOP=false
while [[ "$#" -gt 0 ]]; do
    case $1 in
        --type)
            TYPE="$2"
            shift
            ;;
        --clean-logs)
            CLEAN_LOGS=true
            ;;
        --no-start)
            NO_START=true
            CI_ARGS+=("$1")
            ;;
        --mode)
            MODE="$2"
            CI_ARGS+=("$1" "$2")
            shift
            ;;
        stop|--stop|kill|--kill)
            DO_STOP=true
            ;;
        *)
            CI_ARGS+=("$1")
            ;;
    esac
    shift
done

if [ "$DO_STOP" = true ]; then
    TYPE="all" # Skip type validation when stopping
elif [[ "$TYPE" != "spam" && "$TYPE" != "recovery" && "$TYPE" != "snapshot" && "$TYPE" != "spam_xapian" ]]; then
    echo "❌ Lỗi: Loại monitor không hợp lệ. Chỉ chấp nhận: spam, recovery, snapshot, spam_xapian."
    exit 1
fi

# Thiết lập file tương ứng với từng loại
if [ "$TYPE" = "spam" ]; then
    MONITOR_NAME="ci_monitor"
    TEST_SCRIPT="auto_test.sh"
    LOGS_DIR_NAME="auto_test_logs"
    # Default mode cho spam nếu không truyền gì
    if [ ${#CI_ARGS[@]} -eq 0 ]; then
        CI_ARGS=("--mode" "single")
    fi
elif [ "$TYPE" = "recovery" ]; then
    MONITOR_NAME="ci_recovery_monitor"
    TEST_SCRIPT="test-node-recovery-gap.sh"
    LOGS_DIR_NAME="recovery_test_logs"
elif [ "$TYPE" = "spam_xapian" ]; then
    MONITOR_NAME="ci_spam_xapian_monitor"
    TEST_SCRIPT="test-spam-xapian.sh"
    LOGS_DIR_NAME="spam_xapian_logs"
else
    MONITOR_NAME="ci_snapshot_monitor"
    TEST_SCRIPT="test-snapshot.sh"
    LOGS_DIR_NAME="snapshot_test_logs"
fi

CI_MONITOR="$SCRIPT_DIR/${MONITOR_NAME}.py"
CI_LOG="$SCRIPT_DIR/${MONITOR_NAME}.log"
AUTO_TEST_LOGS="$SCRIPT_DIR/${LOGS_DIR_NAME}"

wait_for_ports_to_release() {
    local ports=("$@")
    echo "🔍 Đang kiểm tra và chờ giải phóng các cổng: ${ports[*]}..."
    local start_time=$(date +%s)
    local timeout=15
    
    local SUDO_PASS="1234@abcd"
    
    while true; do
        local busy_ports=()
        for port in "${ports[@]}"; do
            if ss -tlnp 2>/dev/null | grep -qE ":$port\s"; then
                busy_ports+=("$port")
            fi
        done
        
        if [ ${#busy_ports[@]} -eq 0 ]; then
            echo "✅ Tất cả các cổng đã được giải phóng hoàn toàn!"
            return 0
        fi
        
        local current_time=$(date +%s)
        local elapsed=$((current_time - start_time))
        if [ $elapsed -ge $timeout ]; then
            echo "⚠️ Cảnh báo: Các cổng vẫn bị chiếm giữ sau ${timeout}s: ${busy_ports[*]}"
            for port in "${busy_ports[@]}"; do
                local pids=$(echo "$SUDO_PASS" | sudo -S ss -tlnp 2>/dev/null | grep -E ":$port\s" | grep -oP 'pid=\K[0-9]+' | sort -u || true)
                if [ -n "$pids" ]; then
                    for p in $pids; do
                        echo "  → Force killing PID $p occupying port $port..."
                        echo "$SUDO_PASS" | sudo -S kill -9 "$p" 2>/dev/null || kill -9 "$p" 2>/dev/null || true
                    done
                else
                    echo "  → Force killing port $port using fuser..."
                    echo "$SUDO_PASS" | sudo -S fuser -k -9 -n tcp "$port" 2>/dev/null || fuser -k -9 -n tcp "$port" 2>/dev/null || true
                fi
            done
            sleep 1
            return 0
        fi
        
        echo "  → Các cổng đang bị giữ: ${busy_ports[*]}. Chờ giải phóng..."
        sleep 0.5
    done
}

cleanup_all_processes() {
    echo ""
    echo "🔪 Dọn dẹp TOÀN BỘ tiến trình cũ đang chạy ngầm..."

    local SUDO_PASS="1234@abcd"

    # Dừng các dịch vụ systemd của metanode nếu đang hoạt động
    if [ "$NO_START" != "true" ] && [ "${MODE:-}" != "multi" ]; then
        if systemctl list-units --type=service | grep -qE "metanode-"; then
            echo "   → Phát hiện các dịch vụ systemd metanode đang chạy. Đang dừng các dịch vụ..."
            if ! echo "$SUDO_PASS" | sudo -S systemctl stop metanode-consensus-0 metanode-consensus-1 metanode-consensus-2 metanode-consensus-3 metanode-consensus-4 \
                                     metanode-execution-0 metanode-execution-1 metanode-execution-2 metanode-execution-3 metanode-execution-4 \
                                     metanode-rpc-0 metanode-rpc-1 metanode-rpc-2 metanode-rpc-3 metanode-rpc-4 \
                                     metanode-consensus.service metanode-execution.service metanode.service 2>/dev/null; then
                echo "   ⚠️  Warning: Failed to stop some systemd metanode services."
            fi
        fi
    fi

    # 1. Kill tất cả Python monitors
    for m in "ci_monitor.py" "ci_recovery_monitor.py" "ci_snapshot_monitor.py" "ci_spam_xapian_monitor.py"; do
        local pat="[${m:0:1}]${m:1}"
        PIDS_M=$(pgrep -f "$pat" 2>/dev/null)
        if [ -n "$PIDS_M" ]; then
            echo "   → Tìm thấy $m (PIDs: $PIDS_M). Đang kill..."
            pkill -f "$pat" 2>/dev/null
            sleep 0.5
            pkill -9 -f "$pat" 2>/dev/null || true
        fi
    done

    # 2. Kill tất cả Shell test runners
    for t in "auto_test.sh" "test-node-recovery-gap.sh" "test-snapshot.sh" "test-spam-xapian.sh" "run_spam.sh"; do
        local pat="[${t:0:1}]${t:1}"
        PIDS_T=$(pgrep -f "$pat" 2>/dev/null)
        if [ -n "$PIDS_T" ]; then
            echo "   → Tìm thấy $t (PIDs: $PIDS_T). Đang kill..."
            pkill -f "$pat" 2>/dev/null
            sleep 0.5
            pkill -9 -f "$pat" 2>/dev/null || true
        fi
    done

    # 3. Kill các tiến trình phụ trợ
    for proc in "block_hash_checker_ci" "rpc-tcp-simple" "tps_blast_cc" "tx_sender" "spam_xapian_test"; do
        local pat="[${proc:0:1}]${proc:1}"
        if pgrep -f "$pat" >/dev/null; then
            echo "   → Tìm thấy $proc. Đang kill..."
            pkill -f "$pat" 2>/dev/null
            sleep 0.5
            pkill -9 -f "$pat" 2>/dev/null || true
        fi
    done

    # 3.1. Kill các tool chạy qua go run (watch/loop)
    pkill -9 -f "main.go -loop" 2>/dev/null || true

    if [ "$NO_START" != "true" ] && [ "${MODE:-}" != "multi" ]; then
        # 3.5. Dừng Nginx nếu đang chạy và chiếm cổng
        echo "   → Tắt dịch vụ Nginx (giải quyết lỗi 502 Bad Gateway)..."
        pkill -9 nginx 2>/dev/null || true

        # 4. Kill các session tmux liên quan đến cluster
        echo "   → Tìm và tắt các tmux sessions go-master, metanode, rpc-proxy..."
        for session in $(tmux list-sessions -F '#S' 2>/dev/null | grep -E '^(go-master-|metanode-|rpc-proxy)'); do
            echo "     - Tắt tmux session: $session"
            tmux kill-session -t "$session" 2>/dev/null || true
        done

        # 5. Tắt triệt để các tiến trình Go/Rust và các RPC proxy server
        echo "   → Tắt triệt để các tiến trình simple_chain, metanode, rpc-client, config-rpc..."
        pkill -9 -f "[s]imple_chain" 2>/dev/null || true
        pkill -9 -f "[m]etanode" 2>/dev/null || true
        pkill -9 -f "[r]pc-client" 2>/dev/null || true
        pkill -9 -f "rpc-client-bin" 2>/dev/null || true
        pkill -9 -f "config-rpc-node" 2>/dev/null || true
        pkill -9 -f "config-client-tcp" 2>/dev/null || true

        # 6. Giải phóng port của cluster
        echo "   → Giải phóng các port của cụm cluster..."
        wait_for_ports_to_release 8545 8547 8548 8549 8550 8757 10746 10747 10748 10749 10750 9100 9101 9102 9103 9104 19200 19201 19202 19203 19204 10100 10101 10102 10103 10104 6060 6061 6062 6063 6064 6065 6200 6201 6202 6203 6204 6211 6221 6241 4201 9080 9081 9082 9083 9084 8600 8601 8602 8603 8604
    else
        echo "   → Bỏ qua việc kill tiến trình local cluster và giải phóng port do có flag --no-start hoặc --mode multi"
    fi

    # Xóa cờ lỗi dừng test cũ để tránh nhận diện nhầm
    rm -f /tmp/MTN_CHAIN_ERROR_STOP

    echo "   ✅ Đã dọn sạch các tiến trình."
}

if [ "$DO_STOP" = true ]; then
    echo "======================================================="
    echo "🛑 YÊU CẦU DỪNG TẤT CẢ TIẾN TRÌNH"
    echo "======================================================="
    cleanup_all_processes
    exit 0
fi

echo "======================================================="
echo "🧹 KHỞI ĐỘNG HỆ THỐNG GIÁM SÁT CI: [${TYPE^^}]"
echo "======================================================="

# ─── Bước 1: Kill TẤT CẢ tiến trình cũ để tránh đè cổng ───────
echo ""
echo "🔪 [1/3] Đang dọn dẹp hệ thống..."
cleanup_all_processes

# ─── Bước 2: Xóa logs cũ theo từng loại ─────────────────────────
echo ""
if [ "$CLEAN_LOGS" = true ]; then
    echo "🗑️  [2/3] Xóa logs cũ (--clean-logs) của loại [${TYPE}]..."

    if [ -f "$CI_LOG" ]; then
        rm -f "$CI_LOG"
        echo "   → Đã xóa: ${MONITOR_NAME}.log"
    fi

    if [ -d "$AUTO_TEST_LOGS" ]; then
        # Giữ lại 1 file log mới nhất, xóa các file cũ
        LOG_FILES=($(ls -t "$AUTO_TEST_LOGS"/*.log 2>/dev/null))
        FILE_COUNT=${#LOG_FILES[@]}
        
        if [ "$FILE_COUNT" -gt 1 ]; then
            FILES_TO_DELETE=("${LOG_FILES[@]:1}")
            rm -f "${FILES_TO_DELETE[@]}"
            echo "   → Đã xóa $((${FILE_COUNT}-1)) files cũ trong ${LOGS_DIR_NAME}/, giữ lại 1 file mới nhất."
        elif [ "$FILE_COUNT" -eq 1 ]; then
            echo "   → Chỉ có 1 file log trong ${LOGS_DIR_NAME}/, đã giữ lại."
        else
            echo "   → Thư mục ${LOGS_DIR_NAME}/ trống."
        fi
    fi

    if [ -f "$SCRIPT_DIR/error_report.txt" ]; then
        rm -f "$SCRIPT_DIR/error_report.txt"
        echo "   → Đã xóa: error_report.txt"
    fi

    echo "   ✅ Đã dọn sạch logs."
else
    echo "📁 [2/3] Giữ lại logs cũ (mặc định)."
fi

# ─── Bước 3: Khởi động Monitor mới ────────────────────────
echo ""
echo "🚀 [3/3] Khởi động ${MONITOR_NAME}.py mới..."
echo "   → Args: ${CI_ARGS[*]}"
echo "   → Log:  $CI_LOG"

# Ghi lại log file mới nhất hiện tại trước khi khởi chạy
PREV_LATEST=""
if [ -d "$AUTO_TEST_LOGS" ]; then
    PREV_LATEST=$(ls -t "$AUTO_TEST_LOGS"/*.log 2>/dev/null | head -n 1)
fi

# Đợi các port cũ giải phóng hoàn toàn
if [ "$NO_START" != "true" ]; then
    wait_for_ports_to_release 8545 8547 8548 8549 8550 8757 10746 10747 10748 10749 10750 9100 9101 9102 9103 9104 19200 19201 19202 19203 19204 10100 10101 10102 10103 10104 6060 6061 6062 6063 6064 6065 6200 6201 6202 6203 6204 6211 6221 6241 4201 9080 9081 9082 9083 9084 8600 8601 8602 8603 8604
else
    echo "   → Bỏ qua bước chờ giải phóng port do có flag --no-start"
fi


export MTN_TELE_ALERT=true

# Khởi chạy python monitor dưới nền bằng nohup
nohup "$CI_MONITOR" "${CI_ARGS[@]}" > "$CI_LOG" 2>&1 &
NEW_PID=$!

echo "   → Đang kiểm tra quá trình khởi tạo và build/deploy cụm node..."
SUCCESS=false
# Chờ tối đa 150 giây để quá trình compile và deploy cụm node hoàn tất
for i in {1..150}; do
    # Kiểm tra xem tiến trình Python có bị chết không
    if ! kill -0 "$NEW_PID" 2>/dev/null; then
        echo "   ❌ Monitor process đã chết đột ngột! Kiểm tra log chính:"
        cat "$CI_LOG" 2>/dev/null
        exit 1
    fi

    # Kiểm tra file sentinel báo lỗi dừng khẩn cấp
    if [ -f /tmp/MTN_CHAIN_ERROR_STOP ]; then
        echo "   ❌ Phát hiện lỗi dừng khẩn cấp trong quá trình khởi tạo!"
        cat /tmp/MTN_CHAIN_ERROR_STOP
        exit 1
    fi

    # Lấy file log test mới nhất
    LATEST_LOG_FILE=""
    if [ -d "$AUTO_TEST_LOGS" ]; then
        LATEST_LOG_FILE=$(ls -t "$AUTO_TEST_LOGS"/*.log 2>/dev/null | head -n 1)
    fi

    # Nếu có file log mới và file này khác file cũ, kiểm tra nội dung
    if [ -n "$LATEST_LOG_FILE" ] && [ "$LATEST_LOG_FILE" != "$PREV_LATEST" ]; then
        # Kiểm tra xem đã hoàn thành việc deploy cụm RPC proxy hoặc deploy cluster chưa
        if grep -qE "RPC Proxy Node 4 đã khởi động thành công|DEPLOYMENT COMPLETE!" "$LATEST_LOG_FILE" 2>/dev/null; then
            SUCCESS=true
            break
        fi
    fi
    sleep 1
done

if [ "$SUCCESS" = "true" ]; then
    echo "   ✅ Đã khởi động và deploy cluster thành công! (PID: $NEW_PID)"
    echo "   🚀 Tiến trình test tiếp tục chạy ngầm trong background."
else
    # Nếu hết 150 giây mà chưa thấy tín hiệu thành công
    echo "   ⚠️ Hết thời gian chờ (150s) nhưng chưa nhận dạng được trạng thái hoàn thành deploy."
    echo "   Kiểm tra xem process có còn chạy hay không..."
    if kill -0 "$NEW_PID" 2>/dev/null; then
        echo "   ✅ Process vẫn đang chạy ngầm. (PID: $NEW_PID)"
    else
        echo "   ❌ Process đã chết."
        exit 1
    fi
fi

# Chờ tối đa 5 giây để Python monitor thực hiện git ls-remote/pull và tạo log file mới
LATEST_LOG_FILE=""
for i in {1..50}; do
    LATEST_LOG_FILE=$(ls -t "$AUTO_TEST_LOGS"/*.log 2>/dev/null | head -n 1)
    if [ -n "$LATEST_LOG_FILE" ] && [ "$LATEST_LOG_FILE" != "$PREV_LATEST" ]; then
        break
    fi
    sleep 0.1
done

# Fallback nếu không tìm thấy log file mới hoặc chưa tạo kịp
if [ -z "$LATEST_LOG_FILE" ]; then
    LATEST_LOG_FILE=$(ls -t "$AUTO_TEST_LOGS"/*.log 2>/dev/null | head -n 1)
fi

echo ""
echo "======================================================="
echo "📋 THÔNG TIN TIỆN ÍCH"
echo "======================================================="
echo "  Xem log realtime:  tail -f $CI_LOG"
if [ -n "$LATEST_LOG_FILE" ]; then
    echo "  Xem log test:      tail -f $LATEST_LOG_FILE"
else
    echo "  Xem log test:      tail -f $AUTO_TEST_LOGS/*.log"
fi
echo "  Kill CI Monitor:   pkill -f ${MONITOR_NAME}.py"
echo "======================================================="
