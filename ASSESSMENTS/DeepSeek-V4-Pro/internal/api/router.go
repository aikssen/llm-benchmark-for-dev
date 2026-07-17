package api

import "net/http"

func NewRouter(h *Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/v1/notifications", h.SubmitNotification)
	mux.HandleFunc("GET /api/v1/notifications/{id}", h.GetNotification)
	mux.HandleFunc("GET /api/v1/notifications", h.ListNotifications)

	var handler http.Handler = mux
	handler = LoggingMiddleware(handler)
	handler = RecoveryMiddleware(handler)

	return handler
}
