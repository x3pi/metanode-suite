package main

import (
"fmt"
"os"
"context"
"encoding/json"

"github.com/ethereum/go-ethereum/crypto"
"github.com/ethereum/go-ethereum/ethclient"
)

type Config struct {
RPCUrl     string `json:"rpc_url"`
Address    string `json:"address"`
PrivateKey string `json:"private_key"`
ChainID    int64  `json:"chain_id"`
}

func main() {
rawConfig, _ := os.ReadFile("config.json")
var cfg Config
json.Unmarshal(rawConfig, &cfg)
client, _ := ethclient.Dial(cfg.RPCUrl)

dummyReceiverKey, _ := crypto.HexToECDSA("5e01139ae9168584a6e4ac2e5b71c21d48933e1485be9e4d50b61ec3dfbf4f50")
dummyReceiverAddr := crypto.PubkeyToAddress(dummyReceiverKey.PublicKey)

nonceDummy, _ := client.PendingNonceAt(context.Background(), dummyReceiverAddr)
fmt.Printf("Nonce of dummyReceiver: %d\n", nonceDummy)
}
