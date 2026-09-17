# Test 34: WebSocket Smart Contract Call & Subscription

## 1. Mục đích bài test
Bài test này kiểm tra khả năng tương tác trực tiếp với Smart Contract qua cổng WebSocket của node blockchain (Node 1: `ws://192.168.1.234:10747/ws`):
1. **Kết nối WebSocket RPC (`rpc.Dial`)**: Xác thực bắt tay (handshake) và duy trì kết nối persistent stream.
2. **Truy vấn cơ bản qua WS (`eth_blockNumber`, `eth_chainId`)**: Kiểm tra các API JSON-RPC cơ bản qua giao thức WebSocket.
3. **Đăng ký sự kiện thời gian thực (`eth_subscribe`)**:
   - `newHeads`: Nhận thông báo block mới được đào.
   - `logs` (`SubscribeFilterLogs`): Lắng nghe Event từ Smart Contract.
4. **Gọi hàm đọc Contract (`eth_call` qua WS)**: Gọi method view/pure (`getCount()`) trên contract đã triển khai.
5. **Gọi hàm ghi/thực thi Contract (`eth_sendRawTransaction` qua WS)**: Gửi giao dịch gọi method thay đổi trạng thái (`increment()`) qua WebSocket.

## 2. Phân tích hiện trạng & Kiến trúc Metanode
- **Read Call (`eth_call`)**: Hoạt động bình thường qua WebSocket vì `MetaAPI.Call` chấp nhận `rawInput json.RawMessage` và tự động phân giải cấu trúc JSON-RPC chuẩn.
- **Subscriptions (`eth_subscribe`)**: Hoạt động bình thường.
- **Write Call (`eth_sendRawTransaction`)**: 
  - Giao dịch gửi qua HTTP (`POST /`) thành công vì có `ethSendRawTxMiddleware` chặn bắt và chuyển tiếp vào `customAPI.SendRawEthTransaction` (1 tham số hex).
  - Giao dịch gửi qua WebSocket (`/ws`) hiện tại đi thẳng vào `server.WebsocketHandler`, chạm vào hàm `MetaAPI.SendRawTransaction(ctx, input, inputEth, pubKeyBlsL)` đòi hỏi 3 tham số. Client Ethereum chuẩn chỉ gửi 1 tham số hex dẫn tới lỗi `missing value for required argument 1`.

## 3. Cách chạy test
```bash
cd /home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-chain/34-ws-contract-call
go run main.go
# hoặc với go test:
go test -v -count=1 .
```
Tuỳ chọn truyền URL WebSocket:
```bash
go run main.go --ws=ws://192.168.1.234:10747/ws
```
