package models

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type TxOptions struct {
	Amount      *big.Int
	Related     []common.Address
	MaxGas      uint64
	MaxGasPrice uint64
	MaxTimeUse  uint64
}
