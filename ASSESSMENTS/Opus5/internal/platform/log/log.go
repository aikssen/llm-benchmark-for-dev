// Package log builds the service's structured logger.
//
// Structured logging is not decoration: with one delivery per recipient and
// retries in between, the only way to answer "what happened to this message?"
// is to filter on notification_id and delivery_id, which requires fields
// rather than sentences.
package log

import (
	"io"
	"log/slog"
	"strings"
)

// New builds a logger for the given level and format ("json" or "text").
func New(w io.Writer, level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var handler slog.Handler
	if strings.EqualFold(format, "text") {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	return slog.New(handler)
}

// Discard returns a logger that writes nowhere. Tests use it to keep output
// readable without giving up the code paths that log.
func Discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
