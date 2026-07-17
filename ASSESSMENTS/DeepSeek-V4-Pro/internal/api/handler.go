package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/assessment/notify-service/internal/domain"
	"github.com/assessment/notify-service/internal/orchestrator"
	"github.com/assessment/notify-service/internal/store"

	"github.com/google/uuid"
)

type Handler struct {
	orchestrator *orchestrator.Orchestrator
	store        store.Store
}

func NewHandler(o *orchestrator.Orchestrator, s store.Store) *Handler {
	return &Handler{orchestrator: o, store: s}
}

func (h *Handler) SubmitNotification(w http.ResponseWriter, r *http.Request) {
	var req SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body: " + err.Error()})
		return
	}

	newNotif := domain.NewNotification{
		Channel:    req.Channel,
		Recipients: req.Recipients,
		Message:    req.Message,
	}

	if err := newNotif.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	now := time.Now().UTC()
	notification := &domain.Notification{
		ID:        uuid.New().String(),
		Channel:   req.Channel,
		Recipients: req.Recipients,
		Message:   req.Message,
		Status:    domain.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := h.orchestrator.Submit(r.Context(), notification); err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to submit notification: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, SubmitResponse{
		ID:     notification.ID,
		Status: notification.Status,
	})
}

func (h *Handler) GetNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "notification id is required"})
		return
	}

	n, err := h.store.Get(r.Context(), id)
	if err == domain.ErrNotFound {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, toNotificationResponse(n))
}

func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	notifications, total, err := h.store.List(r.Context(), offset, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	res := ListResponse{
		Notifications: make([]NotificationResponse, 0, len(notifications)),
		Total:         total,
		Offset:        offset,
		Limit:         limit,
	}
	for _, n := range notifications {
		res.Notifications = append(res.Notifications, toNotificationResponse(n))
	}

	writeJSON(w, http.StatusOK, res)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
