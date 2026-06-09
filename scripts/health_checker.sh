#!/bin/bash
set -euo pipefail

# ─────────────────────────────────────────────────────────────────────
# STANDALONE NODE HEALTH CHECKER FOR METANODE PIPELINE
# ─────────────────────────────────────────────────────────────────────
# Runs in the background and monitors node health via HTTP curl.
# Contains a self-termination check: if parent process dies, this exits.
# ─────────────────────────────────────────────────────────────────────

PARENT_PID=""
DEPLOY_MODE="single"
RPC_JSON_PATH="/tmp/rpc_nodes.json"

usage() {
    echo "Usage: $0 --parent-pid <pid> [--mode single|multi] [--rpc-json <path>]"
    exit 1
}

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --parent-pid)
      PARENT_PID="$2"
      shift 2
      ;;
    --mode)
      DEPLOY_MODE="$2"
      shift 2
      ;;
    --rpc-json)
      RPC_JSON_PATH="$2"
      shift 2
      ;;
    *)
      echo "❌ Unknown argument: $1"
      usage
      ;;
  esac
done

if [ -z "$PARENT_PID" ]; then
    echo "❌ Missing required parameter --parent-pid"
    usage
fi

echo "🟢 Health Checker started for parent PID $PARENT_PID (mode: $DEPLOY_MODE)"

while true; do
    sleep 10
    
    # Self-termination check: if parent is dead, exit immediately to prevent orphaning
    if ! kill -0 "$PARENT_PID" 2>/dev/null; then
        echo "ℹ️ Parent process $PARENT_PID has exited. Health checker exiting cleanly."
        exit 0
    fi
    
    if [ "$DEPLOY_MODE" == "multi" ]; then
        if [ -f "$RPC_JSON_PATH" ]; then
            # Read unique RPC URLs from generated JSON config
            urls=$(grep -oE 'http://[^"]+' "$RPC_JSON_PATH" 2>/dev/null || true)
            for url in $urls; do
                if ! curl -s -m 2 "$url" >/dev/null 2>&1; then
                    echo -e "\n\n🚨🚨🚨 PHÁT HIỆN NODE CHẾT TẠI $url! ĐANG TIẾN HÀNH DỪNG AUTO TEST PIPELINE! 🚨🚨🚨\n\n"
                    echo "Node HTTP Server ($url) không phản hồi. Có thể process đã bị crash!" > /tmp/MTN_CHAIN_ERROR_STOP
                    kill -TERM "$PARENT_PID" 2>/dev/null || true
                    exit 1
                fi
            done
        fi
    else
        # Single mode (local mode check of fixed ports)
        PORTS=(8757 10747 10749 10750 10748)
        for port in "${PORTS[@]}"; do
            if ! curl -s -m 2 "http://127.0.0.1:$port" >/dev/null 2>&1; then
                echo -e "\n\n🚨🚨🚨 PHÁT HIỆN NODE CHẾT TẠI CỔNG $port! ĐANG TIẾN HÀNH DỪNG AUTO TEST PIPELINE! 🚨🚨🚨\n\n"
                echo "Node HTTP Server (cổng $port) không phản hồi. Có thể process đã bị crash!" > /tmp/MTN_CHAIN_ERROR_STOP
                kill -TERM "$PARENT_PID" 2>/dev/null || true
                exit 1
            fi
        done
    fi
done
