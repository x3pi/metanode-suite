#!/bin/bash
# start_monitors.sh
# Script to manage background health and block hash monitors

# Configuration
TELEGRAM_BOT_TOKEN="${TELEGRAM_BOT_TOKEN:-""}"
TELEGRAM_CHAT_ID="-1003867050625"
RPC_JSON_PATH="/tmp/rpc_nodes.json"

send_tele() {
    curl -s -X POST "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage" \
        -d chat_id="${TELEGRAM_CHAT_ID}" \
        --data-urlencode text="$1" >/dev/null
}

# If arg is "health", run the health loop (Background worker)
if [ "${1:-}" == "health" ]; then
    echo "Starting health monitor loop..."
    declare -A dead_nodes
    while true; do
        if [ -f "$RPC_JSON_PATH" ]; then
            while read -r node_key node_url; do
                if ! curl -s -m 2 "$node_url" >/dev/null 2>&1; then
                    if [ "${dead_nodes[$node_key]:-0}" == "0" ]; then
                        dead_nodes[$node_key]=1
                        ip=$(echo "$node_url" | awk -F/ '{print $3}' | awk -F: '{print $1}')
                        node_id=${node_key#m}
                        ssh_user="abc"
                        ssh_pass="1234@abcd"
                        
                        crash_time=$(date +%Y%m%d_%H%M%S)
                        crash_dir="/home/abc/nhat/con-chain-v2/metanode-suite/scripts/logs_crash/node_${node_id}_crash_${crash_time}"
                        
                        # Tự động kéo log từ node sập về máy monitor (234)
                        mkdir -p /home/abc/nhat/con-chain-v2/metanode-suite/scripts/logs_crash
                        sshpass -p "$ssh_pass" scp -o StrictHostKeyChecking=no -r "$ssh_user@$ip:/opt/metanode/node-$node_id/logs" "$crash_dir" >/dev/null 2>&1 || true

                        # Xóa các thư mục cũ, chỉ giữ lại 4 thư mục mới nhất
                        ls -dt /home/abc/nhat/con-chain-v2/metanode-suite/scripts/logs_crash/* 2>/dev/null | tail -n +5 | xargs rm -rf

                        send_tele "🚨🚨🚨 [Health Monitor] PHÁT HIỆN NODE CHẾT!
Node: $node_key
IP: $ip
URL: $node_url

🛠 Lệnh kéo thư mục Logs về máy tính của bạn:
sshpass -p \"1234@abcd\" scp -r abc@192.168.1.234:$crash_dir ./node_${node_id}_crash_${crash_time}"
                    fi
                else
                    if [ "${dead_nodes[$node_key]:-0}" == "1" ]; then
                        dead_nodes[$node_key]=0
                        send_tele "🟢 [Health Monitor] Node $node_key ($node_url) đã hoạt động lại!"
                    fi
                fi
            done < <(jq -r '.nodes | to_entries[] | "\(.key) \(.value)"' "$RPC_JSON_PATH" 2>/dev/null || true)
        fi
        sleep 10
    done
    exit 0
fi

echo "🔄 Đang khởi động lại các tiến trình giám sát (Monitors)..."

# 1. Kill old processes
pkill -f "go run main.go.*--no-stop-flag"
pkill -f "start_monitors.sh health"

# 2. Start Health Monitor in background
nohup "$0" health > /dev/null 2>&1 &
echo "✅ Đã bật Health Monitor (kiểm tra node sống/chết)"

# 3. Start Block Hash Checker in background
BLOCK_CHECKER_DIR="$(dirname "$0")/../block/block_hash_checker"
if [ -d "$BLOCK_CHECKER_DIR" ]; then
    cd "$BLOCK_CHECKER_DIR" || exit 1
    nohup go run main.go --watch --interval 5s --config config-m-nodes.json --no-stop-flag > block_checker_daemon.log 2>&1 &
    echo "✅ Đã bật Block Hash Monitor (kiểm tra lệch hash)"
else
    echo "⚠️ Không tìm thấy thư mục block_hash_checker"
fi

echo "🎉 Hoàn tất khởi động các Monitors ngầm!"
