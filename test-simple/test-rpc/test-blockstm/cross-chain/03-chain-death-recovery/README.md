# 💀 Diễn Tập Cứu Hộ Khi Chain Chết (P8 — Chain-Death Recovery Drill)

Bài test thực tế mô phỏng việc **tắt/sập hoàn toàn Private Chain 101** và kiểm chứng việc người dùng vẫn **rút lại 100% tiền an toàn qua Public Chain 991**.

---

## 🚀 Cách Chạy Test Tương Tác (Interactive Drill)

### Bước 1: Mở Terminal 1 và Chạy Script Test
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/34-chain-death-recovery
go run main.go
```

Khi script chạy đến Bước 2, màn hình sẽ hiển thị:
```text
💥 BƯỚC 2: MÔ PHỎNG PRIVATE CHAIN 101 BỊ CHẾT HẲN
👉 HÃY THỬ TẮT CHAIN 101 BÂY GIỜ ĐỂ XEM HỆ THỐNG CỨU HỘ:
   Mở 1 tab terminal khác và chạy lệnh:
   fuser -k 8546/tcp
```

---

### Bước 2: Mở Terminal 2 và Tắt Chain 101
```bash
fuser -k 8546/tcp
```

---

### Bước 3: Quan Sát Kết Quả Cứu Hộ (Ở Terminal 1)
* Script ở Terminal 1 sẽ phát hiện Chain 101 đã chết hẳn (Connection Refused).
* Script tự động chuyển hướng qua **Public Chain 991** để nộp Merkle Proof và giải ngân $+500\text{ MTN}$ an toàn về **Chain 102**.

---

## ⚡ Cách Chạy Tự Động (Auto-Kill)
Nếu muốn script tự động kill Chain 101 và chạy từ A-Z:
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/34-chain-death-recovery
go run main.go --auto-kill
```

---

## 🔄 Bật Lại Chain 101 Sau Khi Test Xong
Sau khi diễn tập xong, bạn có thể bật lại 2 Private Chain bất cứ lúc nào bằng lệnh:
```bash
cd /home/abc/nhat/consensus-chain/metanode/deploy/systemd && bash setup_2_private_chains.sh
```
