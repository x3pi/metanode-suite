# Hướng dẫn chạy Demo Chat cho 2 User

Để chạy luồng chat giữa 2 user, bạn cần mở 2 terminal riêng biệt trong thư mục `demo-chat`.

## Terminal 1 (User 1 - Người tạo Contract)
Chạy lệnh sau để User 1 khởi tạo và deploy smart contract:
```bash
go run main.go -config=user1.json -deploy
```
👉 Sau khi chạy thành công, terminal sẽ in ra địa chỉ của contract (ví dụ: `🎉 DEPLOY THÀNH CÔNG! Address: 0x...`). Hãy copy địa chỉ này.

## Terminal 2 (User 2 - Người kết nối)
Sử dụng địa chỉ contract vừa copy được ở trên, chạy lệnh sau cho User 2 (thay `<ADDRESS>` bằng địa chỉ contract thực tế):
```bash
go run main.go -config=user2.json -contract=0x1098Ed005916B0C2a2fe56E358C96015bBe2FbFA -target=0x5e582475A504998c5631E12A5a2585D2B1911812

go run main.go -config=user1.json -contract=0x1098Ed005916B0C2a2fe56E358C96015bBe2FbFA -target=0x2C71210D239D472e963a7Be8362eCBdeD5337fE6


go run main_rpc.go -config=user2.json -contract=0x1098Ed005916B0C2a2fe56E358C96015bBe2FbFA -target=0x5e582475A504998c5631E12A5a2585D2B1911812

go run main_rpc.go -config=user1.json -contract=0x1098Ed005916B0C2a2fe56E358C96015bBe2FbFA -target=0x2C71210D239D472e963a7Be8362eCBdeD5337fE6





go run main_rpc.go -config=user2.json -contract=0xad7ED1B49F7EED17703E15F9b2c7dA1C8761Dc88 -target=0x5e582475A504998c5631E12A5a2585D2B1911812

go run main_rpc.go -config=user1.json -contract=0xad7ED1B49F7EED17703E15F9b2c7dA1C8761Dc88 -target=0x2C71210D239D472e963a7Be8362eCBdeD5337fE6 -spam 200 -ping


go run main.go -config=user2.json -contract=0x4E58562C5CDa4B80633dD7a3C759e7702213675d -target=0x5e582475A504998c5631E12A5a2585D2B1911812

go run main.go -config=user1.json -contract=0x4E58562C5CDa4B80633dD7a3C759e7702213675d -target=0x2C71210D239D472e963a7Be8362eCBdeD5337fE6 -spam 200 -ping

```

## 3. Cách nhắn tin qua lại

Sau khi cả 2 terminal kết nối thành công và hiện dòng chữ:
`💬 CHAT ĐÃ SẴN SÀNG! Bạn có thể gõ nội dung và nhấn Enter.`

Tại màn hình terminal sẽ xuất hiện dấu nhắc lệnh `>`. 
- **Gửi tin nhắn:** Bạn gõ trực tiếp nội dung tin nhắn vào terminal (ví dụ: `Alo, nghe rõ trả lời!`) và ấn phím **Enter**.
- **Nhận tin nhắn:** Cùng lúc đó, terminal của người kia sẽ tự động hiển thị:
  `[📥 NHẬN từ 0x...] (block time: ...): Alo, nghe rõ trả lời!`

Tương tự, người kia có thể gõ tin nhắn phản hồi trực tiếp vào terminal của họ và nhấn **Enter** để nhắn lại. Quá trình chat diễn ra theo thời gian thực (real-time) thông qua sự kiện (Event) của Smart Contract.

---
**Lưu ý:**
- Tool tự động nhận diện người nhận tin nhắn chéo cho nhau dựa trên file config (`user1.json` sẽ mặc định gửi cho `user2.json` và ngược lại).
