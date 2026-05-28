#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Đánh dấu reset lỗi
rm -f /tmp/MTN_CHAIN_ERROR_STOP

echo "======================================================="
echo "🚀 BƯỚC 1: KHỞI TẠO CỤM VÀ TEST CƠ BẢN (simple_test.sh)"
echo "======================================================="
cd "$SCRIPT_DIR"
./simple_test.sh

echo "======================================================="
echo "🔥 BƯỚC 2: BẮT ĐẦU SPAM XAPIAN LIÊN TỤC (go run trực tiếp)"
echo "======================================================="
SPAM_DIR="$SCRIPT_DIR/../test-simple/test-rpc/spam_xapian"

# Gọi go run trực tiếp, bỏ file trung gian run_spam.sh
# Exit code từ Go sẽ được propagate đúng lên ci_spam_xapian_monitor.py
go run "$SPAM_DIR/main.go" \
    -config="$SPAM_DIR/config-local.json" \
    -deploy-json="$SPAM_DIR/../test_read_wire_xapian/data-xapian-v2.json" \
    -method="runStep1_Setup" \
    -wallets=1000 || {
    if [ ! -s /tmp/MTN_CHAIN_ERROR_STOP ]; then
        echo "Lỗi Spam Xapian!" > /tmp/MTN_CHAIN_ERROR_STOP
    fi
    echo -e "\n======================================================="
    cat /tmp/MTN_CHAIN_ERROR_STOP
    echo -e "=======================================================\n"
    exit 1
}
