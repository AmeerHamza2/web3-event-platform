// Package chainmonitor follows an EVM chain: it indexes blocks and ERC-20
// Transfer logs into a Store, treats blocks as final only after a confirmation
// depth, detects and rolls back reorgs, and publishes domain events.
package chainmonitor

import (
	"context"
	"log/slog"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	"github.com/AmeerHamza2/web3-event-platform/pkg/events"
)

// Config tunes the monitor.
type Config struct {
	// Confirmations is the depth at which a block is considered final. Reorgs
	// are only handled within this window.
	Confirmations uint64
	// PollInterval is how often the head is polled.
	PollInterval time.Duration
	// StartBlock is where indexing begins when the store is empty.
	StartBlock uint64
	// Tokens optionally restricts Transfer indexing to these contracts; empty
	// means all ERC-20 Transfer logs.
	Tokens []common.Address
}

// Monitor indexes a chain into a Store and publishes events.
type Monitor struct {
	backend ChainBackend
	store   Store
	bus     events.Bus
	log     *slog.Logger
	decoder *transferDecoder
	cfg     Config
}

func New(backend ChainBackend, store Store, bus events.Bus, log *slog.Logger, cfg Config) (*Monitor, error) {
	dec, err := newTransferDecoder()
	if err != nil {
		return nil, err
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 12 * time.Second // ~Ethereum block time
	}
	return &Monitor{backend: backend, store: store, bus: bus, log: log, decoder: dec, cfg: cfg}, nil
}

// Run polls until ctx is cancelled. A sync error is logged and retried on the
// next tick rather than killing the loop.
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.PollInterval)
	defer ticker.Stop()
	for {
		if err := m.Sync(ctx); err != nil {
			m.log.Warn("sync failed", slog.Any("error", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Sync advances the index to the current head, handling reorgs. It is safe to
// call repeatedly; one call indexes whatever blocks are newly available.
func (m *Monitor) Sync(ctx context.Context) error {
	head, err := m.backend.HeaderByNumber(ctx, nil)
	if err != nil {
		return err
	}
	headNum := head.Number.Uint64()

	// Reorg check at the tip: if our last indexed block's hash no longer matches
	// the canonical header at that height, roll it back and re-sync next tick.
	last, ok, err := m.store.LastBlock(ctx)
	if err != nil {
		return err
	}
	if ok {
		canon, err := m.backend.HeaderByNumber(ctx, new(big.Int).SetUint64(last.Number))
		if err != nil {
			return err
		}
		if canon.Hash() != last.Hash {
			return m.rollback(ctx, last.Number)
		}
	}

	from := m.cfg.StartBlock
	if ok {
		from = last.Number + 1
	}

	for n := from; n <= headNum; n++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		header, err := m.backend.HeaderByNumber(ctx, new(big.Int).SetUint64(n))
		if err != nil {
			return err
		}

		// Parent-linkage check: the new block must build on the one we indexed.
		// A mismatch means a reorg below the tip; roll back the divergent block.
		if n > 0 {
			if prev, ok, err := m.store.BlockByNumber(ctx, n-1); err != nil {
				return err
			} else if ok && prev.Hash != header.ParentHash {
				return m.rollback(ctx, n-1)
			}
		}

		transfers, err := m.fetchTransfers(ctx, n)
		if err != nil {
			return err
		}

		block := Block{Number: n, Hash: header.Hash(), ParentHash: header.ParentHash, Time: header.Time}
		confirmed := headNum-n >= m.cfg.Confirmations
		if err := m.store.SaveBlock(ctx, block, transfers, confirmed); err != nil {
			return err
		}

		m.publishBlock(ctx, block, len(transfers), confirmed)
		for _, t := range transfers {
			m.publishTransfer(ctx, t, n, confirmed)
		}
	}

	// Promote any now-deep-enough blocks to confirmed.
	if headNum >= m.cfg.Confirmations {
		if err := m.store.ConfirmThrough(ctx, headNum-m.cfg.Confirmations); err != nil {
			return err
		}
	}
	return nil
}

func (m *Monitor) fetchTransfers(ctx context.Context, n uint64) ([]Transfer, error) {
	num := new(big.Int).SetUint64(n)
	logs, err := m.backend.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: num,
		ToBlock:   num,
		Addresses: m.cfg.Tokens,
		Topics:    [][]common.Hash{{m.decoder.TopicID}},
	})
	if err != nil {
		return nil, err
	}
	out := make([]Transfer, 0, len(logs))
	for _, lg := range logs {
		if t, ok := m.decoder.decode(lg); ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *Monitor) rollback(ctx context.Context, from uint64) error {
	m.log.Warn("reorg detected, rolling back", slog.Uint64("from_block", from))
	if err := m.store.DeleteFrom(ctx, from); err != nil {
		return err
	}
	_ = m.bus.Publish(ctx, events.SubjectChainReorg, map[string]any{"from_block": from})
	return nil
}

func (m *Monitor) publishBlock(ctx context.Context, b Block, txfers int, confirmed bool) {
	_ = m.bus.Publish(ctx, events.SubjectChainBlock, map[string]any{
		"number": b.Number, "hash": b.Hash.Hex(), "transfers": txfers, "confirmed": confirmed,
	})
}

func (m *Monitor) publishTransfer(ctx context.Context, t Transfer, block uint64, confirmed bool) {
	_ = m.bus.Publish(ctx, events.SubjectChainTransfer, map[string]any{
		"block": block, "token": t.Token.Hex(), "from": t.From.Hex(), "to": t.To.Hex(),
		"value": t.Value.String(), "tx_hash": t.TxHash.Hex(), "confirmed": confirmed,
	})
}
