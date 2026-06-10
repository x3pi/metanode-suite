// package main

// import (
// 	"encoding/hex"
// 	"fmt"
// 	"os"
// 	"strings"
// )

// // compareBytecode so sánh hai bytecode và tìm sự khác biệt
// func compareBytecode(remixBytecode, mainGoBytecode string) {
// 	// Remove 0x prefix
// 	remixHex := strings.TrimPrefix(remixBytecode, "0x")
// 	mainGoHex := strings.TrimPrefix(mainGoBytecode, "0x")

// 	remixBytes, err1 := hex.DecodeString(remixHex)
// 	mainGoBytes, err2 := hex.DecodeString(mainGoHex)

// 	if err1 != nil {
// 		fmt.Printf("❌ Lỗi decode Remix bytecode: %v\n", err1)
// 		return
// 	}
// 	if err2 != nil {
// 		fmt.Printf("❌ Lỗi decode main.go bytecode: %v\n", err2)
// 		return
// 	}

// 	fmt.Println("=" + strings.Repeat("=", 80))
// 	fmt.Println("SO SÁNH BYTECODE")
// 	fmt.Println("=" + strings.Repeat("=", 80))
// 	fmt.Printf("Remix bytecode length:   %d bytes (%d hex chars)\n", len(remixBytes), len(remixHex))
// 	fmt.Printf("main.go bytecode length: %d bytes (%d hex chars)\n", len(mainGoBytes), len(mainGoHex))
// 	fmt.Println()

// 	if len(remixBytes) == len(mainGoBytes) {
// 		// So sánh từng byte
// 		differences := 0
// 		for i := 0; i < len(remixBytes); i++ {
// 			if remixBytes[i] != mainGoBytes[i] {
// 				differences++
// 				if differences <= 10 { // Chỉ hiển thị 10 differences đầu tiên
// 					fmt.Printf("⚠️  Khác biệt tại byte %d: Remix=0x%02x, main.go=0x%02x\n", i, remixBytes[i], mainGoBytes[i])
// 				}
// 			}
// 		}
// 		if differences == 0 {
// 			fmt.Println("✅ Bytecode GIỐNG NHAU hoàn toàn!")
// 		} else {
// 			fmt.Printf("⚠️  Tổng cộng %d bytes khác biệt\n", differences)
// 		}
// 	} else {
// 		// Bytecode có độ dài khác nhau
// 		fmt.Printf("⚠️  Bytecode có độ dài KHÁC NHAU\n")
// 		fmt.Printf("   Chênh lệch: %d bytes\n", len(remixBytes)-len(mainGoBytes))

// 		// Tìm phần giống nhau ở đầu
// 		minLen := len(remixBytes)
// 		if len(mainGoBytes) < minLen {
// 			minLen = len(mainGoBytes)
// 		}

// 		commonPrefix := 0
// 		for i := 0; i < minLen; i++ {
// 			if remixBytes[i] == mainGoBytes[i] {
// 				commonPrefix++
// 			} else {
// 				break
// 			}
// 		}

// 		fmt.Printf("   Phần giống nhau ở đầu: %d bytes\n", commonPrefix)

// 		if len(remixBytes) > len(mainGoBytes) {
// 			// Remix dài hơn → có thể có constructor args hoặc metadata
// 			extraBytes := remixBytes[len(mainGoBytes):]
// 			fmt.Printf("\n📝 Phần thêm ở Remix (%d bytes):\n", len(extraBytes))
// 			fmt.Printf("   Hex: %s\n", hex.EncodeToString(extraBytes))
// 			fmt.Printf("   Có thể là:\n")
// 			fmt.Printf("   - Constructor arguments (nếu contract có constructor)\n")
// 			fmt.Printf("   - Metadata (CBOR-encoded, thường bắt đầu với 0xa2)\n")
// 		} else {
// 			// main.go dài hơn (ít gặp)
// 			extraBytes := mainGoBytes[len(remixBytes):]
// 			fmt.Printf("\n📝 Phần thêm ở main.go (%d bytes):\n", len(extraBytes))
// 			fmt.Printf("   Hex: %s\n", hex.EncodeToString(extraBytes))
// 		}
// 	}

// 	fmt.Println()
// 	fmt.Println("=" + strings.Repeat("=", 80))
// }

// // analyzeBytecode phân tích bytecode để tìm metadata
// func analyzeBytecode(bytecodeHex string) {
// 	bytecodeHex = strings.TrimPrefix(bytecodeHex, "0x")
// 	bytes, err := hex.DecodeString(bytecodeHex)
// 	if err != nil {
// 		fmt.Printf("❌ Lỗi decode bytecode: %v\n", err)
// 		return
// 	}

// 	fmt.Println("=" + strings.Repeat("=", 80))
// 	fmt.Println("PHÂN TÍCH BYTECODE")
// 	fmt.Println("=" + strings.Repeat("=", 80))
// 	fmt.Printf("Total length: %d bytes\n", len(bytes))

// 	// Tìm metadata marker (0xa2 0x64 "ipfs" ...)
// 	// Metadata thường ở cuối bytecode
// 	if len(bytes) > 4 {
// 		// Kiểm tra 4 bytes cuối
// 		last4 := bytes[len(bytes)-4:]
// 		fmt.Printf("Last 4 bytes: %s\n", hex.EncodeToString(last4))

// 		// Tìm pattern "ipfs" hoặc "solc" (metadata markers)
// 		bytecodeStr := string(bytes)
// 		if strings.Contains(bytecodeStr, "ipfs") || strings.Contains(bytecodeStr, "solc") {
// 			fmt.Println("⚠️  Phát hiện metadata trong bytecode!")
// 			// Tìm vị trí bắt đầu metadata (thường là 0xa2)
// 			for i := len(bytes) - 1; i >= 0; i-- {
// 				if bytes[i] == 0xa2 {
// 					fmt.Printf("   Metadata bắt đầu tại byte %d\n", i)
// 					fmt.Printf("   Metadata hex: %s\n", hex.EncodeToString(bytes[i:]))
// 					break
// 				}
// 			}
// 		}
// 	}

// 	// Kiểm tra constructor arguments
// 	// Constructor arguments thường được append sau bytecode
// 	// Không có cách chắc chắn để detect, nhưng nếu bytecode dài hơn bình thường
// 	// và không có metadata, có thể là constructor args
// 	fmt.Println()
// }

// func main() {
// 	if len(os.Args) < 3 {
// 		fmt.Println("Usage: go run compare_bytecode.go <remix_bytecode> <main_go_bytecode>")
// 		fmt.Println("Example:")
// 		fmt.Println("  go run compare_bytecode.go \"0x6080604052...\" \"0x6080604052...\"")
// 		fmt.Println()
// 		fmt.Println("Hoặc chỉ phân tích một bytecode:")
// 		fmt.Println("  go run compare_bytecode.go --analyze \"0x6080604052...\"")
// 		os.Exit(1)
// 	}

// 	if os.Args[1] == "--analyze" {
// 		if len(os.Args) < 3 {
// 			fmt.Println("❌ Thiếu bytecode để phân tích")
// 			os.Exit(1)
// 		}
// 		analyzeBytecode(os.Args[2])
// 		return
// 	}

// 	remixBytecode := os.Args[1]
// 	mainGoBytecode := os.Args[2]

// 	compareBytecode(remixBytecode, mainGoBytecode)
// 	analyzeBytecode(remixBytecode)
// }
