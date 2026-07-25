package memory_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/store"
	"github.com/evercifuentes/notifier/internal/store/memory"
)

func notification(id string, ch domain.Channel, deliveryIDs ...string) *domain.Notification {
	n := &domain.Notification{
		ID:        id,
		Channel:   ch,
		Message:   domain.Message{Subject: "Hi", Body: "Hello"},
		CreatedAt: time.Unix(0, 0),
		UpdatedAt: time.Unix(0, 0),
	}
	for _, dID := range deliveryIDs {
		n.Deliveries = append(n.Deliveries, &domain.Delivery{
			ID:             dID,
			NotificationID: id,
			Recipient:      domain.Recipient{ID: dID, Address: dID + "@example.com"},
			Status:         domain.DeliveryPending,
		})
	}
	return n
}

func TestCreateAndGet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := memory.New()
	want := notification("ntf_1", domain.ChannelEmail, "dlv_1", "dlv_2")

	if err := s.Create(ctx, want); err != nil {
		t.Fatalf("Create() = %v", err)
	}
	got, err := s.Get(ctx, "ntf_1")
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if got.ID != want.ID || len(got.Deliveries) != 2 {
		t.Errorf("Get() returned %+v, want the stored notification", got)
	}
}

func TestGetUnknownIDReturnsNotFound(t *testing.T) {
	t.Parallel()

	_, err := memory.New().Get(context.Background(), "ntf_missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get() = %v, want domain.ErrNotFound", err)
	}
}

func TestCreateRejectsDuplicates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := memory.New()

	n := notification("ntf_1", domain.ChannelEmail, "dlv_1")
	n.IdempotencyKey = "key-1"
	if err := s.Create(ctx, n); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	if err := s.Create(ctx, notification("ntf_1", domain.ChannelSMS, "dlv_2")); !errors.Is(err, memory.ErrDuplicate) {
		t.Errorf("Create() with a duplicate id = %v, want ErrDuplicate", err)
	}

	other := notification("ntf_2", domain.ChannelEmail, "dlv_3")
	other.IdempotencyKey = "key-1"
	if err := s.Create(ctx, other); !errors.Is(err, memory.ErrDuplicate) {
		t.Errorf("Create() with a duplicate idempotency key = %v, want ErrDuplicate", err)
	}
}

// The store must hand out copies: a caller holding the returned pointer must
// not be able to rewrite persisted state.
func TestStoredStateIsIsolatedFromCallers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := memory.New()
	original := notification("ntf_1", domain.ChannelEmail, "dlv_1")

	if err := s.Create(ctx, original); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	// Mutating the object passed to Create must not affect the store.
	original.Deliveries[0].Status = domain.DeliveryDelivered

	got, _ := s.Get(ctx, "ntf_1")
	if got.Deliveries[0].Status != domain.DeliveryPending {
		t.Error("mutating the argument to Create changed the stored state")
	}

	// Mutating what Get returns must not affect the store either.
	got.Deliveries[0].Status = domain.DeliveryFailed
	again, _ := s.Get(ctx, "ntf_1")
	if again.Deliveries[0].Status != domain.DeliveryPending {
		t.Error("mutating the result of Get changed the stored state")
	}
}

func TestUpdateDelivery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := time.Unix(1000, 0)
	s := memory.New(memory.WithClock(func() time.Time { return clock }))

	if err := s.Create(ctx, notification("ntf_1", domain.ChannelEmail, "dlv_1", "dlv_2")); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	err := s.UpdateDelivery(ctx, "ntf_1", "dlv_1", func(d *domain.Delivery) {
		d.Status = domain.DeliveryDelivered
		d.Provider = "simulated-email"
		d.Attempts = 2
	})
	if err != nil {
		t.Fatalf("UpdateDelivery() = %v", err)
	}

	got, _ := s.Get(ctx, "ntf_1")
	if got.Deliveries[0].Status != domain.DeliveryDelivered || got.Deliveries[0].Attempts != 2 {
		t.Errorf("delivery = %+v, want the mutation applied", got.Deliveries[0])
	}
	if got.Deliveries[1].Status != domain.DeliveryPending {
		t.Error("the update leaked into a sibling delivery")
	}
	if !got.UpdatedAt.Equal(clock) || !got.Deliveries[0].UpdatedAt.Equal(clock) {
		t.Error("UpdatedAt was not refreshed on the delivery and its notification")
	}
	// The aggregate status follows from the deliveries.
	if got.Status() != domain.StatusProcessing {
		t.Errorf("Status() = %q, want %q", got.Status(), domain.StatusProcessing)
	}
}

func TestUpdateDeliveryOnUnknownIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := memory.New()
	_ = s.Create(ctx, notification("ntf_1", domain.ChannelEmail, "dlv_1"))

	noop := func(*domain.Delivery) {}
	if err := s.UpdateDelivery(ctx, "ntf_missing", "dlv_1", noop); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown notification = %v, want ErrNotFound", err)
	}
	if err := s.UpdateDelivery(ctx, "ntf_1", "dlv_missing", noop); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown delivery = %v, want ErrNotFound", err)
	}
}

func TestFindByIdempotencyKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := memory.New()

	n := notification("ntf_1", domain.ChannelEmail, "dlv_1")
	n.IdempotencyKey = "key-1"
	_ = s.Create(ctx, n)

	got, err := s.FindByIdempotencyKey(ctx, "key-1")
	if err != nil {
		t.Fatalf("FindByIdempotencyKey() = %v", err)
	}
	if got.ID != "ntf_1" {
		t.Errorf("found %q, want %q", got.ID, "ntf_1")
	}

	if _, err := s.FindByIdempotencyKey(ctx, "key-unknown"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown key = %v, want ErrNotFound", err)
	}
	if _, err := s.FindByIdempotencyKey(ctx, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("empty key = %v, want ErrNotFound", err)
	}
}

func TestListReturnsNewestFirst(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := memory.New()
	for i := range 5 {
		_ = s.Create(ctx, notification(fmt.Sprintf("ntf_%d", i), domain.ChannelEmail, "dlv"))
	}

	page, err := s.List(ctx, store.Filter{Limit: 10})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if page.Total != 5 || len(page.Items) != 5 {
		t.Fatalf("List() returned %d of %d, want 5 of 5", len(page.Items), page.Total)
	}
	if page.Items[0].ID != "ntf_4" {
		t.Errorf("first item = %q, want the most recent (%q)", page.Items[0].ID, "ntf_4")
	}
}

func TestListPagination(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := memory.New()
	for i := range 10 {
		_ = s.Create(ctx, notification(fmt.Sprintf("ntf_%d", i), domain.ChannelEmail, "dlv"))
	}

	page, err := s.List(ctx, store.Filter{Limit: 3, Offset: 3})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if page.Total != 10 {
		t.Errorf("Total = %d, want the unpaged count of 10", page.Total)
	}
	if len(page.Items) != 3 {
		t.Fatalf("returned %d items, want 3", len(page.Items))
	}
	// Newest first: index 3 of [9,8,7,6,...] is 6.
	if page.Items[0].ID != "ntf_6" {
		t.Errorf("first item at offset 3 = %q, want %q", page.Items[0].ID, "ntf_6")
	}
}

func TestListFilters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := memory.New()

	_ = s.Create(ctx, notification("ntf_email", domain.ChannelEmail, "dlv_1"))
	_ = s.Create(ctx, notification("ntf_sms", domain.ChannelSMS, "dlv_2"))
	_ = s.UpdateDelivery(ctx, "ntf_sms", "dlv_2", func(d *domain.Delivery) {
		d.Status = domain.DeliveryDelivered
	})

	t.Run("by channel", func(t *testing.T) {
		page, _ := s.List(ctx, store.Filter{Channel: domain.ChannelSMS, Limit: 10})
		if page.Total != 1 || page.Items[0].ID != "ntf_sms" {
			t.Errorf("filtering by channel returned %d items, want just the SMS one", page.Total)
		}
	})

	t.Run("by derived status", func(t *testing.T) {
		page, _ := s.List(ctx, store.Filter{Status: domain.StatusDelivered, Limit: 10})
		if page.Total != 1 || page.Items[0].ID != "ntf_sms" {
			t.Errorf("filtering by status returned %d items, want just the delivered one", page.Total)
		}
	})

	t.Run("no match", func(t *testing.T) {
		page, _ := s.List(ctx, store.Filter{Channel: domain.ChannelPush, Limit: 10})
		if page.Total != 0 || len(page.Items) != 0 {
			t.Errorf("returned %d items, want none", len(page.Items))
		}
		if page.Items == nil {
			t.Error("Items should be an empty slice, not nil, so it serialises as []")
		}
	})
}

// Many workers advance different deliveries of the same notification at once;
// the store must serialise those updates without losing any.
func TestConcurrentDeliveryUpdates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := memory.New()

	const deliveries = 100
	ids := make([]string, deliveries)
	for i := range ids {
		ids[i] = fmt.Sprintf("dlv_%d", i)
	}
	if err := s.Create(ctx, notification("ntf_1", domain.ChannelEmail, ids...)); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	var wg sync.WaitGroup
	for _, dID := range ids {
		wg.Add(1)
		go func(dID string) {
			defer wg.Done()
			_ = s.UpdateDelivery(ctx, "ntf_1", dID, func(d *domain.Delivery) {
				d.Status = domain.DeliveryDelivered
				d.Attempts = 1
			})
		}(dID)
	}
	// Concurrent readers, to catch a read/write race.
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Get(ctx, "ntf_1")
			_, _ = s.List(ctx, store.Filter{Limit: 5})
		}()
	}
	wg.Wait()

	got, _ := s.Get(ctx, "ntf_1")
	if summary := got.Summary(); summary.Delivered != deliveries {
		t.Errorf("%d deliveries marked delivered, want %d", summary.Delivered, deliveries)
	}
	if got.Status() != domain.StatusDelivered {
		t.Errorf("Status() = %q, want %q", got.Status(), domain.StatusDelivered)
	}
}
