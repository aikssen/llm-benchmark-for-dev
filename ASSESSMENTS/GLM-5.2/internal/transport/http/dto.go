// Package httptransport exposes the notification service over a REST/JSON
// API using only the Go standard library (Go 1.22+ method-pattern ServeMux).
package httptransport

import (
	"strings"
	"time"

	"github.com/example/notification-service/internal/domain"
)

// createRequest is the payload for POST /notifications.
type createRequest struct {
	Channel    string         `json:"channel"`
	Recipients []recipientDTO `json:"recipients"`
	Message    messageDTO     `json:"message"`
}

type recipientDTO struct {
	ID        string `json:"id"`
	Email     string `json:"email,omitempty"`
	Phone     string `json:"phone,omitempty"`
	PushToken string `json:"push_token,omitempty"`
}

type messageDTO struct {
	Subject string            `json:"subject,omitempty"`
	Body    string            `json:"body"`
	Data    map[string]string `json:"data,omitempty"`
}

// notificationResponse is the canonical serialization of a notification.
type notificationResponse struct {
	ID         string               `json:"id"`
	Channel    string               `json:"channel"`
	Status     string               `json:"status"`
	Recipients []recipientDTO       `json:"recipients"`
	Message    messageDTO           `json:"message"`
	Results    []recipientResultDTO `json:"results"`
	CreatedAt  time.Time            `json:"created_at"`
	UpdatedAt  time.Time            `json:"updated_at"`
}

type recipientResultDTO struct {
	RecipientID      string               `json:"recipient_id"`
	Status           string               `json:"status"`
	Attempts         int                  `json:"attempts"`
	LastError        string               `json:"last_error,omitempty"`
	ValidationErrors []validationErrorDTO `json:"validation_errors,omitempty"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

type validationErrorDTO struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type listResponse struct {
	Notifications []notificationResponse `json:"notifications"`
	Total         int                    `json:"total"`
	Limit         int                    `json:"limit"`
	Offset        int                    `json:"offset"`
}

type errorResponse struct {
	Error  errorBody `json:"error"`
	Status int       `json:"-"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Detail carries validation errors when applicable.
	ValidationErrors []validationErrorDTO `json:"validation_errors,omitempty"`
}

func toRecipientDTO(r domain.Recipient) recipientDTO {
	return recipientDTO{ID: r.ID, Email: r.Email, Phone: r.Phone, PushToken: r.PushToken}
}

func toMessageDTO(m domain.Message) messageDTO {
	return messageDTO{Subject: m.Subject, Body: m.Body, Data: m.Data}
}

func toResponse(n *domain.Notification) notificationResponse {
	recipients := make([]recipientDTO, 0, len(n.Recipients))
	for _, r := range n.Recipients {
		recipients = append(recipients, toRecipientDTO(r))
	}
	results := make([]recipientResultDTO, 0, len(n.Results))
	for _, r := range n.Results {
		verrs := make([]validationErrorDTO, 0, len(r.ValidationErrors))
		for _, ve := range r.ValidationErrors {
			verrs = append(verrs, validationErrorDTO{Code: ve.Code, Field: ve.Field, Message: ve.Message})
		}
		results = append(results, recipientResultDTO{
			RecipientID:      r.RecipientID,
			Status:           string(r.Status),
			Attempts:         r.Attempts,
			LastError:        r.LastError,
			ValidationErrors: verrs,
			UpdatedAt:        r.UpdatedAt,
		})
	}
	return notificationResponse{
		ID:         string(n.ID),
		Channel:    string(n.Channel),
		Status:     string(n.Status),
		Recipients: recipients,
		Message:    toMessageDTO(n.Message),
		Results:    results,
		CreatedAt:  n.CreatedAt,
		UpdatedAt:  n.UpdatedAt,
	}
}

// parseChannel normalizes and validates the channel string.
func parseChannel(s string) (domain.Channel, error) {
	switch strings.ToLower(s) {
	case "email":
		return domain.ChannelEmail, nil
	case "sms":
		return domain.ChannelSMS, nil
	case "push":
		return domain.ChannelPush, nil
	default:
		return "", domain.ErrChannelUnsupported
	}
}
