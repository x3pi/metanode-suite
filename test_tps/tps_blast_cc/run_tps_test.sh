#!/bin/bash
set -e

# Cấu hình mặc định cho quy trình chạy
RESET=true
COUNT=50000

# Cấu hình mặc định cho công cụ test tps_blast_cc
ROUNDS=3
LOAD_BALANCE=true
BATCH=5000
TPS_TARGET=50000
EPOCH_WAIT=0
CONFIG=""
EXTRA_ARGS=()

# Phân tích tham số truyền vào
while [[ $# -gt 0 ]]; do
  case $1 in
    --no-reset)
      RESET=false
      shift
      ;;
    --count)
      COUNT=$2
      shift 2
      ;;
    --count=*)
      COUNT="${1#*=}"
      shift
      ;;
    --rounds)
      ROUNDS=$2
      shift 2
      ;;
    --rounds=*)
      ROUNDS="${1#*=}"
      shift
      ;;
    --load_balance)
      LOAD_BALANCE=$2
      shift 2
      ;;
    --load_balance=*)
      LOAD_BALANCE="${1#*=}"
      shift
      ;;
    --batch)
      BATCH=$2
      shift 2
      ;;
    --batch=*)
      BATCH="${1#*=}"
      shift
      ;;
    --tps-target)
      TPS_TARGET=$2
      shift 2
      ;;
    --tps-target=*)
      TPS_TARGET="${1#*=}"
      shift
      ;;
    --epoch-wait)
      EPOCH_WAIT=$2
      shift 2
      ;;
    --epoch-wait=*)
      EPOCH_WAIT="${1#*=}"
      shift
      ;;
    --amount)
      EXTRA_ARGS+=("--amount" "$2")
      shift 2
      ;;
    --amount=*)
      EXTRA_ARGS+=("$1")
      shift
      ;;
    --config)
      CONFIG=$2
      shift 2
      ;;
    --config=*)
      CONFIG="${1#*=}"
      shift
      ;;
    *)
      if [[ "$1" =~ ^[0-9]+$ ]]; then
        COUNT=$1
      else
        EXTRA_ARGS+=("$1")
      fi
      shift
      ;;
  esac
done

echo "=========================================================="
echo "🚀 BẮT ĐẦU QUY TRÌNH TEST TPS"
echo "   - RESET CỤM NODE : $RESET"
echo "   - SỐ LƯỢNG VÍ    : $COUNT"
echo "   - SỐ VÒNG TEST   : $ROUNDS"
echo "   - BATCH SIZE     : $BATCH"
echo "   - TPS TARGET     : $TPS_TARGET"
echo "   - EPOCH WAIT     : $EPOCH_WAIT"
echo "   - CẤU HÌNH       : $CONFIG"
if [ ${#EXTRA_ARGS[@]} -gt 0 ]; then
echo "   - THAM SỐ KHÁC   : ${EXTRA_ARGS[*]}"
fi
echo "=========================================================="

if [ "$RESET" = true ]; then
  echo "👉 Bước 1-3: Đã loại bỏ do chuyển sang Rust (Mock RPC không cần pre-fund ví trong genesis)."

  echo "👉 Bước 4: Deploy & Reset lại toàn bộ cụm node (Xóa dữ liệu cũ)..."
  cd ../../../metanode/deploy/ansible
  ./ansible_deploy.sh --reset-all
else
  echo "ℹ️  Bỏ qua sinh ví và deploy (Giữ nguyên dữ liệu blockchain hiện tại)."
fi

echo "👉 Bước 5: Chạy test TPS với các tùy chỉnh..."
cd ../../../metanode-suite/test_tps/tps_blast_cc
# Cập nhật cấu hình IP/Proxy
../../scripts/update-ip/update-ip.sh || true

rm -f /tmp/MTN_CHAIN_ERROR_STOP

# Đo dung lượng ổ cứng trước khi bắn test
echo ""
echo "=========================================================="
echo "📊 [DEBUG Ổ CỨNG] DUNG LƯỢNG TRƯỚC KHI TEST:"
if [ -d "/opt/metanode/node-0" ]; then
  BEFORE_NODE0_ALLOC=$(du -sh /opt/metanode/node-0 2>/dev/null | cut -f1)
  BEFORE_NODE0_DATA=$(du -sh --apparent-size /opt/metanode/node-0 2>/dev/null | cut -f1)
  BEFORE_NODE0_KB=$(du -sk /opt/metanode/node-0 2>/dev/null | cut -f1)
  BEFORE_NODE0_DATA_KB=$(du -sk --apparent-size /opt/metanode/node-0 2>/dev/null | cut -f1)
  TOTAL_OPT_BEFORE=$(du -sh /opt/metanode 2>/dev/null | cut -f1)
  echo "   • node-0 chiếm ổ đĩa (Allocated): $BEFORE_NODE0_ALLOC"
  echo "   • node-0 dữ liệu thật (Apparent) : $BEFORE_NODE0_DATA"
  echo "   • Tổng toàn bộ /opt/metanode     : $TOTAL_OPT_BEFORE"
else
  echo "   (Chưa tìm thấy /opt/metanode/node-0)"
fi
echo "=========================================================="
echo ""

CMD_ARGS=(
  --count "$COUNT"
  --rounds "$ROUNDS"
  --load_balance="$LOAD_BALANCE"
  --batch "$BATCH"
  --tps-target "$TPS_TARGET"
  --epoch-wait "$EPOCH_WAIT"
)
if [ -n "$CONFIG" ]; then
  CMD_ARGS+=(--config "$CONFIG")
fi

GOMAXPROCS=16 go run main.go \
  "${CMD_ARGS[@]}" \
  "${EXTRA_ARGS[@]}"

echo ""
echo "=========================================================="
echo "📊 [DEBUG Ổ CỨNG] DUNG LƯỢNG SAU KHI TEST XONG:"
if [ -d "/opt/metanode/node-0" ]; then
  AFTER_NODE0_ALLOC=$(du -sh /opt/metanode/node-0 2>/dev/null | cut -f1)
  AFTER_NODE0_DATA=$(du -sh --apparent-size /opt/metanode/node-0 2>/dev/null | cut -f1)
  AFTER_NODE0_KB=$(du -sk /opt/metanode/node-0 2>/dev/null | cut -f1)
  AFTER_NODE0_DATA_KB=$(du -sk --apparent-size /opt/metanode/node-0 2>/dev/null | cut -f1)
  TOTAL_OPT_AFTER=$(du -sh /opt/metanode 2>/dev/null | cut -f1)

  DIFF_ALLOC_MB=$(( (AFTER_NODE0_KB - BEFORE_NODE0_KB) / 1024 ))
  DIFF_DATA_MB=$(( (AFTER_NODE0_DATA_KB - BEFORE_NODE0_DATA_KB) / 1024 ))

  echo "   • node-0 chiếm ổ đĩa (Allocated): $AFTER_NODE0_ALLOC (Tăng: +${DIFF_ALLOC_MB} MB)"
  echo "   • node-0 dữ liệu thật (Apparent) : $AFTER_NODE0_DATA (Tăng: +${DIFF_DATA_MB} MB)"
  echo "   • Tổng toàn bộ /opt/metanode     : $TOTAL_OPT_AFTER"
fi
echo "=========================================================="

echo "=========================================================="
echo "✅ HOÀN THÀNH QUY TRÌNH TEST!"
echo "=========================================================="
