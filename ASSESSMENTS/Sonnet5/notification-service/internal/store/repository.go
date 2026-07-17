// Package store defines the persistence boundary for notifications.
//
// The only implementation provided is an in-memory store, which keeps the
// assessment runnable with zero external dependencies. Because everything
// goes through the Repository interface, swapping in Postgres/DynamoDB/etc.
// later only requires a new implementation of this interface — no caller
// changes. See README "Future improvements" for what that migration would
// need (durability, real cursor pagination, transactional batch writes).
package store

import (
	"context"

	"github.com/aikssen/notification-service/internal/domain"
)

// ListFilter narrows down List results. Zero values mean "no filter".
type ListFilter struct {
	BatchID   string
	Status    domain.Status
	Channel   domain.Channel
	Recipient string
	Limit     int
	Offset    int
}

// Repository persists and queries notifications.
type Repository interface {
	// Create inserts a brand new notification. ID must be pre-populated.
	Create(ctx context.Context, n *domain.Notification) error
	// Update persists changes to an existing notification (status, attempts, etc).
	Update(ctx context.Context, n *domain.Notification) error
	// Get returns a single notification by ID, or domain.ErrNotFound.
	Get(ctx context.Context, id string) (*domain.Notification, error)
	// List returns notifications matching filter, ordered by CreatedAt desc,
	// plus the total count of matching records (ignoring Limit/Offset) so
	// callers can build pagination metadata.
	List(ctx context.Context, filter ListFilter) ([]*domain.Notification, int, error)
}
