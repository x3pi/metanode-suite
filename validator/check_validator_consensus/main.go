// Tool kiểm tra validators đã được Rust load vào committee theo từng epoch,
// so sánh với danh sách on-chain từ Go StakeDB.
//
// Usage:
//
//	go run main.go                          # check epoch hiện tại
//	go run main.go -epoch 2                 # check epoch cụ thể
//	go run main.go -addr 0x781E...          # highlight địa chỉ cụ thể
//	go run main.go -watch                   # poll liên tục
//	go run main.go -log /path/to/node.log   # custom rust log path
package main

import (
	"bufio"
	"flag"
	"fmt"
	"math/big"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

// ─── config ───────────────────────────────────────────────────────────────────

const (
	DEFAULT_RPC        = "http://localhost:8545"
	VALIDATOR_CONTRACT = "0x0000000000000000000000000000000000001001"
	CALL_FROM          = "0x0000000000000000000000000000000000000001"
	// Default Rust log path (node_0). Use -log to override.
	DEFAULT_LOG = "../../../metanode/consensus/metanode/logs/node_0/go-master-stdout.log"
)

const validatorsAbiJSON = `[
  {"inputs":[],"name":"getValidatorCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"internalType":"uint256","name":"","type":"uint256"}],"name":"validatorAddresses","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"validators","outputs":[
    {"internalType":"address","name":"owner","type":"address"},
    {"internalType":"string","name":"primaryAddress","type":"string"},
    {"internalType":"string","name":"workerAddress","type":"string"},
    {"internalType":"string","name":"p2pAddress","type":"string"},
    {"internalType":"string","name":"name","type":"string"},
    {"internalType":"string","name":"description","type":"string"},
    {"internalType":"string","name":"website","type":"string"},
    {"internalType":"string","name":"image","type":"string"},
    {"internalType":"uint256","name":"commissionRate","type":"uint256"},
    {"internalType":"uint256","name":"minSelfDelegation","type":"uint256"},
    {"internalType":"uint256","name":"totalStakedAmount","type":"uint256"},
    {"internalType":"uint256","name":"accumulatedRewardsPerShare","type":"uint256"},
    {"internalType":"string","name":"hostname","type":"string"},
    {"internalType":"string","name":"authority_key","type":"string"},
    {"internalType":"string","name":"protocol_key","type":"string"},
    {"internalType":"string","name":"network_key","type":"string"}
  ],"stateMutability":"view","type":"function"}
]`

// ─── types ────────────────────────────────────────────────────────────────────

type GoValidator struct {
	Address  common.Address
	Name     string
	Hostname string
	Stake    *big.Int
}

// RustCommittee holds the validators that Rust actually received from Go for a given epoch.
type RustCommittee struct {
	Epoch         uint64
	BoundaryBlock uint64
	Attempt       int // last attempt number when this was confirmed
	Validators    []RustValidator
}

type RustValidator struct {
	Index        int
	Address      string
	Stake        string
	Name         string
	AuthorityKey string // first 50 chars
}

type EpochBoundary struct {
	Epoch       uint64
	BlockNumber uint64
}

// ─── regex patterns for Rust log parsing ──────────────────────────────────────

var (
	// [RUST←GO] EpochBoundaryData Validator[0]: address=0x..., stake=1000, name=node-2, authority_key=hK+5...
	reValidator = regexp.MustCompile(
		`\[RUST←GO\] EpochBoundaryData Validator\[(\d+)\]: address=(0x[0-9a-fA-F]+), stake=(\S+), name=(\S+), authority_key=([^\s]+)`)

	// UNIFIED TIMESTAMP] Got from Go: epoch=3, timestamp_ms=..., boundary_block=200 (attempt 28)
	reUnifiedTS = regexp.MustCompile(
		`UNIFIED TIMESTAMP.*epoch=(\d+),.*boundary_block=(\d+).*attempt (\d+)`)

	// EpochBoundaryData] Received unified epoch boundary data from Go FFI: epoch=3, ..., validator_count=5
	reBoundaryHeader = regexp.MustCompile(
		`EPOCH BOUNDARY.*epoch=(\d+),.*validator_count=(\d+)`)
)

// ─── main ─────────────────────────────────────────────────────────────────────

func main() {
	rpcURL := flag.String("rpc", DEFAULT_RPC, "RPC endpoint")
	logPath := flag.String("log", DEFAULT_LOG, "Rust node log file (go-master-stdout.log)")
	targetAddr := flag.String("addr", "", "Highlight validator address (0x...)")
	epochFlag := flag.Int64("epoch", -1, "Check specific epoch (-1 = current from log)")
	watchMode := flag.Bool("watch", false, "Poll every 10s")
	stuckFlag := flag.Duration("stuck", 150*time.Second, "Ngưỡng thời gian báo chain stuck (ví dụ: 120s, 150s)")
	flag.Parse()

	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║   VALIDATOR CONSENSUS CHECKER  (Go + Rust verification)  ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")
	fmt.Printf("  RPC:  %s\n", *rpcURL)
	fmt.Printf("  Log:  %s\n\n", *logPath)

	client, err := rpc.Dial(*rpcURL)
	if err != nil {
		fmt.Printf("❌ Không kết nối được RPC: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	parsedABI, _ := abi.JSON(strings.NewReader(validatorsAbiJSON))

	if *watchMode {
		var prevBlock uint64
		var sameBlockCount int
		for i := 1; ; i++ {
			fmt.Printf("\n[%s] ─── lần %d ───────────────────────────────────\n",
				time.Now().UTC().Format("15:04:05"), i)
			curBlock := runCheck(client, parsedABI, *logPath, *targetAddr, *epochFlag, *stuckFlag)
			if curBlock > 0 && curBlock == prevBlock {
				sameBlockCount++
				if sameBlockCount >= 3 {
					fmt.Printf("\n🚨 CHAIN STUCK: Block #%d không đổi trong %d lần poll (~%ds)\n",
						curBlock, sameBlockCount, sameBlockCount*10)
				}
			} else {
				sameBlockCount = 0
			}
			prevBlock = curBlock
			time.Sleep(10 * time.Second)
		}
	}
	runCheck(client, parsedABI, *logPath, *targetAddr, *epochFlag, *stuckFlag)
}

// ─── runCheck ─────────────────────────────────────────────────────────────────

// runCheck performs a full check and returns the latest block number (for stuck detection).
func runCheck(client *rpc.Client, parsedABI abi.ABI, logPath, targetAddr string, epochFlag int64, stuckTime time.Duration) uint64 {
	contractAddr := common.HexToAddress(VALIDATOR_CONTRACT)
	fromAddr := common.HexToAddress(CALL_FROM)

	// 1. Parse Rust log → all committees Rust has seen
	committees, err := parseRustLog(logPath)
	if err != nil {
		fmt.Printf("⚠️  Không đọc được Rust log: %v\n", err)
	}

	// Pick target epoch
	var targetEpoch uint64
	if epochFlag >= 0 {
		targetEpoch = uint64(epochFlag)
	} else if len(committees) > 0 {
		targetEpoch = committees[len(committees)-1].Epoch
	}

	// 2. Scan system txs for EpochBoundary
	var head struct {
		Number hexutil.Big    `json:"number"`
		Time   hexutil.Uint64 `json:"timestamp"`
	}
	_ = client.Call(&head, "eth_getBlockByNumber", "latest", false)
	latestBlock := head.Number.ToInt().Uint64()
	latestBlockTime := time.Unix(int64(head.Time), 0).UTC()
	blockAge := time.Since(latestBlockTime)

	boundaries := scanEpochBoundaries(client, latestBlock, 500)
	currentEpoch := uint64(0)
	if len(boundaries) > 0 {
		currentEpoch = boundaries[len(boundaries)-1].Epoch
	}

	fmt.Printf("📦 Block mới nhất: #%d  |  Epoch on-chain: %d  |  Block age: %s\n",
		latestBlock, currentEpoch, blockAge.Round(time.Second))

	// 3. Print Rust committee summary
	fmt.Println("\n═══════════════════════════════════════════════════════════")
	fmt.Printf("🦀 RUST: Committee đã load (từ Rust log)\n")
	fmt.Println("═══════════════════════════════════════════════════════════")

	if len(committees) == 0 {
		fmt.Println("  ⚠️  Không tìm thấy dữ liệu committee trong Rust log")
	}

	rustLatestEpoch := uint64(0)
	if len(committees) > 0 {
		rustLatestEpoch = committees[len(committees)-1].Epoch
	}

	foundEpochToPrint := false
	for _, c := range committees {
		if c.Epoch != targetEpoch {
			continue
		}
		foundEpochToPrint = true
		fmt.Printf("\n  📋 Epoch %d (boundary_block=#%d, attempt=%d)", c.Epoch, c.BoundaryBlock, c.Attempt)
		if c.Epoch == rustLatestEpoch {
			fmt.Printf("  ◄ MỚI NHẤT")
		}
		fmt.Println()
		for _, v := range c.Validators {
			indicator := ""
			if targetAddr != "" && strings.EqualFold(v.Address, targetAddr) {
				indicator = "  ← TARGET"
			}
			keyPreview := v.AuthorityKey
			if len(keyPreview) > 30 {
				keyPreview = keyPreview[:30] + "..."
			}
			fmt.Printf("    [%d] %-42s  name=%-10s stake=%-9s key=%s%s\n",
				v.Index, v.Address, v.Name, v.Stake, keyPreview, indicator)
		}
	}
	if !foundEpochToPrint && len(committees) > 0 {
		fmt.Printf("\n  ⚠️ Không tìm thấy epoch %d trong Rust log\n", targetEpoch)
	}
	fmt.Println()

	// 4. Get Go StakeDB validators
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println("🟢 GO StakeDB: Validators on-chain (latest block)")
	fmt.Println("═══════════════════════════════════════════════════════════")

	goValidators := getGoValidators(client, parsedABI, contractAddr, fromAddr, "latest")
	fmt.Printf("  Tổng: %d validators\n\n", len(goValidators))
	for i, v := range goValidators {
		highlight := ""
		if targetAddr != "" && strings.EqualFold(v.Address.Hex(), targetAddr) {
			highlight = "  ← TARGET"
		}
		fmt.Printf("  [%d] %-44s  name=%-12s  stake=%s%s\n",
			i, v.Address.Hex(), v.Name, v.Stake.String(), highlight)
	}

	// 5. Cross-verify target epoch
	fmt.Println("\n═══════════════════════════════════════════════════════════")
	fmt.Printf("🔍 VERIFY: Epoch %d — So sánh Rust committee vs Go StakeDB\n", targetEpoch)
	fmt.Println("═══════════════════════════════════════════════════════════")

	var rustCommittee *RustCommittee
	for i := range committees {
		if committees[i].Epoch == targetEpoch {
			rustCommittee = &committees[i]
			break
		}
	}

	if rustCommittee == nil {
		fmt.Printf("  ⚠️  Rust CHƯA load committee cho epoch %d\n", targetEpoch)
		if targetEpoch > currentEpoch {
			fmt.Println("  → Epoch này chưa xảy ra")
		} else {
			fmt.Println("  → Có thể Rust đang chờ boundary data từ Go")
		}
		return latestBlock
	}

	// Build address sets for comparison
	rustAddrs := map[string]RustValidator{}
	for _, v := range rustCommittee.Validators {
		rustAddrs[strings.ToLower(v.Address)] = v
	}
	goAddrs := map[string]GoValidator{}
	for _, v := range goValidators {
		goAddrs[strings.ToLower(v.Address.Hex())] = v
	}

	// Validators in Rust but not Go
	fmt.Printf("\n  Rust committee có %d validators, Go StakeDB có %d validators\n",
		len(rustCommittee.Validators), len(goValidators))

	allGood := true

	fmt.Println("\n  [A] Validators trong Rust committee:")
	for _, v := range rustCommittee.Validators {
		inGo := ""
		key := strings.ToLower(v.Address)
		if _, ok := goAddrs[key]; ok {
			inGo = "✅ có trong Go"
		} else {
			inGo = "❌ KHÔNG có trong Go StakeDB"
			allGood = false
		}
		targetMark := ""
		if targetAddr != "" && strings.EqualFold(v.Address, targetAddr) {
			targetMark = " ← TARGET"
		}
		fmt.Printf("    [%d] %s  %s%s\n", v.Index, v.Address, inGo, targetMark)
	}

	fmt.Println("\n  [B] Validators trong Go StakeDB nhưng KHÔNG có trong Rust committee:")
	missing := []GoValidator{}
	for _, v := range goValidators {
		if _, ok := rustAddrs[strings.ToLower(v.Address.Hex())]; !ok {
			missing = append(missing, v)
		}
	}
	if len(missing) == 0 {
		fmt.Println("    ✅ Không có validator nào bị thiếu")
	} else {
		allGood = false
		for _, v := range missing {
			targetMark := ""
			if targetAddr != "" && strings.EqualFold(v.Address.Hex(), targetAddr) {
				targetMark = " ← TARGET"
			}
			fmt.Printf("    ❌ %s  name=%s  stake=%s%s\n",
				v.Address.Hex(), v.Name, v.Stake.String(), targetMark)
		}
	}

	fmt.Println()
	if allGood && len(rustCommittee.Validators) == len(goValidators) {
		fmt.Println("  ✅ HOÀN TOÀN KHỚP: Rust đã load đúng toàn bộ validator từ Go")
	} else if allGood {
		fmt.Printf("  ⚠️  Rust=%d vs Go=%d validators — Rust có thể dùng stake filter\n",
			len(rustCommittee.Validators), len(goValidators))
	} else {
		fmt.Println("  ❌ KHÔNG KHỚP: Rust thiếu một số validators so với Go StakeDB")
	}

	// 6. Check target address specifically
	if targetAddr != "" {
		fmt.Println("\n═══════════════════════════════════════════════════════════")
		fmt.Printf("🎯 TARGET: %s\n", targetAddr)
		fmt.Println("═══════════════════════════════════════════════════════════")
		inRust := rustAddrs[strings.ToLower(targetAddr)]
		inGo := goAddrs[strings.ToLower(targetAddr)]
		if inRust.Address != "" {
			fmt.Printf("  🦀 Rust epoch %d: ✅ CÓ (stake=%s, name=%s)\n",
				targetEpoch, inRust.Stake, inRust.Name)
		} else {
			fmt.Printf("  🦀 Rust epoch %d: ❌ KHÔNG CÓ trong committee\n", targetEpoch)
		}
		if inGo.Address != (common.Address{}) {
			fmt.Printf("  🟢 Go StakeDB: ✅ CÓ (stake=%s, name=%s)\n",
				inGo.Stake.String(), inGo.Name)
		} else {
			fmt.Printf("  🟢 Go StakeDB: ❌ KHÔNG CÓ\n")
		}
	}

	// ─── 7. HEALTH CHECK SUMMARY ─────────────────────────────────────────────
	fmt.Println("\n═══════════════════════════════════════════════════════════")
	fmt.Println("⚕️  HEALTH CHECK")
	fmt.Println("═══════════════════════════════════════════════════════════")

	issues := []string{}

	// A. Chain stuck?
	if blockAge > stuckTime {
		issues = append(issues, fmt.Sprintf(
			"🚨 CHAIN STUCK: Block #%d không đổi trong %v (block age > %v)",
			latestBlock, blockAge.Round(time.Second), stuckTime))
	}

	// B. Epoch mismatch: Rust log nói epoch N nhưng on-chain boundary chỉ thấy epoch M
	if len(committees) > 0 {
		rustLatestEpoch = committees[len(committees)-1].Epoch
	}
	if rustLatestEpoch > currentEpoch {
		issues = append(issues, fmt.Sprintf(
			"⚠️  EPOCH MISMATCH: Rust đã advance tới epoch %d, nhưng on-chain EpochBoundary system tx chỉ thấy epoch %d → chain chưa produce block mới cho epoch này",
			rustLatestEpoch, currentEpoch))
	}

	// C. Rust committee không khớp Go
	if rustCommittee != nil && len(missing) > 0 {
		issues = append(issues, fmt.Sprintf(
			"❌ COMMITTEE MISMATCH epoch %d: Rust thiếu %d validator(s) so với Go StakeDB",
			targetEpoch, len(missing)))
	}

	// D. Target không có trong Rust committee epoch mới nhất
	if targetAddr != "" && rustLatestEpoch > 0 {
		var latestCommittee *RustCommittee
		for i := range committees {
			if committees[i].Epoch == rustLatestEpoch {
				latestCommittee = &committees[i]
			}
		}
		if latestCommittee != nil {
			found := false
			for _, v := range latestCommittee.Validators {
				if strings.EqualFold(v.Address, targetAddr) {
					found = true
					break
				}
			}
			if !found {
				issues = append(issues, fmt.Sprintf(
					"❌ TARGET %s KHÔNG có trong Rust committee epoch %d (epoch mới nhất Rust đã load)",
					targetAddr, rustLatestEpoch))
			}
		}
	}

	if len(issues) == 0 {
		fmt.Println("  ✅ Không phát hiện vấn đề nào")
	} else {
		for _, issue := range issues {
			fmt.Printf("  %s\n", issue)
		}
		fmt.Printf("\n  📋 Tổng: %d vấn đề cần xử lý\n", len(issues))
	}

	return latestBlock
}

// ─── Rust log parser ──────────────────────────────────────────────────────────

// parseRustLog reads the Rust stdout log and extracts the FINAL confirmed committee
// for each epoch. It groups Validator[N] lines by epoch and deduplicates by keeping
// the last complete group (highest attempt).
func parseRustLog(path string) ([]RustCommittee, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// epoch → committee (overwrite each time we see a complete group)
	type inProgress struct {
		epoch         uint64
		boundaryBlock uint64
		attempt       int
		vals          []RustValidator
	}

	byEpoch := map[uint64]*inProgress{}
	var currentParsingEpoch uint64

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Match UNIFIED TIMESTAMP (gives us epoch + boundary_block + attempt)
		if m := reUnifiedTS.FindStringSubmatch(line); m != nil {
			epoch, _ := strconv.ParseUint(m[1], 10, 64)
			boundary, _ := strconv.ParseUint(m[2], 10, 64)
			attempt, _ := strconv.Atoi(m[3])
			if byEpoch[epoch] == nil {
				byEpoch[epoch] = &inProgress{epoch: epoch}
			}
			byEpoch[epoch].boundaryBlock = boundary
			byEpoch[epoch].attempt = attempt
			continue
		}

		// Match individual validator lines
		if m := reValidator.FindStringSubmatch(line); m != nil {
			idx, _ := strconv.Atoi(m[1])
			addr := m[2]
			stake := m[3]
			name := m[4]
			authKey := m[5]

			targetIP := byEpoch[currentParsingEpoch]
			if targetIP == nil {
				continue
			}

			v := RustValidator{
				Index:        idx,
				Address:      addr,
				Stake:        stake,
				Name:         name,
				AuthorityKey: authKey,
			}
			if idx == 0 {
				// New group starts → reset validators for this epoch
				targetIP.vals = []RustValidator{v}
			} else {
				targetIP.vals = append(targetIP.vals, v)
			}
			continue
		}

		// Match header line to establish current epoch context
		if m := reBoundaryHeader.FindStringSubmatch(line); m != nil {
			epoch, _ := strconv.ParseUint(m[1], 10, 64)
			currentParsingEpoch = epoch
			if byEpoch[epoch] == nil {
				byEpoch[epoch] = &inProgress{epoch: epoch}
			}
		}
	}

	// Convert to sorted slice
	var result []RustCommittee
	for _, ip := range byEpoch {
		if len(ip.vals) == 0 {
			continue
		}
		result = append(result, RustCommittee{
			Epoch:         ip.epoch,
			BoundaryBlock: ip.boundaryBlock,
			Attempt:       ip.attempt,
			Validators:    ip.vals,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Epoch < result[j].Epoch
	})
	return result, scanner.Err()
}

// ─── Go StakeDB query ─────────────────────────────────────────────────────────

func getGoValidators(client *rpc.Client, parsedABI abi.ABI,
	contractAddr, fromAddr common.Address, blockTag string) []GoValidator {

	countData := common.FromHex("0x7071688a")
	res, err := ethCall(client, fromAddr, contractAddr, countData, blockTag)
	if err != nil {
		return nil
	}
	count := new(big.Int).SetBytes(res).Int64()

	var out []GoValidator
	for i := int64(0); i < count; i++ {
		methodId := crypto.Keccak256Hash([]byte("validatorAddresses(uint256)")).Bytes()[:4]
		callData := append(methodId, common.LeftPadBytes(big.NewInt(i).Bytes(), 32)...)
		addrResult, err := ethCall(client, fromAddr, contractAddr, callData, blockTag)
		if err != nil || len(addrResult) < 32 {
			continue
		}
		addr := common.BytesToAddress(addrResult)

		vMethodId := parsedABI.Methods["validators"].ID
		vCallData := append(vMethodId, common.LeftPadBytes(addr.Bytes(), 32)...)
		vResult, err := ethCall(client, fromAddr, contractAddr, vCallData, blockTag)
		if err != nil {
			continue
		}
		unpacked, err := parsedABI.Unpack("validators", vResult)
		if err != nil || len(unpacked) < 11 {
			continue
		}
		out = append(out, GoValidator{
			Address:  addr,
			Name:     unpacked[4].(string),
			Hostname: unpacked[12].(string),
			Stake:    unpacked[10].(*big.Int),
		})
	}
	return out
}

// ─── epoch boundary scanner ───────────────────────────────────────────────────

func scanEpochBoundaries(client *rpc.Client, latestBlock, scanRange uint64) []EpochBoundary {
	checkFrom := uint64(0)
	if latestBlock > scanRange {
		checkFrom = latestBlock - scanRange
	}
	var out = []EpochBoundary{}
	for blockNum := checkFrom; blockNum <= latestBlock; blockNum++ {
		var sysTxs []map[string]interface{}
		if err := client.Call(&sysTxs, "eth_getSystemTransactionsByBlockNumber",
			hexutil.EncodeUint64(blockNum)); err != nil {
			continue
		}
		for _, tx := range sysTxs {
			if kind, _ := tx["type"].(string); kind != "EndOfEpoch" {
				continue
			}
			epoch := uint64(0)
			if e, ok := tx["new_epoch"].(uint64); ok {
				epoch = e
			} else if e, ok := tx["new_epoch"].(float64); ok {
				epoch = uint64(e)
			}
			if epoch == 0 {
				continue
			}
			out = append(out, EpochBoundary{
				Epoch:       epoch,
				BlockNumber: blockNum,
			})
		}
	}
	return out
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func ethCall(client *rpc.Client, from, to common.Address, data []byte, blockTag string) ([]byte, error) {
	callObj := map[string]interface{}{
		"from": from.Hex(),
		"to":   to.Hex(),
		"data": hexutil.Encode(data),
	}
	var result hexutil.Bytes
	if err := client.Call(&result, "eth_call", callObj, blockTag); err != nil {
		return nil, err
	}
	return result, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
