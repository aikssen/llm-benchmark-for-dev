package processing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evercifuentes/notifier/internal/dispatcher"
	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/platform/log"
	"github.com/evercifuentes/notifier/internal/processing"
	"github.com/evercifuentes/notifier/internal/provider"
	"github.com/evercifuentes/notifier/internal/queue"
	"github.com/evercifuentes/notifier/internal/store/memory"
	"github.com/evercifuentes/notifier/internal/validation"
)

// stubDispatcher returns a fixed outcome without touching a provider.
type stubDispatcher struct {
	result dispatcher.Result
	err    error
	calls  int
}

func (s *stubDispatcher) Dispatch(context.Context, dispatcher.Request) (dispatcher.Result, error) {
	s.calls++
	return s.result, s.err
}

// fixture wires a processor over a real in-memory store with one pending
// delivery, which is the state a worker actually receives.
type fixture struct {
	store      *memory.Store
	dispatcher *stubDispatcher
	processor  *processing.Processor
	job        queue.Job
	now        time.Time
}

func newFixture(t *testing.T, validators *validation.Pipeline, disp *stubDispatcher) *fixture {
	t.Helper()

	now := time.Unix(1000, 0)
	st := memory.New(memory.WithClock(func() time.Time { return now }))

	n := &domain.Notification{
		ID:      "ntf_1",
		Channel: domain.ChannelEmail,
		Message: domain.Message{Subject: "Hi", Body: "Hello"},
		Deliveries: []*domain.Delivery{{
			ID:             "dlv_1",
			NotificationID: "ntf_1",
			Recipient:      domain.Recipient{ID: "u1", Address: "user@example.com"},
			Status:         domain.DeliveryPending,
		}},
	}
	if err := st.Create(context.Background(), n); err != nil {
		t.Fatalf("seeding the store: %v", err)
	}

	return &fixture{
		store:      st,
		dispatcher: disp,
		now:        now,
		processor: processing.New(st, validators, disp, log.Discard(),
			processing.WithClock(func() time.Time { return now })),
		job: queue.Job{
			NotificationID: "ntf_1",
			DeliveryID:     "dlv_1",
			Channel:        domain.ChannelEmail,
			Recipient:      domain.Recipient{ID: "u1", Address: "user@example.com"},
			Message:        domain.Message{Subject: "Hi", Body: "Hello"},
		},
	}
}

// delivery reads back the stored delivery.
func (f *fixture) delivery(t *testing.T) *domain.Delivery {
	t.Helper()
	n, err := f.store.Get(context.Background(), "ntf_1")
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	return n.Deliveries[0]
}

// passing is a pipeline whose validators all accept.
func passing() *validation.Pipeline {
	return validation.New(validation.ValidatorFunc{
		ValidatorName: "always-ok",
		Fn:            func(context.Context, validation.Subject) error { return nil },
	})
}

func TestProcessRecordsASuccessfulDelivery(t *testing.T) {
	t.Parallel()

	f := newFixture(t, passing(), &stubDispatcher{
		result: dispatcher.Result{Provider: "simulated-email", MessageID: "msg-1", Attempts: 2},
	})
	f.processor.Process(context.Background(), f.job)

	d := f.delivery(t)
	if d.Status != domain.DeliveryDelivered {
		t.Errorf("Status = %q, want %q", d.Status, domain.DeliveryDelivered)
	}
	if d.Provider != "simulated-email" || d.ProviderMessageID != "msg-1" || d.Attempts != 2 {
		t.Errorf("delivery = %+v, want the dispatch result recorded for tracing", d)
	}
	if d.CompletedAt == nil || !d.CompletedAt.Equal(f.now) {
		t.Error("CompletedAt was not stamped on a terminal delivery")
	}
	if d.FailureCode != "" || d.FailureReason != "" {
		t.Errorf("a successful delivery carries failure details: %q / %q", d.FailureCode, d.FailureReason)
	}
}

// A rejected delivery must never reach a provider: that is the whole point of
// validating first.
func TestProcessRejectsWithoutCallingTheProvider(t *testing.T) {
	t.Parallel()

	pipeline := validation.New(validation.ValidatorFunc{
		ValidatorName: "preferences",
		Fn: func(context.Context, validation.Subject) error {
			return validation.Reject(validation.CodeOptedOut, "recipient has opted out")
		},
	})
	disp := &stubDispatcher{}
	f := newFixture(t, pipeline, disp)

	f.processor.Process(context.Background(), f.job)

	if disp.calls != 0 {
		t.Error("the provider was called despite a failed validation")
	}
	d := f.delivery(t)
	if d.Status != domain.DeliveryRejected {
		t.Errorf("Status = %q, want %q", d.Status, domain.DeliveryRejected)
	}
	if d.FailureCode != validation.CodeOptedOut {
		t.Errorf("FailureCode = %q, want %q", d.FailureCode, validation.CodeOptedOut)
	}
	if d.Attempts != 0 {
		t.Errorf("Attempts = %d, want 0: no provider call was made", d.Attempts)
	}
	if d.CompletedAt == nil {
		t.Error("a rejected delivery is terminal and must be stamped as completed")
	}
}

// A validator whose own dependency is broken is an infrastructure problem.
// Recording it as "rejected" would wrongly imply the notification was bad.
func TestProcessDistinguishesABrokenValidatorFromARejection(t *testing.T) {
	t.Parallel()

	pipeline := validation.New(validation.ValidatorFunc{
		ValidatorName: "preferences",
		Fn: func(context.Context, validation.Subject) error {
			return errors.New("preference service unreachable")
		},
	})
	f := newFixture(t, pipeline, &stubDispatcher{})

	f.processor.Process(context.Background(), f.job)

	d := f.delivery(t)
	if d.Status != domain.DeliveryFailed {
		t.Errorf("Status = %q, want %q", d.Status, domain.DeliveryFailed)
	}
	if d.FailureCode != processing.CodeValidationUnavailable {
		t.Errorf("FailureCode = %q, want %q", d.FailureCode, processing.CodeValidationUnavailable)
	}
}

func TestProcessRecordsAProviderFailure(t *testing.T) {
	t.Parallel()

	f := newFixture(t, passing(), &stubDispatcher{
		result: dispatcher.Result{Provider: "simulated-email", Attempts: 3},
		err:    provider.Transient("simulated-email", provider.CodeUnavailable, "provider is down"),
	})
	f.processor.Process(context.Background(), f.job)

	d := f.delivery(t)
	if d.Status != domain.DeliveryFailed {
		t.Errorf("Status = %q, want %q", d.Status, domain.DeliveryFailed)
	}
	if d.FailureCode != provider.CodeUnavailable {
		t.Errorf("FailureCode = %q, want %q", d.FailureCode, provider.CodeUnavailable)
	}
	if d.FailureReason == "" {
		t.Error("a failed delivery must carry a human-readable reason")
	}
	// The attempt count is what tells an operator how hard the service tried.
	if d.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", d.Attempts)
	}
}

// Every path through Process must reach a terminal state; a delivery stuck in
// "sending" is invisible to clients and to alerting.
func TestProcessAlwaysReachesATerminalState(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		pipeline *validation.Pipeline
		disp     *stubDispatcher
	}{
		"delivered": {passing(), &stubDispatcher{result: dispatcher.Result{Attempts: 1}}},
		"rejected": {validation.New(validation.ValidatorFunc{
			ValidatorName: "v",
			Fn:            func(context.Context, validation.Subject) error { return validation.Reject("code", "reason") },
		}), &stubDispatcher{}},
		"validator broken": {validation.New(validation.ValidatorFunc{
			ValidatorName: "v",
			Fn:            func(context.Context, validation.Subject) error { return errors.New("boom") },
		}), &stubDispatcher{}},
		"provider failed": {passing(), &stubDispatcher{err: errors.New("boom")}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, tc.pipeline, tc.disp)
			f.processor.Process(context.Background(), f.job)

			if d := f.delivery(t); !d.Status.Terminal() {
				t.Errorf("Status = %q, want a terminal state", d.Status)
			}
		})
	}
}

// If the outcome were written with the cancelled context, a shutdown would
// leave deliveries permanently stuck in "sending".
func TestProcessRecordsTheOutcomeEvenWhenTheContextIsCancelled(t *testing.T) {
	t.Parallel()

	f := newFixture(t, passing(), &stubDispatcher{
		result: dispatcher.Result{Provider: "simulated-email", Attempts: 1},
		err:    context.Canceled,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.processor.Process(ctx, f.job)

	d := f.delivery(t)
	if !d.Status.Terminal() {
		t.Fatalf("Status = %q, want the outcome persisted despite the cancellation", d.Status)
	}
	if d.FailureCode != processing.CodeCancelled {
		t.Errorf("FailureCode = %q, want %q", d.FailureCode, processing.CodeCancelled)
	}
}

// A missing delivery must be logged, not fatal: one bad job cannot be allowed
// to take down the worker that picked it up.
func TestProcessToleratesAnUnknownDelivery(t *testing.T) {
	t.Parallel()

	f := newFixture(t, passing(), &stubDispatcher{result: dispatcher.Result{Attempts: 1}})
	job := f.job
	job.DeliveryID = "dlv_missing"

	f.processor.Process(context.Background(), job) // must not panic

	if d := f.delivery(t); d.Status != domain.DeliveryPending {
		t.Errorf("the real delivery was modified: Status = %q", d.Status)
	}
}
