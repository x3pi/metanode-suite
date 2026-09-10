package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"google.golang.org/protobuf/proto"
	"math/big"
	"os"
	"strings"
	"testing"
	"tool-test/pkg/bls"
	cm "tool-test/pkg/common"
	pb "tool-test/pkg/proto"
)

func TestSponsoredSetCodeTCP(t *testing.T) {
	if os.Getenv("RUN_TCP_EIP7702") != "1" {
		t.Skip("set RUN_TCP_EIP7702=1 to submit transactions to the configured chain")
	}
	if err := RunTest("../config.json", "data.json"); err != nil {
		t.Fatal(err)
	}
}

// TestDeviceKeyWire creates an offline fixture for the canonical node decoder.
func TestDeviceKeyWire(t *testing.T) {
	relayer, err := crypto.HexToECDSA(strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	authority, err := crypto.HexToECDSA(strings.Repeat("2", 64))
	if err != nil {
		t.Fatal(err)
	}
	key := bls.NewKeyPair(common.LeftPadBytes([]byte{3}, 32))
	delegate := common.HexToAddress("0x7702")
	auth, err := types.SignSetCode(authority, types.SetCodeAuthorization{ChainID: *uint256.NewInt(991), Address: delegate, Nonce: 8})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := types.SignNewTx(relayer, types.NewPragueSigner(big.NewInt(991)), &types.SetCodeTx{
		ChainID: uint256.NewInt(991), Nonce: 7, GasTipCap: uint256.NewInt(1), GasFeeCap: uint256.NewInt(20), Gas: 250000,
		To: crypto.PubkeyToAddress(authority.PublicKey), Value: uint256.NewInt(0), Data: []byte{1, 2, 3, 4}, AuthList: []types.SetCodeAuthorization{auth},
	})
	if err != nil {
		t.Fatal(err)
	}
	rawKey := common.LeftPadBytes([]byte{4}, 32)
	lastKey := common.HexToHash("0x05")
	wire, hash, err := encodeTransaction(tx, key, lastKey, rawKey)
	if err != nil {
		t.Fatal(err)
	}
	var wrapper pb.TransactionWithDeviceKey
	if err = proto.Unmarshal(wire, &wrapper); err != nil {
		t.Fatal(err)
	}
	p := wrapper.Transaction
	if p == nil || p.Type != 4 || !bytes.Equal(wrapper.DeviceKey, rawKey) || !bytes.Equal(p.NewDeviceKey, crypto.Keccak256(rawKey)) || !bytes.Equal(p.LastDeviceKey, lastKey.Bytes()) {
		t.Fatal("device key wrapper lost transaction fields")
	}
	if !bls.VerifySign(key.PublicKey(), cm.SignFromBytes(p.Sign), hash.Bytes()) {
		t.Fatal("invalid BLS signature")
	}
	_, alteredHash, err := encodeTransaction(tx, key, common.HexToHash("0x06"), rawKey)
	if err != nil {
		t.Fatal(err)
	}
	if hash == alteredHash || bls.VerifySign(key.PublicKey(), cm.SignFromBytes(p.Sign), alteredHash.Bytes()) {
		t.Fatal("device key not committed by signature")
	}
	if _, _, err := encodeTransaction(tx, key, lastKey, nil); err == nil {
		t.Fatal("accepted missing device key")
	}
	if _, _, err := encodeTransaction(nil, key, lastKey, rawKey); err == nil {
		t.Fatal("accepted nil transaction")
	}
	// Optional export contains deterministic test keys only, never config secrets.
	if path := os.Getenv("TCP_SETCODE_FIXTURE"); path != "" {
		fixture := map[string]string{"wire": hex.EncodeToString(wire), "hash": hash.Hex(), "eth_hash": tx.Hash().Hex(), "bls_public_key": hex.EncodeToString(key.BytesPublicKey()), "authority": tx.To().Hex()}
		data, err := json.Marshal(fixture)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReceiptTotalFee(t *testing.T) {
	if got := receiptTotalFee(45000, 20000000000).String(); got != "900000000000000" {
		t.Fatalf("reported transaction total fee = %s", got)
	}
	// Multiplication must not overflow uint64 before conversion to big.Int.
	if got := receiptTotalFee(2, 1<<63).String(); got != "18446744073709551616" {
		t.Fatalf("large total fee = %s", got)
	}
}
