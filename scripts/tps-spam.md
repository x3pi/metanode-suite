# tps-spam — TPS Benchmark Tool

Chạy vô hạn loop TPS test, ghi kết quả từng round vào log. Dùng để đo throughput của chain liên tục.

---

## Cách dùng

```bash
cd /home/abc/nhat/con-chain-v2/tool-test/scripts

./tps-spam.sh            # Khởi động trong tmux (attach nếu đã đang chạy)
./tps-spam.sh stop       # Dừng
./tps-spam.sh log        # Xem TPS log từng round
./tps-spam.sh errors     # Xem chi tiết các round bị lỗi
```

> Nhấn **Ctrl+B rồi D** để tách khỏi tmux mà không dừng script.  
> Nhấn **Ctrl+C** bên trong tmux để dừng sau khi round hiện tại kết thúc.

---

## Cấu hình (override qua env var)

| Biến | Mặc định | Ý nghĩa |
|---|---|---|
| `COUNT` | `20000` | Số TX mỗi round |
| `BATCH` | `10` | TX mỗi batch gửi |
| `SLEEP_MS` | `10` | Delay giữa các batch (ms) |
| `WAIT` | `120` | Timeout chờ chain xử lý (giây) |
| `PARALLEL` | `true` | Dùng native parallel transfer |
| `LOAD_BALANCE` | `false` | Gửi TX qua nhiều node |

```bash
# Ví dụ: chạy với 50000 TX/round, load balance bật
COUNT=50000 LOAD_BALANCE=true ./tps-spam.sh
```

---

## Log files

| File | Nội dung |
|---|---|
| `logs/tps_rounds.log` | TPS từng round: timestamp, round number, TPS, block range |
| `logs/tps_errors.log` | Full output của round bị lỗi (để debug) |

Ví dụ `tps_rounds.log`:
```
[2026-05-05 09:57:33] Round 1 | TPS: ~958 tx/s | Blocks: 272 to 430 | TXs: 20000
[2026-05-05 09:58:08] Round 2 | TPS: ~853 tx/s | Blocks: 430 to 521 | TXs: 20000
```
