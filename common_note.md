# fix bug

khi gọi start_unified_epoch_monitor thì cần hủy tiên trình ngầm đó để tránh bị lỗi

```bash
# log đồng thuận rust : check bao nhiêu stake
info!("Consensus committee: {:?}", committee);

🔄 [EPOCH RECOVERY] Extracting  : node clean epoch pending
🗑️ [DEBUG-RÁC]  : log check dọn rác
```

# đang còn lỗi validator -> synOnly

spam giao dịch normal + getAccount có khả năng bị lỗi
