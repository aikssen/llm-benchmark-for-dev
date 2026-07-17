package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/service"
	"github.com/aikssen/notification-service/internal/store"
	"github.com/aikssen/notification-service/internal/validation"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeEnqueuer struct {
	mu       sync.Mutex
	enqueued []string
	err      error
}

func (f *fakeEnqueuer) Enqueue(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.enqueued = append(f.enqueued, id)
	return nil
}

func (f *fakeEnqueuer) enqueuedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.enqueued...)
}

type erroringRequestValidator struct{ err error }

func (erroringRequestValidator) Name() string { return "erroring" }
func (v erroringRequestValidator) Validate(context.Context, domain.BatchRequest) error {
	return v.err
}

// perRecipientValidator fails validation only for recipients present in fail.
type perRecipientValidator struct{ fail map[string]error }

func (perRecipientValidator) Name() string { return "per_recipient" }
func (v perRecipientValidator) Validate(_ context.Context, _ domain.Channel, recipient string) error {
	return v.fail[recipient]
}

func TestSubmit_NoRecipients(t *testing.T) {
	repo := store.NewMemoryRepository()
	pipeline := validation.NewPipeline(nil, nil)
	queue := &fakeEnqueuer{}
	svc := service.New(repo, pipeline, queue, testLogger())

	_, err := svc.Submit(context.Background(), domain.BatchRequest{Channel: domain.ChannelEmail})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrValidation)

	_, total, listErr := repo.List(context.Background(), store.ListFilter{})
	require.NoError(t, listErr)
	assert.Equal(t, 0, total, "no notification rows should be created when the batch itself is rejected")
}

func TestSubmit_RequestValidationFailureCreatesNothing(t *testing.T) {
	repo := store.NewMemoryRepository()
	boom := errors.New("boom")
	pipeline := validation.NewPipeline(
		[]validation.RequestValidator{erroringRequestValidator{err: boom}},
		nil,
	)
	queue := &fakeEnqueuer{}
	svc := service.New(repo, pipeline, queue, testLogger())

	_, err := svc.Submit(context.Background(), domain.BatchRequest{
		Channel: domain.ChannelEmail, Recipients: []string{"a@example.com"},
	})
	require.ErrorIs(t, err, boom)

	_, total, listErr := repo.List(context.Background(), store.ListFilter{})
	require.NoError(t, listErr)
	assert.Equal(t, 0, total)
	assert.Empty(t, queue.enqueuedIDs())
}

func TestSubmit_MixedRecipientOutcomesAreIndependent(t *testing.T) {
	repo := store.NewMemoryRepository()
	badErr := errors.New("bad recipient")
	pipeline := validation.NewPipeline(
		nil,
		[]validation.RecipientValidator{perRecipientValidator{fail: map[string]error{"bad@example.com": badErr}}},
	)
	queue := &fakeEnqueuer{}
	svc := service.New(repo, pipeline, queue, testLogger())

	result, err := svc.Submit(context.Background(), domain.BatchRequest{
		Channel:    domain.ChannelEmail,
		Recipients: []string{"good@example.com", "bad@example.com"},
	})
	require.NoError(t, err, "a per-recipient failure must not fail the whole batch")
	require.Len(t, result.Notifications, 2)

	byRecipient := map[string]*domain.Notification{}
	for _, n := range result.Notifications {
		byRecipient[n.Recipient] = n
		assert.Equal(t, result.BatchID, n.BatchID)
	}

	assert.Equal(t, domain.StatusPending, byRecipient["good@example.com"].Status)
	assert.Equal(t, domain.StatusFailed, byRecipient["bad@example.com"].Status)
	assert.NotEmpty(t, byRecipient["bad@example.com"].LastError)

	assert.Equal(t, []string{byRecipient["good@example.com"].ID}, queue.enqueuedIDs(),
		"only the recipient that passed validation should be enqueued for delivery")
}

func TestSubmit_QueueFullMarksNotificationFailed(t *testing.T) {
	repo := store.NewMemoryRepository()
	pipeline := validation.NewPipeline(nil, nil)
	queue := &fakeEnqueuer{err: domain.ErrQueueFull}
	svc := service.New(repo, pipeline, queue, testLogger())

	result, err := svc.Submit(context.Background(), domain.BatchRequest{
		Channel: domain.ChannelEmail, Recipients: []string{"a@example.com"},
	})
	require.NoError(t, err)
	require.Len(t, result.Notifications, 1)
	n := result.Notifications[0]
	assert.Equal(t, domain.StatusFailed, n.Status)
	assert.Contains(t, n.LastError, "queue is full")

	stored, getErr := repo.Get(context.Background(), n.ID)
	require.NoError(t, getErr)
	assert.Equal(t, domain.StatusFailed, stored.Status, "the persisted record must reflect the queue-full outcome")
}

func TestGetAndList_DelegateToRepository(t *testing.T) {
	repo := store.NewMemoryRepository()
	pipeline := validation.NewPipeline(nil, nil)
	queue := &fakeEnqueuer{}
	svc := service.New(repo, pipeline, queue, testLogger())

	result, err := svc.Submit(context.Background(), domain.BatchRequest{
		Channel: domain.ChannelEmail, Recipients: []string{"a@example.com"},
	})
	require.NoError(t, err)
	id := result.Notifications[0].ID

	got, err := svc.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)

	_, err = svc.Get(context.Background(), "missing")
	assert.ErrorIs(t, err, domain.ErrNotFound)

	list, total, err := svc.List(context.Background(), store.ListFilter{})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, list, 1)
}
