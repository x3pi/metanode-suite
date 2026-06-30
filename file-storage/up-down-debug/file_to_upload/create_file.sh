#!/usr/bin/env bash
set -e

OUT="output.txt"
CHUNK_SIZE=256000 # Mặc định 250KB (250 * 1024)
CHUNKS=20
TARGET_BYTES=0

while [[ $# -gt 0 ]]; do
  case $1 in
    --chunks)
      CHUNKS="$2"
      shift 2
      ;;
    --chunk-size-kb)
      CHUNK_SIZE=$(($2 * 1024))
      shift 2
      ;;
    --size-mb)
      TARGET_BYTES=$(($2 * 1024 * 1024))
      shift 2
      ;;
    *)
      OUT="$1"
      shift
      ;;
  esac
done

if [ "$TARGET_BYTES" -eq 0 ]; then
    TARGET_BYTES=$(( CHUNK_SIZE * CHUNKS ))
    echo "Tạo file '${OUT}' với số lượng ${CHUNKS} chunks (Mỗi chunk $((CHUNK_SIZE/1024))KB) -> Tổng kích thước: ${TARGET_BYTES} bytes..."
else
    echo "Tạo file '${OUT}' với kích thước $(($TARGET_BYTES/1024/1024))MB -> Tổng kích thước: ${TARGET_BYTES} bytes..."
fi

# Ưu tiên tạo file text có ký tự in được: dùng `yes` (nhiều hệ thống có sẵn)
if command -v yes >/dev/null 2>&1 && command -v head >/dev/null 2>&1; then
  yes "This is a sample text line for filling the file." | head -c "${TARGET_BYTES}" > "${OUT}"
  echo "Hoàn tất (đã dùng yes | head)."
  exit 0
fi

# Nếu fallocate có sẵn (tạo file nhanh, nhưng có thể không ghi ký tự in được)
if command -v fallocate >/dev/null 2>&1; then
  fallocate -l "${SIZE_MB}M" "${OUT}"
  echo "Hoàn tất (đã dùng fallocate)."
  exit 0
fi

# Nếu dd có sẵn, dùng dd từ /dev/zero (có thể chứa byte null, vẫn là file .txt nhưng không in đẹp)
if command -v dd >/dev/null 2>&1; then
  dd if=/dev/zero of="${OUT}" bs=1M count="${SIZE_MB}" status=none
  echo "Hoàn tất (đã dùng dd from /dev/zero)."
  exit 0
fi

# Dự phòng: dùng head từ /dev/zero nếu có
if command -v head >/dev/null 2>&1; then
  head -c "${TARGET_BYTES}" </dev/zero > "${OUT}"
  echo "Hoàn tất (đã dùng head từ /dev/zero)."
  exit 0
fi

echo "Lỗi: Không tìm thấy công cụ phù hợp (yes, fallocate, dd hoặc head)."
exit 1
