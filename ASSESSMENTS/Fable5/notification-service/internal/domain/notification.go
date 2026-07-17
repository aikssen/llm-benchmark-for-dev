// Package domain contains the core business entities of the notification
// service. It has no dependencies on other layers.
package domain

import "time"

// Channel identifies the delivery mechanism for a notification.
type Channel string

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
	ChannelPush  Channel = "push"
)

// Valid reports whether the channel is one of the supported channels.
func (c Channel) Valid() bool {
	switch c {
	case ChannelEmail, ChannelSMS, ChannelPush:
		return true
	}
	return false
}

// Channels lists all supported channels.
func Channels() []Channel {
	return []Channel{ChannelEmail, ChannelSMS, ChannelPush}
}

// Status is the aggregate state of a notification, derived from the state of
// its per-recipient deliveries.
type Status string

const (
	StatusPending            Status = "pending"
	StatusProcessing         Status = "processing"
	StatusDelivered          Status = "delivered"
	StatusPartiallyDelivered Status = "partially_delivered"
	StatusFailed             Status = "failed"
)

// DeliveryStatus is the state of a single per-recipient delivery.
type DeliveryStatus string

const (
	DeliveryPending    DeliveryStatus = "pending"
	DeliveryProcessing DeliveryStatus = "processing"
	DeliveryDelivered  DeliveryStatus = "delivered"
	DeliveryFailed     DeliveryStatus = "failed"
)

// Message is the content of a notification. Subject is only meaningful for
// channels that support it (email).
type Message struct {
	Subject string
	Body    string
}

// Notification is a request to deliver a message to one or more recipients
// over a single channel. Each recipient gets its own Delivery, which is the
// unit of processing, retrying and status tracking.
type Notification struct {
	ID         string
	Channel    Channel
	Message    Message
	CreatedAt  time.Time
	Deliveries []Delivery
}

// Status derives the aggregate notification status from its deliveries.
func (n Notification) Status() Status {
	return AggregateStatus(n.Deliveries)
}

// Delivery tracks the outcome of sending a notification to one recipient.
type Delivery struct {
	ID             string
	NotificationID string
	Recipient      string
	Status         DeliveryStatus
	// Attempts is the number of provider send attempts performed so far.
	// It stays 0 when the delivery fails validation before reaching a provider.
	Attempts  int
	LastError string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AggregateStatus reduces per-delivery states into a single notification
// status:
//
//   - all pending                          -> pending
//   - all delivered                        -> delivered
//   - all failed                           -> failed
//   - all terminal, mixed outcome          -> partially_delivered
//   - anything else (work still going on)  -> processing
func AggregateStatus(deliveries []Delivery) Status {
	if len(deliveries) == 0 {
		return StatusPending
	}
	var pending, delivered, failed int
	for _, d := range deliveries {
		switch d.Status {
		case DeliveryPending:
			pending++
		case DeliveryDelivered:
			delivered++
		case DeliveryFailed:
			failed++
		}
	}
	total := len(deliveries)
	switch {
	case pending == total:
		return StatusPending
	case delivered == total:
		return StatusDelivered
	case failed == total:
		return StatusFailed
	case delivered+failed == total:
		return StatusPartiallyDelivered
	default:
		return StatusProcessing
	}
}
