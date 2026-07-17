// Package api exposes the HTTP interface of the notification service.
//
// Endpoints:
//
//	POST /v1/notifications       submit a notification (async, returns 202)
//	GET  /v1/notifications/{id}  current status incl. per-recipient deliveries
//	GET  /v1/notifications       list notifications (pagination + filters)
//	GET  /healthz                liveness probe
package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"notificationservice/internal/domain"
	"notificationservice/internal/id"
	"notificationservice/internal/store"
	"notificationservice/internal/worker"
)

// Enqueuer is the slice of the worker pool the API needs; decoupled for
// testability.
type Enqueuer interface {
	EnqueueBatch(jobs []worker.Job) error
}

// Config tunes API request handling.
type Config struct {
	// MaxRecipients caps recipients per request to keep work per request
	// bounded and queue accounting fair.
	MaxRecipients int
	// MaxBodyBytes caps the request body size.
	MaxBodyBytes int64
	// DefaultPageSize and MaxPageSize control list pagination.
	DefaultPageSize int
	MaxPageSize     int
}

type handler struct {
	cfg      Config
	store    store.Store
	enqueuer Enqueuer
	logger   *slog.Logger
	now      func() time.Time
}

// New assembles the HTTP handler with routing and middleware.
func New(cfg Config, st store.Store, enq Enqueuer, logger *slog.Logger) http.Handler {
	h := &handler{cfg: cfg, store: st, enqueuer: enq, logger: logger, now: time.Now}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/notifications", h.createNotification)
	mux.HandleFunc("GET /v1/notifications/{id}", h.getNotification)
	mux.HandleFunc("GET /v1/notifications", h.listNotifications)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return recoverPanics(logRequests(mux, logger), logger)
}

// createNotification validates the request shape synchronously, persists the
// notification, and enqueues one job per recipient. Business validations
// (preferences, rate limits, ...) run asynchronously in the workers.
func (h *handler) createNotification(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decodeJSON(w, r, h.cfg.MaxBodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}

	recipients, err := h.validateCreate(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	now := h.now().UTC()
	n := domain.Notification{
		ID:      id.New("ntf"),
		Channel: domain.Channel(req.Channel),
		Message: domain.Message{
			Subject: req.Message.Subject,
			Body:    req.Message.Body,
		},
		CreatedAt: now,
	}
	jobs := make([]worker.Job, 0, len(recipients))
	for _, recipient := range recipients {
		d := domain.Delivery{
			ID:             id.New("dlv"),
			NotificationID: n.ID,
			Recipient:      recipient,
			Status:         domain.DeliveryPending,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		n.Deliveries = append(n.Deliveries, d)
		jobs = append(jobs, worker.Job{NotificationID: n.ID, DeliveryID: d.ID})
	}

	// Persist before enqueueing so workers always find the record; roll back
	// if the queue cannot absorb the batch.
	if err := h.store.CreateNotification(r.Context(), n); err != nil {
		h.logger.Error("cannot persist notification", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not store notification")
		return
	}
	if err := h.enqueuer.EnqueueBatch(jobs); err != nil {
		if delErr := h.store.DeleteNotification(r.Context(), n.ID); delErr != nil {
			h.logger.Error("cannot roll back notification after enqueue failure", "notification_id", n.ID, "error", delErr)
		}
		if errors.Is(err, worker.ErrQueueFull) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "queue_full",
				"the service is at capacity, retry shortly")
			return
		}
		h.logger.Error("cannot enqueue deliveries", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not enqueue notification")
		return
	}

	writeJSON(w, http.StatusAccepted, toNotificationResponse(n))
}

// validateCreate checks the structural shape of the request (cheap, fails the
// request synchronously) and returns the deduplicated recipient list.
func (h *handler) validateCreate(req createRequest) ([]string, error) {
	ch := domain.Channel(req.Channel)
	if !ch.Valid() {
		return nil, fmt.Errorf("unknown channel %q, supported: %v", req.Channel, domain.Channels())
	}
	if req.Message.Body == "" {
		return nil, errors.New("message.body is required")
	}
	if len(req.Recipients) == 0 {
		return nil, errors.New("at least one recipient is required")
	}

	// Deduplicate while preserving order; duplicates would double-send.
	seen := make(map[string]struct{}, len(req.Recipients))
	recipients := make([]string, 0, len(req.Recipients))
	for _, r := range req.Recipients {
		r = strings.TrimSpace(r)
		if r == "" {
			return nil, errors.New("recipients must not be empty strings")
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		recipients = append(recipients, r)
	}
	if len(recipients) > h.cfg.MaxRecipients {
		return nil, fmt.Errorf("too many recipients: %d, maximum is %d", len(recipients), h.cfg.MaxRecipients)
	}
	return recipients, nil
}

func (h *handler) getNotification(w http.ResponseWriter, r *http.Request) {
	n, err := h.store.GetNotification(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "notification not found")
		return
	}
	if err != nil {
		h.logger.Error("cannot load notification", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not load notification")
		return
	}
	writeJSON(w, http.StatusOK, toNotificationResponse(n))
}

func (h *handler) listNotifications(w http.ResponseWriter, r *http.Request) {
	filter, err := h.parseListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	result, err := h.store.ListNotifications(r.Context(), filter)
	if err != nil {
		h.logger.Error("cannot list notifications", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not list notifications")
		return
	}

	items := make([]notificationResponse, 0, len(result.Notifications))
	for _, n := range result.Notifications {
		items = append(items, toNotificationResponse(n))
	}
	writeJSON(w, http.StatusOK, listResponse{
		Items:  items,
		Total:  result.Total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

func (h *handler) parseListFilter(r *http.Request) (store.ListFilter, error) {
	q := r.URL.Query()
	filter := store.ListFilter{Limit: h.cfg.DefaultPageSize}

	if v := q.Get("limit"); v != "" {
		limit, err := strconv.Atoi(v)
		if err != nil || limit < 1 || limit > h.cfg.MaxPageSize {
			return filter, fmt.Errorf("limit must be an integer between 1 and %d", h.cfg.MaxPageSize)
		}
		filter.Limit = limit
	}
	if v := q.Get("offset"); v != "" {
		offset, err := strconv.Atoi(v)
		if err != nil || offset < 0 {
			return filter, errors.New("offset must be a non-negative integer")
		}
		filter.Offset = offset
	}
	if v := q.Get("channel"); v != "" {
		ch := domain.Channel(v)
		if !ch.Valid() {
			return filter, fmt.Errorf("unknown channel %q", v)
		}
		filter.Channel = ch
	}
	if v := q.Get("status"); v != "" {
		switch s := domain.Status(v); s {
		case domain.StatusPending, domain.StatusProcessing, domain.StatusDelivered,
			domain.StatusPartiallyDelivered, domain.StatusFailed:
			filter.Status = s
		default:
			return filter, fmt.Errorf("unknown status %q", v)
		}
	}
	return filter, nil
}
