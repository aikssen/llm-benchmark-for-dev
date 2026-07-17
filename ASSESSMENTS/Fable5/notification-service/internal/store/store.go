// Package store defines the persistence boundary of the service and provides
// an in-memory implementation suitable for local runs and tests. A relational
// implementation (e.g. Postgres) can be swapped in behind the same interface.
package store

import (
	"context"
	"errors"

	"notificationservice/internal/domain"
)

// ErrNotFound is returned when a notification or delivery does not exist.
var ErrNotFound = errors.New("not found")

// ListFilter selects and paginates notifications. Zero values mean
// "no filter". Limit must be > 0.
type ListFilter struct {
	Status  domain.Status
	Channel domain.Channel
	Limit   int
	Offset  int
}

// ListResult is a page of notifications plus the total number of matches,
// so clients can paginate.
type ListResult struct {
	Notifications []domain.Notification
	Total         int
}

// Store persists notifications and their per-recipient deliveries.
//
// Implementations must be safe for concurrent use and must return/accept
// values that the caller can retain or mutate freely (i.e. no shared internal
// state leaks across the boundary).
type Store interface {
	// CreateNotification stores a new notification with its deliveries.
	CreateNotification(ctx context.Context, n domain.Notification) error

	// DeleteNotification removes a notification. Used to roll back a
	// submission that could not be enqueued.
	DeleteNotification(ctx context.Context, notificationID string) error

	// GetNotification returns a notification with its deliveries, or
	// ErrNotFound.
	GetNotification(ctx context.Context, notificationID string) (domain.Notification, error)

	// ListNotifications returns notifications matching the filter, newest
	// first.
	ListNotifications(ctx context.Context, f ListFilter) (ListResult, error)

	// UpdateDelivery applies fn to the delivery identified by
	// (notificationID, deliveryID) atomically and returns the updated copy.
	// Implementations refresh the delivery's UpdatedAt timestamp.
	UpdateDelivery(ctx context.Context, notificationID, deliveryID string, fn func(*domain.Delivery)) (domain.Delivery, error)
}
