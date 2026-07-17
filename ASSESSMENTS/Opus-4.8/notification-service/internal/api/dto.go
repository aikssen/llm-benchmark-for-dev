package api

import (
	"github.com/aikssen/notification-service/internal/domain"
)

// The DTOs decouple the wire format from the domain model. Keeping them separate
// means the internal model can change without breaking API clients, and vice
// versa.

// submitRequest is the JSON body of POST /v1/notifications. A single request may
// target one or many recipients that share the same channel and message.
type submitRequest struct {
	Channel    string     `json:"channel"`
	Recipients []string   `json:"recipients"`
	Message    messageDTO `json:"message"`
}

type messageDTO struct {
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body"`
}

func (m messageDTO) toDomain() domain.Message {
	return domain.Message{Subject: m.Subject, Body: m.Body}
}

// notificationDTO is the API representation of a notification.
type notificationDTO struct {
	ID        string     `json:"id"`
	RequestID string     `json:"request_id"`
	Channel   string     `json:"channel"`
	Recipient string     `json:"recipient"`
	Message   messageDTO `json:"message"`
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	LastError string     `json:"last_error,omitempty"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

func toDTO(n *domain.Notification) notificationDTO {
	return notificationDTO{
		ID:        n.ID,
		RequestID: n.RequestID,
		Channel:   string(n.Channel),
		Recipient: n.Recipient,
		Message:   messageDTO{Subject: n.Message.Subject, Body: n.Message.Body},
		Status:    string(n.Status),
		Attempts:  n.Attempts,
		LastError: n.LastError,
		CreatedAt: n.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt: n.UpdatedAt.UTC().Format(timeFormat),
	}
}

func toDTOs(ns []*domain.Notification) []notificationDTO {
	out := make([]notificationDTO, len(ns))
	for i, n := range ns {
		out[i] = toDTO(n)
	}
	return out
}

// submitResponse is returned by POST /v1/notifications. It echoes the created
// notifications (one per recipient) so the client learns each ID and can poll
// its status.
type submitResponse struct {
	RequestID     string            `json:"request_id"`
	Count         int               `json:"count"`
	Notifications []notificationDTO `json:"notifications"`
}

// listResponse is returned by GET /v1/notifications.
type listResponse struct {
	Total         int               `json:"total"`
	Limit         int               `json:"limit"`
	Offset        int               `json:"offset"`
	Notifications []notificationDTO `json:"notifications"`
}

// errorResponse is the uniform error envelope for all failures.
type errorResponse struct {
	Error string `json:"error"`
}

const timeFormat = "2006-01-02T15:04:05.000Z07:00"
