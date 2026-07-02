package main

import (
	"os"
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
		RateLimit: gateway.NewRateLimiter(20, 40),
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
