# Tạo file 20 chunk, mỗi chunk 250KB (ra file 5.120.000 bytes)
./create_file.sh --chunks 20 --chunk-size-kb 250 output.txt
./create_file.sh --chunks 20 --chunk-size-kb 250 output.txt

# Tạo file chính xác 10 MB
./create_file.sh --size-mb 10 my_custom_file.txt

./create_file.sh --size-mb 2 file_2mb.txt