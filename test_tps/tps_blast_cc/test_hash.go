package main

import (
"encoding/hex"
"fmt"
"math/big"
"tool-test/pkg/transaction"
"tool-test/pkg/common"
eth_common "github.com/ethereum/go-ethereum/common"
"github.com/ethereum/go-ethereum/crypto"
)

func main() {
privKeyBytes, _ := hex.DecodeString("0000000000000000000000000000000000000000000000000000000000000001")
ecdsaKey, _ := crypto.ToECDSA(privKeyBytes)
fromAddr := crypto.PubkeyToAddress(ecdsaKey.PublicKey)
targetContract := eth_common.HexToAddress("0x00000000000000000000000000000000B429C0B2")
var pKey common.PrivateKey

internalTx := transaction.NewTransaction(
fromAddr, targetContract, big.NewInt(100),
1000000, 1000000, 0,
[]byte{1,2,3}, [][]byte{},
eth_common.Hash{}, eth_common.Hash{},
1, 1,
)
internalTx.SetSign(pKey)

fmt.Printf("Hash: %s\n", internalTx.Hash().Hex())
}
