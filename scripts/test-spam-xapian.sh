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

# Bật giám sát lệch hash ngầm trong suốt quá trình spam
echo "📌 BẬT GIÁM SÁT LỆCH HASH NGẦM (block_hash_checker) CHO QUÁ TRÌNH SPAM..."
(
    cd "$SCRIPT_DIR/../block/block_hash_checker"
    go build -o block_hash_checker_bin main.go
    ./block_hash_checker_bin --watch --interval 5s > block_hash_checker_spam.log 2>&1
) &
CHECKER_PID=$!

# Trap dọn dẹp background process khi test-spam-xapian.sh dừng
_spam_test_cleanup() {
    local _exit_code=$?
    kill -9 $CHECKER_PID 2>/dev/null || true
    exit $_exit_code
}
trap _spam_test_cleanup EXIT

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
