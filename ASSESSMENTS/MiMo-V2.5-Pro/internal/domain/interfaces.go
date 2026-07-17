package domain

import "context"

// Repository defines the interface for notification storage.
type Repository interface {
	// Save persists a notification.
	Save(ctx context.Context, notification *Notification) error

	// GetByID retrieves a notification by its ID.
	GetByID(ctx context.Context, id string) (*Notification, error)

	// Update updates an existing notification.
	Update(ctx context.Context, notification *Notification) error

	// List retrieves notifications based on the given parameters.
	List(ctx context.Context, params ListNotificationsParams) ([]*Notification, int, error)
}

// Provider defines the interface for sending notifications through external services.
type Provider interface {
	// Send delivers a notification to a single recipient.
	Send(ctx context.Context, recipient Recipient, content Content) error

	// Channel returns the channel this provider handles.
	Channel() Channel

	// Name returns a human-readable name for the provider.
	Name() string
}

// Validator defines the interface for notification validation steps.
type Validator interface {
	// Validate performs a validation step on a notification request.
	// Returns nil if validation passes, or an error describing the failure.
	Validate(ctx context.Context, req *CreateNotificationRequest) error

	// Name returns a human-readable name for the validator.
	Name() string
}
