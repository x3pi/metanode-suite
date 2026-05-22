# Snapshot Architecture — Hướng dẫn chi tiết cho người mới

**Cập nhật lần cuối:** 2026-05-22

## Mục lục
0. [**Thứ tự hàm được gọi**](#0-thứ-tự-hàm-được-gọi) ← **Đọc mục này trước!**
1. [Snapshot là gì và tại sao cần?](#1-snapshot-là-gì-và-tại-sao-cần)
2. [Kiến trúc tổng quan](#2-kiến-trúc-tổng-quan)
3. [Khi nào snapshot được tạo?](#3-khi-nào-snapshot-được-tạo)
4. [Luồng tạo Snapshot (createAtomicSnapshot)](#4-luồng-tạo-snapshot-createatomicsnapshot)
5. [Ba phương pháp tạo Snapshot](#5-ba-phương-pháp-tạo-snapshot)
6. [Cấu trúc thư mục Snapshot](#6-cấu-trúc-thư-mục-snapshot)
7. [HTTP Snapshot Server](#7-http-snapshot-server)
8. [Luồng phục hồi Node từ Snapshot (restore_node.sh)](#8-luồng-phục-hồi-node-từ-snapshot-restore_nodesh)
9. [Điều gì xảy ra khi Node khởi động sau khi restore?](#9-điều-gì-xảy-ra-khi-node-khởi-động-sau-khi-restore)
10. [Cơ chế bảo vệ & các vấn đề thường gặp](#10-cơ-chế-bảo-vệ--các-vấn-đề-thường-gặp)
11. [Các thuật ngữ quan trọng](#11-các-thuật-ngữ-quan-trọng)

---

## 0. Thứ tự hàm được gọi

> Bản đồ code chính xác cho 3 luồng: **khởi tạo**, **tạo snapshot**, và **restore node**.

### A. Luồng khởi tạo hệ thống snapshot (1 lần khi start)

```
NewApp()                             app.go:94
└── app.initBlockchain()             app.go:184
    └── InitSnapshotSystem()           snapshot_init.go:20
        │
        ├── [Nếu disabled / SUB node]
        │   ├── &SnapshotManager{enabled: false}   ← dummy manager
        │   └── storage.SetBlockCommitCallback()    ← chỉ log rotation
        │
        └── [Nếu MASTER + enabled=true]
            ├── NewSnapshotManager(dataDir, snapshotDir, 2, 20)  snapshot_manager.go:102
            │   ├── detectReflinkSupport(dataDir)   ← kiểm tra btrfs/xfs
            │   └── [PANIC nếu ext4 mà bật snapshot]
            │
            ├── sm.SetSnapshotFrequency(cfg.SnapshotFrequencyBlocks)
            ├── sm.SetSnapshotBlockOffset(cfg.SnapshotBlockOffset)
            │
            ├── sm.SetForceFlushCallback()      ← storageMgr.FlushAll()
            ├── sm.SetCheckpointCallback()      ← storageMgr.CheckpointAll() + CheckpointChangelogs()
            ├── sm.SetNomtSnapshotCallback()    ← mt_trie.SnapshotAllNomtDBs()
            │
            ├── storage.SetBlockCommitCallback(func(blockNumber) {
            │       ├── sm.DetectEpochChange()         ← epoch boundary trigger?
            │       ├── sm.ForceSnapshotNow()          ← nếu epoch change
            │       └── sm.OnBlockCommitted()          ← mọi block commit
            │   })
            │
            └── StartSnapshotServer(snapshotDir, port, sm)  snapshot_server.go:184
                └── go server.Start()               ← HTTP :8700 (goroutine riêng)
```

### B. Luồng trigger + tạo snapshot (mỗi N block hoặc epoch change)

```
[Mỗi block commit trong Go]
blockProcessor.commitWorker()
└── storage.UpdateLastBlockNumber(blockNumber)
    └── [gọi tất cả BlockCommitCallbacks]
        └── callback(blockNumber)             ← được đăng ký ở InitSnapshotSystem
            ├── sm.DetectEpochChange()            snapshot_manager.go
            │   └── [Nếu epoch mới] ForceSnapshotNow(blockNumber, epoch)
            │       └── go sm.CreateSnapshot() / CreateHybridSnapshot() / CreateRsyncSnapshot()
            │
            └── sm.OnBlockCommitted(blockNumber)  snapshot_manager.go:296
                ├── [KIỂM TRA isStandardTrigger] blockNumber >= epochBoundary + 20 ?
                ├── [KIỂM TRA isPeriodicTrigger] (blockNumber - offset) % frequency == 0 ?
                └── [Nếu trigger] sm.isCreating = true
                    └── go func() {                      ← goroutine riêng, không block
                        ├── createAtomicSnapshot(epoch, blockNumber, boundaryBlock)  snapshot_manager.go:381
                        │   │
                        │   ├── PHASE 0: FREEZE
                        │   │   ├── pauseCallback()           ← Go: acquires ExecutionMutex, drain queue
                        │   │   ├── forceFlushCallback()      ← storageMgr.FlushAll()
                        │   │   └── rustPauseCallback()       ← Rust: bắt đầu 30s watchdog
                        │   │
                        │   ├── PHASE 0.5: CAPTURE STATE
                        │   │   ├── storage.GetLastBlockNumber()
                        │   │   ├── storage.GetLastGlobalExecIndex()
                        │   │   ├── stateRootCallback()       ← NOMT.Root()
                        │   │   └── stakeRootCallback()       ← NOMTStake.Root()
                        │   │
                        │   ├── PHASE 1: PebbleDB CHECKPOINT
                        │   │   └── checkpointCallback(snapshotPath)
                        │   │       ├── storageMgr.CheckpointAll(destPath)
                        │   │       └── chainState.CheckpointChangelogs(destPath)
                        │   │           ├── stateChangelogDB.Checkpoint(destPath/changelog_db_account)
                        │   │           └── stakeChangelogDB.Checkpoint(destPath/changelog_db_stake)
                        │   │
                        │   ├── PHASE 1.5: NOMT SNAPSHOT
                        │   │   └── nomtSnapshotCallback(snapshotPath, useReflink)
                        │   │       └── mt_trie.SnapshotAllNomtDBs()
                        │   │           ├── [Btrfs/XFS] reflinkCopyDir()   ← instant
                        │   │           └── [ext4]      parallelCopyDir(8)  ← cần thời gian
                        │   │
                        │   ├── PHASE 4: WRITE METADATA
                        │   │   ├── calculateDirectoryChecksum()   ← snapshot_verify.go
                        │   │   └── os.WriteFile(metadata.json)
                        │   │
                        │   └── defer RESUME (LUÔN chạy)
                        │       ├── rustResumeCallback()      ← Rust resume TRƯỚC
                        │       └── resumeCallback()          ← Go resume SAU
                        │
                        └── RotateSnapshots()             ← xóa snapshots cũ nếu > maxSnapshots
                    }()
```

### C. Luồng restore node từ snapshot

```
restore_node.sh <node_id>           scripts/node/restore_node.sh
│
├── [1] stop_node.sh                 ← dừng tmux sessions + pkill
├── [2] rm -rf data/ back_up/          ← xóa data cũ
│       rm -rf config/storage/node_X  ← xóa Rust DAG (CRITICAL!)
│
├── [3] Tải + giải nén snapshot
│   ├── curl /api/snapshots → chọn snapshot mới nhất
│   ├── [Network] wget -c -r http://node:8700/files/snap_name/
│   │
│   ├── copy folders: snap/* → nodeX/data/data/
│   ├── copy back_up: snap/back_up/* → nodeX/back_up/
│   ├── copy epoch_data_backup.json   ← CRITICAL
│   ├── rm -rf data/data/rust_consensus  ← anti-split-brain
│   └── find ... -name "LOCK" -delete   ← xóa stale locks
│
├── [4] Validate
│   ├── json.load(epoch_data_backup.json)
│   └── kiểm tra blocks/, nomt_db/, transaction_state/
│
├── [5] Start node (tuần tự)
│   ├── tmux new-session: go-master-X → ./simple_chain -config=...
│   ├── tmux new-session: go-sub-X    → ./simple_chain -config=...
│   └── [Rust tự khởi động qua supervisor, chờ Go is_ready]
│
├── [6] Giám sát sync 120s
│   ├── [mỗi 10s] eth_getBlockByNumber("latest")
│   └── so sánh block local vs reference node
│
└── [7] Anti-fork check
    ├── eth_getBlockByNumber(check_block) → restored node
    ├── eth_getBlockByNumber(check_block) → reference node
    └── so sánh hash: MATCH ✓ | MISMATCH ✗ FORK!
```

### Tóm tắt thứ tự hàm khi tạo snapshot

| # | Hàm | File | Mục đích |
|:---:|---|---|---|
| 1 | `OnBlockCommitted(N)` | `snapshot_manager.go:296` | Trigger mỗi block |
| 2 | `createAtomicSnapshot()` | `snapshot_manager.go:381` | Orchestrate toàn bộ |
| 3 | `pauseCallback()` | đăng ký từ `app_blockchain.go` | Dừng Go execution |
| 4 | `forceFlushCallback()` | `storageMgr.FlushAll()` | Flush RAM → disk |
| 5 | `rustPauseCallback()` | từ consensus layer | Dừng Rust write |
| 6 | `storage.GetLastBlockNumber()` | `snapshot_manager.go:468` | Lấy state thực tế |
| 7 | `checkpointCallback(dest)` | `snapshot_init.go:126` | PebbleDB + Changelog checkpoint |
| 8 | `storageMgr.CheckpointAll()` | `storage_manager.go` | Checkpoint tất cả PebbleDB |
| 9 | `chainState.CheckpointChangelogs()` | `chain_state.go` | Checkpoint lịch sử |
| 10 | `nomtSnapshotCallback(dest, reflink)` | `snapshot_init.go:135` | NOMT snapshot |
| 11 | `mt_trie.SnapshotAllNomtDBs()` | `nomt_state_trie.go` | Copy NOMT files |
| 12 | `calculateDirectoryChecksum()` | `snapshot_verify.go:62` | Tính SHA256 |
| 13 | `os.WriteFile(metadata.json)` | `snapshot_manager.go:734` | Lưu metadata |
| 14 | `rustResumeCallback()` | consensus layer | Resume Rust TRƯỚC |
| 15 | `resumeCallback()` | `app_blockchain.go` | Resume Go SAU |
| 16 | `RotateSnapshots()` | `snapshot_manager.go:744` | Xóa snapshot cũ |

---

## 1. Snapshot là gì và tại sao cần?

### Vấn đề
Khi một node mới tham gia mạng (hoặc node cũ bị hỏng dữ liệu), nó cần có toàn bộ dữ liệu blockchain từ Block 0 đến hiện tại. Nếu phải **tính lại từ đầu**, sẽ tốn rất nhiều thời gian — mạng đang chạy block 100,000, node mới cần xử lý lại 100,000 block!

### Giải pháp — Snapshot
Snapshot là một **bản chụp toàn bộ dữ liệu blockchain** tại một thời điểm nhất định. Node mới chỉ cần:
1. Tải snapshot về (~vài phút thay vì vài ngày)
2. Bật lên và đồng bộ thêm phần còn lại (từ thời điểm snapshot đến nay)

### Snapshot chứa gì?
```
snap_epoch_5_block_1250/
├── account_state/       ← Số dư, nonce của tất cả tài khoản (NOMT DB)
├── blocks/              ← Tất cả block headers và bodies
├── receipts/            ← Kết quả giao dịch
├── transaction_state/   ← Mapping tx hash → block
├── stake_db/            ← Thông tin validator/stake
├── nomt_db/             ← Merkle tree state (Btrfs/XFS reflink)
├── back_up/             ← PebbleDB backup + epoch_data_backup.json
├── changelog_db_account/ ← Lịch sử thay đổi account state theo block
├── changelog_db_stake/  ← Lịch sử thay đổi stake state theo block
└── metadata.json        ← Thông tin snapshot (epoch, block, checksum...)
```

---

## 2. Kiến trúc tổng quan

```
┌─────────────────────────────────────────────────────────────────┐
│                    SnapshotManager (Go)                         │
│                                                                 │
│  ┌─────────────────┐    ┌─────────────────────────────────┐    │
│  │  Trigger Module  │    │      createAtomicSnapshot()     │    │
│  │                 │    │                                  │    │
│  │ • EpochBoundary │───►│  Phase 0: FREEZE execution      │    │
│  │ • PeriodicBlock │    │  Phase 1: PebbleDB Checkpoint   │    │
│  │   (N blocks)   │    │  Phase 1.5: NOMT Snapshot       │    │
│  └─────────────────┘    │  Phase 2: Xapian copy (Hybrid) │    │
│                         │  Phase 4: Write metadata.json   │    │
│  ┌─────────────────┐    │  Defer: RESUME execution        │    │
│  │  SnapshotServer  │    └─────────────────────────────────┘    │
│  │  (HTTP :8700)   │                                            │
│  │ • /api/snapshots│    ┌─────────────────────────────────┐    │
│  │ • /files/<name>│    │        RotateSnapshots()         │    │
│  └─────────────────┘    │  Giữ lại max 2 snapshots gần nhất    │
│                         └─────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
```

**File liên quan:**
- [snapshot_init.go](file:///home/abc/nhat/con-chain-v2/metanode/execution/executor/snapshot_init.go) — Khởi tạo hệ thống snapshot
- [snapshot_manager.go](file:///home/abc/nhat/con-chain-v2/metanode/execution/executor/snapshot_manager.go) — Logic tạo/xoay vòng snapshot
- [snapshot_server.go](file:///home/abc/nhat/con-chain-v2/metanode/execution/executor/snapshot_server.go) — HTTP server phục vụ tải snapshot
- [snapshot_verify.go](file:///home/abc/nhat/con-chain-v2/metanode/execution/executor/snapshot_verify.go) — Xác minh tính toàn vẹn
- [restore_node.sh](file:///home/abc/nhat/con-chain-v2/metanode/consensus/metanode/scripts/node/restore_node.sh) — Script phục hồi node

---

## 3. Khi nào snapshot được tạo?

Có **2 cơ chế trigger**, được kiểm tra mỗi khi có block mới commit:

**File:** [snapshot_manager.go:296](file:///home/abc/nhat/con-chain-v2/metanode/execution/executor/snapshot_manager.go#L296)

```go
func (sm *SnapshotManager) OnBlockCommitted(blockNumber uint64) {
    // Cơ chế 1: Sau epoch transition
    isStandardTrigger := sm.snapshotPending &&
        blockNumber >= (sm.epochBoundaryBlock + uint64(sm.blocksAfterEpoch))

    // Cơ chế 2: Định kỳ theo block (stagger để tránh tất cả node snapshot cùng lúc)
    // Formula: (blockNumber - offset) % frequency == 0
    // VD: frequency=500, node0 offset=0 → snap tại 500, 1000, 1500...
    //                    node1 offset=100 → snap tại 600, 1100, 1600...
    isPeriodicTrigger = (blockNumber - sm.blockOffset) % sm.frequencyBlocks == 0
}
```

### Cơ chế 1: Epoch Boundary Trigger
```
Epoch 4 → Epoch 5 (boundary_block = 1200)
                    │
                    ├── Block 1200: Epoch transition!
                    │   └── OnEpochAdvanced(1200, 5)
                    │       └── snapshotPending = true
                    │
                    ├── Block 1201..1219: Đợi...
                    │
                    └── Block 1220 (= 1200 + blocksAfterEpoch=20):
                        └── TRIGGER SNAPSHOT! 📸
```

> **Tại sao phải đợi 20 blocks?** Ngay sau epoch transition, consensus có thể còn đang ổn định (leader change, committee change). Chờ thêm 20 blocks để đảm bảo trạng thái đã hoàn toàn ổn định trước khi chụp.

### Cơ chế 2: Periodic Trigger (Staggered)
Đảm bảo **không bao giờ có 2 node snapshot cùng lúc** để tránh mất quorum consensus:

```
frequency = 500 blocks, 5 nodes
Node 0 (offset=0):   snap at 500, 1000, 1500...
Node 1 (offset=100): snap at 600, 1100, 1600...
Node 2 (offset=200): snap at 700, 1200, 1700...
Node 3 (offset=300): snap at 800, 1300, 1800...
Node 4 (offset=400): snap at 900, 1400, 1900...
```

Bao giờ cũng chỉ có tối đa 1 node snapshot ở bất kỳ thời điểm nào.

---

## 4. Luồng tạo Snapshot (createAtomicSnapshot)

Đây là trái tim của toàn bộ hệ thống. Yêu cầu quan trọng nhất: snapshot phải **atomic** — nhất quán tại một thời điểm, không bị pha trộn với block đang xử lý dở.

**File:** [snapshot_manager.go:381](file:///home/abc/nhat/con-chain-v2/metanode/execution/executor/snapshot_manager.go#L381)

```
createAtomicSnapshot(epoch=5, blockNumber=1250, boundaryBlock=1200)
│
├─ PHASE 0: FREEZE TOÀN BỘ EXECUTION
│   ├─ pauseCallback() → Dừng Go Master (acquires ExecutionMutex)
│   │   └─ Drain toàn bộ commit queue → memory state = disk state
│   │
│   ├─ forceFlushCallback() → Flush tất cả memory tables xuống ổ đĩa
│   │   └─ PebbleDB MemTable → SST files (không còn gì trong RAM)
│   │
│   └─ rustPauseCallback() → Dừng Rust consensus writing (30s timer)
│       └─ Rust sẽ tự resume nếu snapshot mất > 30s (watchdog)
│
├─ PHASE 0.5: CAPTURE ATOMIC STATE (trong khi execution bị freeze)
│   ├─ actualBlockNumber = storage.GetLastBlockNumber() → 1250
│   ├─ actualGEI = storage.GetLastGlobalExecIndex() → 8750
│   ├─ actualStateRoot = NOMT.Root() → "0xabc123..."
│   └─ actualStakeRoot = NOMTStake.Root() → "0xdef456..."
│
├─ PHASE 1: PebbleDB CHECKPOINT (atomic, near-instant)
│   ├─ checkpointCallback(snapshotPath)
│   │   ├─ storageMgr.CheckpointAll(destPath)   ← Checkpoint tất cả PebbleDB
│   │   └─ chainState.CheckpointChangelogs(destPath)  ← Checkpoint changelog DBs
│   │       ├─ changelog_db_account/ → snapshot
│   │       └─ changelog_db_stake/  → snapshot
│   └─ Tất cả SST files được hard-link (0 giây, 0 dung lượng thêm)
│
├─ PHASE 1.5: NOMT SNAPSHOT
│   └─ nomtSnapshotCallback(snapshotPath, useReflink)
│       └─ mt_trie.SnapshotAllNomtDBs()
│           ├─ Lock NOMT database
│           ├─ Trên Btrfs/XFS: cp --reflink (instant copy)
│           ├─ Trên ext4:      parallel Go copy (8 goroutines)
│           └─ Unlock NOMT database
│
├─ [Hybrid only] PHASE 2: Copy Xapian dirs
│   └─ Xapian không phải database binary → cần copy thực sự
│
├─ PHASE 4: Write metadata.json
│   └─ {epoch, block_number, state_root, GEI, checksums...}
│
└─ defer: RESUME (luôn chạy kể cả khi có lỗi)
    ├─ rustResumeCallback()  ← Resume Rust TRƯỚC
    └─ resumeCallback()      ← Resume Go SAU
```

> **Vì sao resume Rust trước?** Rust chỉ pause 30 giây (watchdog). Nếu resume Go trước → Go bắt đầu nhận block mới → Rust vẫn còn pause → mismatch. Resume Rust trước đảm bảo thứ tự an toàn.

---

## 5. Ba phương pháp tạo Snapshot

| Method | Cách hoạt động | Dùng khi nào |
|--------|---------------|-------------|
| **hardlink** | Hard-link toàn bộ LevelDB files (instant, shared inode) | Default — ổ đĩa bình thường |
| **rsync** | `rsync -a --delete src/ dest/` | Cần sync toàn bộ thư mục kể cả file cấu hình |
| **hybrid** | Checkpoint PebbleDB + reflink/copy Xapian | Hệ thống có Xapian search engine |

### Tại sao PebbleDB dùng Checkpoint thay vì hardlink?

PebbleDB là append-only database với compaction. Nếu hardlink một file đang bị compact (xóa và viết lại), bản hardlink sẽ giữ inode cũ → data inconsistency.

**PebbleDB Checkpoint** sử dụng cơ chế nội bộ của database: tạo hard-link các SST files đang tồn tại và đảm bảo tất cả WAL (Write-Ahead Log) được flush. Kết quả là một thư mục database độc lập, nhất quán, an toàn.

```
Original PebbleDB/          Checkpoint/
├── 000001.sst ──────────── 000001.sst (hard-link, share inode)
├── 000002.sst ──────────── 000002.sst (hard-link)
├── 000003.sst (being       [EXCLUDED - unsafe file]
│   compacted)
└── MANIFEST ─────── copy ─ MANIFEST
```

### Filesystem yêu cầu
Hệ thống yêu cầu **Btrfs hoặc XFS** nếu bật snapshot (`reflink` support). Trên ext4, NOMT snapshot phải copy song song bằng 8 goroutines và mất nhiều thời gian hơn. Nếu ổ đĩa không hỗ trợ reflink → **node PANIC ngay lúc khởi động**:

```go
// snapshot_init.go:86
if cfg.SnapshotEnabled && !sm.reflinkSupported {
    panic("CRITICAL: Reflink (btrfs/xfs) is required for snapshotting.")
}
```

---

## 6. Cấu trúc thư mục Snapshot

```
snapshot_data_node0/                     ← snapshotBaseDir (cấu hình)
├── snap_epoch_4_block_900/              ← snapshot cũ (sẽ bị xóa nếu > maxSnapshots)
│   ├── account_state/                   ← PebbleDB Checkpoint
│   ├── blocks/                          ← PebbleDB Checkpoint
│   ├── nomt_db/                         ← NOMT reflink/copy
│   ├── changelog_db_account/            ← StateChangelogDB Checkpoint
│   ├── changelog_db_stake/              ← StakeChangelogDB Checkpoint
│   ├── back_up/
│   │   └── epoch_data_backup.json       ← CRITICAL: Epoch boundary data
│   └── metadata.json                    ← Thông tin snapshot
│
└── snap_epoch_5_block_1250/             ← snapshot mới nhất
    ├── ... (same structure)
    └── metadata.json
        {
          "epoch": 5,
          "block_number": 1250,
          "global_exec_index": 8750,
          "state_root": "0xabc123...",
          "last_handled_commit_index": 1000,
          "critical_checksums": {
              "account_state": "sha256hash...",
              "blocks": "sha256hash..."
          },
          "metadata_checksum": "sha256ofthisjson..."
        }
```

### RotateSnapshots — Tự động dọn dẹp

Sau mỗi snapshot mới, `RotateSnapshots()` giữ lại tối đa `maxSnapshots` (mặc định: 2) snapshots gần nhất theo thời gian tạo, và **xóa** các snapshot cũ hơn.

---

## 7. HTTP Snapshot Server

Node Master tự phục vụ snapshot qua HTTP server tích hợp:

**File:** [snapshot_server.go:40](file:///home/abc/nhat/con-chain-v2/metanode/execution/executor/snapshot_server.go#L40)

```
http://node-master:8700/
│
├── /                           ← Web UI — liệt kê tất cả snapshots
├── /api/snapshots              ← JSON API — trả về danh sách metadata
├── /api/snapshots/verify?name= ← Verify checksum một snapshot
└── /files/<snapshot_name>/     ← Download files (hỗ trợ Range, resume)
```

**Port:** Cấu hình qua `snapshot_server_port` trong config (default: 8700). Với 5 nodes thường dùng port 8700–8704 (mỗi node 1 port riêng).

**Download ví dụ:**
```bash
# Tải toàn bộ snapshot (có thể resume nếu bị ngắt)
wget -c -r -np -nH --cut-dirs=2 http://node0:8700/files/snap_epoch_5_block_1250/ -P ./restored_data

# Xem danh sách snapshot qua API
curl http://node0:8700/api/snapshots | python3 -m json.tool
```

**Xác minh checksum trước khi dùng:**
```bash
curl "http://node0:8700/api/snapshots/verify?name=snap_epoch_5_block_1250"
# → {"status":"verified", "metadata": {...}}
```

---

## 8. Luồng phục hồi Node từ Snapshot (restore_node.sh)

Dùng khi một node bị hỏng dữ liệu hoặc là node hoàn toàn mới cần tham gia mạng.

**Script:** [restore_node.sh](file:///home/abc/nhat/con-chain-v2/metanode/consensus/metanode/scripts/node/restore_node.sh)

```bash
./restore_node.sh <node_id> [snapshot_name] [source_node_id]
# VD: ./restore_node.sh 1              # Tự tìm snapshot mới nhất từ node 0
#     ./restore_node.sh 1 snap_epoch_5_block_1250 2  # Chỉ định cụ thể
```

### 7 bước thực hiện

```
[1/7] 🛑 Dừng Node
│   ├─ tmux send-keys C-c → graceful shutdown Go/Rust
│   ├─ tmux kill-session
│   └─ pkill fallback nếu session không thoát
│
[2/7] 🗑️ Xóa toàn bộ dữ liệu cũ
│   ├─ rm -rf sample/nodeX/data
│   ├─ rm -rf sample/nodeX/back_up
│   └─ rm -rf consensus/metanode/config/storage/node_X  ← Rust DAG storage
│       CRITICAL: Rust DAG PHẢI xóa để force clean bootstrap!
│
[3/7] 📸 Restore dữ liệu từ snapshot
│   ├─ [Network mode] wget -c -r từ HTTP server
│   │   └─ Download vào /tmp/snapshot_restore_$$
│   │
│   ├─ [Local mode] Nếu snapshot nằm cùng máy
│   │   └─ cp -a từ snapshot_data_nodeY/
│   │
│   ├─ Mapping thư mục:
│   │   ├─ snap/back_up/* → sample/nodeX/back_up/
│   │   └─ snap/<other dirs> → sample/nodeX/data/data/
│   │
│   ├─ 🚨 CRITICAL: rm -rf data/data/rust_consensus
│   │   └─ Lý do: Rust DAG data từ snapshot thuộc node khác
│   │      Nếu giữ lại → split-brain (Rust nghĩ đang ở GEI=8750 nhưng
│   │      DAG chứa commits của nguồn, không phải của node hiện tại)
│   │      → Force clean bootstrap: Rust tự build DAG từ đầu
│   │
│   ├─ Restore metadata.json → nhiều vị trí
│   ├─ Restore epoch_data_backup.json ← CRITICAL cho Go khởi động đúng epoch
│   └─ Xóa LOCK files và NOMT .lock files (PID của process cũ)
│
[4/7] 🔍 Validate dữ liệu
│   ├─ Kiểm tra epoch_data_backup.json là valid JSON
│   ├─ Kiểm tra các thư mục bắt buộc: blocks/, nomt_db/, transaction_state/
│   └─ Kiểm tra kích thước back_up/
│
[5/7] 🚀 Khởi động tuần tự
│   ├─ [5a] Go Master start (tmux new-session)
│   ├─ [5b] Go Sub start (tmux new-session)
│   ├─ Đợi 10s cho Go load xong dữ liệu snapshot
│   └─ Rust sẽ khởi động sau khi Go báo is_ready=true
│        (xem NODE_RESTART_ARCHITECTURE.md)
│
[6/7] 📊 Giám sát sync (120s)
│   └─ Mỗi 10s: so sánh block của node này với reference node
│       ├─ block tăng → đang sync
│       ├─ block == reference → SYNCED ✅
│       └─ hết 120s → báo cáo manual check
│
[7/7] 🔒 Kiểm tra hash divergence (Anti-Fork)
    ├─ Lấy block number hiện tại của node vừa restore
    ├─ Query hash của block đó từ node vừa restore
    ├─ Query hash của cùng block đó từ một reference node khác
    └─ So sánh:
        ├─ MATCH → ✅ Node đang trên đúng chain
        └─ MISMATCH → ❌ FORK DETECTED! Cần investigate
```

---

## 9. Điều gì xảy ra khi Node khởi động sau khi restore?

Đây là phần kết nối với [NODE_RESTART_ARCHITECTURE.md](file:///home/abc/nhat/con-chain-v2/metanode/consensus/metanode/docs/NODE_RESTART_ARCHITECTURE.md). Node sau restore trải qua flow **đặc biệt**:

```
Node restart (sau restore từ snapshot)
│
├─ Go Master khởi động
│   ├─ Mở Block Database → GetLastBlock() → Block 1250 ✅
│   ├─ Mở NOMT → Root match StateRoot trong snapshot ✅
│   ├─ Load epoch từ epoch_data_backup.json → Epoch 5 ✅
│   └─ SetBlockchainInitDone() → is_ready = true
│
├─ Rust khởi động — setup_storage()
│   ├─ get_last_block_number() → 1250 (is_ready=true)
│   ├─ get_current_epoch() → Epoch 5
│   │
│   ├─ [COLD-START DETECTION]
│   │   ├─ Kiểm tra epochs/epoch_5/consensus_db → RỖNG (đã xóa rust_consensus!)
│   │   └─ dag_has_history = false
│   │
│   └─ Committee Verification với peers (bắt buộc vì cold-start)
│       └─ Xác nhận committee hash khớp với mạng
│
├─ Rust — setup_consensus()
│   │
│   ├─ [RECOVERY BARRIER ACTIVATED]
│   │   └─ go_has_state = true (block=1250 > 0)
│   │       → activate_recovery_barrier()  ← Block mọi proposals
│   │
│   ├─ STARTUP-SYNC BARRIER
│   │   ├─ highest_peer_block = 1320 (mạng đã tiến thêm 70 blocks)
│   │   ├─ local_go_block = 1250
│   │   ├─ Gap = 70 blocks → Tải và xử lý 70 blocks
│   │   └─ Go xử lý 70 blocks qua HandleSyncBlocksRequest
│   │       → local_go_block = 1320 ✅
│   │
│   ├─ CommitSyncerSupervisor khởi động
│   │   ├─ Phát hiện: DAG rỗng, last_handled_commit_index = 1000
│   │   ├─ reset_to_network_baseline(1000) → DAG bắt đầu từ commit 1000
│   │   └─ CatchingUp: Tải commits 1001..current từ peers
│   │
│   └─ Phase: Bootstrapping → DagCatchingUp → ScheduleVerifying → Healthy
│       → Node bắt đầu vote và propose block ✅
```

### Tại sao phải xóa `rust_consensus` khi restore?
- Snapshot của node 0 tại block 1250 có `rust_consensus/` chứa DAG commits của node 0
- Nếu node 1 restore từ snapshot này và GIỮ `rust_consensus`, Rust của node 1 sẽ nghĩ "tôi đã xử lý đến commit 1000 từ DAG của node 0"
- Nhưng DAG là **epoch-scoped và peer-specific** — commit 1000 của node 0 khác commit 1000 của consensus network
- Kết quả: Rust node 1 sẽ dùng sai `go_replay_after`, sai GEI → **FORK**
- **Giải pháp:** Xóa `rust_consensus`, Rust sẽ rebuild DAG từ commit 0 và tự nhảy về vị trí đúng qua CommitSyncer

---

## 10. Cơ chế bảo vệ & các vấn đề thường gặp

### 10a. Atomic Freeze: không bao giờ snapshot dở

Go Master **không được** xử lý block mới trong khi đang snapshot. Cơ chế:

```go
// snapshot_manager.go:420
if pauseCb != nil {
    pauseCb()  // Acquires ExecutionMutex, drains commit queue
}
// ... tất cả công việc snapshot ...
defer func() {
    if resumeCb != nil { resumeCb() }  // LUÔN resume, kể cả panic
}()
```

Nếu snapshot mất > 30 giây → Rust watchdog tự động resume (tránh consensus stall). Vì vậy **PebbleDB Checkpoint và NOMT reflink phải hoàn thành rất nhanh**.

### 10b. Checksum Integrity Verification

Mỗi snapshot có 2 lớp checksum:

```
metadata.json {
  "critical_checksums": {
    "account_state": "sha256(tất cả files trong thư mục)",
    "blocks": "sha256..."
  },
  "metadata_checksum": "sha256(json này, bỏ trường metadata_checksum)"
}
```

Khi verify: tính lại checksum và so sánh. Nếu bất kỳ file nào bị thay đổi hoặc bị xóa → verify thất bại.

### 10c. epoch_data_backup.json — Critical file

File này chứa epoch boundary data (committee, validators, boundary_block, boundary_GEI). **Nếu thiếu file này khi restore**, Go sẽ không biết mình đang ở epoch nào → Rust không thể load committee đúng → **Node không thể tham gia consensus**.

### 10d. LOCK files & NOMT .lock files

Sau khi copy snapshot, các file `LOCK` (PebbleDB) và `.lock` (NOMT) còn chứa PID của process nguồn. Nếu không xóa, PebbleDB sẽ từ chối mở vì nghĩ "đang có process khác đang dùng". Script `restore_node.sh` tự động xóa:

```bash
find "$NODE_DATA" -name "LOCK" -delete
find "$NODE_DATA" -name ".lock" -path "*/nomt_db/*" -delete
```

### 10e. isSnapshotInProgress() — Bảo vệ RPC

Trong khi snapshot đang chạy, một số RPC calls có thể gây deadlock (do cần NOMT session). RPC handler kiểm tra:

```go
// snapshot_manager.go:195
func (sm *SnapshotManager) IsSnapshotInProgress() bool {
    return sm.isCreating
}
// → HandleSyncBlocksRequest sẽ reject/delay nếu isCreating=true
```

---

## 11. Các thuật ngữ quan trọng

| Thuật ngữ | Giải thích |
|---|---|
| **Snapshot** | Bản chụp toàn bộ dữ liệu blockchain tại một thời điểm |
| **Atomic Snapshot** | Snapshot nhất quán — không bị pha trộn với block đang xử lý |
| **Epoch Boundary** | Block cuối của một epoch, khi committee validator thay đổi |
| **blocksAfterEpoch** | Số block chờ sau epoch boundary trước khi tạo snapshot (default: 20) |
| **PebbleDB Checkpoint** | Cơ chế tạo bản sao nhất quán của PebbleDB mà không cần lock lâu dài |
| **Hardlink** | File system trick: 2 path trỏ cùng inode (data), không tốn thêm dung lượng |
| **Reflink** | Btrfs/XFS trick: Copy-on-Write clone, instant copy, chia sẻ data blocks |
| **NOMT** | Near-Optimal Merkle Trie — cây Merkle lưu account state |
| **StateChangelogDB** | Database PebbleDB lưu lịch sử thay đổi state theo từng block |
| **epoch_data_backup.json** | File JSON chứa thông tin epoch boundary — critical cho restart |
| **rust_consensus** | Thư mục chứa DAG data của Rust, phải xóa khi restore để tránh split-brain |
| **Stagger** | Kỹ thuật phân tán thời điểm snapshot giữa các node để không mất quorum |
| **SnapshotManager** | Go struct quản lý toàn bộ vòng đời snapshot: tạo, rotate, phục vụ HTTP |
| **Split-brain** | Tình trạng node dùng dữ liệu DAG sai → nhận định sai về vị trí trong chain → fork |
| **RotateSnapshots** | Xóa các snapshot cũ, chỉ giữ lại maxSnapshots (default: 2) snapshot gần nhất |
