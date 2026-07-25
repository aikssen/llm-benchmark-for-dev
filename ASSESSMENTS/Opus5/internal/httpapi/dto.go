package httpapi

import (
	"time"

	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/service"
	"github.com/evercifuentes/notifier/internal/store"
)

// The wire types are kept separate from the domain model on purpose: the API
// is a contract with clients and must be able to stay stable while the
// internal model evolves, and vice versa.

type submitRequest struct {
	Channel    string            `json:"channel"`
	Recipients []recipientDTO    `json:"recipients"`
	Message    messageDTO        `json:"message"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type recipientDTO struct {
	ID      string `json:"id,omitempty"`
	Address string `json:"address"`
}

type messageDTO struct {
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body"`
}

// toCommand maps the request onto the application-layer command. It only
// shapes data; every rule is enforced by the service.
func (r submitRequest) toCommand(idempotencyKey string) service.SubmitCommand {
	recipients := make([]domain.Recipient, len(r.Recipients))
	for i, rec := range r.Recipients {
		recipients[i] = domain.Recipient{ID: rec.ID, Address: rec.Address}
	}
	return service.SubmitCommand{
		Channel:        domain.ParseChannel(r.Channel),
		Recipients:     recipients,
		Message:        domain.Message{Subject: r.Message.Subject, Body: r.Message.Body},
		Metadata:       r.Metadata,
		IdempotencyKey: idempotencyKey,
	}
}

type notificationResponse struct {
	ID         string            `json:"id"`
	Channel    string            `json:"channel"`
	Status     string            `json:"status"`
	Message    messageDTO        `json:"message"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Summary    summaryDTO        `json:"summary"`
	Deliveries []deliveryDTO     `json:"deliveries"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

type summaryDTO struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Sending   int `json:"sending"`
	Delivered int `json:"delivered"`
	Rejected  int `json:"rejected"`
	Failed    int `json:"failed"`
}

type deliveryDTO struct {
	ID                string       `json:"id"`
	Recipient         recipientDTO `json:"recipient"`
	Status            string       `json:"status"`
	Attempts          int          `json:"attempts"`
	Provider          string       `json:"provider,omitempty"`
	ProviderMessageID string       `json:"provider_message_id,omitempty"`
	FailureCode       string       `json:"failure_code,omitempty"`
	FailureReason     string       `json:"failure_reason,omitempty"`
	CreatedAt         time.Time    `json:"created_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
	CompletedAt       *time.Time   `json:"completed_at,omitempty"`
}

type listResponse struct {
	Items      []notificationResponse `json:"items"`
	Pagination paginationDTO          `json:"pagination"`
}

type paginationDTO struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func toNotificationResponse(n *domain.Notification) notificationResponse {
	summary := n.Summary()
	deliveries := make([]deliveryDTO, len(n.Deliveries))
	for i, d := range n.Deliveries {
		deliveries[i] = deliveryDTO{
			ID:                d.ID,
			Recipient:         recipientDTO{ID: d.Recipient.ID, Address: d.Recipient.Address},
			Status:            string(d.Status),
			Attempts:          d.Attempts,
			Provider:          d.Provider,
			ProviderMessageID: d.ProviderMessageID,
			FailureCode:       d.FailureCode,
			FailureReason:     d.FailureReason,
			CreatedAt:         d.CreatedAt,
			UpdatedAt:         d.UpdatedAt,
			CompletedAt:       d.CompletedAt,
		}
	}
	return notificationResponse{
		ID:       n.ID,
		Channel:  n.Channel.String(),
		Status:   string(n.Status()),
		Message:  messageDTO{Subject: n.Message.Subject, Body: n.Message.Body},
		Metadata: n.Metadata,
		Summary: summaryDTO{
			Total:     summary.Total,
			Pending:   summary.Pending,
			Sending:   summary.Sending,
			Delivered: summary.Delivered,
			Rejected:  summary.Rejected,
			Failed:    summary.Failed,
		},
		Deliveries: deliveries,
		CreatedAt:  n.CreatedAt,
		UpdatedAt:  n.UpdatedAt,
	}
}

func toListResponse(page store.Page) listResponse {
	items := make([]notificationResponse, len(page.Items))
	for i, n := range page.Items {
		items[i] = toNotificationResponse(n)
	}
	return listResponse{
		Items: items,
		Pagination: paginationDTO{
			Total:  page.Total,
			Limit:  page.Limit,
			Offset: page.Offset,
		},
	}
}
