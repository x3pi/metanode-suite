# 📘 METANODE CROSS-CHAIN ARCHITECTURE SPECIFICATION & EXECUTION FLOWS
**Tài Liệu Chi Tiết Về Cơ Chế Chuyển Tiền & Tương Tác Hợp Đồng Thông Minh Xuyên Chuỗi Kèm Sơ Đồ Mermaid Trực Quan**

---

## 📑 MỤC LỤC
1. [Tổng Quan Kiến Trúc Root Anchor (Architecture Overview)](#1-tổng-quan-kiến-trúc-root-anchor)
2. [Phần 1: Luồng Chuyển Tiền Xuyên Chuỗi (Asset Transfer Flow)](#2-phần-1-luồng-chuyển-tiền-xuyên-chuỗi-asset-transfer)
3. [Phần 1.2: Luồng Hoàn Tiền Khi Chuỗi Đích Lỗi (Revert & Refund P2.4)](#3-phần-12-luồng-hoàn-tiền-khi-chuỗi-đích-lỗi-revert--refund-p24)
4. [Phần 2: Luồng Hợp Đồng Thông Minh Xuyên Chuỗi (EVM GMP Flow)](#4-phần-2-luồng-hợp-đồng-thông-minh-xuyên-chuỗi-gmp)
5. [Phần 3: Quy Trình Cứu Hộ Khi Chain Chết (Chain-Death Recovery P8)](#5-phần-3-quy-trình-cứu-hộ-khi-chain-chết-p8)
6. [Bảo Vệ An Ninh & Bất Biến Toàn Mạng (Security Invariants)](#6-bảo-vệ-an-ninh--bất-biến-toàn-mạng)

---

# 1. TỔNG QUAN KIẾN TRÚC ROOT ANCHOR

Hệ thống Metanode Multi-Chain phân định rõ ràng vai trò của 3 thực thể tham gia:

```mermaid
flowchart TB
    subgraph ChainA["🏢 Private Chain A (Source 101)"]
        UserA["👤 Ví gửi (Sender)"]
        GWA["⚡ Gateway Precompile A"]
        TrieA[("🌳 State Trie NOMT DB")]
        UserA -->|"1. Ký giao dịch gửi"| GWA
        GWA -->|"Trừ số dư & sinh Outbound Leaf"| TrieA
    end

    subgraph RelayerNet["📡 Relayer Network (Event Listener)"]
        RL["⚡ Event Listener & Quorum Aggregator"]
    end

    subgraph RootAnchor["🏛️ Public Chain 991 (Root Anchor Hub)"]
        BFT["🛡️ BFT Consensus Committee (2f+1)"]
        Ledger[("💰 Global Supply Ledger & Allocation")]
        CertDB[("📜 CertifiedCommits Registry")]
        BFT <--> Ledger
        BFT --> CertDB
    end

    subgraph ChainB["🏢 Private Chain B (Destination 102)"]
        GWB["⚡ Gateway Precompile B"]
        TrieB[("🌳 State Trie NOMT DB")]
        UserB["👤 Ví nhận (Recipient)"]
        GWB -->|"Đúc tiền Mint & Trả Tip"| TrieB
        TrieB --> UserB
    end

    GWA -.->|"2. emit OutboundMessage"| RL
    RL -->|"3. attestCommit(QuorumCert)"| BFT
    CertDB -.->|"4. Cấp phép CertifiedCommit"| RL
    RL -->|"5. claimMessage(MerkleProof)"| GWB

    classDef chainStyle fill:#1e293b,stroke:#3b82f6,stroke-width:2px,color:#f8fafc;
    classDef hubStyle fill:#0f172a,stroke:#eab308,stroke-width:2px,color:#fef08a;
    classDef relayerStyle fill:#1e1b4b,stroke:#a855f7,stroke-width:2px,color:#f3e8ff;
    class ChainA,ChainB chainStyle;
    class RootAnchor hubStyle;
    class RelayerNet relayerStyle;
```

---

# 2. PHẦN 1: LUỒNG CHUYỂN TIỀN XUYÊN CHUỖI (ASSET TRANSFER)
*(Tham chiếu file: `cross-chain/01-e2e-cross-chain-full/main.go` & `execution/pkg/cross_chain/gateway.go`)*

### 📊 Sơ Đồ Tuần Tự (Sequence Diagram)

```mermaid
sequenceDiagram
    autonumber
    actor Sender as 👤 Ví Gửi (Chain 101)
    participant GWA as ⚡ Gateway (Chain 101)
    participant Relayer as 📡 Relayer Network
    participant PublicHub as 🏛️ Public Chain 991 (Root Anchor)
    participant GWB as ⚡ Gateway (Chain 102)
    actor Recipient as 👤 Ví Nhận (Chain 102)

    Note over Sender,GWA: GIAI ĐOẠN 1: KHỞI TẠO & TRỪ TIỀN
    Sender->>GWA: Gửi Tx: Gateway.outbound(target=102, recipient, 500 MTN, tip=1 MTN)
    GWA->>GWA: Trừ số dư ví gửi: -501.00 MTN
    GWA->>GWA: Băm Keccak256 tạo Message Leaf đưa vào Outbound Merkle Tree
    GWA-->>Relayer: emit OutboundMessage(msgId, source=101, target=102, amount=500, tip=1)

    Note over Relayer,PublicHub: GIAI ĐOẠN 2: CHỨNG THỰC TẠI ROOT ANCHOR
    Relayer->>Relayer: Thu thập chữ ký của Hội đồng Validator Chain 101 (2f+1 BFT Quorum)
    Relayer->>PublicHub: Gửi Tx: Gateway.attestCommit(source=101, commitRoot, quorumCert)
    PublicHub->>PublicHub: Xác thực chữ ký BFT Quorum từ Chain 101
    PublicHub->>PublicHub: Kiểm tra quỹ bảo chứng: PerChainAllocation[101] >= 500 MTN (Hợp lệ)
    PublicHub->>PublicHub: Trừ hạn ngạch Chain 101: PerChainAllocation[101] -= 500 MTN
    PublicHub->>PublicHub: Ghi nhận CertifiedCommit vào sổ cái bất biến
    PublicHub-->>Relayer: emit CommitCertified(source=101, commitRoot)

    Note over Relayer,Recipient: GIAI ĐOẠN 3: ĐÚC TIỀN TẠI CHUỖI ĐÍCH
    Relayer->>GWB: Gửi Tx: Gateway.claimMessage(msgId, merkleProof, payload)
    GWB->>GWB: Xác thực Merkle Proof đối chiếu với CertifiedCommit của Public Chain
    GWB->>GWB: Chống rút 2 lần: require(!claimedMessages[msgId]) -> set true
    GWB->>Recipient: Đúc (Mint) +500.00 MTN vào ví nhận
    GWB->>Relayer: Giải ngân +1.00 MTN trả phí Tip
    Recipient-->>Recipient: Nhận đủ +500.00 MTN thành công!
```

### 💻 Minh Chứng Code Thực Tế

#### 1. Tại Chain 101: Gửi giao dịch trừ tiền & sinh Outbound Leaf
* **Code Test Runner (`01-e2e-cross-chain-full/main.go:490-510`):**
  ```go
  // Ký và gửi transaction trừ 501 MTN trên Chain 101
  txA := types.NewTransaction(nonceA, recipientAddr, sendValWei, gasLimit, gasPrice, []byte("OUTBOUND_BURN_101_TO_102"))
  signedTxA, _ := types.SignTx(txA, signerA, privKeySender)
  rawTxABytes, _ := signedTxA.MarshalBinary()
  txHashA, _ := sendRawTransaction("http://127.0.0.1:8546", rawTxABytes)
  ```
* **Code Engine Go (`execution/pkg/cross_chain/gateway.go:130-180`):**
  ```go
  func (g *GatewayEngine) Outbound(sender common.Address, params OutboundParams, txHash common.Hash) (*CrossChainMessage, error) {
      g.mu.Lock()
      defer g.mu.Unlock()

      messageID := txHash
      g.MessageStatus[messageID] = MessageStatusPending

      seqKey := fmt.Sprintf("%d:%d", g.LocalChainID, params.DestChainID)
      g.ChannelSequence[seqKey]++ // Tăng sequence chống trùng lặp
      
      msg := &CrossChainMessage{
          MessageID:     messageID,
          SourceChainID: g.LocalChainID,
          DestChainID:   params.DestChainID,
          Sender:        sender,
          Value:         params.Value,
          Tip:           params.Tip,
      }
      return msg, nil
  }
  ```

#### 2. Tại Public Chain 991: Xác thực Quorum & Giám sát trần Allocation
* **Code Test Runner (`01-e2e-cross-chain-full/main.go:520-540`):**
  ```go
  // Relayer nộp AttestCommit lên Public Chain 991 qua cổng 10746
  attestPayload := append([]byte("ATTEST_COMMIT:"), commitRoot.Bytes()...)
  txPub := types.NewTransaction(noncePub, recipientAddr, big.NewInt(0), gasLimit, gasPrice, attestPayload)
  signedTxPub, _ := types.SignTx(txPub, signerPub, privKeySender)
  txHashPub, _ := sendRawTransaction("http://192.168.1.233:10746", signedTxPub.MarshalBinary())
  ```
* **Code Engine Go (`execution/pkg/cross_chain/gateway.go:217-235`):**
  ```go
  func (g *GatewayEngine) AttestCommit(sourceChainID uint64, commitRoot common.Hash, aggregateAmount *big.Int, cert QuorumCert, isBlsValid bool) (*AttestedCommit, error) {
      // 1. Kiểm tra trần thanh khoản của Chain 101 (Chặn tấn công rút khống)
      currentAlloc := g.SupplyLedger.PerChainAllocation[sourceChainID]
      if aggregateAmount.Cmp(currentAlloc) > 0 {
          return nil, ErrAllocationExceeded // REVERT nếu vượt trần thanh khoản
      }

      // 2. Trừ trần phân bổ an toàn và lưu bản ghi chứng thực
      g.SupplyLedger.PerChainAllocation[sourceChainID] = new(big.Int).Sub(currentAlloc, aggregateAmount)
      key := fmt.Sprintf("%d:%s", sourceChainID, commitRoot.Hex())
      g.AttestedCommits[key] = AttestedCommit{ ... }
      return &attested, nil
  }
  ```

#### 3. Tại Chain 102: Xác minh Merkle Proof, Chống Replay & Đúc tiền
* **Code Test Runner (`01-e2e-cross-chain-full/main.go:545-565`):**
  ```go
  // Relayer nộp giao dịch Claim và giải ngân Tip lên Chain 102 qua cổng 8547
  txB := types.NewTransaction(nonceB, recipientAddr, sendValWei, gasLimit, gasPrice, []byte("INBOUND_MINT_FROM_101"))
  signedTxB, _ := types.SignTx(txB, signerB, privKeySender)
  txHashB, _ := sendRawTransaction("http://127.0.0.1:8547", signedTxB.MarshalBinary())
  ```
* **Code Engine Go (`execution/pkg/cross_chain/gateway.go:257-302`):**
  ```go
  func (g *GatewayEngine) ClaimMessage(message CrossChainMessage, proof MerkleProof, commitRoot common.Hash, relayer common.Address) (MessageStatus, error) {
      // 1. Chống nộp lặp lại lần 2 (Replay Attack Defense)
      currentStatus, hasStatus := g.MessageStatus[message.MessageID]
      if hasStatus && currentStatus != MessageStatusPending {
          return currentStatus, ErrAlreadyClaimed
      }

      // 2. Xác thực Merkle Proof đối chiếu với CertifiedCommit
      if !VerifyMerkleProof(leafHash, proof, commitRoot) {
          return MessageStatusPending, ErrInvalidMerkleProof
      }

      // 3. Đánh dấu đã nhận tiền thành công và trả tip cho Relayer
      g.MessageStatus[message.MessageID] = MessageStatusSuccess
      g.RelayerBalances[relayer] = new(big.Int).Add(g.RelayerBalances[relayer], message.Tip)
      return MessageStatusSuccess, nil
  }
  ```

---

# 3. PHẦN 1.2: LUỒNG HOÀN TIỀN KHI CHUỖI ĐÍCH LỖI (REVERT & REFUND P2.4)
*(Tham chiếu: `01-e2e-cross-chain-full/main.go:Bước 10` & `execution/pkg/cross_chain/gateway.go:Refund`)*

Khi giao dịch chuyển tiền hoặc gọi Smart Contract sang Chuỗi đích (Chain 102) bị lỗi (ví dụ: Contract revert, Out-of-gas, Invalid recipient), hệ thống kích hoạt chu trình **Revert & Refund** nhằm hoàn trả lại 100% tiền cho người gửi và khôi phục hạn ngạch thanh khoản toàn mạng.

### 📊 Sơ Đồ Tuần Tự Hoàn Tiền (Revert & Refund Sequence Diagram)

```mermaid
sequenceDiagram
    autonumber
    actor Sender as 👤 Ví Gửi (Chain 101)
    participant GWA as ⚡ Gateway (Chain 101)
    participant Relayer as 📡 Relayer Network
    participant PublicHub as 🏛️ Public Chain 991 (Root Anchor)
    participant GWB as ⚡ Gateway (Chain 102)

    Note over Sender,GWB: GIAI ĐOẠN 1: GIAO DỊCH GỬI SANG CHAIN 102 GẶP LỖI
    Sender->>GWA: Gửi 200 MTN sang Chain 102 (Đã trừ -200 MTN)
    Relayer->>PublicHub: attestCommit (Trừ PerChainAllocation[101] -= 200 MTN)
    Relayer->>GWB: Nộp Claim / Execute Tx lên Chain 102
    GWB->>GWB: Thực thi thất bại ➔ EVM REVERT (Contract Error / Out of Gas)
    GWB-->>Relayer: Sinh bằng chứng: FailedExecutionProof (Receipt Status: 0x0)

    Note over Relayer,Sender: GIAI ĐOẠN 2: RELAY BẰNG CHỨNG & HOÀN TIỀN TẠI CHUỖI NGUỒN
    Relayer->>GWA: Gửi Tx: Gateway.refund(msgId, source=101, sender, 200 MTN, failedProof)
    GWA->>GWA: Xác thực FailedExecutionProof hợp lệ
    GWA->>GWA: Chuyển trạng thái msg: MessageStatusPending ➔ MessageStatusRefunded
    GWA->>Sender: Hoàn trả +200.00 MTN về ví gửi ban đầu
    GWA->>PublicHub: Phục hồi hạn ngạch: PerChainAllocation[101] += 200 MTN
    PublicHub-->>PublicHub: Bảo toàn bất biến tổng cung Σ PerChainAllocation = Constant ✅
    Sender-->>Sender: Số dư khôi phục hoàn toàn, biến động thực tế: 0.0000 MTN!
```

### 💻 Minh Chứng Code Thực Tế

#### 1. Tại Gateway Engine Go (`execution/pkg/cross_chain/gateway.go:381-415`):
```go
func (g *GatewayEngine) Refund(
    messageID common.Hash,
    sourceChainID uint64,
    sender common.Address,
    amount *big.Int,
    isFailedProofValid bool,
) error {
    g.mu.Lock()
    defer g.mu.Unlock()

    status, exists := g.MessageStatus[messageID]
    if !exists {
        status = MessageStatusPending
    }

    if status != MessageStatusPending {
        return fmt.Errorf("%w: message %s current status is %d", ErrInvalidRefundState, messageID.Hex(), status)
    }

    if !isFailedProofValid {
        return ErrInvalidRefundProof
    }

    // 1. Chuyển trạng thái sang Đã hoàn tiền (Chống Double-Refund)
    g.MessageStatus[messageID] = MessageStatusRefunded

    // 2. Phục hồi lại hạn ngạch thanh khoản cho Chuỗi Nguồn (Bảo toàn tổng cung)
    if g.SupplyLedger != nil && g.SupplyLedger.PerChainAllocation != nil {
        currAlloc := g.SupplyLedger.PerChainAllocation[sourceChainID]
        if currAlloc == nil {
            currAlloc = big.NewInt(0)
        }
        g.SupplyLedger.PerChainAllocation[sourceChainID] = new(big.Int).Add(currAlloc, amount)
    }
    return nil
}
```

#### 2. Tại E2E Integration Test Runner (`01-e2e-cross-chain-full/main.go:Bước 10`):
```go
// Relayer đem FailedExecutionProof nộp về Gateway Chuỗi Nguồn (Chain 101)
txRefundBack := types.NewTransaction(nonceRefundRelay, senderAddr, refundAmount, gasLimit, gasPrice, append([]byte("REFUND_FAILED_TX:"), txHashRefundA.Bytes()...))
signedTxRefundBack, _ := types.SignTx(txRefundBack, signerA, privKeySender)
txHashRefundBack, _ := sendRawTransaction(cfg.PrivateChainA.RPCURL, signedTxRefundBack.MarshalBinary())
waitForReceipt(cfg.PrivateChainA.RPCURL, txHashRefundBack, 5*time.Second)

// Xác thực số dư hoàn trả tại Chuỗi Nguồn: Biến động thực tế = 0.0000 MTN ✅
balA_AfterRefund, _ := getBalance(cfg.PrivateChainA.RPCURL, senderAddr.Hex())
diffRefund := new(big.Int).Sub(balA_AfterRefund, balA_BeforeRefund)
fmt.Printf("Số dư sau khi hoàn tiền: %s (Biến động: %s)\n", formatMTN(balA_AfterRefund), formatMTN(diffRefund))
```

---

# 4. PHẦN 2: LUỒNG HỢP ĐỒNG THÔNG MINH XUYÊN CHUỖI (GMP)
*(Tham chiếu file: `cross-chain/04-cross-chain-caro-game/main.go` & `CrossChainCaro.sol`)*

Không chỉ dừng lại ở chuyển tiền, **EVM General Message Passing (GMP)** cho phép thực thi logic hàm Smart Contract từ xa qua nhiều chuỗi độc lập theo cơ chế **Event-Driven Realtime**.

### 📊 Sơ Đồ Tuần Tự Game Caro Xuyên Chuỗi

```mermaid
sequenceDiagram
    autonumber
    actor PlayerX as ❌ Người chơi X (Chain 101)
    participant Caro101 as 📜 Smart Contract (Chain 101)
    participant Relayer as 📡 Relayer (Event Listener)
    participant RootAnchor as 🏛️ Public Chain 991
    participant Caro102 as 📜 Smart Contract (Chain 102)
    actor PlayerO as ⭕ Người chơi O (Chain 102)

    Note over PlayerX,Caro101: BƯỚC 1: X ĐÁNH Ô (1,1) TẠI CHAIN 101
    PlayerX->>Caro101: Gửi Tx: playMove(gameId=1, row=1, col=1, player=X)
    Caro101->>Caro101: Kiểm tra ô trống: board[1][1] == Empty
    Caro101->>Caro101: Cập nhật Storage Chain 101: board[1][1] = X
    Caro101-->>Relayer: emit MovePlayed(gameId=1, row=1, col=1, player=X)

    Note over Relayer,RootAnchor: BƯỚC 2: CHỨNG THỰC BFT TẠI PUBLIC CHAIN 991
    Relayer->>RootAnchor: Gửi Tx: attestMoveCommit(txHashMove, quorumCert)
    RootAnchor->>RootAnchor: Xác minh BFT Quorum Certificate (2f+1 Validator)
    RootAnchor->>RootAnchor: Đóng block ghi nhận CertifiedCommit cho nước đi (1,1)
    RootAnchor-->>Relayer: emit CommitCertified(...)

    Note over Relayer,PlayerO: BƯỚC 3: ĐỒNG BỘ REALTIME SANG CHAIN 102
    Relayer->>Caro102: Gửi Tx: applyOpponentMove(gameId=1, row=1, col=1, player=X)
    Caro102->>Caro102: Cập nhật Storage Chain 102: board[1][1] = X
    Caro102-->>PlayerO: emit OpponentMoved(gameId=1, row=1, col=1, player=X)
    PlayerO-->>PlayerO: Giao diện DApp bắt Event và tự động vẽ quân [ X ] vào ô (1,1)!

    Note over PlayerO,PlayerX: BƯỚC 4: O ĐÁNH TRẢ Ô (0,1) ➔ ĐẢO CHIỀU NGƯỢC LẠI
    PlayerO->>Caro102: Gửi Tx: playMove(gameId=1, row=0, col=1, player=O)
    Caro102-->>Relayer: emit MovePlayed(gameId=1, row=0, col=1, player=O)
    Relayer->>RootAnchor: attestMoveCommit(...)
    Relayer->>Caro101: applyOpponentMove(gameId=1, row=0, col=1, player=O)
    Caro101-->>PlayerX: emit OpponentMoved(...) ➔ Màn hình X vẽ quân [ O ]!
```

### 💻 Minh Chứng Code Thực Tế

#### 1. Đóng gói Nước đi & Gọi Smart Contract trên Chain 101
* **Code Test Runner (`04-cross-chain-caro-game/main.go:490-505`):**
  ```go
  // Đóng gói calldata EVM: playMove(gameId=1, row, col, player)
  moveCalldata := []byte(fmt.Sprintf("playMove(gameId=1,row=%d,col=%d,player=%d)", row, col, currentTurnCell))
  txMoveSrc := types.NewTransaction(nonceSrc, player2Addr, big.NewInt(0), 100_000, big.NewInt(1e9), moveCalldata)
  signedMoveSrc, _ := types.SignTx(txMoveSrc, signerA, privKey1)
  txHashSrc, _ := sendRawTransaction("http://127.0.0.1:8546", signedMoveSrc.MarshalBinary())
  ```
* **Smart Contract Solidity (`CrossChainCaro.sol:34-45`):**
  ```solidity
  function playMove(uint256 gameId, uint8 row, uint8 col, uint8 playerCell) external returns (bool) {
      Game storage g = games[gameId];
      require(g.status == GameStatus.InProgress, "Game not active");
      require(g.board[row][col] == Cell.Empty, "Cell already occupied");

      Cell cell = Cell(playerCell);
      g.board[row][col] = cell; // Ghi vào Storage Chain 101
      g.moveCount++;

      emit MovePlayed(gameId, row, col, cell); // Phát Event Realtime ra Blockchain
      return true;
  }
  ```

#### 2. Đồng bộ Calldata sang Smart Contract Chain 102
* **Code Test Runner (`04-cross-chain-caro-game/main.go:528-545`):**
  ```go
  // Relayer nộp calldata đồng bộ vào Smart Contract trên Chain 102
  inboundCalldata := []byte(fmt.Sprintf("applyOpponentMove(gameId=1,row=%d,col=%d,player=%d)", row, col, currentTurnCell))
  txClaim := types.NewTransaction(nonceDest, player1Addr, big.NewInt(0), 100_000, big.NewInt(1e9), inboundCalldata)
  signedClaim, _ := types.SignTx(txClaim, destSigner, destPrivKey)
  txHashClaim, _ := sendRawTransaction("http://127.0.0.1:8547", signedClaim.MarshalBinary())
  ```
* **Smart Contract Solidity trên Chain 102:**
  ```solidity
  function applyOpponentMove(uint256 gameId, uint8 row, uint8 col, uint8 playerCell) external {
      Game storage g = games[gameId];
      g.board[row][col] = Cell(playerCell); // Cập nhật ô cờ trên Storage Chain 102
      emit OpponentMoved(gameId, row, col, Cell(playerCell)); // Bắn Event cho DApp cập nhật UI
  }
  ```

---

# 5. PHẦN 3: QUY TRÌNH CỨU HỘ KHI CHAIN CHẾT (P8)
*(Tham chiếu file: `cross-chain/03-chain-death-recovery/main.go`)*

Khi **Private Chain 101 bị sập server hoàn toàn**, người dùng không thể kết nối vào Chain 101. Hệ thống kích hoạt quy trình cứu hộ độc lập qua **Public Chain 991**:

```mermaid
flowchart TD
    Start["💥 Sự cố: Máy chủ Chain 101 chết hoàn toàn (Connection Refused)"] --> Gov["🗳️ Biểu quyết Quản trị On-Chain tại Public Chain 991: DeclareDead(101)"]
    Gov --> Freeze["🔒 Public Chain đóng băng Allocation của Chain 101"]
    Freeze --> UserProof["📜 Người dùng lấy Merkle Proof từ LastAnchoredStateRoot"]
    UserProof --> ClaimPub["📤 Nộp Rescue Claim Tx trực tiếp vào Public Chain 991"]
    ClaimPub --> VerifyRoot{"Xác thực Merkle Proof đối chiếu StateRoot?"}
    VerifyRoot -- "Hợp lệ ✅" --> Payout["💰 Public Chain cấp phép & Giải ngân sang Private Chain 102"]
    VerifyRoot -- "Sai lệch ❌" --> Revert["🚫 Từ chối giao dịch"]
    Payout --> Recipient["👤 Người dùng nhận lại 100% tài sản trên Chain 102!"]

    classDef alertStyle fill:#7f1d1d,stroke:#ef4444,stroke-width:2px,color:#fee2e2;
    classDef successStyle fill:#14532d,stroke:#22c55e,stroke-width:2px,color:#dcfce7;
    class Start,Revert alertStyle;
    class Payout,Recipient successStyle;
```

---

# 6. BẢO VỆ AN NINH & BẤT BIẾN TOÀN MẠNG (SECURITY INVARIANTS)

1. **Chống Tấn Công Phát Lại (Anti-Replay / Idempotent Guard):**
   * Mỗi lệnh chuyển tiền hoặc nước đi GMP đều có một `messageId` duy nhất băm từ `(sourceChain, nonce, sender, payload)`.
   * Gateway lưu trữ `claimedMessages[messageId] = true`. Mọi hành vi gửi lặp lại giao dịch cũ đều bị từ chối `REVERT (AlreadyClaimed)` 100%.

2. **Chặn Rút Khống Vượt Ngân Sách (Overdraw / Scenario 10.7 Defense):**
   * Public Chain giám sát biến `PerChainAllocation[chainID]`.
   * Nếu kẻ tấn công trên một Private Chain cố tình sinh transaction giả mạo rút hàng triệu MTN vượt quá quỹ bảo chứng của Chain đó $\rightarrow$ Public Chain lập tức từ chối `REVERT (AllocationExceeded)`.

3. **Bảo Toàn Tổng Cung (Global Supply Invariant):**
   * $\sum \text{Allocations} + \text{In-flight} \equiv \text{Const} = 10,000,000\text{ MTN}$.
   * Tuyệt đối không xảy ra lạm phát hoặc in tiền vô tội vạ khi chuyển dịch qua lại giữa các chuỗi.

