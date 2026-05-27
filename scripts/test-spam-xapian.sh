#!/bin/bash
set -e

# Đánh dấu reset lỗi
rm -f /tmp/MTN_CHAIN_ERROR_STOP

echo "======================================================="
echo "🚀 BƯỚC 1: KHỞI TẠO CỤM VÀ TEST CƠ BẢN (simple_test.sh)"
echo "======================================================="
cd /home/abc/nhat/con-chain-v2/metanode-suite/scripts
./simple_test.sh

echo "======================================================="
echo "🔥 BƯỚC 2: BẮT ĐẦU SPAM XAPIAN LIÊN TỤC (run_spam.sh)"
echo "======================================================="
cd /home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/spam_xapian

# Hàm bash này sẽ bắt lại mã lỗi để ci_spam_xapian_monitor.py đọc
# Tool spam được thiết kế chạy liên tục, nếu crash sẽ văng lỗi ra
./run_spam.sh deploy runStep1_Setup 1000 || {
    echo "Lỗi bất thường xảy ra trong quá trình chạy Spam Xapian!" > /tmp/MTN_CHAIN_ERROR_STOP
    exit 1
}
