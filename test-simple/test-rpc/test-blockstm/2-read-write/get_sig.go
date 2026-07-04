package main
import (
"fmt"
"github.com/ethereum/go-ethereum/crypto"
)
func main() {
hash := crypto.Keccak256Hash([]byte("sharedData()"))
fmt.Printf("sharedData(): %s\n", hash.Hex()[:10])
    hash2 := crypto.Keccak256Hash([]byte("userReads(address)"))
fmt.Printf("userReads(address): %s\n", hash2.Hex()[:10])
}
