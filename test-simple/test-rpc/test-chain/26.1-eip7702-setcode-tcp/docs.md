# 26.1 — EIP-7702 ký hộ qua TCP protobuf

Account 0 (`private_keys[0]`) là relayer/from và trả gas. `bls_private_key` ký hash protobuf, chữ ký nằm trong trường `Transaction.Sign`; authority ký authorization cho phép gắn delegation code. Hai tài khoản bắt buộc khác nhau. Giao dịch được gửi bằng lệnh TCP `SendTransactionWithDeviceKey` với body `TransactionWithDeviceKey` protobuf, không gửi bằng `eth_sendRawTransaction`.

## Chạy

```bash
cd /home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-chain/26.1-eip7702-setcode-tcp
go run main.go -config=../config.json -data=data.json
```

Hoặc chạy integration test:

```bash
RUN_TCP_EIP7702=1 go test -v .
```

`go test .` mặc định skip integration test để không tự gửi giao dịch.

## Cấu hình

- `../config.json`: dùng trực tiếp cấu hình chung `test-chain/config.json`, không sao chép key sang folder test.
- `bls_private_key`: key ký BLS của giao dịch và dùng khởi tạo TCP client. Account 0 phải đăng ký public key tương ứng; test kiểm tra điều này trước khi gửi.
- `private_keys[0]`: suy ra địa chỉ `from` và ký `R/S/V` của giao dịch ngoài EIP-7702. `Sign` chứa chữ ký BLS, không chứa chữ ký secp256k1.
- `private_keys[1]`: authority ký authorization EIP-7702 bằng secp256k1.
- `tcp_node`: endpoint TCP của Node 0 (hoặc node duy nhất nếu chạy 1 node), ví dụ `192.168.1.223:6200`.
- `chain_id`: đọc từ cấu hình chung. `rpc_url` không được sử dụng. `parent_address` và `version` được dùng nếu có; version mặc định `0.0.1.0`.
- `data.json`: chỉ chứa dữ liệu giao dịch, không chứa key hoặc endpoint.
- `delegate`: địa chỉ implementation được authority ủy quyền. Mặc định `0x...7702` như test 26; test không deploy contract.
- `input_data`: calldata dạng hex, mặc định `0x`; `gas`: gas limit.
- `gas_tip_cap`, `gas_fee_cap`: phí tính bằng wei, mặc định trong data lần lượt 1 gwei và 20 gwei. Tip phải lớn hơn 0, fee cap phải >= tip và đủ đáp ứng phí mạng khi chạy.

Toàn bộ thao tác dùng kết nối TCP tại `tcp_node`: `GetChainId`, `GetAccountState`, `GetDeviceKey`, `SendTransactionWithDeviceKey` và receipt. Nonce, BLS public key, balance và code hash được đọc từ `AccountState`; phí lấy từ `data.json`. Không khởi tạo HTTP client. Relayer cần đủ tiền trả gas. Dùng riêng hai tài khoản trong lúc chạy để tránh thay đổi nonce/balance bởi giao dịch khác.

## Điều kiện PASS

1. Chain ID của TCP và cấu hình khớp nhau; BLS public key đăng ký trên account 0 khớp `bls_private_key`.
2. Device key cũ đọc theo `LastHash` phải khớp hash trong account state. Key mới là 32 byte ngẫu nhiên; `LastDeviceKey` và `NewDeviceKey` đều tham gia hash ký BLS.
3. Receipt TCP đúng hash nội bộ, from = relayer, to = authority; status RETURNED hoặc HALTED.
4. Code hash authority đúng `keccak256(0xef0100 || delegate)` qua `AccountState.SmartContractState.CodeHash` và nonce authority tăng đúng 1. Receipt thành công nhưng authorization bị bỏ qua sẽ không PASS.
5. Nonce relayer tăng đúng 1.
6. Với delegate chưa có code và calldata rỗng, balance authority giữ nguyên; balance relayer giảm đúng `receipt.GasUsed() × receipt.GasFee()` (`GasFee` của receipt native là giá mỗi gas, đơn vị wei/gas). Dùng tài khoản test riêng không nhận reward để kiểm tra delta chính xác.
7. `sender.DeviceKey` khớp key mới; `GetDeviceKey(txHash)` trả đúng raw key đã gửi.
8. Gas used > 0, không vượt gas limit; gas fee > 0. Nếu có `expected_return`, receipt return phải khớp byte-for-byte.

TCP receipt không có trường Ethereum transaction type; type `0x04` được đặt trong protobuf trước khi gửi. In cả hash protobuf và hash Ethereum để đối chiếu log.

Mặc định kiểm tra cài delegation và sponsorship; khi delegate không có code, chưa kiểm tra thực thi wallet logic. Có thể chỉ định implementation đã deploy và calldata tương ứng, và thêm `expected_return` (hex) trong data để kiểm tra giá trị trả về. Test không tự triển khai implementation hay xác minh mọi thay đổi storage nghiệp vụ.

## Tương thích protobuf

Generated proto của suite chưa có `AuthorizationList`. Test mã hóa trường này cục bộ qua protobuf unknown fields theo `metanode/execution/pkg/proto/transaction.proto`: tag 26 của `Transaction`, tag 21 của `TransactionHashData`. Các trường authorization dùng tag 1–6 đúng schema node; hash nội bộ bao gồm authorization. Không cần thay đổi proto hoặc TCP client dùng chung.

## Kiểm tra biên dịch

```bash
go build -o /tmp/metanode-26-1-setcode-tcp .
```

Build Go/Rust/FFI toàn hệ thống theo quy định repo:

```bash
cd /home/abc/nhat/con-chain-v2/metanode/consensus/metanode/scripts
./build_check.sh
```

TCP client hiện có tự retry kết nối khi node offline; dừng bằng Ctrl+C nếu cấu hình sai. Receipt dùng thời gian chờ của TCP client; deadline kiểm tra state TCP chỉ giới hạn test, không điều khiển consensus/dispatch commit. Test để lại delegation trên authority và tiêu thụ nonce/gas, có thể chạy lại với nonce được đọc mới.

## Verify offline giữa test-suite và node

Không cần bật node. Fixture dùng key test cố định, không đọc key trong config:

```bash
cd /home/abc/nhat/con-chain-v2/metanode-suite
TCP_SETCODE_FIXTURE=/tmp/metanode-tcp-setcode-fixture.json go test -v ./test-simple/test-rpc/test-chain/26.1-eip7702-setcode-tcp -run TestDeviceKeyWire
cd /home/abc/nhat/con-chain-v2/metanode/execution
TCP_SETCODE_FIXTURE=/tmp/metanode-tcp-setcode-fixture.json go test ./cmd/simple_chain/processor -run 'TestDeviceKeyHandlerMissingTransaction|TestTCPSetCodeSuiteFixture' -count=1
```

Node test gọi đúng handler TCP, kiểm tra payload vào queue còn type 4, authorization, device key, hash và chữ ký BLS/Ethereum. Các trường hợp sửa key cũ, key mới, authorization nonce/address hoặc chữ ký đều phải làm BLS verification thất bại. Wrapper rỗng/thiếu transaction hoặc protobuf lỗi phải bị từ chối.

Kiểm tra offline không chứng minh node đang chạy đã nhận/commit transaction; integration test mới kiểm tra receipt và state thực tế.
