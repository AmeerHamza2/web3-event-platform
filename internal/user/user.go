// Package user owns user identities and publishes user.created. Store abstracts
// persistence; the MVP ships an in-memory implementation, Postgres is the
// production swap.
package user

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/AmeerHamza2/web3-event-platform/pkg/events"
	"github.com/AmeerHamza2/web3-event-platform/pkg/httpx"
)

var ErrNotFound = errors.New("user not found")

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type Store interface {
	Create(u User) error
	Get(id string) (User, error)
	List() []User
}

// MemStore is a concurrency-safe in-memory Store.
type MemStore struct {
	mu    sync.RWMutex
	users map[string]User
}

func NewMemStore() *MemStore { return &MemStore{users: make(map[string]User)} }

func (m *MemStore) Create(u User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[u.ID] = u
	return nil
}

func (m *MemStore) Get(id string) (User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (m *MemStore) List() []User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, u)
	}
	return out
}

type Service struct {
	store Store
	bus   events.Bus
}

func NewService(store Store, bus events.Bus) *Service {
	return &Service{store: store, bus: bus}
}

// Register validates the email, persists the user, and publishes user.created.
func (s *Service) Register(ctx context.Context, email string) (User, error) {
	if _, err := mail.ParseAddress(email); err != nil {
		return User{}, errors.New("invalid email")
	}
	u := User{ID: uuid.NewString(), Email: email, CreatedAt: time.Now().UTC()}
	if err := s.store.Create(u); err != nil {
		return User{}, err
	}
	_ = s.bus.Publish(ctx, events.SubjectUserCreated, u) // best-effort
	return u, nil
}

type registerRequest struct {
	Email string `json:"email"`
}

func (s *Service) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /users", func(w http.ResponseWriter, r *http.Request) {
		var req registerRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
		u, err := s.Register(r.Context(), req.Email)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		httpx.JSON(w, http.StatusCreated, u)
	})

	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, err := s.store.Get(r.PathValue("id"))
		if err != nil {
			httpx.Error(w, http.StatusNotFound, "user not found")
			return
		}
		httpx.JSON(w, http.StatusOK, u)
	})

	mux.Handle("GET /users", httpx.RequireRole("admin")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			httpx.JSON(w, http.StatusOK, s.store.List())
		})))

	return mux
}
