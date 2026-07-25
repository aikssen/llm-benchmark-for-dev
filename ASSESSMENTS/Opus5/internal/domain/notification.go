package domain

import (
	"maps"
	"time"
)

// Status is the aggregate state of a notification request.
//
// It is *derived* from the states of the individual deliveries rather than
// stored alongside them. That removes a whole class of bugs where the two
// disagree, at the cost of computing it on read (see docs/DESIGN.md).
type Status string

const (
	StatusPending            Status = "pending"
	StatusProcessing         Status = "processing"
	StatusDelivered          Status = "delivered"
	StatusPartiallyDelivered Status = "partially_delivered"
	StatusFailed             Status = "failed"
)

// Notification is the aggregate root: one client request, fanned out into one
// Delivery per recipient.
type Notification struct {
	ID       string            `json:"id"`
	Channel  Channel           `json:"channel"`
	Message  Message           `json:"message"`
	Metadata map[string]string `json:"metadata,omitempty"`

	// IdempotencyKey, when set by the client, makes re-submission of the same
	// request return the original notification instead of sending twice.
	IdempotencyKey string `json:"-"`

	Deliveries []*Delivery `json:"deliveries"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Summary counts deliveries by outcome.
type Summary struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Sending   int `json:"sending"`
	Delivered int `json:"delivered"`
	Rejected  int `json:"rejected"`
	Failed    int `json:"failed"`
}

// Summary aggregates the delivery states.
func (n *Notification) Summary() Summary {
	s := Summary{Total: len(n.Deliveries)}
	for _, d := range n.Deliveries {
		switch d.Status {
		case DeliveryPending:
			s.Pending++
		case DeliverySending:
			s.Sending++
		case DeliveryDelivered:
			s.Delivered++
		case DeliveryRejected:
			s.Rejected++
		case DeliveryFailed:
			s.Failed++
		}
	}
	return s
}

// Status derives the aggregate status from the deliveries.
func (n *Notification) Status() Status {
	s := n.Summary()
	switch {
	case s.Total == 0:
		return StatusPending
	case s.Pending == s.Total:
		return StatusPending
	case s.Pending+s.Sending > 0:
		return StatusProcessing
	case s.Delivered == s.Total:
		return StatusDelivered
	case s.Delivered == 0:
		return StatusFailed
	default:
		return StatusPartiallyDelivered
	}
}

// Clone returns a deep copy of the notification and all of its deliveries.
func (n *Notification) Clone() *Notification {
	if n == nil {
		return nil
	}
	cp := *n
	cp.Metadata = maps.Clone(n.Metadata)
	cp.Deliveries = make([]*Delivery, len(n.Deliveries))
	for i, d := range n.Deliveries {
		cp.Deliveries[i] = d.Clone()
	}
	return &cp
}
