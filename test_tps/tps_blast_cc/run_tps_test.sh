#!/bin/bash
set -e

# Khởi tạo mặc định
RESET=true
COUNT=50000

# Phân tích tham số truyền vào
while [[ $# -gt 0 ]]; do
  case $1 in
    --no-reset)
      RESET=false
      shift
      ;;
    *)
      COUNT=$1
      shift
      ;;
  esac
done

echo "=========================================================="
echo "🚀 BẮT ĐẦU QUY TRÌNH TEST TPS (RESET-ALL: $RESET | COUNT: $COUNT)"
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

echo "👉 Bước 5: Chạy test TPS với $COUNT giao dịch..."
cd ~/chain-n/metanode-suite/test_tps/tps_blast_cc
# Cập nhật cấu hình IP/Proxy
../../scripts/update-ip/update-ip.sh || true
go run main.go --count $COUNT --rounds 3 --load_balance=true --batch 20000 --tps-target 50000 --epoch-wait 0 --config=config-multi.json

echo "=========================================================="
echo "✅ HOÀN THÀNH QUY TRÌNH TEST!"
echo "=========================================================="
