# fix bug

khi gọi start_unified_epoch_monitor thì cần hủy tiên trình ngầm đó để tránh bị lỗi

```bash
# log đồng thuận rust : check bao nhiêu stake
info!("Consensus committee: {:?}", committee);

🔄 [EPOCH RECOVERY] Extracting  : node clean epoch pending
🗑️ [DEBUG-RÁC]  : log check dọn rác

Để kiểm tra trực tiếp xem có bao nhiêu dữ liệu đang "lơ lửng" trên RAM mà chưa kịp lưu xuống ổ cứng, bạn có thể chạy lệnh này trên Terminal của hệ điều hành Linux (tại máy Node 4) trong lúc Node đang hoạt động:
cat /proc/meminfo | grep Dirty


# xem log
grep -rn "\[PERF-POOL-BATCH\] addTransactionsToPoolInternal took" /home/abc/nhat/con-chain-v2/metanode/consensus/logs_systemd/run_20260618_080945/
grep -rnE "\[BLOCK-TRACE\]|\[PERF-EVM\]" /home/abc/nhat/con-chain-v2/metanode/consensus/logs_systemd/run_20260618_080945/

```

key genesis:
 {
    "index": 0,
    "private_key": "31d9b4391503b818fcc0272eaa3f2e9c517fd1851c9e8818b17c6d4e6a0acba8",
    "address": "0x8cF200967660DB21739CaC872e64Bb5cfFBA0049"
  },
  {
    "index": 1,
    "private_key": "f2a45ec7c59aea49ce421aacf231ed98bf9d3092f36b58306be949457b651409",
    "address": "0x7174Ad7F17a5B57a7d1835ba1a942521407c0dC6"
  },
  
# sủa env deploy ssh nhiều máy
sửa :  
/home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-history/config-mutil.json
/home/abc/nhat/con-chain-v2/metanode-suite/test_tps/tps_blast_cc/config-multi.json
/home/abc/nhat/con-chain-v2/metanode-suite/block/block_hash_checker/config-m-nodes.json
# đang còn lỗi validator -> synOnly



🚨 [EXECUTION-STALL] Go execution stuck at


🚨 [LIVENESS-STALL] DAG commit frozen at