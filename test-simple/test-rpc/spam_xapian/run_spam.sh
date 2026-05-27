#!/bin/bash

if [ "$1" == "deploy" ]; then
    echo "⚡ Chế độ: Tự động Deploy Contract từ test_read_wire_xapian/data-xapian-v2.json..."
    METHOD=${2:-"runStep1_Setup"}
    WALLETS=${3:-1000}
    ROUNDS=${4:-2000}
    
    echo "⚡ Build Go binary..."
    go build -o spam_xapian_test main.go
    if [ $? -ne 0 ]; then
        echo "❌ Build Go thất bại!"
        exit 1
    fi

    echo "🚀 Bắt đầu chạy test (Tự động Deploy)..."
    ./spam_xapian_test -deploy-json="../test_read_wire_xapian/data-xapian-v2.json" -method="$METHOD" -wallets="$WALLETS" -rounds="$ROUNDS"
    exit 0
fi

if [ -z "$1" ]; then
    echo "❌ Lỗi: Bạn cần cung cấp Contract Address hoặc dùng tham số 'deploy'!"
    echo "Cách 1: ./run_spam.sh deploy [MethodName] [NumWallets] [NumRounds]"
    echo "Cách 2: ./run_spam.sh <ContractAddress> [MethodName] [NumWallets] [NumRounds]"
    exit 1
fi

CONTRACT_ADDR=$1
METHOD=${2:-"runStep1_Setup"}
WALLETS=${3:-1000}
ROUNDS=${4:-2000}

echo "⚡ Build Go binary..."
go build -o spam_xapian_test main.go
if [ $? -ne 0 ]; then
    echo "❌ Build Go thất bại!"
    exit 1
fi

echo "🚀 Bắt đầu chạy test..."
./spam_xapian_test -contract="$CONTRACT_ADDR" -method="$METHOD" -wallets="$WALLETS" -rounds="$ROUNDS"
