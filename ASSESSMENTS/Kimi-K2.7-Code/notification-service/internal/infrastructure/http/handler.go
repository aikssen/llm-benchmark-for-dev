package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"notification-service/internal/application"
	"notification-service/internal/domain"
)

// Handler exposes the notification API.
type Handler struct {
	service *application.Service
}

// NewHandler creates an HTTP handler.
func NewHandler(service *application.Service) *Handler {
	return &Handler{service: service}
}

// Submit handles POST /notifications.
func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	var req SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}

	resp, err := h.service.Submit(r.Context(), application.SubmitRequest(req))
	if err != nil {
		status := httpStatusFromError(err)
		writeError(w, status, err.Error(), err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}

// GetStatus handles GET /notifications/{id}.
func (h *Handler) GetStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing notification id", nil)
		return
	}

	n, err := h.service.GetStatus(r.Context(), id)
	if err != nil {
		status := httpStatusFromError(err)
		writeError(w, status, err.Error(), err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(n)
}

// List handles GET /notifications?limit=&offset=.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	resp, err := h.service.List(r.Context(), application.ListRequest{Limit: limit, Offset: offset})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func httpStatusFromError(err error) int {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrInvalidRecipient),
		errors.Is(err, domain.ErrInvalidMessage),
		errors.Is(err, domain.ErrInvalidRequest),
		errors.Is(err, domain.ErrUnsupportedChannel):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrRateLimitExceeded):
		return http.StatusTooManyRequests
	case errors.Is(err, domain.ErrPreferenceBlocked):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

func writeError(w http.ResponseWriter, status int, message string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: errText, Message: message})
}
