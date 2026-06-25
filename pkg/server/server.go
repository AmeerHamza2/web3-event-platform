// Package server holds the graceful HTTP-server lifecycle and small env helpers
// shared by every service main, so each entrypoint stays a few lines.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// EnvOr returns the env var named key, or def when unset/empty.
func EnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Run starts an HTTP server on addr and blocks until SIGINT/SIGTERM, then
// drains in-flight requests with a 10s deadline. Every service shares this so
// shutdown semantics (and the slowloris ReadHeaderTimeout) are uniform.
func Run(log *slog.Logger, addr string, h http.Handler) {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("listening", slog.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	log.Info("shutting down")
	_ = srv.Shutdown(ctx)
}
