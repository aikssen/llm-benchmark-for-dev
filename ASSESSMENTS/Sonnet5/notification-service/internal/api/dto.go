// Package api is the HTTP adapter: JSON (de)serialization, routing and
// translating between wire DTOs and the domain/service layer. It contains
// no business logic — that all lives in internal/service and below.
package api

import (
	"time"

	"github.com/aikssen/notification-service/internal/domain"
)

// MessageDTO is the wire representation of domain.Message.
type MessageDTO struct {
	Subject string            `json:"subject,omitempty"`
	Body    string            `json:"body"`
	Data    map[string]string `json:"data,omitempty"`
}

// SubmitRequest is the JSON body accepted by POST /v1/notifications.
type SubmitRequest struct {
	Channel    string     `json:"channel"`
	Recipients []string   `json:"recipients"`
	Message    MessageDTO `json:"message"`
}

// NotificationDTO is the JSON representation of a domain.Notification.
type NotificationDTO struct {
	ID        string     `json:"id"`
	BatchID   string     `json:"batch_id"`
	Channel   string     `json:"channel"`
	Recipient string     `json:"recipient"`
	Message   MessageDTO `json:"message"`
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	LastError string     `json:"last_error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func toNotificationDTO(n *domain.Notification) NotificationDTO {
	return NotificationDTO{
		ID:        n.ID,
		BatchID:   n.BatchID,
		Channel:   string(n.Channel),
		Recipient: n.Recipient,
		Message: MessageDTO{
			Subject: n.Message.Subject,
			Body:    n.Message.Body,
			Data:    n.Message.Data,
		},
		Status:    string(n.Status),
		Attempts:  n.Attempts,
		LastError: n.LastError,
		CreatedAt: n.CreatedAt,
		UpdatedAt: n.UpdatedAt,
	}
}

// SubmitResponse is returned by POST /v1/notifications.
type SubmitResponse struct {
	BatchID       string            `json:"batch_id"`
	Notifications []NotificationDTO `json:"notifications"`
}

// ListResponse is returned by GET /v1/notifications.
type ListResponse struct {
	Notifications []NotificationDTO `json:"notifications"`
	Total         int               `json:"total"`
	Limit         int               `json:"limit"`
	Offset        int               `json:"offset"`
}

// ErrorResponse is the JSON body for any non-2xx response.
type ErrorResponse struct {
	Error string `json:"error"`
}
