package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"notification-service/internal/app"
	"notification-service/internal/domain"
	"notification-service/internal/processing"
	"notification-service/internal/store"
)

type Handler struct {
	service *app.Service
}

func NewHandler(service *app.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("POST /notifications", h.createNotification)
	mux.HandleFunc("GET /notifications", h.listNotifications)
	mux.HandleFunc("GET /notifications/{id}", h.getNotification)
	return recoverer(mux)
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) createNotification(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var request createNotificationRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	recipients := request.Recipients
	if request.Recipient != "" {
		recipients = append([]string{request.Recipient}, recipients...)
	}

	result, err := h.service.Submit(r.Context(), app.SubmitCommand{
		Recipients: recipients,
		Channel:    request.Channel,
		Message:    request.Message,
	})
	if err != nil {
		switch {
		case errors.Is(err, app.ErrInvalidRequest), errors.Is(err, app.ErrUnsupportedChannel):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, processing.ErrQueueFull):
			writeError(w, http.StatusServiceUnavailable, "notification queue is full")
		default:
			writeError(w, http.StatusInternalServerError, "failed to submit notification")
		}
		return
	}

	response := submitResponse{
		RequestID:     result.RequestID,
		Notifications: make([]notificationSummary, 0, len(result.Notifications)),
	}
	for _, notification := range result.Notifications {
		response.Notifications = append(response.Notifications, notificationSummary{
			ID:        notification.ID,
			Recipient: notification.Recipient,
			Channel:   notification.Channel,
			Status:    notification.Status,
		})
	}

	writeJSON(w, http.StatusAccepted, response)
}

func (h *Handler) getNotification(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "notification id is required")
		return
	}

	notification, err := h.service.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notification not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to retrieve notification")
		return
	}

	writeJSON(w, http.StatusOK, notification.Redacted())
}

func (h *Handler) listNotifications(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit := 50
	if rawLimit := query.Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(parsed, 100)
	}

	filter := store.ListFilter{
		Status:  domain.Status(query.Get("status")),
		Channel: domain.Channel(query.Get("channel")),
		Limit:   limit,
	}

	notifications, err := h.service.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list notifications")
		return
	}

	response := listResponse{Notifications: make([]domain.Notification, 0, len(notifications))}
	for _, notification := range notifications {
		response.Notifications = append(response.Notifications, notification.Redacted())
	}

	writeJSON(w, http.StatusOK, response)
}

type createNotificationRequest struct {
	Recipient  string         `json:"recipient,omitempty"`
	Recipients []string       `json:"recipients,omitempty"`
	Channel    domain.Channel `json:"channel"`
	Message    domain.Message `json:"message"`
}

type submitResponse struct {
	RequestID     string                `json:"request_id"`
	Notifications []notificationSummary `json:"notifications"`
}

type notificationSummary struct {
	ID        string         `json:"id"`
	Recipient string         `json:"recipient"`
	Channel   domain.Channel `json:"channel"`
	Status    domain.Status  `json:"status"`
}

type listResponse struct {
	Notifications []domain.Notification `json:"notifications"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
