package main

import (
	"net/http"
	"os"

	"github.com/AmeerHamza2/web3-event-platform/internal/notification"
	"github.com/AmeerHamza2/web3-event-platform/pkg/events"
	"github.com/AmeerHamza2/web3-event-platform/pkg/httpx"
	"github.com/AmeerHamza2/web3-event-platform/pkg/logging"
	"github.com/AmeerHamza2/web3-event-platform/pkg/server"
)

func main() {
	log := logging.New("notification")

	addr := server.EnvOr("NOTIFICATION_ADDR", ":8083")
	natsURL := server.EnvOr("NATS_URL", "nats://localhost:4222")

	bus, err := events.Connect(natsURL)
	if err != nil {
		log.Error("connect bus", "error", err)
		os.Exit(1)
	}
	defer bus.Close()

	consumer := notification.NewConsumer(bus, log)
	if err := consumer.Start(); err != nil {
		log.Error("start consumer", "error", err)
		os.Exit(1)
	}
	defer consumer.Stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	server.Run(log, addr, httpx.Chain(mux, httpx.RequestID, httpx.Recovery(log)))
}
