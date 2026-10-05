package transaction

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	pb "tool-test/pkg/proto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecpProto_GoldenVector(t *testing.T) {
	key, err := crypto.HexToECDSA("4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318")
	require.NoError(t, err)
	from := crypto.PubkeyToAddress(key.PublicKey)
	require.Equal(t, "0x2c7536E3605D9C16a7a3D7b1898e529396a65c23", from.Hex())

	tx := &Transaction{proto: &pb.Transaction{
		FromAddress: from.Bytes(),
		ToAddress:   common.HexToAddress("0x2222222222222222222222222222222222222222").Bytes(),
		Amount:      []byte{0x03, 0xe8},
		MaxGas:      21000,
		MaxGasPrice: 1000,
		Data:        []byte{0xde, 0xad},
		Nonce:       []byte{0, 0, 0, 0, 0, 0, 0, 7},
		ChainID:     991,
		Type:        0xFF,
	}}

	assert.Equal(t, "0xc307992ce6856ac915c04ebaaa30d946db6f66712b0ef68d0ab102eb7072f14e", tx.SigningHash().Hex())

	require.NoError(t, tx.SignSecpProto(key))
	assert.Equal(t, "e164ccec12532da3a83707698000f3174a8cec11711c14fb82960c303ae1195b", common.Bytes2Hex(tx.proto.R))
	assert.Equal(t, "23bbeda7b9c266495964d3c6d5045c4628eb1d2ba9809447d79260f78a5b3606", common.Bytes2Hex(tx.proto.S))
	assert.Equal(t, "01", common.Bytes2Hex(tx.proto.V))
	assert.Equal(t, "0xd9ea692bf0d57ba20c81b4090e30cad303c0a584c4d382603ac413727025290f", tx.Hash().Hex())
	assert.True(t, tx.ValidSecpProtoSign())
}
