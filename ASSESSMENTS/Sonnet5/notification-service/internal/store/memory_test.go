package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/store"
)

func newNotification(id string, channel domain.Channel, status domain.Status, recipient string) *domain.Notification {
	return &domain.Notification{
		ID:        id,
		Channel:   channel,
		Recipient: recipient,
		Status:    status,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func TestMemoryRepository_CreateGet(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()

	n := newNotification("1", domain.ChannelEmail, domain.StatusPending, "a@example.com")
	require.NoError(t, repo.Create(ctx, n))

	got, err := repo.Get(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusPending, got.Status)

	// Mutating the returned pointer must not affect stored state.
	got.Status = domain.StatusFailed
	reread, err := repo.Get(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusPending, reread.Status)
}

func TestMemoryRepository_GetNotFound(t *testing.T) {
	repo := store.NewMemoryRepository()
	_, err := repo.Get(context.Background(), "missing")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestMemoryRepository_Update(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()
	n := newNotification("1", domain.ChannelEmail, domain.StatusPending, "a@example.com")
	require.NoError(t, repo.Create(ctx, n))

	n.Status = domain.StatusDelivered
	require.NoError(t, repo.Update(ctx, n))

	got, err := repo.Get(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusDelivered, got.Status)
}

func TestMemoryRepository_UpdateNotFound(t *testing.T) {
	repo := store.NewMemoryRepository()
	err := repo.Update(context.Background(), newNotification("missing", domain.ChannelEmail, domain.StatusPending, "a@example.com"))
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestMemoryRepository_ListFilterAndPagination(t *testing.T) {
	repo := store.NewMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, newNotification("1", domain.ChannelEmail, domain.StatusPending, "a@example.com")))
	require.NoError(t, repo.Create(ctx, newNotification("2", domain.ChannelSMS, domain.StatusFailed, "+14155552671")))
	require.NoError(t, repo.Create(ctx, newNotification("3", domain.ChannelEmail, domain.StatusDelivered, "b@example.com")))

	// Filter by channel.
	results, total, err := repo.List(ctx, store.ListFilter{Channel: domain.ChannelEmail})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, results, 2)

	// Filter by status.
	results, total, err = repo.List(ctx, store.ListFilter{Status: domain.StatusFailed})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, results, 1)
	assert.Equal(t, "2", results[0].ID)

	// Newest-first ordering.
	results, total, err = repo.List(ctx, store.ListFilter{})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	require.Len(t, results, 3)
	assert.Equal(t, []string{"3", "2", "1"}, []string{results[0].ID, results[1].ID, results[2].ID})

	// Pagination: limit+offset over the full (unfiltered) set.
	results, total, err = repo.List(ctx, store.ListFilter{Limit: 1, Offset: 1})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	require.Len(t, results, 1)
	assert.Equal(t, "2", results[0].ID)

	// Offset beyond the result set yields an empty page, not an error.
	results, _, err = repo.List(ctx, store.ListFilter{Offset: 100})
	require.NoError(t, err)
	assert.Empty(t, results)
}
