# 28.1 - EIP-7702 Delegated Factory CREATE & Nonce Verification

## 📖 Mục tiêu bài test
Kiểm chứng cơ chế tăng nonce nâng cao của **EIP-7702**:
1. **Source Contract Solidity (`Factory.sol`)**:
   - Chứa contract `Factory` và `ChildContract`.
   - Hàm `createChild(uint256 _id)` thực thi opcode `CREATE` thông qua câu lệnh `new ChildContract(_id)`.
   - Hàm `fallback()` và `receive()` tự động gọi `createChild` khi nhận calldata hoặc native transfer.
2. **Ký Ủy quyền (Delegation)**:
   - Ví ký hộ (Authority / Ví 1) có nonce ban đầu N, ký `SetCodeAuthorization` tuple ủy quyền mã thực thi của mình sang `Factory`.
3. **Thực thi giao dịch ký hộ (Sponsored SetCodeTx)**:
   - Ví trả hộ (Relayer / Ví 0) gửi giao dịch EIP-7702 (Type 0x04) tới địa chỉ Ví ký hộ với calldata kích hoạt.
   - Ví trả hộ chịu toàn bộ chi phí gas (Gas Limit, MaxFeePerGas, GasTipCap).
4. **Kiểm tra Nonce tăng đúng 2 đơn vị (N -> N + 2)**:
   - Khi xử lý Authorization Tuple: Nonce ví ký hộ tăng +1 để chống tấn công phát lại (replay attack).
   - Khi EVM thực thi bytecode của Factory trên ngữ cảnh ví ký hộ: Lệnh `new ChildContract` (opcode `CREATE`) chạy, ví ký hộ đóng vai trò là deployer của contract con, làm nonce tăng thêm +1.
   - Kết quả: Nonce cuối cùng = Nonce ban đầu + 2 (ví dụ từ 1 lên 3, từ 3 lên 5, hoặc từ 5 lên 7).
5. **Kiểm tra Contract con**:
   - Địa chỉ contract con được tính bằng công thức: `CreateAddress(AuthorityAddress, NonceBanDau + 1)`.

## 📄 File hợp đồng trong thư mục
- `Factory.sol`: Mã nguồn Solidity của Factory và ChildContract.
- `Factory.bin` & `Factory.abi`: Bytecode và ABI đã biên dịch sẵn của Factory.
- `ChildContract.bin` & `ChildContract.abi`: Bytecode và ABI của ChildContract.

## 🚀 Cách chạy bài test

### Chạy trực tiếp qua go run:
```bash
cd metanode-suite/test-simple/test-rpc/test-chain/28.1-eip7702-factory-create-nonce
go run .
```

### Chạy qua go test:
```bash
cd metanode-suite/test-simple/test-rpc/test-chain/28.1-eip7702-factory-create-nonce
go test -v .
```

### Chạy kèm cấu hình mạng cụ thể (ví dụ chain_a):
```bash
TARGET_CHAIN=chain_a go run .
```
