# 📋 Lệnh quản lý File Registry Contract

### 1. Xem danh sách các File Contract đã đăng ký (List)
```bash
go run . -action=list -registry=0x1CB31325ACAeCa405328087Eb26f447c98e6239D
```

### 2. Thêm (Đăng ký) File Contract mới vào Registry (Add)
```bash
go run . -action=add -registry=0x1CB31325ACAeCa405328087Eb26f447c98e6239D -file=0x67C7271B909573714238b2F966A90FDFfB526443
```

### 3. Hủy đăng ký File Contract khỏi Registry (Remove)
```bash
go run . -action=remove -registry=0x1CB31325ACAeCa405328087Eb26f447c98e6239D -file=0x087cdab97d38a3bfFcDee170739E8C11Af651569
```
