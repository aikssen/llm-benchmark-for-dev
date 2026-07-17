package http

import "notification-service/internal/domain"

// SubmitRequest is the JSON body accepted by POST /notifications.
type SubmitRequest struct {
	Channel    string   `json:"channel"`
	Recipients []string `json:"recipients"`
	Subject    string   `json:"subject,omitempty"`
	Body       string   `json:"body"`
}

// SubmitResponse is returned immediately after accepting a notification.
type SubmitResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// ListResponse wraps a list of notifications.
type ListResponse struct {
	Notifications []domain.Notification `json:"notifications"`
	Total         int                   `json:"total"`
}
