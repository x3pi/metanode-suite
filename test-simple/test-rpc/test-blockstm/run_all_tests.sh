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
    "25-eip4844-blob-tx"
    "26-eip7702-setcode-tx"
    "27-eip4844-edge-cases"
    "28-eip7702-delegated-execution-and-revocation"
    "29-blockhash-opcode-verifier"
)

TOTAL_TEST_CASES=${#TESTS[@]}
TOTAL_RUNS=$((TOTAL_TEST_CASES * 3))
SUCCESS=0
FAILED=0

# Lưu trữ lý do lỗi cho từng bài test
declare -A REASONS

# Thư mục lưu log chi tiết
LOG_DIR="test_logs"
mkdir -p "$LOG_DIR"



echo "🚀 BẮT ĐẦU CHẠY TỔNG HỢP $TOTAL_TEST_CASES BÀI TEST ($TOTAL_RUNS LƯỢT CHẠY) BLOCK-STM..."
echo "=========================================================="

for test_dir in "${TESTS[@]}"; do
    if [ ! -d "$test_dir" ]; then
        echo "⚠️  Bỏ qua $test_dir (Thư mục không tồn tại)"
        continue
    fi

    for run_idx in {1..3}; do
        echo -n "▶️ Đang chạy $test_dir (Lần $run_idx/3) ... "
        
        cd "$test_dir" || exit
        
        # Chạy lệnh go run và lưu output realtime
        log_file="../$LOG_DIR/${test_dir}_run${run_idx}.log"
        go run main.go 2>&1 | tee "$log_file"
        exit_code=${PIPESTATUS[0]}
        
        output=$(cat "$log_file")
        
        cd ..

        # Phân tích kết quả dựa trên output và exit code
        
        # Kiểm tra các từ khóa báo lỗi trong code của chính các bài test
        if echo "$output" | grep -q "TEST FAILED"; then
            FAILED=$((FAILED + 1))
            REASONS["${test_dir}_run${run_idx}"]="Kết quả tính toán sai hoặc Lỗi Block-STM (Phát hiện cờ TEST FAILED)"
            echo "❌ THẤT BẠI (Sai Logic)"
            break 2
        fi

        if [ $exit_code -ne 0 ]; then
            FAILED=$((FAILED + 1))
            REASONS["${test_dir}_run${run_idx}"]="Lỗi biên dịch hoặc chương trình Crash (Exit code: $exit_code)"
            echo "❌ THẤT BẠI (Crash)"
            break 2
        fi

        # Kiểm tra trường hợp đặc biệt bài 1: Lỗi Block-STM chưa apply state
        if [ "$test_dir" == "1-update-same-contract" ] && echo "$output" | grep -q "Giá trị count cuối cùng: 1$"; then
            FAILED=$((FAILED + 1))
            REASONS["${test_dir}_run${run_idx}"]="Lỗi Block-STM Sequential Merge (Kỳ vọng count=10 nhưng ra 1)"
            echo "❌ THẤT BẠI (Lỗi Block-STM Bug)"
            break 2
        fi

        # Kiểm tra các bài test bị kẹt giao dịch hoặc có lỗi kết nối
        if echo "$output" | grep -q "lỗi send tx" || echo "$output" | grep -q "Lỗi kết nối" || echo "$output" | grep -q "timeout waiting for receipt"; then
            FAILED=$((FAILED + 1))
            REASONS["${test_dir}_run${run_idx}"]="Gặp lỗi khi gửi Transaction, lỗi RPC hoặc Timeout"
            echo "❌ THẤT BẠI (Lỗi Gửi Tx / Timeout)"
            break 2
        fi

        # Các bài test khác: Nếu xuất hiện dấu ❌ thì coi như lỗi (giao dịch bị revert ngoài ý muốn)
        # Loại trừ các bài test cố ý gây revert: 4-abort, 11-double-spending-same-nonce, 12-insufficient-balance-parallel, 14-selfdestruct-conflict
        if [[ "$test_dir" != "4-abort" && "$test_dir" != "11-double-spending-same-nonce" && "$test_dir" != "12-insufficient-balance-parallel" && "$test_dir" != "14-selfdestruct-conflict" ]] && echo "$output" | grep -q "❌"; then
            FAILED=$((FAILED + 1))
            REASONS["${test_dir}_run${run_idx}"]="Có giao dịch bị Revert hoặc FAILED ngoài ý muốn"
            echo "❌ THẤT BẠI (Lỗi Tx Revert)"
            break 2
        fi

        # Nếu qua hết các lưới lọc trên thì là thành công
        SUCCESS=$((SUCCESS + 1))
        echo "✅ THÀNH CÔNG"
    done
done

echo ""
echo "=========================================================="
echo "📊 BÁO CÁO THỐNG KÊ BLOCK-STM TEST SUITE"
echo "=========================================================="
echo "🔹 Tổng số bài test : $TOTAL_TEST_CASES"
echo "🔹 Tổng số lượt chạy: $TOTAL_RUNS"
echo "🔹 Thành công       : $SUCCESS"
echo "🔹 Thất bại         : $FAILED"

if [ $FAILED -gt 0 ]; then
    echo "----------------------------------------------------------"
    echo "🚨 CHI TIẾT CÁC BÀI THẤT BẠI:"
    for key in "${!REASONS[@]}"; do
        echo "  ❌ [$key]: ${REASONS[$key]}"
        echo "     (Đọc log chi tiết tại: $LOG_DIR/${key}.log)"
    done
fi
echo "=========================================================="

# Xuất báo cáo ra file Markdown
REPORT_FILE="test_report.md"
echo "# 📊 Báo Cáo Kết Quả Test Block-STM" > "$REPORT_FILE"
echo "**Thời gian chạy:** $(date '+%Y-%m-%d %H:%M:%S')" >> "$REPORT_FILE"
echo "" >> "$REPORT_FILE"
echo "## 📈 Thống Kê Chung" >> "$REPORT_FILE"
echo "- **Tổng số bài test**: $TOTAL_TEST_CASES" >> "$REPORT_FILE"
echo "- **Tổng số lượt chạy**: $TOTAL_RUNS" >> "$REPORT_FILE"
echo "- **✅ Lượt thành công**: $SUCCESS" >> "$REPORT_FILE"
echo "- **❌ Lượt thất bại**: $FAILED" >> "$REPORT_FILE"
echo "" >> "$REPORT_FILE"

if [ $FAILED -gt 0 ]; then
    echo "## 🚨 Chi Tiết Lỗi & Nguyên Nhân" >> "$REPORT_FILE"
    for key in "${!REASONS[@]}"; do
        reason_text="${REASONS[$key]}"
        echo "### ❌ Bài Test Thất Bại: \`$key\`" >> "$REPORT_FILE"
        echo "- **Hiện tượng / Lỗi thực tế (Actual)**: $reason_text" >> "$REPORT_FILE"
        
        # Bổ sung giải thích nguyên nhân và kỳ vọng
        if echo "$reason_text" | grep -q "Crash"; then
            echo "- **Nguyên nhân dự đoán**: Core Node bị crash hoặc Panic trong quá trình thực thi Block-STM (panic ở chan, bộ nhớ, vv.)." >> "$REPORT_FILE"
        elif echo "$reason_text" | grep -q "Timeout"; then
            echo "- **Nguyên nhân dự đoán**: Mạng bị kẹt, giao dịch không được mine, hoặc mempool không gửi giao dịch đi được, gây ra timeout khi chờ receipt." >> "$REPORT_FILE"
        elif echo "$reason_text" | grep -q "Revert"; then
            echo "- **Nguyên nhân dự đoán**: EVM revert giao dịch do lỗi logic trên Smart Contract hoặc thiếu Gas, out of balance." >> "$REPORT_FILE"
        elif echo "$reason_text" | grep -q "Sai Logic"; then
            echo "- **Nguyên nhân dự đoán**: Read/Write Conflict không được Block-STM xử lý đúng, hoặc dữ liệu state (counter, Xapian doc) sau khi block chạy xong không khớp với kết quả tuần tự." >> "$REPORT_FILE"
        fi
        
        echo "- **Kết quả kỳ vọng (Expected)**: Node xử lý mượt mà không crash, giao dịch được confirm thành công trên các node và giá trị State cập nhật chính xác tuyệt đối." >> "$REPORT_FILE"
        echo "- **File Log Chi Tiết**: [\`${key}.log\`](./$LOG_DIR/${key}.log)" >> "$REPORT_FILE"
        echo "" >> "$REPORT_FILE"
    done
    echo "💾 Đã tự động xuất báo cáo chi tiết ra file: $REPORT_FILE"
fi

