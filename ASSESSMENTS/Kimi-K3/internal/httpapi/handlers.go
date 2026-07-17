package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"notifications/internal/dispatcher"
	"notifications/internal/domain"
	"notifications/internal/store"
)

// ---- Request/response shapes -------------------------------------------

type createRequest struct {
	Channel    string         `json:"channel"`
	Recipients []string       `json:"recipients"`
	Content    domain.Content `json:"content"`
}

type notificationSummary struct {
	ID        string `json:"id"`
	Recipient string `json:"recipient"`
	Channel   string `json:"channel"`
	Status    string `json:"status"`
}

type createResponse struct {
	RequestID     string                `json:"request_id"`
	Notifications []notificationSummary `json:"notifications"`
}

type listResponse struct {
	Items  []*domain.Notification `json:"items"`
	Count  int                    `json:"count"`
	Limit  int                    `json:"limit"`
	Offset int                    `json:"offset"`
}

// ---- Handlers -----------------------------------------------------------

// handleCreate accepts a notification request for one or more recipients,
// persists one notification per recipient (status pending) and enqueues
// them for asynchronous processing. Returns 202 Accepted.
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is not valid JSON: "+err.Error())
		return
	}
	if err := s.validateCreate(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	now := time.Now().UTC()
	requestID := domain.NewID()
	notifications := make([]*domain.Notification, 0, len(req.Recipients))
	ids := make([]string, 0, len(req.Recipients))
	for _, recipient := range req.Recipients {
		n := domain.New(requestID, domain.Channel(req.Channel), recipient, req.Content, now)
		notifications = append(notifications, n)
		ids = append(ids, n.ID)
	}

	// Persist first, then enqueue atomically (all-or-nothing) so a worker
	// can never look up an ID that is not stored yet.
	for _, n := range notifications {
		if err := s.store.Upsert(r.Context(), n); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "failed to persist notification")
			return
		}
	}
	if err := s.dispatcher.EnqueueBatch(ids); err != nil {
		switch {
		case errors.Is(err, dispatcher.ErrQueueFull):
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "overloaded",
				"processing queue is full; retry later")
		case errors.Is(err, dispatcher.ErrStopped):
			writeError(w, http.StatusServiceUnavailable, "shutting_down", "service is shutting down")
		default:
			writeError(w, http.StatusInternalServerError, "internal", "failed to enqueue notifications")
		}
		return
	}

	resp := createResponse{RequestID: requestID}
	for _, n := range notifications {
		resp.Notifications = append(resp.Notifications, notificationSummary{
			ID:        n.ID,
			Recipient: n.Recipient,
			Channel:   string(n.Channel),
			Status:    string(n.Status),
		})
	}
	writeJSON(w, http.StatusAccepted, resp)
}

// validateCreate checks the request *shape* only. Business validations
// (recipient format, preferences, rate limits, content rules) run
// asynchronously in the validation pipeline and are reported through the
// notification status.
func (s *Server) validateCreate(req *createRequest) error {
	if !s.registry.Supports(domain.Channel(req.Channel)) {
		return errors.New("unsupported channel " + strconv.Quote(req.Channel))
	}
	if len(req.Recipients) == 0 {
		return errors.New("at least one recipient is required")
	}
	if len(req.Recipients) > s.maxRecips {
		return errors.New("too many recipients: max " + strconv.Itoa(s.maxRecips) + " per request")
	}
	for _, rcp := range req.Recipients {
		if len(rcp) == 0 || len(rcp) > 512 {
			return errors.New("recipients must be between 1 and 512 characters")
		}
	}
	if len(req.Content.Body) == 0 {
		return errors.New("content.body is required")
	}
	if len(req.Content.Body) > 100_000 {
		return errors.New("content.body is too large")
	}
	if len(req.Content.Subject) > 1000 {
		return errors.New("content.subject is too large")
	}
	return nil
}

// handleGet returns the current state of one notification.
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "notification not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to load notification")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

// handleList lists notifications with optional filters and pagination.
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := store.ListFilter{
		RequestID: q.Get("request_id"),
		Limit:     clampAtoi(q.Get("limit"), store.DefaultListLimit, 1, 200),
		Offset:    clampAtoi(q.Get("offset"), 0, 0, 1_000_000),
	}

	if v := q.Get("status"); v != "" {
		st := domain.Status(v)
		if !st.Valid() {
			writeError(w, http.StatusBadRequest, "invalid_request", "unknown status "+strconv.Quote(v))
			return
		}
		filter.Status = st
	}
	if v := q.Get("channel"); v != "" {
		ch := domain.Channel(v)
		if !s.registry.Supports(ch) {
			writeError(w, http.StatusBadRequest, "invalid_request", "unknown channel "+strconv.Quote(v))
			return
		}
		filter.Channel = ch
	}

	items, err := s.store.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to list notifications")
		return
	}
	if items == nil {
		items = []*domain.Notification{}
	}
	writeJSON(w, http.StatusOK, listResponse{
		Items:  items,
		Count:  len(items),
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

func clampAtoi(s string, def, min, max int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < min {
		return def
	}
	if v > max {
		return max
	}
	return v
}
