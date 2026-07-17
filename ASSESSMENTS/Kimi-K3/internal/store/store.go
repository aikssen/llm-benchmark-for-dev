// Package store defines the persistence contract of the service and an
// in-memory implementation. The contract is deliberately small so it can
// be backed by a real database later without touching the rest of the
// codebase.
package store

import (
	"context"
	"errors"

	"notifications/internal/domain"
)

// ErrNotFound is returned when a notification ID does not exist.
var ErrNotFound = errors.New("notification not found")

// ListFilter narrows down List results.
type ListFilter struct {
	Status    domain.Status  // optional
	Channel   domain.Channel // optional
	RequestID string         // optional
	Limit     int            // max items to return; <= 0 means default
	Offset    int            // items to skip (insertion order)
}

// Store is the persistence contract for notifications.
type Store interface {
	// Upsert inserts a new notification or replaces an existing one.
	Upsert(ctx context.Context, n *domain.Notification) error
	// Get returns a copy of the notification with the given ID.
	Get(ctx context.Context, id string) (*domain.Notification, error)
	// List returns copies of matching notifications in insertion order.
	List(ctx context.Context, f ListFilter) ([]*domain.Notification, error)
}
