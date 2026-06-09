package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"strings"

	"tool-test/pkg/bls"
)

func main() {
	// Khởi tạo cấu hình BLS
	bls.Init()

	// Khai báo flag nhận Private Key
	pkFlag := flag.String("pk", "", "Private key BLS (hex) để khôi phục")
	flag.Parse()

	fmt.Println("=======================================")
	fmt.Println("🔑 BLS KEY GENERATOR & RECOVERY TOOL")
	fmt.Println("=======================================")

	if *pkFlag == "" {
		// Tạo mới hoàn toàn
		fmt.Println("Tạo mới một BLS KeyPair ngẫu nhiên...")
		kp := bls.GenerateKeyPair()
		if kp == nil {
			fmt.Println("❌ Lỗi: Không thể tạo BLS Key Pair!")
			return
		}
		fmt.Printf("Private Key: %s\n", hex.EncodeToString(kp.BytesPrivateKey()))
		fmt.Printf("Public Key:  %s\n", hex.EncodeToString(kp.BytesPublicKey()))
		fmt.Printf("Address:     %s\n", kp.Address().Hex())
	} else {
		// Khôi phục từ private key truyền vào
		fmt.Println("Khôi phục BLS KeyPair từ Private Key...")
		hexKey := strings.TrimPrefix(*pkFlag, "0x")
		
		priv, pub, addr := bls.GenerateKeyPairFromSecretKey(hexKey)
		
		if len(priv.Bytes()) == 0 {
			fmt.Println("❌ Lỗi: Private key không hợp lệ hoặc sai độ dài!")
			return
		}

		fmt.Printf("Private Key: %s\n", hex.EncodeToString(priv.Bytes()))
		fmt.Printf("Public Key:  %s\n", hex.EncodeToString(pub.Bytes()))
		fmt.Printf("Address:     %s\n", addr.Hex())
	}
	fmt.Println("=======================================")
}
