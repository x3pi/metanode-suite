# 📊 Báo Cáo Kết Quả Test Block-STM
**Thời gian chạy:** 2026-08-07 07:45:29

## 📈 Thống Kê Chung
- **Tổng số bài test**: 24
- **Tổng số lượt chạy**: 72
- **✅ Lượt thành công**: 28
- **❌ Lượt thất bại**: 1

## 🚨 Chi Tiết Lỗi & Nguyên Nhân
### ❌ Bài Test Thất Bại: `10-cross-contract-payable_run2`
- **Hiện tượng / Lỗi thực tế (Actual)**: Lỗi biên dịch hoặc chương trình Crash (Exit code: 1)
- **Nguyên nhân dự đoán**: Core Node bị crash hoặc Panic trong quá trình thực thi Block-STM (panic ở chan, bộ nhớ, vv.).
- **Kết quả kỳ vọng (Expected)**: Node xử lý mượt mà không crash, giao dịch được confirm thành công trên các node và giá trị State cập nhật chính xác tuyệt đối.
- **File Log Chi Tiết**: [`10-cross-contract-payable_run2.log`](./test_logs/10-cross-contract-payable_run2.log)

