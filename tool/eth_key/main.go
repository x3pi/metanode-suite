package main

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	command := strings.ToLower(os.Args[1])
	switch command {
	case "generate", "gen":
		generateKey()
	case "recover", "rec":
		if len(os.Args) < 3 {
			fmt.Println("Error: Missing private key for recovery.")
			fmt.Println("Usage: go run main.go recover <private_key_hex>")
			os.Exit(1)
		}
		recoverKey(os.Args[2])
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
	}
}

func printUsage() {
	fmt.Println("Ethereum Key Tool Usage:")
	fmt.Println("  generate                      Generate a new private key, public key, and address")
	fmt.Println("  recover <private_key_hex>    Recover public key and address from a private key hex string")
}

func generateKey() {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		fmt.Printf("Error generating key: %v\n", err)
		os.Exit(1)
	}

	displayKeys(privateKey)
}

func recoverKey(privHex string) {
	// Clean hex prefix if present
	privHex = strings.TrimPrefix(privHex, "0x")
	
	// Validate length (must be 64 hex characters for 32 bytes)
	if len(privHex) != 64 {
		fmt.Printf("Error: Invalid private key length. Expected 64 characters (32 bytes), got %d\n", len(privHex))
		os.Exit(1)
	}

	privateKey, err := crypto.HexToECDSA(privHex)
	if err != nil {
		fmt.Printf("Error parsing private key: %v\n", err)
		os.Exit(1)
	}

	displayKeys(privateKey)
}

func displayKeys(privateKey *ecdsa.PrivateKey) {
	// Private key bytes
	privBytes := crypto.FromECDSA(privateKey)
	privHex := hex.EncodeToString(privBytes)

	// Public key
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		fmt.Println("Error: Failed to cast public key to ECDSA")
		os.Exit(1)
	}

	// Format public key
	// Uncompressed public key: 65 bytes starting with 0x04
	pubBytesUncompressed := crypto.FromECDSAPub(publicKeyECDSA)
	pubHexUncompressed := hex.EncodeToString(pubBytesUncompressed)

	// Compressed public key: 33 bytes starting with 0x02 or 0x03
	pubBytesCompressed := crypto.CompressPubkey(publicKeyECDSA)
	pubHexCompressed := hex.EncodeToString(pubBytesCompressed)

	// Address
	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	fmt.Println("==================================================================================")
	fmt.Println("🔑 ETHEREUM KEY DETAILS")
	fmt.Println("==================================================================================")
	fmt.Printf("  Private Key (Hex):         0x%s\n", privHex)
	fmt.Printf("  Public Key (Uncompressed): 0x%s\n", pubHexUncompressed)
	fmt.Printf("  Public Key (Compressed):   0x%s\n", pubHexCompressed)
	fmt.Printf("  Ethereum Address:          %s\n", address.Hex())
	fmt.Println("==================================================================================")
}
