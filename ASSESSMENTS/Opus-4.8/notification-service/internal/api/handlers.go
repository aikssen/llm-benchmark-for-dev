package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/service"
	"github.com/aikssen/notification-service/internal/store"
)

// maxBodyBytes caps request bodies to protect against oversized/abusive payloads.
const maxBodyBytes = 1 << 20 // 1 MiB

// Notifier is the subset of the service the HTTP layer depends on. Depending on
// an interface (rather than *service.Service) keeps handlers testable with a
// fake and documents exactly what the transport needs.
type Notifier interface {
	Submit(ctx context.Context, req service.SubmitRequest) ([]*domain.Notification, error)
	Get(ctx context.Context, id string) (*domain.Notification, error)
	List(ctx context.Context, f store.ListFilter) ([]*domain.Notification, int, error)
}

// handleSubmit accepts a notification request, fans it out and returns the
// created notifications with 202 Accepted, reflecting that delivery happens
// asynchronously after the response.
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var req submitRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	notifications, err := s.notifier.Submit(r.Context(), service.SubmitRequest{
		Channel:    domain.Channel(req.Channel),
		Recipients: req.Recipients,
		Message:    req.Message.toDomain(),
	})
	if err != nil {
		s.writeServiceError(w, err)
		return
	}

	resp := submitResponse{
		Count:         len(notifications),
		Notifications: toDTOs(notifications),
	}
	if len(notifications) > 0 {
		resp.RequestID = notifications[0].RequestID
	}
	writeJSON(w, http.StatusAccepted, resp)
}

// handleGet returns the current state of a single notification.
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := s.notifier.Get(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(n))
}

// handleList returns previously submitted notifications, filtered and paginated.
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := store.ListFilter{
		Channel:   domain.Channel(q.Get("channel")),
		Status:    domain.Status(q.Get("status")),
		Recipient: q.Get("recipient"),
	}

	limit, err := parseIntParam(q.Get("limit"), 50)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit: "+err.Error())
		return
	}
	offset, err := parseIntParam(q.Get("offset"), 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid offset: "+err.Error())
		return
	}
	filter.Limit, filter.Offset = limit, offset

	items, total, err := s.notifier.List(r.Context(), filter)
	if err != nil {
		s.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, listResponse{
		Total:         total,
		Limit:         limit,
		Offset:        offset,
		Notifications: toDTOs(items),
	})
}

// handleHealth is a lightweight liveness probe.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- helpers -----------------------------------------------------------------

// writeServiceError maps domain errors to HTTP status codes in one place, so the
// individual handlers stay free of status-mapping logic.
func (s *Server) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "notification not found")
	case errors.Is(err, domain.ErrValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		// Do not leak internal error details to clients; log for operators.
		s.log.Error("unhandled service error", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

// decodeJSON strictly decodes a JSON request body, rejecting unknown fields and
// oversized payloads, and requiring exactly one JSON value.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid JSON body: " + err.Error())
	}
	// Ensure there is no trailing content after the first JSON value.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func parseIntParam(raw string, def int) (int, error) {
	if raw == "" {
		return def, nil
	}
	return strconv.Atoi(raw)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
