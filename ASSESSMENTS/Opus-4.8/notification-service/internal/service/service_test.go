package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aikssen/notification-service/internal/domain"
	"github.com/aikssen/notification-service/internal/store"
)

// --- fakes -------------------------------------------------------------------

type fakeValidator struct{ err error }

func (f fakeValidator) Validate(context.Context, *domain.Notification) error { return f.err }

type fakeDeliverer struct {
	result DeliveryResult
}

func (f fakeDeliverer) Deliver(context.Context, *domain.Notification) DeliveryResult {
	return f.result
}

type fakeQueue struct {
	ids []string
	err error
}

func (f *fakeQueue) Submit(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.ids = append(f.ids, id)
	return nil
}

func newTestService(v validator, d deliverer, q enqueuer) (*Service, store.Store) {
	st := store.NewMemory()
	svc := New(Deps{
		Store:          st,
		Validator:      v,
		Deliverer:      d,
		Queue:          q,
		SupportedChans: []domain.Channel{domain.ChannelEmail, domain.ChannelSMS, domain.ChannelPush},
		Clock:          func() time.Time { return time.Unix(0, 0) },
	})
	return svc, st
}

func validRequest() SubmitRequest {
	return SubmitRequest{
		Channel:    domain.ChannelEmail,
		Recipients: []string{"a@example.com", "b@example.com", "c@example.com"},
		Message:    domain.Message{Body: "hello"},
	}
}

// --- Submit ------------------------------------------------------------------

func TestSubmit_FansOutPerRecipient(t *testing.T) {
	q := &fakeQueue{}
	svc, st := newTestService(fakeValidator{}, fakeDeliverer{}, q)

	created, err := svc.Submit(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(created) != 3 {
		t.Fatalf("expected 3 notifications, got %d", len(created))
	}
	if len(q.ids) != 3 {
		t.Errorf("expected 3 enqueued, got %d", len(q.ids))
	}
	// All share one request ID and are persisted as pending.
	reqID := created[0].RequestID
	for _, n := range created {
		if n.RequestID != reqID {
			t.Error("recipients should share one request ID")
		}
		got, err := st.Get(context.Background(), n.ID)
		if err != nil {
			t.Fatalf("notification not persisted: %v", err)
		}
		if got.Status != domain.StatusPending {
			t.Errorf("expected pending, got %s", got.Status)
		}
	}
}

func TestSubmit_RejectsInvalidRequests(t *testing.T) {
	svc, _ := newTestService(fakeValidator{}, fakeDeliverer{}, &fakeQueue{})
	cases := map[string]SubmitRequest{
		"unknown channel": {Channel: "carrier-pigeon", Recipients: []string{"x"}, Message: domain.Message{Body: "b"}},
		"no recipients":   {Channel: domain.ChannelEmail, Recipients: nil, Message: domain.Message{Body: "b"}},
		"empty body":      {Channel: domain.ChannelEmail, Recipients: []string{"x"}, Message: domain.Message{Body: ""}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Submit(context.Background(), req)
			if !errors.Is(err, domain.ErrValidation) {
				t.Errorf("expected validation error, got %v", err)
			}
		})
	}
}

func TestSubmit_EnqueueFailureMarksFailed(t *testing.T) {
	q := &fakeQueue{err: errors.New("overloaded")}
	svc, st := newTestService(fakeValidator{}, fakeDeliverer{}, q)

	created, err := svc.Submit(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("submit should succeed even if enqueue fails: %v", err)
	}
	for _, n := range created {
		got, _ := st.Get(context.Background(), n.ID)
		if got.Status != domain.StatusFailed {
			t.Errorf("un-enqueued notification should be marked failed, got %s", got.Status)
		}
	}
}

// --- Process -----------------------------------------------------------------

func processOne(t *testing.T, v validator, d deliverer) *domain.Notification {
	t.Helper()
	q := &fakeQueue{}
	svc, st := newTestService(v, d, q)
	created, err := svc.Submit(context.Background(), SubmitRequest{
		Channel: domain.ChannelEmail, Recipients: []string{"a@example.com"}, Message: domain.Message{Body: "hi"},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	svc.Process(context.Background(), created[0].ID)
	got, _ := st.Get(context.Background(), created[0].ID)
	return got
}

func TestProcess_HappyPath(t *testing.T) {
	got := processOne(t, fakeValidator{}, fakeDeliverer{result: DeliveryResult{Attempts: 2}})
	if got.Status != domain.StatusDelivered {
		t.Errorf("expected delivered, got %s", got.Status)
	}
	if got.Attempts != 2 {
		t.Errorf("expected attempts recorded, got %d", got.Attempts)
	}
}

func TestProcess_ValidationFailure(t *testing.T) {
	got := processOne(t, fakeValidator{err: errors.New("bad recipient")}, fakeDeliverer{})
	if got.Status != domain.StatusFailed {
		t.Errorf("expected failed, got %s", got.Status)
	}
	if got.LastError == "" {
		t.Error("expected failure reason recorded")
	}
}

func TestProcess_DeliveryFailure(t *testing.T) {
	got := processOne(t, fakeValidator{}, fakeDeliverer{result: DeliveryResult{Attempts: 4, Err: errors.New("exhausted")}})
	if got.Status != domain.StatusFailed {
		t.Errorf("expected failed, got %s", got.Status)
	}
	if got.Attempts != 4 {
		t.Errorf("expected attempts recorded, got %d", got.Attempts)
	}
}

func TestProcess_MissingNotificationIsNoOp(t *testing.T) {
	svc, _ := newTestService(fakeValidator{}, fakeDeliverer{}, &fakeQueue{})
	// Should not panic on an unknown ID.
	svc.Process(context.Background(), "does-not-exist")
}
