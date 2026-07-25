package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/service"
	"github.com/evercifuentes/notifier/internal/store"
)

// handler holds the dependencies of the notification endpoints.
type handler struct {
	svc    *service.Service
	logger *slog.Logger
}

// submit handles POST /v1/notifications.
//
// It answers 202 Accepted, not 201: the notification has been accepted for
// delivery, but delivery itself has not happened yet. Saying 201 would imply
// a result the service cannot yet promise.
func (h *handler) submit(w http.ResponseWriter, r *http.Request) {
	var req submitRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			writeError(w, r, h.logger, http.StatusRequestEntityTooLarge, codePayloadTooBig,
				"request body exceeds the maximum allowed size", "")
			return
		}
		writeError(w, r, h.logger, http.StatusBadRequest, codeMalformedJSON, err.Error(), "")
		return
	}

	// An idempotency key is a header rather than a body field: it describes
	// the delivery of the request, not the notification being requested.
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))

	notification, err := h.svc.Submit(r.Context(), req.toCommand(idempotencyKey))
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}

	w.Header().Set("Location", "/v1/notifications/"+notification.ID)
	writeJSON(w, h.logger, http.StatusAccepted, toNotificationResponse(notification))
}

// get handles GET /v1/notifications/{id}.
func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	notification, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	writeJSON(w, h.logger, http.StatusOK, toNotificationResponse(notification))
}

// list handles GET /v1/notifications.
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit, err := intParam(q.Get("limit"), "limit")
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	offset, err := intParam(q.Get("offset"), "offset")
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}

	page, err := h.svc.List(r.Context(), store.Filter{
		Channel: domain.ParseChannel(q.Get("channel")),
		Status:  domain.Status(strings.ToLower(strings.TrimSpace(q.Get("status")))),
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	writeJSON(w, h.logger, http.StatusOK, toListResponse(page))
}

// channels handles GET /v1/channels. It exists so clients can discover what a
// deployment supports instead of hard-coding the list.
func (h *handler) channels(w http.ResponseWriter, _ *http.Request) {
	channels := h.svc.Channels()
	names := make([]string, len(channels))
	for i, c := range channels {
		names[i] = c.String()
	}
	writeJSON(w, h.logger, http.StatusOK, map[string]any{"channels": names})
}

// decodeJSON strictly decodes a request body.
//
// Unknown fields are rejected: silently ignoring a misspelled "recipeints"
// would send a notification that does not match what the caller asked for.
func decodeJSON(body io.Reader, v any) error {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.Is(err, io.EOF):
			return errors.New("a JSON request body is required")
		case errors.As(err, &syntaxErr):
			return errors.New("request body is not valid JSON")
		case errors.As(err, &typeErr):
			return errors.New("field " + strconv.Quote(typeErr.Field) + " has the wrong type")
		default:
			return err
		}
	}
	// Reject trailing content so two concatenated objects are not silently
	// truncated to the first.
	if err := dec.Decode(new(struct{})); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON object")
	}
	return nil
}

// intParam parses an optional non-negative integer query parameter.
func intParam(raw, name string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, domain.Invalid(name, "%q is not a valid integer", raw)
	}
	if n < 0 {
		return 0, domain.Invalid(name, "must not be negative")
	}
	return n, nil
}
