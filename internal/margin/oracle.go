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

// Chainlink AggregatorV3.latestRoundData — the standard price-feed read.
const aggregatorABI = `[{
	"name": "latestRoundData",
	"type": "function",
	"stateMutability": "view",
	"inputs": [],
	"outputs": [
		{"name": "roundId",         "type": "uint80"},
		{"name": "answer",          "type": "int256"},
		{"name": "startedAt",       "type": "uint256"},
		{"name": "updatedAt",       "type": "uint256"},
		{"name": "answeredInRound", "type": "uint80"}
	]
}]`

// Price is a USD price with its own decimal scale (Chainlink USD feeds use 8).
type Price struct {
	Value    *big.Int
	Decimals uint8
}

// Rat returns the price as an exact rational in USD.
func (p Price) Rat() *big.Rat {
	return new(big.Rat).SetFrac(p.Value, pow10(p.Decimals))
}

// PriceOracle returns the USD price for a given price feed.
type PriceOracle interface {
	Price(ctx context.Context, feed common.Address) (Price, error)
}

// ChainlinkOracle reads prices from Chainlink aggregators via eth_call.
type ChainlinkOracle struct {
	caller   ContractCaller
	abi      abi.ABI
	decimals uint8 // feed decimals (8 for Chainlink USD feeds)
}

func NewChainlinkOracle(caller ContractCaller, feedDecimals uint8) (*ChainlinkOracle, error) {
	parsed, err := abi.JSON(strings.NewReader(aggregatorABI))
	if err != nil {
		return nil, fmt.Errorf("parse aggregator abi: %w", err)
	}
	return &ChainlinkOracle{caller: caller, abi: parsed, decimals: feedDecimals}, nil
}

func (o *ChainlinkOracle) Price(ctx context.Context, feed common.Address) (Price, error) {
	data, err := o.abi.Pack("latestRoundData")
	if err != nil {
		return Price{}, err
	}
	out, err := o.caller.CallContract(ctx, ethereum.CallMsg{To: &feed, Data: data}, nil)
	if err != nil {
		return Price{}, fmt.Errorf("call latestRoundData: %w", err)
	}
	vals, err := o.abi.Unpack("latestRoundData", out)
	if err != nil {
		return Price{}, fmt.Errorf("decode latestRoundData: %w", err)
	}
	answer, ok := vals[1].(*big.Int) // outputs[1] = answer
	if !ok {
		return Price{}, fmt.Errorf("unexpected answer type")
	}
	if answer.Sign() <= 0 {
		return Price{}, fmt.Errorf("non-positive oracle price")
	}
	return Price{Value: answer, Decimals: o.decimals}, nil
}

// StaticOracle is a fixed PriceOracle for tests, keyed by feed address.
type StaticOracle map[common.Address]Price

func (s StaticOracle) Price(_ context.Context, feed common.Address) (Price, error) {
	if p, ok := s[feed]; ok {
		return p, nil
	}
	return Price{}, fmt.Errorf("no price for feed %s", feed)
}

func pow10(n uint8) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}
