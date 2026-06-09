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

# ─── Startup Grace Period (120 seconds) ──────────────────────────────
# We wait 120s to allow remote execution layers to completely import genesis blocks (e.g. 50k accounts).
# We check parent process status every 10s to exit early if parent is killed.
echo "⏳ Waiting 120 seconds for nodes to initialize completely (genesis loading)..."
for ((i=1; i<=12; i++)); do
    sleep 10
    if ! kill -0 "$PARENT_PID" 2>/dev/null; then
        echo "ℹ️ Parent process $PARENT_PID has exited during startup grace period. Health checker exiting cleanly."
        exit 0
    fi
done

echo "🟢 Startup grace period completed. Starting active node health monitoring..."

while true; do
    sleep 10
    
    # Self-termination check: if parent is dead, exit immediately to prevent orphaning
    if ! kill -0 "$PARENT_PID" 2>/dev/null; then
        echo "ℹ️ Parent process $PARENT_PID has exited. Health checker exiting cleanly."
        exit 0
    fi
    
    if [ "$DEPLOY_MODE" == "multi" ]; then
        if [ -f "$RPC_JSON_PATH" ]; then
            while read -r node_key node_url; do
                local node_id="${node_key#m}"
                local is_excluded=false
                if [ -f /tmp/MTN_EXCLUDE_NODES ]; then
                    if grep -qE "(^|,)${node_id}(,|$)" /tmp/MTN_EXCLUDE_NODES; then
                        is_excluded=true
                    fi
                fi
                if [ -n "${MTN_EXCLUDE_NODES:-}" ]; then
                    if echo "$MTN_EXCLUDE_NODES" | grep -qE "(^|,)${node_id}(,|$)" ; then
                        is_excluded=true
                    fi
                fi

                if [ "$is_excluded" = false ]; then
                    if ! curl -s -m 2 "$node_url" >/dev/null 2>&1; then
                        echo -e "\n\n🚨🚨🚨 PHÁT HIỆN NODE CHẾT TẠI $node_url ($node_key)! ĐANG TIẾN HÀNH DỪNG AUTO TEST PIPELINE! 🚨🚨🚨\n\n"
                        echo "Node HTTP Server ($node_url) không phản hồi. Có thể process đã bị crash!" > /tmp/MTN_CHAIN_ERROR_STOP
                        kill -TERM "$PARENT_PID" 2>/dev/null || true
                        exit 1
                    fi
                fi
            done < <(jq -r '.nodes | to_entries[] | "\(.key) \(.value)"' "$RPC_JSON_PATH" 2>/dev/null || true)
        fi
    else
        # Single mode (local mode check of fixed ports)
        PORTS=(8757 10747 10749 10750 10748)
        for node_id in 0 1 2 3 4; do
            local is_excluded=false
            if [ -f /tmp/MTN_EXCLUDE_NODES ]; then
                if grep -qE "(^|,)${node_id}(,|$)" /tmp/MTN_EXCLUDE_NODES; then
                    is_excluded=true
                fi
            fi
            if [ -n "${MTN_EXCLUDE_NODES:-}" ]; then
                if echo "$MTN_EXCLUDE_NODES" | grep -qE "(^|,)${node_id}(,|$)" ; then
                    is_excluded=true
                fi
            fi

            if [ "$is_excluded" = false ]; then
                local port="${PORTS[$node_id]}"
                if ! curl -s -m 2 "http://127.0.0.1:$port" >/dev/null 2>&1; then
                    echo -e "\n\n🚨🚨🚨 PHÁT HIỆN NODE CHẾT TẠI CỔNG $port! ĐANG TIẾN HÀNH DỪNG AUTO TEST PIPELINE! 🚨🚨🚨\n\n"
                    echo "Node HTTP Server (cổng $port) không phản hồi. Có thể process đã bị crash!" > /tmp/MTN_CHAIN_ERROR_STOP
                    kill -TERM "$PARENT_PID" 2>/dev/null || true
                    exit 1
                fi
            fi
        done
    fi
done
