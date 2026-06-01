package main

import (
"context"
"fmt"
"log"

"github.com/ethereum/go-ethereum"
"github.com/ethereum/go-ethereum/common"
"github.com/ethereum/go-ethereum/ethclient"
)

func main() {
client, err := ethclient.Dial("http://192.168.1.234:8545")
if err != nil {
log.Fatalf("Dial err: %v", err)
}

contractAddr := common.HexToAddress("0x00000000000000000000000000000000D844bb55")
// getPublickeyBls() selector is keccak256("getPublickeyBls()")[:4]
// Let's compute it. Wait, I can just use getPublickeyBls hash, but I'll use common.Hex2Bytes("80...")
// Wait, I don't know the hash. Let's just use "getPublickeyBls()".
// Or maybe I can use getPublickeyBls.

// I'll just use ABI.
}
