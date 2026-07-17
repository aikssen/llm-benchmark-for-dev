package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/service"
	"github.com/aikssen/notification-service/internal/store"
)

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

var supportedChannels = map[string]domain.Channel{
	string(domain.ChannelEmail): domain.ChannelEmail,
	string(domain.ChannelSMS):   domain.ChannelSMS,
	string(domain.ChannelPush):  domain.ChannelPush,
}

// Handler holds the dependencies needed by the HTTP handlers.
type Handler struct {
	service *service.NotificationService
	logger  *slog.Logger
}

// NewHandler builds a Handler.
func NewHandler(svc *service.NotificationService, logger *slog.Logger) *Handler {
	return &Handler{service: svc, logger: logger}
}

// PostNotifications handles POST /v1/notifications: submits a batch of
// notifications, one per recipient, for validation and delivery.
func (h *Handler) PostNotifications(w http.ResponseWriter, r *http.Request) {
	var req SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed JSON body: "+err.Error())
		return
	}

	channel, ok := supportedChannels[req.Channel]
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported channel: "+req.Channel)
		return
	}

	batchReq := domain.BatchRequest{
		Channel:    channel,
		Recipients: req.Recipients,
		Message: domain.Message{
			Subject: req.Message.Subject,
			Body:    req.Message.Body,
			Data:    req.Message.Data,
		},
	}

	result, err := h.service.Submit(r.Context(), batchReq)
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.logger.Error("submit failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := SubmitResponse{BatchID: result.BatchID, Notifications: make([]NotificationDTO, 0, len(result.Notifications))}
	for _, n := range result.Notifications {
		resp.Notifications = append(resp.Notifications, toNotificationDTO(n))
	}
	writeJSON(w, http.StatusAccepted, resp)
}

// GetNotification handles GET /v1/notifications/{id}.
func (h *Handler) GetNotification(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	n, err := h.service.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notification not found")
			return
		}
		h.logger.Error("get notification failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toNotificationDTO(n))
}

// ListNotifications handles GET /v1/notifications.
func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := defaultListLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(n, maxListLimit)
	}

	offset := 0
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a non-negative integer")
			return
		}
		offset = n
	}

	filter := store.ListFilter{
		BatchID:   q.Get("batch_id"),
		Status:    domain.Status(q.Get("status")),
		Channel:   domain.Channel(q.Get("channel")),
		Recipient: q.Get("recipient"),
		Limit:     limit,
		Offset:    offset,
	}

	notifications, total, err := h.service.List(r.Context(), filter)
	if err != nil {
		h.logger.Error("list notifications failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := ListResponse{Notifications: make([]NotificationDTO, 0, len(notifications)), Total: total, Limit: limit, Offset: offset}
	for _, n := range notifications {
		resp.Notifications = append(resp.Notifications, toNotificationDTO(n))
	}
	writeJSON(w, http.StatusOK, resp)
}

// Health handles GET /healthz.
func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}
