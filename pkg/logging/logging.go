// Package logging provides a JSON slog logger tagged with the service name.
package logging

import (
	"log/slog"
	"os"
)

// New returns a structured JSON logger tagged with service.
func New(service string) *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h).With(slog.String("service", service))
}
