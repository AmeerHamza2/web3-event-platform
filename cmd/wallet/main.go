package main

import (
	"os"

	"github.com/AmeerHamza2/web3-event-platform/internal/wallet"
	"github.com/AmeerHamza2/web3-event-platform/pkg/events"
	"github.com/AmeerHamza2/web3-event-platform/pkg/httpx"
	"github.com/AmeerHamza2/web3-event-platform/pkg/logging"
	"github.com/AmeerHamza2/web3-event-platform/pkg/server"
)

// Sepolia chain ID.
const sepoliaChainID = 11155111

func main() {
	log := logging.New("wallet")

	addr := server.EnvOr("WALLET_ADDR", ":8082")
	natsURL := server.EnvOr("NATS_URL", "nats://localhost:4222")
	ksDir := server.EnvOr("KEYSTORE_DIR", "/data/keystore")
	pass := server.EnvOr("KEYSTORE_PASSPHRASE", "")
	light := server.EnvOr("KEYSTORE_LIGHT_SCRYPT", "") == "true"

	bus, err := events.Connect(natsURL)
	if err != nil {
		log.Error("connect bus", "error", err)
		os.Exit(1)
	}
	defer bus.Close()

	svc, err := wallet.NewService(ksDir, pass, sepoliaChainID, light, bus)
	if err != nil {
		log.Error("init wallet service", "error", err)
		os.Exit(1)
	}

	handler := httpx.Chain(svc.Routes(),
		httpx.RequestID,
		httpx.Logger(log),
		httpx.Recovery(log),
	)

	server.Run(log, addr, handler)
}
