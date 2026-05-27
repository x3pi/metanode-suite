#!/bin/bash

echo "🚀 Bắt đầu spam GetData_View 5 luồng x 50 vòng (Tổng: 250 request)..."

for i in {1..50}; do
  echo "🌀 Spam vòng thứ $i..."
  
  go run main.go -config=./config-local.json -data=./test_read_wire_xapian/spam/data-xapian-t1.json > /dev/null 2>&1 &
  PID1=$!
  
  go run main.go -config=./config-local.json -data=./test_read_wire_xapian/spam/data-xapian-t2.json > /dev/null 2>&1 &
  PID2=$!
  
  go run main.go -config=./config-local.json -data=./test_read_wire_xapian/spam/data-xapian-t3.json > /dev/null 2>&1 &
  PID3=$!
  
  go run main.go -config=./config-local.json -data=./test_read_wire_xapian/spam/data-xapian-t4.json > /dev/null 2>&1 &
  PID4=$!
  
  go run main.go -config=./config-local.json -data=./test_read_wire_xapian/spam/data-xapian-t5.json > /dev/null 2>&1 &
  PID5=$!
  
  # Đợi 5 thread của vòng này xong rồi mới xả tiếp vòng sau để tránh tràn RAM
  wait $PID1 $PID2 $PID3 $PID4 $PID5
done

echo "✅ Hoàn tất Spam GetData_View!"
