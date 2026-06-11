#!/bin/bash
# run_continuous_spam_loop.sh
# Runs run_spam_loop.sh in a continuous loop. If a failure is detected, it stops so the developer can inspect and fix.

SCRIPT_DIR="/home/abc/nhat/consensus-chain/metanode-suite/scripts"
cd "$SCRIPT_DIR"

ROUND=1
while true; do
    echo "=================================================="
    echo "🔄 CHẠY VÒNG LẶP LIÊN TỤC: ROUND $ROUND"
    echo "=================================================="
    
    # Chạy script test một chu kỳ
    ./run_spam_loop.sh
    RESULT=$?
    
    if [ $RESULT -ne 0 ]; then
        echo "❌ LỖI PHÁT HIỆN TẠI ROUND $ROUND!"
        exit $RESULT
    fi
    
    echo "✅ ROUND $ROUND HOÀN THÀNH THÀNH CÔNG!"
    ROUND=$((ROUND + 1))
    sleep 5
done
