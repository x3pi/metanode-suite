#!/usr/bin/env bash

# ==============================================================================
# SCRIPT CHẠY BỘ TEST CROSS-CHAIN VÀ PARENT CHAIN HEALTH
# Hỗ trợ tự động quét tất cả các folder test (01-*, 02-*, ...) hoặc chạy từng bài.
# ==============================================================================

set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Màu sắc hiển thị
C_RESET="\033[0m"
C_RED="\033[31m"
C_GREEN="\033[32m"
C_YELLOW="\033[33m"
C_BLUE="\033[34m"
C_CYAN="\033[36m"
C_PURPLE="\033[35m"
C_BOLD="\033[1m"

print_banner() {
    echo -e "${C_PURPLE}╔═══════════════════════════════════════════════════════════════════════╗${C_RESET}"
    echo -e "${C_PURPLE}║         🌐 METANODE CROSS-CHAIN & PARENT CHAIN TEST RUNNER            ║${C_RESET}"
    echo -e "${C_PURPLE}╚═══════════════════════════════════════════════════════════════════════╝${C_RESET}"
}

print_help() {
    echo -e "${C_BOLD}CÁCH SỬ DỤNG:${C_RESET}"
    echo -e "  ./run_tests.sh [LỰA CHỌN] [CHẾ ĐỘ]"
    echo ""
    echo -e "${C_BOLD}LỰA CHỌN BÀI TEST:${C_RESET}"
    echo -e "  ${C_CYAN}all${C_RESET}               Chạy toàn bộ các thư mục test theo thứ tự (mặc định)"
    echo -e "  ${C_CYAN}01 | parent${C_RESET}       Chạy test 01: Kiểm tra sức khỏe toàn diện Parent Chain"
    echo -e "  ${C_CYAN}02 | transfer${C_RESET}     Chạy test 02: Giao dịch chuyển tiền xuyên chuỗi (Exec 1 <-> Exec 2)"
    echo -e "  ${C_CYAN}03 | stress${C_RESET}       Chạy test 03: Stress test phát sóng đồng thời nhiều tx xuyên chuỗi"
    echo -e "  ${C_CYAN}04 | contract${C_RESET}     Chạy test 04: Gọi & tương tác Smart Contract xuyên chuỗi"
    echo -e "  ${C_CYAN}<tên_folder>${C_RESET}      Chạy thư mục test cụ thể bất kỳ (ví dụ: 01-parent-chain-health)"
    echo ""
    echo -e "${C_BOLD}CHẾ ĐỘ THỰC THI (MODE):${C_RESET}"
    echo -e "  ${C_CYAN}(mặc định)${C_RESET}        Chạy 'go run .' với giao diện dashboard chi tiết"
    echo -e "  ${C_CYAN}--gotest | -test${C_RESET}  Chạy 'go test -v .' theo chuẩn unit test của Go"
    echo ""
    echo -e "${C_BOLD}VÍ DỤ:${C_RESET}"
    echo -e "  ./run_tests.sh"
    echo -e "  ./run_tests.sh 01"
    echo -e "  ./run_tests.sh 02 --gotest"
    echo -e "  ./run_tests.sh all --gotest"
    echo ""
}

# Phân tích tham số dòng lệnh
MODE="gorun"
TARGET_PATTERN="all"

for arg in "$@"; do
    case "$arg in" in
        *)
            case "$arg" in
                --help|-h|help)
                    print_help
                    exit 0
                    ;;
                --gotest|-gotest|--test|-test)
                    MODE="gotest"
                    ;;
                all)
                    TARGET_PATTERN="all"
                    ;;
                01|1|parent|parent-only|*parent-chain-health*)
                    TARGET_PATTERN="01*"
                    ;;
                02|2|transfer|transfer-only|*cross-chain-transfer*)
                    TARGET_PATTERN="02*"
                    ;;
                03|3|stress|*cross-chain-stress*)
                    TARGET_PATTERN="03*"
                    ;;
                04|4|contract|contract-only|*cross-chain-contract*)
                    TARGET_PATTERN="04*"
                    ;;
                *)
                    # Nếu truyền tên folder cụ thể
                    if [ -d "$SCRIPT_DIR/$arg" ]; then
                        TARGET_PATTERN="$arg"
                    else
                        TARGET_PATTERN="$arg"
                    fi
                    ;;
            esac
            ;;
    esac
done

print_banner

# Quét danh sách các folder test (tự động nhận mọi folder có dạng số-*)
FOLDERS=()
if [ "$TARGET_PATTERN" = "all" ]; then
    for dir in $(find . -maxdepth 1 -mindepth 1 -type d -name "[0-9]*" | sort); do
        FOLDERS+=("$(basename "$dir")")
    done
else
    for dir in $(find . -maxdepth 1 -mindepth 1 -type d -name "$TARGET_PATTERN" | sort); do
        FOLDERS+=("$(basename "$dir")")
    done
fi

if [ ${#FOLDERS[@]} -eq 0 ]; then
    echo -e "${C_RED}❌ Không tìm thấy thư mục test nào khớp với mẫu: '$TARGET_PATTERN'${C_RESET}"
    exit 1
fi

echo -e "${C_BOLD}Danh sách bài test sẽ chạy (${#FOLDERS[@]} bài, Mode: ${C_CYAN}${MODE}${C_RESET}${C_BOLD}):${C_RESET}"
for f in "${FOLDERS[@]}"; do
    echo -e "  • ${C_YELLOW}$f${C_RESET}"
done
echo ""

# Biến theo dõi kết quả
PASSED_TESTS=()
FAILED_TESTS=()
START_TOTAL=$(date +%s)

for test_dir in "${FOLDERS[@]}"; do
    echo -e "${C_CYAN}═══════════════════════════════════════════════════════════════════════${C_RESET}"
    echo -e "${C_BOLD}▶ ĐANG CHẠY BÀI TEST: ${C_YELLOW}${test_dir}${C_RESET}"
    echo -e "${C_CYAN}═══════════════════════════════════════════════════════════════════════${C_RESET}"

    cd "$SCRIPT_DIR/$test_dir" || continue

    T_START=$(date +%s)
    if [ "$MODE" = "gotest" ]; then
        go test -v .
        STATUS=$?
    else
        go run .
        STATUS=$?
    fi
    T_END=$(date +%s)
    DURATION=$((T_END - T_START))

    if [ $STATUS -eq 0 ]; then
        echo -e "${C_GREEN}✔ HOÀN THÀNH: ${test_dir} (Thời gian: ${DURATION}s)${C_RESET}"
        PASSED_TESTS+=("$test_dir (${DURATION}s)")
    else
        echo -e "${C_RED}✖ THẤT BÀI: ${test_dir} (Thoát mã: ${STATUS}, Thời gian: ${DURATION}s)${C_RESET}"
        FAILED_TESTS+=("$test_dir (${DURATION}s)")
    fi
    echo ""
done

cd "$SCRIPT_DIR"
END_TOTAL=$(date +%s)
TOTAL_DURATION=$((END_TOTAL - START_TOTAL))

# BẢNG TỔNG KẾT
echo -e "${C_PURPLE}═══════════════════════════════════════════════════════════════════════${C_RESET}"
echo -e "${C_BOLD}                       BẢNG TỔNG KẾT BÀI TEST                          ${C_RESET}"
echo -e "${C_PURPLE}═══════════════════════════════════════════════════════════════════════${C_RESET}"
echo -e "Tổng số bài test đã chạy: ${#FOLDERS[@]}"
echo -e "Tổng thời gian:           ${TOTAL_DURATION}s"
echo -e "${C_GREEN}Số bài đạt (PASS):        ${#PASSED_TESTS[@]}${C_RESET}"
for p in "${PASSED_TESTS[@]}"; do
    echo -e "  ✅ $p"
done

if [ ${#FAILED_TESTS[@]} -gt 0 ]; then
    echo -e "${C_RED}Số bài lỗi (FAIL):        ${#FAILED_TESTS[@]}${C_RESET}"
    for f in "${FAILED_TESTS[@]}"; do
        echo -e "  ❌ $f"
    done
    echo -e "${C_PURPLE}═══════════════════════════════════════════════════════════════════════${C_RESET}"
    exit 1
else
    echo -e "${C_GREEN}TẤT CẢ CÁC BÀI TEST ĐỀU THÀNH CÔNG! 🎉${C_RESET}"
    echo -e "${C_PURPLE}═══════════════════════════════════════════════════════════════════════${C_RESET}"
    exit 0
fi
