// Package api is the HTTP transport layer. It translates HTTP requests into
// service calls and domain results into HTTP responses. It contains no business
// logic; that lives in the service and its collaborators.
package api

import (
	"log/slog"
	"net/http"
)

// Server holds the HTTP dependencies and builds the router.
type Server struct {
	notifier Notifier
	log      *slog.Logger
}

// NewServer constructs a Server.
func NewServer(notifier Notifier, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{notifier: notifier, log: log}
}

// Handler builds the http.Handler with all routes and middleware applied. It
// uses the standard library's method-aware routing (Go 1.22+), avoiding a
// third-party router dependency.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/notifications", s.handleSubmit)
	mux.HandleFunc("GET /v1/notifications", s.handleList)
	mux.HandleFunc("GET /v1/notifications/{id}", s.handleGet)
	mux.HandleFunc("GET /healthz", s.handleHealth)

	// Middleware is applied outermost-first: panic recovery wraps request
	// logging so that even a panic is logged with its request context.
	return recoverer(s.log)(requestLogger(s.log)(mux))
}
