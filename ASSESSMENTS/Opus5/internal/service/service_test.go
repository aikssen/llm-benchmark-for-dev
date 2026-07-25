package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/platform/log"
	"github.com/evercifuentes/notifier/internal/provider"
	"github.com/evercifuentes/notifier/internal/provider/simulated"
	"github.com/evercifuentes/notifier/internal/queue"
	"github.com/evercifuentes/notifier/internal/service"
	"github.com/evercifuentes/notifier/internal/store"
	"github.com/evercifuentes/notifier/internal/store/memory"
)

type harness struct {
	svc   *service.Service
	store *memory.Store
	queue *queue.Queue
}

func newHarness(t *testing.T, queueCapacity, maxRecipients int) *harness {
	t.Helper()

	registry := provider.NewRegistry()
	registry.MustRegister(
		simulated.NewEmail(simulated.Behaviour{}),
		simulated.NewSMS(simulated.Behaviour{}),
		simulated.NewPush(simulated.Behaviour{}),
	)

	st := memory.New()
	q := queue.New(queueCapacity)

	return &harness{
		store: st,
		queue: q,
		svc: service.New(st, q, registry,
			service.Limits{MaxRecipients: maxRecipients}, log.Discard()),
	}
}

func command(recipients ...string) service.SubmitCommand {
	cmd := service.SubmitCommand{
		Channel: domain.ChannelEmail,
		Message: domain.Message{Subject: "Hi", Body: "Hello"},
	}
	for _, addr := range recipients {
		cmd.Recipients = append(cmd.Recipients, domain.Recipient{Address: addr})
	}
	return cmd
}

// requireInvalid asserts a request-level validation failure on a given field.
func requireInvalid(t *testing.T, err error, wantField string) {
	t.Helper()
	var invalid *domain.InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want a *domain.InvalidInputError for field %q", err, wantField)
	}
	if invalid.Field != wantField {
		t.Fatalf("field = %q, want %q (message: %s)", invalid.Field, wantField, invalid.Message)
	}
}

func TestSubmitFansOutOneDeliveryPerRecipient(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 100, 100)
	n, err := h.svc.Submit(context.Background(), command("a@example.com", "b@example.com", "c@example.com"))
	if err != nil {
		t.Fatalf("Submit() = %v", err)
	}

	if len(n.Deliveries) != 3 {
		t.Fatalf("created %d deliveries, want one per recipient (3)", len(n.Deliveries))
	}
	if n.Status() != domain.StatusPending {
		t.Errorf("Status() = %q, want %q", n.Status(), domain.StatusPending)
	}
	if got := h.queue.Depth(); got != 3 {
		t.Errorf("queue depth = %d, want one job per recipient", got)
	}

	// Every delivery must be persisted and reachable by ID.
	stored, err := h.store.Get(context.Background(), n.ID)
	if err != nil {
		t.Fatalf("the notification was not persisted: %v", err)
	}
	if len(stored.Deliveries) != 3 {
		t.Errorf("persisted %d deliveries, want 3", len(stored.Deliveries))
	}
}

func TestSubmitQueuesEveryDelivery(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 100, 100)
	n, err := h.svc.Submit(context.Background(), command("a@example.com", "b@example.com"))
	if err != nil {
		t.Fatalf("Submit() = %v", err)
	}

	seen := make(map[string]bool)
	for range 2 {
		job := <-h.queue.Jobs()
		seen[job.DeliveryID] = true
		if job.NotificationID != n.ID {
			t.Errorf("job carries notification %q, want %q", job.NotificationID, n.ID)
		}
		// The job must be self-contained so the worker needs no store read.
		if job.Message.Body != "Hello" || job.Channel != domain.ChannelEmail || job.Recipient.Address == "" {
			t.Errorf("job = %+v, want a self-contained snapshot", job)
		}
	}
	for _, d := range n.Deliveries {
		if !seen[d.ID] {
			t.Errorf("delivery %q was never queued", d.ID)
		}
	}
}

func TestSubmitValidationStopsAtTheFirstProblem(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 100, 5)

	tests := []struct {
		name      string
		cmd       service.SubmitCommand
		wantField string
	}{
		{
			name:      "missing channel",
			cmd:       service.SubmitCommand{Message: domain.Message{Body: "Hello"}},
			wantField: "channel",
		},
		{
			name: "unsupported channel",
			cmd: service.SubmitCommand{
				Channel:    domain.Channel("carrier-pigeon"),
				Recipients: []domain.Recipient{{Address: "a@example.com"}},
				Message:    domain.Message{Body: "Hello"},
			},
			wantField: "channel",
		},
		{
			name:      "no recipients",
			cmd:       command(),
			wantField: "recipients",
		},
		{
			name: "empty body",
			cmd: service.SubmitCommand{
				Channel:    domain.ChannelEmail,
				Recipients: []domain.Recipient{{Address: "a@example.com"}},
				Message:    domain.Message{Body: "   "},
			},
			wantField: "message.body",
		},
		{
			name:      "malformed address is reported with its index",
			cmd:       command("a@example.com", "not-an-address"),
			wantField: "recipients[1].address",
		},
		{
			name:      "empty address",
			cmd:       command("a@example.com", "  "),
			wantField: "recipients[1].address",
		},
		{
			name:      "duplicate recipient",
			cmd:       command("a@example.com", "a@example.com"),
			wantField: "recipients[1]",
		},
		{
			name:      "too many recipients",
			cmd:       command("a@x.com", "b@x.com", "c@x.com", "d@x.com", "e@x.com", "f@x.com"),
			wantField: "recipients",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.svc.Submit(context.Background(), tt.cmd)
			requireInvalid(t, err, tt.wantField)
		})
	}

	// A rejected request must leave no trace: nothing stored, nothing queued.
	if got := h.queue.Depth(); got != 0 {
		t.Errorf("queue depth = %d after only invalid requests, want 0", got)
	}
	page, _ := h.store.List(context.Background(), store.Filter{Limit: 10})
	if page.Total != 0 {
		t.Errorf("stored %d notifications from invalid requests, want 0", page.Total)
	}
}

// A request either enters the system whole or not at all: half a notification
// would silently drop recipients.
func TestSubmitIsRejectedWholeWhenCapacityIsShort(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 5, 100)

	if _, err := h.svc.Submit(context.Background(), command("a@x.com", "b@x.com", "c@x.com")); err != nil {
		t.Fatalf("first Submit() = %v", err)
	}
	// Only 2 slots remain, so a 3-recipient request cannot be admitted.
	_, err := h.svc.Submit(context.Background(), command("d@x.com", "e@x.com", "f@x.com"))
	if !errors.Is(err, service.ErrOverloaded) {
		t.Fatalf("Submit() = %v, want service.ErrOverloaded", err)
	}

	if got := h.queue.Depth(); got != 3 {
		t.Errorf("queue depth = %d, want the refused request to have reserved nothing", got)
	}
	page, _ := h.store.List(context.Background(), store.Filter{Limit: 10})
	if page.Total != 1 {
		t.Errorf("stored %d notifications, want the refused one not to be persisted", page.Total)
	}
}

// A request bigger than the queue could never succeed, so it is a client
// error to fix rather than a load condition to retry.
func TestSubmitReportsARequestLargerThanTheQueueAsInvalid(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 2, 100)
	addresses := []string{"a@x.com", "b@x.com", "c@x.com"}

	_, err := h.svc.Submit(context.Background(), command(addresses...))
	requireInvalid(t, err, "recipients")
	if errors.Is(err, service.ErrOverloaded) {
		t.Error("a permanently oversized request should not be reported as a transient overload")
	}
}

func TestSubmitIsIdempotent(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 100, 100)
	cmd := command("a@example.com")
	cmd.IdempotencyKey = "key-1"

	first, err := h.svc.Submit(context.Background(), cmd)
	if err != nil {
		t.Fatalf("first Submit() = %v", err)
	}
	second, err := h.svc.Submit(context.Background(), cmd)
	if err != nil {
		t.Fatalf("second Submit() = %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("replay returned %q, want the original %q", second.ID, first.ID)
	}
	if got := h.queue.Depth(); got != 1 {
		t.Errorf("queue depth = %d, want the replay not to enqueue a second delivery", got)
	}
	page, _ := h.store.List(context.Background(), store.Filter{Limit: 10})
	if page.Total != 1 {
		t.Errorf("stored %d notifications, want 1", page.Total)
	}
}

func TestSubmitTreatsDifferentIdempotencyKeysAsDistinct(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 100, 100)

	cmdA := command("a@example.com")
	cmdA.IdempotencyKey = "key-a"
	cmdB := command("a@example.com")
	cmdB.IdempotencyKey = "key-b"

	first, _ := h.svc.Submit(context.Background(), cmdA)
	second, err := h.svc.Submit(context.Background(), cmdB)
	if err != nil {
		t.Fatalf("Submit() = %v", err)
	}
	if first.ID == second.ID {
		t.Error("different idempotency keys produced the same notification")
	}
}

func TestSubmitAcceptsAValidRequestOnEveryChannel(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 100, 100)
	cases := []struct {
		channel domain.Channel
		address string
	}{
		{domain.ChannelEmail, "user@example.com"},
		{domain.ChannelSMS, "+14155550123"},
		{domain.ChannelPush, "dGhpcy1pcy1hLXRva2Vu:APA91b"},
	}

	for _, tc := range cases {
		t.Run(tc.channel.String(), func(t *testing.T) {
			_, err := h.svc.Submit(context.Background(), service.SubmitCommand{
				Channel:    tc.channel,
				Recipients: []domain.Recipient{{Address: tc.address}},
				Message:    domain.Message{Subject: "Hi", Body: "Hello"},
			})
			if err != nil {
				t.Fatalf("Submit() on %q = %v", tc.channel, err)
			}
		})
	}
}

func TestGet(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 100, 100)
	n, _ := h.svc.Submit(context.Background(), command("a@example.com"))

	got, err := h.svc.Get(context.Background(), n.ID)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if got.ID != n.ID {
		t.Errorf("Get() returned %q, want %q", got.ID, n.ID)
	}

	if _, err := h.svc.Get(context.Background(), "ntf_missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get() for an unknown id = %v, want ErrNotFound", err)
	}
	if _, err := h.svc.Get(context.Background(), "  "); err == nil {
		t.Error("Get() with a blank id = nil, want an error")
	}
}

func TestList(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 200, 100)
	for i := range 5 {
		_, err := h.svc.Submit(context.Background(), command(fmt.Sprintf("user%d@example.com", i)))
		if err != nil {
			t.Fatalf("Submit() = %v", err)
		}
	}

	t.Run("defaults to a bounded page", func(t *testing.T) {
		page, err := h.svc.List(context.Background(), store.Filter{})
		if err != nil {
			t.Fatalf("List() = %v", err)
		}
		if page.Limit != 20 {
			t.Errorf("Limit = %d, want the default of 20", page.Limit)
		}
		if page.Total != 5 {
			t.Errorf("Total = %d, want 5", page.Total)
		}
	})

	t.Run("caps an excessive limit", func(t *testing.T) {
		page, err := h.svc.List(context.Background(), store.Filter{Limit: 10_000})
		if err != nil {
			t.Fatalf("List() = %v", err)
		}
		if page.Limit > 100 {
			t.Errorf("Limit = %d, want it capped at 100 so one client cannot ask for everything", page.Limit)
		}
	})

	t.Run("rejects an unsupported channel filter", func(t *testing.T) {
		_, err := h.svc.List(context.Background(), store.Filter{Channel: domain.Channel("fax")})
		requireInvalid(t, err, "channel")
	})

	t.Run("rejects an unknown status filter", func(t *testing.T) {
		_, err := h.svc.List(context.Background(), store.Filter{Status: domain.Status("in_progress")})
		requireInvalid(t, err, "status")
	})

	t.Run("rejects a negative offset", func(t *testing.T) {
		_, err := h.svc.List(context.Background(), store.Filter{Offset: -1})
		requireInvalid(t, err, "offset")
	})
}

func TestSubmitIsRefusedOnceTheQueueIsClosed(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 100, 100)
	h.queue.Close()

	_, err := h.svc.Submit(context.Background(), command("a@example.com"))
	if !errors.Is(err, service.ErrOverloaded) {
		t.Fatalf("Submit() during shutdown = %v, want service.ErrOverloaded", err)
	}
}

// The error message must name the channels the deployment actually supports,
// so a client can fix the request without reading the source.
func TestUnsupportedChannelErrorListsTheSupportedOnes(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 100, 100)
	_, err := h.svc.Submit(context.Background(), service.SubmitCommand{
		Channel:    domain.Channel("fax"),
		Recipients: []domain.Recipient{{Address: "a@example.com"}},
		Message:    domain.Message{Body: "Hello"},
	})

	var invalid *domain.InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want a *domain.InvalidInputError", err)
	}
	for _, want := range []string{"email", "push", "sms"} {
		if !strings.Contains(invalid.Message, want) {
			t.Errorf("message %q does not mention the supported channel %q", invalid.Message, want)
		}
	}
}

func TestChannelsReflectsTheRegistry(t *testing.T) {
	t.Parallel()

	h := newHarness(t, 10, 10)
	if got := len(h.svc.Channels()); got != 3 {
		t.Errorf("Channels() returned %d entries, want 3", got)
	}
}
