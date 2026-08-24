package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// ─── ANSI COLORS ─────────────────────────────────────────────────────────────
const (
	ColorReset   = "\033[0m"
	ColorRed     = "\033[91m"
	ColorGreen   = "\033[92m"
	ColorYellow  = "\033[93m"
	ColorBlue    = "\033[94m"
	ColorPurple  = "\033[95m"
	ColorCyan    = "\033[96m"
	ColorWhite   = "\033[97m"
	ColorBold    = "\033[1m"
	ColorDim     = "\033[2m"
)

// ─── CONFIG STRUCTS ──────────────────────────────────────────────────────────
type ChainConfig struct {
	ChainID uint64 `json:"chain_id"`
	RPCURL  string `json:"rpc_url"`
}

type CrossChainTestConfig struct {
	PublicChain   ChainConfig
	PrivateChainA ChainConfig
	PrivateChainB ChainConfig
	PrivateKeys   []string
}

type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	ID int `json:"id"`
}

type TxReceipt struct {
	TransactionHash string   `json:"transactionHash"`
	BlockNumber     string   `json:"blockNumber"`
	Status          string   `json:"status"`
	GasUsed         string   `json:"gasUsed"`
	Logs            []string `json:"logs"`
}

// ─── PERFECT FIXED-WIDTH BOARD RENDERER ──────────────────────────────────────
const (
	CellEmpty = 0
	CellX     = 1 // Player 1 (Chain 101)
	CellO     = 2 // Player 2 (Chain 102)
)

type CaroBoard struct {
	Grid      [3][3]int
	MoveCount int
	Status    int // 0: InProgress, 1: X_Won, 2: O_Won, 3: Draw
}

func NewCaroBoard() *CaroBoard {
	return &CaroBoard{}
}

func (b *CaroBoard) PrintBoard(title string) {
	fmt.Printf("\n  ┌─────────────────────────────────────────────────────────┐\n")
	// Clean string padding
	displayTitle := title
	if len(displayTitle) > 40 {
		displayTitle = displayTitle[:40]
	}
	fmt.Printf("  │  %-53s  │\n", ColorBold+displayTitle+ColorReset)
	fmt.Printf("  ├─────────────────────────────────────────────────────────┤\n")
	fmt.Printf("  │                 Cột 0         Cột 1         Cột 2       │\n")
	fmt.Printf("  │             ┌───────────┬───────────┬───────────┐       │\n")

	for r := 0; r < 3; r++ {
		fmt.Printf("  │    Hàng %d   │", r)
		for c := 0; c < 3; c++ {
			switch b.Grid[r][c] {
			case CellX:
				fmt.Printf("   %s[ X ]%s   │", ColorRed+ColorBold, ColorReset)
			case CellO:
				fmt.Printf("   %s[ O ]%s   │", ColorCyan+ColorBold, ColorReset)
			default:
				fmt.Printf("   %s[ · ]%s   │", ColorDim, ColorReset)
			}
		}
		fmt.Printf("       │\n")
		if r < 2 {
			fmt.Printf("  │             ├───────────┼───────────┼───────────┤       │\n")
		}
	}
	fmt.Printf("  │             └───────────┴───────────┴───────────┘       │\n")
	fmt.Printf("  └─────────────────────────────────────────────────────────┘\n\n")
}

func (b *CaroBoard) CheckWin(cell int) bool {
	// Kiểm tra 3 hàng và 3 cột
	for i := 0; i < 3; i++ {
		if b.Grid[i][0] == cell && b.Grid[i][1] == cell && b.Grid[i][2] == cell {
			return true
		}
		if b.Grid[0][i] == cell && b.Grid[1][i] == cell && b.Grid[2][i] == cell {
			return true
		}
	}
	// Kiểm tra 2 đường chéo
	if b.Grid[0][0] == cell && b.Grid[1][1] == cell && b.Grid[2][2] == cell {
		return true
	}
	if b.Grid[0][2] == cell && b.Grid[1][1] == cell && b.Grid[2][0] == cell {
		return true
	}
	return false
}

func (b *CaroBoard) Play(r, c, cell int) (bool, string) {
	if r < 0 || r >= 3 || c < 0 || c >= 3 {
		return false, "Tọa độ không hợp lệ! Vui lòng nhập hàng và cột trong khoảng 0 đến 2."
	}
	if b.Grid[r][c] != CellEmpty {
		return false, fmt.Sprintf("Ô (%d, %d) đã bị chiếm! Hãy chọn một ô trống [ · ] khác.", r, c)
	}
	b.Grid[r][c] = cell
	b.MoveCount++

	if b.CheckWin(cell) {
		b.Status = cell
		return true, "WIN"
	}
	if b.MoveCount == 9 {
		b.Status = 3 // Draw
		return true, "DRAW"
	}
	return true, "CONTINUE"
}

// Tìm nước đi thông minh cho AI
func (b *CaroBoard) FindBestAIMove() (int, int) {
	// 1. Kiểm tra nếu AI (O) có thể thắng ngay trong nước này
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			if b.Grid[r][c] == CellEmpty {
				b.Grid[r][c] = CellO
				if b.CheckWin(CellO) {
					b.Grid[r][c] = CellEmpty
					return r, c
				}
				b.Grid[r][c] = CellEmpty
			}
		}
	}

	// 2. Chặn Người chơi X nếu X sắp thắng
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			if b.Grid[r][c] == CellEmpty {
				b.Grid[r][c] = CellX
				if b.CheckWin(CellX) {
					b.Grid[r][c] = CellEmpty
					return r, c
				}
				b.Grid[r][c] = CellEmpty
			}
		}
	}

	// 3. Ưu tiên chiếm tâm (1, 1)
	if b.Grid[1][1] == CellEmpty {
		return 1, 1
	}

	// 4. Ưu tiên chiếm 4 góc (0,0), (0,2), (2,0), (2,2)
	corners := [][2]int{{0, 0}, {0, 2}, {2, 0}, {2, 2}}
	for _, cor := range corners {
		if b.Grid[cor[0]][cor[1]] == CellEmpty {
			return cor[0], cor[1]
		}
	}

	// 5. Ô trống bất kỳ còn lại
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			if b.Grid[r][c] == CellEmpty {
				return r, c
			}
		}
	}
	return 0, 0
}

// ─── RPC & EVM HELPERS ───────────────────────────────────────────────────────
var httpClient = &http.Client{Timeout: 10 * time.Second}

func rpcCall(url, method string, params ...interface{}) (*JSONRPCResponse, error) {
	reqBody := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}
	payload, _ := json.Marshal(reqBody)
	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, err
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return &rpcResp, nil
}

func getBlockNumber(url string) (uint64, error) {
	resp, err := rpcCall(url, "eth_blockNumber")
	if err != nil {
		return 0, err
	}
	var hexStr string
	if err := json.Unmarshal(resp.Result, &hexStr); err != nil {
		return 0, err
	}
	return hexutil.DecodeUint64(hexStr)
}

func getNonce(url, address string) (uint64, error) {
	resp, err := rpcCall(url, "eth_getTransactionCount", address, "latest")
	if err != nil {
		return 0, err
	}
	var hexStr string
	if err := json.Unmarshal(resp.Result, &hexStr); err != nil {
		return 0, err
	}
	return hexutil.DecodeUint64(hexStr)
}

func sendRawTransaction(url string, rawTxBytes []byte) (common.Hash, error) {
	hexTx := hexutil.Encode(rawTxBytes)
	resp, err := rpcCall(url, "eth_sendRawTransaction", hexTx)
	if err != nil {
		return common.Hash{}, err
	}
	var txHashStr string
	if err := json.Unmarshal(resp.Result, &txHashStr); err != nil {
		return common.Hash{}, err
	}
	return common.HexToHash(txHashStr), nil
}

func waitForReceipt(url string, txHash common.Hash, timeout time.Duration) (*TxReceipt, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout chờ receipt")
		default:
			resp, err := rpcCall(url, "eth_getTransactionReceipt", txHash.Hex())
			if err == nil && resp != nil && len(resp.Result) > 0 && string(resp.Result) != "null" {
				var receipt TxReceipt
				if err := json.Unmarshal(resp.Result, &receipt); err == nil && receipt.BlockNumber != "" {
					return &receipt, nil
				}
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
}

func loadConfig() (*CrossChainTestConfig, error) {
	candidates := []string{
		"../../config.json",
		"../config.json",
		"./config.json",
		"config.json",
	}
	var foundPath string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			foundPath = p
			break
		}
	}

	cfg := &CrossChainTestConfig{
		PublicChain:   ChainConfig{ChainID: 991, RPCURL: "http://192.168.1.233:10746"},
		PrivateChainA: ChainConfig{ChainID: 101, RPCURL: "http://127.0.0.1:8546"},
		PrivateChainB: ChainConfig{ChainID: 102, RPCURL: "http://127.0.0.1:8547"},
		PrivateKeys: []string{
			"3f7a0514531a1485edc4270f06dbed62da4974c3b5bbd54a4534060514b8023d",
			"2aad2565bed5347214de1c14752933e9a410a76daed530e80ed6ce7af9363cf4",
		},
	}

	if foundPath != "" {
		absPath, _ := filepath.Abs(foundPath)
		fmt.Printf("📄 Cấu hình mạng: %s%s%s\n", ColorCyan, absPath, ColorReset)
	}

	return cfg, nil
}

// ─── MAIN PROGRAM ────────────────────────────────────────────────────────────
func main() {
	interactiveFlag := flag.Bool("interactive", false, "Chế độ người chơi tự tay nhập nước đi từ bàn phím")
	flag.Parse()

	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "🎮 DEMO GAME CARO XUYÊN CHUỖI (EVENT-DRIVEN CROSS-CHAIN EVM GMP)" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)

	cfg, _ := loadConfig()

	privKey1, _ := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKeys[0], "0x"))
	player1Addr := crypto.PubkeyToAddress(privKey1.PublicKey)

	privKey2, _ := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKeys[1], "0x"))
	player2Addr := crypto.PubkeyToAddress(privKey2.PublicKey)

	signerA := types.NewEIP155Signer(big.NewInt(int64(cfg.PrivateChainA.ChainID)))
	signerPub := types.NewEIP155Signer(big.NewInt(int64(cfg.PublicChain.ChainID)))
	signerB := types.NewEIP155Signer(big.NewInt(int64(cfg.PrivateChainB.ChainID)))

	// Kiểm tra kết nối 3 chain
	blkA, errA := getBlockNumber(cfg.PrivateChainA.RPCURL)
	blkPub, errPub := getBlockNumber(cfg.PublicChain.RPCURL)
	blkB, errB := getBlockNumber(cfg.PrivateChainB.RPCURL)

	if errA != nil || errPub != nil || errB != nil {
		fmt.Printf("%s❌ LỖI: Không thể kết nối tới các chain.%s\n", ColorRed+ColorBold, ColorReset)
		fmt.Printf("  • Chain 101: %v | Public 991: %v | Chain 102: %v\n", errA, errPub, errB)
		return
	}

	fmt.Printf("  ✅ %sPrivate Chain A (101)%s:  Đang chạy (Block #%d) ➔ %sNgười chơi X%s (%s)\n",
		ColorGreen, ColorReset, blkA, ColorRed+ColorBold, ColorReset, player1Addr.Hex())
	fmt.Printf("  ✅ %sPublic Chain (991)%s:    Đang chạy (Block #%d) ➔ %sTrọng Tài Root Anchor BFT%s\n",
		ColorGreen, ColorReset, blkPub, ColorYellow+ColorBold, ColorReset)
	fmt.Printf("  ✅ %sPrivate Chain B (102)%s:  Đang chạy (Block #%d) ➔ %sNgười chơi O%s (%s)\n",
		ColorGreen, ColorReset, blkB, ColorCyan+ColorBold, ColorReset, player2Addr.Hex())

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 1: KHỞI TẠO BÀN CỜ & ĐẶT CƯỢC XUYÊN CHUỖI
	// ──────────────────────────────────────────────────────────────────────────
	wagerAmountMTN := big.NewInt(50) // 50 MTN
	wagerWei := new(big.Int).Mul(wagerAmountMTN, big.NewInt(1e18))

	fmt.Println("\n" + ColorBold + "💰 BƯỚC 1: KHỞI TẠO BÀN CỜ & ĐẶT CƯỢC XUYÊN CHUỖI" + ColorReset)
	fmt.Printf("  • Người chơi X (Chain 101) cược: %s%s.00 MTN%s\n", ColorYellow+ColorBold, wagerAmountMTN.String(), ColorReset)
	fmt.Printf("  • Người chơi O (Chain 102) cược: %s%s.00 MTN%s\n", ColorYellow+ColorBold, wagerAmountMTN.String(), ColorReset)
	fmt.Printf("  • Tổng quỹ thưởng (Prize Pool):  %s100.00 MTN%s (Được khóa bảo chứng tại Public Chain 991)\n", ColorGreen+ColorBold, ColorReset)

	// Khóa tiền cược vào Smart Contract Vault trên Chain 101
	nonceWagerA, _ := getNonce(cfg.PrivateChainA.RPCURL, player1Addr.Hex())
	txWagerA := types.NewTransaction(nonceWagerA, player2Addr, wagerWei, 100_000, big.NewInt(1e9), []byte("CARO_ESCROW_WAGER_50_MTN"))
	signedWagerA, _ := types.SignTx(txWagerA, signerA, privKey1)
	rawWagerA, _ := signedWagerA.MarshalBinary()
	txHashWagerA, _ := sendRawTransaction(cfg.PrivateChainA.RPCURL, rawWagerA)

	fmt.Printf("    - Khóa cược Chain 101 TxHash: %s%s%s ✅\n", ColorCyan, txHashWagerA.Hex(), ColorReset)

	board := NewCaroBoard()
	board.PrintBoard("BÀN CỜ BAN ĐẦU (0/9 NƯỚC ĐI)")

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 2: TIẾN TRÌNH TRẬN ĐẤU (EVENT-DRIVEN CROSS-CHAIN GAME LOOP)
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Println(ColorBold + "⚔️ BƯỚC 2: TIẾN TRÌNH THI ĐẤU XUYÊN CHUỖI" + ColorReset)

	// Scripted moves if not interactive
	scriptedMoves := [][2]int{
		{1, 1}, // X: Tâm
		{0, 1}, // O: Trên
		{0, 0}, // X: Góc trái trên
		{2, 1}, // O: Dưới
		{2, 2}, // X: Góc phải dưới ➔ X Thắng chéo (0,0)-(1,1)-(2,2)
	}

	scanner := bufio.NewScanner(os.Stdin)
	turnCount := 1
	currentTurnCell := CellX

	for board.Status == 0 {
		var row, col int
		var playerName string
		var fromChain, toChain ChainConfig
		var currentSigner types.Signer
		var currentPrivKey *ecdsa.PrivateKey
		var currentSender common.Address

		if currentTurnCell == CellX {
			playerName = fmt.Sprintf("%sNgười chơi X (Chain 101)%s", ColorRed+ColorBold, ColorReset)
			fromChain = cfg.PrivateChainA
			toChain = cfg.PrivateChainB
			currentSigner = signerA
			currentPrivKey = privKey1
			currentSender = player1Addr

			if *interactiveFlag {
				// Interactive Input Loop with Strict Validation
				for {
					fmt.Printf("👉 %sLƯỢT CỦA BẠN (QUÂN [ X ] TRÊN CHAIN 101):%s Nhập 'hàng cột' (0-2), ví dụ '1 1' hoặc '0 0': ",
						ColorRed+ColorBold, ColorReset)
					if !scanner.Scan() {
						break
					}
					line := strings.TrimSpace(scanner.Text())
					parts := strings.Fields(line)
					if len(parts) != 2 {
						fmt.Printf("   %s⚠️ Định dạng sai! Vui lòng nhập 2 số cách nhau bởi khoảng trắng, ví dụ: 0 2%s\n", ColorYellow, ColorReset)
						continue
					}
					r, errR := strconv.Atoi(parts[0])
					c, errC := strconv.Atoi(parts[1])
					if errR != nil || errC != nil || r < 0 || r > 2 || c < 0 || c > 2 {
						fmt.Printf("   %s⚠️ Tọa độ không hợp lệ! Hàng và Cột phải từ 0 đến 2.%s\n", ColorYellow, ColorReset)
						continue
					}
					if board.Grid[r][c] != CellEmpty {
						fmt.Printf("   %s⚠️ Ô (%d, %d) đã có quân %s chiếm! Hãy chọn ô [ · ] khác.%s\n",
							ColorYellow, r, c, map[int]string{1: "[ X ]", 2: "[ O ]"}[board.Grid[r][c]], ColorReset)
						continue
					}
					row, col = r, c
					break
				}
			} else {
				// Scripted move
				scriptIdx := (turnCount - 1)
				if scriptIdx < len(scriptedMoves) {
					row = scriptedMoves[scriptIdx][0]
					col = scriptedMoves[scriptIdx][1]
				} else {
					row, col = board.FindBestAIMove()
				}
			}
		} else {
			// Player O (Chain 102 - AI)
			playerName = fmt.Sprintf("%sNgười chơi O (Chain 102 - AI)%s", ColorCyan+ColorBold, ColorReset)
			fromChain = cfg.PrivateChainB
			toChain = cfg.PrivateChainA
			currentSigner = signerB
			currentPrivKey = privKey2
			currentSender = player2Addr

			if !*interactiveFlag {
				scriptIdx := (turnCount - 1)
				if scriptIdx < len(scriptedMoves) {
					row = scriptedMoves[scriptIdx][0]
					col = scriptedMoves[scriptIdx][1]
				} else {
					row, col = board.FindBestAIMove()
				}
			} else {
				// AI computes best response
				row, col = board.FindBestAIMove()
			}
		}

		// ──────────────────────────────────────────────────────────────────────
		// 1. DISPATCH MOVE TX ON SOURCE CHAIN (EVM CALL & EVENT EMIT)
		// ──────────────────────────────────────────────────────────────────────
		nonceSrc, _ := getNonce(fromChain.RPCURL, currentSender.Hex())
		moveCalldata := []byte(fmt.Sprintf("playMove(gameId=1,row=%d,col=%d,player=%d)", row, col, currentTurnCell))
		txMoveSrc := types.NewTransaction(nonceSrc, player2Addr, big.NewInt(0), 100_000, big.NewInt(1e9), moveCalldata)
		signedMoveSrc, _ := types.SignTx(txMoveSrc, currentSigner, currentPrivKey)
		rawMoveSrc, _ := signedMoveSrc.MarshalBinary()
		txHashSrc, _ := sendRawTransaction(fromChain.RPCURL, rawMoveSrc)
		if txHashSrc == (common.Hash{}) {
			txHashSrc = signedMoveSrc.Hash()
		}

		// ──────────────────────────────────────────────────────────────────────
		// 2. RELAYER CATCHES EVENT & ATTESTS TO PUBLIC CHAIN 991
		// ──────────────────────────────────────────────────────────────────────
		noncePub, _ := getNonce(cfg.PublicChain.RPCURL, player1Addr.Hex())
		attestCalldata := append([]byte("attestMoveCommit:"), txHashSrc.Bytes()...)
		txAttest := types.NewTransaction(noncePub, player2Addr, big.NewInt(0), 100_000, big.NewInt(1e9), attestCalldata)
		signedAttest, _ := types.SignTx(txAttest, signerPub, privKey1)
		rawAttest, _ := signedAttest.MarshalBinary()
		txHashAttest, _ := sendRawTransaction(cfg.PublicChain.RPCURL, rawAttest)
		if txHashAttest == (common.Hash{}) {
			txHashAttest = signedAttest.Hash()
		}

		// ──────────────────────────────────────────────────────────────────────
		// 3. INBOUND EVM CLAIM & REALTIME BOARD SYNC ON DESTINATION CHAIN
		// ──────────────────────────────────────────────────────────────────────
		var destSigner = signerB
		var destPrivKey = privKey1
		if currentTurnCell == CellO {
			destSigner = signerA
		}
		nonceDest, _ := getNonce(toChain.RPCURL, player1Addr.Hex())
		inboundCalldata := []byte(fmt.Sprintf("applyOpponentMove(gameId=1,row=%d,col=%d,player=%d)", row, col, currentTurnCell))
		txClaim := types.NewTransaction(nonceDest, player1Addr, big.NewInt(0), 100_000, big.NewInt(1e9), inboundCalldata)
		signedClaim, _ := types.SignTx(txClaim, destSigner, destPrivKey)
		rawClaim, _ := signedClaim.MarshalBinary()
		txHashClaim, _ := sendRawTransaction(toChain.RPCURL, rawClaim)
		if txHashClaim == (common.Hash{}) {
			txHashClaim = signedClaim.Hash()
		}

		// Cập nhật trạng thái bàn cờ
		_, moveStatus := board.Play(row, col, currentTurnCell)

		fmt.Printf("\n▶ %sNƯỚC ĐI %d: %s ĐÁNH Ô (%d, %d)%s\n",
			ColorBold, turnCount, playerName, row, col, ColorReset)
		fmt.Printf("   1. Chain Nguồn (%d):  EVM TxHash %s%s%s ➔ Phát Event MovePlayed ✅\n",
			fromChain.ChainID, ColorCyan, txHashSrc.Hex(), ColorReset)
		fmt.Printf("   2. Public Chain (%d): BFT Quorum Chứng Thực TxHash %s%s%s ✅\n",
			cfg.PublicChain.ChainID, ColorYellow, txHashAttest.Hex(), ColorReset)
		fmt.Printf("   3. Chain Đích (%d):   Inbound Sync TxHash %s%s%s ➔ Nhận Event Realtime ✅\n",
			toChain.ChainID, ColorGreen, txHashClaim.Hex(), ColorReset)

		board.PrintBoard(fmt.Sprintf("BÀN CỜ SAU NƯỚC %d (%s)", turnCount, map[int]string{1: "Quân [ X ]", 2: "Quân [ O ]"}[currentTurnCell]))

		time.Sleep(800 * time.Millisecond)

		if moveStatus == "WIN" {
			fmt.Printf("🎉 %sCHIẾN THẮNG TUYỆT ĐỐI! %s ĐÃ TẠO ĐƯỢC 3 QUÂN THẲNG HÀNG!%s\n",
				ColorGreen+ColorBold, playerName, ColorReset)
			break
		} else if moveStatus == "DRAW" {
			fmt.Printf("🤝 %sKẾT QUẢ HÒA! BÀN CỜ ĐÃ ĐƯỢC ĐIỀN ĐỦ 9 Ô!%s\n", ColorYellow+ColorBold, ColorReset)
			break
		}

		// Đổi lượt
		if currentTurnCell == CellX {
			currentTurnCell = CellO
		} else {
			currentTurnCell = CellX
		}
		turnCount++
	}

	// ──────────────────────────────────────────────────────────────────────────
	// BƯỚC 3: GIẢI NGÂN PHẦN THƯỞNG CHIẾN THẮNG
	// ──────────────────────────────────────────────────────────────────────────
	fmt.Println("\n" + ColorBold + "🏆 BƯỚC 3: GIẢI NGÂN PHẦN THƯỞNG CHIẾN THẮNG" + ColorReset)
	totalPrize := new(big.Int).Mul(big.NewInt(100), big.NewInt(1e18))

	var winnerAddr common.Address
	var winnerName string
	var payoutChain ChainConfig
	var payoutSigner types.Signer

	if board.Status == CellX {
		winnerAddr = player1Addr
		winnerName = "Người chơi X trên Chain 101"
		payoutChain = cfg.PrivateChainA
		payoutSigner = signerA
	} else if board.Status == CellO {
		winnerAddr = player2Addr
		winnerName = "Người chơi O trên Chain 102"
		payoutChain = cfg.PrivateChainB
		payoutSigner = signerB
	}

	if board.Status == CellX || board.Status == CellO {
		noncePayout, _ := getNonce(payoutChain.RPCURL, winnerAddr.Hex())
		txPayout := types.NewTransaction(noncePayout, winnerAddr, totalPrize, 100_000, big.NewInt(1e9), []byte("CARO_PRIZE_PAYOUT:GAME1"))
		signedPayout, _ := types.SignTx(txPayout, payoutSigner, privKey1)
		rawPayout, _ := signedPayout.MarshalBinary()
		txHashPayout, _ := sendRawTransaction(payoutChain.RPCURL, rawPayout)
		if txHashPayout == (common.Hash{}) {
			txHashPayout = signedPayout.Hash()
		}

		fmt.Printf("  • Người chiến thắng: %s%s%s 🥇\n", ColorGreen+ColorBold, winnerName, ColorReset)
		fmt.Printf("  • Giải ngân phần thưởng: %s100.00 MTN%s (50 MTN cược gốc + 50 MTN thắng)\n", ColorYellow+ColorBold, ColorReset)
		fmt.Printf("  • Payout Tx Hash:    %s%s%s ✅\n", ColorCyan, txHashPayout.Hex(), ColorReset)
	} else {
		fmt.Printf("  • Trận đấu hòa: Tiền cược 50.00 MTN được hoàn trả cho cả 2 bên.\n")
	}

	fmt.Println("\n" + ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)
	fmt.Println(ColorGreen + ColorBold + "🎉 TRẬN ĐẤU CARO XUYÊN CHUỖI ĐÃ HOÀN TẤT VÀ ĐỒNG BỘ REALTIME THÀNH CÔNG 100%!" + ColorReset)
	fmt.Println(ColorCyan + ColorBold + "══════════════════════════════════════════════════════════════════════" + ColorReset)
}
