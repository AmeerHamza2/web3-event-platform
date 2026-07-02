// Package margin is a simplified portfolio margin engine: it reads an account's
// on-chain token balances, values them against price oracles, applies per-asset
// collateral factors, and computes a health factor and margin status.
//
// This models the core of a DeFi prime broker's risk layer. On-chain access
// (balances, prices) sits behind interfaces so the valuation and risk math are
// unit-testable without a node.
package margin

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// ContractCaller is the read-only contract-call surface (eth_call).
// *ethclient.Client satisfies it.
type ContractCaller interface {
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

const erc20BalanceOfABI = `[{
	"name": "balanceOf",
	"type": "function",
	"stateMutability": "view",
	"inputs":  [{"name": "account", "type": "address"}],
	"outputs": [{"name": "", "type": "uint256"}]
}]`

// BalanceReader reads an account's balance of an ERC-20 token (base units).
type BalanceReader interface {
	BalanceOf(ctx context.Context, token, account common.Address) (*big.Int, error)
}

// ChainBalanceReader reads balances via eth_call to the token's balanceOf.
type ChainBalanceReader struct {
	caller ContractCaller
	abi    abi.ABI
}

func NewChainBalanceReader(caller ContractCaller) (*ChainBalanceReader, error) {
	parsed, err := abi.JSON(strings.NewReader(erc20BalanceOfABI))
	if err != nil {
		return nil, fmt.Errorf("parse erc20 abi: %w", err)
	}
	return &ChainBalanceReader{caller: caller, abi: parsed}, nil
}

func (r *ChainBalanceReader) BalanceOf(ctx context.Context, token, account common.Address) (*big.Int, error) {
	data, err := r.abi.Pack("balanceOf", account)
	if err != nil {
		return nil, err
	}
	out, err := r.caller.CallContract(ctx, ethereum.CallMsg{To: &token, Data: data}, nil)
	if err != nil {
		return nil, fmt.Errorf("call balanceOf: %w", err)
	}
	vals, err := r.abi.Unpack("balanceOf", out)
	if err != nil {
		return nil, fmt.Errorf("decode balanceOf: %w", err)
	}
	bal, ok := vals[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected balanceOf return type")
	}
	return bal, nil
}

// StaticBalances is a fixed BalanceReader for tests.
type StaticBalances map[common.Address]*big.Int

func (s StaticBalances) BalanceOf(_ context.Context, token, _ common.Address) (*big.Int, error) {
	if b, ok := s[token]; ok {
		return b, nil
	}
	return big.NewInt(0), nil
}
