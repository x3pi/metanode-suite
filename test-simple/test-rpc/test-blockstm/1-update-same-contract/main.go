/*
 * BÀI TEST: 1-update-same-contract
 * MÔ TẢ   : Gửi nhiều giao dịch (tx) cùng lúc để gọi hàm update trên 1 Smart Contract duy nhất.
 *           - Chế độ mặc định : Test EVM State (hàm increment / count)
 *           - Chế độ -xapian  : Test Xapian DB Precompile (hàm incrementShared / Xapian DB Document)
 * KỲ VỌNG : Block-STM phải phát hiện read/write conflict, abort và re-execute để đảm bảo tính tuần tự.
 */
package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// dialOptimizedClient khởi tạo RPC client với HTTP Transport được tối ưu connection pool cho benchmark tải cao
func dialOptimizedClient(rawURL string) (*ethclient.Client, error) {
	httpClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        2000,
			MaxIdleConnsPerHost: 1000,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	rpcClient, err := rpc.DialHTTPWithClient(rawURL, httpClient)
	if err != nil {
		return nil, err
	}
	return ethclient.NewClient(rpcClient), nil
}

// Xapian Contract ABI & Bytecode (Shared / Conflict)
//
// KEPT IN SYNC WITH: contracts/SharedXapianBlockSTMTest.{abi,bin}. These two
// constants used to be a stale, hand-copied snapshot (compiled with an older
// solc than the canonical .bin) — the resulting deployed runtime bytecode
// only matched the source string for its first ~1172 of 2344 bytes, then
// diverged, so the constructor's getOrCreateDb() precompile call embedded a
// value into the wrong offset and every call landed on a garbage jump target
// ("N is not a jump destination", N differing per deploy since the precompile
// return value differs run to run). Always regenerate these two constants
// from the canonical artifacts instead of hand-editing.
const xapianAbiJSON = `[{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"wallet","type":"address"},{"indexed":false,"internalType":"uint256","name":"newCounter","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"docId","type":"uint256"}],"name":"SharedUpdated","type":"event"},{"inputs":[],"name":"getSharedDataFromDB","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"incrementShared","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"initializeDoc","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"sharedCounter","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"sharedDocId","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`

const xapianBytecodeHex = "0x608060405234801561000f575f80fd5b5061010773ffffffffffffffffffffffffffffffffffffffff1663fbdddaf06040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152506040518263ffffffff1660e01b81526004016100809190610150565b6020604051808303815f875af115801561009c573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906100c091906101a9565b506101d4565b5f81519050919050565b5f82825260208201905092915050565b5f5b838110156100fd5780820151818401526020810190506100e2565b5f8484015250505050565b5f601f19601f8301169050919050565b5f610122826100c6565b61012c81856100d0565b935061013c8185602086016100e0565b61014581610108565b840191505092915050565b5f6020820190508181035f8301526101688184610118565b905092915050565b5f80fd5b5f8115159050919050565b61018881610174565b8114610192575f80fd5b50565b5f815190506101a38161017f565b92915050565b5f602082840312156101be576101bd610170565b5b5f6101cb84828501610195565b91505092915050565b6108ab80620001e25f395ff3fe608060405234801561000f575f80fd5b5060043610610055575f3560e01c80630b6f8f48146100595780638cc38296146100775780638ff7572a14610095578063b4340bbe146100b3578063d32a9a59146100bd575b5f80fd5b6100616100c7565b60405161006e9190610420565b60405180910390f35b61007f61019b565b60405161008c9190610420565b60405180910390f35b61009d6101a0565b6040516100aa9190610420565b60405180910390f35b6100bb6101a6565b005b6100c56102c1565b005b5f8061010773ffffffffffffffffffffffffffffffffffffffff16633e7c7f8b6040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f546040518363ffffffff1660e01b815260040161013c9291906104c3565b5f604051808303815f875af1158015610157573d5f803e3d5ffd5b505050506040513d5f823e3d601f19601f8201168201806040525081019061017f9190610620565b9050808060200190518101906101959190610691565b91505090565b5f5481565b60015481565b5f8054146101e9576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101e090610706565b60405180910390fd5b61010773ffffffffffffffffffffffffffffffffffffffff16639d4799416040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f60405160200161024e9190610420565b6040516020818303038152906040526040518363ffffffff1660e01b815260040161027a929190610776565b6020604051808303815f875af1158015610296573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906102ba9190610691565b5f81905550565b6001805f8282546102d291906107d8565b925050819055505f600154905061010773ffffffffffffffffffffffffffffffffffffffff1663c5d187dd6040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f54846040516020016103469190610420565b6040516020818303038152906040526040518463ffffffff1660e01b81526004016103739392919061080b565b6020604051808303815f875af115801561038f573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906103b39190610691565b503373ffffffffffffffffffffffffffffffffffffffff167f3d523d852046afd5dba341b55c89c4355e15f6c45d6785c48472c0b7eb922911825f546040516103fd92919061084e565b60405180910390a250565b5f819050919050565b61041a81610408565b82525050565b5f6020820190506104335f830184610411565b92915050565b5f81519050919050565b5f82825260208201905092915050565b5f5b83811015610470578082015181840152602081019050610455565b5f8484015250505050565b5f601f19601f8301169050919050565b5f61049582610439565b61049f8185610443565b93506104af818560208601610453565b6104b88161047b565b840191505092915050565b5f6040820190508181035f8301526104db818561048b565b90506104ea6020830184610411565b9392505050565b5f604051905090565b5f80fd5b5f80fd5b5f80fd5b5f80fd5b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b6105408261047b565b810181811067ffffffffffffffff8211171561055f5761055e61050a565b5b80604052505050565b5f6105716104f1565b905061057d8282610537565b919050565b5f67ffffffffffffffff82111561059c5761059b61050a565b5b6105a58261047b565b9050602081019050919050565b5f6105c46105bf84610582565b610568565b9050828152602081018484840111156105e0576105df610506565b5b6105eb848285610453565b509392505050565b5f82601f83011261060757610606610502565b5b81516106178482602086016105b2565b91505092915050565b5f60208284031215610635576106346104fa565b5b5f82015167ffffffffffffffff811115610652576106516104fe565b5b61065e848285016105f3565b91505092915050565b61067081610408565b811461067a575f80fd5b50565b5f8151905061068b81610667565b92915050565b5f602082840312156106a6576106a56104fa565b5b5f6106b38482850161067d565b91505092915050565b7f416c726561647920696e697469616c697a6564000000000000000000000000005f82015250565b5f6106f0601383610443565b91506106fb826106bc565b602082019050919050565b5f6020820190508181035f83015261071d816106e4565b9050919050565b5f81519050919050565b5f82825260208201905092915050565b5f61074882610724565b610752818561072e565b9350610762818560208601610453565b61076b8161047b565b840191505092915050565b5f6040820190508181035f83015261078e818561048b565b905081810360208301526107a2818461073e565b90509392505050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f6107e282610408565b91506107ed83610408565b9250828201905080821115610805576108046107ab565b5b92915050565b5f6060820190508181035f830152610823818661048b565b90506108326020830185610411565b8181036040830152610844818461073e565b9050949350505050565b5f6040820190506108615f830185610411565b61086e6020830184610411565b939250505056fea26469706673582212204b9d38e9071344784d6b4076f31b5b102e9c0ab749e8a0dfc3d19203bd39221364736f6c63430008140033"

// EVM AbortRollback Contract ABI & Bytecode (Parallel / Non-conflict)
const abortRollbackAbiJSON = `[
  {"inputs":[],"name":"phase","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"internalType":"uint256","name":"p","type":"uint256"}],"name":"setPhase","outputs":[],"stateMutability":"nonpayable","type":"function"},
  {"inputs":[{"internalType":"uint256","name":"val","type":"uint256"}],"name":"updateIfPhase1","outputs":[],"stateMutability":"nonpayable","type":"function"},
  {"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"userData","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}
]`

const abortRollbackBytecodeHex = "608060405260015f553480156012575f5ffd5b5061033d806100205f395ff3fe608060405234801561000f575f5ffd5b506004361061004a575f3560e01c80632cc826551461004e578063824331141461006a578063b1c9fe6e14610086578063c8910913146100a4575b5f5ffd5b610068600480360381019061006391906101b7565b6100d4565b005b610084600480360381019061007f91906101b7565b6100dd565b005b61008e610166565b60405161009b91906101f1565b60405180910390f35b6100be60048036038101906100b99190610264565b61016b565b6040516100cb91906101f1565b60405180910390f35b805f8190555050565b60015f5414610121576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610118906102e9565b60405180910390fd5b8060015f3373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f208190555050565b5f5481565b6001602052805f5260405f205f915090505481565b5f5ffd5b5f819050919050565b61019681610184565b81146101a0575f5ffd5b50565b5f813590506101b18161018d565b92915050565b5f602082840312156101cc576101cb610180565b5b5f6101d9848285016101a3565b91505092915050565b6101eb81610184565b82525050565b5f6020820190506102045f8301846101e2565b92915050565b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f6102338261020a565b9050919050565b61024381610229565b811461024d575f80fd5b50565b5f8135905061025e8161023a565b92915050565b5f6020828403121561027957610278610180565b5b5f61028684828501610250565b91505092915050565b5f82825260208201905092915050565b7f5068617365206973206e6f206c6f6e67657220312120526576657274656421005f82015250565b5f6102d3601f8361028f565b91506102de8261029f565b602082019050919050565b5f6020820190508181035f830152610300816102c7565b905091905056fea2646970667358221220751fc9df1f587327ffd0f0537ef2b81ddcbaa8fe57d9818cd051cea1563859d464736f6c63430008230033"

// Xapian Contract ABI & Bytecode (Parallel / Non-conflict)
const xapianParallelAbiJSON = `[
  {"inputs":[],"stateMutability":"nonpayable","type":"constructor"},
  {"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"wallet","type":"address"},{"indexed":false,"internalType":"uint256","name":"newCounter","type":"uint256"},{"indexed":false,"internalType":"uint256","name":"docId","type":"uint256"}],"name":"ParallelUpdated","type":"event"},
  {"inputs":[{"internalType":"address","name":"user","type":"address"}],"name":"getUserDataFromDB","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"nonpayable","type":"function"},
  {"inputs":[],"name":"incrementUser","outputs":[],"stateMutability":"nonpayable","type":"function"},
  {"inputs":[],"name":"initializeDoc","outputs":[],"stateMutability":"nonpayable","type":"function"},
  {"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"userDocIds","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}
]`

const xapianParallelBytecodeHex = "0x608060405234801562000010575f80fd5b5061010773ffffffffffffffffffffffffffffffffffffffff1663fbdddaf06040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e00000000000000008152506040518263ffffffff1660e01b815260040162000083919062000161565b6020604051808303815f875af1158015620000a0573d5f803e3d5ffd5b505050506040513d601f19601f82011682018060405250810190620000c69190620001c1565b50620001f1565b5f81519050919050565b5f82825260208201905092915050565b5f5b8381101562000106578082015181840152602081019050620000e9565b5f8484015250505050565b5f601f19601f8301169050919050565b5f6200012d82620000cd565b620001398185620000d7565b93506200014b818560208601620000e7565b620001568162000111565b840191505092915050565b5f6020820190508181035f8301526200017b818462000121565b905092915050565b5f80fd5b5f8115159050919050565b6200019d8162000187565b8114620001a8575f80fd5b50565b5f81519050620001bb8162000192565b92915050565b5f60208284031215620001d957620001d862000183565b5b5f620001e884828501620001ab565b91505092915050565b610de280620001ff5f395ff3fe608060405234801561000f575f80fd5b506004361061004a575f3560e01c8063147806e31461004e578063a3e6f20714610058578063b4340bbe14610088578063d03df7a014610092575b5f80fd5b6100566100c2565b005b610072600480360381019061006d9190610855565b6104aa565b60405161007f9190610898565b60405180910390f35b610090610645565b005b6100ac60048036038101906100a79190610855565b6107d6565b6040516100b99190610898565b60405180910390f35b5f805f3373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f205490505f8082036102285761010773ffffffffffffffffffffffffffffffffffffffff16639d4799416040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e000000000000000081525060016040516020016101709190610898565b6040516020818303038152906040526040518363ffffffff1660e01b815260040161019c92919061098d565b6020604051808303815f875af11580156101b8573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906101dc91906109ec565b9150815f803373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f20819055506001905061041a565b5f61010773ffffffffffffffffffffffffffffffffffffffff16633e7c7f8b6040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e0000000000000000815250856040518363ffffffff1660e01b815260040161029b929190610a17565b5f604051808303815f875af11580156102b6573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906102de9190610b63565b9050808060200190518101906102f491906109ec565b91506001826103039190610bd7565b915061010773ffffffffffffffffffffffffffffffffffffffff1663c5d187dd6040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e0000000000000000815250858560405160200161036b9190610898565b6040516020818303038152906040526040518463ffffffff1660e01b815260040161039893929190610c0a565b6020604051808303815f875af11580156103b4573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906103d891906109ec565b5f803373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f2081905550505b3373ffffffffffffffffffffffffffffffffffffffff167fbacacd063536a5bb218e91592803262fcc97f5a0282d5776c4b387a3c7261666825f803373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f205460405161049e929190610c4d565b60405180910390a25050565b5f805f808473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f205490505f810361052d576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161052490610cbe565b60405180910390fd5b5f61010773ffffffffffffffffffffffffffffffffffffffff16633e7c7f8b6040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e0000000000000000815250846040518363ffffffff1660e01b81526004016105a0929190610a17565b5f604051808303815f875af11580156105bb573d5f803e3d5ffd5b505050506040513d601f19601f820116820180604052508101906105e39190610b63565b90505f815111610628576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161061f90610d26565b60405180910390fd5b8080602001905181019061063c91906109ec565b92505050919050565b5f805f3373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f2054146106c3576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016106ba90610d8e565b60405180910390fd5b61010773ffffffffffffffffffffffffffffffffffffffff16639d4799416040518060400160405280601881526020017f626c6f636b73746d5f706172616c6c656c5f78617069616e00000000000000008152505f6040516020016107289190610898565b6040516020818303038152906040526040518363ffffffff1660e01b815260040161075492919061098d565b6020604051808303815f875af1158015610770573d5f803e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061079491906109ec565b5f803373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020015f2081905550565b5f602052805f5260405f205f915090505481565b5f604051905090565b5f80fd5b5f80fd5b5f73ffffffffffffffffffffffffffffffffffffffff82169050919050565b5f610824826107fb565b9050919050565b6108348161081a565b811461083e575f80fd5b50565b5f8135905061084f8161082b565b92915050565b5f6020828403121561086a576108696107f3565b5b5f61087784828501610841565b91505092915050565b5f819050919050565b61089281610880565b82525050565b5f6020820190506108ab5f830184610889565b92915050565b5f81519050919050565b5f82825260208201905092915050565b5f5b838110156108e85780820151818401526020810190506108cd565b5f8484015250505050565b5f601f19601f8301169050919050565b5f61090d826108b1565b61091781856108bb565b93506109278185602086016108cb565b610930816108f3565b840191505092915050565b5f81519050919050565b5f82825260208201905092915050565b5f61095f8261093b565b6109698185610945565b93506109798185602086016108cb565b610982816108f3565b840191505092915050565b5f6040820190508181035f8301526109a58185610903565b905081810360208301526109b98184610955565b9050939250505050565b6109cb81610880565b81146109d5575f80fd5b50565b5f815190506109e6816109c2565b92915050565b5f60208284031215610a0157610a006107f3565b5b5f610a0e848285016109d8565b91505092915050565b5f6040820190508181035f830152610a2f8185610903565b9050610a3e6020830184610889565b9392505050565b5f80fd5b5f80fd5b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b610a83826108f3565b810181811067ffffffffffffffff82111715610aa257610aa1610a4d565b5b80604052505050565b5f610ab46107ea565b9050610ac08282610a7a565b919050565b5f67ffffffffffffffff821115610adf57610ade610a4d565b5b610ae8826108f3565b9050602081019050919050565b5f610b07610b0284610ac5565b610aab565b905082815260208101848484011115610b2357610b22610a49565b5b610b2e8482856108cb565b509392505050565b5f82601f830112610b4a57610b49610a45565b5b8151610b5a848260208601610af5565b91505092915050565b5f60208284031215610b7857610b776107f3565b5b5f82015167ffffffffffffffff811115610b9557610b946107f7565b5b610ba184828501610b36565b91505092915050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f610be182610880565b9150610bec83610880565b9250828201905080821115610c0457610c03610baa565b5b92915050565b5f6060820190508181035f830152610c228186610903565b9050610c316020830185610889565b8181036040830152610c438184610955565b9050949350505050565b5f604082019050610c605f830185610889565b610c6d6020830184610889565b9392505050567f4e6f7420696e697469616c697a656400000000000000000000000000000000005f82015250565b5f610ca8600f836108bb565b9150610cb382610c74565b602082019050919050565b5f6020820190508181035f830152610cd581610c9c565b9050919050567f44617461206e6f7420666f756e6420696e2058617069616e00000000000000005f82015250565b5f610d106018836108bb565b9150610d1b82610cdc565b602082019050919050565b5f6020820190508181035f830152610d3d81610d04565b9050919050567f416c726561647920696e697469616c697a6564000000000000000000000000005f82015250565b5f610d786013836108bb565b9150610d8382610d44565b602082019050919050565b5f6020820190508181035f830152610da581610d6c565b905091905056fea264697066735822122027362f8eea02f4ec5a71e7b3e06ec71a03312931c0a45ad94a01ee0e9ab11c9064736f6c63430008140033"

type ContractData struct {
	ABI      string `json:"abi"`
	Bytecode string `json:"bytecode"`
}

type Config struct {
	RPCUrl    string                  `json:"rpc_url"`
	RPCNodes  map[string]string       `json:"rpc_nodes"`
	ChainID   int64                   `json:"chain_id"`
	Contracts map[string]ContractData `json:"contracts"`
}

type GeneratedKey struct {
	Index      int    `json:"index"`
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
}

func main() {
	rounds := flag.Int("rounds", 1, "Số round muốn test")
	configFlag := flag.String("config", "../config.json", "Đường dẫn file config")
	keysFile := flag.String("keys", "../../../../test_tps/gen_spam_keys/generated_keys.json", "Đường dẫn file chứa private keys")
	multiNodes := flag.Bool("multi", false, "Chế độ gửi giao dịch dàn trải lên nhiều RPC node từ config.json")
	numKeys := flag.Int("num", 10, "Số lượng keys để test (0 = tất cả, mặc định là 10)")
	waitMethod := flag.String("wait-method", "block", "Phương thức chờ giao dịch: 'block' hoặc 'receipt'")
	useXapian := flag.Bool("xapian", false, "Chế độ test Xapian DB (thay vì EVM State thông thường)")
	useParallel := flag.Bool("parallel", false, "Chế độ test song song không xung đột (non-conflicting parallel updates)")
	flag.Parse()

	fmt.Println("==========================================================")
	if *useXapian {
		if *useParallel {
			fmt.Println("BÀI TEST: 1-update-same-contract (CHẾ ĐỘ XAPIAN DB - SONG SONG KHÔNG XUNG ĐỘT)")
			fmt.Println("==========================================================")
			fmt.Println("📖 MÔ TẢ   : Gửi nhiều tx cùng lúc cập nhật độc lập từng key trên Xapian DB.")
			fmt.Println("⚡ GỌI     : Giao dịch gọi hàm incrementUser() từ các ví.")
			fmt.Println("🎯 KỲ VỌNG : Block-STM phát hiện KHÔNG CÓ read/write conflict trên Xapian Document, xử lý song song thành công.")
		} else {
			fmt.Println("BÀI TEST: 1-update-same-contract (CHẾ ĐỘ XAPIAN DB - CHUNG 1 GIÁ TRỊ)")
			fmt.Println("==========================================================")
			fmt.Println("📖 MÔ TẢ   : Gửi nhiều tx cùng lúc cập nhật chung 1 giá trị trên Xapian DB.")
			fmt.Println("⚡ GỌI     : Giao dịch gọi hàm incrementShared() từ các ví.")
			fmt.Println("🎯 KỲ VỌNG : Block-STM phải phát hiện read/write conflict trên Xapian Document, re-execute và cho kết quả đúng.")
		}
	} else {
		if *useParallel {
			fmt.Println("BÀI TEST: 1-update-same-contract (CHẾ ĐỘ EVM STATE - SONG SONG KHÔNG XUNG ĐỘT)")
			fmt.Println("==========================================================")
			fmt.Println("📖 MÔ TẢ   : Gửi nhiều giao dịch (tx) cùng lúc để gọi hàm update (ghi mapping độc lập) trên 1 Smart Contract.")
			fmt.Println("⚡ GỌI     : Giao dịch gọi hàm updateIfPhase1() từ các ví.")
			fmt.Println("🎯 KỲ VỌNG : Block-STM phát hiện KHÔNG CÓ read/write conflict, toàn bộ txs chạy song song thành công mượt mà.")
		} else {
			fmt.Println("BÀI TEST: 1-update-same-contract (CHẾ ĐỘ EVM STATE - UPDATE CHUNG BIẾN)")
			fmt.Println("==========================================================")
			fmt.Println("📖 MÔ TẢ   : Gửi nhiều giao dịch (tx) cùng lúc để gọi hàm update (tăng biến count) trên cùng một Smart Contract.")
			fmt.Println("⚡ GỌI     : Giao dịch gọi hàm EVM update state (increment) trên 1 contract duy nhất.")
			fmt.Println("🎯 KỲ VỌNG : Block-STM phải phát hiện read/write conflict, abort và re-execute để đảm bảo tính tuần tự.")
		}
	}
	fmt.Println("==========================================================")
	fmt.Println("🚀 KẾT QUẢ THỰC THI:")

	configPath := *configFlag
	if flag.NArg() > 0 {
		configPath = flag.Arg(0)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc config %s: %v", configPath, err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Fatalf("❌ Lỗi parse config: %v", err)
	}

	client, err := dialOptimizedClient(cfg.RPCUrl)
	if err != nil {
		log.Fatalf("❌ Lỗi kết nối RPC: %v", err)
	}

	var rpcClients []*ethclient.Client
	if *multiNodes && len(cfg.RPCNodes) > 0 {
		for name, url := range cfg.RPCNodes {
			if c, e := dialOptimizedClient(url); e == nil {
				rpcClients = append(rpcClients, c)
			} else {
				fmt.Printf("⚠️ Lỗi kết nối node %s (%s): %v\n", name, url, e)
			}
		}
	}
	if len(rpcClients) == 0 {
		if *multiNodes {
			fmt.Println("⚠️ Không cấu hình rpc_nodes trong config.json hoặc kết nối lỗi, fallback về RPC mặc định")
		}
		rpcClients = append(rpcClients, client)
	} else {
		fmt.Printf("🌐 Đã kết nối tới %d nodes RPC (Chế độ multi)\n", len(rpcClients))
	}

	var parsedABI abi.ABI
	var bytecode []byte

	if *useXapian {
		if *useParallel {
			parsedABI, err = abi.JSON(strings.NewReader(xapianParallelAbiJSON))
			if err != nil {
				log.Fatalf("❌ Lỗi parse Xapian Parallel ABI: %v", err)
			}
			bytecode, err = hexutil.Decode(xapianParallelBytecodeHex)
			if err != nil {
				log.Fatalf("❌ Lỗi decode Xapian Parallel bytecode hex: %v", err)
			}
		} else {
			parsedABI, err = abi.JSON(strings.NewReader(xapianAbiJSON))
			if err != nil {
				log.Fatalf("❌ Lỗi parse Xapian ABI: %v", err)
			}
			bytecode, err = hexutil.Decode("0x" + strings.TrimPrefix(xapianBytecodeHex, "0x"))
			if err != nil {
				log.Fatalf("❌ Lỗi decode Xapian bytecode hex: %v", err)
			}
		}
	} else {
		if *useParallel {
			var abiStr string
			var bcStr string
			if contractData, ok := cfg.Contracts["AbortRollback"]; ok && contractData.ABI != "" {
				abiStr = contractData.ABI
				bcStr = contractData.Bytecode
			} else {
				abiStr = abortRollbackAbiJSON
				bcStr = abortRollbackBytecodeHex
			}
			parsedABI, err = abi.JSON(strings.NewReader(abiStr))
			if err != nil {
				log.Fatalf("❌ Lỗi parse AbortRollback ABI: %v", err)
			}
			bytecode, err = hexutil.Decode("0x" + strings.TrimPrefix(bcStr, "0x"))
			if err != nil {
				log.Fatalf("❌ Lỗi decode AbortRollback bytecode hex: %v", err)
			}
		} else {
			parsedABI, err = abi.JSON(strings.NewReader(cfg.Contracts["TestCounter"].ABI))
			if err != nil {
				log.Fatalf("❌ Lỗi parse Normal ABI: %v", err)
			}
			bytecode, err = hexutil.Decode("0x" + cfg.Contracts["TestCounter"].Bytecode)
			if err != nil {
				log.Fatalf("❌ Lỗi decode Normal bytecode hex: %v", err)
			}
		}
	}

	keysRaw, err := os.ReadFile(*keysFile)
	if err != nil {
		log.Fatalf("❌ Lỗi đọc file keys %s: %v", *keysFile, err)
	}
	var genKeys []GeneratedKey
	if err := json.Unmarshal(keysRaw, &genKeys); err != nil {
		log.Fatalf("❌ Lỗi parse keys: %v", err)
	}

	var testKeys []string
	for _, gk := range genKeys {
		testKeys = append(testKeys, gk.PrivateKey)
	}

	if *numKeys > 0 && len(testKeys) > *numKeys {
		testKeys = testKeys[:*numKeys]
	}

	if len(testKeys) == 0 {
		log.Fatalf("❌ Không có private key nào được load")
	}

	// Use the first key to deploy
	pk0, err := crypto.HexToECDSA(testKeys[0])
	if err != nil {
		log.Fatalf("❌ Lỗi parse private key 0: %v", err)
	}
	from0 := crypto.PubkeyToAddress(*pk0.Public().(*ecdsa.PublicKey))

	fmt.Println("🚀 Deploying contract with Account 0...")
	contractAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		log.Fatalf("❌ Deploy thất bại: %v", err)
	}
	fmt.Printf("📌 Contract deployed at: %s\n\n", contractAddr.Hex())

	if *useXapian {
		fmt.Println("⚙️ Initializing Xapian Document...")
		initHash, err := sendInitializeDoc(client, pk0, cfg.ChainID, from0, contractAddr, parsedABI)
		if err != nil {
			log.Fatalf("❌ InitializeDoc failed: %v", err)
		}
		fmt.Printf("   TX Hash: %s\n", initHash.Hex())

		initReceipt, err := waitReceipt(client, initHash)
		if err != nil || initReceipt.Status != 1 {
			log.Fatalf("❌ Khởi tạo Document thất bại!")
		}
		fmt.Printf("✅ InitializeDoc thành công!\n\n")
	}

	var wg sync.WaitGroup
	var errs []error
	var errsMu sync.Mutex
	// Semaphore giới hạn số goroutine gửi tx đồng thời → tránh cạn kiệt HTTP connection pool
	sendSem := make(chan struct{}, 1000)

	type RoundSummary struct {
		Round        int
		SuccessTx    int
		TotalSuccess int
		DBValue      uint64
		Passed       bool
	}
	var summaries []RoundSummary

	start := time.Now()
	totalSuccess := 0

	// Lấy số dư ví 0 trước khi test
	balanceBefore, err := client.BalanceAt(context.Background(), from0, nil)
	if err != nil {
		log.Fatalf("❌ Lỗi lấy số dư ban đầu: %v", err)
	}
	fmt.Printf("💰 Số dư ví 0 ban đầu: %s wei\n", balanceBefore.String())

	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Fatalf("❌ Lỗi lấy startBlock: %v", err)
	}
	startBlock := header.Number.Uint64()

	fmt.Printf("⏳ Đang tải nonce ban đầu cho %d ví...\n", len(testKeys))
	startNonces := make([]uint64, len(testKeys))
	var nonceWg sync.WaitGroup
	nonceSem := make(chan struct{}, 200)
	for i, pkStr := range testKeys {
		nonceWg.Add(1)
		go func(idx int, pKeyHex string) {
			defer nonceWg.Done()
			nonceSem <- struct{}{}
			defer func() { <-nonceSem }()

			pk, err := crypto.HexToECDSA(pKeyHex)
			if err != nil {
				return
			}
			from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))
			clientForTx := rpcClients[idx%len(rpcClients)]
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			n, err := clientForTx.PendingNonceAt(ctx, from)
			if err == nil {
				startNonces[idx] = n
			}
		}(i, pkStr)
	}
	nonceWg.Wait()
	fmt.Printf("✅ Đã tải xong nonce ban đầu cho tất cả ví!\n\n")

	for r := 1; r <= *rounds; r++ {
		fmt.Printf("\n🔥 --- ROUND %d/%d --- 🔥\n", r, *rounds)
		if *useXapian {
			if *useParallel {
				fmt.Printf("🔥 Gửi %d giao dịch đồng thời để update Xapian DB (Song song không xung đột)...\n", len(testKeys))
			} else {
				fmt.Printf("🔥 Gửi %d giao dịch đồng thời để update Xapian DB (Xung đột shared)...\n", len(testKeys))
			}
		} else {
			if *useParallel {
				fmt.Printf("🔥 Gửi %d giao dịch đồng thời để update EVM State contract (Song song không xung đột)...\n", len(testKeys))
			} else {
				fmt.Printf("🔥 Gửi %d giao dịch đồng thời để update EVM State contract (Xung đột shared)...\n", len(testKeys))
			}
		}

		txHashes := make([]common.Hash, len(testKeys))

		for i, pkStr := range testKeys {
			wg.Add(1)
			go func(idx int, pKeyHex string) {
				defer wg.Done()
				sendSem <- struct{}{}
				defer func() { <-sendSem }()

				pk, err := crypto.HexToECDSA(pKeyHex)
				if err != nil {
					errsMu.Lock()
					errs = append(errs, fmt.Errorf("round %d - lỗi key %d: %v", r, idx, err))
					errsMu.Unlock()
					return
				}
				from := crypto.PubkeyToAddress(*pk.Public().(*ecdsa.PublicKey))

				clientForTx := rpcClients[idx%len(rpcClients)]
				var hash common.Hash
				const maxRetry = 3
				txNonce := startNonces[idx] + uint64(r-1)
				for attempt := 0; attempt < maxRetry; attempt++ {
					clientForTx = rpcClients[(idx+attempt)%len(rpcClients)]
					currentNonce := txNonce
					if attempt > 0 {
						ctxN, cancelN := context.WithTimeout(context.Background(), 5*time.Second)
						if freshNonce, errN := clientForTx.PendingNonceAt(ctxN, from); errN == nil {
							currentNonce = freshNonce
						}
						cancelN()
					}

					if *useXapian {
						if *useParallel {
							hash, err = sendIncrementUser(clientForTx, pk, cfg.ChainID, from, contractAddr, parsedABI, currentNonce)
						} else {
							hash, err = sendIncrementShared(clientForTx, pk, cfg.ChainID, from, contractAddr, parsedABI, currentNonce)
						}
					} else {
						if *useParallel {
							hash, err = sendUpdateDifferentVariables(clientForTx, pk, cfg.ChainID, from, contractAddr, parsedABI, currentNonce)
						} else {
							hash, err = sendIncrement(clientForTx, pk, cfg.ChainID, from, contractAddr, parsedABI, currentNonce)
						}
					}
					if err == nil {
						break
					}
					if attempt < maxRetry-1 {
						time.Sleep(500 * time.Millisecond)
					}
				}

				if err != nil {
					errsMu.Lock()
					fmt.Printf("❌ Round %d - Wallet %d gửi tx thất bại (sau %d lần thử): %v\n", r, idx, maxRetry, err)
					errs = append(errs, fmt.Errorf("round %d - lỗi send tx từ wallet %d (sau %d lần thử): %v", r, idx, maxRetry, err))
					errsMu.Unlock()
					return
				}

				fmt.Printf("✅ Round %d - Wallet %d gửi tx thành công: %s\n", r, idx, hash.Hex())
				txHashes[idx] = hash
			}(i, pkStr)
		}

		wg.Wait()

		var errCount int
		if len(errs) > 0 {
			errsMu.Lock()
			errCount = len(errs)
			fmt.Printf("❌ Round %d: %d/%d giao dịch gửi thất bại (sau 3 lần retry):\n", r, errCount, len(testKeys))
			for _, e := range errs {
				fmt.Println("  -", e)
			}
			errs = nil
			errsMu.Unlock()
			fmt.Printf("🚨 [BỎ QUA LỖI] Round %d có %d tx thất bại sau 3 lần retry. Chuyển sang bước đợi block cho các tx thành công...\n", r, errCount)
		}

		var roundSuccess int
		if *waitMethod == "receipt" {
			fmt.Println("⏳ Chờ các giao dịch được confirm bằng cách lấy Receipt...")
			successCount := 0
			var wgReceipt sync.WaitGroup
			var mu sync.Mutex

			donePrint := make(chan struct{})
			go func() {
				ticker := time.NewTicker(3 * time.Second)
				defer ticker.Stop()
				startTime := time.Now()
				for {
					select {
					case <-ticker.C:
						mu.Lock()
						current := successCount
						mu.Unlock()
						fmt.Printf("   [⏳ Waiting Receipt] Đã confirm %d/%d txs... (Thời gian chờ: %v)\n", current, len(txHashes), time.Since(startTime).Round(time.Second))
					case <-donePrint:
						return
					}
				}
			}()

			sem := make(chan struct{}, 50)

			for _, h := range txHashes {
				if h == (common.Hash{}) {
					continue
				}
				wgReceipt.Add(1)
				go func(txHash common.Hash) {
					defer wgReceipt.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					receipt, err := waitReceipt(client, txHash)
					if err == nil && receipt != nil && receipt.Status == 1 {
						mu.Lock()
						successCount++
						mu.Unlock()
					}
				}(h)
			}
			wgReceipt.Wait()
			close(donePrint)
			roundSuccess = successCount
			totalSuccess += roundSuccess
			fmt.Printf("✅ Đã confirm %d/%d giao dịch bằng Receipt trong round %d\n", roundSuccess, len(txHashes), r)
		} else {
			fmt.Println("⏳ Chờ các giao dịch được confirm bằng cách quét Block...")
			successCount, err := waitForTxHashesByBlock(client, txHashes, startBlock, cfg.RPCNodes)
			if err != nil {
				fmt.Printf("❌ Lỗi khi chờ block: %v\n", err)
				log.Fatalf("🚨 Dừng chương trình do Timeout 4 phút khi chờ confirm block!")
			}
			roundSuccess = successCount
			totalSuccess += roundSuccess
			fmt.Printf("✅ Đã confirm %d/%d giao dịch bằng quét Block trong round %d\n", roundSuccess, len(txHashes), r)
			fmt.Println("⏳ Đợi thêm 20s để đảm bảo State DB commit xong trước khi chạy round tiếp theo...")
			time.Sleep(20 * time.Second)
		}

		var roundActual uint64
		var isPassed bool

		if *useXapian {
			if *useParallel {
				userVal, err := getUserDataFromDB(client, contractAddr, parsedABI, from0)
				if err != nil {
					fmt.Printf("❌ Lỗi đọc state Xapian Parallel sau round %d: %v\n", r, err)
				} else {
					roundActual = userVal.Uint64()
					isPassed = (roundActual == uint64(r))
				}
			} else {
				roundActual, err = getSharedDataFromDB(client, contractAddr, parsedABI)
				if err != nil {
					fmt.Printf("❌ Lỗi đọc state Xapian Shared sau round %d: %v\n", r, err)
				} else {
					isPassed = (uint64(totalSuccess) == roundActual)
				}
			}
		} else {
			if *useParallel {
				userVal, err := getUserDataEVM(client, contractAddr, parsedABI, from0)
				if err != nil {
					fmt.Printf("❌ Lỗi đọc state EVM Parallel sau round %d: %v\n", r, err)
				} else {
					roundActual = userVal.Uint64()
					isPassed = (roundActual == 1)
				}
			} else {
				roundActual, err = getCount(client, contractAddr, parsedABI)
				if err != nil {
					fmt.Printf("❌ Lỗi đọc state EVM count sau round %d: %v\n", r, err)
				} else {
					isPassed = (uint64(totalSuccess) == roundActual)
				}
			}
		}

		if err != nil {
			fmt.Printf("❌ Lỗi đọc state sau round %d: %v\n", r, err)
		} else {
			fmt.Printf("\n📊 KẾT QUẢ ROUND %d:\n", r)
			fmt.Printf("   - Số tx thành công round này : %d\n", roundSuccess)
			fmt.Printf("   - Tổng tx thành công đến hiện tại: %d\n", totalSuccess)
			if *useXapian {
				if *useParallel {
					fmt.Printf("   - Giá trị Xapian DB ví 0 thực tế: %d (Kỳ vọng: %d)\n", roundActual, r)
				} else {
					fmt.Printf("   - Giá trị Xapian DB thực tế    : %d (Kỳ vọng: %d)\n", roundActual, totalSuccess)
				}
			} else {
				if *useParallel {
					fmt.Printf("   - Giá trị EVM ví 0 thực tế    : %d (Kỳ vọng: 1)\n", roundActual)
				} else {
					fmt.Printf("   - Giá trị EVM count thực tế   : %d (Kỳ vọng: %d)\n", roundActual, totalSuccess)
				}
			}

			if isPassed {
				fmt.Printf("   => ✅ ROUND PASSED\n")
			} else {
				fmt.Printf("   => ❌ ROUND FAILED\n")
				fmt.Println("================================================--------------------------------")
				fmt.Printf("🚨 [LỖI LỆCH KẾT QUẢ STATE TẠI ROUND %d]\n", r)
				fmt.Println("================================================--------------------------------")
				log.Fatalf("🚨 Dừng chương trình ngay lập tức do phát hiện điểm sai ở Round %d!", r)
			}

			summaries = append(summaries, RoundSummary{
				Round:        r,
				SuccessTx:    roundSuccess,
				TotalSuccess: totalSuccess,
				DBValue:      roundActual,
				Passed:       isPassed,
			})
		}
	}

	var actual uint64
	var testPassed bool
	if *useXapian {
		if *useParallel {
			val, err := getUserDataFromDB(client, contractAddr, parsedABI, from0)
			if err == nil {
				actual = val.Uint64()
				testPassed = (actual == uint64(*rounds))
			}
		} else {
			actual, err = getSharedDataFromDB(client, contractAddr, parsedABI)
			if err == nil {
				testPassed = (actual == uint64(totalSuccess))
			}
		}
	} else {
		if *useParallel {
			val, err := getUserDataEVM(client, contractAddr, parsedABI, from0)
			if err == nil {
				actual = val.Uint64()
				testPassed = (actual == 1)
			}
		} else {
			actual, err = getCount(client, contractAddr, parsedABI)
			if err == nil {
				testPassed = (actual == uint64(totalSuccess))
			}
		}
	}

	if err != nil {
		log.Fatalf("❌ Lỗi đọc state cuối cùng: %v", err)
	}

	elapsed := time.Since(start)

	balanceAfter, err := client.BalanceAt(context.Background(), from0, nil)
	if err != nil {
		log.Fatalf("❌ Lỗi lấy số dư cuối: %v", err)
	}

	totalCost := new(big.Int).Sub(balanceBefore, balanceAfter)
	fmt.Printf("\n💰 THỐNG KÊ CHI PHÍ GAS VÍ 0:\n")
	fmt.Printf("   - Số dư ban đầu: %s wei\n", balanceBefore.String())
	fmt.Printf("   - Số dư sau cùng: %s wei\n", balanceAfter.String())
	fmt.Printf("   - Tổng phí đã trừ (Gas Cost): %s wei\n", totalCost.String())

	if totalCost.Cmp(big.NewInt(0)) == 0 {
		log.Fatalf("\n❌ [LỖI NGHIÊM TRỌNG] PHÍ GAS KHÔNG BỊ TRỪ! Giao dịch Smart Contract không trừ phí từ Sender!")
	} else {
		fmt.Printf("   => ✅ Phí Gas đã được trừ hợp lệ!\n")
	}

	fmt.Println("\n📊 KẾT QUẢ:")
	fmt.Printf("Thời gian gửi & chờ: %v\n", elapsed)
	if *useXapian {
		if *useParallel {
			fmt.Printf("Giá trị counter ví 0 lưu trong Xapian DB: %d\n", actual)
		} else {
			fmt.Printf("Giá trị counter cuối cùng lưu trong Xapian DB: %d\n", actual)
		}
	} else {
		if *useParallel {
			fmt.Printf("Giá trị state ví 0 EVM: %d\n", actual)
		} else {
			fmt.Printf("Giá trị count EVM cuối cùng: %d\n", actual)
		}
	}
	fmt.Printf("Tổng số lượng tx thành công: %d (trên %d round, mỗi round %d ví)\n", totalSuccess, *rounds, len(testKeys))

	fmt.Println("\n📋 BẢNG TỔNG HỢP CÁC ROUND:")
	fmt.Println("-------------------------------------------------------------------------")
	fmt.Printf("%-10s | %-12s | %-14s | %-10s | %-10s\n", "Round", "Tx Success", "Total Success", "State Value", "Status")
	fmt.Println("-------------------------------------------------------------------------")
	for _, s := range summaries {
		status := "✅ PASSED"
		if !s.Passed {
			status = "⚠️ FAILED"
		}
		fmt.Printf("%-10d | %-12d | %-14d | %-10d | %-10s\n", s.Round, s.SuccessTx, s.TotalSuccess, s.DBValue, status)
	}
	fmt.Println("-------------------------------------------------------------------------")

	fmt.Println("\n🏁 KẾT LUẬN CUỐI CÙNG:")
	if testPassed {
		if *useParallel {
			fmt.Println("🎉 TEST PASSED: BlockSTM xử lý song song không xung đột thành công!")
		} else {
			fmt.Println("🎉 TEST PASSED: BlockSTM xử lý write conflict đúng!")
		}
	} else {
		fmt.Println("⚠️ TEST FAILED: Giá trị state không khớp với kỳ vọng")
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func deployContract(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, bytecode []byte) (*common.Address, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, err
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		gasPrice = big.NewInt(1000000000)
	}

	gasLimit, err := client.EstimateGas(ctx, ethereum.CallMsg{From: from, Data: bytecode})
	if err != nil {
		gasLimit = 5_000_000
	} else {
		gasLimit += 50_000
	}

	tx := types.NewContractCreation(nonce, big.NewInt(0), gasLimit, gasPrice, bytecode)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return nil, err
	}

	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return nil, err
	}

	receipt, err := waitReceipt(client, signedTx.Hash())
	if err != nil {
		return nil, err
	}
	if receipt.Status != 1 {
		return nil, fmt.Errorf("deploy reverted")
	}
	return &receipt.ContractAddress, nil
}

func sendIncrement(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI, nonce uint64) (common.Hash, error) {
	data, err := parsedABI.Pack("increment")
	if err != nil {
		return common.Hash{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gasPrice := big.NewInt(1000000000)
	gasLimit := uint64(100000)

	tx := types.NewTransaction(nonce, *to, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return common.Hash{}, err
	}

	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return common.Hash{}, err
	}
	return signedTx.Hash(), nil
}

func sendIncrementShared(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI, nonce uint64) (common.Hash, error) {
	data, err := parsedABI.Pack("incrementShared")
	if err != nil {
		return common.Hash{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gasPrice := big.NewInt(1000000000)
	gasLimit := uint64(1_000_000)

	tx := types.NewTransaction(nonce, *to, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return common.Hash{}, err
	}

	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return common.Hash{}, err
	}
	return signedTx.Hash(), nil
}

func sendUpdateDifferentVariables(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI, nonce uint64) (common.Hash, error) {
	data, err := parsedABI.Pack("updateIfPhase1", big.NewInt(1))
	if err != nil {
		return common.Hash{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gasPrice := big.NewInt(1000000000)
	gasLimit := uint64(100000)

	tx := types.NewTransaction(nonce, *to, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return common.Hash{}, err
	}

	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return common.Hash{}, err
	}
	return signedTx.Hash(), nil
}

func sendIncrementUser(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI, nonce uint64) (common.Hash, error) {
	data, err := parsedABI.Pack("incrementUser")
	if err != nil {
		return common.Hash{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gasPrice := big.NewInt(1000000000)
	gasLimit := uint64(1_000_000)

	tx := types.NewTransaction(nonce, *to, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return common.Hash{}, err
	}

	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return common.Hash{}, err
	}
	return signedTx.Hash(), nil
}

func getUserDataFromDB(client *ethclient.Client, contractAddr *common.Address, parsedABI abi.ABI, user common.Address) (*big.Int, error) {
	data, err := parsedABI.Pack("getUserDataFromDB", user)
	if err != nil {
		return nil, err
	}

	msg := ethereum.CallMsg{
		To:   contractAddr,
		Data: data,
	}
	result, err := client.CallContract(context.Background(), msg, nil)
	if err != nil {
		return nil, err
	}

	res, err := parsedABI.Unpack("getUserDataFromDB", result)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("không có kết quả trả về")
	}

	val, ok := res[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("kết quả không phải là *big.Int")
	}
	return val, nil
}

func getUserDataEVM(client *ethclient.Client, contractAddr *common.Address, parsedABI abi.ABI, user common.Address) (*big.Int, error) {
	data, err := parsedABI.Pack("userData", user)
	if err != nil {
		return nil, err
	}

	msg := ethereum.CallMsg{
		To:   contractAddr,
		Data: data,
	}
	result, err := client.CallContract(context.Background(), msg, nil)
	if err != nil {
		return nil, err
	}

	res, err := parsedABI.Unpack("userData", result)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("không có kết quả trả về")
	}

	val, ok := res[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("kết quả không phải là *big.Int")
	}
	return val, nil
}

func sendInitializeDoc(client *ethclient.Client, pk *ecdsa.PrivateKey, chainID int64, from common.Address, to *common.Address, parsedABI abi.ABI) (common.Hash, error) {
	data, err := parsedABI.Pack("initializeDoc")
	if err != nil {
		return common.Hash{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return common.Hash{}, err
	}

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		gasPrice = big.NewInt(1000000000)
	}

	gasLimit, err := client.EstimateGas(ctx, ethereum.CallMsg{From: from, To: to, GasPrice: gasPrice, Data: data})
	if err != nil {
		gasLimit = 2_000_000
	} else {
		gasLimit += 200_000
	}

	tx := types.NewTransaction(nonce, *to, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(chainID)), pk)
	if err != nil {
		return common.Hash{}, err
	}

	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return common.Hash{}, err
	}
	return signedTx.Hash(), nil
}

func getCount(client *ethclient.Client, addr *common.Address, parsedABI abi.ABI) (uint64, error) {
	data, _ := parsedABI.Pack("getCount")
	result, err := client.CallContract(context.Background(), ethereum.CallMsg{To: addr, Data: data}, nil)
	if err != nil {
		return 0, err
	}
	outputs, err := parsedABI.Unpack("getCount", result)
	if err != nil {
		return 0, err
	}
	if len(outputs) == 0 {
		return 0, fmt.Errorf("output rỗng")
	}
	val, ok := outputs[0].(*big.Int)
	if !ok {
		return 0, fmt.Errorf("kiểu trả về không phải *big.Int")
	}
	return val.Uint64(), nil
}

func getSharedDataFromDB(client *ethclient.Client, addr *common.Address, parsedABI abi.ABI) (uint64, error) {
	data, _ := parsedABI.Pack("getSharedDataFromDB")
	result, err := client.CallContract(context.Background(), ethereum.CallMsg{To: addr, Data: data}, nil)
	if err != nil {
		return 0, err
	}
	outputs, err := parsedABI.Unpack("getSharedDataFromDB", result)
	if err != nil {
		return 0, err
	}
	if len(outputs) == 0 {
		return 0, fmt.Errorf("output rỗng")
	}
	val, ok := outputs[0].(*big.Int)
	if !ok {
		return 0, fmt.Errorf("kiểu trả về không phải *big.Int")
	}
	return val.Uint64(), nil
}

func waitReceipt(client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	for {
		receipt, err := client.TransactionReceipt(context.Background(), txHash)
		if err == nil && receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.Uint64() > 0 {
			return receipt, nil
		}
		if err != nil && err.Error() != "not found" {
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitForTxHashesByBlock(client *ethclient.Client, txHashes []common.Hash, startBlock uint64, rpcNodes map[string]string) (int, error) {
	pending := make(map[common.Hash]bool)
	for _, h := range txHashes {
		if h != (common.Hash{}) {
			pending[h] = true
		}
	}

	if len(pending) == 0 {
		return 0, nil
	}

	lastChecked := startBlock
	totalSuccess := 0
	totalTxs := len(pending)

	fmt.Printf("   [Info] Đang chờ %d giao dịch từ block %d...\n", totalTxs, lastChecked)

	startTime := time.Now()     // Tổng thời gian chờ toàn bộ round
	lastBlockTime := time.Now() // Thời gian block mới nhất được tìm thấy
	lastLogTime := time.Now()
	var currentLatestBlock uint64 = lastChecked
	var lastSeenBlock uint64 = lastChecked

	for len(pending) > 0 {
		// Timeout 4 phút PER BLOCK: nếu không có block mới nào trong 4 phút → timeout
		waitingForBlock := time.Since(lastBlockTime)
		if waitingForBlock > 4*time.Minute {
			fmt.Printf("\n⏰ [TIMEOUT 4 PHÚT / BLOCK] Không có block mới trong 4 phút! Còn %d/%d giao dịch chưa được đóng block.\n", len(pending), totalTxs)
			fmt.Printf("   📊 Tổng thời gian đã chờ: %v | Thời gian chờ block hiện tại (block %d): %v\n",
				time.Since(startTime).Round(time.Second), lastSeenBlock, waitingForBlock.Round(time.Second))
			fmt.Println("🛑 Dừng chương trình. Dưới đây là danh sách 5 giao dịch chưa xử lý để debug:")
			fmt.Println("--------------------------------------------------------------------------------")

			count := 0
			for txHash := range pending {
				count++
				fmt.Printf("\n🔍 [%d/5] Unconfirmed TxHash: %s\n", count, txHash.Hex())
				if len(rpcNodes) > 0 {
					for nodeName, nodeUrl := range rpcNodes {
						fmt.Printf("   👉 curl (%s): curl -s -X POST -H \"Content-Type: application/json\" --data '{\"jsonrpc\":\"2.0\",\"method\":\"eth_getTransactionReceipt\",\"params\":[\"%s\"],\"id\":1}' %s\n", nodeName, txHash.Hex(), nodeUrl)
					}
				} else {
					fmt.Printf("   👉 curl: curl -s -X POST -H \"Content-Type: application/json\" --data '{\"jsonrpc\":\"2.0\",\"method\":\"eth_getTransactionReceipt\",\"params\":[\"%s\"],\"id\":1}' http://127.0.0.1:10746\n", txHash.Hex())
				}

				if count >= 5 {
					break
				}
			}
			fmt.Println("--------------------------------------------------------------------------------")
			return totalSuccess, fmt.Errorf("timeout 4 phút chờ block mới (block cuối: %d)", lastSeenBlock)
		}

		header, err := client.HeaderByNumber(context.Background(), nil)
		if err != nil {
			fmt.Printf("   [Error] HeaderByNumber lỗi: %v. Sẽ thử lại sau...\n", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		latestBlock := header.Number.Uint64()

		// Có block mới → reset timer chờ block
		if latestBlock > currentLatestBlock {
			lastBlockTime = time.Now()
			lastSeenBlock = latestBlock
		}
		currentLatestBlock = latestBlock

		for b := lastChecked; b <= latestBlock; b++ {
			var block *types.Block
			var err error
			for retry := 0; retry < 3; retry++ {
				block, err = client.BlockByNumber(context.Background(), big.NewInt(int64(b)))
				if err == nil {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			if err != nil {
				fmt.Printf("   [Error] Không thể lấy block %d: %v. Dừng việc quét các block tiếp theo và sẽ thử lại!\n", b, err)
				break
			}

			txs := block.Transactions()
			foundInBlock := 0
			for _, tx := range txs {
				if pending[tx.Hash()] {
					delete(pending, tx.Hash())
					totalSuccess++
					foundInBlock++
				}
			}
			if foundInBlock > 0 {
				fmt.Printf("   [Info] Block %d chứa %d giao dịch của round này (còn lại: %d)\n", b, foundInBlock, len(pending))
			}
			lastChecked = b + 1
		}

		if len(pending) > 0 {
			if time.Since(lastLogTime) > 3*time.Second {
				waitingForBlock = time.Since(lastBlockTime)
				fmt.Printf("   [⏳ Waiting] Đã confirm %d/%d txs... | Tổng chờ: %v | Chờ block %d: %v\n",
					totalSuccess, totalTxs,
					time.Since(startTime).Round(time.Second),
					currentLatestBlock,
					waitingForBlock.Round(time.Second))
				lastLogTime = time.Now()
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	return totalSuccess, nil
}
