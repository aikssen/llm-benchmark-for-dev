package memory_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"notification-service/internal/domain"
	"notification-service/internal/infrastructure/memory"
)

func TestRepository_SaveAndGet(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()

	n := domain.NewNotification(domain.ChannelEmail, []domain.Recipient{{Address: "a@b.com"}}, domain.Content{Body: "hi"})
	err := repo.Save(ctx, n)
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, n.ID)
	require.NoError(t, err)
	assert.Equal(t, n.ID, got.ID)
	assert.Equal(t, domain.StatusPending, got.Status)
}

func TestRepository_GetByID_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()

	_, err := repo.GetByID(ctx, "missing")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestRepository_List_OrderAndPagination(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewNotificationRepository()

	for i := 0; i < 5; i++ {
		n := domain.NewNotification(domain.ChannelSMS, []domain.Recipient{{Address: "+123"}}, domain.Content{Body: "msg"})
		err := repo.Save(ctx, n)
		require.NoError(t, err)
	}

	list, err := repo.List(ctx, 2, 0)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.True(t, list[0].CreatedAt.After(list[1].CreatedAt))
}
