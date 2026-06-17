### 📝 Tóm tắt: Test giới hạn sức mạnh (1 Node)
- **Loại test:** Đo công suất **TỐI ĐA (Peak TPS)** của hệ thống khi không bị độ trễ mạng P2P.
- **Cách test:** Bắn dồn dập 600,000 giao dịch chuyển tiền trực tiếp vào **1 Node duy nhất** (Chain mới reset rỗng).
- **Kết quả:**
  - 🚀 **Đỉnh (Max):** ~5,034 tx/s
  - 📉 **Đáy (Min):** ~1,758 tx/s
  - 📊 **Trung bình (Avg):** ~2,821 tx/s
- **Nhận xét:** Khi đẩy hết việc gom giao dịch cho 1 Node, mạng xử lý cực kỳ nhanh và mượt (trung bình gần 3,000 TPS). Đây là mức giới hạn sức mạnh tốt nhất để đem ra so sánh.
### 📝 Tóm tắt: Test tải mạng P2P (5 Nodes)
- **Loại test:** Đo công suất **THỰC TẾ (Sức chịu tải)** của mạng phân tán khi bị phân mảnh dữ liệu.
- **Cách test:** Bắn dồn dập 600,000 giao dịch rải đều (Load-balanced) lên **tất cả 5 Nodes** cùng một lúc.
- **Kết quả:**
  - 🚀 **Đỉnh (Max):** ~3,861 tx/s
  - 📉 **Đáy (Min):** ~1,282 tx/s
  - 📊 **Trung bình (Avg):** ~2,187 tx/s
- **Nhận xét:** Khi rải giao dịch ra 5 node, tốc độ trung bình giảm khoảng 22% (từ 2,821 xuống 2,187 TPS). Sự sụt giảm này là bắt buộc đối với blockchain do các node phải mất thêm thời gian truyền tay (gossip) giao dịch cho nhau qua mạng lưới (P2P network) để đồng bộ trước khi đóng block.

# 1 Node
╔═══════════════════════════════════════════════════╗
║  📊 BENCHMARK SUMMARY
╠═══════════════════════════════════════════════════╣
║  🔄 Rounds         : 30
║  📤 TXs per round  : 20000
║  ─────────────────────────────────────────────────
║  Round 1  TPS      : ~5034 tx/s
║  Round 2  TPS      : ~4532 tx/s
║  Round 3  TPS      : ~4078 tx/s
║  Round 4  TPS      : ~4098 tx/s
║  Round 5  TPS      : ~3477 tx/s
║  Round 6  TPS      : ~3457 tx/s
║  Round 7  TPS      : ~4218 tx/s
║  Round 8  TPS      : ~3134 tx/s
║  Round 9  TPS      : ~2680 tx/s
║  Round 10 TPS      : ~3619 tx/s
║  Round 11 TPS      : ~2969 tx/s
║  Round 12 TPS      : ~2952 tx/s
║  Round 13 TPS      : ~3615 tx/s
║  Round 14 TPS      : ~2528 tx/s
║  Round 15 TPS      : ~2572 tx/s
║  Round 16 TPS      : ~3018 tx/s
║  Round 17 TPS      : ~2647 tx/s
║  Round 18 TPS      : ~2088 tx/s
║  Round 19 TPS      : ~1983 tx/s
║  Round 20 TPS      : ~2088 tx/s
║  Round 21 TPS      : ~1855 tx/s
║  Round 22 TPS      : ~1919 tx/s
║  Round 23 TPS      : ~2037 tx/s
║  Round 24 TPS      : ~2201 tx/s
║  Round 25 TPS      : ~1811 tx/s
║  Round 26 TPS      : ~2554 tx/s
║  Round 27 TPS      : ~1758 tx/s
║  Round 28 TPS      : ~1856 tx/s
║  Round 29 TPS      : ~1840 tx/s
║  Round 30 TPS      : ~2018 tx/s
║  ─────────────────────────────────────────────────
║  📉 Min TPS        : ~1758 tx/s
║  📈 Max TPS        : ~5034 tx/s
║  📊 Avg TPS        : ~2821 tx/s
╚═══════════════════════════════════════════════════╝
# Multi node


╔═══════════════════════════════════════════════════╗
║  📊 BENCHMARK SUMMARY
╠═══════════════════════════════════════════════════╣
║  🔄 Rounds         : 30
║  📤 TXs per round  : 20000
║  ─────────────────────────────────────────────────
║  Round 1  TPS      : ~3861 tx/s
║  Round 2  TPS      : ~3401 tx/s
║  Round 3  TPS      : ~3699 tx/s
║  Round 4  TPS      : ~3090 tx/s
║  Round 5  TPS      : ~2850 tx/s
║  Round 6  TPS      : ~3549 tx/s
║  Round 7  TPS      : ~2763 tx/s
║  Round 8  TPS      : ~2881 tx/s
║  Round 9  TPS      : ~1777 tx/s
║  Round 10 TPS      : ~1840 tx/s
║  Round 11 TPS      : ~2024 tx/s
║  Round 12 TPS      : ~2132 tx/s
║  Round 13 TPS      : ~1978 tx/s
║  Round 14 TPS      : ~2260 tx/s
║  Round 15 TPS      : ~2302 tx/s
║  Round 16 TPS      : ~1927 tx/s
║  Round 17 TPS      : ~1944 tx/s
║  Round 18 TPS      : ~1794 tx/s
║  Round 19 TPS      : ~2223 tx/s
║  Round 20 TPS      : ~1562 tx/s
║  Round 21 TPS      : ~1910 tx/s
║  Round 22 TPS      : ~1841 tx/s
║  Round 23 TPS      : ~1346 tx/s
║  Round 24 TPS      : ~1339 tx/s
║  Round 25 TPS      : ~1282 tx/s
║  Round 26 TPS      : ~1549 tx/s
║  Round 27 TPS      : ~1349 tx/s
║  Round 28 TPS      : ~1430 tx/s
║  Round 29 TPS      : ~2180 tx/s
║  Round 30 TPS      : ~1513 tx/s
║  ─────────────────────────────────────────────────
║  📉 Min TPS        : ~1282 tx/s
║  📈 Max TPS        : ~3861 tx/s
║  📊 Avg TPS        : ~2187 tx/s
╚═══════════════════════════════════════════════════╝