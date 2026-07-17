package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/example/notification-orchestrator/internal/domain"
	"github.com/example/notification-orchestrator/internal/service"
	"github.com/example/notification-orchestrator/internal/store"
)

type Handler struct {
	service *service.Service
	logger  *log.Logger
}

func NewHandler(service *service.Service, logger *log.Logger) http.Handler {
	handler := &Handler{service: service, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.health)
	mux.HandleFunc("POST /v1/notifications", handler.createNotification)
	mux.HandleFunc("GET /v1/notifications", handler.listNotifications)
	mux.HandleFunc("GET /v1/notifications/{id}", handler.getNotification)
	return handler.withRecovery(handler.withJSON(mux))
}

type createNotificationRequest struct {
	Recipients []string       `json:"recipients"`
	Channel    domain.Channel `json:"channel"`
	Message    string         `json:"message"`
}

func (h *Handler) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) createNotification(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
	defer request.Body.Close()
	var input createNotificationRequest
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(writer, http.StatusBadRequest, "invalid_json", "request body must contain one JSON object")
		return
	}

	notification, created, err := h.service.Submit(service.CreateRequest(input), request.Header.Get("Idempotency-Key"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidRequest):
			writeError(writer, http.StatusBadRequest, "invalid_request", "channel, message, and one or more recipients are required")
		case errors.Is(err, store.ErrIdempotencyConflict):
			writeError(writer, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was already used for a different request")
		case errors.Is(err, service.ErrQueueFull):
			writer.Header().Set("Retry-After", "1")
			writeError(writer, http.StatusServiceUnavailable, "queue_full", "try again shortly")
		default:
			writeError(writer, http.StatusServiceUnavailable, "unavailable", "notification service is shutting down")
		}
		return
	}
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
	}
	writeJSON(writer, status, notification)
}

func (h *Handler) getNotification(writer http.ResponseWriter, request *http.Request) {
	notification, err := h.service.Get(request.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(writer, http.StatusNotFound, "not_found", "notification does not exist")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "internal_error", "could not retrieve notification")
		return
	}
	writeJSON(writer, http.StatusOK, notification)
}

func (h *Handler) listNotifications(writer http.ResponseWriter, request *http.Request) {
	limit := 20
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(writer, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	page := h.service.List(store.ListOptions{Limit: limit, Cursor: request.URL.Query().Get("cursor")})
	writeJSON(writer, http.StatusOK, struct {
		Items      []*domain.Notification `json:"items"`
		NextCursor string                 `json:"next_cursor,omitempty"`
	}{Items: page.Items, NextCursor: page.NextCursor})
}

func (h *Handler) withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		next.ServeHTTP(writer, request)
	})
}

func (h *Handler) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				h.logger.Printf("panic handling %s %s: %v", request.Method, request.URL.Path, recovered)
				writeError(writer, http.StatusInternalServerError, "internal_error", "unexpected server error")
			}
		}()
		next.ServeHTTP(writer, request)
	})
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		// Response may already have started; logging it requires no client-visible recovery.
		return
	}
}
