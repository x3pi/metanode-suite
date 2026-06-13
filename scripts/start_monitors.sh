#!/bin/bash
# start_monitors.sh
# Script to manage background health and block hash monitors

# Configuration
TELEGRAM_BOT_TOKEN="8230176859:AAGoZ_78xzb1q4rgJJ5SYLxRhZBYBTSz_xo"
TELEGRAM_CHAT_ID="-1003867050625"
RPC_JSON_PATH="/tmp/rpc_nodes.json"

send_tele() {
    curl -s -X POST "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage" \
        -d chat_id="${TELEGRAM_CHAT_ID}" \
        -d text="$1" >/dev/null
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
                        
                        echo "Đang fetch log lỗi mới nhất của node ${node_id} từ $ip..."
                        latest_log=$(sshpass -p "$ssh_pass" ssh -o StrictHostKeyChecking=no "$ssh_user@$ip" "tail -n 30 /opt/metanode/node-${node_id}/logs/App.log 2>/dev/null || journalctl -u metanode-execution-${node_id} -n 30 --no-pager" || echo "Không thể lấy log từ server")

                        send_tele "🚨🚨🚨 [Health Monitor] PHÁT HIỆN NODE CHẾT!
Node: $node_key
IP: $ip
URL: $node_url

📄 LOG MỚI NHẤT:
\`\`\`
$latest_log
\`\`\`"
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
