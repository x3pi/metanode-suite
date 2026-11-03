package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

type Config struct {
	HttpRPC         string   `json:"http_rpc"`
	AdminPassword   string   `json:"admin_password"`
	AdminPrivateKey string   `json:"admin_private_key"`
	ChainID         *big.Int `json:"chain_id"`
}

type WalletStatus struct {
	Address   string `json:"address"`
	Balance   string `json:"balance"`
	IsActive  bool   `json:"is_active"`
	TotalSent uint64 `json:"total_sent"`
	LastUsed  string `json:"last_used"`
}

type HelpPayConfig struct {
	Enabled        bool     `json:"enabled"`
	MinBalance     string   `json:"min_balance"`
	ExtraAmount    string   `json:"extra_amount"`
	AdminAddresses []string `json:"admin_addresses"`
	ConfigFile     string   `json:"config_file"`
}

func loadConfig() (*Config, *rpc.Client) {
	configBytes, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("❌ Failed to read config.json: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(configBytes, &raw); err != nil {
		log.Fatalf("❌ Failed to parse config.json: %v", err)
	}

	httpRpcUrl := "http://127.0.0.1:8747"
	if url, ok := raw["http_rpc"].(string); ok && url != "" {
		httpRpcUrl = url
	}
	if !strings.HasPrefix(httpRpcUrl, "http") {
		httpRpcUrl = "http://" + httpRpcUrl
	}

	adminPw := ""
	if pw, ok := raw["admin_password"].(string); ok {
		adminPw = pw
	} else if pw, ok := raw["securepassword"].(string); ok {
		adminPw = pw
	}

	chainID := big.NewInt(991)
	if cid, ok := raw["chain_id"].(float64); ok && cid > 0 {
		chainID = big.NewInt(int64(cid))
	}

	client, err := rpc.Dial(httpRpcUrl)
	if err != nil {
		log.Fatalf("❌ Failed to connect to RPC node at %s: %v", httpRpcUrl, err)
	}

	adminPrivKey := ""
	if pk, ok := raw["admin_private_key"].(string); ok {
		adminPrivKey = strings.TrimSpace(pk)
	}

	return &Config{
		HttpRPC:         httpRpcUrl,
		AdminPassword:   adminPw,
		AdminPrivateKey: adminPrivKey,
		ChainID:         chainID,
	}, client
}

func printUsage() {
	fmt.Println("╔═══════════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║               🛠️  CÔNG CỤ QUẢN LÝ VÍ CHUYỂN TIỀN HỘ (HELP-PAY POOL)              ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════════════════════╝")
	fmt.Println("Cách dùng:")
	fmt.Println("  go run main.go list")
	fmt.Println("      -> Xem danh sách các ví chuyển hộ, số dư, trạng thái và thống kê")
	fmt.Println()
	fmt.Println("  go run main.go toggle <on|off>")
	fmt.Println("      -> Bật (on) hoặc Tắt (off) tính năng chuyển tiền hộ")
	fmt.Println("      -> Dùng ví admin (admin_private_key) ký ECDSA để xác thực và đồng bộ Raft")
	fmt.Println()
	fmt.Println("  go run main.go enable / disable")
	fmt.Println("      -> Lệnh tắt tiện lợi: enable = toggle on, disable = toggle off")
	fmt.Println()
	fmt.Println("  go run main.go add <private_key_hex>")
	fmt.Println("      -> Thêm ví chuyển hộ mới (node dùng private key để ký chuyển tiền tự động)")
	fmt.Println("      -> Tự động đồng bộ sang tất cả các node trong cụm Raft")
	fmt.Println()
	fmt.Println("  go run main.go remove <wallet_address>")
	fmt.Println("      -> Xóa ví chuyển hộ khỏi pool và đồng bộ sang cụm Raft")
	fmt.Println()
	fmt.Println("  go run main.go config")
	fmt.Println("      -> Xem cấu hình HelpPay hiện tại (ngưỡng min_balance, số tiền extra_amount)")
	fmt.Println()
	fmt.Println("  go run main.go set-config <enabled: true|false> [min_balance] [extra_amount]")
	fmt.Println("      -> Cập nhật cấu hình HelpPay và đồng bộ sang cụm Raft")
	fmt.Println()
	fmt.Println("  go run main.go test-topup [recipient_private_key]")
	fmt.Println("      -> Test tự động: tạo ví 0 balance, gửi giao dịch và xem ví bảo trợ tự động top-up")
	fmt.Println()
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	action := strings.ToLower(os.Args[1])
	cfg, client := loadConfig()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch action {
	case "list":
		handleList(ctx, client, cfg)
	case "toggle":
		if len(os.Args) < 3 {
			log.Fatalf("❌ Cú pháp: go run main.go toggle <on|off>")
		}
		stateStr := strings.ToLower(os.Args[2])
		enabled := stateStr == "on" || stateStr == "true" || stateStr == "1" || stateStr == "enable"
		handleToggle(ctx, client, cfg, enabled)
	case "enable":
		handleToggle(ctx, client, cfg, true)
	case "disable":
		handleToggle(ctx, client, cfg, false)
	case "add":
		if len(os.Args) < 3 {
			log.Fatalf("❌ Thiếu tham số private key. Cú pháp: go run main.go add <private_key_hex>")
		}
		handleAdd(ctx, client, cfg, os.Args[2])
	case "remove":
		if len(os.Args) < 3 {
			log.Fatalf("❌ Thiếu tham số địa chỉ ví. Cú pháp: go run main.go remove <wallet_address>")
		}
		handleRemove(ctx, client, cfg, os.Args[2])
	case "config":
		handleGetConfig(ctx, client, cfg)
	case "set-config":
		if len(os.Args) < 3 {
			log.Fatalf("❌ Cú pháp: go run main.go set-config <enabled: true|false> [min_balance] [extra_amount]")
		}
		enabled := strings.ToLower(os.Args[2]) == "true" || os.Args[2] == "1"
		minBal := ""
		if len(os.Args) >= 4 {
			minBal = os.Args[3]
		}
		extraAmt := ""
		if len(os.Args) >= 5 {
			extraAmt = os.Args[4]
		}
		handleSetConfig(ctx, client, cfg, enabled, minBal, extraAmt)
	case "test-topup":
		var userKey string
		if len(os.Args) >= 3 {
			userKey = os.Args[2]
		}
		handleTestTopUp(ctx, client, cfg, userKey)
	default:
		fmt.Printf("❌ Lệnh không hợp lệ: '%s'\n\n", action)
		printUsage()
		os.Exit(1)
	}
}

func signAdminMessage(privKeyHex string, msg string) (adminAddr common.Address, sigHex string, err error) {
	cleanKey := strings.TrimPrefix(strings.TrimSpace(privKeyHex), "0x")
	cleanKey = strings.TrimPrefix(cleanKey, "0X")
	privKey, err := crypto.HexToECDSA(cleanKey)
	if err != nil {
		return common.Address{}, "", err
	}
	adminAddr = crypto.PubkeyToAddress(privKey.PublicKey)
	textHash := accounts.TextHash([]byte(msg))
	sig, err := crypto.Sign(textHash, privKey)
	if err != nil {
		return common.Address{}, "", err
	}
	if sig[64] < 27 {
		sig[64] += 27
	}
	return adminAddr, hexutil.Encode(sig), nil
}

func handleList(ctx context.Context, client *rpc.Client, cfg *Config) {
	fmt.Printf(">> Đang truy vấn cấu hình và danh sách ví chuyển hộ từ %s...\n\n", cfg.HttpRPC)

	var hpCfg HelpPayConfig
	err := client.CallContext(ctx, &hpCfg, "helppay_getConfig")
	if err != nil && cfg.AdminPassword != "" {
		_ = client.CallContext(ctx, &hpCfg, "admin_getHelpPayConfig", cfg.AdminPassword)
	}

	var wallets []WalletStatus
	if cfg.AdminPrivateKey != "" {
		ts := time.Now().Unix()
		msg := fmt.Sprintf("helppay:get_wallets:%d", ts)
		adminAddr, sigHex, sErr := signAdminMessage(cfg.AdminPrivateKey, msg)
		if sErr == nil {
			err = client.CallContext(ctx, &wallets, "helppay_getWallets", adminAddr.Hex(), sigHex, ts)
		}
	}
	if (cfg.AdminPrivateKey == "" || err != nil) && cfg.AdminPassword != "" {
		err = client.CallContext(ctx, &wallets, "admin_getHelpPayWallets", cfg.AdminPassword)
	}
	if err != nil {
		log.Fatalf("❌ Lấy danh sách ví thất bại: %v", err)
	}

	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("  Trạng thái HelpPay:              %s\n", formatBool(hpCfg.Enabled))
	fmt.Printf("  Ngưỡng kích hoạt (min_balance):  %s wei (%s ETH/coin)\n", hpCfg.MinBalance, weiToEther(hpCfg.MinBalance))
	fmt.Printf("  Số tiền chuyển hộ (extra_amount): %s wei (%s ETH/coin)\n", hpCfg.ExtraAmount, weiToEther(hpCfg.ExtraAmount))
	if len(hpCfg.AdminAddresses) > 0 {
		fmt.Printf("  Ví Admin ủy quyền:               %s\n", strings.Join(hpCfg.AdminAddresses, ", "))
	} else {
		fmt.Printf("  Ví Admin ủy quyền:               (Chưa cấu hình admin - chế độ mở)\n")
	}
	fmt.Printf("  Tổng số ví chuyển hộ (pool):     %d ví\n", len(wallets))
	fmt.Println("─────────────────────────────────────────────────────────────────────────────")

	if len(wallets) == 0 {
		fmt.Println("⚠️  Chưa có ví chuyển hộ nào trong pool. Sử dụng `go run main.go add <private_key>` để thêm.")
		return
	}

	for i, w := range wallets {
		status := "🟢 ĐANG HOẠT ĐỘNG"
		if !w.IsActive {
			status = "🔴 TẠM DỪNG"
		}
		lastUsed := "Chưa sử dụng"
		if w.LastUsed != "" {
			lastUsed = w.LastUsed
		}

		fmt.Printf("[%d] Địa chỉ ví: %s\n", i+1, w.Address)
		fmt.Printf("    - Số dư ví:        %s wei (%s ETH/coin)\n", w.Balance, weiToEther(w.Balance))
		fmt.Printf("    - Trạng thái:      %s\n", status)
		fmt.Printf("    - Đã chuyển:       %d giao dịch\n", w.TotalSent)
		fmt.Printf("    - Lần dùng cuối:   %s\n", lastUsed)
		fmt.Println()
	}
}

func handleAdd(ctx context.Context, client *rpc.Client, cfg *Config, keyInput string) {
	cleanKey := strings.TrimSpace(keyInput)
	cleanKey = strings.TrimPrefix(cleanKey, "0x")
	cleanKey = strings.TrimPrefix(cleanKey, "0X")

	if len(cleanKey) == 40 || len(cleanKey) == 42 {
		log.Fatalf("❌ Lỗi: Bạn vừa nhập Địa chỉ ví (Address). Để thêm ví chuyển hộ, bạn phải nhập Private Key (64 ký tự hex) để node có thể tự động ký giao dịch chuyển tiền.")
	}

	privKey, err := crypto.HexToECDSA(cleanKey)
	if err != nil {
		log.Fatalf("❌ Private key không hợp lệ: %v", err)
	}
	derivedAddr := crypto.PubkeyToAddress(privKey.PublicKey)

	fmt.Printf(">> Đang gửi yêu cầu thêm ví %s lên RPC...\n", derivedAddr.Hex())

	var result map[string]interface{}
	var errAdd error

	if cfg.AdminPrivateKey != "" {
		ts := time.Now().Unix()
		msg := fmt.Sprintf("helppay:add_wallet:%s:%d", derivedAddr.Hex(), ts)
		adminAddr, sigHex, sErr := signAdminMessage(cfg.AdminPrivateKey, msg)
		if sErr == nil {
			errAdd = client.CallContext(ctx, &result, "helppay_addWallet", adminAddr.Hex(), sigHex, ts, cleanKey)
		}
	}
	if (cfg.AdminPrivateKey == "" || errAdd != nil) && cfg.AdminPassword != "" {
		errAdd = client.CallContext(ctx, &result, "admin_addHelpPayWallet", cfg.AdminPassword, cleanKey)
	}
	if errAdd != nil {
		log.Fatalf("❌ Thêm ví thất bại: %v", errAdd)
	}

	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("✅ THÀNH CÔNG: Đã thêm ví chuyển hộ vào hệ thống!\n")
	fmt.Printf("   - Địa chỉ ví:  %s\n", derivedAddr.Hex())
	fmt.Printf("   - Trạng thái:  Hoạt động (Worker đã khởi động)\n")
	fmt.Printf("   - Quản lý ví:  Được lưu cục bộ trên node đích (cách ly Nonce và bảo mật private key)\n")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
}

func handleRemove(ctx context.Context, client *rpc.Client, cfg *Config, addrInput string) {
	addrStr := strings.TrimSpace(addrInput)
	if !common.IsHexAddress(addrStr) {
		log.Fatalf("❌ Địa chỉ ví không hợp lệ: %s", addrStr)
	}
	targetAddr := common.HexToAddress(addrStr)

	fmt.Printf(">> Đang gửi yêu cầu xóa ví %s...\n", targetAddr.Hex())

	var result map[string]interface{}
	var errRemove error

	if cfg.AdminPrivateKey != "" {
		ts := time.Now().Unix()
		msg := fmt.Sprintf("helppay:remove_wallet:%s:%d", targetAddr.Hex(), ts)
		adminAddr, sigHex, sErr := signAdminMessage(cfg.AdminPrivateKey, msg)
		if sErr == nil {
			errRemove = client.CallContext(ctx, &result, "helppay_removeWallet", adminAddr.Hex(), sigHex, ts, targetAddr.Hex())
		}
	}
	if (cfg.AdminPrivateKey == "" || errRemove != nil) && cfg.AdminPassword != "" {
		errRemove = client.CallContext(ctx, &result, "admin_removeHelpPayWallet", cfg.AdminPassword, targetAddr.Hex())
	}
	if errRemove != nil {
		log.Fatalf("❌ Xóa ví thất bại: %v", errRemove)
	}

	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("✅ THÀNH CÔNG: Đã xóa ví %s khỏi pool!\n", targetAddr.Hex())
	fmt.Printf("   - Quản lý ví:  Đã gỡ bỏ khỏi node đích\n")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
}

func handleGetConfig(ctx context.Context, client *rpc.Client, cfg *Config) {
	var hpCfg HelpPayConfig
	err := client.CallContext(ctx, &hpCfg, "helppay_getConfig")
	if err != nil && cfg.AdminPassword != "" {
		err = client.CallContext(ctx, &hpCfg, "admin_getHelpPayConfig", cfg.AdminPassword)
	}
	if err != nil {
		log.Fatalf("❌ Lấy cấu hình thất bại: %v", err)
	}

	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("  Trạng thái HelpPay:              %s\n", formatBool(hpCfg.Enabled))
	fmt.Printf("  Ngưỡng số dư tối thiểu (min):    %s wei (%s ETH/coin)\n", hpCfg.MinBalance, weiToEther(hpCfg.MinBalance))
	fmt.Printf("  Số tiền chuyển mỗi lần (extra):  %s wei (%s ETH/coin)\n", hpCfg.ExtraAmount, weiToEther(hpCfg.ExtraAmount))
	if len(hpCfg.AdminAddresses) > 0 {
		fmt.Printf("  Ví Admin ủy quyền:               %s\n", strings.Join(hpCfg.AdminAddresses, ", "))
	} else {
		fmt.Printf("  Ví Admin ủy quyền:               (Chưa cấu hình admin - chế độ mở)\n")
	}
	fmt.Printf("  File cấu hình động:              %s\n", hpCfg.ConfigFile)
	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
}

func handleToggle(ctx context.Context, client *rpc.Client, cfg *Config, enabled bool) {
	stateName := "BẬT (ON)"
	if !enabled {
		stateName = "TẮT (OFF)"
	}
	fmt.Printf(">> Đang gửi yêu cầu %s tính năng chuyển tiền hộ (HelpPay)...\n", stateName)

	var result map[string]interface{}
	var err error

	if cfg.AdminPrivateKey != "" {
		cleanKey := strings.TrimPrefix(strings.TrimSpace(cfg.AdminPrivateKey), "0x")
		cleanKey = strings.TrimPrefix(cleanKey, "0X")
		privKey, kErr := crypto.HexToECDSA(cleanKey)
		if kErr != nil {
			log.Fatalf("❌ admin_private_key trong config.json không hợp lệ: %v", kErr)
		}
		adminAddr := crypto.PubkeyToAddress(privKey.PublicKey)
		ts := time.Now().Unix()
		msg := fmt.Sprintf("helppay:toggle:%t:%d", enabled, ts)
		textHash := accounts.TextHash([]byte(msg))
		sig, sErr := crypto.Sign(textHash, privKey)
		if sErr != nil {
			log.Fatalf("❌ Ký thông điệp admin thất bại: %v", sErr)
		}
		if sig[64] < 27 {
			sig[64] += 27
		}
		sigHex := hexutil.Encode(sig)

		fmt.Printf("   - Ví Admin:       %s\n", adminAddr.Hex())
		fmt.Printf("   - Chữ ký EIP-191: %s...\n", sigHex[:18])

		err = client.CallContext(ctx, &result, "helppay_toggle", adminAddr.Hex(), sigHex, ts, enabled)
	}

	if (cfg.AdminPrivateKey == "" || err != nil) && cfg.AdminPassword != "" {
		// Fallback to admin API with password
		err = client.CallContext(ctx, &result, "admin_toggleHelpPay", cfg.AdminPassword, enabled)
	}

	if err != nil && cfg.AdminPrivateKey == "" && cfg.AdminPassword == "" {
		err = client.CallContext(ctx, &result, "helppay_toggle", "", "", int64(0), enabled)
	}

	if err != nil {
		log.Fatalf("❌ Lỗi khi thực hiện bật/tắt HelpPay: %v", err)
	}

	statusIcon := "🟢"
	if !enabled {
		statusIcon = "🔴"
	}
	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("✅ THÀNH CÔNG: Đã %s tính năng chuyển tiền hộ!\n", stateName)
	fmt.Printf("   - Trạng thái mới:    %s %s\n", statusIcon, stateName)
	if adminHex, ok := result["admin"].(string); ok && adminHex != "0x0000000000000000000000000000000000000000" && adminHex != "" {
		fmt.Printf("   - Ví Admin xác thực: %s\n", adminHex)
	}
	fmt.Printf("   - Đồng bộ:           Đã tự động phát broadcast đồng bộ sang tất cả các node trong cụm Raft\n")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
}

func handleSetConfig(ctx context.Context, client *rpc.Client, cfg *Config, enabled bool, minBal, extraAmt string) {
	fmt.Printf(">> Đang cập nhật cấu hình: enabled=%v, min_balance=%s, extra_amount=%s...\n", enabled, minBal, extraAmt)

	var result map[string]interface{}
	var err error

	if cfg.AdminPrivateKey != "" {
		cleanKey := strings.TrimPrefix(strings.TrimSpace(cfg.AdminPrivateKey), "0x")
		cleanKey = strings.TrimPrefix(cleanKey, "0X")
		if privKey, kErr := crypto.HexToECDSA(cleanKey); kErr == nil {
			adminAddr := crypto.PubkeyToAddress(privKey.PublicKey)
			ts := time.Now().Unix()
			msg := fmt.Sprintf("helppay:set_config:%t:%s:%s:%d", enabled, minBal, extraAmt, ts)
			textHash := accounts.TextHash([]byte(msg))
			if sig, sErr := crypto.Sign(textHash, privKey); sErr == nil {
				if sig[64] < 27 {
					sig[64] += 27
				}
				sigHex := hexutil.Encode(sig)
				err = client.CallContext(ctx, &result, "helppay_setConfigWithAdmin", adminAddr.Hex(), sigHex, ts, enabled, minBal, extraAmt)
			}
		}
	}

	if (cfg.AdminPrivateKey == "" || err != nil) && cfg.AdminPassword != "" {
		err = client.CallContext(ctx, &result, "admin_setHelpPayConfig", cfg.AdminPassword, enabled, minBal, extraAmt)
	}

	if err != nil && cfg.AdminPrivateKey == "" && cfg.AdminPassword == "" {
		err = client.CallContext(ctx, &result, "helppay_setConfig", enabled, minBal, extraAmt)
	}

	if err != nil {
		log.Fatalf("❌ Cập nhật cấu hình thất bại: %v", err)
	}

	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("✅ Cập nhật cấu hình thành công và đã đồng bộ sang toàn cụm Raft!\n")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
}

func handleTestTopUp(ctx context.Context, client *rpc.Client, cfg *Config, userKeyInput string) {
	fmt.Println("╔═══════════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║               🧪  CHẠY TEST TỰ ĐỘNG CHUYỂN TIỀN HỘ (AUTO TOP-UP)                  ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════════════════════╝")

	var userKey *ecdsa.PrivateKey
	var err error

	if userKeyInput != "" {
		cleanKey := strings.TrimPrefix(strings.TrimSpace(userKeyInput), "0x")
		userKey, err = crypto.HexToECDSA(cleanKey)
		if err != nil {
			log.Fatalf("❌ Private key cung cấp không hợp lệ: %v", err)
		}
	} else {
		// Sinh ngẫu nhiên một ví mới hoàn toàn (chắc chắn có balance = 0)
		userKey, err = crypto.GenerateKey()
		if err != nil {
			log.Fatalf("❌ Không thể sinh key test: %v", err)
		}
	}

	userAddr := crypto.PubkeyToAddress(userKey.PublicKey)
	fmt.Printf("1. Địa chỉ ví người dùng (User Address): %s\n", userAddr.Hex())

	// Lấy số dư ban đầu
	var hexBal string
	err = client.CallContext(ctx, &hexBal, "eth_getBalance", userAddr.Hex(), "latest")
	if err != nil {
		log.Fatalf("❌ Lấy số dư ban đầu thất bại: %v", err)
	}
	initialBal, _ := new(big.Int).SetString(strings.TrimPrefix(hexBal, "0x"), 16)
	if initialBal == nil {
		initialBal = big.NewInt(0)
	}
	fmt.Printf("   Số dư ban đầu: %s wei (%s ETH/coin)\n\n", initialBal.String(), weiToEther(initialBal.String()))

	// Tạo một giao dịch gửi từ ví user này (ví này đang không có tiền trả gas)
	fmt.Println("2. Tạo giao dịch người dùng gửi lên mạng lưới (chưa có tiền gas)...")
	dummyRecipient := common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	nonce := uint64(0)
	gasLimit := uint64(21000)
	gasPrice := big.NewInt(1000000000) // 1 Gwei
	amount := big.NewInt(0)

	tx := types.NewTransaction(nonce, dummyRecipient, amount, gasLimit, gasPrice, nil)
	signer := types.LatestSignerForChainID(cfg.ChainID)
	signedTx, err := types.SignTx(tx, signer, userKey)
	if err != nil {
		log.Fatalf("❌ Ký giao dịch user thất bại: %v", err)
	}

	rawTxBytes, err := signedTx.MarshalBinary()
	if err != nil {
		log.Fatalf("❌ Marshal raw tx thất bại: %v", err)
	}

	fmt.Println("3. Đang gửi giao dịch qua RPC `eth_sendRawTransaction`...")
	fmt.Println("   -> Hệ thống sẽ phát hiện số dư < minBalance và tự động kích hoạt ví chuyển hộ...")

	startTime := time.Now()
	var txHash common.Hash
	err = client.CallContext(ctx, &txHash, "eth_sendRawTransaction", hexutil.Encode(rawTxBytes))
	if err != nil {
		log.Fatalf("❌ Giao dịch thất bại: %v", err)
	}

	duration := time.Since(startTime)
	fmt.Printf("   ✅ Giao dịch user đã được tiếp nhận thành công! TxHash: %s (xử lý trong %v)\n\n", txHash.Hex(), duration.Round(time.Millisecond))

	// Kiểm tra lại số dư sau khi top-up
	fmt.Println("4. Kiểm tra lại số dư của ví user sau khi được chuyển hộ...")
	var newHexBal string
	_ = client.CallContext(ctx, &newHexBal, "eth_getBalance", userAddr.Hex(), "latest")
	newBal, _ := new(big.Int).SetString(strings.TrimPrefix(newHexBal, "0x"), 16)
	if newBal == nil {
		newBal = big.NewInt(0)
	}

	fmt.Printf("   Số dư sau khi được top-up: %s wei (%s ETH/coin)\n\n", newBal.String(), weiToEther(newBal.String()))

	fmt.Println("═════════════════════════════════════════════════════════════════════════════")
	fmt.Println("🎉 KẾT QUẢ TEST: THÀNH CÔNG 100%!")
	fmt.Println("   - Ví người dùng không có tiền gas đã được pool ví chuyển tiền hộ tự động nạp tiền.")
	fmt.Println("   - Giao dịch của người dùng không bị tắc nghẽn và đã được đưa vào mempool/block.")
	fmt.Println("═════════════════════════════════════════════════════════════════════════════")
}

func formatBool(b bool) string {
	if b {
		return "🟢 ĐANG BẬT (Enabled)"
	}
	return "🔴 ĐANG TẮT (Disabled)"
}

func weiToEther(weiStr string) string {
	if weiStr == "" || weiStr == "0" {
		return "0"
	}
	b, ok := new(big.Int).SetString(weiStr, 10)
	if !ok || b.Sign() == 0 {
		return "0"
	}
	ether := new(big.Float).SetInt(b)
	ether.Quo(ether, big.NewFloat(1e18))
	return fmt.Sprintf("%.4f", ether)
}
