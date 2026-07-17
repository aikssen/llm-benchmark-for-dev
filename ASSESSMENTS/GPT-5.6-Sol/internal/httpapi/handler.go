package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"notification/internal/notification"
	"notification/internal/service"
)

const maxRequestBytes = 1 << 20

type Handler struct {
	service *service.Service
	logger  *slog.Logger
}

func New(service *service.Service, logger *slog.Logger) http.Handler {
	handler := &Handler{service: service, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.health)
	mux.HandleFunc("GET /readyz", handler.health)
	mux.HandleFunc("POST /v1/notifications", handler.submit)
	mux.HandleFunc("GET /v1/notifications", handler.list)
	mux.HandleFunc("GET /v1/notifications/{id}", handler.get)
	return handler.recover(handler.logRequests(mux))
}

type submitRequest struct {
	Recipients []string             `json:"recipients"`
	Channel    notification.Channel `json:"channel"`
	Message    notification.Message `json:"message"`
}

func (h *Handler) submit(writer http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload submitRequest
	if err = decoder.Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_json", "request body must be a valid JSON object")
		return
	}
	if err = ensureEOF(decoder); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_json", "request body must contain exactly one JSON object")
		return
	}

	value, replayed, err := h.service.Submit(request.Context(), service.Submission{
		Recipients:     payload.Recipients,
		Channel:        payload.Channel,
		Message:        payload.Message,
		IdempotencyKey: request.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		h.writeServiceError(writer, err)
		return
	}
	writer.Header().Set("Location", "/v1/notifications/"+value.ID)
	status := http.StatusAccepted
	if replayed {
		status = http.StatusOK
		writer.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(writer, status, value)
}

func (h *Handler) get(writer http.ResponseWriter, request *http.Request) {
	id := strings.TrimSpace(request.PathValue("id"))
	if id == "" {
		writeError(writer, http.StatusNotFound, "not_found", "notification not found")
		return
	}
	value, err := h.service.Get(request.Context(), id)
	if err != nil {
		h.writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *Handler) list(writer http.ResponseWriter, request *http.Request) {
	limit, err := queryInt(request, "limit", 20)
	if err != nil || limit < 1 || limit > 100 {
		writeError(writer, http.StatusBadRequest, "invalid_pagination", "limit must be between 1 and 100")
		return
	}
	offset, err := queryInt(request, "offset", 0)
	if err != nil || offset < 0 {
		writeError(writer, http.StatusBadRequest, "invalid_pagination", "offset must be zero or greater")
		return
	}
	page, err := h.service.List(request.Context(), limit, offset)
	if err != nil {
		h.writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"data": page.Items,
		"pagination": map[string]int{
			"total": page.Total, "limit": page.Limit, "offset": page.Offset,
		},
	})
}

func (*Handler) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) writeServiceError(writer http.ResponseWriter, err error) {
	var invalid service.InvalidInputError
	switch {
	case errors.As(err, &invalid):
		writeError(writer, http.StatusBadRequest, "invalid_request", invalid.Error())
	case errors.Is(err, notification.ErrNotFound):
		writeError(writer, http.StatusNotFound, "not_found", "notification not found")
	case errors.Is(err, notification.ErrIdempotencyConflict):
		writeError(writer, http.StatusConflict, "idempotency_conflict", err.Error())
	case errors.Is(err, service.ErrQueueFull):
		writer.Header().Set("Retry-After", "1")
		writeError(writer, http.StatusServiceUnavailable, "queue_full", "service is temporarily at capacity")
	case errors.Is(err, service.ErrServiceClosed):
		writeError(writer, http.StatusServiceUnavailable, "shutting_down", "service is shutting down")
	default:
		h.logger.Error("request failed", "error", err)
		writeError(writer, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}

func (h *Handler) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				h.logger.Error("panic recovered", "panic", recovered)
				writeError(writer, http.StatusInternalServerError, "internal_error", "an internal error occurred")
			}
		}()
		next.ServeHTTP(writer, request)
	})
}

func (h *Handler) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorder := &statusRecorder{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(recorder, request)
		h.logger.Info("http request", "method", request.Method, "path", request.URL.Path, "status", recorder.status)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func queryInt(request *http.Request, key string, fallback int) (int, error) {
	value := request.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return errors.New("additional JSON value")
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
