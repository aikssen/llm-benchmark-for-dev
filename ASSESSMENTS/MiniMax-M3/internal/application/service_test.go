package application_test

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anomalyco/notification-service/internal/application"
	"github.com/anomalyco/notification-service/internal/application/validation"
	"github.com/anomalyco/notification-service/internal/domain"
	"github.com/anomalyco/notification-service/internal/infrastructure/providers"
	"github.com/anomalyco/notification-service/internal/infrastructure/repository"
)

type recordingProvider struct {
	calls atomic.Int32
	err   error
}

func (r *recordingProvider) Name() string               { return "rec" }
func (r *recordingProvider) Channels() []domain.Channel { return []domain.Channel{domain.ChannelEmail} }
func (r *recordingProvider) Send(_ context.Context, _ *domain.Notification) error {
	r.calls.Add(1)
	return r.err
}

type flakyProvider struct {
	failUntil int32
	calls     atomic.Int32
}

func (f *flakyProvider) Name() string               { return "flaky" }
func (f *flakyProvider) Channels() []domain.Channel { return []domain.Channel{domain.ChannelSMS} }
func (f *flakyProvider) Send(_ context.Context, _ *domain.Notification) error {
	n := f.calls.Add(1)
	if n <= f.failUntil {
		return errors.New("transient")
	}
	return nil
}

type missingProvider struct{}

func (missingProvider) Name() string                                         { return "x" }
func (missingProvider) Channels() []domain.Channel                           { return []domain.Channel{domain.ChannelPush} }
func (missingProvider) Send(_ context.Context, _ *domain.Notification) error { return nil }

func newSvc(t *testing.T, providersToRegister []providers.Provider) (*application.NotificationService, *application.Dispatcher, *repository.MemoryRepository) {
	t.Helper()
	repo := repository.NewMemoryRepository()
	reg := providers.NewRegistry()
	for _, p := range providersToRegister {
		reg.Register(p)
	}
	pipe := validation.NewPipeline(validation.ContentValidator{})
	cfg := application.DefaultDispatcherConfig()
	cfg.WorkerCount = 2
	cfg.QueueSize = 32
	cfg.BaseBackoff = 1 * time.Millisecond
	cfg.MaxBackoff = 5 * time.Millisecond
	cfg.PerAttemptTimeout = 100 * time.Millisecond
	cfg.ShutdownTimeout = 2 * time.Second
	log := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	d := application.NewDispatcher(cfg, repo, reg, pipe, log)
	svc := application.NewNotificationService(repo, d, log)
	d.Start(context.Background())
	return svc, d, repo
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) { w.t.Log(string(p)); return len(p), nil }

func waitForStatus(t *testing.T, repo *repository.MemoryRepository, id string, want domain.Status, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n, err := repo.Get(context.Background(), id)
		if err == nil && n.Status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	n, _ := repo.Get(context.Background(), id)
	t.Fatalf("timeout waiting for status %s; current = %+v", want, n)
}

func TestSubmit_AndDelivery(t *testing.T) {
	p := &recordingProvider{}
	svc, d, repo := newSvc(t, []providers.Provider{p})
	defer d.Shutdown()

	res, err := svc.Submit(context.Background(), []string{"a@b.com"}, domain.ChannelEmail, domain.Message{Subject: "s", Body: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notifications) != 1 {
		t.Fatalf("expected 1, got %d", len(res.Notifications))
	}
	waitForStatus(t, repo, res.Notifications[0].ID, domain.StatusDelivered, 2*time.Second)
	if p.calls.Load() == 0 {
		t.Fatal("provider was not called")
	}
}

func TestSubmit_MultipleRecipients(t *testing.T) {
	p := &recordingProvider{}
	svc, d, repo := newSvc(t, []providers.Provider{p})
	defer d.Shutdown()

	res, err := svc.Submit(context.Background(), []string{"a@b.com", "c@d.com", "e@f.com"}, domain.ChannelEmail, domain.Message{Subject: "s", Body: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notifications) != 3 {
		t.Fatalf("expected 3, got %d", len(res.Notifications))
	}
	for _, n := range res.Notifications {
		waitForStatus(t, repo, n.ID, domain.StatusDelivered, 2*time.Second)
	}
}

func TestRetry_EventuallySucceeds(t *testing.T) {
	p := &flakyProvider{failUntil: 2}
	svc, d, repo := newSvc(t, []providers.Provider{p})
	defer d.Shutdown()
	res, err := svc.Submit(context.Background(), []string{"+14155551234"}, domain.ChannelSMS, domain.Message{Body: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, repo, res.Notifications[0].ID, domain.StatusDelivered, 3*time.Second)
	if got := p.calls.Load(); got < 3 {
		t.Errorf("expected at least 3 calls, got %d", got)
	}
}

func TestRetry_ExhaustsAttempts(t *testing.T) {
	p := &recordingProvider{err: errors.New("boom")}
	svc, d, repo := newSvc(t, []providers.Provider{p})
	defer d.Shutdown()
	res, err := svc.Submit(context.Background(), []string{"a@b.com"}, domain.ChannelEmail, domain.Message{Subject: "s", Body: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, repo, res.Notifications[0].ID, domain.StatusFailed, 3*time.Second)
}

func TestValidation_Rejection(t *testing.T) {
	repo := repository.NewMemoryRepository()
	reg := providers.NewRegistry()
	reg.Register(&recordingProvider{})
	pipe := validation.NewPipeline(validation.ContentValidator{})
	cfg := application.DefaultDispatcherConfig()
	cfg.WorkerCount = 1
	cfg.BaseBackoff = time.Millisecond
	log := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	d := application.NewDispatcher(cfg, repo, reg, pipe, log)
	svc := application.NewNotificationService(repo, d, log)
	d.Start(context.Background())
	defer d.Shutdown()

	res, err := svc.Submit(context.Background(), []string{"not-an-email"}, domain.ChannelEmail, domain.Message{Subject: "s", Body: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, repo, res.Notifications[0].ID, domain.StatusRejected, 2*time.Second)
}

func TestNoProvider_Fails(t *testing.T) {
	repo := repository.NewMemoryRepository()
	reg := providers.NewRegistry()
	pipe := validation.NewPipeline(validation.ContentValidator{})
	cfg := application.DefaultDispatcherConfig()
	cfg.WorkerCount = 1
	log := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	d := application.NewDispatcher(cfg, repo, reg, pipe, log)
	svc := application.NewNotificationService(repo, d, log)
	d.Start(context.Background())
	defer d.Shutdown()

	res, err := svc.Submit(context.Background(), []string{"a@b.com"}, domain.ChannelEmail, domain.Message{Subject: "s", Body: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, repo, res.Notifications[0].ID, domain.StatusFailed, 2*time.Second)
}

func TestListAndGet(t *testing.T) {
	svc, d, _ := newSvc(t, []providers.Provider{&recordingProvider{}})
	defer d.Shutdown()
	_, err := svc.Submit(context.Background(), []string{"a@b.com", "c@d.com"}, domain.ChannelEmail, domain.Message{Subject: "s", Body: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := svc.List(context.Background(), repository.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2, got %d", len(items))
	}
	if _, err := svc.Get(context.Background(), "missing"); err != domain.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSubmit_EmptyRecipients(t *testing.T) {
	svc, d, _ := newSvc(t, []providers.Provider{&recordingProvider{}})
	defer d.Shutdown()
	_, err := svc.Submit(context.Background(), nil, domain.ChannelEmail, domain.Message{Subject: "s", Body: "hi"})
	if err == nil {
		t.Fatal("expected error")
	}
}
