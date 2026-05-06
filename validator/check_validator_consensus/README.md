# check_validator_consensus

Tool kiểm tra validator đã đăng ký và tham gia consensus chưa.

## Cài đặt

```bash

go run main.go -watch -addr 0x781E6EC6EBDCA11Be4B53865a34C0c7f10b6da6e
```

---

## Sử dụng

### 1. Xem tất cả validators đang hoạt động

```bash
go run main.go
```

Kết quả: danh sách toàn bộ validator on-chain + epoch hiện tại.

---

### 2. Kiểm tra 1 validator cụ thể

```bash
go run main.go -addr 0xYourValidatorAddress
```

Kết quả:

- ❌ Chưa đăng ký → cần gọi `registerValidator()`
- ⚠️  Stake = 0 → cần gọi `delegate()`
- ✅ Đăng ký + stake > 0 → sẽ vào committee ở epoch tiếp theo

---

### 3. Theo dõi liên tục (polling 10s)

```bash
go run main.go -watch -addr 0xYourValidatorAddress
```

Tự động poll mỗi 10 giây. Dừng và thông báo khi validator đã sẵn sàng tham gia consensus.  
Nhấn `Ctrl+C` để thoát.

---

### 4. Kiểm tra committee của epoch cụ thể

```bash
go run main.go -epoch 3
```

---

### 5. Dùng RPC khác (không phải localhost)

```bash
go run main.go -rpc http://192.168.1.100:8545 -addr 0xYourValidatorAddress
```

---

## Các flag

| Flag | Mặc định | Mô tả |
|---|---|---|
| `-rpc` | `http://localhost:8545` | URL RPC node |
| `-addr` | _(trống)_ | Địa chỉ validator cần kiểm tra |
| `-watch` | `false` | Bật chế độ polling liên tục |
| `-epoch` | `-1` (hiện tại) | Kiểm tra epoch cụ thể |
| `-stuck` | `150s` | Thời gian ngưỡng để báo lỗi kẹt block (ví dụ: `120s`, `150s`) |

---

## Luồng đăng ký validator

```
1. registerValidator()  →  contract 0x...1001
2. delegate()           →  stake tokens (stake > 0)
3. Chờ epoch transition →  EpochBoundary system tx xuất hiện
4. Validator vào committee epoch N+1
```

**Xác nhận trong Rust log:**

```
📊 Built committee with N authorities for epoch X
✅ [TRANSITION] Self in committee at index Y
```
