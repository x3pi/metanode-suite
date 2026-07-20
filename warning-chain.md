


WARNING

Lỗi seed_from_execution_state trong Rust vi phạm nghiêm trọng nguyên tắc BFT (tự phong quorum mà không cần chữ ký). Tuy nhiên, nếu xóa nó đi thì có thể module commit_syncer sẽ bị treo ở cold-start (không bootstrap được). Tạm thời để chặn đứng việc Fork xảy ra trong bài test này, mình đề xuất chỉ sửa lỗi Deadlock ở Block-STM (Go). Một khi Go chạy 100% Deterministic và không bao giờ rớt txs, các node sẽ luôn luôn sinh ra Block giống nhau, và lỗi Rust sẽ không bị trigger. Về lâu dài, bạn cần xem xét lại kiến trúc của seed_from_execution_state bên Rust.





stm.waitersMu[blockingVer].Lock()
    stm.waiters[blockingVer] = append(stm.waiters[blockingVer], uint32(txIndex))
    stm.waitersMu[blockingVer].Unlock()
tôi có cần dùng syncmap để tránh block nhau k ?