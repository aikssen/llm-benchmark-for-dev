package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"notificationservice/internal/domain"
)

// ---------------------------------------------------------------------------
// Request payloads
// ---------------------------------------------------------------------------

type createRequest struct {
	Channel    string     `json:"channel"`
	Recipients []string   `json:"recipients"`
	Message    messageDTO `json:"message"`
}

type messageDTO struct {
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body"`
}

// ---------------------------------------------------------------------------
// Response payloads
// ---------------------------------------------------------------------------

type notificationResponse struct {
	ID         string             `json:"id"`
	Channel    string             `json:"channel"`
	Status     string             `json:"status"`
	Message    messageDTO         `json:"message"`
	CreatedAt  time.Time          `json:"created_at"`
	Deliveries []deliveryResponse `json:"deliveries"`
}

type deliveryResponse struct {
	ID        string    `json:"id"`
	Recipient string    `json:"recipient"`
	Status    string    `json:"status"`
	Attempts  int       `json:"attempts"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type listResponse struct {
	Items  []notificationResponse `json:"items"`
	Total  int                    `json:"total"`
	Limit  int                    `json:"limit"`
	Offset int                    `json:"offset"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func toNotificationResponse(n domain.Notification) notificationResponse {
	resp := notificationResponse{
		ID:      n.ID,
		Channel: string(n.Channel),
		Status:  string(n.Status()),
		Message: messageDTO{
			Subject: n.Message.Subject,
			Body:    n.Message.Body,
		},
		CreatedAt:  n.CreatedAt,
		Deliveries: make([]deliveryResponse, 0, len(n.Deliveries)),
	}
	for _, d := range n.Deliveries {
		resp.Deliveries = append(resp.Deliveries, deliveryResponse{
			ID:        d.ID,
			Recipient: d.Recipient,
			Status:    string(d.Status),
			Attempts:  d.Attempts,
			Error:     d.LastError,
			UpdatedAt: d.UpdatedAt,
		})
	}
	return resp
}

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

// decodeJSON parses the request body into dst, enforcing a size cap and
// rejecting unknown fields so client typos surface as errors.
func decodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if maxErr, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return fmt.Errorf("request body exceeds %d bytes", maxErr.Limit)
		}
		return fmt.Errorf("invalid JSON body: %v", err)
	}
	if dec.More() {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: message}})
}
