# 📡 Metanode Autonomous Relayer Network Daemon (24/7 Service)

Tiến trình Relayer độc lập chạy ngầm để tự động phát hiện, chứng thực và chuyển phát các giao dịch liên chuỗi (Asset Transfer & Smart Contract GMP) giữa các Private Chain và Public Root Anchor.

---

## 🏛️ Cơ Chế Hoạt Động (24/7 Event-Driven)

```
[ NGƯỜI DÙNG ] ──(Chỉ gửi 1 Tx trên Chain 101)──► [ Private Chain 101 ]
                                                            │
                                                            ▼ (Block mới)
                                             ┌─────────────────────────────┐
                                             │ 📡 RELAYER DAEMON           │
                                             │ 1. Tự động phát hiện Tx     │
                                             │ 2. Tự nộp Attest lên 991    │
                                             │ 3. Tự nộp Claim sang 102    │
                                             │ 4. Nhận 1.00 MTN tiền Tip   │
                                             └──────────────┬──────────────┘
                                                            │
                                                            ▼
[ NGƯỜI NHẬN ] ◄──(Tự động nhận +100 MTN)─────── [ Private Chain 102 ]
```

---

## 🚀 Hướng Dẫn Sử Dụng

Vào thư mục:
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/cross-chain/05-relayer-network
```

### 1. Bật Relayer Daemon Chạy Ngầm (Tab 1):
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/cross-chain/05-relayer-network/daemon
go run .
```
*(Hoặc từ thư mục `05-relayer-network`: `go run ./daemon`)*

---

### 2. Người Dùng Gửi Tiền Thử Nghiệm (Tab 2):
Mở một tab terminal khác và chạy đóng vai Người dùng:
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/cross-chain/05-relayer-network/user-client
go run . --amount 100
```
*(Hoặc từ thư mục `05-relayer-network`: `go run ./user-client --amount 100`)*

* Người dùng **chỉ gửi đúng 1 giao dịch lên Chain 101 và xong việc**.
* Quan sát bên **Tab 1 (Relayer Daemon)**: Bạn sẽ thấy Relayer lập tức bắt được giao dịch $\rightarrow$ Nộp sang Public Chain 991 $\rightarrow$ Nộp sang Chain 102 $\rightarrow$ Thu về $+1.00\text{ MTN}$ tiền Tip!
* Bên **Tab 2 (Người dùng)**: Số dư trên Chain 102 tự động nhảy $+100.00\text{ MTN}$ hoàn tất!
