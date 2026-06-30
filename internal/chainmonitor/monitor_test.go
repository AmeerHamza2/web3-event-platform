package chainmonitor

import (
	"context"
	"math/big"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/AmeerHamza2/web3-event-platform/pkg/events"
	"github.com/AmeerHamza2/web3-event-platform/pkg/logging"
)

// fakeBackend is an in-memory chain whose canonical headers can be swapped to
// simulate a reorg.
type fakeBackend struct {
	mu      sync.Mutex
	headers map[uint64]*types.Header
	logs    map[uint64][]types.Log
	head    uint64
}

func (f *fakeBackend) HeaderByNumber(_ context.Context, number *big.Int) (*types.Header, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.head
	if number != nil {
		n = number.Uint64()
	}
	h, ok := f.headers[n]
	if !ok {
		return nil, ethereum.NotFound
	}
	return h, nil
}

func (f *fakeBackend) FilterLogs(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logs[q.FromBlock.Uint64()], nil
}

// buildChain produces headers 0..n. Blocks at or above forkAt carry forkSalt in
// Extra so two chains built with different salts share a common prefix and
// diverge at forkAt — exactly a reorg.
func buildChain(n, forkAt uint64, forkSalt byte) map[uint64]*types.Header {
	headers := make(map[uint64]*types.Header)
	var parent common.Hash
	for i := uint64(0); i <= n; i++ {
		extra := []byte{0}
		if i >= forkAt {
			extra = []byte{forkSalt}
		}
		h := &types.Header{
			Number:     new(big.Int).SetUint64(i),
			ParentHash: parent,
			Time:       i,
			Extra:      extra,
		}
		headers[i] = h
		parent = h.Hash()
	}
	return headers
}

type captureBus struct {
	mu       sync.Mutex
	subjects []string
}

func (c *captureBus) Publish(_ context.Context, subject string, _ any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subjects = append(c.subjects, subject)
	return nil
}
func (c *captureBus) Subscribe(string, func(events.Event)) (func() error, error) { return nil, nil }
func (c *captureBus) Close()                                                     {}
func (c *captureBus) count(subject string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, s := range c.subjects {
		if s == subject {
			n++
		}
	}
	return n
}

func newTestMonitor(t *testing.T, be ChainBackend, store Store, bus events.Bus, cfg Config) *Monitor {
	t.Helper()
	m, err := New(be, store, bus, logging.New("test"), cfg)
	if err != nil {
		t.Fatalf("new monitor: %v", err)
	}
	return m
}

func TestSyncIndexesBlocks(t *testing.T) {
	be := &fakeBackend{headers: buildChain(5, 6, 0), logs: map[uint64][]types.Log{}, head: 5}
	store := NewMemStore()
	m := newTestMonitor(t, be, store, &captureBus{}, Config{Confirmations: 0})

	if err := m.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	last, ok, _ := store.LastBlock(context.Background())
	if !ok || last.Number != 5 {
		t.Fatalf("expected head at block 5, got %+v ok=%v", last, ok)
	}
}

func TestConfirmationGating(t *testing.T) {
	// One transfer per block; confirmations=2, head=5 → blocks 0..3 final.
	td, _ := newTransferDecoder()
	logs := map[uint64][]types.Log{}
	for n := uint64(0); n <= 5; n++ {
		logs[n] = []types.Log{transferLog(td, n)}
	}
	be := &fakeBackend{headers: buildChain(5, 6, 0), logs: logs, head: 5}
	store := NewMemStore()
	m := newTestMonitor(t, be, store, &captureBus{}, Config{Confirmations: 2})

	if err := m.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Inspect the store directly (white-box). Value encodes the block number.
	confirmed := map[uint64]bool{}
	for _, st := range store.transfers {
		confirmed[st.transfer.Value.Uint64()] = st.confirmed
	}
	for n := uint64(0); n <= 5; n++ {
		want := n <= 3 // head(5) - n >= 2
		if confirmed[n] != want {
			t.Errorf("block %d transfer confirmed=%v, want %v", n, confirmed[n], want)
		}
	}
}

func TestDecodeTransfer(t *testing.T) {
	td, _ := newTransferDecoder()
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	lg := types.Log{
		Address: common.HexToAddress("0xToken00000000000000000000000000000000beef"),
		Topics:  []common.Hash{td.TopicID, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())},
		Data:    common.LeftPadBytes(big.NewInt(4242).Bytes(), 32),
	}
	got, ok := td.decode(lg)
	if !ok {
		t.Fatal("expected log to decode as a Transfer")
	}
	if got.From != from || got.To != to || got.Value.Uint64() != 4242 {
		t.Fatalf("bad decode: %+v", got)
	}
}

func TestReorgRollsBackAndReindexes(t *testing.T) {
	ctx := context.Background()
	be := &fakeBackend{headers: buildChain(5, 6, 0), logs: map[uint64][]types.Log{}, head: 5}
	store := NewMemStore()
	bus := &captureBus{}
	m := newTestMonitor(t, be, store, bus, Config{Confirmations: 0})

	if err := m.Sync(ctx); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	orig, _, _ := store.LastBlock(ctx)

	// Reorg: blocks 3..5 are replaced on a fork sharing the 0..2 prefix.
	be.mu.Lock()
	be.headers = buildChain(5, 3, 1)
	be.mu.Unlock()

	// Each Sync rolls back one divergent block; loop until it converges.
	for i := 0; i < 12; i++ {
		if err := m.Sync(ctx); err != nil {
			t.Fatalf("resync %d: %v", i, err)
		}
	}

	last, _, _ := store.LastBlock(ctx)
	canon := be.headers[5]
	if last.Hash != canon.Hash() {
		t.Fatalf("after reorg, tip hash = %s, want canonical %s", last.Hash, canon.Hash())
	}
	if last.Hash == orig.Hash {
		t.Fatal("tip hash did not change after reorg")
	}
	if bus.count(events.SubjectChainReorg) == 0 {
		t.Fatal("expected at least one reorg event")
	}
}

// transferLog builds a Transfer log in block n with value = n (so tests can map
// a transfer back to its block).
func transferLog(td *transferDecoder, n uint64) types.Log {
	addr := common.HexToAddress("0x000000000000000000000000000000000000dead")
	return types.Log{
		Address: addr,
		Topics:  []common.Hash{td.TopicID, common.BytesToHash(addr.Bytes()), common.BytesToHash(addr.Bytes())},
		Data:    common.LeftPadBytes(new(big.Int).SetUint64(n).Bytes(), 32),
	}
}
