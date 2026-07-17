package application

import (
	"context"
	"errors"
	"fmt"

	"notification-service/internal/domain"
)

// GetStatus retrieves the current state of a notification.
func (s *Service) GetStatus(ctx context.Context, id string) (domain.Notification, error) {
	if id == "" {
		return domain.Notification{}, domain.ErrInvalidRequest
	}

	n, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Notification{}, domain.ErrNotFound
		}
		return domain.Notification{}, fmt.Errorf("failed to get notification: %w", err)
	}
	return n, nil
}
