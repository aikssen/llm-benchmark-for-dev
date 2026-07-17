package delivery_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aikssen/notification-service/internal/delivery"
	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/provider"
	"github.com/aikssen/notification-service/internal/store"
)

func TestWorkerPool_EnqueueFailsWhenQueueIsFull(t *testing.T) {
	fp := &fakeProvider{channel: domain.ChannelEmail}
	registry := provider.NewRegistry(fp)
	repo := store.NewMemoryRepository()
	d := delivery.NewDispatcher(registry, repo, delivery.Options{
		MaxAttempts: 1, Policy: delivery.BackoffPolicy{}, ProviderTimeout: time.Second,
	}, testLogger())

	// Pool is never Started, so nothing drains the queue: capacity 1 means
	// exactly one Enqueue can succeed before ErrQueueFull kicks in.
	pool := delivery.NewWorkerPool(1, 1, d, testLogger())

	require.NoError(t, pool.Enqueue("a"))
	err := pool.Enqueue("b")
	assert.ErrorIs(t, err, domain.ErrQueueFull)
}

func TestWorkerPool_ProcessesEnqueuedJobs(t *testing.T) {
	fp := &fakeProvider{channel: domain.ChannelEmail}
	registry := provider.NewRegistry(fp)
	repo := store.NewMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, newTestNotification("n1", domain.ChannelEmail)))

	d := delivery.NewDispatcher(registry, repo, delivery.Options{
		MaxAttempts: 1, Policy: delivery.BackoffPolicy{}, ProviderTimeout: time.Second,
	}, testLogger())

	pool := delivery.NewWorkerPool(2, 10, d, testLogger())
	poolCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	pool.Start(poolCtx)

	require.NoError(t, pool.Enqueue("n1"))

	require.Eventually(t, func() bool {
		got, err := repo.Get(ctx, "n1")
		return err == nil && got.Status == domain.StatusDelivered
	}, time.Second, 5*time.Millisecond, "notification should be delivered by a worker")

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, time.Second)
	defer shutdownCancel()
	require.NoError(t, pool.Shutdown(shutdownCtx))
}
