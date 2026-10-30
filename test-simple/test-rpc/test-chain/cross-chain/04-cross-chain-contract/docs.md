# Test 04: Gọi & Tương Tác Smart Contract Xuyên Chuỗi (Cross-Chain Contract)

## 1. Mục Đích
Bài test này xác thực khả năng tương tác Smart Contract xuyên chuỗi:
1. Triển khai hợp đồng thông minh EVM (`TestCounter`) trên Execution Cluster đích (Exec 2).
2. Phát sóng lệnh kích hoạt / cấp vốn xuyên chuỗi từ Exec 1 sang Smart Contract trên Exec 2 thông qua lớp neo Parent Chain.
3. Thực thi chuyển đổi trạng thái (state transition - `increment()`) trên hợp đồng tại Exec 2 bằng nguồn lực và phí gas xuyên chuỗi.
4. Kiểm tra sự toàn vẹn của Precompile Gateway Contract (`0x0000000000000000000000000000000000001002`) và định lý bảo toàn tổng cung (Conservation Check).

---

## 2. Quy Trình Thực Thi
1. **Kiểm tra kết nối**: Đo lường trạng thái RPC của Exec 1 (`http://192.168.1.223:8747`) và Exec 2 (`http://192.168.1.223:8750`).
2. **Deploy Contract**: Đưa bytecode của `TestCounter` lên Exec 2 và xác định địa chỉ contract.
3. **Cross-Chain Activation**: Gọi `mtn_sendCrossChainTransfer` từ Exec 1 nhắm tới địa chỉ Smart Contract trên Exec 2.
4. **State Transition**: Gọi hàm `increment()` và kiểm tra giá trị `getCount()` tăng lên.
5. **Conservation**: Xác thực biến động `diff`, `float`, `supply` trên cả 2 cụm.

---

## 3. Cách Chạy Độc Lập
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-chain/cross-chain/04-cross-chain-contract
go run .
# hoặc:
go test -v .
```
