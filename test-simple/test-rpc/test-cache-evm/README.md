# EVM State Cache/Drift Testing Tool

Thư mục này chứa các kịch bản kiểm thử nhằm phát hiện và tái hiện lỗi **State Drift (Trôi nổi trạng thái)** trong EVM của blockchain Metanode. Cụ thể là kiểm tra xem cơ chế cache số dư (balance) của tài khoản trong EVM (thường ở lớp C++/Rust FFI) có bị đồng bộ sai lệch giữa các giao dịch gọi hợp đồng (Smart Contract calls) và các giao dịch chuyển tiền native (Native Value Transfer) hay không.

---

## 📌 Tổng quan về Lỗi State Drift (EVM Cache Bug)

Khi một tài khoản thực hiện chuỗi giao dịch hỗn hợp:
1. **Tx 1 (Contract Call):** Gọi Smart Contract (ví dụ: `deposit`). EVM load tài khoản vào cache và ghi nhận số dư ban đầu (`bal1`).
2. **Tx 2 (Native Transfer):** Chuyển tiền native từ tài khoản đó sang tài khoản khác. Giao dịch này thay đổi số dư tài khoản trên State DB gốc nhưng **không** thông qua EVM execution.
3. **Tx 3 (Contract Call):** Gọi lại Smart Contract (ví dụ: `step3`). EVM đọc lại số dư của tài khoản (`bal3`).

Nếu cơ chế cache của EVM không được làm mới (invalidated/refreshed) sau giao dịch native (Tx 2), Tx 3 sẽ sử dụng lại dữ liệu cache cũ từ Tx 1. Khi đó, `bal3` sẽ bằng `bal1` (hoặc không bị trừ đi số tiền của Tx 2), dẫn đến **State Drift Bug**. Lỗi này cực kỳ nguy hiểm vì có thể dẫn đến tấn công Double Spend hoặc sai lệch trạng thái tài chính của người dùng.

---

## 🛠️ Chi tiết các file kiểm thử

### 1. `main.go` (Kiểm thử tuần tự - Sequential Test)
* **Mục đích:** Kiểm tra xem lỗi State Drift có xảy ra khi các giao dịch được gửi và đóng block hoàn toàn tuần tự hay không.
* **Quy trình hoạt động:**
  1. Đọc cấu hình `config.json` để kết nối tới RPC Node.
  2. Deploy Smart Contract `BalanceChecker` (`test_drift.sol`).
  3. Tự động gọi `register_bls` để tạo một tài khoản ví tạm thời (`dummyReceiver`) để tránh xung đột nonce và số dư của ví Master.
  4. Chuyển `10 coin` từ Master sang `dummyReceiver`.
  5. Tạo và gửi lần lượt các giao dịch từ ví `dummyReceiver`:
     * **Tx 1 (Deposit):** Gọi hàm `deposit()` gửi `1 coin` vào contract để ghi nhận số dư vào biến `bal1`. Chờ nhận receipt.
     * **Tx 2 (Transfer):** Chuyển native `8 coin` trở lại ví Master. Chờ nhận receipt.
     * **Tx 3 (Step 3):** Gọi hàm `step3()` để đọc lại số dư và lưu vào biến `bal3`. Chờ nhận receipt.
  6. Sử dụng `CallContract` (`eth_call`) để truy vấn giá trị `bal1` và `bal3` từ contract.
  7. **So sánh kết quả:** Nếu `bal3` không giảm tương ứng sau khi đã chuyển đi `8 coin` ở Tx 2, chương trình sẽ báo lỗi **`PHÁT HIỆN BUG (STATE DRIFT)`**.

### 2. `main-paralel.go` (Kiểm thử song song - Concurrency/Same-block Test)
* **Mục đích:** Kiểm tra xem lỗi State Drift có xảy ra khi các giao dịch được gửi đồng thời và được đóng chung vào một Block hay không. Ở môi trường TPS cao, việc xử lý song song hoặc gom nhóm giao dịch trong block dễ phát sinh lỗi cache state nếu thứ tự cập nhật state db và cache evm không đồng bộ.
* **Quy trình hoạt động:**
  1. Tương tự như `main.go` nhưng chuyển cho ví `dummyReceiver` nhiều tiền hơn (`20 coin`).
  2. Chuẩn bị 4 giao dịch từ ví `dummyReceiver` với các nonce tăng dần liên tục:
     * **Tx 1 (Deposit):** Gửi `1 coin` vào contract.
     * **Tx 1_1 (Deposit):** Gửi `1 coin` vào contract.
     * **Tx 2 (Transfer):** Chuyển native `8 coin` trở lại ví Master.
     * **Tx 3 (Step 3):** Gọi hàm `step3()` để đọc lại số dư.
  3. **Bắn đồng loạt:** Gửi cả 4 giao dịch lên RPC Node cùng một lúc (song song) không đợi receipt giữa các lệnh gửi nhằm ép các giao dịch này vào cùng 1 block.
  4. Đợi nhận receipt của cả 4 giao dịch.
  5. Truy vấn `bal1` và `bal3`.
  6. **So sánh kết quả:** Kiểm tra xem EVM trong cùng block có cập nhật đúng số dư của ví gửi sau khi thực hiện chuyển tiền native hay không.

---

## 📑 Hợp đồng thông minh sử dụng (`test_drift.sol`)

Hợp đồng ghi nhận số dư của tài khoản gọi (`msg.sender.balance`) tại các thời điểm khác nhau:

```solidity
pragma solidity ^0.8.0;

contract BalanceChecker {
    uint256 public bal1;
    uint256 public bal3;
    uint256 public contractBalance;

    function deposit() public payable {
        bal1 = msg.sender.balance; // Lưu số dư tại Tx 1
        contractBalance = address(this).balance;
    }

    function step3() public {
        bal3 = msg.sender.balance; // Lưu số dư tại Tx 3
    }
}
```

---

## 🚀 Cách chạy thử nghiệm

1. **Chuẩn bị file `config.json`** ở thư mục hiện tại:
   ```json
   {
     "rpc_url": "http://localhost:8545",
     "address": "0x...",
     "private_key": "0x...",
     "chain_id": 12345
   }
   ```
2. **Đảm bảo các file abi/bin đã được compile:**
   * `test_drift_BalanceChecker.abi`
   * `test_drift_BalanceChecker.bin`
3. **Chạy kịch bản kiểm thử:**
   * Chạy kiểm thử tuần tự:
     ```bash
     go run main.go
     ```
   * Chạy kiểm thử song song / cùng block:
     ```bash
     go run main-paralel.go
     ```
