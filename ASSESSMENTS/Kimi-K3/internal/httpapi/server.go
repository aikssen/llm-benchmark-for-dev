// Package httpapi exposes the notification service over HTTP.
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"notifications/internal/dispatcher"
	"notifications/internal/provider"
	"notifications/internal/store"
)

// Server bundles the dependencies of the HTTP layer.
type Server struct {
	store      store.Store
	dispatcher *dispatcher.Dispatcher
	registry   *provider.Registry
	logger     *slog.Logger
	maxRecips  int
}

// NewServer wires the HTTP layer. maxRecipients caps how many recipients
// a single request may carry (protects queue capacity from single huge
// requests).
func NewServer(st store.Store, d *dispatcher.Dispatcher, r *provider.Registry, maxRecipients int, logger *slog.Logger) *Server {
	if maxRecipients <= 0 {
		maxRecipients = 100
	}
	return &Server{
		store:      st,
		dispatcher: d,
		registry:   r,
		logger:     logger,
		maxRecips:  maxRecipients,
	}
}

// Handler returns the root HTTP handler with routing and middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/v1/notifications", s.handleCreate)
	mux.HandleFunc("GET /api/v1/notifications", s.handleList)
	mux.HandleFunc("GET /api/v1/notifications/{id}", s.handleGet)

	return chain(mux,
		requestID,
		recoverer(s.logger),
		accessLog(s.logger),
	)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
}
