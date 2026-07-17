package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/anomalyco/notification-service/internal/application"
	"github.com/anomalyco/notification-service/internal/domain"
	"github.com/anomalyco/notification-service/internal/infrastructure/repository"
)

type submitRequest struct {
	Recipients []string       `json:"recipients"`
	Channel    string         `json:"channel"`
	Message    domain.Message `json:"message"`
}

type submitResponse struct {
	Notifications []*domain.Notification `json:"notifications"`
}

type errorResponse struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

type Handler struct {
	svc *application.NotificationService
}

func NewHandler(svc *application.NotificationService) *Handler {
	return &Handler{svc: svc}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	var verr *domain.ValidationError
	resp := errorResponse{Error: err.Error()}
	if errors.As(err, &verr) {
		resp.Field = verr.Field
	}
	writeJSON(w, status, resp)
}

func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	defer r.Body.Close()
	var req submitRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid json: "+err.Error()))
		return
	}
	channel, err := domain.ParseChannel(req.Channel)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := h.svc.Submit(r.Context(), req.Recipients, channel, req.Message)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, domain.ErrUnknownChannel) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusAccepted, submitResponse{Notifications: result.Notifications})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	id := r.PathValue("id")
	n, err := h.svc.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	filter := repository.ListFilter{
		Limit:  50,
		Offset: 0,
	}
	if c := r.URL.Query().Get("channel"); c != "" {
		ch, err := domain.ParseChannel(c)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		filter.Channel = ch
	}
	if s := r.URL.Query().Get("status"); s != "" {
		filter.Status = domain.Status(s)
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		v, err := strconv.Atoi(l)
		if err != nil || v < 0 {
			writeError(w, http.StatusBadRequest, errors.New("invalid limit"))
			return
		}
		filter.Limit = v
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		v, err := strconv.Atoi(o)
		if err != nil || v < 0 {
			writeError(w, http.StatusBadRequest, errors.New("invalid offset"))
			return
		}
		filter.Offset = v
	}
	items, err := h.svc.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": items,
		"limit":         filter.Limit,
		"offset":        filter.Offset,
		"count":         len(items),
	})
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}
