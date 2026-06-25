// Command user runs the User service.
package main

import (
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

	bus, err := events.Connect(natsURL)
	if err != nil {
		log.Error("connect bus", "error", err)
		os.Exit(1)
	}
	defer bus.Close()

	svc := user.NewService(user.NewMemStore(), bus)

	handler := httpx.Chain(svc.Routes(),
		httpx.RequestID,
		httpx.Logger(log),
		httpx.Recovery(log),
	)

	server.Run(log, addr, handler)
}
