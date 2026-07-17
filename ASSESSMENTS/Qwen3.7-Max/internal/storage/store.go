package storage

import (
	"context"

	"github.com/notifications-service/internal/domain"
)

type Store interface {
	Save(ctx context.Context, notification *domain.Notification) error
	Get(ctx context.Context, id string) (*domain.Notification, error)
	List(ctx context.Context, filter domain.ListFilter) ([]*domain.Notification, int, error)
	UpdateStatus(ctx context.Context, id string, status domain.Status) error

	SaveRecipientStatus(ctx context.Context, rs *domain.RecipientNotification) error
	GetRecipientStatuses(ctx context.Context, notificationID string) ([]*domain.RecipientNotification, error)
}
