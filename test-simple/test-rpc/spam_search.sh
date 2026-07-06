#!/bin/bash
echo "⚡ Đang build Go binary để xả bão tốc độ cao..."
go build -o spam_bin main.go

echo "🚀 Bắt đầu spam QuerySearch_View vô tận (Ctrl+C để thoát)..."
trap "echo '🛑 Đã dừng spam!'; exit 0" SIGINT SIGTERM

round=1
while true; do
  echo "🌀 Spam vòng thứ $round..."
  
  ./spam_bin -config=./config-local.json -data=./test_read_wire_xapian/spam/data-xapian-search-t1.json &
  PID1=$!
  
  ./spam_bin -config=./config-local.json -data=./test_read_wire_xapian/spam/data-xapian-search-t2.json &
  PID2=$!
  
  ./spam_bin -config=./config-local.json -data=./test_read_wire_xapian/spam/data-xapian-search-t3.json &
  PID3=$!
  
  ./spam_bin -config=./config-local.json -data=./test_read_wire_xapian/spam/data-xapian-search-t4.json &
  PID4=$!
  
  ./spam_bin -config=./config-local.json -data=./test_read_wire_xapian/spam/data-xapian-search-t5.json &
  PID5=$!
  
  wait $PID1 || { echo "❌ Lỗi ở luồng 1! Thoát..."; exit 1; }
  wait $PID2 || { echo "❌ Lỗi ở luồng 2! Thoát..."; exit 1; }
  wait $PID3 || { echo "❌ Lỗi ở luồng 3! Thoát..."; exit 1; }
  wait $PID4 || { echo "❌ Lỗi ở luồng 4! Thoát..."; exit 1; }
  wait $PID5 || { echo "❌ Lỗi ở luồng 5! Thoát..."; exit 1; }
  
  ((round++))
done
