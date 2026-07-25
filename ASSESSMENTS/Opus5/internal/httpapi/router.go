// Package httpapi is the HTTP transport.
//
// It owns wire formats, status codes and request plumbing, and nothing else:
// no business rule is decided here, so the same service can later be exposed
// over a different protocol without moving logic around.
package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/evercifuentes/notifier/internal/service"
)

// methodNotAllowed answers 405 with the Allow header clients need in order to
// correct the request.
func methodNotAllowed(logger *slog.Logger, allowed ...string) http.HandlerFunc {
	allow := strings.Join(allowed, ", ")
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		writeError(w, r, logger, http.StatusMethodNotAllowed, codeMethodNotAllowed,
			r.Method+" is not supported on "+r.URL.Path+"; allowed methods are "+allow, "")
	}
}

// Config tunes the transport.
type Config struct {
	// MaxRequestBytes caps the accepted body size.
	MaxRequestBytes int64
	// RequestTimeout bounds how long a handler may run.
	RequestTimeout time.Duration
}

// DefaultConfig returns sensible transport defaults.
func DefaultConfig() Config {
	return Config{
		MaxRequestBytes: 1 << 20, // 1 MiB
		RequestTimeout:  10 * time.Second,
	}
}

// HealthChecker reports whether the service can accept work.
type HealthChecker interface {
	// Ready returns nil when the service is ready, or the reason it is not.
	Ready() error
	// Details returns diagnostic information for the health endpoint.
	Details() map[string]any
}

// NewRouter wires the endpoints and middleware.
//
// Uses net/http's own pattern matching (Go 1.22+): the routing needs of this
// API are met by the standard library, and a router dependency would be
// weight without benefit.
func NewRouter(svc *service.Service, health HealthChecker, cfg Config, logger *slog.Logger) http.Handler {
	h := &handler{svc: svc, logger: logger}
	hc := &healthHandler{checker: health, logger: logger}

	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/notifications", h.submit)
	mux.HandleFunc("GET /v1/notifications", h.list)
	mux.HandleFunc("GET /v1/notifications/{id}", h.get)
	mux.HandleFunc("GET /v1/channels", h.channels)

	// Health endpoints are outside /v1: they describe the process, not the
	// API version, and must keep working across API changes.
	mux.HandleFunc("GET /healthz", hc.live)
	mux.HandleFunc("GET /readyz", hc.ready)

	// Method-less patterns catch the requests the handlers above declined.
	// ServeMux prefers the method-qualified pattern when it matches, so
	// these only fire on a method mismatch — which keeps 405 responses in
	// the same JSON error envelope as everything else, rather than falling
	// through to the catch-all as a misleading 404.
	mux.HandleFunc("/v1/notifications", methodNotAllowed(logger, http.MethodGet, http.MethodPost))
	mux.HandleFunc("/v1/notifications/{id}", methodNotAllowed(logger, http.MethodGet))
	mux.HandleFunc("/v1/channels", methodNotAllowed(logger, http.MethodGet))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, logger, http.StatusNotFound, codeNotFound,
			"no route matches "+r.Method+" "+r.URL.Path, "")
	})

	// Outermost first: panics must be caught even in the logger, and the
	// request ID must exist before anything tries to log it.
	return chain(mux,
		recoverPanic(logger),
		requestID(),
		requestLogger(logger),
		limitBody(cfg.MaxRequestBytes),
		timeout(cfg.RequestTimeout),
	)
}
