#!/bin/bash
# auto_debug_loop.sh
# Script to run ci_monitor.sh, monitor its status, and exit with an error details when a failure occurs.

SCRIPT_DIR="/home/abc/nhat/consensus-chain/metanode-suite/scripts"
cd "$SCRIPT_DIR"

echo "=== BẮT ĐẦU CHU KỲ AUTO DEBUG LOOP ==="

# Clean old error triggers
rm -f "$SCRIPT_DIR/error_report.txt"
rm -f /tmp/MTN_CHAIN_ERROR_STOP

# Run the test pipeline
./ci_monitor.sh --type spam --batch 300 --no-listen --clean-logs

# Wait for initiation
sleep 10

# Monitor the test process
while true; do
    MONITOR_PID=$(pgrep -f "ci_monitor.py" || true)
    
    if [ -z "$MONITOR_PID" ]; then
        echo "⚠️ ci_monitor.py has stopped!"
        break
    fi
    
    if [ -f "$SCRIPT_DIR/error_report.txt" ] || [ -f /tmp/MTN_CHAIN_ERROR_STOP ]; then
        echo "🚨 Failure sentinel or report file detected!"
        break
    fi
    
    sleep 10
done

# Wait for logs to be flushed
sleep 5

# Check if there is an error
HAS_ERROR=false
if [ -f "$SCRIPT_DIR/error_report.txt" ]; then
    HAS_ERROR=true
elif [ -f /tmp/MTN_CHAIN_ERROR_STOP ]; then
    HAS_ERROR=true
elif grep -qE "Exit Code: [1-9]" "$SCRIPT_DIR/ci_monitor.log" 2>/dev/null; then
    echo "❌ Detected non-zero exit code in ci_monitor.log"
    HAS_ERROR=true
fi

if [ "$HAS_ERROR" = true ]; then
    echo "❌ FAILURE DETECTED DURING RUN!"
    
    if [ -f "$SCRIPT_DIR/error_report.txt" ]; then
        echo "=== NỘI DUNG ERROR REPORT ==="
        cat "$SCRIPT_DIR/error_report.txt"
        echo "============================="
    fi
    
    LATEST_LOG_FILE=$(ls -t "$SCRIPT_DIR/auto_test_logs"/*.log 2>/dev/null | head -n 1 || true)
    if [ -n "$LATEST_LOG_FILE" ] && [ -f "$LATEST_LOG_FILE" ]; then
        echo "=== 100 DÒNG LOG TEST CUỐI CÙNG ($LATEST_LOG_FILE) ==="
        tail -n 100 "$LATEST_LOG_FILE"
        echo "======================================================"
    fi
    
    exit 1
else
    echo "✅ Pipeline completed successfully without errors."
    exit 0
fi
