cd /home/abc/nhat/con-chain-v2/metanode-suite/scripts
./simple_test.sh

cd /home/abc/nhat/con-chain-v2/metanode-suite/test-simple/test-rpc/test-history
go run main.go -config config-local.json -action save -file /tmp/pending_check_1.json
sleep 1s

cd /home/abc/nhat/con-chain-v2/metanode/consensus/metanode/scripts
./mtn-orchestrator.sh stop-node 1
sleep 1s

cd /home/abc/nhat/con-chain-v2/metanode-suite/test_tps/tps_blast_cc
go run main.go --count 20000 --parallel_native=true --rounds 1 --load_balance=false --batch=10 --target-node 0

sleep 1s

cd /home/abc/nhat/con-chain-v2/metanode/consensus/metanode/scripts
./mtn-orchestrator.sh start-node 1