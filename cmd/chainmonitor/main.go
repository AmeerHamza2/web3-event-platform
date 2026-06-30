package main

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/AmeerHamza2/web3-event-platform/internal/chainmonitor"
	"github.com/AmeerHamza2/web3-event-platform/pkg/events"
	"github.com/AmeerHamza2/web3-event-platform/pkg/httpx"
	"github.com/AmeerHamza2/web3-event-platform/pkg/logging"
	"github.com/AmeerHamza2/web3-event-platform/pkg/server"
)

func main() {
	log := logging.New("chainmonitor")

	addr := server.EnvOr("CHAINMONITOR_ADDR", ":8084")
	natsURL := server.EnvOr("NATS_URL", "nats://localhost:4222")
	rpcURL := server.EnvOr("ETH_RPC_URL", "https://ethereum-sepolia-rpc.publicnode.com")
	dsn := server.EnvOr("POSTGRES_DSN", "postgres://postgres:postgres@localhost:5432/chainmonitor?sslmode=disable")
	confirmations := envUint("CONFIRMATIONS", 5)
	pollInterval := envDuration("POLL_INTERVAL", 12*time.Second)

	bus, err := events.Connect(natsURL)
	if err != nil {
		log.Error("connect bus", "error", err)
		os.Exit(1)
	}
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store, err := chainmonitor.NewPostgresStore(ctx, dsn)
	if err != nil {
		log.Error("connect postgres", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	// ethclient.Client satisfies chainmonitor.ChainBackend. HTTP dials lazily, so
	// a node that is down at boot doesn't stop us — Sync errors are retried.
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		log.Error("dial ethereum rpc", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	cfg := chainmonitor.Config{
		Confirmations: confirmations,
		PollInterval:  pollInterval,
		StartBlock:    resolveStartBlock(ctx, log, client),
	}
	monitor, err := chainmonitor.New(client, store, bus, log, cfg)
	if err != nil {
		log.Error("init monitor", "error", err)
		os.Exit(1)
	}

	go monitor.Run(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	server.Run(log, addr, httpx.Chain(mux, httpx.RequestID, httpx.Recovery(log)))
}

// resolveStartBlock honours START_BLOCK; otherwise it follows the tip (index
// only new blocks), since indexing from genesis on a live network is infeasible.
func resolveStartBlock(ctx context.Context, log loggerLike, client *ethclient.Client) uint64 {
	if v := os.Getenv("START_BLOCK"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err == nil {
			return n
		}
		log.Warn("invalid START_BLOCK, ignoring", "value", v)
	}
	headCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	head, err := client.HeaderByNumber(headCtx, nil)
	if err != nil {
		log.Warn("could not read head at boot; starting from 0 (set START_BLOCK to avoid a full backfill)", "error", err)
		return 0
	}
	return head.Number.Uint64()
}

type loggerLike interface {
	Warn(msg string, args ...any)
}

func envUint(key string, def uint64) uint64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
