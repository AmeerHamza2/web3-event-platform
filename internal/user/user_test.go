package user

import (
	"context"
	"sync"
	"testing"

	"github.com/AmeerHamza2/web3-event-platform/pkg/events"
)

// mockBus records published subjects so tests can assert events fire.
type mockBus struct {
	mu        sync.Mutex
	published []string
}

func (m *mockBus) Publish(_ context.Context, subject string, _ any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.published = append(m.published, subject)
	return nil
}
func (m *mockBus) Subscribe(string, func(events.Event)) (func() error, error) { return nil, nil }
func (m *mockBus) Close()                                                     {}

func TestRegisterPublishesEvent(t *testing.T) {
	bus := &mockBus{}
	svc := NewService(NewMemStore(), bus)

	u, err := svc.Register(context.Background(), "bob@example.com")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if u.ID == "" {
		t.Fatal("expected a generated id")
	}
	if len(bus.published) != 1 || bus.published[0] != events.SubjectUserCreated {
		t.Fatalf("expected one user.created event, got %v", bus.published)
	}
}

func TestRegisterRejectsBadEmail(t *testing.T) {
	svc := NewService(NewMemStore(), &mockBus{})
	if _, err := svc.Register(context.Background(), "not-an-email"); err == nil {
		t.Fatal("expected invalid email to be rejected")
	}
}

func TestStoreGetMissing(t *testing.T) {
	s := NewMemStore()
	if _, err := s.Get("nope"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
