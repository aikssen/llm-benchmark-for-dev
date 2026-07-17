package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/notifications-service/internal/domain"
	"github.com/notifications-service/internal/service"
)

type Handler struct {
	service *service.NotificationService
	logger  *slog.Logger
}

func NewHandler(service *service.NotificationService, logger *slog.Logger) *Handler {
	return &Handler{
		service: service,
		logger:  logger,
	}
}

func (h *Handler) SubmitNotification(w http.ResponseWriter, r *http.Request) {
	var req domain.NotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	notification, err := h.service.Submit(r.Context(), &req)
	if err != nil {
		var validationErr *domain.ValidationError
		if errors.As(err, &validationErr) {
			h.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.logger.Error("failed to submit notification", "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to submit notification")
		return
	}

	h.writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"id":         notification.ID,
		"status":     notification.Status,
		"channel":    notification.Channel,
		"created_at": notification.CreatedAt,
	})
}

func (h *Handler) GetNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "notification ID is required")
		return
	}

	response, err := h.service.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotificationNotFound) {
			h.writeError(w, http.StatusNotFound, "notification not found")
			return
		}
		h.logger.Error("failed to get notification", "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to retrieve notification")
		return
	}

	h.writeJSON(w, http.StatusOK, response)
}

func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	filter := domain.ListFilter{
		Limit:  50,
		Offset: 0,
	}

	if channel := r.URL.Query().Get("channel"); channel != "" {
		c := domain.Channel(channel)
		if !c.IsValid() {
			h.writeError(w, http.StatusBadRequest, "invalid channel filter")
			return
		}
		filter.Channel = &c
	}

	if status := r.URL.Query().Get("status"); status != "" {
		s := domain.Status(status)
		filter.Status = &s
	}

	if limit := r.URL.Query().Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil && l > 0 && l <= 100 {
			filter.Limit = l
		}
	}

	if offset := r.URL.Query().Get("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil && o >= 0 {
			filter.Offset = o
		}
	}

	notifications, total, err := h.service.List(r.Context(), filter)
	if err != nil {
		h.logger.Error("failed to list notifications", "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to list notifications")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"notifications": notifications,
		"total":         total,
		"limit":         filter.Limit,
		"offset":        filter.Offset,
	})
}

func (h *Handler) HealthCheck(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, map[string]string{
		"error": message,
	})
}
