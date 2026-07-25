// Package store defines persistence for notifications.
//
// The interface is written so it can be satisfied by a SQL database without
// changing callers: mutations are expressed as targeted operations rather
// than "read the aggregate, change it, write it back", which would be a lost
// update waiting to happen once more than one process is running.
package store

import (
	"context"

	"github.com/evercifuentes/notifier/internal/domain"
)

// Filter selects and pages through notifications.
type Filter struct {
	// Channel, when set, restricts results to one channel.
	Channel domain.Channel
	// Status, when set, restricts results to one aggregate status.
	Status domain.Status
	// Limit is the maximum number of items to return.
	Limit int
	// Offset is the number of items to skip.
	Offset int
}

// Page is a slice of results plus the information a client needs to page.
type Page struct {
	Items  []*domain.Notification
	Total  int
	Limit  int
	Offset int
}

// Store persists notifications and their deliveries.
//
// Implementations must be safe for concurrent use and must return deep copies
// so callers cannot mutate stored state by holding on to a pointer.
type Store interface {
	// Create stores a new notification. It returns an error if the ID or the
	// idempotency key is already taken.
	Create(ctx context.Context, n *domain.Notification) error

	// Get returns a notification, or domain.ErrNotFound.
	Get(ctx context.Context, id string) (*domain.Notification, error)

	// List returns notifications newest first.
	List(ctx context.Context, f Filter) (Page, error)

	// UpdateDelivery applies mutate to a single delivery atomically. Scoping
	// the mutation to one delivery is what allows many workers to advance
	// different recipients of the same notification concurrently.
	UpdateDelivery(ctx context.Context, notificationID, deliveryID string, mutate func(*domain.Delivery)) error

	// FindByIdempotencyKey returns the notification created under key, or
	// domain.ErrNotFound.
	FindByIdempotencyKey(ctx context.Context, key string) (*domain.Notification, error)
}
