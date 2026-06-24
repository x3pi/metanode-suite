package main

import (
"encoding/hex"
"fmt"
"strings"

"github.com/ethereum/go-ethereum/accounts/abi"
)

func main() {
abiJSON := `[{"inputs":[{"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"setBlsPublicKey","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"}]`
parsedABI, err := abi.JSON(strings.NewReader(abiJSON))
if err != nil {
panic(err)
}

blsPubKeyHex := ""
blsPubKeyBytes, err := hex.DecodeString(strings.TrimPrefix(blsPubKeyHex, "0x"))
if err != nil {
panic(err)
}

data, err := parsedABI.Pack("setBlsPublicKey", blsPubKeyBytes)
if err != nil {
panic(err)
}
fmt.Printf("Data: %x\n", data)
}
