package validation_test

import (
	"context"
	"testing"
	"time"

	"github.com/anomalyco/notification-service/internal/application/validation"
	"github.com/anomalyco/notification-service/internal/domain"
)

type stubVal struct {
	name string
	err  error
}

func (s stubVal) Name() string                                             { return s.name }
func (s stubVal) Validate(_ context.Context, _ *domain.Notification) error { return s.err }

func newNotif(recipient string, ch domain.Channel) *domain.Notification {
	return domain.NewNotification("i", recipient, ch, domain.Message{Subject: "s", Body: "b"}, time.Now().UTC())
}

func TestPipeline_StopsOnFirstError(t *testing.T) {
	p := validation.NewPipeline(
		stubVal{name: "a", err: nil},
		stubVal{name: "b", err: domain.NewValidationError("x", "boom")},
		stubVal{name: "c", err: nil},
	)
	err := p.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPipeline_RunParallel_CancelsOnError(t *testing.T) {
	p := validation.NewPipeline(
		stubVal{name: "a", err: domain.NewValidationError("x", "boom")},
		stubVal{name: "b", err: nil},
	)
	if err := p.RunParallel(context.Background(), nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestContentValidator(t *testing.T) {
	v := validation.ContentValidator{}
	if err := v.Validate(context.Background(), newNotif("bad", domain.ChannelEmail)); err == nil {
		t.Error("expected error for bad email")
	}
	if err := v.Validate(context.Background(), newNotif("a@b.com", domain.ChannelEmail)); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTokenBucket_AllowsThenLimits(t *testing.T) {
	tb := validation.NewTokenBucket(2, 0.01)
	ctx := context.Background()
	if ok, _ := tb.Allow(ctx, "k"); !ok {
		t.Fatal("expected first to pass")
	}
	if ok, _ := tb.Allow(ctx, "k"); !ok {
		t.Fatal("expected second to pass")
	}
	if ok, _ := tb.Allow(ctx, "k"); ok {
		t.Fatal("expected third to be denied")
	}
	if ok, _ := tb.Allow(ctx, "other"); !ok {
		t.Fatal("expected other key to pass")
	}
}

func TestPreferenceValidator(t *testing.T) {
	store := validation.NewMemoryPreferenceStore()
	store.OptOut("u1", domain.ChannelEmail)
	v := validation.NewPreferenceValidator(store)
	if err := v.Validate(context.Background(), newNotif("u1", domain.ChannelEmail)); err == nil {
		t.Fatal("expected opted out error")
	}
	if err := v.Validate(context.Background(), newNotif("u2", domain.ChannelEmail)); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
