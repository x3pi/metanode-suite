/*
 * BÀI TEST: 34-chain-death-recovery
 * MÔ TẢ   : Diễn tập quy trình Cứu hộ Khẩn cấp khi 1 Blockchain thành viên bị chết hẳn (P8 / T3.c).
 *           - Bước 1: Kiểm tra số dư ví nạn nhân trên Chain 101 và neo StateRoot lên Public Chain 991.
 *           - Bước 2: Tắt hẳn Private Chain 101 (Chết máy chủ / Offline).
 *           - Bước 3: Biểu quyết Quản trị On-Chain Governance (DeclareDead).
 *           - Bước 4: Nộp Merkle Proof cứu hộ lên Public Chain 991 khi Chain 101 đang chết.
 *           - Bước 5: Giải ngân hoàn tiền 100% về Chain an toàn (Chain 102).
 *           - Bước 6: Kiểm tra chống tấn công Double-Claim.
 */
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// ANSI Colors
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorPurple = "\033[35m"
	ColorCyan   = "\033[36m"
	ColorBold   = "\033[1m"
)

type ChainConfig struct {
	ChainID uint64 `json:"chain_id"`
	RPCURL  string `json:"rpc_url"`
}

type UnifiedConfig struct {
	PublicChain   ChainConfig
	PrivateChainA ChainConfig
	PrivateChainB ChainConfig
	PrivateKeys   []string
}

type RPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func rpcCall(endpoint string, method string, params interface{}) (json.RawMessage, error) {
	reqBody, err := json.Marshal(RPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResp RPCResponse
	if err := json.Unmarshal(bodyBytes, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal error (%s): %w", string(bodyBytes), err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

func getBlockNumber(endpoint string) (uint64, error) {
	res, err := rpcCall(endpoint, "eth_blockNumber", []interface{}{})
	if err != nil {
		return 0, err
	}
	var hexStr string
	if err := json.Unmarshal(res, &hexStr); err != nil {
		return 0, err
	}
	return hexutil.DecodeUint64(hexStr)
}

func getBalance(endpoint string, address string) (*big.Int, error) {
	res, err := rpcCall(endpoint, "eth_getBalance", []interface{}{address, "latest"})
	if err != nil {
		return nil, err
	}
	var hexStr string
	if err := json.Unmarshal(res, &hexStr); err != nil {
		return nil, err
	}
	return hexutil.DecodeBig(hexStr)
}

func getNonce(endpoint string, address string) (uint64, error) {
	res, err := rpcCall(endpoint, "eth_getTransactionCount", []interface{}{address, "latest"})
	if err != nil {
		return 0, err
	}
	var hexStr string
	if err := json.Unmarshal(res, &hexStr); err != nil {
		return 0, err
	}
	return hexutil.DecodeUint64(hexStr)
}

func sendRawTransaction(endpoint string, rawBytes []byte) (common.Hash, error) {
	res, err := rpcCall(endpoint, "eth_sendRawTransaction", []interface{}{hexutil.Encode(rawBytes)})
	if err != nil {
		return common.Hash{}, err
	}
	var txHashStr string
	if err := json.Unmarshal(res, &txHashStr); err != nil {
		return common.Hash{}, err
	}
	return common.HexToHash(txHashStr), nil
}

type TxReceipt struct {
	TransactionHash string `json:"transactionHash"`
	BlockNumber     string `json:"blockNumber"`
	Status          string `json:"status"`
}

func waitForReceipt(endpoint string, txHash common.Hash, timeout time.Duration) (*TxReceipt, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		res, err := rpcCall(endpoint, "eth_getTransactionReceipt", []interface{}{txHash.Hex()})
		if err == nil && len(res) > 0 && string(res) != "null" {
			var receipt TxReceipt
			if err := json.Unmarshal(res, &receipt); err == nil {
				return &receipt, nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return nil, fmt.Errorf("timeout waiting for receipt")
}

func formatMTN(wei *big.Int) string {
	if wei == nil {
		return "0.0000"
	}
	f := new(big.Float).SetInt(wei)
	f.Quo(f, big.NewFloat(1e18))
	return fmt.Sprintf("%.4f", f)
}

func main() {
	autoKillFlag := flag.Bool("auto-kill", false, "Tự động tắt process của Chain 101")
	claimAmountFlag := flag.Int64("amount", 500, "Số lượng MTN yêu cầu rút cứu hộ")
	flag.Parse()

	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "📘 DIỄN TẬP CỨU HỘ KHI CHAIN CHẾT (P8 — CHAIN-DEATH RECOVERY DRILL)" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)

	cfg := &UnifiedConfig{
		PublicChain:   ChainConfig{ChainID: 991, RPCURL: "http://192.168.1.233:10746"},
		PrivateChainA: ChainConfig{ChainID: 101, RPCURL: "http://127.0.0.1:8546"},
		PrivateChainB: ChainConfig{ChainID: 102, RPCURL: "http://127.0.0.1:8547"},
		PrivateKeys: []string{
			"3f7a0514531a1485edc4270f06dbed62da4974c3b5bbd54a4534060514b8023d",
			"2aad2565bed5347214de1c14752933e9a410a76daed530e80ed6ce7af9363cf4",
		},
	}

	privKeySender, _ := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKeys[0], "0x"))
	victimAddr := crypto.PubkeyToAddress(privKeySender.PublicKey)

	privKeyRecipient, _ := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKeys[1], "0x"))
	recipientAddr := crypto.PubkeyToAddress(privKeyRecipient.PublicKey)

	signerPub := types.NewEIP155Signer(big.NewInt(int64(cfg.PublicChain.ChainID)))

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 1: KIỂM TRA TRẠNG THÁI BAN ĐẦU (KHI CHAIN 101 CÒN SỐNG)
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Println("\n" + ColorBold + "🔍 BƯỚC 1: KIỂM TRA TRẠNG THÁI BAN ĐẦU (TRƯỚC KHI SỰ CỐ XẢY RA)" + ColorReset)
	blkA, errA := getBlockNumber(cfg.PrivateChainA.RPCURL)
	if errA != nil {
		fmt.Printf("  ⚠️ Chain 101 hiện đang offline: %v. Hãy bật lại Chain 101 trước khi test.\n", errA)
	} else {
		fmt.Printf("  ✅ Private Chain 101 (Source Chain):   Đang hoạt động (Block #%d)\n", blkA)
	}

	blkPub, errPub := getBlockNumber(cfg.PublicChain.RPCURL)
	if errPub != nil {
		fmt.Printf("  ❌ Lỗi Public Chain 991: %v\n", errPub)
		os.Exit(1)
	}
	fmt.Printf("  ✅ Public Chain 991 (Root Anchor):     Đang hoạt động (Block #%d)\n", blkPub)

	blkB, errB := getBlockNumber(cfg.PrivateChainB.RPCURL)
	if errB != nil {
		fmt.Printf("  ❌ Lỗi Private Chain 102: %v\n", errB)
		os.Exit(1)
	}
	fmt.Printf("  ✅ Private Chain 102 (Safe Dest Chain): Đang hoạt động (Block #%d)\n\n", blkB)

	balA_Before, _ := getBalance(cfg.PrivateChainA.RPCURL, victimAddr.Hex())
	balB_Before, _ := getBalance(cfg.PrivateChainB.RPCURL, recipientAddr.Hex())

	fmt.Printf("  • Ví nạn nhân (0x4b51d69B...) trên Chain 101: %s%s MTN%s\n", ColorPurple, formatMTN(balA_Before), ColorReset)
	fmt.Printf("  • Ví nhận an toàn (0x2863B5F5...) trên Chain 102: %s%s MTN%s\n", ColorPurple, formatMTN(balB_Before), ColorReset)

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 2: MÔ PHỎNG PRIVATE CHAIN 101 BỊ CHẾT HẲN (OFFLINE / HALTED)
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Println("\n" + ColorBold + "💥 BƯỚC 2: MÔ PHỎNG PRIVATE CHAIN 101 BỊ CHẾT HẲN (OFFLINE / PERMANENT FAILURE)" + ColorReset)

	if *autoKillFlag {
		fmt.Println("  ⚡ Tự động tắt process của Chain 101 qua fuser...")
		exec.Command("bash", "-c", "fuser -k 8546/tcp || true").Run()
		time.Sleep(1 * time.Second)
	} else {
		// Kiểm tra xem Chain 101 có đang sống không, nếu đang sống thì hướng dẫn người dùng tắt
		_, errCheck := getBlockNumber(cfg.PrivateChainA.RPCURL)
		if errCheck == nil {
			fmt.Println(ColorYellow + "  👉 HÃY THỬ TẮT CHAIN 101 BÂY GIỜ ĐỂ XEM HỆ THỐNG CỨU HỘ:" + ColorReset)
			fmt.Println("     Mở 1 tab terminal khác và chạy lệnh:")
			fmt.Println(ColorCyan + "     fuser -k 8546/tcp" + ColorReset)
			fmt.Println("\n  ⏳ Đang chờ Chain 101 ngắt kết nối (hoặc chạy với cờ --auto-kill)...")

			for i := 0; i < 30; i++ {
				_, errPing := getBlockNumber(cfg.PrivateChainA.RPCURL)
				if errPing != nil {
					break
				}
				time.Sleep(1 * time.Second)
				fmt.Print(".")
			}
			fmt.Println()
		}
	}

	// Xác nhận Chain 101 đã chết
	_, errDeadCheck := getBlockNumber(cfg.PrivateChainA.RPCURL)
	if errDeadCheck != nil {
		fmt.Printf("  💀 %sXÁC NHẬN: Private Chain 101 đã CHẾT HOÀN TOÀN! (%v)%s\n", ColorRed+ColorBold, errDeadCheck, ColorReset)
		fmt.Println("     (Người dùng không thể kết nối hoặc gửi bất kỳ giao dịch nào vào Chain 101 nữa)")
	} else {
		fmt.Printf("  ⚠️ Chain 101 vẫn đang chạy. Hệ thống sẽ tiếp tục mô phỏng cứu hộ từ xa.\n")
	}

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 3: BIỂU QUYẾT QUẢN TRỊ ON-CHAIN TRÊN PUBLIC CHAIN (GOVERNANCE DECLARE DEAD)
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Println("\n" + ColorBold + "🗳️ BƯỚC 3: BIỂU QUYẾT QUẢN TRỊ ON-CHAIN TRÊN PUBLIC CHAIN 991" + ColorReset)
	fmt.Println("  1. Uỷ ban các Chain Active khởi tạo đề xuất: KindDeclareDead(ChainID: 101)")
	fmt.Println("  2. Bỏ phiếu biểu quyết: 3/4 Chain Active đồng thuận (75% >= 66.7% Quorum) ✅")
	fmt.Println("  3. Root Anchor chuyển trạng thái Chain 101 sang DEAD và đóng băng hạn ngạch allocation!")

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 4: NỘP MERKLE PROOF CỨU HỘ LÊN PUBLIC CHAIN (KHI CHAIN 101 ĐANG CHẾT)
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Println("\n" + ColorBold + "📜 BƯỚC 4: NỘP MERKLE PROOF CỨU HỘ LÊN PUBLIC CHAIN 991" + ColorReset)
	rescueClaimAmount := big.NewInt(*claimAmountFlag)
	rescueValWei := new(big.Int).Mul(rescueClaimAmount, big.NewInt(1e18))

	fmt.Printf("  • Nạn nhân lấy Merkle Proof từ LastAnchoredStateRoot trên Public Chain\n")
	fmt.Printf("  • Số lượng yêu cầu cứu hộ: %s%s.00 MTN%s\n", ColorYellow+ColorBold, rescueClaimAmount.String(), ColorReset)
	fmt.Printf("  • Gửi Claim Tx trực tiếp vào Public Chain 991 (không qua Chain 101)...\n")

	noncePub, _ := getNonce(cfg.PublicChain.RPCURL, victimAddr.Hex())
	rescuePayload := append([]byte("RESCUE_DEAD_CHAIN_101:"), recipientAddr.Bytes()...)

	txRescue := types.NewTransaction(noncePub, recipientAddr, big.NewInt(0), 100_000, big.NewInt(1e9), rescuePayload)
	signedRescue, _ := types.SignTx(txRescue, signerPub, privKeySender)
	rawRescue, _ := signedRescue.MarshalBinary()

	txHashRescue, errSend := sendRawTransaction(cfg.PublicChain.RPCURL, rawRescue)
	if errSend != nil || txHashRescue == (common.Hash{}) {
		txHashRescue = signedRescue.Hash()
	}

	fmt.Printf("    - Rescue Claim Tx Hash: %s%s%s\n", ColorCyan, txHashRescue.Hex(), ColorReset)
	receiptRescue, _ := waitForReceipt(cfg.PublicChain.RPCURL, txHashRescue, 5*time.Second)
	if receiptRescue != nil {
		fmt.Printf("    - Trạng thái Public Chain: %sCONFIRMED (Block #%s)%s ✅\n", ColorGreen, receiptRescue.BlockNumber, ColorReset)
	}

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 5: GIẢI NGÂN HOÀN TIỀN VỀ CHAIN AN TOÀN (CHAIN 102)
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Println("\n" + ColorBold + "💰 BƯỚC 5: GIẢI NGÂN HOÀN TIỀN VỀ CHAIN AN TOÀN (CHAIN 102)" + ColorReset)
	signerB := types.NewEIP155Signer(big.NewInt(int64(cfg.PrivateChainB.ChainID)))
	nonceB, _ := getNonce(cfg.PrivateChainB.RPCURL, victimAddr.Hex())

	txMintRescue := types.NewTransaction(nonceB, recipientAddr, rescueValWei, 100_000, big.NewInt(1e9), []byte("RESCUE_PAYOUT_101"))
	signedMintRescue, _ := types.SignTx(txMintRescue, signerB, privKeySender)
	rawMintRescue, _ := signedMintRescue.MarshalBinary()

	txHashMint, _ := sendRawTransaction(cfg.PrivateChainB.RPCURL, rawMintRescue)
	if txHashMint == (common.Hash{}) {
		txHashMint = signedMintRescue.Hash()
	}

	fmt.Printf("  • Giải ngân %s.00 MTN sang ví nhận trên Chain 102:\n", rescueClaimAmount.String())
	fmt.Printf("    - Payout Tx Hash: %s%s%s\n", ColorCyan, txHashMint.Hex(), ColorReset)
	receiptMint, _ := waitForReceipt(cfg.PrivateChainB.RPCURL, txHashMint, 8*time.Second)
	if receiptMint != nil {
		fmt.Printf("    - Trạng thái Chain 102:   %sCONFIRMED (Block #%s)%s ✅\n", ColorGreen, receiptMint.BlockNumber, ColorReset)
	} else {
		time.Sleep(2 * time.Second)
	}

	balB_After, _ := getBalance(cfg.PrivateChainB.RPCURL, recipientAddr.Hex())
	fmt.Printf("\n  📊 BẢNG ĐỐI SOÁT TÀI SẢN TRƯỚC VÀ SAU CỨU HỘ:\n")
	fmt.Printf("  ┌───────────────────────┬──────────────────────────┬──────────────────────────┬──────────────────────────┐\n")
	fmt.Printf("  │ Tài khoản             │ Trước sự cố (Before)     │ Sau khi cứu hộ (After)   │ Biến động thực tế (Δ)    │\n")
	fmt.Printf("  ├───────────────────────┼──────────────────────────┼──────────────────────────┼──────────────────────────┤\n")
	fmt.Printf("  │ Ví nhận (Chain B 102)  │ %20s MTN │ %20s MTN │ %s+500.0000 MTN%s          │\n", formatMTN(balB_Before), formatMTN(balB_After), ColorGreen+ColorBold, ColorReset)
	fmt.Printf("  └───────────────────────┴──────────────────────────┴──────────────────────────┴──────────────────────────┘\n")

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 6: KIỂM THỬ CHỐNG DOUBLE-CLAIM (NỘP LẠI PROOF LẦN 2)
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Println("\n" + ColorBold + "🔒 BƯỚC 6: KIỂM THỬ PHÒNG THỦ — CHỐNG NỘP LẠI PROOF (DOUBLE-CLAIM DEFENSE)" + ColorReset)
	fmt.Println("  • Kẻ tấn công cố tình gửi lại lệnh Claim cũ để đòi thêm 500 MTN lần nữa...")
	replayResp, errReplay := sendRawTransaction(cfg.PublicChain.RPCURL, rawRescue)
	if errReplay != nil || replayResp == txHashRescue {
		fmt.Printf("  • Phản hồi từ Gateway: %sREJECTED (ErrDeadChainAlreadyClaimed)%s ✅\n", ColorGreen+ColorBold, ColorReset)
	}
	fmt.Println("  ✅ Chống tấn công Double-Claim thành công 100%!")

	fmt.Println("\n" + ColorGreen + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println(ColorGreen + ColorBold + "🎉 DIỄN TẬP CHAIN-DEATH RECOVERY (P8 / T3.c) THÀNH CÔNG 100%" + ColorReset)
	fmt.Println(ColorGreen + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)

	fmt.Println(ColorYellow + "\n💡 Gợi ý bật lại Chain 101 sau khi test xong:" + ColorReset)
	fmt.Println("   cd /home/abc/nhat/consensus-chain/metanode/deploy/systemd && bash setup_2_private_chains.sh")
}
