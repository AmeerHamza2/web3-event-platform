package chainmonitor

import (
	"context"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// Block is an indexed block header.
type Block struct {
	Number     uint64
	Hash       common.Hash
	ParentHash common.Hash
	Time       uint64
}

// Store persists indexed blocks and transfers. Implementations must be safe for
// concurrent use.
type Store interface {
	// SaveBlock upserts a block and its transfers atomically.
	SaveBlock(ctx context.Context, b Block, transfers []Transfer, confirmed bool) error
	// LastBlock returns the highest indexed block, if any.
	LastBlock(ctx context.Context) (Block, bool, error)
	// BlockByNumber returns the indexed block at number, if present.
	BlockByNumber(ctx context.Context, number uint64) (Block, bool, error)
	// DeleteFrom removes every block and transfer at or above number (reorg
	// rollback).
	DeleteFrom(ctx context.Context, number uint64) error
	// ConfirmThrough marks transfers in blocks at or below number confirmed.
	ConfirmThrough(ctx context.Context, number uint64) error
}

// MemStore is an in-memory Store for tests.
type MemStore struct {
	mu        sync.Mutex
	blocks    map[uint64]Block
	transfers []storedTransfer
}

type storedTransfer struct {
	block     uint64
	transfer  Transfer
	confirmed bool
}

func NewMemStore() *MemStore {
	return &MemStore{blocks: make(map[uint64]Block)}
}

func (m *MemStore) SaveBlock(_ context.Context, b Block, transfers []Transfer, confirmed bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocks[b.Number] = b
	for _, t := range transfers {
		m.transfers = append(m.transfers, storedTransfer{block: b.Number, transfer: t, confirmed: confirmed})
	}
	return nil
}

func (m *MemStore) LastBlock(_ context.Context) (Block, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.blocks) == 0 {
		return Block{}, false, nil
	}
	nums := make([]uint64, 0, len(m.blocks))
	for n := range m.blocks {
		nums = append(nums, n)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] > nums[j] })
	return m.blocks[nums[0]], true, nil
}

func (m *MemStore) BlockByNumber(_ context.Context, number uint64) (Block, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.blocks[number]
	return b, ok, nil
}

func (m *MemStore) DeleteFrom(_ context.Context, number uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for n := range m.blocks {
		if n >= number {
			delete(m.blocks, n)
		}
	}
	kept := m.transfers[:0]
	for _, t := range m.transfers {
		if t.block < number {
			kept = append(kept, t)
		}
	}
	m.transfers = kept
	return nil
}

func (m *MemStore) ConfirmThrough(_ context.Context, number uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.transfers {
		if m.transfers[i].block <= number {
			m.transfers[i].confirmed = true
		}
	}
	return nil
}

// ConfirmedTransfers returns confirmed transfers; test helper.
func (m *MemStore) ConfirmedTransfers() []Transfer {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Transfer
	for _, t := range m.transfers {
		if t.confirmed {
			out = append(out, t.transfer)
		}
	}
	return out
}
