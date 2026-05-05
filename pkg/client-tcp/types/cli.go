package types

import (
	"tool-test/pkg/types"

	"github.com/ethereum/go-ethereum/common"
)

type Cli interface {
	Start()
	Stop()
	PrintCommands()
	PrintMessage(string, string)
	SendTransaction() error
	CreateAccount()
	ChangeAccount()
	AccountState(common.Address) (types.AccountState, error)
	ReadInput() string
	ReadInputAddress() common.Address
}
