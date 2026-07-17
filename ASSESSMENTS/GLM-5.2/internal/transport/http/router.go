package httptransport

import (
	"log/slog"
	"net/http"

	"github.com/example/notification-service/internal/service"
)

// NewRouter builds the HTTP router with all endpoints and middleware applied.
func NewRouter(svc *service.NotificationService, log *slog.Logger, listDefault, listMax int) http.Handler {
	h := NewHandler(svc, log, listDefault, listMax)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/notifications", h.Create)
	mux.HandleFunc("GET /api/v1/notifications", h.List)
	mux.HandleFunc("GET /api/v1/notifications/{id}", h.Get)
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return chain(mux,
		requestIDMiddleware,
		recoveryMiddleware(log),
		loggingMiddleware(log),
	)
}
