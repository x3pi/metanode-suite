#!/bin/bash
set -e

# Cấu hình mặc định cho quy trình chạy
RESET=true
COUNT=50000

# Cấu hình mặc định cho công cụ test tps_blast_cc
ROUNDS=3
LOAD_BALANCE=true
BATCH=20000
TPS_TARGET=50000
EPOCH_WAIT=0
CONFIG="config-multi.json"
EXTRA_ARGS=()

# Phân tích tham số truyền vào
while [[ $# -gt 0 ]]; do
  case $1 in
    --no-reset)
      RESET=false
      shift
      ;;
    --rounds)
      ROUNDS=$2
      shift 2
      ;;
    --load_balance)
      LOAD_BALANCE=$2
      shift 2
      ;;
    --batch)
      BATCH=$2
      shift 2
      ;;
    --tps-target)
      TPS_TARGET=$2
      shift 2
      ;;
    --epoch-wait)
      EPOCH_WAIT=$2
      shift 2
      ;;
    --config)
      CONFIG=$2
      shift 2
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
  echo "👉 Bước 1: Sinh bộ $COUNT ví kép chuẩn (BLS + ECDSA tương khớp)..."
  cd ~/chain-n/metanode/execution
  sed -i "s/count := [0-9]*/count := $COUNT/g" generate_valid_keys.go
  go run generate_valid_keys.go

  echo "👉 Bước 2: Nạp các ví vừa sinh vào genesis.json..."
  cp ~/chain-n/metanode/deploy/systemd/genesis.json.example ~/chain-n/metanode/deploy/systemd/genesis.json
  python3 ~/chain-n/metanode-suite/test_tps/gen_spam_keys/manage_genesis.py add ~/chain-n/metanode/deploy/systemd/genesis.json ~/chain-n/metanode-suite/test_tps/gen_spam_keys/generated_keys.json

  echo "👉 Bước 3: Đồng bộ hóa khóa public BLS tương ứng vào genesis.json..."
  go run fix_genesis.go

  echo "👉 Bước 4: Deploy & Reset lại toàn bộ cụm node (Xóa dữ liệu cũ)..."
  cd ~/chain-n/metanode/deploy/ansible
  ./ansible_deploy.sh --reset-all
else
  echo "ℹ️  Bỏ qua sinh ví và deploy (Giữ nguyên dữ liệu blockchain hiện tại)."
fi

echo "👉 Bước 5: Chạy test TPS với các tùy chỉnh..."
cd ~/chain-n/metanode-suite/test_tps/tps_blast_cc
# Cập nhật cấu hình IP/Proxy
../../scripts/update-ip/update-ip.sh || true

go run main.go \
  --count "$COUNT" \
  --rounds "$ROUNDS" \
  --load_balance="$LOAD_BALANCE" \
  --batch "$BATCH" \
  --tps-target "$TPS_TARGET" \
  --epoch-wait "$EPOCH_WAIT" \
  --config="$CONFIG" \
  "${EXTRA_ARGS[@]}"

echo "=========================================================="
echo "✅ HOÀN THÀNH QUY TRÌNH TEST!"
echo "=========================================================="
