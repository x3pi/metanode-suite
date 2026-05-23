# Hướng dẫn chạy thủ công test Node Recovery

Dựa vào kịch bản tự động trong `test-node-recovery-gap.sh`, quá trình test node recovery bao gồm các bước tạm dừng một node, ép các node còn lại chạy tiếp để tạo khoảng trống dữ liệu (gap), sau đó bật lại node đã tắt và kiểm tra xem nó có đồng bộ lại (recover) chính xác hay không.

Dưới đây là các bước chạy thủ công để bạn có thể debug từng phần (giả sử test với **Node 1** và tạo **Gap 1 epoch**):

### 1. Dọn dẹp và khởi tạo mạng cơ bản
Đầu tiên, chạy script test cơ bản để mạng khởi động và chạy ổn định.
```bash
cd /home/abc/nhat/con-chain-v2/metanode-suite/scripts
./simple_test.sh
```
*Chờ một lúc cho mạng tạo block và đạt ít nhất Epoch 1.* (Có thể dùng RPC check epoch: `curl -s -X POST http://127.0.0.1:8757 -d '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest",false],"id":1}'`)

### 2. Lưu trạng thái lịch sử của mạng
Lưu lại trạng thái RPC hiện tại để sau khi node 1 hồi phục, bạn có thể kiểm tra xem nó có bị sai lệch dữ liệu cũ không.
```bash
cd /home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-history
go run main.go -config config-local.json -action save -file /tmp/pending_check_1.json
```

### 3. Tắt Node mục tiêu (Node 1)
```bash
cd /home/abc/nhat/con-chain-v2/metanode/consensus/metanode/scripts
./mtn-orchestrator.sh stop-node 1
```

### 4. Bắn giao dịch để tạo Gap (Khoảng trống dữ liệu)
Bạn bắn giao dịch vào một node vẫn đang chạy (ví dụ Node 0) để ép mạng tiến lên các epoch tiếp theo, tạo gap cho Node 1 đang tắt.
```bash
cd /home/abc/nhat/con-chain-v2/metanode-suite/test_tps/tps_blast_cc
go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10 --target-node 0
```
*Ghi chú: Bạn có thể đợi một lát cho Epoch tăng lên 1 hoặc 2 đơn vị so với lúc Node 1 bị tắt, sau đó nhấn `Ctrl+C` dừng lệnh blast nếu cần thiết.*

### 5. Bật lại Node mục tiêu (Node 1)
```bash
cd /home/abc/nhat/con-chain-v2/metanode/consensus/metanode/scripts
./mtn-orchestrator.sh start-node 1
```
*Ghi chú: Tại bước này bạn nên theo dõi log của Node 1 để xem tiến trình Recovery diễn ra như thế nào, có bị panic hoặc lỗi integrity không:*
```bash
tail -f /home/abc/nhat/con-chain-v2/metanode/consensus/metanode/logs/node_1/go-master-stdout.log
```

### 6. Xác minh trạng thái lịch sử
Kiểm tra xem dữ liệu lịch sử trên Node 1 có bảo toàn được sau khi recovery không (so sánh với file đã lưu ở bước 2).
```bash
cd /home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-history
go run main.go -config config-local.json -action verify -file /tmp/pending_check_1.json -target-node 1
```

### 7. Kiểm tra tính đồng bộ (Hash Checker)
Chạy Hash Checker xem Node 1 sau khi sống lại có đồng bộ block hash giống hệt các node khác không.
```bash
cd /home/abc/nhat/con-chain-v2/metanode-suite/block/block_hash_checker
go run main.go --watch --interval 5s --check-last 100 --nodes "m0=http://127.0.0.1:8757,m1=http://127.0.0.1:10747,m2=http://127.0.0.1:10749,m3=http://127.0.0.1:10750,m4=http://127.0.0.1:10748"
```
Nếu bị lệch hash, script trên sẽ dừng và báo lỗi, ghi log vào `hash_mismatch_alert.log`.

### 8. Stress test sau hồi phục
Trong lúc các node (đặc biệt Node 1) đã hồi phục, bạn bắn giao dịch liên tục vào nó để xem nó có gặp lỗi (như race condition, panic...) trong lúc vừa phải bắt kịp mạng vừa nhận giao dịch mới không:
```bash
# Bắn vào Node 1 (node vừa hồi phục)
cd /home/abc/nhat/con-chain-v2/metanode-suite/test_tps/tps_blast_cc
go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10 --target-node 1
```
