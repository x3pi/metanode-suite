#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUITE_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
METANODE_DIR="$(cd "$SUITE_DIR/../metanode" 2>/dev/null && pwd || echo "/home/abc/nhat/con-chain-v2/metanode")"

# Cấu hình mặc định cho quy trình chạy (Mặc định: chain_a)
CHAIN="chain_a"
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
    --chain)
      CHAIN=$2
      shift 2
      ;;
    --chain=*)
      CHAIN="${1#*=}"
      shift
      ;;
    -c)
      CHAIN=$2
      shift 2
      ;;
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
echo "   - TARGET CHAIN   : $CHAIN"
echo "   - RESET CỤM NODE : $RESET"
echo "   - SỐ LƯỢNG VÍ    : $COUNT"
echo "   - SỐ VÒNG TEST   : $ROUNDS"
echo "   - BATCH SIZE     : $BATCH"
echo "   - TPS TARGET     : $TPS_TARGET"
echo "   - EPOCH WAIT     : $EPOCH_WAIT"
if [ -n "$CONFIG" ]; then
  echo "   - CẤU HÌNH       : $CONFIG"
fi
if [ ${#EXTRA_ARGS[@]} -gt 0 ]; then
  echo "   - THAM SỐ KHÁC   : ${EXTRA_ARGS[*]}"
fi
echo "=========================================================="

if [ "$RESET" = true ]; then
  echo "👉 Bước 1-3: Đã loại bỏ do chuyển sang Rust (Mock RPC không cần pre-fund ví trong genesis)."

  if [[ "$CHAIN" == "chain_"* || "$CHAIN" == "exec"* || "$CHAIN" == "1" || "$CHAIN" == "2" ]]; then
    echo "👉 Bước 4: Deploy & Reset lại Execution Clusters ($CHAIN)..."
    if [ -f "$METANODE_DIR/deploy/ansible_clusters/reset_clusters.sh" ]; then
      cd "$METANODE_DIR/deploy/ansible_clusters"
      ./reset_clusters.sh
    else
      echo "⚠️  Không tìm thấy reset_clusters.sh, bỏ qua reset cụm cluster."
    fi
  else
    echo "👉 Bước 4: Deploy & Reset lại toàn bộ cụm Parent Chain (Xóa dữ liệu cũ)..."
    if [ -f "$METANODE_DIR/deploy/ansible/ansible_deploy.sh" ]; then
      cd "$METANODE_DIR/deploy/ansible"
      ./ansible_deploy.sh --reset-all
    fi
  fi
else
  echo "ℹ️  Bỏ qua sinh ví và deploy (Giữ nguyên dữ liệu blockchain hiện tại)."
fi

echo "👉 Bước 5: Chạy test TPS với các tùy chỉnh..."
cd "$SCRIPT_DIR"

# Cập nhật cấu hình IP/Proxy cho target chain
"$SUITE_DIR/scripts/update-ip/update-ip.sh" --chain "$CHAIN" || true

rm -f /tmp/MTN_CHAIN_ERROR_STOP

# Xác định thư mục kiểm tra dung lượng ổ cứng
CHECK_DIR="/opt/metanode/node-0"
if [[ "$CHAIN" == "chain_a" || "$CHAIN" == "exec1" || "$CHAIN" == "1" ]]; then
  CHECK_DIR="/opt/metanode/exec1_r1"
elif [[ "$CHAIN" == "chain_b" || "$CHAIN" == "exec2" || "$CHAIN" == "2" ]]; then
  CHECK_DIR="/opt/metanode/exec2"
fi

# Đo dung lượng ổ cứng trước khi bắn test
echo ""
echo "=========================================================="
echo "📊 [DEBUG Ổ CỨNG] DUNG LƯỢNG TRƯỚC KHI TEST:"
if [ -d "$CHECK_DIR" ]; then
  BEFORE_NODE_ALLOC=$(du -sh "$CHECK_DIR" 2>/dev/null | cut -f1)
  BEFORE_NODE_DATA=$(du -sh --apparent-size "$CHECK_DIR" 2>/dev/null | cut -f1)
  BEFORE_NODE_KB=$(du -sk "$CHECK_DIR" 2>/dev/null | cut -f1)
  TOTAL_OPT_BEFORE=$(du -sh /opt/metanode 2>/dev/null | cut -f1)
  echo "   • $(basename "$CHECK_DIR") chiếm ổ đĩa (Allocated): $BEFORE_NODE_ALLOC"
  echo "   • $(basename "$CHECK_DIR") dữ liệu thật (Apparent) : $BEFORE_NODE_DATA"
  echo "   • Tổng toàn bộ /opt/metanode     : $TOTAL_OPT_BEFORE"
else
  echo "   (Chưa tìm thấy $CHECK_DIR)"
fi
echo "=========================================================="
echo ""

CMD_ARGS=(
  --chain "$CHAIN"
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
if [ -d "$CHECK_DIR" ]; then
  AFTER_NODE_ALLOC=$(du -sh "$CHECK_DIR" 2>/dev/null | cut -f1)
  AFTER_NODE_DATA=$(du -sh --apparent-size "$CHECK_DIR" 2>/dev/null | cut -f1)
  AFTER_NODE_KB=$(du -sk "$CHECK_DIR" 2>/dev/null | cut -f1)
  TOTAL_OPT_AFTER=$(du -sh /opt/metanode 2>/dev/null | cut -f1)

  DIFF_ALLOC_MB=$(( (AFTER_NODE_KB - BEFORE_NODE_KB) / 1024 ))

  echo "   • $(basename "$CHECK_DIR") chiếm ổ đĩa (Allocated): $AFTER_NODE_ALLOC (Tăng: +${DIFF_ALLOC_MB} MB)"
  echo "   • $(basename "$CHECK_DIR") dữ liệu thật (Apparent) : $AFTER_NODE_DATA"
  echo "   • Tổng toàn bộ /opt/metanode     : $TOTAL_OPT_AFTER"
fi
echo "=========================================================="

echo "=========================================================="
echo "✅ HOÀN THÀNH QUY TRÌNH TEST!"
echo "=========================================================="
