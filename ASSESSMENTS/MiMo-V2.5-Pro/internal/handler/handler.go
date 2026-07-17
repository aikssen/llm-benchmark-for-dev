package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/notification-service/internal/domain"
	"github.com/notification-service/internal/service"
	apperrors "github.com/notification-service/pkg/errors"
)

// Handler contains the HTTP handlers for the notification API.
type Handler struct {
	service *service.NotificationService
	logger  *slog.Logger
}

// NewHandler creates a new HTTP handler.
func NewHandler(service *service.NotificationService, logger *slog.Logger) *Handler {
	return &Handler{
		service: service,
		logger:  logger,
	}
}

// RegisterRoutes registers the API routes on the given router.
func (h *Handler) RegisterRoutes(router *mux.Router) {
	api := router.PathPrefix("/api/v1").Subrouter()

	api.HandleFunc("/notifications", h.CreateNotification).Methods("POST")
	api.HandleFunc("/notifications", h.ListNotifications).Methods("GET")
	api.HandleFunc("/notifications/{id}", h.GetNotification).Methods("GET")

	api.HandleFunc("/health", h.HealthCheck).Methods("GET")
}

// CreateNotification handles POST /api/v1/notifications.
func (h *Handler) CreateNotification(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateNotificationRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	notification, err := h.service.CreateNotification(r.Context(), &req)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	h.respondJSON(w, http.StatusAccepted, notification)
}

// GetNotification handles GET /api/v1/notifications/{id}.
func (h *Handler) GetNotification(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	notification, err := h.service.GetNotification(r.Context(), id)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	h.respondJSON(w, http.StatusOK, notification)
}

// ListNotifications handles GET /api/v1/notifications.
func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	params := domain.ListNotificationsParams{
		Limit:  20,
		Offset: 0,
	}

	if channel := r.URL.Query().Get("channel"); channel != "" {
		params.Channel = domain.Channel(channel)
	}

	if status := r.URL.Query().Get("status"); status != "" {
		params.Status = domain.Status(status)
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			params.Limit = limit
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			params.Offset = offset
		}
	}

	notifications, total, err := h.service.ListNotifications(r.Context(), params)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	response := map[string]interface{}{
		"notifications": notifications,
		"total":         total,
		"limit":         params.Limit,
		"offset":        params.Offset,
	}

	h.respondJSON(w, http.StatusOK, response)
}

// HealthCheck handles GET /api/v1/health.
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	h.respondJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (h *Handler) handleServiceError(w http.ResponseWriter, err error) {
	var notFoundErr *apperrors.NotFoundError
	var validationErr *apperrors.ValidationError
	var rateLimitErr *apperrors.RateLimitError
	var providerErr *apperrors.ProviderError

	switch {
	case errors.As(err, &notFoundErr):
		h.respondError(w, http.StatusNotFound, err.Error())
	case errors.As(err, &validationErr):
		h.respondError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &rateLimitErr):
		h.respondError(w, http.StatusTooManyRequests, err.Error())
	case errors.As(err, &providerErr):
		h.logger.Error("provider error", "error", err)
		h.respondError(w, http.StatusServiceUnavailable, "notification delivery temporarily unavailable")
	default:
		h.logger.Error("unexpected error", "error", err)
		h.respondError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *Handler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

func (h *Handler) respondError(w http.ResponseWriter, status int, message string) {
	h.respondJSON(w, status, map[string]string{"error": message})
}
