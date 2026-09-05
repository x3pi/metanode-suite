package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"
)

const (
	ColorReset  = "\033[0m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorCyan   = "\033[36m"
	ColorBold   = "\033[1m"
	ColorPurple = "\033[35m"
	ColorRed    = "\033[31m"
)

type ChainConfig struct {
	ChainID     int64    `json:"chain_id"`
	RPCUrl      string   `json:"rpc_url"`
	PrivateKeys []string `json:"private_keys"`
}

type Config struct {
	RPCUrl        string                 `json:"rpc_url"`
	ChainID       int64                  `json:"chain_id"`
	PrivateKeys   []string               `json:"private_keys"`
	PrivateChains map[string]ChainConfig `json:"private_chains"`
}

func formatMTN(wei *big.Int) string {
	if wei == nil {
		return "0.0000 MTN"
	}
	if wei.Sign() == 0 {
		return "0 MTN (0 wei)"
	}
	f := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(1e18))
	// Nếu số wei nhỏ hơn 1e12 (0.000001 MTN), hiển thị chi tiết cả wei và số thập phân sâu để không bị làm tròn về 0
	if wei.Cmp(big.NewInt(1e12)) < 0 {
		return fmt.Sprintf("%.10f MTN (%s wei)", f, wei.String())
	}
	return fmt.Sprintf("%.6f MTN", f)
}

func main() {
	var configPath string
	var rpcFlag string
	var chainIDFlag int64
	var sponsorKeyFlag string
	var userKeyFlag string

	flag.StringVar(&configPath, "config", "../config.json", "Đường dẫn file config.json")
	flag.StringVar(&rpcFlag, "rpc", "", "RPC URL node (tùy chọn, ghi đè config)")
	flag.Int64Var(&chainIDFlag, "chainid", 0, "Chain ID (tùy chọn, ghi đè config)")
	flag.StringVar(&sponsorKeyFlag, "sponsor-key", "", "Private key của ví bảo trợ gas (Sponsor / Paymaster)")
	flag.StringVar(&userKeyFlag, "user-key", "", "Private key của ví người dùng (User Authority)")
	flag.Parse()

	fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println(ColorBold + ColorCyan + "👛 VÍ DỤ: VÍ TRẢ PHÍ HỘ (GAS SPONSORSHIP) BẰNG EIP-7702 SETCODE TX (0x04)" + ColorReset)
	fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println("📖 MÔ TẢ   : Cho phép User (Authority) ký ủy quyền (Authorization) mà không tốn gas.")
	fmt.Println("⚡ GỌI     : Ví Sponsor (Paymaster) nộp SetCodeTx (TxType 0x04) và chịu 100% phí gas.")
	fmt.Println("🎯 KỲ VỌNG : User được gán code delegate, số dư User không đổi, Sponsor trừ gas.")
	fmt.Println(ColorBold + ColorCyan + "══════════════════════════════════════════════════════════════════════════════\n" + ColorReset)

	// 1. Đọc và phân tích config
	var rpcURL string
	var chainIDInt64 int64
	var keySponsorHex string
	var keyUserHex string

	if raw, err := os.ReadFile(configPath); err == nil {
		var cfg Config
		if err := json.Unmarshal(raw, &cfg); err == nil {
			// Ưu tiên chain_a nếu có private_chains, hoặc dùng root config
			if chainA, exists := cfg.PrivateChains["chain_a"]; exists && chainA.RPCUrl != "" {
				rpcURL = chainA.RPCUrl
				chainIDInt64 = chainA.ChainID
				if len(chainA.PrivateKeys) > 0 {
					keySponsorHex = chainA.PrivateKeys[0]
				}
				if len(chainA.PrivateKeys) > 1 {
					keyUserHex = chainA.PrivateKeys[1]
				}
			} else {
				rpcURL = cfg.RPCUrl
				chainIDInt64 = cfg.ChainID
				if len(cfg.PrivateKeys) > 0 {
					keySponsorHex = cfg.PrivateKeys[0]
				}
				if len(cfg.PrivateKeys) > 1 {
					keyUserHex = cfg.PrivateKeys[1]
				}
			}
		}
	}

	// Ghi đè bằng CLI flag nếu có
	if rpcFlag != "" {
		rpcURL = rpcFlag
	}
	if chainIDFlag != 0 {
		chainIDInt64 = chainIDFlag
	}
	if sponsorKeyFlag != "" {
		keySponsorHex = sponsorKeyFlag
	}
	if userKeyFlag != "" {
		keyUserHex = userKeyFlag
	}

	if rpcURL == "" {
		rpcURL = "http://127.0.0.1:8546"
	}
	if keySponsorHex == "" {
		keySponsorHex = "f0c569debd26c9e08924ead34931482ae9267b6cb8e6666bf7fc8023ca6a4106"
	}
	if keyUserHex == "" {
		// Tạo key mới nếu không có để giả lập user mới chưa có đồng nào
		keyUserHex = "edb63ea1c26ce5c5d29df010ddedf2c57b3a4af38d776290f9a789205366acea"
	}

	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC (%s): %v", rpcURL, err)
	}

	pkSponsor, err := crypto.HexToECDSA(keySponsorHex)
	if err != nil {
		log.Fatalf("❌ Parse private key Sponsor thất bại: %v", err)
	}
	sponsorAddr := crypto.PubkeyToAddress(pkSponsor.PublicKey)

	pkUser, err := crypto.HexToECDSA(keyUserHex)
	if err != nil {
		log.Fatalf("❌ Parse private key User thất bại: %v", err)
	}
	userAddr := crypto.PubkeyToAddress(pkUser.PublicKey)

	// Lấy Chain ID từ RPC nếu chưa rõ
	var chainID *big.Int
	if chainIDInt64 > 0 {
		chainID = big.NewInt(chainIDInt64)
	} else {
		cid, err := client.ChainID(context.Background())
		if err != nil {
			log.Fatalf("❌ Lấy ChainID từ RPC thất bại: %v", err)
		}
		chainID = cid
	}

	// 2. Tra cứu số dư và Nonce ban đầu
	balSponsorBefore, _ := client.BalanceAt(context.Background(), sponsorAddr, nil)
	balUserBefore, _ := client.BalanceAt(context.Background(), userAddr, nil)
	sponsorNonce, err := client.PendingNonceAt(context.Background(), sponsorAddr)
	if err != nil {
		log.Fatalf("❌ Không lấy được nonce sponsor: %v", err)
	}
	userNonce, err := client.PendingNonceAt(context.Background(), userAddr)
	if err != nil {
		log.Fatalf("❌ Không lấy được nonce user: %v", err)
	}

	// Địa chỉ Smart Contract Logic sẽ được delegate (ví dụ: ERC-4337 smart wallet implementation)
	delegateContract := common.HexToAddress("0x0000000000000000000000000000000000007702")

	fmt.Println("📋 " + ColorBold + "THÔNG TIN CẤU HÌNH & TÀI KHOẢN:" + ColorReset)
	fmt.Printf("   🌐 RPC URL          : %s\n", rpcURL)
	fmt.Printf("   🆔 Chain ID         : %s\n", chainID.String())
	fmt.Printf("   💳 Ví Sponsor (Trả Gas): %s%s%s (Nonce: %d, Số dư: %s)\n", ColorPurple, sponsorAddr.Hex(), ColorReset, sponsorNonce, formatMTN(balSponsorBefore))
	fmt.Printf("   👤 Ví User (Được tài trợ): %s%s%s (Nonce: %d, Số dư: %s)\n", ColorYellow, userAddr.Hex(), ColorReset, userNonce, formatMTN(balUserBefore))
	fmt.Printf("   📜 Delegate Contract : %s\n\n", delegateContract.Hex())

	// 3. Bước 1: User ký Authorization Tuple (EIP-7702) Offline
	fmt.Println("🔹 " + ColorBold + "BƯỚC 1: User ký Authorization Tuple (EIP-7702)" + ColorReset)
	authTuple := types.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: delegateContract,
		Nonce:   userNonce,
	}
	signedAuth, err := types.SignSetCode(pkUser, authTuple)
	if err != nil {
		log.Fatalf("❌ Ký SetCode Authorization thất bại: %v", err)
	}
	fmt.Printf("   ✍️ User đã ký offline ủy quyền delegate tới %s với nonce = %d\n", delegateContract.Hex(), userNonce)

	// 4. Bước 2: Sponsor đóng gói SetCodeTx (TxType 0x04) và ký giao dịch
	fmt.Println("\n🔹 " + ColorBold + "BƯỚC 2: Sponsor tạo SetCodeTx (0x04) & ký trả gas" + ColorReset)
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil || gasPrice.Cmp(big.NewInt(0)) == 0 {
		gasPrice = big.NewInt(20_000_000_000) // 20 Gwei
	}
	gasTipCap, err := client.SuggestGasTipCap(context.Background())
	if err != nil || gasTipCap.Cmp(big.NewInt(0)) == 0 {
		gasTipCap = big.NewInt(1_000_000_000) // 1 Gwei
	}

	setCodeTxData := &types.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     sponsorNonce,
		GasTipCap: uint256.MustFromBig(gasTipCap),
		GasFeeCap: uint256.MustFromBig(gasPrice),
		Gas:       250000,
		To:        userAddr, // Gửi tương tác đến địa chỉ User
		Value:     uint256.NewInt(0),
		Data:      []byte{},
		AuthList:  []types.SetCodeAuthorization{signedAuth},
	}

	signer := types.NewPragueSigner(chainID)
	signedTx, err := types.SignNewTx(pkSponsor, signer, setCodeTxData)
	if err != nil {
		log.Fatalf("❌ Sponsor ký SetCodeTx thất bại: %v", err)
	}
	fmt.Printf("   📦 Đã gói AuthList vào SetCodeTx. TxHash: %s%s%s (Type=0x%02x)\n", ColorCyan, signedTx.Hash().Hex(), ColorReset, signedTx.Type())

	// 5. Bước 3: Sponsor phát giao dịch qua RPC
	fmt.Println("\n🔹 " + ColorBold + "BƯỚC 3: Phát giao dịch lên Blockchain qua RPC" + ColorReset)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		log.Fatalf("❌ Lỗi gửi SetCodeTx qua RPC: %v", err)
	}
	fmt.Printf("   🚀 Giao dịch đã được gửi vào Mempool! Chờ đào block...\n")

	// 6. Chờ Transaction Receipt
	var receipt *types.Receipt
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		receipt, err = client.TransactionReceipt(context.Background(), signedTx.Hash())
		if err == nil && receipt != nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if receipt == nil {
		log.Fatalf("❌ Timeout khi chờ receipt cho giao dịch %s", signedTx.Hash().Hex())
	}

	fmt.Println("\n" + ColorBold + "📄 KẾT QUẢ TRANSACTION RECEIPT:" + ColorReset)
	fmt.Printf("   ├─ Block Number : %d\n", receipt.BlockNumber.Uint64())
	fmt.Printf("   ├─ Status       : %d (%s)\n", receipt.Status, map[uint64]string{1: ColorGreen + "SUCCESS" + ColorReset, 0: ColorRed + "FAILED" + ColorReset}[receipt.Status])
	fmt.Printf("   ├─ Tx Type      : 0x%02x (EIP-7702 SetCodeTx)\n", receipt.Type)
	fmt.Printf("   └─ Gas Used     : %d\n", receipt.GasUsed)

	// 7. Kiểm tra kết quả số dư và Code
	balSponsorAfter, _ := client.BalanceAt(context.Background(), sponsorAddr, nil)
	balUserAfter, _ := client.BalanceAt(context.Background(), userAddr, nil)
	userCode, _ := client.CodeAt(context.Background(), userAddr, nil)

	fmt.Println("\n" + ColorBold + "📊 ĐỐI SOÁT TÀI KHOẢN & PHÍ GAS:" + ColorReset)
	sponsorSpent := new(big.Int).Sub(balSponsorBefore, balSponsorAfter)
	userSpent := new(big.Int).Sub(balUserBefore, balUserAfter)
	fmt.Printf("   💳 Sponsor chi trả gas : %s%s%s (Số dư mới: %s)\n", ColorPurple, formatMTN(sponsorSpent), ColorReset, formatMTN(balSponsorAfter))
	fmt.Printf("   👤 User chi trả gas    : %s%s%s (Số dư mới: %s)\n", ColorGreen, formatMTN(userSpent), ColorReset, formatMTN(balUserAfter))
	fmt.Printf("   🛡️ Delegation Code User: %s (len: %d bytes)\n", hexutil.Encode(userCode), len(userCode))

	if receipt.Status == 1 && receipt.Type == types.SetCodeTxType {
		fmt.Printf("\n%s🎉 BÀI TEST VÍ TRẢ PHÍ HỘ (EIP-7702 SPONSORED TX) HOÀN TẤT THÀNH CÔNG 100%%!%s\n", ColorGreen+ColorBold, ColorReset)
	} else {
		fmt.Printf("\n%s❌ TEST CHƯA ĐẠT KỲ VỌNG!%s\n", ColorRed+ColorBold, ColorReset)
		os.Exit(1)
	}
}
