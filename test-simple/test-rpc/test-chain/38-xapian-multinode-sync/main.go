/*
 * BÀI TEST: 38-xapian-multinode-sync
 * MÔ TẢ   : Kiểm thử E2E toàn diện tính nhất quán tuyệt đối của Xapian FullText DB (0x107)
 *           và EVM StateRoot/BlockHash giữa các Node trong cụm Raft Consensus (m0, m1, m2) + Sync Nodes.
 * KỊCH BẢN:
 *   1. Deploy hợp đồng XapianMultiNodeSync (tạo DB cô lập riêng cho mỗi lượt chạy) + Seed 3 baseline docs & counter.
 *   2. Khởi chạy 6 luồng đọc eth_call (queryCluster) bắn liên tục không nghỉ vào m0, m1, m2 suốt thời gian
 *      gửi và commit các block ghi để kiểm chứng WriterPreferredSharedMutex không gây nghẽn (starvation) hay đọc xé lẻ.
 *   3. Gửi giao dịch qua Follower Node ghi document có virtual ID ngắn (docId = 2) -> bảo toàn physical docid = 2.
 *   4. Bắn song song N giao dịch từ nhiều ví rải đều qua m0, m1, m2 tạo documents có cùng điểm BM25 ("equalrank")
 *      và tự tính toán đối chiếu thứ tự (commitBlock ASC, VirtualID ASC) kỳ vọng độc lập bằng Go.
 *   5. Bắn song song N giao dịch tăng bộ đếm chung Xapian + EVM, ĐỒNG THỜI trộn xen kẽ các giao dịch
 *      ghi bẩn/xóa Xapian rồi cố ý REVERT (revertAfterDirtyWrites) để chứng minh không rò rỉ dữ liệu rác (ghost doc).
 *   6. Cập nhật baseline[0], xóa baseline[2], gọi xóa ID ngắn không tồn tại (docId = 1), và gọi giao dịch on-chain
 *      anchorSearchDigestOnChain("metanode") để neo băm kết quả Xapian thẳng vào StateRoot của chuỗi.
 *   7. Chờ toàn bộ các node đồng bộ tới block cuối cùng và kiểm chứng:
 *      - BlockHash và StateRoot (Header.Root) khớp 100% trên mọi node từ block deploy tới block cuối.
 *      - anchoredSearchDigest trên EVM khớp với dữ liệu Xapian và không có bất kỳ ghost doc nào từ tx revert.
 *      - Toàn bộ dữ liệu đọc và 3 truy vấn Search khớp 100% byte-for-byte trên tất cả các node.
 */
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	_ "embed"
	"flag"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tool-test/test-simple/test-rpc/test-chain/config"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

//go:embed contract/abi.json
var contractABIJSON string

//go:embed contract/bytecode.hex
var contractBytecodeHex string

type NodeEndpoint struct {
	Name   string
	URL    string
	Role   string
	Client *ethclient.Client
}

type WalletSigner struct {
	Index   int
	PrivKey *ecdsa.PrivateKey
	Address common.Address
}

type SearchResultItem struct {
	Docid   *big.Int
	Rank    *big.Int
	Percent *big.Int
	Data    []byte
}

type SearchResultsPage struct {
	Total   *big.Int
	Results []SearchResultItem
}

func collectClusterNodes(cfg *config.Config) ([]*NodeEndpoint, []*NodeEndpoint, error) {
	var txNodes []*NodeEndpoint
	var allReadNodes []*NodeEndpoint
	seenURL := make(map[string]bool)

	if len(cfg.RPCNodes) > 0 {
		names := make([]string, 0, len(cfg.RPCNodes))
		for k := range cfg.RPCNodes {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			u := strings.TrimSpace(cfg.RPCNodes[name])
			if u == "" || seenURL[u] {
				continue
			}
			cli, err := ethclient.Dial(u)
			if err != nil {
				return nil, nil, fmt.Errorf("không thể dial validator RPC %s (%s): %w", name, u, err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			_, err = cli.BlockNumber(ctx)
			cancel()
			if err != nil {
				cli.Close()
				return nil, nil, fmt.Errorf("validator RPC %s (%s) không phản hồi eth_blockNumber: %w", name, u, err)
			}
			ep := &NodeEndpoint{Name: name, URL: u, Role: "validator-rpc", Client: cli}
			txNodes = append(txNodes, ep)
			allReadNodes = append(allReadNodes, ep)
			seenURL[u] = true
		}
	}

	if len(txNodes) == 0 && strings.TrimSpace(cfg.RPCUrl) != "" {
		u := strings.TrimSpace(cfg.RPCUrl)
		cli, err := ethclient.Dial(u)
		if err != nil {
			return nil, nil, fmt.Errorf("không thể dial primary RPC (%s): %w", u, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_, err = cli.BlockNumber(ctx)
		cancel()
		if err != nil {
			cli.Close()
			return nil, nil, fmt.Errorf("primary RPC (%s) không phản hồi eth_blockNumber: %w", u, err)
		}
		ep := &NodeEndpoint{Name: "primary", URL: u, Role: "validator-rpc", Client: cli}
		txNodes = append(txNodes, ep)
		allReadNodes = append(allReadNodes, ep)
		seenURL[u] = true
	}

	if len(cfg.SyncNodes) > 0 {
		syncNames := make([]string, 0, len(cfg.SyncNodes))
		for k := range cfg.SyncNodes {
			syncNames = append(syncNames, k)
		}
		sort.Strings(syncNames)
		for _, name := range syncNames {
			u := strings.TrimSpace(cfg.SyncNodes[name])
			if u == "" || seenURL[u] {
				continue
			}
			cli, err := ethclient.Dial(u)
			if err != nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, bErr := cli.BlockNumber(ctx)
			cancel()
			if bErr != nil {
				cli.Close()
				continue
			}
			ep := &NodeEndpoint{Name: name, URL: u, Role: "sync-rpc", Client: cli}
			allReadNodes = append(allReadNodes, ep)
			seenURL[u] = true
		}
	}

	if len(txNodes) == 0 {
		return nil, nil, fmt.Errorf("không tìm thấy RPC node hợp lệ nào trong cấu hình")
	}
	return txNodes, allReadNodes, nil
}

func closeClusterNodes(nodes []*NodeEndpoint) {
	for _, n := range nodes {
		if n != nil && n.Client != nil {
			n.Client.Close()
		}
	}
}

func suggestSafeGasPrice(primary *ethclient.Client) *big.Int {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	gp, err := primary.SuggestGasPrice(ctx)
	if err != nil || gp == nil || gp.Sign() <= 0 {
		return big.NewInt(1_000_000_000)
	}
	return gp
}

func nextPendingNonce(primary, target *ethclient.Client, addr common.Address) (uint64, error) {
	ctx1, cancel1 := context.WithTimeout(context.Background(), 8*time.Second)
	nPrimary, err1 := primary.PendingNonceAt(ctx1, addr)
	cancel1()
	if err1 != nil {
		return 0, fmt.Errorf("không đọc được pending nonce từ primary cho %s: %w", addr.Hex(), err1)
	}

	if target == nil || target == primary {
		return nPrimary, nil
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 8*time.Second)
	nTarget, err2 := target.PendingNonceAt(ctx2, addr)
	cancel2()
	if err2 == nil && nTarget > nPrimary {
		return nTarget, nil
	}
	return nPrimary, nil
}

func broadcastSignedTx(primary, target *NodeEndpoint, signedTx *types.Transaction) (string, error) {
	chosen := target
	if chosen == nil {
		chosen = primary
	}

	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := chosen.Client.SendTransaction(ctx, signedTx)
		cancel()
		if err == nil || strings.Contains(strings.ToLower(err.Error()), "already known") {
			return chosen.Name, nil
		}
		time.Sleep(150 * time.Millisecond)
	}

	if chosen != primary {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := primary.Client.SendTransaction(ctx, signedTx)
		cancel()
		if err == nil || strings.Contains(strings.ToLower(err.Error()), "already known") {
			return primary.Name, nil
		}
		return "", fmt.Errorf("gửi giao dịch %s qua %s và fallback %s không thành công: %w",
			signedTx.Hash().Hex(), chosen.Name, primary.Name, err)
	}

	return "", fmt.Errorf("gửi giao dịch %s qua %s không thành công", signedTx.Hash().Hex(), chosen.Name)
}

func waitForReceiptOnCluster(nodes []*NodeEndpoint, txHash common.Hash, timeout time.Duration) (*types.Receipt, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, n := range nodes {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			rcp, err := n.Client.TransactionReceipt(ctx, txHash)
			cancel()
			if err == nil && rcp != nil && rcp.BlockNumber != nil && rcp.BlockNumber.Uint64() > 0 {
				return rcp, nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil, fmt.Errorf("quá thời gian chờ biên nhận giao dịch %s sau %v", txHash.Hex(), timeout)
}

func waitAllNodesReachBlock(nodes []*NodeEndpoint, targetBlock uint64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	lastHeights := make(map[string]uint64, len(nodes))

	for time.Now().Before(deadline) {
		allReached := true
		for _, n := range nodes {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			bn, err := n.Client.BlockNumber(ctx)
			cancel()
			if err != nil {
				allReached = false
				continue
			}
			lastHeights[n.Name] = bn
			if bn < targetBlock {
				allReached = false
			}
		}
		if allReached {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("các node chưa đồng bộ kịp tới block %d (trạng thái hiện tại: %v)", targetBlock, lastHeights)
}

func deploySyncContract(
	primary *NodeEndpoint,
	txNodes []*NodeEndpoint,
	signer WalletSigner,
	chainID int64,
	bytecode []byte,
) (common.Address, uint64, error) {
	nonce, err := nextPendingNonce(primary.Client, primary.Client, signer.Address)
	if err != nil {
		return common.Address{}, 0, err
	}
	gasPrice := suggestSafeGasPrice(primary.Client)
	unsigned := types.NewContractCreation(nonce, big.NewInt(0), 5_500_000, gasPrice, bytecode)
	signed, err := types.SignTx(unsigned, types.NewEIP155Signer(big.NewInt(chainID)), signer.PrivKey)
	if err != nil {
		return common.Address{}, 0, fmt.Errorf("ký giao dịch deploy không thành công: %w", err)
	}
	if _, err := broadcastSignedTx(primary, primary, signed); err != nil {
		return common.Address{}, 0, err
	}
	rcp, err := waitForReceiptOnCluster(txNodes, signed.Hash(), 45*time.Second)
	if err != nil {
		return common.Address{}, 0, err
	}
	if rcp.Status != types.ReceiptStatusSuccessful {
		return common.Address{}, 0, fmt.Errorf("deploy hợp đồng bị revert (status=%d, tx=%s)", rcp.Status, signed.Hash().Hex())
	}
	return rcp.ContractAddress, rcp.BlockNumber.Uint64(), nil
}

func sendContractCallViaNode(
	primary *NodeEndpoint,
	target *NodeEndpoint,
	txNodes []*NodeEndpoint,
	signer WalletSigner,
	chainID int64,
	contractAddr common.Address,
	parsedABI abi.ABI,
	expectRevert bool,
	method string,
	args ...interface{},
) (*types.Receipt, string, error) {
	callData, err := parsedABI.Pack(method, args...)
	if err != nil {
		return nil, "", fmt.Errorf("pack calldata %s lỗi: %w", method, err)
	}
	nonce, err := nextPendingNonce(primary.Client, target.Client, signer.Address)
	if err != nil {
		return nil, "", err
	}
	gasPrice := suggestSafeGasPrice(primary.Client)
	unsigned := types.NewTransaction(nonce, contractAddr, big.NewInt(0), 3_500_000, gasPrice, callData)
	signed, err := types.SignTx(unsigned, types.NewEIP155Signer(big.NewInt(chainID)), signer.PrivKey)
	if err != nil {
		return nil, "", fmt.Errorf("ký giao dịch %s lỗi: %w", method, err)
	}
	sentVia, err := broadcastSignedTx(primary, target, signed)
	if err != nil {
		return nil, "", err
	}
	rcp, err := waitForReceiptOnCluster(txNodes, signed.Hash(), 45*time.Second)
	if err != nil {
		return nil, "", err
	}
	if expectRevert {
		if rcp.Status != types.ReceiptStatusFailed {
			return nil, sentVia, fmt.Errorf("giao dịch %s đáng lẽ phải revert (status=0) nhưng lại thành công", method)
		}
		return rcp, sentVia, nil
	}
	if rcp.Status != types.ReceiptStatusSuccessful {
		return nil, sentVia, fmt.Errorf("giao dịch %s bị revert ngoài ý muốn (status=%d, tx=%s)", method, rcp.Status, signed.Hash().Hex())
	}
	return rcp, sentVia, nil
}

func ethCallRawWithRetry(
	node *NodeEndpoint,
	caller common.Address,
	contractAddr common.Address,
	callData []byte,
) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		out, err := node.Client.CallContract(ctx, ethereum.CallMsg{
			From: caller,
			To:   &contractAddr,
			Gas:  6_000_000,
			Data: callData,
		}, nil)
		cancel()
		if err == nil {
			return out, nil
		}
		lastErr = err
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("eth_call trên node %s (%s) gặp sự cố: %w", node.Name, node.URL, lastErr)
}

func verifyBlockHeadersAndStateRootsAcrossNodes(nodes []*NodeEndpoint, startBlock, endBlock uint64) error {
	if len(nodes) < 2 {
		return nil
	}
	for bn := startBlock; bn <= endBlock; bn++ {
		bNum := new(big.Int).SetUint64(bn)
		var refHash common.Hash
		var refRoot common.Hash
		for idx, n := range nodes {
			var hdr *types.Header
			var hErr error
			for attempt := 0; attempt < 5; attempt++ {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				hdr, hErr = n.Client.HeaderByNumber(ctx, bNum)
				cancel()
				if hErr == nil && hdr != nil {
					break
				}
				time.Sleep(150 * time.Millisecond)
			}
			if hErr != nil || hdr == nil {
				return fmt.Errorf("không đọc được header block %d trên node %s: %v", bn, n.Name, hErr)
			}
			if idx == 0 {
				refHash = hdr.Hash()
				refRoot = hdr.Root
			} else {
				if hdr.Hash() != refHash {
					return fmt.Errorf("LỆCH BLOCK HASH tại block %d: node %s (%s) != node %s (%s)",
						bn, n.Name, hdr.Hash().Hex(), nodes[0].Name, refHash.Hex())
				}
				if hdr.Root != refRoot {
					return fmt.Errorf("LỆCH STATE ROOT tại block %d: node %s (%s) != node %s (%s)",
						bn, n.Name, hdr.Root.Hex(), nodes[0].Name, refRoot.Hex())
				}
			}
		}
	}
	return nil
}

func RunTest(configPath string) error {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("đọc cấu hình thất bại: %w", err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(contractABIJSON))
	if err != nil {
		return fmt.Errorf("parse ABI thất bại: %w", err)
	}
	bytecode := common.FromHex(strings.TrimSpace(contractBytecodeHex))
	if len(bytecode) == 0 {
		return fmt.Errorf("bytecode hợp đồng rỗng")
	}

	if len(cfg.PrivateKeys) < 2 {
		return fmt.Errorf("cần tối thiểu 2 private keys trong cấu hình, hiện có %d", len(cfg.PrivateKeys))
	}

	maxWallets := len(cfg.PrivateKeys)
	if maxWallets > 6 {
		maxWallets = 6
	}
	wallets := make([]WalletSigner, 0, maxWallets)
	for i := 0; i < maxWallets; i++ {
		cleanKey := strings.TrimPrefix(strings.TrimSpace(cfg.PrivateKeys[i]), "0x")
		pk, kErr := crypto.HexToECDSA(cleanKey)
		if kErr != nil {
			return fmt.Errorf("parse private key vị trí %d lỗi: %w", i, kErr)
		}
		addr := crypto.PubkeyToAddress(pk.PublicKey)
		wallets = append(wallets, WalletSigner{
			Index:   i,
			PrivKey: pk,
			Address: addr,
		})
	}

	txNodes, readNodes, err := collectClusterNodes(cfg)
	if err != nil {
		return err
	}
	defer closeClusterNodes(readNodes)

	primary := txNodes[0]

	fmt.Println("==========================================================")
	fmt.Println("🚀 BÀI TEST: 38-xapian-multinode-sync")
	fmt.Printf("   • Chain ID           : %d\n", cfg.ChainID)
	fmt.Printf("   • Số ví song song    : %d ví\n", len(wallets))
	fmt.Printf("   • Validator RPC Nodes: %d node\n", len(txNodes))
	for _, n := range txNodes {
		fmt.Printf("     - %s (%s) [%s]\n", n.Name, n.URL, n.Role)
	}
	if len(readNodes) > len(txNodes) {
		for _, n := range readNodes[len(txNodes):] {
			fmt.Printf("     - %s (%s) [%s]\n", n.Name, n.URL, n.Role)
		}
	}
	fmt.Println("==========================================================")

	// -------------------------------------------------------------------------
	// BƯỚC 1: Deploy Contract & Seed Baseline trên Node đầu tiên
	// -------------------------------------------------------------------------
	fmt.Println("\n[Bước 1/7] Deploy hợp đồng XapianMultiNodeSync và khởi tạo Baseline...")
	contractAddr, deployBlock, err := deploySyncContract(primary, txNodes, wallets[0], cfg.ChainID, bytecode)
	if err != nil {
		return err
	}
	maxBlock := deployBlock
	fmt.Printf("   ✅ Hợp đồng đã deploy tại: %s (Block %d)\n", contractAddr.Hex(), deployBlock)

	if err := waitAllNodesReachBlock(readNodes, maxBlock, 25*time.Second); err != nil {
		return err
	}

	seedRcp, seedVia, err := sendContractCallViaNode(
		primary, txNodes[0], txNodes, wallets[0], cfg.ChainID, contractAddr, parsedABI, false, "seedBaseline",
	)
	if err != nil {
		return err
	}
	if seedRcp.BlockNumber.Uint64() > maxBlock {
		maxBlock = seedRcp.BlockNumber.Uint64()
	}
	if err := waitAllNodesReachBlock(readNodes, maxBlock, 25*time.Second); err != nil {
		return err
	}
	fmt.Printf("   ✅ Đã tạo 3 baseline docs + 1 counter doc qua node %s (Block %d)\n", seedVia, seedRcp.BlockNumber.Uint64())

	// -------------------------------------------------------------------------
	// Khởi chạy luồng đọc eth_call (queryCluster) liên tục song song với luồng commit block
	// để kiểm chứng thực tế WriterPreferredSharedMutex không gây nghẽn hay lỗi đọc giữa chừng!
	// -------------------------------------------------------------------------
	var (
		stopReadFlood atomic.Bool
		readFloodOps  atomic.Uint64
		readFloodErr  atomic.Value
		readFloodWG   sync.WaitGroup
	)
	bgQueryCallData, err := parsedABI.Pack("queryCluster", "metanode", int64(-1), false, uint64(20))
	if err != nil {
		return err
	}
	for _, rn := range readNodes {
		for worker := 0; worker < 2; worker++ {
			readFloodWG.Add(1)
			go func(ep *NodeEndpoint) {
				defer readFloodWG.Done()
				for !stopReadFlood.Load() {
					ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
					out, cErr := ep.Client.CallContract(ctx, ethereum.CallMsg{
						From: wallets[0].Address,
						To:   &contractAddr,
						Gas:  4_000_000,
						Data: bgQueryCallData,
					}, nil)
					cancel()
					if cErr != nil {
						readFloodErr.CompareAndSwap(nil, fmt.Errorf("luồng đọc nền trên %s báo lỗi: %w", ep.Name, cErr))
						return
					}
					var unpacked struct {
						Page SearchResultsPage
					}
					if uErr := parsedABI.UnpackIntoInterface(&unpacked, "queryCluster", out); uErr != nil {
						readFloodErr.CompareAndSwap(nil, fmt.Errorf("luồng đọc nền trên %s giải mã lỗi: %w", ep.Name, uErr))
						return
					}
					if unpacked.Page.Total.Int64() < 2 {
						readFloodErr.CompareAndSwap(nil, fmt.Errorf("luồng đọc nền trên %s thấy dữ liệu rỗng bất thường (total=%s)",
							ep.Name, unpacked.Page.Total.String()))
						return
					}
					readFloodOps.Add(1)
					time.Sleep(5 * time.Millisecond)
				}
			}(rn)
		}
	}
	defer func() {
		stopReadFlood.Store(true)
		readFloodWG.Wait()
	}()

	// -------------------------------------------------------------------------
	// BƯỚC 2: Ghi Virtual DocID ngắn (docId = 2) qua Node thứ hai (Follower forwarding)
	// -------------------------------------------------------------------------
	stage2Target := txNodes[1%len(txNodes)]
	fmt.Printf("\n[Bước 2/7] Ghi Virtual DocID ngắn (docId = 2) qua node %s (trong khi 6 luồng eth_call đang đọc liên tục)...\n", stage2Target.Name)
	shortRcp, shortVia, err := sendContractCallViaNode(
		primary, stage2Target, txNodes, wallets[0], cfg.ChainID, contractAddr, parsedABI, false, "writeShortHexDoc",
	)
	if err != nil {
		return err
	}
	if shortRcp.BlockNumber.Uint64() > maxBlock {
		maxBlock = shortRcp.BlockNumber.Uint64()
	}
	fmt.Printf("   ✅ Đã ghi docId=2 độc lập qua node %s (Block %d)\n", shortVia, shortRcp.BlockNumber.Uint64())

	// -------------------------------------------------------------------------
	// BƯỚC 3: Tạo song song N documents có điểm BM25 bằng nhau rải đều qua m0, m1, m2
	// -------------------------------------------------------------------------
	if err := waitAllNodesReachBlock(readNodes, maxBlock, 25*time.Second); err != nil {
		return err
	}
	fmt.Printf("\n[Bước 3/7] Bắn song song %d giao dịch parallelCreateAndIndex rải qua %d RPC nodes...\n", len(wallets), len(txNodes))
	var (
		wg             sync.WaitGroup
		mu             sync.Mutex
		stepErrs       []error
		nodeHits       = make(map[string]int)
		parCommitByTag = make(map[string]uint64)
	)
	for i := range wallets {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			target := txNodes[idx%len(txNodes)]
			tag := fmt.Sprintf("par-doc-w%d", idx)
			score := fmt.Sprintf("%d", (idx+1)*10)
			rcp, via, callErr := sendContractCallViaNode(
				primary, target, txNodes, wallets[idx], cfg.ChainID,
				contractAddr, parsedABI, false, "parallelCreateAndIndex", tag, score,
			)
			mu.Lock()
			defer mu.Unlock()
			if callErr != nil {
				stepErrs = append(stepErrs, fmt.Errorf("ví %d parallelCreateAndIndex: %w", idx, callErr))
				return
			}
			nodeHits[via]++
			parCommitByTag[tag] = rcp.BlockNumber.Uint64()
			if rcp.BlockNumber.Uint64() > maxBlock {
				maxBlock = rcp.BlockNumber.Uint64()
			}
		}(i)
	}
	wg.Wait()
	if len(stepErrs) > 0 {
		return stepErrs[0]
	}
	fmt.Printf("   ✅ Hoàn tất tạo song song %d equal-rank docs (phân bổ node nhận tx: %v, maxBlock=%d)\n",
		len(wallets), nodeHits, maxBlock)

	// -------------------------------------------------------------------------
	// BƯỚC 4: Bắn song song N giao dịch incrementSharedCounter TRỘN LẪN giao dịch REVERT Xapian!
	// -------------------------------------------------------------------------
	if err := waitAllNodesReachBlock(readNodes, maxBlock, 25*time.Second); err != nil {
		return err
	}
	fmt.Printf("\n[Bước 4/7] Bắn song song %d tx incrementSharedCounter + trộn xen kẽ tx cố ý REVERT ghi bẩn Xapian...\n", len(wallets))
	stepErrs = nil
	for i := range wallets {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			target := txNodes[(idx+1)%len(txNodes)]
			rcp, via, callErr := sendContractCallViaNode(
				primary, target, txNodes, wallets[idx], cfg.ChainID,
				contractAddr, parsedABI, false, "incrementSharedCounter",
			)
			mu.Lock()
			defer mu.Unlock()
			if callErr != nil {
				stepErrs = append(stepErrs, fmt.Errorf("ví %d incrementSharedCounter: %w", idx, callErr))
				return
			}
			nodeHits[via]++
			if rcp.BlockNumber.Uint64() > maxBlock {
				maxBlock = rcp.BlockNumber.Uint64()
			}
		}(i)
	}
	wg.Wait()
	if len(stepErrs) > 0 {
		return stepErrs[0]
	}

	// Bắn tiếp đợt giao dịch cố ý REVERT sau khi đã tạo ghost doc, sửa bẩn baseline[1] và xóa baseline[0]
	if err := waitAllNodesReachBlock(readNodes, maxBlock, 25*time.Second); err != nil {
		return err
	}
	numRevertTxs := 3
	if len(wallets) < numRevertTxs {
		numRevertTxs = len(wallets)
	}
	for i := 0; i < numRevertTxs; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			target := txNodes[idx%len(txNodes)]
			ghostTag := fmt.Sprintf("ghost-reverted-w%d", idx)
			rcp, _, revErr := sendContractCallViaNode(
				primary, target, txNodes, wallets[idx], cfg.ChainID,
				contractAddr, parsedABI, true, "revertAfterDirtyWrites", ghostTag,
			)
			mu.Lock()
			defer mu.Unlock()
			if revErr != nil {
				stepErrs = append(stepErrs, fmt.Errorf("ví %d revertAfterDirtyWrites: %w", idx, revErr))
				return
			}
			if rcp.BlockNumber.Uint64() > maxBlock {
				maxBlock = rcp.BlockNumber.Uint64()
			}
		}(i)
	}
	wg.Wait()
	if len(stepErrs) > 0 {
		return stepErrs[0]
	}
	fmt.Printf("   ✅ Hoàn tất %d tx tăng counter + %d tx revert ghi bẩn Xapian đúng kỳ vọng (status=0, maxBlock=%d)\n",
		len(wallets), numRevertTxs, maxBlock)

	// -------------------------------------------------------------------------
	// BƯỚC 5: Update baseline[0], Delete baseline[2], Delete(docId=1) qua Node thứ ba
	// -------------------------------------------------------------------------
	if err := waitAllNodesReachBlock(readNodes, maxBlock, 25*time.Second); err != nil {
		return err
	}
	stage5Target := txNodes[2%len(txNodes)]
	fmt.Printf("\n[Bước 5/7] Cập nhật baseline[0], xóa baseline[2] và kiểm tra delete(docId=1) qua node %s...\n", stage5Target.Name)
	mutRcp, mutVia, err := sendContractCallViaNode(
		primary, stage5Target, txNodes, wallets[0], cfg.ChainID, contractAddr, parsedABI, false, "updateAndDeleteBaseline",
	)
	if err != nil {
		return err
	}
	if mutRcp.BlockNumber.Uint64() > maxBlock {
		maxBlock = mutRcp.BlockNumber.Uint64()
	}
	fmt.Printf("   ✅ Hoàn tất update/delete qua node %s (Block %d)\n", mutVia, mutRcp.BlockNumber.Uint64())

	// -------------------------------------------------------------------------
	// BƯỚC 6: Neo kết quả querySearch("metanode") trực tiếp vào EVM StateRoot trên on-chain!
	// -------------------------------------------------------------------------
	if err := waitAllNodesReachBlock(readNodes, maxBlock, 25*time.Second); err != nil {
		return err
	}
	fmt.Println("\n[Bước 6/7] Gọi giao dịch on-chain anchorSearchDigestOnChain(\"metanode\") để neo Xapian vào StateRoot...")
	anchorRcp, anchorVia, err := sendContractCallViaNode(
		primary, txNodes[1%len(txNodes)], txNodes, wallets[0], cfg.ChainID,
		contractAddr, parsedABI, false, "anchorSearchDigestOnChain", "metanode",
	)
	if err != nil {
		return err
	}
	if anchorRcp.BlockNumber.Uint64() > maxBlock {
		maxBlock = anchorRcp.BlockNumber.Uint64()
	}

	// Dừng luồng đọc nền và kiểm tra kết quả WriterPreferredSharedMutex
	stopReadFlood.Store(true)
	readFloodWG.Wait()
	if bgErrVal := readFloodErr.Load(); bgErrVal != nil {
		return bgErrVal.(error)
	}
	fmt.Printf("   ✅ Đã neo digest Xapian vào EVM StateRoot qua node %s (Block %d)\n", anchorVia, anchorRcp.BlockNumber.Uint64())
	fmt.Printf("   ✅ Luồng đọc song song (WriterPreferredSharedMutex): %d lượt eth_call thành công, 0 lỗi, 0 nghẽn commit!\n",
		readFloodOps.Load())

	// -------------------------------------------------------------------------
	// BƯỚC 7: Chờ đồng bộ toàn cụm & Đối chiếu BlockHash + StateRoot + Xapian Byte-for-Byte
	// -------------------------------------------------------------------------
	fmt.Printf("\n[Bước 7/7] Chờ tất cả %d node đồng bộ tới Block >= %d và kiểm chứng toàn diện...\n",
		len(readNodes), maxBlock)
	if err := waitAllNodesReachBlock(readNodes, maxBlock, 30*time.Second); err != nil {
		return err
	}

	// 7.1 Đối chiếu BlockHash và StateRoot trên TẤT CẢ các node từ deployBlock..maxBlock
	if err := verifyBlockHeadersAndStateRootsAcrossNodes(readNodes, deployBlock, maxBlock); err != nil {
		return err
	}
	fmt.Printf("   ✅ [Consensus Header & StateRoot] Toàn bộ blocks [%d..%d] có BlockHash và StateRoot KHỚP 100%% trên cả %d node!\n",
		deployBlock, maxBlock, len(readNodes))

	caller := wallets[0].Address

	// 7.2 Kiểm tra anchoredSearchDigest trên mọi node (khác 0 và đồng nhất)
	digestCall, err := parsedABI.Pack("anchoredSearchDigest")
	if err != nil {
		return err
	}
	refDigestRaw, err := ethCallRawWithRetry(readNodes[0], caller, contractAddr, digestCall)
	if err != nil {
		return err
	}
	var anchoredDigest [32]byte
	if err := parsedABI.UnpackIntoInterface(&anchoredDigest, "anchoredSearchDigest", refDigestRaw); err != nil {
		return err
	}
	if anchoredDigest == ([32]byte{}) {
		return fmt.Errorf("anchoredSearchDigest bị rỗng (0x0)")
	}
	for idx := 1; idx < len(readNodes); idx++ {
		raw, cErr := ethCallRawWithRetry(readNodes[idx], caller, contractAddr, digestCall)
		if cErr != nil {
			return cErr
		}
		if !bytes.Equal(refDigestRaw, raw) {
			return fmt.Errorf("anchoredSearchDigest trên node %s lệch so với node %s", readNodes[idx].Name, readNodes[0].Name)
		}
	}
	fmt.Printf("   ✅ [On-Chain Xapian Digest] anchoredSearchDigest = 0x%x (khớp trong StateRoot cả %d node)\n",
		anchoredDigest, len(readNodes))

	// 7.3 Kiểm tra KHÔNG CÓ bất kỳ dữ liệu rò rỉ (ghost doc) nào từ các giao dịch đã REVERT!
	ghostQueryCall, err := parsedABI.Pack("queryCluster", "ghost", int64(-1), false, uint64(10))
	if err != nil {
		return err
	}
	for _, n := range readNodes {
		rawGhost, gErr := ethCallRawWithRetry(n, caller, contractAddr, ghostQueryCall)
		if gErr != nil {
			return gErr
		}
		var ghostUnpacked struct {
			Page SearchResultsPage
		}
		if uErr := parsedABI.UnpackIntoInterface(&ghostUnpacked, "queryCluster", rawGhost); uErr != nil {
			return uErr
		}
		if ghostUnpacked.Page.Total.Sign() != 0 || len(ghostUnpacked.Page.Results) != 0 {
			return fmt.Errorf("PHÁT HIỆN RÒ RỈ GHOST DOC TỪ TX REVERT trên node %s: total=%s",
				n.Name, ghostUnpacked.Page.Total.String())
		}
	}
	fmt.Printf("   ✅ [Revert Isolation Guard] 0 ghost docs từ %d giao dịch revert trên tất cả %d node!\n",
		numRevertTxs, len(readNodes))

	// 7.4 Kiểm tra getTrackedIds & readSharedCounter trên mọi node
	trackedCall, err := parsedABI.Pack("getTrackedIds")
	if err != nil {
		return err
	}
	refTrackedRaw, err := ethCallRawWithRetry(readNodes[0], caller, contractAddr, trackedCall)
	if err != nil {
		return err
	}
	var trackedOut struct {
		BaseIds  [3]*big.Int
		CntDocId *big.Int
		EvmCnt   *big.Int
		ParIds   []*big.Int
	}
	if err := parsedABI.UnpackIntoInterface(&trackedOut, "getTrackedIds", refTrackedRaw); err != nil {
		return fmt.Errorf("unpack getTrackedIds lỗi: %w", err)
	}
	if int(trackedOut.EvmCnt.Int64()) != len(wallets) {
		return fmt.Errorf("evmCounter sai lệch: kỳ vọng %d, nhận được %s", len(wallets), trackedOut.EvmCnt.String())
	}
	if len(trackedOut.ParIds) != len(wallets) {
		return fmt.Errorf("số lượng parallelDocIds sai lệch: kỳ vọng %d, nhận được %d", len(wallets), len(trackedOut.ParIds))
	}

	cntCall, err := parsedABI.Pack("readSharedCounter")
	if err != nil {
		return err
	}
	for _, n := range readNodes {
		rawCnt, cErr := ethCallRawWithRetry(n, caller, contractAddr, cntCall)
		if cErr != nil {
			return cErr
		}
		var cntDecoded struct {
			XapianCnt *big.Int
			EvmCnt    *big.Int
		}
		if uErr := parsedABI.UnpackIntoInterface(&cntDecoded, "readSharedCounter", rawCnt); uErr != nil {
			return fmt.Errorf("unpack readSharedCounter trên node %s lỗi: %w", n.Name, uErr)
		}
		if int(cntDecoded.XapianCnt.Int64()) != len(wallets) || int(cntDecoded.EvmCnt.Int64()) != len(wallets) {
			return fmt.Errorf("node %s lệch bộ đếm chia sẻ: xapian=%s, evm=%s (kỳ vọng %d)",
				n.Name, cntDecoded.XapianCnt.String(), cntDecoded.EvmCnt.String(), len(wallets))
		}
	}
	fmt.Printf("\n📌 [Shared Counter Doc] Virtual DocID = 0x%064x | Xapian Counter = %d | EVM Counter = %d (khớp cả %d node)\n",
		trackedOut.CntDocId, len(wallets), len(wallets), len(readNodes))

	// 7.5 Kiểm tra & in chi tiết từng document trên mọi node
	type docCheckSpec struct {
		label     string
		docID     *big.Int
		wantData  string
		wantScore string
		deleted   bool
		checkData bool
	}
	docChecks := []docCheckSpec{
		{
			label:     "baseline[0] (đã update & boost BM25)",
			docID:     trackedOut.BaseIds[0],
			wantData:  "baseline-alpha|v2-updated",
			wantScore: "150",
			checkData: true,
		},
		{
			label:     "baseline[1] (giữ nguyên tại physical docid)",
			docID:     trackedOut.BaseIds[1],
			wantData:  "baseline-bravo-phys2-guard|v1",
			wantScore: "200",
			checkData: true,
		},
		{
			label:   "baseline[2] (đã gọi deleteDocument)",
			docID:   trackedOut.BaseIds[2],
			deleted: true,
		},
		{
			label:     "short virtual docId=2 (độc lập Q2)",
			docID:     big.NewInt(2),
			wantData:  "short-docid-0x2|independent",
			wantScore: "250",
			checkData: true,
		},
	}
	for i, pID := range trackedOut.ParIds {
		docChecks = append(docChecks, docCheckSpec{
			label:     fmt.Sprintf("parallelDoc[%d]", i),
			docID:     pID,
			checkData: false,
		})
	}

	type docMetaInfo struct {
		VirtualID   *big.Int
		VirtualHex  string
		ScoreVal    string
		CommitBlock uint64
	}
	payloadToMeta := make(map[string]docMetaInfo)

	fmt.Println("\n📋 DANH SÁCH TOÀN BỘ DOCUMENTS TRÊN XAPIAN (ĐÃ ĐỐI CHIẾU KHỚP CẢ 3 NODE):")
	for _, spec := range docChecks {
		snapCall, pErr := parsedABI.Pack("readDocSnapshot", spec.docID)
		if pErr != nil {
			return pErr
		}
		var refSnapRaw []byte
		var printedSnap struct {
			Data     []byte
			ScoreVal string
			Terms    []string
		}
		for idx, n := range readNodes {
			raw, cErr := ethCallRawWithRetry(n, caller, contractAddr, snapCall)
			if cErr != nil {
				return cErr
			}
			if idx == 0 {
				refSnapRaw = raw
			} else if !bytes.Equal(refSnapRaw, raw) {
				return fmt.Errorf("dữ liệu readDocSnapshot(%s) trên node %s không khớp byte-for-byte với node %s",
					spec.label, n.Name, readNodes[0].Name)
			}

			var snap struct {
				Data     []byte
				ScoreVal string
				Terms    []string
			}
			if uErr := parsedABI.UnpackIntoInterface(&snap, "readDocSnapshot", raw); uErr != nil {
				return fmt.Errorf("unpack readDocSnapshot(%s) trên node %s lỗi: %w", spec.label, n.Name, uErr)
			}
			if idx == 0 {
				printedSnap = snap
			}
			if spec.deleted {
				if len(snap.Data) != 0 {
					return fmt.Errorf("node %s vẫn còn dữ liệu của document đã xóa (%s): %q",
						n.Name, spec.label, string(snap.Data))
				}
			} else if spec.checkData {
				if string(snap.Data) != spec.wantData || snap.ScoreVal != spec.wantScore {
					return fmt.Errorf("node %s sai nội dung %s: got data=%q score=%q, want data=%q score=%q",
						n.Name, spec.label, string(snap.Data), snap.ScoreVal, spec.wantData, spec.wantScore)
				}
			} else if len(snap.Data) == 0 {
				return fmt.Errorf("node %s thiếu dữ liệu của %s", n.Name, spec.label)
			}
		}

		vHex := fmt.Sprintf("0x%064x", spec.docID)
		if spec.deleted {
			fmt.Printf("   • %-42s | VirtualID: %s | Trạng thái: [ĐÃ XÓA - RỖNG]\n", spec.label, vHex)
		} else {
			payloadStr := string(printedSnap.Data)
			payloadToMeta[payloadStr] = docMetaInfo{
				VirtualID:   new(big.Int).Set(spec.docID),
				VirtualHex:  vHex,
				ScoreVal:    printedSnap.ScoreVal,
				CommitBlock: parCommitByTag[payloadStr],
			}
			fmt.Printf("   • %-42s | VirtualID: %s | Slot0=%-4s | Data=%q | Terms=%v\n",
				spec.label, vHex, printedSnap.ScoreVal, payloadStr, printedSnap.Terms)
		}
	}

	// Tính toán độc lập trong Go thứ tự kỳ vọng chính xác của 6 parallel docs theo đặc tả:
	// (CommitBlock tăng dần, trong cùng 1 block thì VirtualID hex 64 ký tự tăng dần)
	type expectedEqualRankItem struct {
		Tag         string
		CommitBlock uint64
		VirtualID   *big.Int
	}
	expectedEqualRank := make([]expectedEqualRankItem, 0, len(wallets))
	for i := range wallets {
		tag := fmt.Sprintf("par-doc-w%d", i)
		meta, ok := payloadToMeta[tag]
		if !ok {
			return fmt.Errorf("không tìm thấy metadata cho %s", tag)
		}
		expectedEqualRank = append(expectedEqualRank, expectedEqualRankItem{
			Tag:         tag,
			CommitBlock: meta.CommitBlock,
			VirtualID:   meta.VirtualID,
		})
	}
	sort.Slice(expectedEqualRank, func(i, j int) bool {
		if expectedEqualRank[i].CommitBlock != expectedEqualRank[j].CommitBlock {
			return expectedEqualRank[i].CommitBlock < expectedEqualRank[j].CommitBlock
		}
		return expectedEqualRank[i].VirtualID.Cmp(expectedEqualRank[j].VirtualID) < 0
	})

	// 7.6 Đối chiếu 3 truy vấn Search cốt lõi trên MỌI node
	type querySpec struct {
		name      string
		query     string
		sortSlot  int64
		sortAsc   bool
		wantTotal int
		checkFn   func(nodeName string, page SearchResultsPage) error
	}

	expectedAllMetanodeDocs := len(wallets) + 3 // N parallel docs + baseline[0] + baseline[1] + shortId(2)

	queries := []querySpec{
		{
			name:      "Equal-Rank BM25 Physical DocID Tie-Break",
			query:     "equalrank",
			sortSlot:  -1,
			sortAsc:   false,
			wantTotal: len(wallets),
			checkFn: func(nodeName string, page SearchResultsPage) error {
				if len(page.Results) != len(wallets) {
					return fmt.Errorf("node %s trả về %d kết quả equalrank, kỳ vọng %d", nodeName, len(page.Results), len(wallets))
				}
				for i, item := range page.Results {
					gotTag := string(item.Data)
					wantTag := expectedEqualRank[i].Tag
					if gotTag != wantTag {
						return fmt.Errorf("node %s sai thứ tự tất định equalrank tại vị trí #%d: got %q, want %q",
							nodeName, i+1, gotTag, wantTag)
					}
					if i > 0 && item.Docid.Cmp(page.Results[i-1].Docid) <= 0 {
						return fmt.Errorf("node %s physical docid không tăng chặt tại vị trí #%d", nodeName, i+1)
					}
				}
				return nil
			},
		},
		{
			name:      "Value-Slot 0 Ascending Sort",
			query:     "metanode",
			sortSlot:  0,
			sortAsc:   true,
			wantTotal: expectedAllMetanodeDocs,
			checkFn: func(nodeName string, page SearchResultsPage) error {
				if len(page.Results) != expectedAllMetanodeDocs {
					return fmt.Errorf("node %s trả về %d kết quả sort slot 0, kỳ vọng %d",
						nodeName, len(page.Results), expectedAllMetanodeDocs)
				}
				firstPayload := string(page.Results[0].Data)
				lastPayload := string(page.Results[len(page.Results)-1].Data)
				if firstPayload != "par-doc-w0" {
					return fmt.Errorf("node %s sai phần tử đầu khi sort tăng dần slot 0: got %q, want %q",
						nodeName, firstPayload, "par-doc-w0")
				}
				if lastPayload != "short-docid-0x2|independent" {
					return fmt.Errorf("node %s sai phần tử cuối khi sort tăng dần slot 0: got %q, want %q",
						nodeName, lastPayload, "short-docid-0x2|independent")
				}
				return nil
			},
		},
		{
			name:      "BM25 Relevance Boost Ranking",
			query:     "metanode",
			sortSlot:  -1,
			sortAsc:   false,
			wantTotal: expectedAllMetanodeDocs,
			checkFn: func(nodeName string, page SearchResultsPage) error {
				if len(page.Results) != expectedAllMetanodeDocs {
					return fmt.Errorf("node %s trả về %d kết quả BM25 metanode, kỳ vọng %d",
						nodeName, len(page.Results), expectedAllMetanodeDocs)
				}
				topPayload := string(page.Results[0].Data)
				if topPayload != "baseline-alpha|v2-updated" {
					return fmt.Errorf("node %s sai hạng 1 BM25 sau khi boost baseline[0]: got payload=%q (physical docid=%s), want %q",
						nodeName, topPayload, page.Results[0].Docid.String(), "baseline-alpha|v2-updated")
				}
				if page.Results[0].Docid.Sign() <= 0 || page.Results[0].Docid.Int64() > 4 {
					return fmt.Errorf("node %s physical docid của baseline[0] nằm ngoài miền [1..4]: got %s",
						nodeName, page.Results[0].Docid.String())
				}
				return nil
			},
		},
	}

	fmt.Println("\n🔍 KẾT QUẢ CHI TIẾT 3 BÀI TEST TÌM KIẾM XAPIAN TRÊN CỤM NODE:")
	for _, q := range queries {
		qCall, pErr := parsedABI.Pack("queryCluster", q.query, q.sortSlot, q.sortAsc, uint64(50))
		if pErr != nil {
			return pErr
		}
		var refQueryRaw []byte
		var refPage SearchResultsPage
		for idx, n := range readNodes {
			raw, cErr := ethCallRawWithRetry(n, caller, contractAddr, qCall)
			if cErr != nil {
				return cErr
			}
			if idx == 0 {
				refQueryRaw = raw
			} else if !bytes.Equal(refQueryRaw, raw) {
				return fmt.Errorf("kết quả queryCluster(%s) trên node %s LỆCH byte-for-byte so với node %s",
					q.name, n.Name, readNodes[0].Name)
			}

			var unpacked struct {
				Page SearchResultsPage
			}
			if uErr := parsedABI.UnpackIntoInterface(&unpacked, "queryCluster", raw); uErr != nil {
				return fmt.Errorf("unpack queryCluster(%s) trên node %s lỗi: %w", q.name, n.Name, uErr)
			}
			if idx == 0 {
				refPage = unpacked.Page
			}
			if int(unpacked.Page.Total.Int64()) != q.wantTotal {
				return fmt.Errorf("node %s sai tổng số kết quả %s: got %s, want %d",
					n.Name, q.name, unpacked.Page.Total.String(), q.wantTotal)
			}
			if chkErr := q.checkFn(n.Name, unpacked.Page); chkErr != nil {
				return chkErr
			}
		}

		// Với truy vấn thứ 3 ("metanode", sortSlot=-1, sortAsc=false), kiểm chứng thêm keccak256(refQueryRaw) == anchoredSearchDigest!
		if q.name == "BM25 Relevance Boost Ranking" {
			expectedDigest := crypto.Keccak256Hash(refQueryRaw)
			if expectedDigest != common.Hash(anchoredDigest) {
				return fmt.Errorf("anchoredSearchDigest trên on-chain (0x%x) không khớp keccak256 kết quả eth_call (%s)",
					anchoredDigest, expectedDigest.Hex())
			}
		}

		fmt.Printf("\n   ✅ [%s] Khớp 100%% byte-for-byte & đúng đặc tả trên cả %d node (query=%q, sortSlot=%d, sortAsc=%v, total=%d):\n",
			q.name, len(readNodes), q.query, q.sortSlot, q.sortAsc, q.wantTotal)
		for rIdx, item := range refPage.Results {
			payloadStr := string(item.Data)
			meta := payloadToMeta[payloadStr]
			fmt.Printf("      #%d | PhysDocID=%-2s | BM25=%-3s%% | Rank=%-2s | Slot0=%-4s | Data=%-30q | VirtualID=%s\n",
				rIdx+1,
				item.Docid.String(),
				item.Percent.String(),
				item.Rank.String(),
				meta.ScoreVal,
				payloadStr,
				meta.VirtualHex,
			)
		}
	}

	for idx := 1; idx < len(readNodes); idx++ {
		raw, cErr := ethCallRawWithRetry(readNodes[idx], caller, contractAddr, trackedCall)
		if cErr != nil {
			return cErr
		}
		if !bytes.Equal(refTrackedRaw, raw) {
			return fmt.Errorf("getTrackedIds trên node %s lệch so với node %s", readNodes[idx].Name, readNodes[0].Name)
		}
	}

	fmt.Println("\n==========================================================")
	fmt.Printf("🎉 HOÀN TẤT: Toàn bộ %d node đồng bộ BlockHash, StateRoot, Xapian & Revert Guard khớp 100%%!\n", len(readNodes))
	fmt.Println("==========================================================")
	return nil
}

func main() {
	cfgPath := flag.String("config", "", "Đường dẫn tới file config.json")
	flag.Parse()

	if err := RunTest(*cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "LỖI KIỂM THỬ 38-xapian-multinode-sync: %v\n", err)
		os.Exit(1)
	}
}
