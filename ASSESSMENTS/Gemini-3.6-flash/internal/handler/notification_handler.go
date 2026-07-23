package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"notification-service/internal/domain"
	"notification-service/internal/service"
)

type NotificationHandler struct {
	svc service.Orchestrator
}

func NewNotificationHandler(svc service.Orchestrator) *NotificationHandler {
	return &NotificationHandler{svc: svc}
}

func (h *NotificationHandler) Submit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var input service.SubmitNotificationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid JSON payload: "+err.Error())
		return
	}

	req, err := h.svc.SubmitNotification(r.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidChannel),
			errors.Is(err, domain.ErrEmptyRecipients),
			errors.Is(err, domain.ErrEmptyContent):
			respondError(w, http.StatusBadRequest, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "Internal server error: "+err.Error())
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", fmt.Sprintf("/api/v1/notifications/%s", req.ID))
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(req)
}

func (h *NotificationHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/notifications/")
	id = strings.TrimSpace(id)

	if id == "" || strings.Contains(id, "/") {
		respondError(w, http.StatusBadRequest, "Missing or invalid notification ID")
		return
	}

	req, err := h.svc.GetNotificationStatus(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotificationNotFound) {
			respondError(w, http.StatusNotFound, "Notification request not found")
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(req)
}

func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	query := r.URL.Query()
	channelStr := query.Get("channel")
	statusStr := query.Get("status")
	limitStr := query.Get("limit")
	offsetStr := query.Get("offset")

	limit := 10
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	offset := 0
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	filter := domain.NotificationFilter{
		Channel: domain.Channel(channelStr),
		Status:  domain.Status(statusStr),
		Limit:   limit,
		Offset:  offset,
	}

	res, err := h.svc.ListNotifications(r.Context(), filter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

func respondError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error":       message,
		"status_code": statusCode,
	})
}
