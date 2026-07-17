package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/anomalyco/notification-service/internal/domain"
	"github.com/anomalyco/notification-service/internal/infrastructure/repository"
)

func newNotif(id, recipient string, ch domain.Channel) *domain.Notification {
	return domain.NewNotification(id, recipient, ch, domain.Message{Subject: "s", Body: "b"}, time.Now().UTC())
}

func TestMemoryRepository_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryRepository()
	if err := repo.Create(ctx, newNotif("a", "u@example.com", domain.ChannelEmail)); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Create(ctx, newNotif("a", "x@y.com", domain.ChannelEmail)); err == nil {
		t.Fatal("expected duplicate id error")
	}
	got, err := repo.Get(ctx, "a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Recipient != "u@example.com" {
		t.Errorf("recipient = %q", got.Recipient)
	}
	if _, err := repo.Get(ctx, "missing"); err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryRepository_Update(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryRepository()
	_ = repo.Create(ctx, newNotif("a", "u@example.com", domain.ChannelEmail))
	got, _ := repo.Get(ctx, "a")
	got.Status = domain.StatusDelivered
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, _ := repo.Get(ctx, "a")
	if got2.Status != domain.StatusDelivered {
		t.Errorf("status = %q", got2.Status)
	}
}

func TestMemoryRepository_ListFilters(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryRepository()
	must(t, repo.Create(ctx, newNotif("1", "a@x", domain.ChannelEmail)))
	must(t, repo.Create(ctx, newNotif("2", "b@x", domain.ChannelSMS)))
	must(t, repo.Create(ctx, newNotif("3", "c@x", domain.ChannelPush)))
	must(t, repo.Create(ctx, newNotif("4", "d@x", domain.ChannelEmail)))

	emails, err := repo.List(ctx, repository.ListFilter{Channel: domain.ChannelEmail})
	if err != nil {
		t.Fatal(err)
	}
	if len(emails) != 2 {
		t.Errorf("expected 2 emails, got %d", len(emails))
	}
	page, err := repo.List(ctx, repository.ListFilter{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Errorf("expected 2 with offset 1, got %d", len(page))
	}
	none, err := repo.List(ctx, repository.ListFilter{Offset: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("expected empty, got %d", len(none))
	}
}

func TestMemoryRepository_SweepOlderThan(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewMemoryRepository()
	now := time.Now().UTC()
	_ = repo.Create(ctx, newNotif("1", "a@x", domain.ChannelEmail))
	got, _ := repo.Get(ctx, "1")
	got.Status = domain.StatusDelivered
	got.UpdatedAt = now.Add(-2 * time.Hour)
	_ = repo.Update(ctx, got)

	_ = repo.Create(ctx, newNotif("2", "b@x", domain.ChannelEmail))
	got2, _ := repo.Get(ctx, "2")
	got2.Status = domain.StatusDelivered
	got2.UpdatedAt = now
	_ = repo.Update(ctx, got2)

	removed := repo.SweepOlderThan(ctx, time.Hour, now)
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	if repo.Len() != 1 {
		t.Errorf("expected 1 remaining, got %d", repo.Len())
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
