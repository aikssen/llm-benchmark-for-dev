package store

import (
	"context"
	"github.com/assessment/notify-service/internal/domain"
)

type Store interface {
	Save(ctx context.Context, n *domain.Notification) error
	Get(ctx context.Context, id string) (*domain.Notification, error)
	List(ctx context.Context, offset, limit int) ([]*domain.Notification, int, error)
}
