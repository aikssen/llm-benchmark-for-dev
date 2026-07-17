package application

import (
	"context"
	"fmt"

	"notification-service/internal/domain"
)

// SubmitRequest represents a client request to send a notification.
type SubmitRequest struct {
	Channel    string   `json:"channel"`
	Recipients []string `json:"recipients"`
	Subject    string   `json:"subject,omitempty"`
	Body       string   `json:"body"`
}

// SubmitResponse returns the accepted notification ID.
type SubmitResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Submit validates the request, persists the notification and enqueues it for background processing.
func (s *Service) Submit(ctx context.Context, req SubmitRequest) (SubmitResponse, error) {
	channel, err := domain.ParseChannel(req.Channel)
	if err != nil {
		return SubmitResponse{}, err
	}

	if len(req.Recipients) == 0 {
		return SubmitResponse{}, domain.ErrInvalidRecipient
	}

	recipients := make([]domain.Recipient, len(req.Recipients))
	for i, addr := range req.Recipients {
		recipients[i] = domain.Recipient{Address: addr}
	}

	content := domain.Content{Subject: req.Subject, Body: req.Body}
	notification := domain.NewNotification(channel, recipients, content)

	// Persist immediately so clients can query status even before processing.
	if err := s.repo.Save(ctx, notification); err != nil {
		return SubmitResponse{}, fmt.Errorf("failed to save notification: %w", err)
	}

	// Enqueue for asynchronous validation and delivery.
	s.pool.Enqueue(notification.ID)

	return SubmitResponse{
		ID:     notification.ID,
		Status: string(notification.Status),
	}, nil
}
