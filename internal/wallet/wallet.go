// Package wallet is the Wallet service: Ethereum key custody and transaction
// signing, reusing go-ethereum's keystore (Web3 Secret Storage) so private keys
// are encrypted at rest with scrypt + AES-128-CTR and never leave the service.
//
// On wallet creation it publishes a wallet.created domain event to the bus, so
// downstream consumers (notifications, indexers) react without the wallet
// service knowing they exist.
package wallet

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"

	"github.com/AmeerHamza2/web3-event-platform/pkg/events"
	"github.com/AmeerHamza2/web3-event-platform/pkg/httpx"
)

// Wallet is the public (non-sensitive) view of an account.
type Wallet struct {
	Address   string    `json:"address"`
	CreatedAt time.Time `json:"created_at"`
}

// Service manages keystore-backed accounts.
type Service struct {
	ks         *keystore.KeyStore
	passphrase string
	chainID    *big.Int
	bus        events.Bus

	mu      sync.RWMutex
	created map[common.Address]time.Time
}

// NewService opens (or creates) a keystore at dir. It uses light scrypt params
// only when light is true (tests); production uses the standard (stronger) ones.
func NewService(dir, passphrase string, chainID int64, light bool, bus events.Bus) (*Service, error) {
	if passphrase == "" {
		return nil, errors.New("keystore passphrase must not be empty")
	}
	n, p := keystore.StandardScryptN, keystore.StandardScryptP
	if light {
		n, p = keystore.LightScryptN, keystore.LightScryptP
	}
	return &Service{
		ks:         keystore.NewKeyStore(dir, n, p),
		passphrase: passphrase,
		chainID:    big.NewInt(chainID),
		bus:        bus,
		created:    make(map[common.Address]time.Time),
	}, nil
}

// Create generates a fresh account, persists it encrypted, and publishes
// wallet.created.
func (s *Service) Create(ctx context.Context) (Wallet, error) {
	acct, err := s.ks.NewAccount(s.passphrase)
	if err != nil {
		return Wallet{}, err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	s.created[acct.Address] = now
	s.mu.Unlock()

	w := Wallet{Address: acct.Address.Hex(), CreatedAt: now}
	_ = s.bus.Publish(ctx, events.SubjectWalletCreated, w)
	return w, nil
}

// List returns all accounts in the keystore.
func (s *Service) List() []Wallet {
	accts := s.ks.Accounts()
	out := make([]Wallet, 0, len(accts))
	for _, a := range accts {
		s.mu.RLock()
		ts := s.created[a.Address]
		s.mu.RUnlock()
		out = append(out, Wallet{Address: a.Address.Hex(), CreatedAt: ts})
	}
	return out
}

// Routes returns the wallet service's HTTP handler.
func (s *Service) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /wallets", func(w http.ResponseWriter, r *http.Request) {
		wal, err := s.Create(r.Context())
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "could not create wallet")
			return
		}
		httpx.JSON(w, http.StatusCreated, wal)
	})

	// Listing every wallet is admin-only (RBAC).
	mux.Handle("GET /wallets", httpx.RequireRole("admin")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			httpx.JSON(w, http.StatusOK, s.List())
		})))

	return mux
}
