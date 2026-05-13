Deleted test-snap-shot.md
Deleted test_snapshot_pipeline.sh
Ran command: `chmod +x ./test-snapshot.sh`
Ran command: `./test_snapshot_pipeline.sh --node 1 --loops 5 --tps-rounds 10`
Ran command: `./test_snapshot.sh --node 1 --loops 5 --tps-rounds 10`
Ran command: `chmod +x ./test-snapshot.sh`
Ran command: `./test_snapshot.sh --node 1 --loops 5 --tps-rounds 10`
Edited test-snapshot.md
Viewed test-snapshot.sh:51-85

Đoạn code này đóng vai trò là một **vòng lặp giám sát (monitoring loop)** để đảm bảo rằng nếu có lỗi lệch hash xảy ra trong lúc đang bơm giao dịch, script sẽ dừng lại ngay lập tức thay vì chạy lãng phí thời gian.

Dưới đây là giải thích chi tiết từng dòng:

1. **`while kill -0 $TPS_PID 2>/dev/null; do`**
   - Lệnh `kill -0` thực chất KHÔNG giết tiến trình. Nó chỉ gửi một tín hiệu rỗng để **kiểm tra xem tiến trình đó (ở đây là tiến trình chạy TPS) có đang hoạt động hay không**.
   - Ý nghĩa: "Chừng nào tiến trình bơm giao dịch (`TPS_PID`) vẫn còn đang chạy, thì tiếp tục vòng lặp này".

2. **`if ! kill -0 $CHECKER_PID 2>/dev/null; then`**
   - Bên trong vòng lặp, nó kiểm tra liên tục xem tiến trình `block_hash_checker` (`CHECKER_PID`) có còn sống hay không. Dấu `!` phía trước mang ý nghĩa phủ định (NOT).
   - Tiến trình `block_hash_checker` được thiết kế để tự động thoát (bị kill/crash) nếu nó phát hiện ra mạng lưới bị lệch hash (hash divergence). 
   - Ý nghĩa: "Nếu tiến trình checker ĐÃ CHẾT, thì thực thi khối lệnh báo lỗi bên trong".

3. **`echo "❌ LỖI NGHIÊM TRỌNG..."` và `tail -n 20 ...`**
   - In ra thông báo lỗi màu đỏ cho bạn biết mạng lưới vừa bị lệch hash.
   - Dùng lệnh `tail -n 20` để trích xuất 20 dòng log cuối cùng của file log checker in thẳng ra màn hình để bạn có thể xem được ngay mã lỗi, số block và Node bị phân nhánh mà không cần mở file.

4. **`kill -9 $TPS_PID 2>/dev/null` và `exit 1`**
   - Khi đã phát hiện lỗi, nó dùng `kill -9` để ép buộc tắt tiến trình bơm giao dịch TPS để không spam thêm rác vào mạng lưới.
   - Gọi lệnh `exit 1` để dừng toàn bộ file bash (dừng toàn bộ pipeline script hiện tại), báo hiệu là tiến trình test thất bại.

5. **`sleep 2`**
   - Vòng lặp sẽ lặp lại mỗi 2 giây một lần để tiết kiệm tài nguyên CPU.

**Tóm lại:** Khối code này giúp script không bị "mù". Thay vì đứng im chờ cái TPS bơm xong 20,000 giao dịch (có thể mất vài phút) rồi mới check kết quả, thì nó vừa chờ TPS vừa "liếc" sang coi thằng Checker có báo động (chết) hay không, có lỗi cái là cắt cầu dao (dừng TPS, báo lỗi) liền.