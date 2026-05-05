package types

import (
	"github.com/ethereum/go-ethereum/common"

	pb "tool-test/pkg/proto"
)

type EventLog interface {
	// general
	FromProto(logPb *pb.EventLog)
	Unmarshal([]byte) error
	Marshal() ([]byte, error)
	Proto() *pb.EventLog
	String() string
	Copy() EventLog
	// getter
	Hash() common.Hash
	Address() common.Address
	TransactionHash() string
	Data() string
	Topics() []string
}

type EventLogs interface {
	Unmarshal([]byte) error
	Marshal() ([]byte, error)
	FromProto(logPb *pb.EventLogs)
	Proto() *pb.EventLogs
	EventLogList() []EventLog
	Copy() EventLogs
}
