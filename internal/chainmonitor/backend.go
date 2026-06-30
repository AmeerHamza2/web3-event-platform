package chainmonitor

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
)

// ChainBackend is the subset of the Ethereum RPC the monitor needs.
// *ethclient.Client satisfies it; tests provide a fake.
type ChainBackend interface {
	// HeaderByNumber returns the header at number, or the latest head when
	// number is nil.
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	// FilterLogs returns logs matching q.
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
}
