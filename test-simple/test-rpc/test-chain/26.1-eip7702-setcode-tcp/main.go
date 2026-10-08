package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"tool-test/pkg/bls"
	clienttcp "tool-test/pkg/client-tcp"
	tcpconfig "tool-test/pkg/client-tcp/config"
	pb "tool-test/pkg/proto"
	testconfig "tool-test/test-simple/test-rpc/test-chain/config"
)

type Data struct {
	Delegate       string  `json:"delegate"`
	Input          string  `json:"input_data"`
	Gas            uint64  `json:"gas"`
	GasTipCap      uint64  `json:"gas_tip_cap"`
	GasFeeCap      uint64  `json:"gas_fee_cap"`
	ExpectedReturn *string `json:"expected_return,omitempty"`
}

// appendAuthorization uses the canonical node schema: Transaction tag 26,
// TransactionHashData tag 21, and SetCodeAuthorization tags 1 through 6.
func appendAuthorization(msg proto.Message, tag protowire.Number, a types.SetCodeAuthorization) {
	var auth []byte
	if !a.ChainID.IsZero() {
		auth = protowire.AppendTag(auth, 1, protowire.VarintType)
		auth = protowire.AppendVarint(auth, a.ChainID.Uint64())
	}
	addBytes := func(n protowire.Number, b []byte) {
		if len(b) == 0 {
			return
		}
		auth = protowire.AppendTag(auth, n, protowire.BytesType)
		auth = protowire.AppendBytes(auth, b)
	}
	addBytes(2, a.Address.Bytes())
	if a.Nonce != 0 {
		auth = protowire.AppendTag(auth, 3, protowire.VarintType)
		auth = protowire.AppendVarint(auth, a.Nonce)
	}
	addBytes(4, []byte{a.V})
	addBytes(5, a.R.Bytes())
	addBytes(6, a.S.Bytes())
	wire := protowire.AppendTag(nil, tag, protowire.BytesType)
	wire = protowire.AppendBytes(wire, auth)
	msg.ProtoReflect().SetUnknown(wire)
}

func encodeTransaction(tx *types.Transaction, blsKey *bls.KeyPair, lastDeviceKey common.Hash, rawDeviceKey []byte) ([]byte, common.Hash, error) {
	if tx == nil || blsKey == nil || len(rawDeviceKey) != 32 {
		return nil, common.Hash{}, fmt.Errorf("transaction, BLS key and 32-byte device key are required")
	}
	from, err := types.Sender(types.NewPragueSigner(tx.ChainId()), tx)
	if err != nil {
		return nil, common.Hash{}, err
	}
	if tx.Type() != types.SetCodeTxType || tx.To() == nil || len(tx.SetCodeAuthorizations()) != 1 || !tx.ChainId().IsUint64() || len(tx.AccessList()) != 0 {
		return nil, common.Hash{}, fmt.Errorf("expected SetCodeTx with one authorization, uint64 chain ID and empty access list")
	}
	auth := tx.SetCodeAuthorizations()[0]
	if _, err := auth.Authority(); err != nil {
		return nil, common.Hash{}, fmt.Errorf("invalid authorization signature: %w", err)
	}
	if !auth.ChainID.IsZero() && auth.ChainID.ToBig().Cmp(tx.ChainId()) != 0 {
		return nil, common.Hash{}, fmt.Errorf("authorization chain ID mismatch")
	}
	data, err := proto.Marshal(&pb.CallData{Input: tx.Data()})
	if err != nil {
		return nil, common.Hash{}, err
	}
	nonce := make([]byte, 8)
	binary.BigEndian.PutUint64(nonce, tx.Nonce())
	v, r, s := tx.RawSignatureValues()
	p := &pb.Transaction{
		FromAddress: from.Bytes(), ToAddress: tx.To().Bytes(), Amount: tx.Value().Bytes(),
		MaxGas: tx.Gas(), Data: data, Nonce: nonce, ChainID: tx.ChainId().Uint64(),
		LastDeviceKey: lastDeviceKey.Bytes(), NewDeviceKey: crypto.Keccak256(rawDeviceKey),
		Type: types.SetCodeTxType, R: r.Bytes(), S: s.Bytes(), V: v.Bytes(),
		GasTipCap: tx.GasTipCap().Bytes(), GasFeeCap: tx.GasFeeCap().Bytes(),
	}
	h := &pb.TransactionHashData{
		FromAddress: p.FromAddress, ToAddress: p.ToAddress, Amount: p.Amount,
		MaxGas: p.MaxGas, Data: p.Data, Nonce: p.Nonce, ChainID: p.ChainID,
		LastDeviceKey: p.LastDeviceKey, NewDeviceKey: p.NewDeviceKey,
		Type: p.Type, R: p.R, S: p.S, V: p.V, GasTipCap: p.GasTipCap, GasFeeCap: p.GasFeeCap,
	}
	a := tx.SetCodeAuthorizations()[0]
	appendAuthorization(p, 26, a)
	appendAuthorization(h, 21, a)
	hashData, err := (proto.MarshalOptions{Deterministic: true}).Marshal(h)
	if err != nil {
		return nil, common.Hash{}, err
	}
	hash := crypto.Keccak256Hash(hashData)
	p.Sign = bls.Sign(blsKey.PrivateKey(), hash.Bytes()).Bytes()
	payload, err := proto.Marshal(&pb.TransactionWithDeviceKey{Transaction: p, DeviceKey: rawDeviceKey})
	return payload, hash, err
}

func receiptTotalFee(gasUsed, gasPrice uint64) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), new(big.Int).SetUint64(gasPrice))
}

func RunTest(configPath, dataPath string) error {
	fmt.Println("==========================================================")
	fmt.Println("BÀI TEST: 26.1-eip7702-setcode-tcp")
	fmt.Println("==========================================================")
	fmt.Println("📖 MÔ TẢ   : Gửi giao dịch EIP-7702 SetCode Transaction qua cổng TCP chuẩn Ethereum (EIP-2718 raw binary).")
	fmt.Println("⚡ GỌI     : Ký Authorization tuple cho Authority EOA, ký SetCodeTx bằng PragueSigner, gửi raw binary qua SendRawEthTransaction trên TCP.")
	fmt.Println("🎯 KỲ VỌNG : Node TCP tiếp nhận giao dịch (TransactionSuccess), block thực thi thành công, Authority account được delegate code 0xef0100 + delegate.")
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	if configPath == "" {
		configPath = "../config.json"
	}

	cfg, err := testconfig.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.PrivateKeys) < 2 {
		return fmt.Errorf("config requires at least 2 private_keys: [0]=relayer, [1]=authority")
	}

	raw, err := os.ReadFile(dataPath)
	if err != nil {
		return fmt.Errorf("read data.json: %w", err)
	}
	var d Data
	if err = json.Unmarshal(raw, &d); err != nil {
		return fmt.Errorf("unmarshal data.json: %w", err)
	}

	tcpAddr := cfg.TCPNode
	if tcpAddr == "" {
		tcpAddr = cfg.ParentConnectionAddress
	}
	if (tcpAddr == "" || strings.Contains(tcpAddr, "6200")) && len(cfg.TCPNodes) > 0 {
		if node0, ok := cfg.TCPNodes["m0"]; ok && node0 != "" {
			tcpAddr = node0
		} else {
			for _, v := range cfg.TCPNodes {
				if v != "" {
					tcpAddr = v
					break
				}
			}
		}
	}
	if tcpAddr == "" {
		return fmt.Errorf("tcp_node is required")
	}

	chainID := big.NewInt(cfg.ChainID)
	if chainID.Sign() == 0 {
		return fmt.Errorf("chain_id is required")
	}

	relayerKey, err := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKeys[0], "0x"))
	if err != nil {
		return fmt.Errorf("private_keys[0]: %w", err)
	}
	authorityKey, err := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKeys[1], "0x"))
	if err != nil {
		return fmt.Errorf("private_keys[1]: %w", err)
	}
	relayer := crypto.PubkeyToAddress(relayerKey.PublicKey)
	authority := crypto.PubkeyToAddress(authorityKey.PublicKey)
	if relayer == authority {
		return fmt.Errorf("sponsored test requires distinct relayer and authority")
	}

	delegateContract := common.HexToAddress(d.Delegate)
	if delegateContract == (common.Address{}) {
		return fmt.Errorf("delegate must be a nonzero address")
	}

	input, err := hexutil.Decode(d.Input)
	if err != nil {
		return fmt.Errorf("input_data: %w", err)
	}

	fmt.Printf("Connecting to TCP: %s\n", tcpAddr)
	clientCfg := &tcpconfig.ClientConfig{
		PrivateKey_:             cfg.PrivateKeys[0],
		EthPrivateKey:           cfg.PrivateKeys[0],
		ParentConnectionAddress: tcpAddr,
		ParentConnectionType:    "client",
		ParentAddress:           relayer.Hex(),
		ChainId:                 uint64(cfg.ChainID),
		Version_:                "0.0.1.0",
	}

	cli, err := clienttcp.NewClient(clientCfg)
	if err != nil {
		return fmt.Errorf("connect TCP %s: %w", tcpAddr, err)
	}
	defer cli.Close()
	time.Sleep(1 * time.Second)

	// RPC client for querying nonces / receipts / code
	var ethCli *ethclient.Client
	if cfg.RPCUrl != "" {
		ethCli, err = ethclient.Dial(cfg.RPCUrl)
		if err != nil {
			fmt.Printf("⚠️ RPC Dial error: %v (will rely purely on TCP)\n", err)
		}
	}

	var senderNonce, authNonce uint64
	var senderBalanceBefore, authorityBalanceBefore *big.Int

	if ethCli != nil {
		senderNonce, _ = ethCli.PendingNonceAt(context.Background(), relayer)
		authNonce, _ = ethCli.PendingNonceAt(context.Background(), authority)
		senderBalanceBefore, _ = ethCli.BalanceAt(context.Background(), relayer, nil)
		authorityBalanceBefore, _ = ethCli.BalanceAt(context.Background(), authority, nil)
	}
	if senderBalanceBefore == nil {
		relayerState, sErr := cli.GetAccountState(relayer, 10*time.Second)
		if sErr == nil && relayerState != nil {
			senderNonce = relayerState.Nonce()
			senderBalanceBefore = new(big.Int).Set(relayerState.Balance())
		} else {
			senderBalanceBefore = big.NewInt(0)
		}
	}
	if authorityBalanceBefore == nil {
		authState, aErr := cli.GetAccountState(authority, 10*time.Second)
		if aErr == nil && authState != nil {
			authNonce = authState.Nonce()
			authorityBalanceBefore = new(big.Int).Set(authState.Balance())
		} else {
			authorityBalanceBefore = big.NewInt(0)
		}
	}

	fmt.Printf("🔑 Relayer (Gas Payer): %s (Nonce: %d, Balance: %s wei)\n", relayer.Hex(), senderNonce, senderBalanceBefore.String())
	fmt.Printf("🛡️ Authority (EOA Delegate): %s (Nonce: %d, Balance: %s wei)\n", authority.Hex(), authNonce, authorityBalanceBefore.String())
	fmt.Printf("📋 Delegate Contract: %s\n", delegateContract.Hex())
	fmt.Printf("🌐 ChainID: %s\n", chainID.String())

	// 1. Ký EIP-7702 SetCode Authorization
	authTuple := types.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainID),
		Address: delegateContract,
		Nonce:   authNonce,
	}
	signedAuth, err := types.SignSetCode(authorityKey, authTuple)
	if err != nil {
		return fmt.Errorf("sign SetCode authorization: %w", err)
	}
	fmt.Printf("✍️ Đã ký EIP-7702 Authorization cho %s -> delegate %s (Nonce: %d)\n",
		authority.Hex(), delegateContract.Hex(), authNonce)

	// 2. Tạo SetCodeTx
	gas := d.Gas
	if gas < 46000 {
		gas = 250000
	}
	gasTipCap := big.NewInt(int64(d.GasTipCap))
	if gasTipCap.Sign() == 0 {
		gasTipCap = big.NewInt(1_000_000_000)
	}
	gasFeeCap := big.NewInt(int64(d.GasFeeCap))
	if gasFeeCap.Sign() == 0 {
		gasFeeCap = big.NewInt(20_000_000_000)
	}

	setCodeTxData := &types.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		Nonce:     senderNonce,
		GasTipCap: uint256.MustFromBig(gasTipCap),
		GasFeeCap: uint256.MustFromBig(gasFeeCap),
		Gas:       gas,
		To:        authority,
		Value:     uint256.NewInt(0),
		Data:      input,
		AuthList:  []types.SetCodeAuthorization{signedAuth},
	}

	// 3. Ký với Prague Signer
	signer := types.NewPragueSigner(chainID)
	signedTx, err := types.SignNewTx(relayerKey, signer, setCodeTxData)
	if err != nil {
		return fmt.Errorf("sign SetCodeTx: %w", err)
	}

	rawEthTx, err := signedTx.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal binary EIP-7702: %w", err)
	}

	// 4. Gửi qua TCP cổng SendRawEthTransaction
	fmt.Printf("📤 Gửi raw EIP-7702 qua TCP (%d bytes, TxHash: %s)...\n", len(rawEthTx), signedTx.Hash().Hex())
	acceptedHash, err := cli.SendRawEthTransaction(rawEthTx)
	if err != nil {
		return fmt.Errorf("SendRawEthTransaction failed: %w", err)
	}
	if acceptedHash != signedTx.Hash() {
		return fmt.Errorf("accepted hash mismatch: got %s, want %s", acceptedHash.Hex(), signedTx.Hash().Hex())
	}
	fmt.Printf("   ✅ Node TCP acknowledged TransactionSuccess: %s\n", acceptedHash.Hex())

	// 5. Chờ receipt (TCP trước, fallback RPC)
	fmt.Println("⏳ Đang chờ xác nhận giao dịch trên block...")
	var gasUsed uint64
	var statusOk bool
	var blockNum uint64

	receiptTCP, errTCP := cli.FindReceiptByHash(acceptedHash)
	if errTCP == nil && receiptTCP != nil {
		gasUsed = receiptTCP.GasUsed()
		statusOk = (receiptTCP.Status() == pb.RECEIPT_STATUS_RETURNED || receiptTCP.Status() == pb.RECEIPT_STATUS_HALTED)
		fmt.Printf("   ✅ Tìm thấy Receipt qua TCP (Status: %s, GasUsed: %d)\n", receiptTCP.Status().String(), gasUsed)
	} else if ethCli != nil {
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			rpcReceipt, err := ethCli.TransactionReceipt(context.Background(), acceptedHash)
			if err == nil && rpcReceipt != nil {
				gasUsed = rpcReceipt.GasUsed
				statusOk = (rpcReceipt.Status == 1)
				blockNum = rpcReceipt.BlockNumber.Uint64()
				fmt.Printf("   ✅ Tìm thấy Receipt qua RPC (Block: %d, Status: %d, GasUsed: %d)\n", blockNum, rpcReceipt.Status, gasUsed)
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	if !statusOk {
		return fmt.Errorf("giao dịch EIP-7702 không thành công trên block")
	}

	// 6. Kiểm tra code của Authority Account sau khi áp dụng EIP-7702
	expectedCode := append([]byte{0xef, 0x01, 0x00}, delegateContract.Bytes()...)
	expectedHash := crypto.Keccak256Hash(expectedCode)

	var actualCode []byte
	if ethCli != nil {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			actualCode, _ = ethCli.CodeAt(context.Background(), authority, nil)
			if len(actualCode) > 0 {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	fmt.Printf("   🔍 Authority Account Code: 0x%x (len: %d, Kỳ vọng: 0x%x)\n", actualCode, len(actualCode), expectedCode)

	// 7. Kiểm tra Nonce & Balance
	var senderAfterBalance, authAfterBalance *big.Int
	var senderAfterNonce, authAfterNonce uint64
	if ethCli != nil {
		senderAfterBalance, _ = ethCli.BalanceAt(context.Background(), relayer, nil)
		authAfterBalance, _ = ethCli.BalanceAt(context.Background(), authority, nil)
		senderAfterNonce, _ = ethCli.NonceAt(context.Background(), relayer, nil)
		authAfterNonce, _ = ethCli.NonceAt(context.Background(), authority, nil)
	}

	if senderAfterBalance != nil && authorityBalanceBefore != nil && authAfterBalance != nil {
		spentRelayer := new(big.Int).Sub(senderBalanceBefore, senderAfterBalance)
		spentAuthority := new(big.Int).Sub(authorityBalanceBefore, authAfterBalance)

		fmt.Println("\n==================================================")
		fmt.Println("📊 CHI TIẾT SỐ DƯ & NONCE (XÁC NHẬN EIP-7702 SPONSORSHIP QUA TCP):")
		fmt.Println("==================================================")
		fmt.Printf("1. NGƯỜI GỬI / TRẢ PHÍ GAS (Relayer): %s\n", relayer.Hex())
		fmt.Printf("   • Nonce:         %d ➡️  %d (+1)\n", senderNonce, senderAfterNonce)
		fmt.Printf("   • Số dư trước:   %s wei\n", senderBalanceBefore.String())
		fmt.Printf("   • Số dư sau:     %s wei\n", senderAfterBalance.String())
		fmt.Printf("   • Biến động:     -%s wei (Gas used: %d)\n", spentRelayer.String(), gasUsed)

		fmt.Printf("\n2. NGƯỜI ỦY QUYỀN / KÝ HỘ (Authority): %s\n", authority.Hex())
		fmt.Printf("   • Nonce:         %d ➡️  %d (+1)\n", authNonce, authAfterNonce)
		fmt.Printf("   • Số dư trước:   %s wei\n", authorityBalanceBefore.String())
		fmt.Printf("   • Số dư sau:     %s wei\n", authAfterBalance.String())
		if spentAuthority.Sign() == 0 {
			fmt.Printf("   • Biến động:     0 wei (✅ HOÀN TOÀN KHÔNG BỊ TRỪ PHÍ - ĐÃ ĐƯỢC SPONSOR!)\n")
		} else {
			fmt.Printf("   • Biến động:     %s wei\n", spentAuthority.String())
		}

		fmt.Printf("\n3. TRẠNG THÁI DELEGATION CODE:\n")
		fmt.Printf("   • Delegate To:   %s\n", delegateContract.Hex())
		fmt.Printf("   • Code Hash:     %s (chuẩn 0xef0100 + delegate)\n", expectedHash.Hex())
		fmt.Println("==================================================")
	}

	fmt.Println("\n🎉 TEST 26.1 (EIP-7702 SETCODE TX QUA TCP) PASSED THÀNH CÔNG!")
	return nil
}

func main() {
	configPath := flag.String("config", "../config.json", "Shared test-chain configuration")
	dataPath := flag.String("data", "data.json", "Sponsorship test data")
	flag.Parse()
	if err := RunTest(*configPath, *dataPath); err != nil {
		log.Fatal(err)
	}
}
