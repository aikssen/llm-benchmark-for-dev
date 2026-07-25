package domain_test

import (
	"testing"
	"time"

	"github.com/evercifuentes/notifier/internal/domain"
)

// deliveries builds a notification whose deliveries have the given statuses.
func deliveries(statuses ...domain.DeliveryStatus) *domain.Notification {
	n := &domain.Notification{ID: "ntf_1", Channel: domain.ChannelEmail}
	for i, s := range statuses {
		n.Deliveries = append(n.Deliveries, &domain.Delivery{
			ID:             string(rune('a'+i)) + "_dlv",
			NotificationID: n.ID,
			Status:         s,
		})
	}
	return n
}

func TestNotificationStatusIsDerivedFromDeliveries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		statuses []domain.DeliveryStatus
		want     domain.Status
	}{
		{"no deliveries yet", nil, domain.StatusPending},
		{"all queued", []domain.DeliveryStatus{domain.DeliveryPending, domain.DeliveryPending}, domain.StatusPending},
		{"one in flight", []domain.DeliveryStatus{domain.DeliverySending, domain.DeliveryPending}, domain.StatusProcessing},
		{"partially finished", []domain.DeliveryStatus{domain.DeliveryDelivered, domain.DeliveryPending}, domain.StatusProcessing},
		{"all delivered", []domain.DeliveryStatus{domain.DeliveryDelivered, domain.DeliveryDelivered}, domain.StatusDelivered},
		{"all failed", []domain.DeliveryStatus{domain.DeliveryFailed, domain.DeliveryFailed}, domain.StatusFailed},
		{"all rejected counts as failed", []domain.DeliveryStatus{domain.DeliveryRejected}, domain.StatusFailed},
		{"rejected and failed mix", []domain.DeliveryStatus{domain.DeliveryRejected, domain.DeliveryFailed}, domain.StatusFailed},
		{"some delivered some not", []domain.DeliveryStatus{domain.DeliveryDelivered, domain.DeliveryFailed}, domain.StatusPartiallyDelivered},
		{"delivered and rejected", []domain.DeliveryStatus{domain.DeliveryDelivered, domain.DeliveryRejected}, domain.StatusPartiallyDelivered},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := deliveries(tt.statuses...).Status(); got != tt.want {
				t.Errorf("Status() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNotificationSummaryCountsEveryState(t *testing.T) {
	t.Parallel()

	n := deliveries(
		domain.DeliveryPending,
		domain.DeliverySending,
		domain.DeliveryDelivered,
		domain.DeliveryDelivered,
		domain.DeliveryRejected,
		domain.DeliveryFailed,
	)

	want := domain.Summary{Total: 6, Pending: 1, Sending: 1, Delivered: 2, Rejected: 1, Failed: 1}
	if got := n.Summary(); got != want {
		t.Errorf("Summary() = %+v, want %+v", got, want)
	}
}

func TestDeliveryStatusTerminal(t *testing.T) {
	t.Parallel()

	terminal := []domain.DeliveryStatus{domain.DeliveryDelivered, domain.DeliveryRejected, domain.DeliveryFailed}
	for _, s := range terminal {
		if !s.Terminal() {
			t.Errorf("%q should be terminal", s)
		}
	}
	for _, s := range []domain.DeliveryStatus{domain.DeliveryPending, domain.DeliverySending} {
		if s.Terminal() {
			t.Errorf("%q should not be terminal", s)
		}
	}
}

// Clone must be deep: the store hands clones to callers, and a shallow copy
// would let a caller mutate persisted state through the returned pointer.
func TestCloneIsDeep(t *testing.T) {
	t.Parallel()

	completed := time.Now()
	original := &domain.Notification{
		ID:         "ntf_1",
		Channel:    domain.ChannelSMS,
		Metadata:   map[string]string{"campaign": "welcome"},
		Deliveries: []*domain.Delivery{{ID: "dlv_1", Status: domain.DeliveryDelivered, CompletedAt: &completed}},
	}

	clone := original.Clone()
	clone.Metadata["campaign"] = "tampered"
	clone.Deliveries[0].Status = domain.DeliveryFailed
	*clone.Deliveries[0].CompletedAt = time.Time{}

	if original.Metadata["campaign"] != "welcome" {
		t.Error("mutating the clone's metadata changed the original")
	}
	if original.Deliveries[0].Status != domain.DeliveryDelivered {
		t.Error("mutating the clone's delivery changed the original")
	}
	if original.Deliveries[0].CompletedAt.IsZero() {
		t.Error("mutating the clone's CompletedAt changed the original")
	}
}

func TestRecipientKeyPrefersID(t *testing.T) {
	t.Parallel()

	if got := (domain.Recipient{ID: "u1", Address: "a@example.com"}).Key(); got != "u1" {
		t.Errorf("Key() = %q, want the user id", got)
	}
	if got := (domain.Recipient{Address: "a@example.com"}).Key(); got != "a@example.com" {
		t.Errorf("Key() = %q, want the address as fallback", got)
	}
}

func TestParseChannelNormalises(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"EMAIL", " email ", "Email"} {
		if got := domain.ParseChannel(in); got != domain.ChannelEmail {
			t.Errorf("ParseChannel(%q) = %q, want %q", in, got, domain.ChannelEmail)
		}
	}
}
