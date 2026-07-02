package main

import (
	"context"
	"os"

	"github.com/AmeerHamza2/web3-event-platform/internal/user"
	"github.com/AmeerHamza2/web3-event-platform/pkg/events"
	"github.com/AmeerHamza2/web3-event-platform/pkg/httpx"
	"github.com/AmeerHamza2/web3-event-platform/pkg/logging"
	"github.com/AmeerHamza2/web3-event-platform/pkg/server"
)

func main() {
	log := logging.New("user")

	addr := server.EnvOr("USER_ADDR", ":8081")
	natsURL := server.EnvOr("NATS_URL", "nats://localhost:4222")
	dsn := os.Getenv("POSTGRES_DSN")

	bus, err := events.Connect(natsURL)
	if err != nil {
		log.Error("connect bus", "error", err)
		os.Exit(1)
	}
	defer bus.Close()

	// Postgres keeps the service stateless so replicas scale horizontally;
	// in-memory is the local/dev fallback.
	store := selectStore(log, dsn)
	svc := user.NewService(store, bus)

	handler := httpx.Chain(svc.Routes(),
		httpx.RequestID,
		httpx.Logger(log),
		httpx.Recovery(log),
	)

	server.Run(log, addr, handler)
}

func selectStore(log interface {
	Info(string, ...any)
	Warn(string, ...any)
	Error(string, ...any)
}, dsn string) user.Store {
	if dsn == "" {
		log.Warn("POSTGRES_DSN not set; using in-memory store (single-instance only)")
		return user.NewMemStore()
	}
	store, err := user.NewPostgresStore(context.Background(), dsn)
	if err != nil {
		log.Error("connect postgres", "error", err)
		os.Exit(1)
	}
	log.Info("using postgres store (stateless, horizontally scalable)")
	return store
}
