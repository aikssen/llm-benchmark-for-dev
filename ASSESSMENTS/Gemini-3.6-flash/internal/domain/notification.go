package domain

import (
	"time"
)

// NotificationRequest represents the client-facing notification submission request and aggregate state.
type NotificationRequest struct {
	ID              string                         `json:"id"`
	Channel         Channel                        `json:"channel"`
	Recipients      []string                       `json:"recipients"`
	Subject         string                         `json:"subject,omitempty"`
	Body            string                         `json:"body"`
	Metadata        map[string]string              `json:"metadata,omitempty"`
	Status          Status                         `json:"status"`
	TotalRecipients int                            `json:"total_recipients"`
	DeliveredCount  int                            `json:"delivered_count"`
	FailedCount     int                            `json:"failed_count"`
	CreatedAt       time.Time                      `json:"created_at"`
	UpdatedAt       time.Time                      `json:"updated_at"`
	RecipientDetail []NotificationRecipientStatus  `json:"recipient_details,omitempty"`
}

// NotificationRecipientStatus tracks individual delivery status for each recipient in a request.
type NotificationRecipientStatus struct {
	ID           string    `json:"id"`
	RequestID    string    `json:"request_id"`
	Recipient    string    `json:"recipient"`
	Channel      Channel   `json:"channel"`
	Status       Status    `json:"status"`
	Error        string    `json:"error,omitempty"`
	ProviderUsed string    `json:"provider_used,omitempty"`
	Attempts     int       `json:"attempts"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// SingleRecipientItem is the unit of work dispatched through the validation pipeline and worker pool.
type SingleRecipientItem struct {
	ID        string
	RequestID string
	Channel   Channel
	Recipient string
	Subject   string
	Body      string
	Metadata  map[string]string
	CreatedAt time.Time
}

// NotificationFilter represents parameters for querying notifications.
type NotificationFilter struct {
	Channel Channel
	Status  Status
	Limit   int
	Offset  int
}

// ListResult holds paginated results.
type ListResult struct {
	Notifications []*NotificationRequest `json:"notifications"`
	Total         int                    `json:"total"`
	Limit         int                    `json:"limit"`
	Offset        int                    `json:"offset"`
}
