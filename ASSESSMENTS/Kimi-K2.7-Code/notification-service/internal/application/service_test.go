package application_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"notification-service/internal/application"
	"notification-service/internal/domain"
	"notification-service/internal/infrastructure/memory"
	"notification-service/internal/infrastructure/providers"
	"notification-service/pkg/workerpool"
)

func newTestService(t *testing.T) (*application.Service, *memory.NotificationRepository) {
	t.Helper()
	repo := memory.NewNotificationRepository()
	reg := make(providers.Registry)
	reg.Register(&fakeProvider{channel: domain.ChannelEmail})
	pool := workerpool.NewPool(2, 100)
	svc := application.NewService(repo, reg, pool, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc.Start(ctx)
	t.Cleanup(func() { svc.Shutdown(time.Second) })
	return svc, repo
}

type fakeProvider struct {
	channel domain.Channel
	fail    bool
}

func (f *fakeProvider) Name() string            { return "fake" }
func (f *fakeProvider) Channel() domain.Channel { return f.channel }
func (f *fakeProvider) Send(_ context.Context, _ domain.Recipient, _ domain.Content) error {
	if f.fail {
		return errors.New("boom")
	}
	return nil
}

func TestService_Submit(t *testing.T) {
	svc, repo := newTestService(t)
	ctx := context.Background()

	resp, err := svc.Submit(ctx, application.SubmitRequest{
		Channel:    "email",
		Recipients: []string{"alice@example.com"},
		Subject:    "Hello",
		Body:       "World",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, string(domain.StatusPending), resp.Status)

	// Eventually processed.
	require.Eventually(t, func() bool {
		n, err := repo.GetByID(ctx, resp.ID)
		require.NoError(t, err)
		return n.Status == domain.StatusSent
	}, 2*time.Second, 50*time.Millisecond)
}

func TestService_SubmitInvalidChannel(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.Submit(ctx, application.SubmitRequest{
		Channel:    "fax",
		Recipients: []string{"alice@example.com"},
		Body:       "b",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrUnsupportedChannel)
}

func TestService_ValidationStopsOnFirstFailure(t *testing.T) {
	svc, repo := newTestService(t)
	ctx := context.Background()

	resp, err := svc.Submit(ctx, application.SubmitRequest{
		Channel:    "email",
		Recipients: []string{"alice@example.com"},
		Body:       "only body, no subject",
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		n, err := repo.GetByID(ctx, resp.ID)
		require.NoError(t, err)
		return n.Status == domain.StatusRejected
	}, 2*time.Second, 50*time.Millisecond)
}

func TestService_GetStatusAndList(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	resp, err := svc.Submit(ctx, application.SubmitRequest{
		Channel:    "email",
		Recipients: []string{"bob@example.com"},
		Subject:    "S",
		Body:       "B",
	})
	require.NoError(t, err)

	n, err := svc.GetStatus(ctx, resp.ID)
	require.NoError(t, err)
	assert.Equal(t, resp.ID, n.ID)

	list, err := svc.List(ctx, application.ListRequest{Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, list.Total, 1)
}

func TestService_ListDefaultLimit(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := svc.Submit(ctx, application.SubmitRequest{
			Channel:    "email",
			Recipients: []string{"u@example.com"},
			Subject:    "s",
			Body:       "b",
		})
		require.NoError(t, err)
	}

	list, err := svc.List(ctx, application.ListRequest{Limit: 0})
	require.NoError(t, err)
	assert.Len(t, list.Notifications, 5)
}
