package api

import (
	"log/slog"
	"net/http"

	"github.com/notifications-service/internal/service"
)

func NewRouter(svc *service.NotificationService, logger *slog.Logger) http.Handler {
	handler := NewHandler(svc, logger)

	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/v1/notifications", handler.SubmitNotification)
	mux.HandleFunc("GET /api/v1/notifications/{id}", handler.GetNotification)
	mux.HandleFunc("GET /api/v1/notifications", handler.ListNotifications)
	mux.HandleFunc("GET /health", handler.HealthCheck)

	var h http.Handler = mux
	h = LoggingMiddleware(logger)(h)
	h = RecoveryMiddleware(logger)(h)

	return h
}
