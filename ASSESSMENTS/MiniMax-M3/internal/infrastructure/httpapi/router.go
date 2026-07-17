package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

type Router struct {
	mux *http.ServeMux
	h   *Handler
	log *slog.Logger
}

func NewRouter(h *Handler, log *slog.Logger) *Router {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/notifications", h.Submit)
	mux.HandleFunc("GET /v1/notifications", h.List)
	mux.HandleFunc("GET /v1/notifications/{id}", h.Get)
	mux.HandleFunc("GET /healthz", h.Health)
	return &Router{mux: mux, h: h, log: log}
}

func (r *Router) Handler() http.Handler {
	return r.loggingMiddleware(r.mux)
}

func (r *Router) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lrw, req)
		r.log.Info("http",
			"method", req.Method,
			"path", req.URL.Path,
			"status", lrw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", req.RemoteAddr,
		)
	})
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (l *loggingResponseWriter) WriteHeader(code int) {
	l.status = code
	l.ResponseWriter.WriteHeader(code)
}
