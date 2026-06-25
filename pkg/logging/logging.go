// Package logging provides a small wrapper around log/slog so every service
// emits structured JSON logs with a consistent service tag.
package logging

import (
	"log/slog"
	"os"
)

// New returns a JSON structured logger tagged with the service name. In
// production this lands in stdout where a log shipper (Loki, CloudWatch, ELK)
// collects it; the service tag lets you filter one service out of the platform.
func New(service string) *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(h).With(slog.String("service", service))
}
