package api

import (
	"time"

	"github.com/assessment/notify-service/internal/domain"
)

type SubmitRequest struct {
	Channel    domain.Channel     `json:"channel"`
	Recipients []domain.Recipient `json:"recipients"`
	Message    domain.Message     `json:"message"`
}

type SubmitResponse struct {
	ID     string        `json:"id"`
	Status domain.Status `json:"status"`
}

type NotificationResponse struct {
	ID               string                   `json:"id"`
	Channel          domain.Channel           `json:"channel"`
	Recipients       []domain.Recipient       `json:"recipients"`
	Message          domain.Message           `json:"message"`
	Status           domain.Status            `json:"status"`
	ValidationErrors []string                 `json:"validation_errors,omitempty"`
	DeliveryResults  []domain.DeliveryResult  `json:"delivery_results,omitempty"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
}

type ListResponse struct {
	Notifications []NotificationResponse `json:"notifications"`
	Total         int                    `json:"total"`
	Offset        int                    `json:"offset"`
	Limit         int                    `json:"limit"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func toNotificationResponse(n *domain.Notification) NotificationResponse {
	return NotificationResponse{
		ID:               n.ID,
		Channel:          n.Channel,
		Recipients:       n.Recipients,
		Message:          n.Message,
		Status:           n.Status,
		ValidationErrors: n.ValidationErrors,
		DeliveryResults:  n.DeliveryResults,
		CreatedAt:        n.CreatedAt,
		UpdatedAt:        n.UpdatedAt,
	}
}
