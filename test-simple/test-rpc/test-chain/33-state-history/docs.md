# Test 33: State History Verification on RPC Archive Nodes

## Mục Đích
Kiểm tra khả năng lưu trữ và truy vấn State History (lịch sử trạng thái tài khoản) trên các Node có cấu hình `rpc_nodes` (Archive / Full RPC nodes) được định nghĩa trong `inventory.yml` và đồng bộ vào `config.json`.

## Cơ Chế Kiểm Tra
1. **Mốc Block A:** Gửi giao dịch từ ví kiểm tra, sau khi được confirm tại Block A, lưu lại số dư (`eth_getBalance`), nonce (`eth_getTransactionCount`), và `mtn_getAccountState`.
2. **Mốc Block B:** Gửi thêm các giao dịch phụ để tăng block number và thay đổi số dư/nonce của ví.
3. **Đối chiếu lịch sử:**
   - Truy vấn dữ liệu tài khoản tại mốc Block A và Block B trên **tất cả các node RPC** được bật (`rpc_nodes`).
   - Đảm bảo dữ liệu tại Block A phản ánh đúng 100% mốc lịch sử lúc Block A, không bị trôi state về giá trị mới nhất của Block B.
   - Đảm bảo tính nhất quán giữa API tiêu chuẩn Ethereum (`eth_*`) và API chuyên biệt của Metanode (`mtn_*`).
