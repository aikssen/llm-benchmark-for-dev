package domain

import "time"

// DeliveryStatus is the state of a single recipient's delivery attempt chain.
type DeliveryStatus string

const (
	// DeliveryPending: accepted and queued, not picked up by a worker yet.
	DeliveryPending DeliveryStatus = "pending"
	// DeliverySending: a worker is validating or calling the provider.
	DeliverySending DeliveryStatus = "sending"
	// DeliveryDelivered: the provider accepted the message.
	DeliveryDelivered DeliveryStatus = "delivered"
	// DeliveryRejected: a validation rule refused the delivery. Terminal and
	// never retried — retrying cannot change the outcome.
	DeliveryRejected DeliveryStatus = "rejected"
	// DeliveryFailed: the provider could not be reached or refused the
	// message, and the retry budget was exhausted.
	DeliveryFailed DeliveryStatus = "failed"
)

// Terminal reports whether the delivery has reached a final state.
func (s DeliveryStatus) Terminal() bool {
	switch s {
	case DeliveryDelivered, DeliveryRejected, DeliveryFailed:
		return true
	default:
		return false
	}
}

// Delivery tracks the fan-out of one notification to one recipient. It is the
// unit of work processed by the worker pool, so that a request with many
// recipients does not monopolise a single worker.
type Delivery struct {
	ID             string         `json:"id"`
	NotificationID string         `json:"notification_id"`
	Recipient      Recipient      `json:"recipient"`
	Status         DeliveryStatus `json:"status"`

	// Attempts counts provider calls made for this delivery. It stays at 0
	// when the delivery is rejected during validation.
	Attempts int `json:"attempts"`

	// Provider and ProviderMessageID are populated once a provider accepts
	// the message, and are what an operator needs to trace a delivery in the
	// external system.
	Provider          string `json:"provider,omitempty"`
	ProviderMessageID string `json:"provider_message_id,omitempty"`

	// FailureCode is a stable, machine-readable reason. FailureReason is the
	// human-readable detail. Both are empty unless the delivery failed.
	FailureCode   string `json:"failure_code,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`

	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Clone returns a deep copy. The store hands out clones so that callers can
// never mutate persisted state by accident.
func (d *Delivery) Clone() *Delivery {
	if d == nil {
		return nil
	}
	cp := *d
	if d.CompletedAt != nil {
		t := *d.CompletedAt
		cp.CompletedAt = &t
	}
	return &cp
}
