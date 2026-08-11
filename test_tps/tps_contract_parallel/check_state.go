package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

const abiString = `[{"inputs":[{"internalType":"address","name":"user","type":"address"}],"name":"getValue","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"sharedValue","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint256","name":"val","type":"uint256"}],"name":"updateState","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"val","type":"uint256"}],"name":"updateStateConflict","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"values","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`

func main() {
	rpcURL := flag.String("rpc", "http://192.168.1.233:10746", "RPC URL of the node to check")
	contractAddrHex := flag.String("contract", "", "Contract Address (Hex)")
	userAddrHex := flag.String("account", "", "User Account Address (Hex)")
	flag.Parse()

	if *contractAddrHex == "" || *userAddrHex == "" {
		log.Fatalf("Vui lòng cung cấp đầy đủ -contract và -account. VD: go run check_state.go -rpc http://... -contract 0x... -account 0x...")
	}

	client, err := ethclient.Dial(*rpcURL)
	if err != nil {
		log.Fatalf("❌ Không thể kết nối tới RPC %s: %v", *rpcURL, err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(abiString))
	if err != nil {
		log.Fatalf("❌ Lỗi parse ABI: %v", err)
	}

	contractAddr := common.HexToAddress(*contractAddrHex)
	userAddr := common.HexToAddress(*userAddrHex)

	callData, err := parsedABI.Pack("getValue", userAddr)
	if err != nil {
		log.Fatalf("❌ Lỗi pack ABI: %v", err)
	}

	fmt.Printf("🔍 Đang truy vấn State...\n")
	fmt.Printf("   Node RPC: %s\n", *rpcURL)
	fmt.Printf("   Contract: %s\n", contractAddr.Hex())
	fmt.Printf("   Account:  %s\n", userAddr.Hex())

	res, err := client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &contractAddr,
		Data: callData,
	}, nil)

	if err != nil {
		log.Fatalf("❌ Lỗi CallContract: %v\n(Gợi ý: Hãy kiểm tra lại Contract Address hoặc RPC node có đang sống không)", err)
	}

	if len(res) >= 32 {
		val := new(big.Int).SetBytes(res)
		fmt.Printf("\n🎯 KẾT QUẢ TRẢ VỀ TỪ NODE:\n   State (getValue) = %s\n\n", val.String())
	} else {
		fmt.Printf("\n⚠️ Node trả về kết quả rỗng (0 bytes). Có thể Contract này chưa từng được deploy hoặc Address bị sai!\n\n")
	}
}
