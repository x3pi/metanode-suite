### 2. Giám sát liên tục theo thời gian thực (Watch mode)

Cú pháp cho mạng 5 Nodes (cấu hình trong `config.json`):

```bash
# single machine
#### KHÔNG ẢNH HƯỞNG LUỒNG TELE ####
go run main.go --watch --interval 5s --no-stop-flag

# CHO TELE CHẠY ###############
go run main.go --watch --interval 5s


# multiple machine (nếu ghi đè config)
go run main.go --watch --interval 5s --nodes "m0=http://192.168.1.234:8757,m1=http://192.168.1.234:10747,..."
```
