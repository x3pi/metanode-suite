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

cd "$SPAM_DIR"

# Gọi go run trực tiếp từ thư mục của nó
# Điều này đảm bảo tất cả đường dẫn tương đối (-keys, -deploy-json, -abi) hoạt động chính xác
go run main.go \
    -config="config-local.json" \
    -deploy-json="../test_read_wire_xapian/data-xapian-v2.json" \
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
