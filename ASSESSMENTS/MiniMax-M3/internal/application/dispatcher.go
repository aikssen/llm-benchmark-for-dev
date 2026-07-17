package application

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/anomalyco/notification-service/internal/domain"
	"github.com/anomalyco/notification-service/internal/infrastructure/providers"
	"github.com/anomalyco/notification-service/internal/infrastructure/repository"
)

type ValidationPipeline interface {
	Run(ctx context.Context, n *domain.Notification) error
	RunParallel(ctx context.Context, n *domain.Notification) error
}

type DispatcherConfig struct {
	WorkerCount       int
	QueueSize         int
	MaxAttempts       int
	BaseBackoff       time.Duration
	MaxBackoff        time.Duration
	PerAttemptTimeout time.Duration
	ShutdownTimeout   time.Duration
}

func DefaultDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{
		WorkerCount:       8,
		QueueSize:         256,
		MaxAttempts:       4,
		BaseBackoff:       100 * time.Millisecond,
		MaxBackoff:        2 * time.Second,
		PerAttemptTimeout: 5 * time.Second,
		ShutdownTimeout:   10 * time.Second,
	}
}

type Dispatcher struct {
	cfg       DispatcherConfig
	repo      repository.NotificationRepository
	registry  *providers.Registry
	pipeline  ValidationPipeline
	log       *slog.Logger
	jitterRnd *rand.Rand
	jitterMu  sync.Mutex

	queue chan string
	wg    sync.WaitGroup
}

func NewDispatcher(
	cfg DispatcherConfig,
	repo repository.NotificationRepository,
	registry *providers.Registry,
	pipeline ValidationPipeline,
	log *slog.Logger,
) *Dispatcher {
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 1
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 64
	}
	return &Dispatcher{
		cfg:       cfg,
		repo:      repo,
		registry:  registry,
		pipeline:  pipeline,
		log:       log,
		jitterRnd: rand.New(rand.NewSource(time.Now().UnixNano())),
		queue:     make(chan string, cfg.QueueSize),
	}
}

func (d *Dispatcher) Start(ctx context.Context) {
	for i := 0; i < d.cfg.WorkerCount; i++ {
		d.wg.Add(1)
		go d.worker(ctx, i)
	}
}

func (d *Dispatcher) Shutdown() {
	close(d.queue)
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d.cfg.ShutdownTimeout):
		d.log.Warn("dispatcher shutdown timeout exceeded, exiting")
	}
}

func (d *Dispatcher) Enqueue(ctx context.Context, id string) error {
	select {
	case d.queue <- id:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errors.New("dispatcher queue is full")
	}
}

func (d *Dispatcher) worker(ctx context.Context, id int) {
	defer d.wg.Done()
	log := d.log.With("worker", id)
	for {
		select {
		case <-ctx.Done():
			d.drainQueue(log)
			return
		case notifID, ok := <-d.queue:
			if !ok {
				return
			}
			d.process(ctx, log, notifID)
		}
	}
}

func (d *Dispatcher) drainQueue(log *slog.Logger) {
	for notifID := range d.queue {
		d.process(context.Background(), log, notifID)
	}
}

func (d *Dispatcher) process(parent context.Context, log *slog.Logger, id string) {
	n, err := d.repo.Get(parent, id)
	if err != nil {
		log.Warn("get failed", "id", id, "err", err)
		return
	}
	if n.Status.Terminal() {
		return
	}

	now := time.Now().UTC()
	n.MarkStatus(domain.StatusValidating, now)
	_ = d.repo.Update(parent, n)

	if err := d.pipeline.Run(parent, n); err != nil {
		n.MarkStatus(domain.StatusRejected, time.Now().UTC())
		n.Reason = err.Error()
		_ = d.repo.Update(parent, n)
		log.Info("notification rejected", "id", id, "reason", err.Error())
		return
	}
	n.MarkStatus(domain.StatusValidated, time.Now().UTC())
	_ = d.repo.Update(parent, n)

	if err := d.dispatchWithRetry(parent, n, log); err != nil {
		n.MarkStatus(domain.StatusFailed, time.Now().UTC())
		n.Reason = err.Error()
		_ = d.repo.Update(parent, n)
		log.Warn("notification delivery failed permanently", "id", id, "err", err)
		return
	}
	n.MarkStatus(domain.StatusDelivered, time.Now().UTC())
	_ = d.repo.Update(parent, n)
	log.Info("notification delivered", "id", id, "channel", n.Channel, "attempts", n.Attempts)
}

func (d *Dispatcher) dispatchWithRetry(parent context.Context, n *domain.Notification, log *slog.Logger) error {
	provider, ok := d.registry.Get(n.Channel)
	if !ok {
		return domain.ErrUnknownChannel
	}
	var lastErr error
	for attempt := 1; attempt <= d.cfg.MaxAttempts; attempt++ {
		n.MarkStatus(domain.StatusSending, time.Now().UTC())
		_ = d.repo.Update(parent, n)

		ctx, cancel := context.WithTimeout(parent, d.cfg.PerAttemptTimeout)
		err := provider.Send(ctx, n)
		cancel()

		if err == nil {
			n.RecordAttemptSuccess(time.Now().UTC())
			_ = d.repo.Update(parent, n)
			return nil
		}
		lastErr = err
		n.RecordFailure(err, time.Now().UTC())
		_ = d.repo.Update(parent, n)

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			log.Warn("send attempt timed out", "id", n.ID, "attempt", attempt)
		} else {
			log.Warn("send attempt failed", "id", n.ID, "attempt", attempt, "err", err)
		}

		if attempt == d.cfg.MaxAttempts {
			break
		}
		backoff := d.backoff(attempt)
		select {
		case <-time.After(backoff):
		case <-parent.Done():
			return parent.Err()
		}
	}
	return lastErr
}

func (d *Dispatcher) backoff(attempt int) time.Duration {
	base := float64(d.cfg.BaseBackoff) * math.Pow(2, float64(attempt-1))
	if base > float64(d.cfg.MaxBackoff) {
		base = float64(d.cfg.MaxBackoff)
	}
	d.jitterMu.Lock()
	jitter := d.jitterRnd.Float64() * base * 0.2
	d.jitterMu.Unlock()
	return time.Duration(base + jitter)
}
