// Package observability provides small, shared observability helpers
// (structured logging setup).
package observability

import (
	"log/slog"
	"os"
)

// NewLogger builds a JSON structured logger writing to stdout. level is one of
// debug|info|warn|error.
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler)
}
