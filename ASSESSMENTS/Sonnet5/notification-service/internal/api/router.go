package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter builds the HTTP router for the service.
func NewRouter(h *Handler) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(h.logger))

	r.Get("/healthz", h.Health)

	r.Route("/v1/notifications", func(r chi.Router) {
		r.Post("/", h.PostNotifications)
		r.Get("/", h.ListNotifications)
		r.Get("/{id}", h.GetNotification)
	})

	return r
}
