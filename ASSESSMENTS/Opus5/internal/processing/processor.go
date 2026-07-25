// Package processing implements what happens to one delivery once a worker
// picks it up: validate, then dispatch, then record the outcome.
//
// It is deliberately the only place that knows the full lifecycle, so the
// state machine can be read top to bottom in a single function.
package processing

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/evercifuentes/notifier/internal/dispatcher"
	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/provider"
	"github.com/evercifuentes/notifier/internal/queue"
	"github.com/evercifuentes/notifier/internal/store"
	"github.com/evercifuentes/notifier/internal/validation"
	"github.com/evercifuentes/notifier/internal/worker"
)

// Failure codes recorded on a delivery when the failure is the service's own
// rather than a provider's or a validator's.
const (
	CodeValidationUnavailable = "validation_unavailable"
	CodeInternal              = "internal_error"
	CodeCancelled             = "cancelled"
)

// Dispatcher is the port used to reach a provider.
type Dispatcher interface {
	Dispatch(ctx context.Context, req dispatcher.Request) (dispatcher.Result, error)
}

// Processor turns a queued job into a completed delivery.
type Processor struct {
	store      store.Store
	validators *validation.Pipeline
	dispatcher Dispatcher
	logger     *slog.Logger
	now        func() time.Time
}

// Compile-time proof that the processor is usable by the worker pool.
var _ worker.Processor = (*Processor)(nil)

// Option customises a Processor.
type Option func(*Processor)

// WithClock replaces the time source.
func WithClock(now func() time.Time) Option {
	return func(p *Processor) { p.now = now }
}

// New builds a Processor.
func New(s store.Store, validators *validation.Pipeline, d Dispatcher, logger *slog.Logger, opts ...Option) *Processor {
	p := &Processor{
		store:      s,
		validators: validators,
		dispatcher: d,
		logger:     logger,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Process runs the delivery lifecycle for one job.
//
// Every exit path writes a terminal state, so a delivery can never be left
// stuck in "sending" because of an unhandled branch.
func (p *Processor) Process(ctx context.Context, job queue.Job) {
	log := p.logger.With(
		slog.String("notification_id", job.NotificationID),
		slog.String("delivery_id", job.DeliveryID),
		slog.String("channel", job.Channel.String()),
	)

	p.update(ctx, log, job, func(d *domain.Delivery) { d.Status = domain.DeliverySending })

	// 1. Validation. The checks are independent and run concurrently; the
	//    first failure cancels the rest.
	err := p.validators.Run(ctx, validation.Subject{
		NotificationID: job.NotificationID,
		DeliveryID:     job.DeliveryID,
		Channel:        job.Channel,
		Recipient:      job.Recipient,
		Message:        job.Message,
		Metadata:       job.Metadata,
	})
	if err != nil {
		p.finishValidationFailure(ctx, log, job, err)
		return
	}

	// 2. Delivery. Retries and circuit breaking live in the dispatcher.
	result, err := p.dispatcher.Dispatch(ctx, dispatcher.Request{
		NotificationID: job.NotificationID,
		DeliveryID:     job.DeliveryID,
		Channel:        job.Channel,
		Recipient:      job.Recipient,
		Message:        job.Message,
		Metadata:       job.Metadata,
	})
	if err != nil {
		code, reason := describe(err)
		log.Warn("delivery failed",
			slog.String("provider", result.Provider),
			slog.Int("attempts", result.Attempts),
			slog.String("failure_code", code),
			slog.String("error", reason))

		p.update(ctx, log, job, func(d *domain.Delivery) {
			p.markTerminal(d, domain.DeliveryFailed)
			d.Provider = result.Provider
			d.Attempts = result.Attempts
			d.FailureCode = code
			d.FailureReason = reason
		})
		return
	}

	log.Info("delivery succeeded",
		slog.String("provider", result.Provider),
		slog.Int("attempts", result.Attempts),
		slog.String("provider_message_id", result.MessageID))

	p.update(ctx, log, job, func(d *domain.Delivery) {
		p.markTerminal(d, domain.DeliveryDelivered)
		d.Provider = result.Provider
		d.Attempts = result.Attempts
		d.ProviderMessageID = result.MessageID
		d.FailureCode = ""
		d.FailureReason = ""
	})
}

// finishValidationFailure separates a genuine rejection, which is permanent,
// from a validator that could not do its job, which is not the notification's
// fault and must be recorded differently.
func (p *Processor) finishValidationFailure(ctx context.Context, log *slog.Logger, job queue.Job, err error) {
	if rejection, ok := validation.AsRejection(err); ok {
		log.Info("delivery rejected during validation",
			slog.String("validator", rejection.Validator),
			slog.String("rejection_code", rejection.Code),
			slog.String("reason", rejection.Reason))

		p.update(ctx, log, job, func(d *domain.Delivery) {
			p.markTerminal(d, domain.DeliveryRejected)
			d.FailureCode = rejection.Code
			d.FailureReason = rejection.Reason
		})
		return
	}

	code, reason := CodeValidationUnavailable, err.Error()
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = CodeCancelled
		reason = "processing was interrupted before the delivery could be validated"
	}
	log.Error("validation could not be completed",
		slog.String("failure_code", code),
		slog.String("error", err.Error()))

	p.update(ctx, log, job, func(d *domain.Delivery) {
		p.markTerminal(d, domain.DeliveryFailed)
		d.FailureCode = code
		d.FailureReason = reason
	})
}

// markTerminal sets a final status and stamps the completion time.
func (p *Processor) markTerminal(d *domain.Delivery, status domain.DeliveryStatus) {
	now := p.now()
	d.Status = status
	d.CompletedAt = &now
}

// update applies a mutation, using a context that survives cancellation.
//
// Recording the outcome must happen even when the caller's context is already
// done — otherwise a shutdown would leave deliveries permanently stuck in
// "sending" with no record of what happened to them.
func (p *Processor) update(ctx context.Context, log *slog.Logger, job queue.Job, mutate func(*domain.Delivery)) {
	writeCtx := context.WithoutCancel(ctx)
	if err := p.store.UpdateDelivery(writeCtx, job.NotificationID, job.DeliveryID, mutate); err != nil {
		log.Error("could not persist delivery state", slog.String("error", err.Error()))
	}
}

// describe extracts a stable code and a readable reason from a dispatch error.
func describe(err error) (code, reason string) {
	var perr *provider.Error
	if errors.As(err, &perr) {
		return perr.Code, perr.Message
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return provider.CodeTimeout, "the provider did not respond before the deadline"
	case errors.Is(err, context.Canceled):
		return CodeCancelled, "delivery was interrupted before it completed"
	default:
		return CodeInternal, err.Error()
	}
}
