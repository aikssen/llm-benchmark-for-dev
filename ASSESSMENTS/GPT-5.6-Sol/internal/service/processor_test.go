package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"notification/internal/memory"
	"notification/internal/notification"
)

type fakeRegistry struct{ provider notification.Provider }

func (r fakeRegistry) Provider(notification.Channel) (notification.Provider, bool) {
	return r.provider, r.provider != nil
}

type fakeProvider struct {
	mu        sync.Mutex
	failures  int
	retryable bool
	calls     int
}

func (p *fakeProvider) Send(context.Context, notification.ProviderMessage) (notification.ProviderResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls <= p.failures {
		return notification.ProviderResult{}, notification.ProviderError{Err: errors.New("provider failed"), CanRetry: p.retryable}
	}
	return notification.ProviderResult{ID: "provider-1"}, nil
}

type fakeValidator struct {
	name  string
	err   error
	calls int
}

func (v *fakeValidator) Name() string { return v.name }
func (v *fakeValidator) Validate(context.Context, notification.Channel, string, notification.Message) error {
	v.calls++
	return v.err
}

func TestProcessorStopsValidationAtFirstFailure(t *testing.T) {
	t.Parallel()
	repository := memory.NewRepository()
	value := queuedNotification(t, repository)
	first := &fakeValidator{name: "first", err: notification.NewValidationError("blocked", "blocked")}
	second := &fakeValidator{name: "second"}
	provider := &fakeProvider{}
	processor := NewProcessor(repository, fakeRegistry{provider}, []notification.Validator{first, second}, testRetryPolicy())

	processor.Process(context.Background(), Job{NotificationID: value.ID, DeliveryIndex: 0})
	got, _ := repository.Get(context.Background(), value.ID)
	if got.Deliveries[0].Status != notification.DeliveryRejected {
		t.Fatalf("delivery status = %q", got.Deliveries[0].Status)
	}
	if second.calls != 0 || provider.calls != 0 {
		t.Fatalf("remaining work was not stopped: validator calls %d, provider calls %d", second.calls, provider.calls)
	}
}

func TestProcessorRetriesTransientProviderErrors(t *testing.T) {
	t.Parallel()
	repository := memory.NewRepository()
	value := queuedNotification(t, repository)
	provider := &fakeProvider{failures: 2, retryable: true}
	processor := NewProcessor(repository, fakeRegistry{provider}, nil, testRetryPolicy())

	processor.Process(context.Background(), Job{NotificationID: value.ID, DeliveryIndex: 0})
	got, _ := repository.Get(context.Background(), value.ID)
	delivery := got.Deliveries[0]
	if delivery.Status != notification.DeliveryDelivered || delivery.Attempts != 3 || provider.calls != 3 {
		t.Fatalf("delivery = %+v, provider calls = %d", delivery, provider.calls)
	}
}

func TestProcessorDoesNotRetryPermanentProviderError(t *testing.T) {
	t.Parallel()
	repository := memory.NewRepository()
	value := queuedNotification(t, repository)
	provider := &fakeProvider{failures: 3, retryable: false}
	processor := NewProcessor(repository, fakeRegistry{provider}, nil, testRetryPolicy())

	processor.Process(context.Background(), Job{NotificationID: value.ID, DeliveryIndex: 0})
	got, _ := repository.Get(context.Background(), value.ID)
	if got.Deliveries[0].Status != notification.DeliveryFailed || provider.calls != 1 {
		t.Fatalf("delivery = %+v, provider calls = %d", got.Deliveries[0], provider.calls)
	}
}

func queuedNotification(t *testing.T, repository notification.Repository) notification.Notification {
	t.Helper()
	now := time.Now().UTC()
	value := notification.Notification{
		ID: "n-1", Channel: notification.ChannelEmail,
		Message: notification.Message{Subject: "subject", Body: "body"},
		Status:  notification.StatusQueued, CreatedAt: now, UpdatedAt: now,
		Deliveries: []notification.Delivery{{Recipient: "user@example.com", Status: notification.DeliveryQueued, UpdatedAt: now}},
	}
	if _, _, err := repository.Create(context.Background(), value, "", ""); err != nil {
		t.Fatal(err)
	}
	return value
}

func testRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, BaseDelay: time.Nanosecond, MaxDelay: time.Nanosecond, Timeout: time.Second}
}
