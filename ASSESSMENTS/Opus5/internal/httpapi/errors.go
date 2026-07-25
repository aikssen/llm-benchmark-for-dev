package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/service"
)

// errorResponse is the single error shape the API returns, so clients can
// write one parser and rely on `code` rather than on prose.
type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	// Code is stable and machine-readable.
	Code string `json:"code"`
	// Message is human-readable and safe to show to a developer.
	Message string `json:"message"`
	// Field is set when the failure is attributable to one input field.
	Field string `json:"field,omitempty"`
	// RequestID lets a client quote a specific request in a bug report.
	RequestID string `json:"request_id,omitempty"`
}

// Error codes returned by the API.
const (
	codeInvalidRequest   = "invalid_request"
	codeMalformedJSON    = "malformed_json"
	codeNotFound         = "not_found"
	codeMethodNotAllowed = "method_not_allowed"
	codeOverloaded       = "service_overloaded"
	codeInternal         = "internal_error"
	codePayloadTooBig    = "payload_too_large"
)

// writeJSON renders v as JSON with the given status.
func writeJSON(w http.ResponseWriter, logger *slog.Logger, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already sent, so this can only be logged.
		logger.Error("could not write response body", slog.String("error", err.Error()))
	}
}

// writeError renders an error payload.
func writeError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, status int, code, message, field string) {
	writeJSON(w, logger, status, errorResponse{Error: errorBody{
		Code:      code,
		Message:   message,
		Field:     field,
		RequestID: RequestIDFrom(r.Context()),
	}})
}

// respondError maps an application error onto an HTTP response.
//
// Mapping lives in one place so that a new error type cannot silently start
// returning 500s, and so internal details are never leaked to clients.
func respondError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var invalid *domain.InvalidInputError

	switch {
	case errors.As(err, &invalid):
		writeError(w, r, logger, http.StatusBadRequest, codeInvalidRequest, invalid.Message, invalid.Field)

	case errors.Is(err, domain.ErrNotFound):
		writeError(w, r, logger, http.StatusNotFound, codeNotFound, "notification not found", "")

	case errors.Is(err, service.ErrOverloaded):
		// 429 rather than 503: the service is healthy, it is the caller's
		// rate that must come down. Retry-After makes that actionable.
		w.Header().Set("Retry-After", "1")
		writeError(w, r, logger, http.StatusTooManyRequests, codeOverloaded,
			"the service is at capacity, please retry shortly", "")

	default:
		// Unexpected: log the detail, return none of it.
		logger.ErrorContext(r.Context(), "unhandled error",
			slog.String("error", err.Error()),
			slog.String("path", r.URL.Path))
		writeError(w, r, logger, http.StatusInternalServerError, codeInternal,
			"an unexpected error occurred", "")
	}
}
