package main

import (
	"bytes"
	"crypto/rand"
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
	"github.com/holiman/uint256"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"tool-test/pkg/bls"
	clienttcp "tool-test/pkg/client-tcp"
	"tool-test/pkg/client-tcp/command"
	tcpconfig "tool-test/pkg/client-tcp/config"
	pb "tool-test/pkg/proto"
)

type ChainConfig struct {
	BLSPrivateKey string   `json:"bls_private_key"`
	PrivateKeys   []string `json:"private_keys"`
	TCPURL        string   `json:"tcp_url"`
	ChainID       uint64   `json:"chain_id"`
	ParentAddress string   `json:"parent_address"`
	Version       string   `json:"version"`
}

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
// The suite's older generated messages preserve these as unknown fields.
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
	// Sign is the BLS signature over the complete native protobuf hash.
	// R/S/V retain the outer EIP-7702 signature for Ethereum conversion.
	p.Sign = bls.Sign(blsKey.PrivateKey(), hash.Bytes()).Bytes()
	payload, err := proto.Marshal(&pb.TransactionWithDeviceKey{Transaction: p, DeviceKey: rawDeviceKey})
	return payload, hash, err
}

// receiptTotalFee converts the native receipt's per-gas price into total wei.
func receiptTotalFee(gasUsed, gasPrice uint64) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), new(big.Int).SetUint64(gasPrice))
}

// getDeviceKey matches the echoed transaction hash and bounds the wait.
func getDeviceKey(cli *clienttcp.Client, hash common.Hash) (common.Hash, error) {
	cc := cli.GetClientContext()
	if err := cc.MessageSender.SendBytes(cc.ConnectionsManager.ParentConnection(), "GetDeviceKey", hash.Bytes()); err != nil {
		return common.Hash{}, err
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case result, ok := <-cli.GetDeviceKeyChan():
			if !ok {
				return common.Hash{}, fmt.Errorf("device key channel closed")
			}
			if !bytes.Equal(result.TransactionHash, hash.Bytes()) {
				continue
			}
			if len(result.LastDeviceKeyFromServer) != 32 {
				return common.Hash{}, fmt.Errorf("invalid device key length")
			}
			return common.BytesToHash(result.LastDeviceKeyFromServer), nil
		case <-timer.C:
			return common.Hash{}, fmt.Errorf("timeout reading device key for %s", hash)
		}
	}
}

func RunTest(configPath, dataPath string) error {
	// Read exact paths; do not silently fall back to another network config.
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var shared ChainConfig
	if err = json.Unmarshal(raw, &shared); err != nil {
		return err
	}
	if len(shared.PrivateKeys) < 2 {
		return fmt.Errorf("config requires private_keys[0] (from) and private_keys[1] (authority)")
	}
	keyBytes, err := hexutil.Decode("0x" + strings.TrimPrefix(shared.BLSPrivateKey, "0x"))
	if err != nil || len(keyBytes) != 32 {
		return fmt.Errorf("bls_private_key must be a 32-byte hex key")
	}
	blsKey := bls.NewKeyPair(keyBytes)
	if blsKey == nil {
		return fmt.Errorf("invalid bls_private_key")
	}
	version := shared.Version
	if version == "" {
		version = "0.0.1.0"
	}
	cfg := tcpconfig.ClientConfig{
		PrivateKey_:             strings.TrimPrefix(shared.BLSPrivateKey, "0x"),
		ParentConnectionAddress: shared.TCPURL, ParentConnectionType: "client",
		ParentAddress: shared.ParentAddress, ChainId: shared.ChainID, Version_: version,
	}
	raw, err = os.ReadFile(dataPath)
	if err != nil {
		return err
	}
	var d Data
	if err = json.Unmarshal(raw, &d); err != nil {
		return err
	}
	if cfg.ChainId == 0 || cfg.GetParentConnectionAddress() == "" {
		return fmt.Errorf("chain_id and tcp_url are required")
	}
	if !common.IsHexAddress(d.Delegate) || common.HexToAddress(d.Delegate) == (common.Address{}) {
		return fmt.Errorf("delegate must be a nonzero address")
	}
	if d.Gas < 46000 {
		return fmt.Errorf("gas must be at least 46000")
	}
	if d.GasTipCap == 0 || d.GasFeeCap < d.GasTipCap {
		return fmt.Errorf("gas_tip_cap must be positive and gas_fee_cap must be >= gas_tip_cap (wei)")
	}
	relayerKey, err := crypto.HexToECDSA(strings.TrimPrefix(shared.PrivateKeys[0], "0x"))
	if err != nil {
		return fmt.Errorf("private_keys[0]: %w", err)
	}
	authorityKey, err := crypto.HexToECDSA(strings.TrimPrefix(shared.PrivateKeys[1], "0x"))
	if err != nil {
		return fmt.Errorf("private_keys[1]: %w", err)
	}
	relayer := crypto.PubkeyToAddress(relayerKey.PublicKey)
	authority := crypto.PubkeyToAddress(authorityKey.PublicKey)
	if relayer == authority {
		return fmt.Errorf("sponsored test requires distinct relayer and authority")
	}
	input, err := hexutil.Decode(d.Input)
	if err != nil {
		return fmt.Errorf("input_data: %w", err)
	}
	var expectedReturn []byte
	if d.ExpectedReturn != nil {
		expectedReturn, err = hexutil.Decode(*d.ExpectedReturn)
		if err != nil {
			return fmt.Errorf("expected_return: %w", err)
		}
	}
	fmt.Printf("Connecting to TCP: %s\n", shared.TCPURL)
	cli, err := clienttcp.NewClient(&cfg)
	if err != nil {
		return err
	}
	defer cli.Close()
	tcpChainID, err := cli.ChainGetChainId()
	if err != nil {
		return err
	}
	if tcpChainID != cfg.ChainId {
		return fmt.Errorf("TCP/config chain ID mismatch")
	}
	chainID := new(big.Int).SetUint64(tcpChainID)
	account, err := cli.GetAccountState(relayer, 10*time.Second)
	if err != nil {
		return fmt.Errorf("read account 0: %w", err)
	}
	if account == nil || !bytes.Equal(account.PublicKeyBls(), blsKey.BytesPublicKey()) {
		return fmt.Errorf("account 0 must have the public key for bls_private_key registered before this test")
	}
	authorityBefore, err := cli.GetAccountState(authority, 10*time.Second)
	if err != nil {
		return fmt.Errorf("read authority: %w", err)
	}
	if authorityBefore == nil {
		return fmt.Errorf("missing authority account state")
	}
	senderNonce, authNonce := account.Nonce(), authorityBefore.Nonce()
	if senderNonce == ^uint64(0) || authNonce == ^uint64(0) {
		return fmt.Errorf("nonce overflow")
	}
	balanceBefore := new(big.Int).Set(authorityBefore.Balance())
	senderBalanceBefore := new(big.Int).Set(account.Balance())
	maxCost := new(big.Int).Mul(new(big.Int).SetUint64(d.Gas), new(big.Int).SetUint64(d.GasFeeCap))
	if senderBalanceBefore.Cmp(maxCost) < 0 {
		return fmt.Errorf("account 0 balance is below gas * gas_fee_cap")
	}
	lastDeviceKey, err := getDeviceKey(cli, account.LastHash())
	if err != nil {
		return err
	}
	if account.DeviceKey() != (common.Hash{}) && crypto.Keccak256Hash(lastDeviceKey.Bytes()) != account.DeviceKey() {
		fmt.Printf("⚠️ Warning: previous device key (%s -> %s) does not match account 0 state (%s), continuing like client-tcp\n",
			lastDeviceKey.Hex(), crypto.Keccak256Hash(lastDeviceKey.Bytes()).Hex(), account.DeviceKey().Hex())
	}
	rawDeviceKey := make([]byte, 32)
	if _, err = rand.Read(rawDeviceKey); err != nil {
		return err
	}
	newDeviceKey := crypto.Keccak256Hash(rawDeviceKey)
	if newDeviceKey == account.DeviceKey() {
		return fmt.Errorf("new device key must differ from current key")
	}
	tip := new(big.Int).SetUint64(d.GasTipCap)
	fee := new(big.Int).SetUint64(d.GasFeeCap)
	delegate := common.HexToAddress(d.Delegate)
	auth, err := types.SignSetCode(authorityKey, types.SetCodeAuthorization{
		ChainID: *uint256.NewInt(cfg.ChainId), Address: delegate, Nonce: authNonce,
	})
	if err != nil {
		return err
	}
	tx, err := types.SignNewTx(relayerKey, types.NewPragueSigner(chainID), &types.SetCodeTx{
		ChainID: uint256.NewInt(cfg.ChainId), Nonce: senderNonce, GasTipCap: uint256.MustFromBig(tip),
		GasFeeCap: uint256.MustFromBig(fee), Gas: d.Gas, To: authority, Value: uint256.NewInt(0),
		Data: input, AuthList: []types.SetCodeAuthorization{auth},
	})
	if err != nil {
		return err
	}
	payload, hash, err := encodeTransaction(tx, blsKey, lastDeviceKey, rawDeviceKey)
	if err != nil {
		return err
	}
	fmt.Printf("TCP: %s; transaction signer: BLS; from: private_keys[0]\n", shared.TCPURL)
	fmt.Printf("Relayer: %s; authority: %s; delegate: %s\n", relayer, authority, delegate)
	fmt.Printf("TCP protobuf hash: %s; Ethereum hash: %s\n", hash, tx.Hash())
	cc := cli.GetClientContext()
	if err = cc.MessageSender.SendBytes(cc.ConnectionsManager.ParentConnection(), command.SendTransactionWithDeviceKey, payload); err != nil {
		return err
	}
	receipt, err := cli.FindReceiptByHash(hash)
	if err != nil {
		return fmt.Errorf("TCP receipt %s: %w", hash, err)
	}
	if receipt == nil {
		return fmt.Errorf("missing TCP receipt")
	}
	if receipt.TransactionHash() != hash || receipt.FromAddress() != relayer || receipt.ToAddress() != authority {
		return fmt.Errorf("TCP receipt hash/from/to mismatch")
	}
	if receipt.Status() != pb.RECEIPT_STATUS_RETURNED && receipt.Status() != pb.RECEIPT_STATUS_HALTED {
		return fmt.Errorf("TCP transaction failed: %s, return=0x%x", receipt.Status(), receipt.Return())
	}
	if d.ExpectedReturn != nil && !bytes.Equal(receipt.Return(), expectedReturn) {
		return fmt.Errorf("receipt return mismatch: got 0x%x, want 0x%x", receipt.Return(), expectedReturn)
	}
	if receipt.GasUsed() == 0 || receipt.GasUsed() > d.Gas || receipt.GasFee() == 0 {
		return fmt.Errorf("invalid receipt gas accounting: gas=%d gas_price=%d", receipt.GasUsed(), receipt.GasFee())
	}
	// Verify the committed delegation using CodeHash from TCP AccountState.
	// The polling deadline bounds this test only; it does not control consensus.
	expectedCode := append([]byte{0xef, 0x01, 0x00}, delegate.Bytes()...)
	expectedHash := crypto.Keccak256Hash(expectedCode)
	deadline := time.Now().Add(60 * time.Second)
	for {
		authorityAfter, err := cli.GetAccountState(authority, 10*time.Second)
		if err != nil {
			return fmt.Errorf("verify authority over TCP: %w", err)
		}
		senderAfter, err := cli.GetAccountState(relayer, 10*time.Second)
		if err != nil {
			return fmt.Errorf("verify relayer over TCP: %w", err)
		}
		if authorityAfter == nil || senderAfter == nil {
			return fmt.Errorf("missing account state during verification")
		}
		sc := authorityAfter.SmartContractState()
		if sc != nil && sc.CodeHash() == expectedHash && authorityAfter.Nonce() == authNonce+1 && senderAfter.Nonce() == senderNonce+1 && senderAfter.DeviceKey() == newDeviceKey {
			delegateState, err := cli.GetAccountState(delegate, 10*time.Second)
			if err != nil {
				return fmt.Errorf("read delegate over TCP: %w", err)
			}
			if delegateState == nil {
				return fmt.Errorf("missing delegate account state")
			}
			delegateSC := delegateState.SmartContractState()
			emptyDelegate := delegateSC == nil || delegateSC.CodeHash() == (common.Hash{}) || delegateSC.CodeHash() == types.EmptyCodeHash
			if emptyDelegate && len(input) == 0 && balanceBefore.Cmp(authorityAfter.Balance()) != 0 {
				return fmt.Errorf("authority balance changed in sponsorship-only test")
			}
			if emptyDelegate && len(input) == 0 {
				spent := new(big.Int).Sub(senderBalanceBefore, senderAfter.Balance())
				expectedFee := receiptTotalFee(receipt.GasUsed(), receipt.GasFee())
				if spent.Cmp(expectedFee) != 0 {
					return fmt.Errorf("relayer balance delta %s does not match total fee %s (gas used=%d, gas price=%d wei; use isolated non-validator test accounts)", spent, expectedFee, receipt.GasUsed(), receipt.GasFee())
				}
			}
			persistedKey, err := getDeviceKey(cli, hash)
			if err != nil {
				return err
			}
			if !bytes.Equal(persistedKey.Bytes(), rawDeviceKey) {
				return fmt.Errorf("stored device key does not match submitted key")
			}
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("TCP state/device key verification timed out: expected delegation hash %s and nonces authority=%d relayer=%d; got authority=%d relayer=%d",
				expectedHash, authNonce+1, senderNonce+1, authorityAfter.Nonce(), senderAfter.Nonce())
		}
		time.Sleep(250 * time.Millisecond)
	}
	fmt.Printf("PASS: TCP sponsored EIP-7702, status=%s gas=%d, delegation, nonces, device key and applicable fee/balance checks verified\n", receipt.Status(), receipt.GasUsed())
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
