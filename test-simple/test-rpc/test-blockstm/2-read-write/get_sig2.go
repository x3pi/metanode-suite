package main
import (
"fmt"
"github.com/ethereum/go-ethereum/crypto"
)
func main() {
fmt.Printf("writeData: %s\n", crypto.Keccak256Hash([]byte("writeData(uint256)")).Hex()[:10])
    fmt.Printf("readDataAndSave: %s\n", crypto.Keccak256Hash([]byte("readDataAndSave()")).Hex()[:10])
}
