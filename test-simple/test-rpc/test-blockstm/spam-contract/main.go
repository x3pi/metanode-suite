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
	"net/url"
	"os"
	"sort"
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

type BlockEpochInfo struct {
	BlockNumber uint64
	Epoch       uint64
	BlockHash   common.Hash
}

func getBlockAndEpoch(rpcURL string) (BlockEpochInfo, error) {
	payload := strings.NewReader(`{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest", false],"id":1}`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", rpcURL, payload)
	if err != nil {
		return BlockEpochInfo{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return BlockEpochInfo{}, err
	}
	defer resp.Body.Close()

	var res struct {
		Result struct {
			Number *hexutil.Uint64 `json:"number"`
			Epoch  *hexutil.Uint64 `json:"epoch"`
			Hash   common.Hash     `json:"hash"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return BlockEpochInfo{}, err
	}
	info := BlockEpochInfo{
		BlockHash: res.Result.Hash,
	}
	if res.Result.Number != nil {
		info.BlockNumber = uint64(*res.Result.Number)
	}
	if res.Result.Epoch != nil {
		info.Epoch = uint64(*res.Result.Epoch)
	}
	return info, nil
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
const xapianAbiJSON = `[
  {
    "inputs": [],
    "stateMutability": "nonpayable",
    "type": "constructor"
  },
  {
    "anonymous": false,
    "inputs": [
      {
        "indexed": true,
        "internalType": "address",
        "name": "wallet",
        "type": "address"
      },
      {
        "indexed": false,
        "internalType": "uint256",
        "name": "newCounter",
        "type": "uint256"
      },
      {
        "indexed": false,
        "internalType": "uint256",
        "name": "docId",
        "type": "uint256"
      }
    ],
    "name": "SharedUpdated",
    "type": "event"
  },
  {
    "inputs": [],
    "name": "getSharedDataFromDB",
    "outputs": [
      {
        "internalType": "uint256",
        "name": "",
        "type": "uint256"
      }
    ],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "incrementShared",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "initializeDoc",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [],
    "name": "sharedDocId",
    "outputs": [
      {
        "internalType": "uint256",
        "name": "",
        "type": "uint256"
      }
    ],
    "stateMutability": "view",
    "type": "function"
  }
]`

const xapianBytecodeHex = "0x608060405234801561000f575f5ffd5b5061010773ffffffffffffffffffffffffffffffffffffffff1663fbdddaf06040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152506040518263ffffffff1660e01b81526004016100809190610136565b6020604051808303815f875af115801561009c573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906100c0919061018f565b506101ba565b5f81519050919050565b5f82825260208201905092915050565b8281835e5f83830152505050565b5f601f19601f8301169050919050565b5f610108826100c6565b61011281856100d0565b93506101228185602086016100e0565b61012b816100ee565b840191505092915050565b5f6020820190508181035f83015261014e81846100fe565b905092915050565b5f5ffd5b5f8115159050919050565b61016e8161015a565b8114610178575f5ffd5b50565b5f8151905061018981610165565b92915050565b5f602082840312156101a4576101a3610156565b5b5f6101b18482850161017b565b91505092915050565b610928806101c75f395ff3fe608060405234801561000f575f5ffd5b506004361061004a575f3560e01c80630b6f8f481461004e5780638cc382961461006c578063b4340bbe1461008a578063d32a9a5914610094575b5f5ffd5b61005661009e565b60405161006391906104b7565b60405180910390f35b6100746101b5565b60405161008191906104b7565b60405180910390f35b6100926101ba565b005b61009c610292565b005b5f5f61010773ffffffffffffffffffffffffffffffffffffffff16633e7c7f8b6040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f546040518363ffffffff1660e01b8152600401610113929190610540565b5f604051808303815f875af115801561012e573d5f5f3e3d5ffd5b505050506040513d5f823e3d601f19601f82011682018060405250810190610156919061069d565b90505f81511161019b576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101929061072e565b60405180910390fd5b808060200190518101906101af9190610776565b91505090565b5f5481565b61010773ffffffffffffffffffffffffffffffffffffffff16639d4799416040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f60405160200161021f91906104b7565b6040516020818303038152906040526040518363ffffffff1660e01b815260040161024b9291906107f3565b6020604051808303815f875af1158015610267573d5f5f3e3d5ffd5b505050506040513d601f19601f8201168201806040525081019061028b9190610776565b5f81905550565b5f61010773ffffffffffffffffffffffffffffffffffffffff16633e7c7f8b6040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f546040518363ffffffff1660e01b8152600401610306929190610540565b5f604051808303815f875af1158015610321573d5f5f3e3d5ffd5b505050506040513d5f823e3d601f19601f82011682018060405250810190610349919061069d565b90505f818060200190518101906103609190610776565b905060018161036f9190610855565b905061010773ffffffffffffffffffffffffffffffffffffffff1663c5d187dd6040518060400160405280601681526020017f626c6f636b73746d5f7368617265645f78617069616e000000000000000000008152505f54846040516020016103d891906104b7565b6040516020818303038152906040526040518463ffffffff1660e01b815260040161040593929190610888565b6020604051808303815f875af1158015610421573d5f5f3e3d5ffd5b505050506040513d601f19601f820116820180604052508101906104459190610776565b5f819055503373ffffffffffffffffffffffffffffffffffffffff167f3d523d852046afd5dba341b55c89c4355e15f6c45d6785c48472c0b7eb922911825f546040516104939291906108cb565b60405180910390a25050565b5f819050919050565b6104b18161049f565b82525050565b5f6020820190506104ca5f8301846104a8565b92915050565b5f81519050919050565b5f82825260208201905092915050565b8281835e5f83830152505050565b5f601f19601f8301169050919050565b5f610512826104d0565b61051c81856104da565b935061052c8185602086016104ea565b610535816104f8565b840191505092915050565b5f6040820190508181035f8301526105588185610508565b905061056760208301846104a8565b9392505050565b5f604051905090565b5f5ffd5b5f5ffd5b5f5ffd5b5f5ffd5b7f4e487b71000000000000000000000000000000000000000000000000000000005f52604160045260245ffd5b6105bd826104f8565b810181811067ffffffffffffffff821117156105dc576105db610587565b5b80604052505050565b5f6105ee61056e565b90506105fa82826105b4565b919050565b5f67ffffffffffffffff82111561061957610618610587565b5b610622826104f8565b9050602081019050919050565b5f61064161063c846105ff565b6105e5565b90508281526020810184848401111561065d5761065c610583565b5b6106688482856104ea565b509392505050565b5f82601f8301126106845761068361057f565b5b815161069484826020860161062f565b91505092915050565b5f602082840312156106b2576106b1610577565b5b5f82015167ffffffffffffffff8111156106cf576106ce61057b565b5b6106db84828501610670565b91505092915050565b7f44617461206e6f7420666f756e6420696e2058617069616e00000000000000005f82015250565b5f6107186018836104da565b9150610723826106e4565b602082019050919050565b5f6020820190508181035f8301526107458161070c565b9050919050565b6107558161049f565b811461075f575f5ffd5b50565b5f815190506107708161074c565b92915050565b5f6020828403121561078b5761078a610577565b5b5f61079884828501610762565b91505092915050565b5f81519050919050565b5f82825260208201905092915050565b5f6107c5826107a1565b6107cf81856107ab565b93506107df8185602086016104ea565b6107e8816104f8565b840191505092915050565b5f6040820190508181035f83015261080b8185610508565b9050818103602083015261081f81846107bb565b90509392505050565b7f4e487b71000000000000000000000000000000000000000000000000000000005f52601160045260245ffd5b5f61085f8261049f565b915061086a8361049f565b925082820190508082111561088257610881610828565b5b92915050565b5f6060820190508181035f8301526108a08186610508565b90506108af60208301856104a8565b81810360408301526108c181846107bb565b9050949350505050565b5f6040820190506108de5f8301856104a8565b6108eb60208301846104a8565b939250505056fea2646970667358221220d92a869bfc8dd4924b06987339722c5496b5592ac9c3625305bcc545679ef75764736f6c63430008220033"

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

type NodeTarget struct {
	Name   string
	URL    string
	Role   string
	Client *ethclient.Client
}

type Config struct {
	RPCUrl    string                  `json:"rpc_url"`
	RPCNodes  map[string]string       `json:"rpc_nodes"`
	SyncNodes map[string]string       `json:"sync_nodes"`
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
	checkAddr := flag.String("check", "", "Contract Address để kiểm tra state độc lập trên các node (bỏ qua deploy)")
	flag.Parse()

	log.Println("==========================================================")
	if *useXapian {
		if *useParallel {
			log.Println("BÀI TEST: 1-update-same-contract (CHẾ ĐỘ XAPIAN DB - SONG SONG KHÔNG XUNG ĐỘT)")
			log.Println("==========================================================")
			log.Println("📖 MÔ TẢ   : Gửi nhiều tx cùng lúc cập nhật độc lập từng key trên Xapian DB.")
			log.Println("⚡ GỌI     : Giao dịch gọi hàm incrementUser() từ các ví.")
			log.Println("🎯 KỲ VỌNG : Block-STM phát hiện KHÔNG CÓ read/write conflict trên Xapian Document, xử lý song song thành công.")
		} else {
			log.Println("BÀI TEST: 1-update-same-contract (CHẾ ĐỘ XAPIAN DB - CHUNG 1 GIÁ TRỊ)")
			log.Println("==========================================================")
			log.Println("📖 MÔ TẢ   : Gửi nhiều tx cùng lúc cập nhật chung 1 giá trị trên Xapian DB.")
			log.Println("⚡ GỌI     : Giao dịch gọi hàm incrementShared() từ các ví.")
			log.Println("🎯 KỲ VỌNG : Block-STM phải phát hiện read/write conflict trên Xapian Document, re-execute và cho kết quả đúng.")
		}
	} else {
		if *useParallel {
			log.Println("BÀI TEST: 1-update-same-contract (CHẾ ĐỘ EVM STATE - SONG SONG KHÔNG XUNG ĐỘT)")
			log.Println("==========================================================")
			log.Println("📖 MÔ TẢ   : Gửi nhiều giao dịch (tx) cùng lúc để gọi hàm update (ghi mapping độc lập) trên 1 Smart Contract.")
			log.Println("⚡ GỌI     : Giao dịch gọi hàm updateIfPhase1() từ các ví.")
			log.Println("🎯 KỲ VỌNG : Block-STM phát hiện KHÔNG CÓ read/write conflict, toàn bộ txs chạy song song thành công mượt mà.")
		} else {
			log.Println("BÀI TEST: 1-update-same-contract (CHẾ ĐỘ EVM STATE - UPDATE CHUNG BIẾN)")
			log.Println("==========================================================")
			log.Println("📖 MÔ TẢ   : Gửi nhiều giao dịch (tx) cùng lúc để gọi hàm update (tăng biến count) trên cùng một Smart Contract.")
			log.Println("⚡ GỌI     : Giao dịch gọi hàm EVM update state (increment) trên 1 contract duy nhất.")
			log.Println("🎯 KỲ VỌNG : Block-STM phải phát hiện read/write conflict, abort và re-execute để đảm bảo tính tuần tự.")
		}
	}
	log.Println("==========================================================")
	log.Println("🚀 KẾT QUẢ THỰC THI:")

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
		var nodeKeys []string
		for k := range cfg.RPCNodes {
			nodeKeys = append(nodeKeys, k)
		}
		sort.Strings(nodeKeys)
		for _, name := range nodeKeys {
			url := cfg.RPCNodes[name]
			if c, e := dialOptimizedClient(url); e == nil {
				rpcClients = append(rpcClients, c)
			} else {
				log.Printf("⚠️ Lỗi kết nối validator node %s (%s): %v", name, url, e)
			}
		}
	}
	if len(rpcClients) == 0 {
		if *multiNodes {
			log.Println("⚠️ Không cấu hình rpc_nodes trong config.json hoặc kết nối lỗi, fallback về RPC mặc định")
		}
		rpcClients = append(rpcClients, client)
	} else {
		log.Printf("🌐 Đã kết nối tới %d Validator RPC nodes (để gửi giao dịch)", len(rpcClients))
	}

	var checkNodes []NodeTarget
	nodeRoleMap := make(map[string]string)
	urlMap := make(map[string]string)

	for k, v := range cfg.RPCNodes {
		urlMap[k] = v
		nodeRoleMap[k] = "Validator"
	}
	for k, v := range cfg.SyncNodes {
		urlMap[k] = v
		nodeRoleMap[k] = "SyncOnly"
	}
	if len(urlMap) == 0 {
		urlMap["m0"] = cfg.RPCUrl
		nodeRoleMap["m0"] = "Validator"
	}

	var allKeys []string
	for k := range urlMap {
		allKeys = append(allKeys, k)
	}
	sort.Strings(allKeys)

	for _, name := range allKeys {
		url := urlMap[name]
		role := nodeRoleMap[name]

		if c, e := dialOptimizedClient(url); e == nil {
			checkNodes = append(checkNodes, NodeTarget{
				Name:   name,
				URL:    url,
				Role:   role,
				Client: c,
			})
		} else {
			log.Printf("⚠️ Lỗi kết nối check node %s (%s): %v", name, url, e)
		}
	}
	log.Printf("🔍 Đã thiết lập %d nodes để kiểm tra đồng bộ (Validator + SyncOnly)", len(checkNodes))

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

	if *checkAddr != "" {
		cAddr := common.HexToAddress(*checkAddr)
		log.Printf("🔍 Đang kiểm tra state của Contract: %s", cAddr.Hex())
		for _, node := range checkNodes {
			var actual uint64
			var err error
			if *useXapian {
				if *useParallel {
					val, e := getUserDataFromDB(node.Client, &cAddr, parsedABI, from0)
					if e == nil {
						actual = val.Uint64()
					}
					err = e
				} else {
					actual, err = getSharedDataFromDB(node.Client, &cAddr, parsedABI)
				}
			} else {
				if *useParallel {
					val, e := getUserDataEVM(node.Client, &cAddr, parsedABI, from0)
					if e == nil {
						actual = val.Uint64()
					}
					err = e
				} else {
					actual, err = getCount(node.Client, &cAddr, parsedABI)
				}
			}
			info, _ := getBlockAndEpoch(node.URL)
			if err != nil {
				log.Printf("   - Node %s [%-9s]: LỖI %v | Block: %d | Epoch: %d", node.Name, node.Role, err, info.BlockNumber, info.Epoch)
			} else {
				log.Printf("   - Node %s [%-9s]: State = %d | Block = %d | Epoch = %d (Hash: %s)", node.Name, node.Role, actual, info.BlockNumber, info.Epoch, info.BlockHash.Hex())
			}
		}
		os.Exit(0)
	}

	log.Println("🚀 Deploying contract with Account 0...")
	contractAddr, err := deployContract(client, pk0, cfg.ChainID, from0, bytecode)
	if err != nil {
		log.Fatalf("❌ Deploy thất bại: %v", err)
	}
	log.Printf("📌 Contract deployed at: %s\n", contractAddr.Hex())

	if *useXapian {
		log.Println("⚙️ Initializing Xapian Document...")
		initHash, err := sendInitializeDoc(client, pk0, cfg.ChainID, from0, contractAddr, parsedABI)
		if err != nil {
			log.Fatalf("❌ InitializeDoc failed: %v", err)
		}
		log.Printf("   TX Hash: %s", initHash.Hex())

		initReceipt, err := waitReceipt(client, initHash)
		if err != nil || initReceipt.Status != 1 {
			log.Fatalf("❌ Khởi tạo Document thất bại!")
		}
		log.Printf("✅ InitializeDoc thành công!\n")
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
	log.Printf("💰 Số dư ví 0 ban đầu: %s wei", balanceBefore.String())

	log.Printf("⏳ Đang tải nonce ban đầu cho %d ví...", len(testKeys))
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
	log.Printf("✅ Đã tải xong nonce ban đầu cho tất cả ví!\n")

	for r := 1; r <= *rounds; r++ {
		log.Printf("\n🔥 --- ROUND %d/%d --- 🔥", r, *rounds)
		if *useXapian {
			if *useParallel {
				log.Printf("🔥 Gửi %d giao dịch đồng thời để update Xapian DB (Song song không xung đột)...", len(testKeys))
			} else {
				log.Printf("🔥 Gửi %d giao dịch đồng thời để update Xapian DB (Xung đột shared)...", len(testKeys))
			}
		} else {
			if *useParallel {
				log.Printf("🔥 Gửi %d giao dịch đồng thời để update EVM State contract (Song song không xung đột)...", len(testKeys))
			} else {
				log.Printf("🔥 Gửi %d giao dịch đồng thời để update EVM State contract (Xung đột shared)...", len(testKeys))
			}
		}

		startInfo, err := getBlockAndEpoch(cfg.RPCUrl)
		if err != nil {
			header, errH := client.HeaderByNumber(context.Background(), nil)
			if errH != nil {
				log.Fatalf("❌ Lỗi lấy startBlock round %d: %v", r, errH)
			}
			startInfo.BlockNumber = header.Number.Uint64()
		}
		startBlock := startInfo.BlockNumber
		startEpoch := startInfo.Epoch

		log.Printf("📌 [BẮT ĐẦU ROUND %d] Gửi giao dịch tại Block: %d | Epoch: %d (Hash: %s)", r, startBlock, startEpoch, startInfo.BlockHash.Hex())

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
						time.Sleep(100 * time.Millisecond)
						
		
					}
				}

				if err != nil {
					errsMu.Lock()
					log.Printf("❌ Round %d - Wallet %d gửi tx thất bại (sau %d lần thử): %v", r, idx, maxRetry, err)
					errs = append(errs, fmt.Errorf("round %d - lỗi send tx từ wallet %d (sau %d lần thử): %v", r, idx, maxRetry, err))
					errsMu.Unlock()
					return
				}

				log.Printf("✅ Round %d - Wallet %d gửi tx thành công: %s", r, idx, hash.Hex())
				txHashes[idx] = hash
			}(i, pkStr)
		}

		wg.Wait()

		var errCount int
		if len(errs) > 0 {
			errsMu.Lock()
			errCount = len(errs)
			log.Printf("❌ Round %d: %d/%d giao dịch gửi thất bại (sau 3 lần retry):", r, errCount, len(testKeys))
			for _, e := range errs {
				log.Println("  -", e)
			}
			errs = nil
			errsMu.Unlock()
			log.Printf("🚨 [BỎ QUA LỖI] Round %d có %d tx thất bại sau 3 lần retry. Chuyển sang bước đợi block cho các tx thành công...", r, errCount)
		}

		var roundSuccess int
		if *waitMethod == "receipt" {
			log.Println("⏳ Chờ các giao dịch được confirm bằng cách lấy Receipt...")
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
						currentInfo, _ := getBlockAndEpoch(cfg.RPCUrl)
						log.Printf("   [⏳ Waiting Receipt] Đã confirm %d/%d txs... | Chờ: %v | Current: Block %d, Epoch %d", current, len(txHashes), time.Since(startTime).Round(time.Second), currentInfo.BlockNumber, currentInfo.Epoch)
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
			log.Printf("✅ Đã confirm %d/%d giao dịch bằng Receipt trong round %d", roundSuccess, len(txHashes), r)
		} else {
			log.Println("⏳ Chờ các giao dịch được confirm bằng cách quét Block...")
			successCount, err := waitForTxHashesByBlock(client, txHashes, startBlock, startEpoch, cfg.RPCNodes, cfg.RPCUrl)
			if err != nil {
				failInfo, _ := getBlockAndEpoch(cfg.RPCUrl)
				log.Printf("❌ Lỗi khi chờ block round %d (Bắt đầu: Block %d | Epoch %d -> Hiện tại: Block %d | Epoch %d): %v", r, startBlock, startEpoch, failInfo.BlockNumber, failInfo.Epoch, err)
				log.Fatalf("🚨 Dừng chương trình do Timeout khi chờ confirm block round %d!", r)
			}
			roundSuccess = successCount
			totalSuccess += roundSuccess
			log.Printf("✅ Đã confirm %d/%d giao dịch bằng quét Block trong round %d", roundSuccess, len(txHashes), r)
			log.Println("⏳ Đợi thêm 20s để đảm bảo State DB commit xong trước khi chạy round tiếp theo...")
			time.Sleep(20 * time.Second)
		}

		var isPassed bool = false
		var checkErr error
		var failedNodes []int
		var nodeResults []uint64
		var nodeBlocks []uint64
		var nodeEpochs []uint64

		syncTimeout := 4 * time.Minute
		syncStart := time.Now()
		firstCheck := true

		for {
			isPassed = true
			failedNodes = nil
			nodeResults = nil
			nodeBlocks = nil
			nodeEpochs = nil
			checkErr = nil

			var maxBlock uint64 = 0

			for nodeIdx, node := range checkNodes {
				var roundActual uint64
				var nodePassed bool

				info, errInfo := getBlockAndEpoch(node.URL)
				var blockNum, epochNum uint64
				if errInfo == nil {
					blockNum = info.BlockNumber
					epochNum = info.Epoch
				} else {
					ctxB, cancelB := context.WithTimeout(context.Background(), 5*time.Second)
					bn, errB := node.Client.BlockNumber(ctxB)
					cancelB()
					if errB != nil {
						log.Printf("⚠️ Lỗi lấy BlockNumber node %s (%s): %v", node.Name, node.Role, errB)
					}
					blockNum = bn
				}
				nodeBlocks = append(nodeBlocks, blockNum)
				nodeEpochs = append(nodeEpochs, epochNum)
				if blockNum > maxBlock {
					maxBlock = blockNum
				}

				if *useXapian {
					if *useParallel {
						userVal, err := getUserDataFromDB(node.Client, contractAddr, parsedABI, from0)
						if err != nil {
							log.Printf("⚠️ Đang thử đọc state Xapian Parallel round %d từ node %s (%s): %v", r, node.Name, node.Role, err)
							checkErr = err
						} else {
							roundActual = userVal.Uint64()
							nodePassed = (roundActual == uint64(r))
						}
					} else {
						var err error
						roundActual, err = getSharedDataFromDB(node.Client, contractAddr, parsedABI)
						if err != nil {
							log.Printf("⚠️ Đang thử đọc state Xapian Shared round %d từ node %s (%s): %v", r, node.Name, node.Role, err)
							checkErr = err
						} else {
							nodePassed = (uint64(totalSuccess) == roundActual)
						}
					}
				} else {
					if *useParallel {
						userVal, err := getUserDataEVM(node.Client, contractAddr, parsedABI, from0)
						if err != nil {
							log.Printf("⚠️ Đang thử đọc state EVM Parallel round %d từ node %s (%s): %v", r, node.Name, node.Role, err)
							checkErr = err
						} else {
							roundActual = userVal.Uint64()
							nodePassed = (roundActual == 1)
						}
					} else {
						var err error
						roundActual, err = getCount(node.Client, contractAddr, parsedABI)
						if err != nil {
							log.Printf("⚠️ Đang thử đọc state EVM count round %d từ node %s (%s): %v", r, node.Name, node.Role, err)
							checkErr = err
						} else {
							nodePassed = (uint64(totalSuccess) == roundActual)
						}
					}
				}

				nodeResults = append(nodeResults, roundActual)
				if checkErr != nil || !nodePassed {
					isPassed = false
					failedNodes = append(failedNodes, nodeIdx)
				}
			}

			// Kiểm tra xem tất cả các node đã đồng bộ block chưa
			allBlocksSynced := true
			for _, b := range nodeBlocks {
				if b < maxBlock {
					allBlocksSynced = false
					break
				}
			}

			// Nếu tất cả node đều pass và block đã đồng bộ -> hoàn thành kiểm tra round này
			if isPassed && allBlocksSynced {
				break
			}

			// Nếu chưa pass hoặc block chưa đồng bộ, kiểm tra xem còn trong thời gian timeout 4 phút không
			if time.Since(syncStart) >= syncTimeout {
				log.Printf("⏱️ [TIMEOUT 4 PHÚT] Đã chờ 4 phút nhưng các node vẫn chưa đồng bộ hoàn toàn!")
				break
			}

			if firstCheck {
				log.Printf("⏳ Phát hiện có node chưa đồng bộ block hoặc state (Block cao nhất: %d, Nodes chưa khớp: %v). Bắt đầu chờ tối đa 4 phút...", maxBlock, failedNodes)
				firstCheck = false
			} else {
				log.Printf("   [⏳ Sync Waiting %v/4m] Block cao nhất: %d | Nodes chưa khớp: %v (Đang thử lại sau 5s...)", time.Since(syncStart).Round(time.Second), maxBlock, failedNodes)
			}
			time.Sleep(5 * time.Second)
		}

		if checkErr != nil {
			failInfo, _ := getBlockAndEpoch(cfg.RPCUrl)
			log.Printf("❌ Lỗi đọc state sau round %d (Bắt đầu: Block %d | Epoch %d -> Hiện tại: Block %d | Epoch %d): %v", r, startBlock, startEpoch, failInfo.BlockNumber, failInfo.Epoch, checkErr)
			teleMsg := fmt.Sprintf("🚨 <b>[SOAK TEST LỖI ROUND %d]</b>\nLỗi đọc state hoặc mất kết nối tới node sau 4 phút timeout!\nBắt đầu: Block %d, Epoch %d | Hiện tại: Block %d, Epoch %d\nLỗi: <code>%v</code>\nNodes lỗi: %v\nContract: <code>%s</code>", r, startBlock, startEpoch, failInfo.BlockNumber, failInfo.Epoch, checkErr, failedNodes, contractAddr.Hex())
			sendTelegramAlert(teleMsg)
			log.Fatalf("🚨 Dừng chương trình ngay lập tức do lỗi đọc state/kết nối ở Round %d: %v", r, checkErr)
		} else {
			log.Printf("\n📊 KẾT QUẢ ROUND %d (Đã check %d nodes):", r, len(checkNodes))
			log.Printf("   - Số tx thành công round này : %d", roundSuccess)
			log.Printf("   - Tổng tx thành công đến hiện tại: %d", totalSuccess)

			var expectedVal uint64
			var valName string
			if *useXapian {
				if *useParallel {
					expectedVal = uint64(r)
					valName = "Xapian DB ví 0"
				} else {
					expectedVal = uint64(totalSuccess)
					valName = "Xapian DB"
				}
			} else {
				if *useParallel {
					expectedVal = 1
					valName = "EVM ví 0"
				} else {
					expectedVal = uint64(totalSuccess)
					valName = "EVM count"
				}
			}

			log.Printf("   - Giá trị kỳ vọng (%s): %d", valName, expectedVal)
			log.Println("   - Kết quả trên từng node:")
			for i, val := range nodeResults {
				target := checkNodes[i]
				if val == expectedVal {
					log.Printf("       + Node %s [%-9s]: %d (✅ KHỚP) - Block: %d | Epoch: %d", target.Name, target.Role, val, nodeBlocks[i], nodeEpochs[i])
				} else {
					log.Printf("       + Node %s [%-9s]: %d (❌ LỆCH) - Block: %d | Epoch: %d", target.Name, target.Role, val, nodeBlocks[i], nodeEpochs[i])
				}
			}

			if isPassed {
				log.Printf("   => ✅ ROUND PASSED (Bắt đầu: Block %d | Epoch %d -> Kết thúc: Block %d | Epoch %d)", startBlock, startEpoch, nodeBlocks[0], nodeEpochs[0])
			} else {
				log.Printf("   => ❌ ROUND FAILED")
				log.Println("================================================--------------------------------")
				log.Printf("🚨 [LỖI LỆCH KẾT QUẢ STATE TẠI ROUND %d] (Bắt đầu gửi lúc: Block %d | Epoch %d)", r, startBlock, startEpoch)
				for i, val := range nodeResults {
					target := checkNodes[i]
					log.Printf("   📌 Node %s [%s]: State=%d (kỳ vọng %d) | Block=%d | Epoch=%d", target.Name, target.Role, val, expectedVal, nodeBlocks[i], nodeEpochs[i])
				}
				log.Printf("📌 CONTRACT ADDRESS ĐỂ KIỂM TRA LẠI: %s", contractAddr.Hex())
				log.Println("================================================--------------------------------")
				teleMsg := fmt.Sprintf("🚨 <b>[SOAK TEST LỆCH KẾT QUẢ ROUND %d]</b>\nBắt đầu: Block %d, Epoch %d\nPhát hiện sai lệch state giữa các nodes sau 4 phút timeout!\nNodes sai: %v\nContract: <code>%s</code>", r, startBlock, startEpoch, failedNodes, contractAddr.Hex())
				sendTelegramAlert(teleMsg)
				log.Fatalf("🚨 Dừng chương trình ngay lập tức do phát hiện điểm sai ở Round %d!", r)
			}

			summaries = append(summaries, RoundSummary{
				Round:        r,
				SuccessTx:    roundSuccess,
				TotalSuccess: totalSuccess,
				DBValue:      nodeResults[0],
				Passed:       isPassed,
			})
		}
	}

	var actual uint64
	var testPassed bool
	var finalCheckErr error

	for _, node := range checkNodes {
		if *useXapian {
			if *useParallel {
				val, err := getUserDataFromDB(node.Client, contractAddr, parsedABI, from0)
				if err == nil {
					actual = val.Uint64()
					testPassed = (actual == uint64(*rounds))
				} else {
					finalCheckErr = err
				}
			} else {
				actual, err = getSharedDataFromDB(node.Client, contractAddr, parsedABI)
				if err == nil {
					testPassed = (actual == uint64(totalSuccess))
				} else {
					finalCheckErr = err
				}
			}
		} else {
			if *useParallel {
				val, err := getUserDataEVM(node.Client, contractAddr, parsedABI, from0)
				if err == nil {
					actual = val.Uint64()
					testPassed = (actual == 1)
				} else {
					finalCheckErr = err
				}
			} else {
				actual, err = getCount(node.Client, contractAddr, parsedABI)
				if err == nil {
					testPassed = (actual == uint64(totalSuccess))
				} else {
					finalCheckErr = err
				}
			}
		}

		if finalCheckErr != nil {
			break
		}

		if !testPassed {
			break
		}
	}

	if finalCheckErr != nil {
		log.Fatalf("❌ Lỗi đọc state cuối cùng: %v", finalCheckErr)
	}

	elapsed := time.Since(start)

	balanceAfter, err := client.BalanceAt(context.Background(), from0, nil)
	if err != nil {
		log.Fatalf("❌ Lỗi lấy số dư cuối: %v", err)
	}

	totalCost := new(big.Int).Sub(balanceBefore, balanceAfter)
	log.Printf("\n💰 THỐNG KÊ CHI PHÍ GAS VÍ 0:")
	log.Printf("   - Số dư ban đầu: %s wei", balanceBefore.String())
	log.Printf("   - Số dư sau cùng: %s wei", balanceAfter.String())
	log.Printf("   - Tổng phí đã trừ (Gas Cost): %s wei", totalCost.String())

	if totalCost.Cmp(big.NewInt(0)) == 0 {
		log.Fatalf("\n❌ [LỖI NGHIÊM TRỌNG] PHÍ GAS KHÔNG BỊ TRỪ! Giao dịch Smart Contract không trừ phí từ Sender!")
	} else {
		log.Printf("   => ✅ Phí Gas đã được trừ hợp lệ!")
	}

	log.Println("\n📊 KẾT QUẢ:")
	log.Printf("Thời gian gửi & chờ: %v", elapsed)
	if *useXapian {
		if *useParallel {
			log.Printf("Giá trị counter ví 0 lưu trong Xapian DB: %d", actual)
		} else {
			log.Printf("Giá trị counter cuối cùng lưu trong Xapian DB: %d", actual)
		}
	} else {
		if *useParallel {
			log.Printf("Giá trị state ví 0 EVM: %d", actual)
		} else {
			log.Printf("Giá trị count EVM cuối cùng: %d", actual)
		}
	}
	log.Printf("Tổng số lượng tx thành công: %d (trên %d round, mỗi round %d ví)", totalSuccess, *rounds, len(testKeys))

	log.Println("\n📋 BẢNG TỔNG HỢP CÁC ROUND:")
	log.Println("-------------------------------------------------------------------------")
	log.Printf("%-10s | %-12s | %-14s | %-10s | %-10s", "Round", "Tx Success", "Total Success", "State Value", "Status")
	log.Println("-------------------------------------------------------------------------")
	for _, s := range summaries {
		status := "✅ PASSED"
		if !s.Passed {
			status = "⚠️ FAILED"
		}
		log.Printf("%-10d | %-12d | %-14d | %-10d | %-10s", s.Round, s.SuccessTx, s.TotalSuccess, s.DBValue, status)
	}
	log.Println("-------------------------------------------------------------------------")

	log.Println("\n🏁 KẾT LUẬN CUỐI CÙNG:")
	if testPassed {
		if *useParallel {
			log.Println("🎉 TEST PASSED: BlockSTM xử lý song song không xung đột thành công!")
		} else {
			log.Println("🎉 TEST PASSED: BlockSTM xử lý write conflict đúng!")
		}
	} else {
		log.Println("⚠️ TEST FAILED: Giá trị state không khớp với kỳ vọng")
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

	log.Printf("TX HASH: %s", signedTx.Hash().Hex())
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

	log.Printf("TX HASH: %s", signedTx.Hash().Hex())
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

	log.Printf("TX HASH: %s", signedTx.Hash().Hex())
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

	log.Printf("TX HASH: %s", signedTx.Hash().Hex())
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

	log.Printf("TX HASH: %s", signedTx.Hash().Hex())
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

	log.Printf("TX HASH: %s", signedTx.Hash().Hex())
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
	timeout := time.After(120 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-timeout:
			panic(fmt.Sprintf("timeout waiting for receipt tx %s", txHash.Hex()))
		case <-ticker.C:
			receipt, err := client.TransactionReceipt(context.Background(), txHash)

			if err != nil && !strings.Contains(err.Error(), "not found") {
				fmt.Printf("Lỗi kết nối RPC: %v\n", err)
				os.Exit(1)
			}
			if err == nil {
				return receipt, nil
			}
			if err.Error() != "not found" {
				return nil, err
			}
		}
	}
}

func sendTelegramAlert(msg string) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		token = "8230176859:AAGoZ_78xzb1q4rgJJ5SYLxRhZBYBTSz_xo"
	}
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if chatID == "" {
		chatID = "-1003867050625"
	}
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	resp, err := http.PostForm(apiURL, url.Values{
		"chat_id": {chatID},
		"text":    {msg},
	})
	if err == nil {
		resp.Body.Close()
	} else {
		log.Printf("Lỗi gửi Telegram: %v", err)
	}
}

func waitForTxHashesByBlock(client *ethclient.Client, txHashes []common.Hash, startBlock uint64, startEpoch uint64, rpcNodes map[string]string, mainRpcUrl string) (int, error) {
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

	log.Printf("   [Info] Đang chờ %d giao dịch từ Block: %d | Epoch: %d...", totalTxs, lastChecked, startEpoch)

	startTime := time.Now()     // Tổng thời gian chờ toàn bộ round
	lastBlockTime := time.Now() // Thời gian block mới nhất được tìm thấy
	lastLogTime := time.Now()
	var currentLatestBlock uint64 = lastChecked
	var lastSeenBlock uint64 = lastChecked

	var sent4MinWarning bool

	for len(pending) > 0 {
		waitingForBlock := time.Since(lastBlockTime)
		totalRoundTime := time.Since(startTime)

		// 1. TIMEOUT TỔNG 20 PHÚT CHO TOÀN BỘ ROUND
		if totalRoundTime > 20*time.Minute {
			log.Printf("\n⏰ [TIMEOUT 20 PHÚT / ROUND] Tổng thời gian chờ của round đã vượt quá 20 phút! Dừng chương trình.")
			log.Printf("📌 [THÔNG TIN ROUND] Bắt đầu gửi tại Block: %d | Epoch: %d (Đã trôi qua: %v)", startBlock, startEpoch, totalRoundTime.Round(time.Second))
			log.Printf("\n🔍 TIẾN HÀNH KIỂM TRA BLOCK VÀ EPOCH CỦA TẤT CẢ CÁC NODE TẠI THỜI ĐIỂM TIMEOUT 20 PHÚT:")
			var nodeBlocksInfo []string
			if len(rpcNodes) > 0 {
				var sortedNames []string
				for n := range rpcNodes {
					sortedNames = append(sortedNames, n)
				}
				sort.Strings(sortedNames)
				for _, nodeName := range sortedNames {
					nodeUrl := rpcNodes[nodeName]
					info, err := getBlockAndEpoch(nodeUrl)
					if err == nil {
						line := fmt.Sprintf("   📌 %s: Block %d | Epoch %d (Hash: %s)", nodeName, info.BlockNumber, info.Epoch, info.BlockHash.Hex())
						log.Println(line)
						nodeBlocksInfo = append(nodeBlocksInfo, line)
					} else {
						line := fmt.Sprintf("   📌 %s: Lỗi kết nối (%v)", nodeName, err)
						log.Println(line)
						nodeBlocksInfo = append(nodeBlocksInfo, line)
					}
				}
			}

			contractTarget := "Unknown"
			for txHash := range pending {
				tx, _, err := client.TransactionByHash(context.Background(), txHash)
				if err == nil && tx.To() != nil {
					contractTarget = tx.To().Hex()
				}
				break
			}

			timeoutMsg := fmt.Sprintf("🚨 [TIMEOUT ROUND 20 PHÚT] Bài test đã dừng do tổng thời gian round vượt quá 20 phút.\n👉 Bắt đầu gửi lúc: Block %d, Epoch %d\n👉 Contract đang gọi: %s\n\n📌 Trạng thái các node lúc timeout:\n%s", 
				startBlock, startEpoch, contractTarget, strings.Join(nodeBlocksInfo, "\n"))
			sendTelegramAlert(timeoutMsg)

			log.Println("\n🛑 Dừng chương trình. Dưới đây là danh sách 5 giao dịch chưa xử lý để debug:")
			log.Println("--------------------------------------------------------------------------------")
			count := 0
			for txHash := range pending {
				count++
				log.Printf("\n🔍 [%d/5] Unconfirmed TxHash: %s", count, txHash.Hex())
				if count >= 5 {
					break
				}
			}
			log.Println("--------------------------------------------------------------------------------")
			return totalSuccess, fmt.Errorf("timeout 20 phút cho toàn bộ round (bắt đầu: Block %d, Epoch %d -> block cuối: %d)", startBlock, startEpoch, lastSeenBlock)
		}
		
		// 2. TIMEOUT 8 PHÚT CHO 1 BLOCK MỚI
		if waitingForBlock > 8*time.Minute {
			log.Printf("\n⏰ [TIMEOUT 8 PHÚT / BLOCK] Không có block mới trong 8 phút! Dừng chương trình.")
			log.Printf("📌 [THÔNG TIN ROUND] Bắt đầu gửi tại Block: %d | Epoch: %d", startBlock, startEpoch)
			log.Printf("\n🔍 TIẾN HÀNH KIỂM TRA BLOCK VÀ EPOCH CỦA TẤT CẢ CÁC NODE TẠI THỜI ĐIỂM TIMEOUT 8 PHÚT:")
			var nodeBlocksInfo []string
			if len(rpcNodes) > 0 {
				var sortedNames []string
				for n := range rpcNodes {
					sortedNames = append(sortedNames, n)
				}
				sort.Strings(sortedNames)
				for _, nodeName := range sortedNames {
					nodeUrl := rpcNodes[nodeName]
					info, err := getBlockAndEpoch(nodeUrl)
					if err == nil {
						line := fmt.Sprintf("   📌 %s: Block %d | Epoch %d (Hash: %s)", nodeName, info.BlockNumber, info.Epoch, info.BlockHash.Hex())
						log.Println(line)
						nodeBlocksInfo = append(nodeBlocksInfo, line)
					} else {
						line := fmt.Sprintf("   📌 %s: Lỗi kết nối (%v)", nodeName, err)
						log.Println(line)
						nodeBlocksInfo = append(nodeBlocksInfo, line)
					}
				}
			}
			
			// Lấy địa chỉ contract từ tx pending đầu tiên nếu có thể
			contractTarget := "Unknown"
			for txHash := range pending {
				tx, _, err := client.TransactionByHash(context.Background(), txHash)
				if err == nil && tx.To() != nil {
					contractTarget = tx.To().Hex()
				}
				break
			}
			
			timeoutMsg := fmt.Sprintf("🚨 [TIMEOUT BLOCK 8 PHÚT] Bài test đã dừng do chờ 8 phút không có block mới.\n👉 Bắt đầu gửi lúc: Block %d, Epoch %d\n👉 Contract đang gọi: %s\n\n📌 Trạng thái các node lúc timeout:\n%s", 
				startBlock, startEpoch, contractTarget, strings.Join(nodeBlocksInfo, "\n"))
			sendTelegramAlert(timeoutMsg)

			log.Println("\n🛑 Dừng chương trình. Dưới đây là danh sách 5 giao dịch chưa xử lý để debug:")
			log.Println("--------------------------------------------------------------------------------")
			count := 0
			for txHash := range pending {
				count++
				log.Printf("\n🔍 [%d/5] Unconfirmed TxHash: %s", count, txHash.Hex())
				if count >= 5 {
					break
				}
			}
			log.Println("--------------------------------------------------------------------------------")
			return totalSuccess, fmt.Errorf("timeout 8 phút chờ block mới (bắt đầu: Block %d, Epoch %d -> block cuối: %d)", startBlock, startEpoch, lastSeenBlock)
		} else if waitingForBlock > 4*time.Minute && !sent4MinWarning {
			sent4MinWarning = true
			log.Printf("\n⏰ [CẢNH BÁO 4 PHÚT] Không có block mới trong 4 phút.")
			log.Printf("📌 [THÔNG TIN ROUND] Bắt đầu gửi tại Block: %d | Epoch: %d", startBlock, startEpoch)
			log.Printf("🔍 KIỂM TRA ĐỒNG BỘ CÁC NODE (Block & Epoch):")
			
			var nodeBlocksInfo []string
			var allSame = true
			var lastVal uint64
			var lastEpoch uint64
			var first = true

			if len(rpcNodes) > 0 {
				var sortedNames []string
				for n := range rpcNodes {
					sortedNames = append(sortedNames, n)
				}
				sort.Strings(sortedNames)
				for _, nodeName := range sortedNames {
					nodeUrl := rpcNodes[nodeName]
					info, err := getBlockAndEpoch(nodeUrl)
					if err == nil {
						line := fmt.Sprintf("   📌 %s: Block %d | Epoch %d (Hash: %s)", nodeName, info.BlockNumber, info.Epoch, info.BlockHash.Hex())
						log.Println(line)
						nodeBlocksInfo = append(nodeBlocksInfo, line)
						if first {
							lastVal = info.BlockNumber
							lastEpoch = info.Epoch
							first = false
						} else if info.BlockNumber != lastVal || info.Epoch != lastEpoch {
							allSame = false
						}
					} else {
						line := fmt.Sprintf("   📌 %s: Lỗi kết nối (%v)", nodeName, err)
						log.Println(line)
						nodeBlocksInfo = append(nodeBlocksInfo, line)
					}
				}
			}
			
			var teleMsg string
			if !allSame {
				teleMsg = fmt.Sprintf("⚠️ [CẢNH BÁO LỆCH NODE] Sau 4 phút chờ, các node đang KHÔNG đồng bộ block/epoch!\nBắt đầu gửi: Block %d, Epoch %d\nTiếp tục chờ thêm 4 phút...\n\n%s", startBlock, startEpoch, strings.Join(nodeBlocksInfo, "\n"))
			} else {
				teleMsg = fmt.Sprintf("⚠️ [CẢNH BÁO ĐỨNG BLOCK] Sau 4 phút chờ, các node ĐỀU DỪNG ở Block %d, Epoch %d!\nBắt đầu gửi: Block %d, Epoch %d\nTiếp tục chờ thêm 4 phút...\n\n%s", lastVal, lastEpoch, startBlock, startEpoch, strings.Join(nodeBlocksInfo, "\n"))
			}
			sendTelegramAlert(teleMsg)
		}

		header, err := client.HeaderByNumber(context.Background(), nil)
		if err != nil {
			log.Printf("   [Error] HeaderByNumber lỗi: %v. Sẽ thử lại sau...", err)
			time.Sleep(100 * time.Millisecond)
			
		
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
				log.Printf("   [Error] Không thể lấy block %d: %v. Dừng việc quét các block tiếp theo và sẽ thử lại!", b, err)
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
				log.Printf("   [Info] Block %d chứa %d giao dịch của round này (còn lại: %d)", b, foundInBlock, len(pending))
			}
			lastChecked = b + 1
		}

		if len(pending) > 0 {
			if time.Since(lastLogTime) > 3*time.Second {
				waitingForBlock = time.Since(lastBlockTime)
				curInfo, _ := getBlockAndEpoch(mainRpcUrl)
				log.Printf("   [⏳ Waiting] Đã confirm %d/%d txs... | Tổng chờ: %v | Chờ block %d: %v | Hiện tại: Block %d | Epoch %d\n",
					totalSuccess, totalTxs,
					time.Since(startTime).Round(time.Second),
					currentLatestBlock,
					waitingForBlock.Round(time.Second),
					curInfo.BlockNumber, curInfo.Epoch)
				lastLogTime = time.Now()
			}
			time.Sleep(100 * time.Millisecond)
			
		}
	}

	return totalSuccess, nil
}
