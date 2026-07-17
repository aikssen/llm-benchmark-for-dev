// Package store defines the persistence boundary for notifications. The service
// depends only on the Store interface, so the in-memory implementation used for
// local development and tests can be swapped for a database-backed one without
// changing any business logic.
package store

import (
	"context"
	"errors"

	"github.com/aikssen/notification-service/internal/domain"
)

// errAlreadyExists is returned by Create when an ID collision occurs. It is an
// internal invariant violation (IDs are generated server-side) rather than a
// client error, so it is not part of the domain sentinel set.
var errAlreadyExists = errors.New("notification already exists")

// ListFilter narrows a List query. Zero-valued fields are ignored, so an empty
// filter returns everything (subject to pagination).
type ListFilter struct {
	Channel   domain.Channel // optional exact match
	Status    domain.Status  // optional exact match
	Recipient string         // optional exact match
	Limit     int            // max items to return; <=0 means "use default"
	Offset    int            // number of items to skip
}

// Store persists and retrieves notifications. Implementations must be safe for
// concurrent use: the worker pool, the API layer and background updates all
// touch it simultaneously.
type Store interface {
	// Create stores a new notification. It returns an error if the ID already
	// exists.
	Create(ctx context.Context, n *domain.Notification) error

	// Get returns the notification with the given ID or domain.ErrNotFound.
	Get(ctx context.Context, id string) (*domain.Notification, error)

	// Update replaces the stored notification. It returns domain.ErrNotFound if
	// the notification does not exist.
	Update(ctx context.Context, n *domain.Notification) error

	// List returns notifications matching the filter, newest first, along with
	// the total count of matches before pagination.
	List(ctx context.Context, f ListFilter) (items []*domain.Notification, total int, err error)
}
