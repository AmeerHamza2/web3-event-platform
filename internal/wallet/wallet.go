// Package wallet manages Ethereum key custody via go-ethereum's encrypted
// keystore (Web3 Secret Storage) and publishes wallet.created. Private keys are
// never returned over the API.
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

type Wallet struct {
	Address   string    `json:"address"`
	CreatedAt time.Time `json:"created_at"`
}

type Service struct {
	ks         *keystore.KeyStore
	passphrase string
	chainID    *big.Int
	bus        events.Bus

	mu      sync.RWMutex
	created map[common.Address]time.Time
}

// NewService opens or creates a keystore at dir. light selects the cheaper
// scrypt parameters (tests/dev); production uses the standard ones.
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

// Create generates an account, persists it encrypted, and publishes
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

func (s *Service) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /wallets", func(w http.ResponseWriter, r *http.Request) {
		wal, err := s.Create(r.Context())
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "could not create wallet")
			return
		}
		httpx.JSON(w, http.StatusCreated, wal)
	})

	mux.Handle("GET /wallets", httpx.RequireRole("admin")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			httpx.JSON(w, http.StatusOK, s.List())
		})))

	return mux
}
