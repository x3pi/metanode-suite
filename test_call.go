package main
import (
"context"
"fmt"
"strings"
"github.com/ethereum/go-ethereum"
"github.com/ethereum/go-ethereum/common"
"github.com/ethereum/go-ethereum/ethclient"
)
func main() {
client, err := ethclient.Dial("http://127.0.0.1:8545")
if err != nil { panic(err) }

addr := common.HexToAddress("0x00000000000000000000000000000000D844bb55")
data := common.Hex2Bytes("2156ef9a")

msg := ethereum.CallMsg{
To:   &addr,
Data: data,
}

res, err := client.CallContract(context.Background(), msg, nil)
if err != nil {
fmt.Println("Error:", err)
return
}

resStr := string(res)
resStr = strings.Trim(resStr, "\"") // remove quotes
fmt.Println("Result string:", resStr)
}
