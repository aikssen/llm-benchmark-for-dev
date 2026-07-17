package processing

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/example/notification-service/internal/channel"
	"github.com/example/notification-service/internal/domain"
	"github.com/example/notification-service/internal/provider"
	"github.com/example/notification-service/internal/storage"
	"github.com/example/notification-service/internal/validation"
)

// Config configures the processor.
type Config struct {
	Workers              int // queue workers
	RecipientConcurrency int // max recipients processed in parallel per notification
}

// Processor drains the Queue with a fixed worker pool and drives the
// validation + delivery pipeline for each notification.
type Processor struct {
	cfg      Config
	queue    *Queue
	repo     storage.Repository
	channels *channel.Registry
	pipeline *validation.Pipeline
	log      *slog.Logger

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// New builds a Processor.
func New(cfg Config, queue *Queue, repo storage.Repository, channels *channel.Registry, pipeline *validation.Pipeline, log *slog.Logger) *Processor {
	return &Processor{
		cfg:      cfg,
		queue:    queue,
		repo:     repo,
		channels: channels,
		pipeline: pipeline,
		log:      log,
	}
}

// Start launches the worker pool. It returns immediately.
func (p *Processor) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	for i := 0; i < p.cfg.Workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
}

// Stop signals workers to stop and waits for them to finish.
func (p *Processor) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	p.queue.Close()
	p.wg.Wait()
}

func (p *Processor) worker(ctx context.Context, idx int) {
	defer p.wg.Done()
	p.log.Debug("worker started", "worker", idx)
	for {
		id, ok := p.queue.Dequeue(ctx)
		if !ok {
			p.log.Debug("worker stopped", "worker", idx)
			return
		}
		if err := p.process(ctx, id); err != nil {
			p.log.Error("process notification failed", "id", id, "err", err)
		}
	}
}

// process runs the full pipeline for one notification.
func (p *Processor) process(ctx context.Context, id domain.ID) error {
	n, err := p.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if n.Status.IsTerminal() {
		return nil
	}

	ch, err := p.channels.Get(n.Channel)
	if err != nil {
		// Unsupported channel: mark every recipient as failed.
		p.failAll(ctx, n, "unsupported channel")
		return nil
	}

	if err := p.repo.SetStatus(ctx, n.ID, domain.StatusProcessing); err != nil {
		return err
	}

	sem := make(chan struct{}, p.cfg.RecipientConcurrency)
	var wg sync.WaitGroup

	for i := range n.Recipients {
		r := n.Recipients[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			p.processRecipient(ctx, n, ch, r)
		}()
	}
	wg.Wait()

	// Reload to compute aggregate status from the persisted results.
	updated, err := p.repo.Get(ctx, n.ID)
	if err != nil {
		return err
	}
	aggregate := domain.AggregateStatus(updated.Results)
	return p.repo.SetStatus(ctx, n.ID, aggregate)
}

// processRecipient validates then delivers to a single recipient.
func (p *Processor) processRecipient(ctx context.Context, n *domain.Notification, ch channel.Channel, r domain.Recipient) {
	now := time.Now().UTC()

	verrs := p.pipeline.Run(ctx, validation.Input{Notification: n, Recipient: r})
	if len(verrs) > 0 {
		result := domain.RecipientResult{
			RecipientID:      r.ID,
			Status:           domain.StatusValidationFailed,
			ValidationErrors: verrs,
			UpdatedAt:        time.Now().UTC(),
		}
		if err := p.repo.UpsertResult(ctx, n.ID, result); err != nil {
			p.log.Error("persist validation result failed", "id", n.ID, "recipient", r.ID, "err", err)
		}
		return
	}

	// Announce sending.
	_ = p.repo.UpsertResult(ctx, n.ID, domain.RecipientResult{
		RecipientID: r.ID,
		Status:      domain.StatusSending,
		UpdatedAt:   now,
	})

	recorder := &provider.AttemptRecorder{}
	sendCtx := provider.WithAttemptRecorder(ctx, recorder)
	err := ch.Send(sendCtx, n, r)

	result := domain.RecipientResult{
		RecipientID: r.ID,
		Attempts:    recorder.Count(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err == nil {
		result.Status = domain.StatusDelivered
	} else {
		result.Status = domain.StatusFailed
		result.LastError = err.Error()
		// A circuit-open error is a transient infrastructure condition; we
		// surface it but the notification is marked failed for this attempt.
		if errors.Is(err, provider.ErrCircuitOpen) {
			p.log.Warn("delivery skipped: circuit open", "id", n.ID, "recipient", r.ID, "channel", n.Channel)
		}
	}
	if err := p.repo.UpsertResult(ctx, n.ID, result); err != nil {
		p.log.Error("persist delivery result failed", "id", n.ID, "recipient", r.ID, "err", err)
	}
}

func (p *Processor) failAll(ctx context.Context, n *domain.Notification, reason string) {
	for _, r := range n.Recipients {
		_ = p.repo.UpsertResult(ctx, n.ID, domain.RecipientResult{
			RecipientID: r.ID,
			Status:      domain.StatusFailed,
			LastError:   reason,
			UpdatedAt:   time.Now().UTC(),
		})
	}
	_ = p.repo.SetStatus(ctx, n.ID, domain.AggregateStatus(nil))
}
