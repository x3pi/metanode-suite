package network

import (
	"errors"
	"fmt"
	"time"

	"tool-test/pkg/client-tcp/command"
	"tool-test/pkg/logger"
	pb "tool-test/pkg/proto"
	"tool-test/pkg/receipt"
	"tool-test/pkg/state"
	"tool-test/pkg/types"
	"tool-test/pkg/types/network"

	"github.com/ethereum/go-ethereum/common"
)

type TpsHandler struct {
	accountStateChan chan types.AccountState
	receiptChan      chan types.Receipt
}

func NewTpsHandler(
	accountStateChan chan types.AccountState,
	receiptChan chan types.Receipt,
) *TpsHandler {
	return &TpsHandler{
		accountStateChan: accountStateChan,
		receiptChan:      receiptChan,
	}
}

func (h *TpsHandler) HandleRequest(r network.Request) error {
	start := time.Now()
	logger.Trace("Handling command " + r.Message().Command())
	defer logger.Trace(
		"Handled command " + r.Message().Command() + "took " + time.Since(start).String(),
	)
	switch r.Message().Command() {
	case command.InitConnection:
		return h.handleInitConnection(r)
	case command.AccountState:
		return h.handleAccountState(r)
	case command.Nonce:
		return h.handleNonce(r)
	case command.Receipt:
		return h.handleReceipt(r)
	}

	return errors.New("command not found: " + r.Message().Command())
}

func (h *TpsHandler) handleInitConnection(request network.Request) (err error) {
	conn := request.Connection()
	initData := &pb.InitConnection{}
	err = request.Message().Unmarshal(initData)
	if err != nil {
		return err
	}
	address := common.BytesToAddress(initData.Address)
	logger.Debug(fmt.Sprintf(
		"init connection from %v type %v", address, initData.Type,
	))
	conn.Init(address, initData.Type)
	return nil
}

func (h *TpsHandler) handleAccountState(request network.Request) (err error) {
	accountState := &state.AccountState{}
	err = accountState.Unmarshal(request.Message().Body())
	if err != nil {
		return err
	}
	// logger.Debug(fmt.Sprintf("Receive Account state: \n%v", accountState))
	h.accountStateChan <- accountState
	return nil
}

func (h *TpsHandler) handleNonce(request network.Request) (err error) {
	accountState := &state.AccountState{}
	err = accountState.Unmarshal(request.Message().Body())
	if err != nil {
		return err
	}
	// logger.Debug(fmt.Sprintf("Receive Account state: \n%v", accountState))
	h.accountStateChan <- accountState
	return nil
}

func (h *TpsHandler) handleReceipt(request network.Request) (err error) {
	receipt := &receipt.Receipt{}
	err = receipt.Unmarshal(request.Message().Body())
	if err != nil {
		return err
	}
	logger.Debug(fmt.Sprintf("Receive receipt: %v", receipt))

	return nil
}
