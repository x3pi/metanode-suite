# Test Suite 33: Cross-Chain Root Anchor Architecture — P0 Verification

Bộ test suite này kiểm thử toàn diện toàn bộ các đặc tả kỹ thuật và bất biến của giai đoạn **P0** (Tasks P0.1, P0.2, P0.3) theo kiến trúc [Cross-Chain Root Anchor & Native Light-Client Bridge](file:///home/abc/nhat/consensus-chain/metanode/note/cross_chain_root_anchor_architecture.md).

---

## 1. Mục đích và Các bài test thành phần

### Phase 1: Task P0.1 — Schema & Global Supply Ledger Invariants
- **Schema Serialization Roundtrip:** Kiểm tra serialization/deserialization JSON & binary của 8 struct lõi: `CrossChainMessage`, `QuorumCert`, `MerkleProof`, `AssetEntry`, `Channel`, `AttestedCommit`, `GovernanceProposal`, `AccountLeaf`.
- **Property-based Fuzz Testing (10.000+ Mutations):** Thực hiện ngẫu nhiên hàng chục nghìn thao tác chuyển tiền phân bổ giữa Reserve chain và các sub-chain, bao gồm cả các hành vi cố tình rút quá số dư. Kiểm chứng bất biến:
  $$\sum \text{per\_chain\_allocation} \equiv \text{genesis\_total\_supply}$$
  luôn luôn được bảo toàn 100% trong mọi trường hợp.

### Phase 2: Task P0.2 — On-Chain Governance Engine (1-Chain-1-Vote & 72h Timelock)
- **1-Chain-1-Vote Quorum ($\ge 2/3$):** Xác thực công thức $\lceil 2N/3 \rceil = \lfloor (2N+2)/3 \rfloor$ dựa trên số lượng active chain (không phụ thuộc vào stake của chain) nhằm chống độc quyền quản trị.
- **Quy trình vòng đời đề xuất (Proposal Lifecycle):**
  1. `Propose` $\rightarrow$ trạng thái `Active`.
  2. Vote chưa đủ $2/3$ $\rightarrow$ gọi `execute()` bị chặn dứt khoát (`ProposalNotTimelocked`).
  3. Đủ $\ge 2/3$ votes $\rightarrow$ chuyển trạng thái sang `Timelocked`, kích hoạt thời gian đếm ngược 72 giờ (`effective_at = approved_at + 72h`).
  4. Gọi `execute()` trước khi hết 72 giờ $\rightarrow$ bị revert (`TimelockNotExpired`).
  5. Gọi `execute()` đúng hoặc sau 72 giờ $\rightarrow$ thực thi thành công và gán `executed = true`.
  6. Gọi lại `execute()` lần 2 $\rightarrow$ bị revert (`AlreadyExecuted` — đảm bảo tính idempotent).
  7. Bị từ chối khi cùng 1 chain vote 2 lần (`AlreadyVoted`) hoặc chain chưa đăng ký (`ChainNotRegistered`).

### Phase 3: Task P0.3 — BLS12-381 Proof-of-Possession (`PopVerify`) & Chống Rogue-Key Attacks
- **Tạo và xác minh PoP:** Ký Proof-of-Possession với Domain Separation Tag `BLS_POP_METANODE_ROOT_ANCHOR_V1:`.
- **Chống Rogue-Key Attacks:**
  - **Attack Case A:** Kẻ tấn công đăng ký public key của nạn nhân nhưng dùng chữ ký của kẻ tấn công $\rightarrow$ **REJECTED**.
  - **Attack Case B:** Kẻ tấn công tái sử dụng chữ ký PoP hợp lệ của nạn nhân cho public key mới của kẻ tấn công $\rightarrow$ **REJECTED**.
  - **Attack Case C:** Chữ ký PoP bị lỗi/hỏng $\rightarrow$ **REJECTED**.
### Phase 4: Task P1 (P1.1 & P1.2) — Root Anchor Genesis & 4-Founding-Chain Committee
- **P1.1 Genesis & 4 Founding Chains:** Khởi tạo Root Anchor Genesis với $\ge 4$ private chain sáng lập cùng góp validator đại diện (mỗi chain có trần stake tối đa $\le 33\%$).
- **P1.2 BFT Quorum Threshold & Chịu lỗi 1/4 Chain Offline (DoD):**
  - Công thức Quorum: $\text{Quorum} = 2f + 1 = \lfloor \frac{2 \times \text{TotalStake}}{3} \rfloor + 1$.
  - Mức chịu lỗi Byzantine tối đa: $f = \lfloor \frac{\text{TotalStake}-1}{3} \rfloor$.
  - **Kịch bản 1/4 Chain Offline:** Khi 1 chain sáng lập ($25\%$ stake $\le f = 33.3\%$) bị offline/ngắt kết nối $\rightarrow$ $3$ chain còn lại ($75\%$ stake $\ge 66.7\%$) vẫn đạt đủ Quorum $\ge 2f+1$ để sinh Quorum Certificate và tiếp tục vận hành liên tục!
### Phase 5: Task P2 (P2.1 – P2.8) — `GatewayPrecompile` & Cơ chế Thực thi Liên Chuỗi
- **P2.1 & P2.5 (`outbound` & `hop_count`):** Khởi tạo message, tính `messageId = txHash`, khoá tip, chấp nhận `hop_count = 6` và **chặn cứng `hop_count = 7`**.
- **P2.2 (`attestCommit` & Kịch bản 10.7):** Xác thực chữ ký BLS `QuorumCert`, khớp `epoch`, kiểm tra trần phân bổ ngân sách: khi `aggregateAmount > per_chain_allocation` $\rightarrow$ **REJECT**, bắn event `AllocationRejected`, không ghi ledger.
- **P2.3 (`claimMessage` & Chống Double-Claim):** Xác minh Merkle proof vào `commitRoot`, đổi trạng thái sang `SUCCESS`, và **chặn đứng Double-Claim** cùng `messageId` (idempotent).
- **P2.4 (Đường hoàn tiền Refund):** Khi đích FAILED $\rightarrow$ hoàn tiền về nguồn cho `sender`, **chặn double-refund**.
- **P2.6 (`getOriginalSender` Context):** Thiết lập execution context cho contract nhận, chặn giả mạo ngoài Gateway.
- **P2.7 (`verifyAndExecute`):** Thực thi nguyên tử 1 message cho trường hợp lẻ tẻ/khối lượng thấp.
- **P2.8 (`claimDeadChainBalance`):** Cho phép người dùng tự claim số dư qua Merkle proof khi chain bị khai tử (`declare_dead`), chặn double-claim.

### Phase 6: Task P3 (P3.1 – P3.2) — Mở rộng Epoch Transition & StateRoot Checkpoint
- **P3.1 (`CommitteeUpdate`):** Chuyển tiếp epoch tuần tự (`epoch -> epoch + 1`), xác thực chữ ký uỷ ban cũ $\ge 2f+1$, xác thực PoP uỷ ban mới và cập nhật `ChainRegistry`. Chặn nhảy cóc epoch và chặn chữ ký uỷ ban cũ không hợp lệ.
- **P3.2 (`StateRootCheckpoint` & AccountTree):** Xây dựng cây Merkle số dư của tất cả tài khoản tại thời điểm chốt epoch, xuất và xác thực Merkle inclusion proofs. Tích hợp **Tamper Guard** phát hiện và từ chối ngay lập tức khi số dư hoặc địa chỉ bị chỉnh sửa.

---




## 2. Cách chạy bài test

### Chạy trực tiếp với cấu hình mặc định (Timelock 1 phút = 60s):
```bash
cd /home/abc/nhat/consensus-chain/metanode-suite/test-simple/test-rpc/test-blockstm/33-cross-chain-p0-root-anchor
go run main.go --config ../config.json --timelock-sec 60
```

### Chạy chế độ Đếm ngược Thời gian thực (Real-time Live Countdown):
Chế độ này sẽ thực sự chờ đếm ngược từng giây (ví dụ 60 giây hoặc 10 giây) để minh họa trực quan việc gọi `execute()` trong lúc đếm ngược sẽ bị chặn và sau khi hết đếm ngược sẽ thành công:
```bash
go run main.go --config ../config.json --timelock-sec 60 --realtime-wait
```

### Các tuỳ chọn nâng cao:
- **Tùy chỉnh thời gian timelock bất kỳ (ví dụ chuẩn 72h = 259.200 giây hoặc 10 giây):**
  ```bash
  go run main.go --config ../config.json --timelock-sec 259200
  ```
- **Tăng số vòng Fuzz mutations (mặc định 10.000 mutations):**
  ```bash
  go run main.go --config ../config.json --fuzz-ops 50000
  ```
- **Bỏ qua kiểm tra RPC node trực tiếp (chỉ chạy offline tests):**
  ```bash
  go run main.go --config ../config.json --skip-rpc
  ```

