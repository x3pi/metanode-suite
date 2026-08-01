#!/bin/bash

# Mảng chứa danh sách các thư mục test
TESTS=(
    "1-update-same-contract"
    "2-read-write"
    "3-amm-dex"
    "4-abort"
    "5-gas"
    "6-native-many-to-one"
    "7-native-one-to-many"
    "8-native-mixed-evm"
    "9-cross-contract-call"
    "10-cross-contract-payable"
    "11-double-spending-same-nonce"
    "12-insufficient-balance-parallel"
    "13-deploy-and-call-same-block"
    "14-selfdestruct-conflict"
    "15-xapian-shared-update"
    "16-xapian-evm-contract"
    "17-xapian-parallel-read-write"
    "18-update-different-variables"
    "19-xapian-parallel-update"
    "20-xapian-read-after-write-same-block"
    "21-sequential-nonce-same-wallet"
    "22-block-timestamp"
    "23-contract-creator-info"
    "24-contract-factory-info"
)

TOTAL_TESTS=${#TESTS[@]}
SUCCESS=0
FAILED=0

# Lưu trữ lý do lỗi cho từng bài test
declare -A REASONS

# Thư mục lưu log chi tiết
LOG_DIR="test_logs"
mkdir -p "$LOG_DIR"



echo "🚀 BẮT ĐẦU CHẠY TỔNG HỢP $TOTAL_TESTS BÀI TEST BLOCK-STM..."
echo "=========================================================="

for test_dir in "${TESTS[@]}"; do
    if [ ! -d "$test_dir" ]; then
        echo "⚠️  Bỏ qua $test_dir (Thư mục không tồn tại)"
        continue
    fi

    echo -n "▶️ Đang chạy $test_dir ... "
    
    cd "$test_dir" || exit
    
    # Chạy lệnh go run và lưu toàn bộ output vào file log
    log_file="../$LOG_DIR/${test_dir}.log"
    output=$(go run main.go 2>&1)
    exit_code=$?
    
    echo "$output" > "$log_file"
    
    cd ..

    # Phân tích kết quả dựa trên output và exit code
    
    # Kiểm tra các từ khóa báo lỗi trong code của chính các bài test
    if echo "$output" | grep -q "TEST FAILED"; then
        FAILED=$((FAILED + 1))
        REASONS["$test_dir"]="Kết quả tính toán sai hoặc Lỗi Block-STM (Phát hiện cờ TEST FAILED)"
        echo "❌ THẤT BẠI (Sai Logic)"
        continue
    fi

    if [ $exit_code -ne 0 ]; then
        FAILED=$((FAILED + 1))
        REASONS["$test_dir"]="Lỗi biên dịch hoặc chương trình Crash (Exit code: $exit_code)"
        echo "❌ THẤT BẠI (Crash)"
        continue
    fi

    # Kiểm tra trường hợp đặc biệt bài 1: Lỗi Block-STM chưa apply state
    if [ "$test_dir" == "1-update-same-contract" ] && echo "$output" | grep -q "Giá trị count cuối cùng: 1$"; then
        FAILED=$((FAILED + 1))
        REASONS["$test_dir"]="Lỗi Block-STM Sequential Merge (Kỳ vọng count=10 nhưng ra 1)"
        echo "❌ THẤT BẠI (Lỗi Block-STM Bug)"
        continue
    fi

    # Kiểm tra các bài test bị kẹt giao dịch hoặc có lỗi kết nối
    if echo "$output" | grep -q "lỗi send tx" || echo "$output" | grep -q "Lỗi kết nối"; then
        FAILED=$((FAILED + 1))
        REASONS["$test_dir"]="Gặp lỗi khi gửi Transaction hoặc lỗi RPC"
        echo "❌ THẤT BẠI (Lỗi Gửi Tx)"
        continue
    fi



    # Các bài test khác: Nếu xuất hiện dấu ❌ thì coi như lỗi (giao dịch bị revert ngoài ý muốn)
    # Loại trừ các bài test cố ý gây revert: 4-abort, 11-double-spending-same-nonce, 12-insufficient-balance-parallel, 14-selfdestruct-conflict
    if [[ "$test_dir" != "4-abort" && "$test_dir" != "11-double-spending-same-nonce" && "$test_dir" != "12-insufficient-balance-parallel" && "$test_dir" != "14-selfdestruct-conflict" ]] && echo "$output" | grep -q "❌"; then
        FAILED=$((FAILED + 1))
        REASONS["$test_dir"]="Có giao dịch bị Revert hoặc FAILED ngoài ý muốn"
        echo "❌ THẤT BẠI (Lỗi Tx Revert)"
        continue
    fi

    # Nếu qua hết các lưới lọc trên thì là thành công
    SUCCESS=$((SUCCESS + 1))
    echo "✅ THÀNH CÔNG"

done

echo ""
echo "=========================================================="
echo "📊 BÁO CÁO THỐNG KÊ BLOCK-STM TEST SUITE"
echo "=========================================================="
echo "🔹 Tổng số bài test : $TOTAL_TESTS"
echo "🔹 Thành công       : $SUCCESS"
echo "🔹 Thất bại         : $FAILED"

if [ $FAILED -gt 0 ]; then
    echo "----------------------------------------------------------"
    echo "🚨 CHI TIẾT CÁC BÀI THẤT BẠI:"
    for test_dir in "${TESTS[@]}"; do
        if [[ -n "${REASONS[$test_dir]}" ]]; then
            echo "  ❌ [$test_dir]: ${REASONS[$test_dir]}"
            echo "     (Đọc log chi tiết tại: $LOG_DIR/${test_dir}.log)"
        fi
    done
fi
echo "=========================================================="
