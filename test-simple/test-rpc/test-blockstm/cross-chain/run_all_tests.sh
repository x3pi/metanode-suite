#!/bin/bash

# ==============================================================================
# METANODE CROSS-CHAIN TEST RUNNER
# Chạy 3 bài test Cross-Chain (mỗi bài test chạy 3 lần).
# Dừng và báo cáo ngay lập tức nếu phát hiện bất kỳ lỗi nào.
# ==============================================================================

# Danh sách 3 bài test Cross-Chain
TESTS=(
    "01-client-only-transfer"
    "02-cross-chain-caro-game"
    "03-cross-chain-failure-refund"
)

TOTAL_TEST_CASES=${#TESTS[@]}
RUNS_PER_TEST=3
TOTAL_RUNS=$((TOTAL_TEST_CASES * RUNS_PER_TEST))
SUCCESS=0
FAILED=0

# Lưu trữ lý do lỗi
declare -A REASONS

# Thư mục lưu log chi tiết
LOG_DIR="test_logs"
mkdir -p "$LOG_DIR"

REPORT_FILE="test_report.md"

echo "=========================================================="
echo "🚀 BẮT ĐẦU CHẠY BỘ TEST CROSS-CHAIN ($TOTAL_TEST_CASES BÀI, MỖI BÀI $RUNS_PER_TEST LẦN = $TOTAL_RUNS LƯỢT CHẠY)..."
echo "=========================================================="

FAILED_TEST_KEY=""

for test_dir in "${TESTS[@]}"; do
    if [ ! -d "$test_dir" ]; then
        echo "⚠️  Bỏ qua $test_dir (Thư mục không tồn tại)"
        continue
    fi

    for run_idx in $(seq 1 $RUNS_PER_TEST); do
        test_key="${test_dir}_run${run_idx}"
        echo -n "▶️ Đang chạy $test_dir (Lần $run_idx/$RUNS_PER_TEST) ... "
        
        cd "$test_dir" || exit 1
        
        log_file="../$LOG_DIR/${test_key}.log"
        go run . > "$log_file" 2>&1
        exit_code=$?
        cat "$log_file"
        
        output=$(cat "$log_file")
        
        cd ..

        has_error=0
        reason=""

        # 1. Kiểm tra cờ TEST FAILED
        if echo "$output" | grep -q "TEST FAILED"; then
            has_error=1
            reason="Kết quả tính toán sai hoặc Lỗi Cross-Chain Logic (Phát hiện cờ TEST FAILED)"
            echo "❌ THẤT BẠI (Sai Logic)"
        # 2. Kiểm tra Exit Code khác 0
        elif [ $exit_code -ne 0 ]; then
            has_error=1
            reason="Lỗi biên dịch hoặc chương trình Crash/Panic (Exit code: $exit_code)"
            echo "❌ THẤT BẠI (Crash/Panic)"
        # 3. Kiểm tra Panic trong Go runtime
        elif echo "$output" | grep -q "panic:"; then
            has_error=1
            reason="Phát hiện Go Runtime Panic trong quá trình thực thi"
            echo "❌ THẤT BẠI (Panic)"
        # 4. Kiểm tra lỗi kết nối, RPC, hoặc timeout
        elif echo "$output" | grep -q -i "lỗi send tx" || echo "$output" | grep -q -i "Lỗi kết nối" || echo "$output" | grep -q -i "timeout waiting for receipt" || echo "$output" | grep -q "Quá thời gian chờ" || echo "$output" | grep -q "Timeout chờ"; then
            has_error=1
            reason="Gặp lỗi khi gửi Transaction, lỗi RPC hoặc Quá thời gian chờ (Timeout)"
            echo "❌ THẤT BẠI (Lỗi Gửi Tx / Timeout)"
        # 5. Kiểm tra ký hiệu lỗi ❌ xuất hiện trong output
        elif echo "$output" | grep -q "❌"; then
            has_error=1
            reason="Phát hiện lỗi trong tiến trình kiểm thử (Ký hiệu ❌ trong log)"
            echo "❌ THẤT BẠI (Lỗi Thực Thi / Assert Thất Bại)"
        fi

        # Nếu phát hiện lỗi: DỪNG NGAY LẬP TỨC
        if [ $has_error -eq 1 ]; then
            FAILED=$((FAILED + 1))
            REASONS["$test_key"]="$reason"
            FAILED_TEST_KEY="$test_key"
            break 2
        fi

        # Nếu qua hết kiểm tra -> Thành công
        SUCCESS=$((SUCCESS + 1))
        echo "✅ THÀNH CÔNG"
    done
done

echo ""
echo "=========================================================="
echo "📊 BÁO CÁO THỐNG KÊ CROSS-CHAIN TEST SUITE"
echo "=========================================================="
echo "🔹 Tổng số bài test : $TOTAL_TEST_CASES"
echo "🔹 Kế hoạch lượt chạy: $TOTAL_RUNS"
echo "🔹 Thành công       : $SUCCESS"
echo "🔹 Thất bại         : $FAILED"

if [ $FAILED -gt 0 ]; then
    echo "----------------------------------------------------------"
    echo "🚨 ĐÃ DỪNG NGAY LẬP TỨC DO PHÁT HIỆN LỖI:"
    for key in "${!REASONS[@]}"; do
        echo "  ❌ [$key]: ${REASONS[$key]}"
        echo "     (Đọc log chi tiết tại: $LOG_DIR/${key}.log)"
    done
fi
echo "=========================================================="

# Xuất báo cáo ra file Markdown
echo "# 📊 Báo Cáo Kết Quả Test Cross-Chain" > "$REPORT_FILE"
echo "**Thời gian chạy:** $(date '+%Y-%m-%d %H:%M:%S')" >> "$REPORT_FILE"
echo "" >> "$REPORT_FILE"
echo "## 📈 Thống Kê Chung" >> "$REPORT_FILE"
echo "- **Tổng số bài test**: $TOTAL_TEST_CASES" >> "$REPORT_FILE"
echo "- **Tổng số lượt chạy dự kiến**: $TOTAL_RUNS" >> "$REPORT_FILE"
echo "- **✅ Lượt thành công**: $SUCCESS" >> "$REPORT_FILE"
echo "- **❌ Lượt thất bại**: $FAILED" >> "$REPORT_FILE"
echo "" >> "$REPORT_FILE"

if [ $FAILED -gt 0 ]; then
    echo "## 🚨 Chi Tiết Lỗi & Nguyên Nhân (Đã Dừng Khẩn Cấp)" >> "$REPORT_FILE"
    for key in "${!REASONS[@]}"; do
        reason_text="${REASONS[$key]}"
        echo "### ❌ Bài Test Thất Bại: \`$key\`" >> "$REPORT_FILE"
        echo "- **Hiện tượng / Lỗi thực tế (Actual)**: $reason_text" >> "$REPORT_FILE"
        
        if echo "$reason_text" | grep -q -i "Crash\|Panic"; then
            echo "- **Nguyên nhân dự đoán**: Core Node hoặc Test Client bị Panic/Crash (Lỗi bộ nhớ, channel, nil pointer, hoặc FFI)." >> "$REPORT_FILE"
        elif echo "$reason_text" | grep -q -i "Timeout"; then
            echo "- **Nguyên nhân dự đoán**: Relayer Network chưa khởi chạy, mempool bị kẹt, hoặc Chain không sinh block kịp thời." >> "$REPORT_FILE"
        elif echo "$reason_text" | grep -q -i "Sai Logic"; then
            echo "- **Nguyên nhân dự đoán**: Dữ liệu State liên chuỗi hoặc kết quả chuyển tiếp token/message không khớp với kỳ vọng." >> "$REPORT_FILE"
        else
            echo "- **Nguyên nhân dự đoán**: Giao dịch bị revert ngoài ý muốn hoặc kiểm tra điều kiện bảo mật không khớp." >> "$REPORT_FILE"
        fi
        
        echo "- **Kết quả kỳ vọng (Expected)**: Hoàn tất trọn vẹn toàn bộ các bước mà không gặp lỗi/timeout." >> "$REPORT_FILE"
        echo "- **File Log Chi Tiết**: [\`${key}.log\`](./$LOG_DIR/${key}.log)" >> "$REPORT_FILE"
        echo "" >> "$REPORT_FILE"
    done
    echo "💾 Đã tự động xuất báo cáo chi tiết ra file: $REPORT_FILE"
    exit 1
else
    echo "## 🎉 Kết Quả" >> "$REPORT_FILE"
    echo "Toàn bộ $TOTAL_RUNS lượt test đã hoàn thành thành công và an toàn tuyệt đối!" >> "$REPORT_FILE"
    echo "💾 Đã tự động xuất báo cáo chi tiết ra file: $REPORT_FILE"
    exit 0
fi
