package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/AmeerHamza2/web3-event-platform/internal/gateway"
	"github.com/AmeerHamza2/web3-event-platform/pkg/auth"
	"github.com/AmeerHamza2/web3-event-platform/pkg/httpx"
	"github.com/AmeerHamza2/web3-event-platform/pkg/logging"
	"github.com/AmeerHamza2/web3-event-platform/pkg/server"
)

const defaultJWTSecret = "dev-insecure-secret-change-me"

func main() {
	log := logging.New("gateway")

	addr := server.EnvOr("GATEWAY_ADDR", ":8080")
	jwtSecret := server.EnvOr("JWT_SECRET", defaultJWTSecret)
	production := server.EnvOr("ENV", "development") == "production"

	if production && jwtSecret == defaultJWTSecret {
		log.Error("refusing to start: JWT_SECRET is the default in production")
		os.Exit(1)
	}
	if !production && jwtSecret == defaultJWTSecret {
		log.Warn("using default JWT secret — development only")
	}

	authn := auth.NewAuthenticator(jwtSecret, "web3-event-platform", time.Hour)

	cfg := gateway.Config{
		Auth: authn,
		Clients: map[string]gateway.Client{
			server.EnvOr("ADMIN_CLIENT_ID", "admin-client"): {
				Secret: server.EnvOr("ADMIN_CLIENT_SECRET", "admin-secret"),
				Role:   auth.RoleAdmin,
			},
			server.EnvOr("USER_CLIENT_ID", "user-client"): {
				Secret: server.EnvOr("USER_CLIENT_SECRET", "user-secret"),
				Role:   auth.RoleUser,
			},
		},
		UserURL:   server.EnvOr("USER_URL", "http://localhost:8081"),
		WalletURL: server.EnvOr("WALLET_URL", "http://localhost:8082"),
		MarginURL: server.EnvOr("MARGIN_URL", "http://localhost:8085"),
		RateLimit: selectLimiter(log),
	}

	gw, err := gateway.New(cfg)
	if err != nil {
		log.Error("init gateway", "error", err)
		os.Exit(1)
	}

	handler := httpx.Chain(gw.Handler(),
		httpx.RequestID,
		httpx.Logger(log),
		httpx.Recovery(log),
	)

	server.Run(log, addr, handler)
}

// selectLimiter uses Redis (shared across replicas) when REDIS_ADDR is set,
// otherwise a per-instance in-memory limiter.
func selectLimiter(log *slog.Logger) gateway.Limiter {
	perMin := int64(1200)
	if v := os.Getenv("RATE_LIMIT_PER_MIN"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			perMin = n
		}
	}
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		rl := gateway.NewRedisRateLimiter(addr, perMin, time.Minute)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := rl.Ping(ctx); err != nil {
			log.Error("connect redis", "error", err)
			os.Exit(1)
		}
		log.Info("using redis rate limiter (shared across replicas)")
		return rl
	}
	log.Warn("REDIS_ADDR not set; using in-memory rate limiter (per-instance)")
	return gateway.NewRateLimiter(20, 40)
}
