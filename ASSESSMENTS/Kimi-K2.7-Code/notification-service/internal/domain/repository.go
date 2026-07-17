package domain

import "context"

// NotificationRepository defines the persistence contract for notifications.
// The interface is intentionally small to allow swapping implementations.
type NotificationRepository interface {
	// Save persists the notification. If the ID already exists it is overwritten.
	Save(ctx context.Context, n Notification) error

	// GetByID retrieves a notification by its unique identifier.
	GetByID(ctx context.Context, id string) (Notification, error)

	// List returns notifications ordered by creation time descending.
	List(ctx context.Context, limit, offset int) ([]Notification, error)
}
