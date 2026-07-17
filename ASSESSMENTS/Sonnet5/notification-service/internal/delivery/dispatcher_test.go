package delivery_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/sony/gobreaker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aikssen/notification-service/internal/delivery"
	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/provider"
	"github.com/aikssen/notification-service/internal/store"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeProvider fails its first failTimes calls then succeeds, unless
// alwaysFail is set, in which case it never succeeds.
type fakeProvider struct {
	channel    domain.Channel
	failTimes  int
	alwaysFail bool

	mu    sync.Mutex
	calls int
}

func (p *fakeProvider) Channel() domain.Channel { return p.channel }

func (p *fakeProvider) Send(_ context.Context, _ domain.Notification) error {
	p.mu.Lock()
	p.calls++
	calls := p.calls
	p.mu.Unlock()

	if p.alwaysFail || calls <= p.failTimes {
		return errors.New("fake provider: simulated failure")
	}
	return nil
}

func (p *fakeProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func newTestNotification(id string, channel domain.Channel) *domain.Notification {
	now := time.Now()
	return &domain.Notification{
		ID: id, Channel: channel, Recipient: "recipient", Status: domain.StatusPending,
		CreatedAt: now, UpdatedAt: now,
	}
}

func TestDispatcher_DeliverSucceedsAfterRetries(t *testing.T) {
	fp := &fakeProvider{channel: domain.ChannelEmail, failTimes: 2}
	registry := provider.NewRegistry(fp)
	repo := store.NewMemoryRepository()
	ctx := context.Background()

	n := newTestNotification("n1", domain.ChannelEmail)
	require.NoError(t, repo.Create(ctx, n))

	d := delivery.NewDispatcher(registry, repo, delivery.Options{
		MaxAttempts:             4,
		Policy:                  delivery.BackoffPolicy{Base: time.Millisecond, Max: 5 * time.Millisecond},
		ProviderTimeout:         time.Second,
		BreakerFailureThreshold: 100, // effectively disabled for this test
		BreakerOpenDuration:     time.Second,
	}, testLogger())

	require.NoError(t, d.Deliver(ctx, "n1"))

	got, err := repo.Get(ctx, "n1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusDelivered, got.Status)
	assert.Equal(t, 3, got.Attempts) // 2 failures + 1 success
}

func TestDispatcher_DeliverExhaustsAttempts(t *testing.T) {
	fp := &fakeProvider{channel: domain.ChannelEmail, alwaysFail: true}
	registry := provider.NewRegistry(fp)
	repo := store.NewMemoryRepository()
	ctx := context.Background()

	n := newTestNotification("n1", domain.ChannelEmail)
	require.NoError(t, repo.Create(ctx, n))

	d := delivery.NewDispatcher(registry, repo, delivery.Options{
		MaxAttempts:             3,
		Policy:                  delivery.BackoffPolicy{Base: time.Millisecond, Max: 5 * time.Millisecond},
		ProviderTimeout:         time.Second,
		BreakerFailureThreshold: 100,
		BreakerOpenDuration:     time.Second,
	}, testLogger())

	err := d.Deliver(ctx, "n1")
	require.Error(t, err)

	got, getErr := repo.Get(ctx, "n1")
	require.NoError(t, getErr)
	assert.Equal(t, domain.StatusFailed, got.Status)
	assert.Equal(t, 3, got.Attempts)
	assert.NotEmpty(t, got.LastError)
}

func TestDispatcher_NoProviderRegisteredFailsImmediately(t *testing.T) {
	registry := provider.NewRegistry() // nothing registered
	repo := store.NewMemoryRepository()
	ctx := context.Background()

	n := newTestNotification("n1", domain.ChannelPush)
	require.NoError(t, repo.Create(ctx, n))

	d := delivery.NewDispatcher(registry, repo, delivery.Options{
		MaxAttempts:     3,
		Policy:          delivery.BackoffPolicy{Base: time.Millisecond, Max: time.Millisecond},
		ProviderTimeout: time.Second,
	}, testLogger())

	require.NoError(t, d.Deliver(ctx, "n1"))

	got, err := repo.Get(ctx, "n1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusFailed, got.Status)
	assert.Equal(t, 0, got.Attempts, "must not attempt delivery when no provider is registered")
}

func TestDispatcher_CircuitBreakerOpensAndSkipsProvider(t *testing.T) {
	fp := &fakeProvider{channel: domain.ChannelEmail, alwaysFail: true}
	registry := provider.NewRegistry(fp)
	repo := store.NewMemoryRepository()
	ctx := context.Background()

	d := delivery.NewDispatcher(registry, repo, delivery.Options{
		MaxAttempts:             1, // one attempt per notification, to control call count precisely
		Policy:                  delivery.BackoffPolicy{Base: time.Millisecond, Max: time.Millisecond},
		ProviderTimeout:         time.Second,
		BreakerFailureThreshold: 2,
		BreakerOpenDuration:     time.Minute,
	}, testLogger())

	for i := 0; i < 2; i++ {
		id := "n" + string(rune('0'+i))
		require.NoError(t, repo.Create(ctx, newTestNotification(id, domain.ChannelEmail)))
		_ = d.Deliver(ctx, id)
	}
	assert.Equal(t, 2, fp.callCount(), "provider should have been called for the first two (breaker still closed)")

	// Third notification: breaker should now be open and skip the provider entirely.
	require.NoError(t, repo.Create(ctx, newTestNotification("n2", domain.ChannelEmail)))
	err := d.Deliver(ctx, "n2")
	require.Error(t, err)
	assert.Equal(t, 2, fp.callCount(), "provider must not be called while the circuit breaker is open")

	got, getErr := repo.Get(ctx, "n2")
	require.NoError(t, getErr)
	assert.Equal(t, domain.StatusFailed, got.Status)
	assert.Contains(t, got.LastError, gobreaker.ErrOpenState.Error())
}
