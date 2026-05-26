test db xapiant

### mở terminal ở main.go

# local

``` bash
go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data.json
#chạy v0
go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-v0.json
go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-v2.json

 go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-t1.json --loop
 go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-t2.json --loop
 go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-t3.json --loop
 go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-t4.json --loop
 go run main.go -config=./config-local.json -data=./test_read_wire_xapian/data-xapian-t5.json --loop

```

# server

```bash
go run main.go -config=./config-server.json -data=./test_read_wire_xapian/data-xapian-v0.json

go run main.go -config=./config-server.json -data=./test_read_wire_xapian/data-xapian-v2.json
```
