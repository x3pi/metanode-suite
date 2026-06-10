package main

import (
	"fmt"
	"log"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

// 0x477f695feDA85d7EaA339CF01154C870B98Bf325
// PredictContractAddress dự đoán địa chỉ smart contract mới
func PredictContractAddress(deployer common.Address, nonce uint64) common.Address {
	// Encode RLP của [deployer, nonce]
	data, err := rlp.EncodeToBytes([]interface{}{deployer, nonce})
	if err != nil {
		log.Fatalf("RLP encode error: %v", err)
	}

	// Băm Keccak256
	hash := crypto.Keccak256(data)

	// Địa chỉ = 20 byte cuối của hash
	return common.BytesToAddress(hash[12:])
}

func main() {
	deployer := common.HexToAddress("0xC6E6474A8DEAD25B0e75b1aeA5d35FA19f69588a")
	nonce := uint64(2)

	contractAddr := PredictContractAddress(deployer, nonce)
	fmt.Printf("Predicted contract address: %s\n", contractAddr.Hex())
}
