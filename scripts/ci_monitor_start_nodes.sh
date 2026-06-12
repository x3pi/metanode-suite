#!/bin/bash

# Script to manage ci_monitor_start_nodes.py
# Usage: ./ci_monitor_start_nodes.sh [start|stop|status|logs]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PYTHON_SCRIPT="ci_monitor_start_nodes.py"
PID_FILE="$SCRIPT_DIR/.ci_monitor.pid"
LOG_FILE="$SCRIPT_DIR/ci_monitor.log"

cmd_start() {
    if [ -f "$PID_FILE" ] && kill -0 $(cat "$PID_FILE") 2>/dev/null; then
        echo "⚠️ Monitor is already running with PID $(cat "$PID_FILE")"
        return
    fi
    
    echo "🚀 Starting CI Monitor..."
    nohup python3 "$SCRIPT_DIR/$PYTHON_SCRIPT" > "$LOG_FILE" 2>&1 &
    PID=$!
    echo $PID > "$PID_FILE"
    echo "✅ Started successfully. PID: $PID"
    echo "📄 Logs are being written to $LOG_FILE"
}

cmd_stop() {
    if [ -f "$PID_FILE" ] && kill -0 $(cat "$PID_FILE") 2>/dev/null; then
        PID=$(cat "$PID_FILE")
        echo "🛑 Stopping CI Monitor (PID: $PID)..."
        kill -15 $PID
        rm -f "$PID_FILE"
        echo "✅ Stopped."
    else
        # Fallback to pkill if pid file is missing/stale
        if pgrep -f "$PYTHON_SCRIPT" > /dev/null; then
            echo "🛑 Stopping CI Monitor using pkill..."
            pkill -15 -f "$PYTHON_SCRIPT"
            echo "✅ Stopped."
        else
            echo "ℹ️ Monitor is not running."
        fi
        rm -f "$PID_FILE"
    fi
    
    echo "🧹 Dọn dẹp các tiến trình phụ (Monitors)..."
    pkill -f "start_monitors.sh health" 2>/dev/null || true
    pkill -f "go run main.go.*--no-stop-flag" 2>/dev/null || true
    echo "✅ Toàn bộ Monitor đã được tắt hoàn toàn."
}

cmd_status() {
    if [ -f "$PID_FILE" ] && kill -0 $(cat "$PID_FILE") 2>/dev/null; then
        echo "🟢 CI Monitor is RUNNING (PID: $(cat "$PID_FILE"))"
    elif pgrep -f "$PYTHON_SCRIPT" > /dev/null; then
        echo "🟢 CI Monitor is RUNNING (PID: $(pgrep -f "$PYTHON_SCRIPT" | head -n 1))"
    else
        echo "🔴 CI Monitor is STOPPED"
    fi
}

cmd_logs() {
    if [ -f "$LOG_FILE" ]; then
        echo "👀 Tailing logs (press Ctrl+C to exit)..."
        tail -f "$LOG_FILE"
    else
        echo "⚠️ Log file $LOG_FILE not found."
    fi
}

case "${1:-}" in
    start)  cmd_start ;;
    stop)   cmd_stop ;;
    status) cmd_status ;;
    logs)   cmd_logs ;;
    *)
        echo "Usage: $0 {start|stop|status|logs}"
        exit 1
        ;;
esac
