# fix bug

khi gọi start_unified_epoch_monitor thì cần hủy tiên trình ngầm đó để tránh bị lỗi

```bash
# log đồng thuận rust : check bao nhiêu stake
info!("Consensus committee: {:?}", committee);

🔄 [EPOCH RECOVERY] Extracting  : node clean epoch pending
🗑️ [DEBUG-RÁC]  : log check dọn rác

Để kiểm tra trực tiếp xem có bao nhiêu dữ liệu đang "lơ lửng" trên RAM mà chưa kịp lưu xuống ổ cứng, bạn có thể chạy lệnh này trên Terminal của hệ điều hành Linux (tại máy Node 4) trong lúc Node đang hoạt động:
cat /proc/meminfo | grep Dirty

```
# sủa env deploy ssh nhiều máy
sửa :  
/home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-history/config-mutil.json
/home/abc/nhat/con-chain-v2/metanode-suite/test_tps/tps_blast_cc/config-multi.json
/home/abc/nhat/con-chain-v2/metanode-suite/block/block_hash_checker/config-m-nodes.json
# đang còn lỗi validator -> synOnly



🚨 [EXECUTION-STALL] Go execution stuck at


🚨 [LIVENESS-STALL] DAG commit frozen at