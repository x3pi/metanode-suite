#!/bin/bash

echo "🚀 Đang khởi chạy 5 tiến trình test song song..."

go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-t1.json --loop --delay=0 &
PID1=$!

go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-t2.json --loop --delay=0 &
PID2=$!

go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-t3.json --loop --delay=0 &
PID3=$!

go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-t4.json --loop --delay=0 &
PID4=$!

go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-t5.json --loop --delay=0 &
PID5=$!

echo "✅ Đã khởi chạy xong 5 tiến trình với các PID: $PID1, $PID2, $PID3, $PID4, $PID5"
echo "Nhấn Ctrl+C để dừng tất cả."

# Bẫy (trap) tín hiệu Ctrl+C để kill toàn bộ 5 tiến trình con khi user dừng script
trap "echo '🛑 Đang dừng tất cả các tiến trình...'; kill $PID1 $PID2 $PID3 $PID4 $PID5 2>/dev/null; exit" SIGINT SIGTERM

# Đợi cho tất cả các tiến trình nền kết thúc
wait $PID1 $PID2 $PID3 $PID4 $PID5
echo "Tất cả các tiến trình đã hoàn thành!"
