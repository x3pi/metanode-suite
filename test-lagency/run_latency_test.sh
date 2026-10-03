#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$DIR"

# Mặc định: chain=parent, proto=all, rounds=10
CHAIN="parent"
PROTO="all"
ROUNDS="10"

# Parse arguments nếu có
while [[ $# -gt 0 ]]; do
  case $1 in
    --chain|-c)
      CHAIN="$2"
      shift 2
      ;;
    --proto|-p)
      PROTO="$2"
      shift 2
      ;;
    --rounds|-r)
      ROUNDS="$2"
      shift 2
      ;;
    --help|-h)
      echo "Cách dùng:"
      echo "  ./run_latency_test.sh [options]"
      echo ""
      echo "Tùy chọn:"
      echo "  --chain, -c   : 'parent' (public) hoặc child chain ('chain_a', 'chain_b') (mặc định: parent)"
      echo "  --proto, -p   : 'all', 'rpc', hoặc 'tcp' (mặc định: all)"
      echo "  --rounds, -r  : Số round ping-pong đo độ trễ (mặc định: 10)"
      echo ""
      echo "Ví dụ:"
      echo "  ./run_latency_test.sh --chain parent --proto all"
      echo "  ./run_latency_test.sh --chain chain_a --proto all"
      echo "  ./run_latency_test.sh --chain parent --proto rpc --rounds 20"
      exit 0
      ;;
    *)
      # Fallback positional arguments
      if [ -z "$CHAIN_SET" ]; then
        CHAIN="$1"
        CHAIN_SET=1
      elif [ -z "$PROTO_SET" ]; then
        PROTO="$1"
        PROTO_SET=1
      elif [ -z "$ROUNDS_SET" ]; then
        ROUNDS="$1"
        ROUNDS_SET=1
      fi
      shift
      ;;
  esac
done

echo "=========================================================="
echo "🚀 Chạy Đo Độ Trễ Metanode: Chain=$CHAIN | Proto=$PROTO | Rounds=$ROUNDS"
echo "=========================================================="

go run main.go --chain "$CHAIN" --proto "$PROTO" --rounds "$ROUNDS"
