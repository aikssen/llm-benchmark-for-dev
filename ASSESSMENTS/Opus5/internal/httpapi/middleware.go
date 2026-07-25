package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/evercifuentes/notifier/internal/platform/id"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// RequestIDFrom returns the request ID attached to ctx, if any.
func RequestIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// middleware is a handler decorator.
type middleware func(http.Handler) http.Handler

// chain applies middlewares so that the first listed is the outermost.
func chain(h http.Handler, mw ...middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// requestID attaches a correlation ID to every request, honouring an
// incoming X-Request-ID so a trace can span services.
func requestID() middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := r.Header.Get("X-Request-ID")
			if rid == "" {
				rid = id.New("req")
			}
			w.Header().Set("X-Request-ID", rid)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, rid)))
		})
	}
}

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// requestLogger emits one structured line per request.
func requestLogger(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			level := slog.LevelInfo
			if rec.status >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			logger.LogAttrs(r.Context(), level, "http request",
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// recoverPanic keeps one bad request from taking the process down.
func recoverPanic(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorContext(r.Context(), "recovered from panic in handler",
						slog.Any("panic", rec),
						slog.String("request_id", RequestIDFrom(r.Context())),
						slog.String("path", r.URL.Path))
					writeError(w, r, logger, http.StatusInternalServerError, codeInternal,
						"an unexpected error occurred", "")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// limitBody caps the request body so a client cannot exhaust memory.
func limitBody(maxBytes int64) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// timeout bounds how long a handler may run. It does not replace the
// per-provider timeouts: this one protects the request path, those protect
// the delivery path.
func timeout(d time.Duration) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
