package httptransport

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/example/notification-service/internal/domain"
	"github.com/example/notification-service/internal/service"
)

// Handler binds HTTP endpoints to the NotificationService.
type Handler struct {
	svc         *service.NotificationService
	log         *slog.Logger
	listDefault int
	listMax     int
}

// NewHandler builds a handler.
func NewHandler(svc *service.NotificationService, log *slog.Logger, listDefault, listMax int) *Handler {
	return &Handler{svc: svc, log: log, listDefault: listDefault, listMax: listMax}
}

// Create handles POST /api/v1/notifications.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}

	channel, err := parseChannel(req.Channel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unsupported_channel", err.Error(), nil)
		return
	}

	recipients := make([]domain.Recipient, 0, len(req.Recipients))
	for _, rd := range req.Recipients {
		recipients = append(recipients, domain.Recipient{
			ID:        rd.ID,
			Email:     rd.Email,
			Phone:     rd.Phone,
			PushToken: rd.PushToken,
		})
	}
	message := domain.Message{Subject: req.Message.Subject, Body: req.Message.Body, Data: req.Message.Data}

	n, err := h.svc.Create(r.Context(), channel, recipients, message)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	w.Header().Set("Location", "/api/v1/notifications/"+string(n.ID))
	writeJSON(w, http.StatusAccepted, toResponse(n))
}

// Get handles GET /api/v1/notifications/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "notification id is required", nil)
		return
	}
	n, err := h.svc.Get(r.Context(), domain.ID(id))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
			return
		}
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toResponse(n))
}

// List handles GET /api/v1/notifications.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := h.listDefault, 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	if limit > h.listMax {
		limit = h.listMax
	}

	items, total, err := h.svc.List(r.Context(), limit, offset)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	out := listResponse{
		Notifications: make([]notificationResponse, 0, len(items)),
		Total:         total,
		Limit:         limit,
		Offset:        offset,
	}
	for _, n := range items {
		out.Notifications = append(out.Notifications, toResponse(n))
	}
	writeJSON(w, http.StatusOK, out)
}

// writeServiceError maps domain/service errors to HTTP responses.
func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	var ve *domain.ValidationErr
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, ve.Code, ve.Message, []validationErrorDTO{{
			Code: ve.Code, Field: ve.Field, Message: ve.Message,
		}})
	case errors.Is(err, domain.ErrChannelUnsupported):
		writeError(w, http.StatusBadRequest, "unsupported_channel", err.Error(), nil)
	case errors.Is(err, domain.ErrQueueFull):
		writeError(w, http.StatusServiceUnavailable, "queue_full", "service is busy, please retry later", nil)
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error(), nil)
	default:
		h.log.Error("internal error", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "unexpected error", nil)
	}
}

// decodeJSON decodes a request body with a size limit andDisallowUnknownFields
// to reject malformed/overly large payloads early.
func decodeJSON(r *http.Request, dst any) error {
	const maxBody = 1 << 20 // 1 MiB
	r.Body = http.MaxBytesReader(nil, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string, verrs []validationErrorDTO) {
	writeJSON(w, status, errorResponse{
		Error:  errorBody{Code: code, Message: message, ValidationErrors: verrs},
		Status: status,
	})
}
