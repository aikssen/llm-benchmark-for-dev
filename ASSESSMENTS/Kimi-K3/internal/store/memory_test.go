package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"notifications/internal/domain"
)

func makeNotif(t *testing.T, st *Memory, channel domain.Channel, recipient string) *domain.Notification {
	t.Helper()
	n := domain.New("req-1", channel, recipient, domain.Content{Body: "x"}, time.Now())
	if err := st.Upsert(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUpsertAndGet(t *testing.T) {
	st := NewMemory()
	n := makeNotif(t, st, domain.ChannelEmail, "a@example.com")

	got, err := st.Get(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != n.ID || got.Recipient != "a@example.com" {
		t.Fatalf("unexpected notification: %+v", got)
	}

	// Update the status and verify it persists.
	n.Status = domain.StatusDelivered
	if err := st.Upsert(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get(context.Background(), n.ID)
	if got.Status != domain.StatusDelivered {
		t.Fatalf("expected delivered, got %s", got.Status)
	}
}

func TestGetNotFound(t *testing.T) {
	st := NewMemory()
	if _, err := st.Get(context.Background(), "nope"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestReturnedCopiesAreIsolated(t *testing.T) {
	st := NewMemory()
	n := makeNotif(t, st, domain.ChannelEmail, "a@example.com")

	got, _ := st.Get(context.Background(), n.ID)
	got.Status = domain.StatusFailed // mutating the copy must not affect the store

	again, _ := st.Get(context.Background(), n.ID)
	if again.Status != domain.StatusPending {
		t.Fatalf("store state was mutated through a returned copy: %s", again.Status)
	}
}

func TestListFiltersAndPagination(t *testing.T) {
	st := NewMemory()
	emails := make([]*domain.Notification, 0, 5)
	for i := 0; i < 5; i++ {
		emails = append(emails, makeNotif(t, st, domain.ChannelEmail, fmt.Sprintf("u%d@example.com", i)))
	}
	makeNotif(t, st, domain.ChannelSMS, "+15550000000")

	// No filter: everything, insertion order.
	all, _ := st.List(context.Background(), ListFilter{Limit: 50})
	if len(all) != 6 {
		t.Fatalf("expected 6, got %d", len(all))
	}

	// Filter by channel.
	sms, _ := st.List(context.Background(), ListFilter{Channel: domain.ChannelSMS})
	if len(sms) != 1 || sms[0].Channel != domain.ChannelSMS {
		t.Fatalf("channel filter failed: %+v", sms)
	}

	// Filter by status.
	emails[0].Status = domain.StatusDelivered
	_ = st.Upsert(context.Background(), emails[0])
	delivered, _ := st.List(context.Background(), ListFilter{Status: domain.StatusDelivered})
	if len(delivered) != 1 {
		t.Fatalf("status filter failed: %+v", delivered)
	}

	// Pagination.
	page, _ := st.List(context.Background(), ListFilter{Limit: 2, Offset: 2})
	if len(page) != 2 || page[0].ID != all[2].ID {
		t.Fatalf("pagination failed: %+v", page)
	}
}

func TestConcurrentAccess(t *testing.T) {
	st := NewMemory()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			n := domain.New("req", domain.ChannelEmail, fmt.Sprintf("u%d@example.com", i), domain.Content{Body: "x"}, time.Now())
			_ = st.Upsert(context.Background(), n)
			_, _ = st.Get(context.Background(), n.ID)
			_, _ = st.List(context.Background(), ListFilter{})
		}(i)
	}
	wg.Wait()
}
