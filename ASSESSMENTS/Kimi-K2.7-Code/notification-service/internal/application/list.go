package application

import (
	"context"
	"fmt"

	"notification-service/internal/domain"
)

// ListRequest contains pagination parameters.
type ListRequest struct {
	Limit  int
	Offset int
}

// ListResponse returns a page of notifications.
type ListResponse struct {
	Notifications []domain.Notification `json:"notifications"`
	Total         int                   `json:"total"`
}

// List returns previously submitted notifications ordered by creation time descending.
func (s *Service) List(ctx context.Context, req ListRequest) (ListResponse, error) {
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	notifications, err := s.repo.List(ctx, req.Limit, req.Offset)
	if err != nil {
		return ListResponse{}, fmt.Errorf("failed to list notifications: %w", err)
	}

	return ListResponse{
		Notifications: notifications,
		Total:         len(notifications),
	}, nil
}
