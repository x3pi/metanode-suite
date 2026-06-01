package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"tool-test/pkg/transaction"
)

type AccountInfo struct {
	Index      int    `json:"index"`
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
}

func main() {
	keysData, err := os.ReadFile("test_tps/gen_spam_keys/generated_keys.json")
	if err != nil {
		panic(err)
	}

	var accounts []AccountInfo
	if err := json.Unmarshal(keysData, &accounts); err != nil {
		panic(err)
	}

	for _, acc := range accounts {
		if strings.ToLower(acc.Address) == strings.ToLower("0x7ac5b3Bb5328a3a28285E0951Efe2D5f3c2e9Fff") {
			fromAddr := common.HexToAddress(acc.Address)
			targetContract := common.HexToAddress("0x7b7b63a0bf6c67657a2332266e98e5cc52879ab6")
			txAmount := big.NewInt(100)
			nonce := uint64(1)
			chainId := uint64(991)
			bCallData := []byte{}

			internalTx := transaction.NewTransaction(
				fromAddr, targetContract, txAmount,
				1000000, 1000000, 0,
				bCallData, [][]byte{},
				common.Hash{}, common.Hash{},
				nonce, chainId,
			)

			fmt.Printf("Expected Hash: %s\n", internalTx.Hash().Hex())
		}
	}
}
