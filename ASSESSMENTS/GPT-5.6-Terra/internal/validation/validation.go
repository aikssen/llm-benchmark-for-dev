package validation

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/example/notification-orchestrator/internal/provider"
)

type Validator interface {
	Name() string
	Validate(context.Context, provider.Payload) error
}

// Pipeline runs independent validations with bounded parallelism. The first failure
// cancels shared work; validators that touch I/O must honor context cancellation.
type Pipeline struct {
	validators []Validator
	workers    int
}

func NewPipeline(workers int, validators ...Validator) *Pipeline {
	if workers < 1 {
		workers = 1
	}
	return &Pipeline{validators: validators, workers: workers}
}

func (p *Pipeline) Validate(ctx context.Context, payload provider.Payload) error {
	if len(p.validators) == 0 {
		return nil
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan Validator)
	var group sync.WaitGroup
	var once sync.Once
	var firstError error
	report := func(validator Validator, err error) {
		if err == nil {
			return
		}
		once.Do(func() {
			firstError = fmt.Errorf("%s: %w", validator.Name(), err)
			cancel()
		})
	}
	worker := func() {
		defer group.Done()
		for validator := range jobs {
			if workCtx.Err() != nil {
				return
			}
			report(validator, validator.Validate(workCtx, payload))
		}
	}
	workerCount := p.workers
	if workerCount > len(p.validators) {
		workerCount = len(p.validators)
	}
	group.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go worker()
	}
	for _, validator := range p.validators {
		select {
		case jobs <- validator:
		case <-workCtx.Done():
			close(jobs)
			group.Wait()
			if firstError != nil {
				return firstError
			}
			return ctx.Err()
		}
	}
	close(jobs)
	group.Wait()
	if firstError != nil {
		return firstError
	}
	return ctx.Err()
}

type RecipientFormat struct{}

func (RecipientFormat) Name() string { return "recipient_format" }

var e164 = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

func (RecipientFormat) Validate(_ context.Context, payload provider.Payload) error {
	recipient := strings.TrimSpace(payload.Recipient)
	switch payload.Channel {
	case "email":
		address, err := mail.ParseAddress(recipient)
		if err != nil || address.Address != recipient {
			return fmt.Errorf("invalid email recipient")
		}
	case "sms":
		if !e164.MatchString(recipient) {
			return fmt.Errorf("SMS recipients must use E.164 format")
		}
	case "push":
		if recipient == "" || utf8.RuneCountInString(recipient) > 256 {
			return fmt.Errorf("invalid push token")
		}
	default:
		return fmt.Errorf("unsupported channel")
	}
	return nil
}

type MessageContent struct{ MaxBytes int }

func (MessageContent) Name() string { return "message_content" }

func (v MessageContent) Validate(_ context.Context, payload provider.Payload) error {
	if strings.TrimSpace(payload.Message) == "" {
		return fmt.Errorf("message must not be empty")
	}
	if len(payload.Message) > v.MaxBytes {
		return fmt.Errorf("message exceeds %d bytes", v.MaxBytes)
	}
	return nil
}

// These defaults make policy dependencies explicit. Production can replace either
// validator with database/API-backed implementations without changing the dispatcher.
type AllowAllPreferences struct{}

func (AllowAllPreferences) Name() string                                     { return "preferences" }
func (AllowAllPreferences) Validate(context.Context, provider.Payload) error { return nil }

type AllowAllRateLimit struct{}

func (AllowAllRateLimit) Name() string                                     { return "rate_limit" }
func (AllowAllRateLimit) Validate(context.Context, provider.Payload) error { return nil }
