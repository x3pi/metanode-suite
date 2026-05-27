#!/bin/bash
# ══════════════════════════════════════════════════════════════════
#  spam_counter/run.sh
#  1. Biên dịch Solidity → lấy bytecode
#  2. Build Go binary
#  3. Chạy vô hạn: increment → getCount → verify tuần tự
#  Nhấn Ctrl+C để dừng
# ══════════════════════════════════════════════════════════════════
set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../../" && pwd)"  # metanode-suite/
SOLC_NODE_PATH="/home/abc/nhat/consensus-chain/mtn-simple-2025/cmd/tool/tool-test-chain/node_modules"

# ── 1. Biên dịch Solidity ───────────────────────────────────────
echo "📦 Biên dịch TestCounter.sol..."
NODE_PATH="$SOLC_NODE_PATH" node \
  "$REPO_ROOT/test-simple/contract/build/compile.js" \
  "$REPO_ROOT/test-simple/contract/test-counter.sol" \
  "$SCRIPT_DIR" 2>&1

BIN_FILE="$SCRIPT_DIR/test-counter_TestCounter.bin"
if [ ! -f "$BIN_FILE" ]; then
  echo "❌ Không tìm thấy $BIN_FILE sau khi biên dịch!"
  exit 1
fi
# Counter binary cần tên chuẩn
cp "$BIN_FILE" "$SCRIPT_DIR/TestCounter.bin"
echo "✅ Bytecode OK ($(wc -c < "$SCRIPT_DIR/TestCounter.bin") chars)"

# ── 2. Build Go binary ──────────────────────────────────────────
echo "⚡ Build Go binary..."
cd "$REPO_ROOT"
go build -o "$SCRIPT_DIR/counter_test" ./test-simple/test-rpc/spam_counter/
echo "✅ Binary: $SCRIPT_DIR/counter_test"

# ── 3. Chạy test ───────────────────────────────────────────────
CONFIG="${1:-$SCRIPT_DIR/../config-local.json}"
echo "🔗 Config: $CONFIG"
echo ""
cd "$SCRIPT_DIR"
exec "./counter_test" "$CONFIG" "$SCRIPT_DIR/TestCounter.bin"
