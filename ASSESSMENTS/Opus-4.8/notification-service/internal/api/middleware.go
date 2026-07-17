package api

import (
	"log/slog"
	"net/http"
	"time"
)

// middleware is the standard decorator shape for http.Handler.
type middleware func(http.Handler) http.Handler

// statusRecorder captures the response status code for logging without altering
// the response.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// requestLogger logs one structured line per request with method, path, status
// and latency. Structured logs are the baseline for observability in a service
// that will be operated in production.
func requestLogger(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			log.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Duration("latency", time.Since(start)),
			)
		})
	}
}

// recoverer converts a panic in a handler into a 500 response and a logged
// error, keeping one bad request from taking down the server.
func recoverer(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered in handler",
						slog.String("path", r.URL.Path),
						slog.Any("panic", rec),
					)
					writeError(w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
