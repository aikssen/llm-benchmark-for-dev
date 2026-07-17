package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"notification/internal/notification"
)

var (
	ErrQueueFull     = errors.New("notification queue is full")
	ErrServiceClosed = errors.New("notification service is shutting down")
)

type InvalidInputError struct {
	Field   string
	Message string
}

func (e InvalidInputError) Error() string { return fmt.Sprintf("%s: %s", e.Field, e.Message) }

type Submission struct {
	Recipients     []string
	Channel        notification.Channel
	Message        notification.Message
	IdempotencyKey string
}

type Config struct {
	Workers       int
	QueueCapacity int
	MaxRecipients int
	MaxBodyBytes  int
}

type Service struct {
	repository notification.Repository
	processor  *Processor
	config     Config
	jobs       chan Job

	submitMu sync.Mutex
	closed   bool
	stopOnce sync.Once
	workers  sync.WaitGroup
	done     chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	now      func() time.Time
	id       func() (string, error)
}

func New(repository notification.Repository, processor *Processor, config Config) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		repository: repository,
		processor:  processor,
		config:     config,
		jobs:       make(chan Job, config.QueueCapacity),
		done:       make(chan struct{}),
		ctx:        ctx,
		cancel:     cancel,
		now:        time.Now,
		id:         newID,
	}
	for range config.Workers {
		s.workers.Add(1)
		go s.work()
	}
	return s
}

func (s *Service) Submit(ctx context.Context, input Submission) (notification.Notification, bool, error) {
	normalized, err := s.validate(input)
	if err != nil {
		return notification.Notification{}, false, err
	}
	fingerprint := requestFingerprint(normalized)

	s.submitMu.Lock()
	defer s.submitMu.Unlock()
	if s.closed {
		return notification.Notification{}, false, ErrServiceClosed
	}

	if normalized.IdempotencyKey != "" {
		existing, lookupErr := s.repository.GetByIdempotencyKey(ctx, normalized.IdempotencyKey, fingerprint)
		if lookupErr == nil {
			return existing, true, nil
		}
		if !errors.Is(lookupErr, notification.ErrNotFound) {
			return notification.Notification{}, false, lookupErr
		}
	}

	if cap(s.jobs)-len(s.jobs) < len(normalized.Recipients) {
		return notification.Notification{}, false, ErrQueueFull
	}

	id, err := s.id()
	if err != nil {
		return notification.Notification{}, false, fmt.Errorf("generate notification id: %w", err)
	}
	now := s.now().UTC()
	value := notification.Notification{
		ID:         id,
		Channel:    normalized.Channel,
		Message:    normalized.Message,
		Status:     notification.StatusQueued,
		CreatedAt:  now,
		UpdatedAt:  now,
		Deliveries: make([]notification.Delivery, len(normalized.Recipients)),
	}
	for index, recipient := range normalized.Recipients {
		value.Deliveries[index] = notification.Delivery{
			Recipient: recipient,
			Status:    notification.DeliveryQueued,
			UpdatedAt: now,
		}
	}

	createdValue, created, err := s.repository.Create(ctx, value, normalized.IdempotencyKey, fingerprint)
	if err != nil {
		return notification.Notification{}, false, err
	}
	if !created {
		return createdValue, true, nil
	}
	for index := range createdValue.Deliveries {
		s.jobs <- Job{NotificationID: createdValue.ID, DeliveryIndex: index}
	}
	return createdValue, false, nil
}

func (s *Service) Get(ctx context.Context, id string) (notification.Notification, error) {
	return s.repository.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, limit, offset int) (notification.Page, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.repository.List(ctx, limit, offset)
}

func (s *Service) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.submitMu.Lock()
		s.closed = true
		close(s.jobs)
		s.submitMu.Unlock()
		go func() {
			s.workers.Wait()
			close(s.done)
		}()
	})

	select {
	case <-s.done:
		s.cancel()
		return nil
	case <-ctx.Done():
		s.cancel()
		return ctx.Err()
	}
}

func (s *Service) work() {
	defer s.workers.Done()
	for job := range s.jobs {
		s.processor.Process(s.ctx, job)
	}
}

func (s *Service) validate(input Submission) (Submission, error) {
	if !input.Channel.Valid() {
		return Submission{}, InvalidInputError{Field: "channel", Message: "must be one of email, sms, push"}
	}
	if len(input.Recipients) == 0 {
		return Submission{}, InvalidInputError{Field: "recipients", Message: "must contain at least one recipient"}
	}
	if len(input.Recipients) > s.config.MaxRecipients {
		return Submission{}, InvalidInputError{Field: "recipients", Message: fmt.Sprintf("must contain at most %d recipients", s.config.MaxRecipients)}
	}
	if strings.TrimSpace(input.Message.Body) == "" {
		return Submission{}, InvalidInputError{Field: "message.body", Message: "must not be empty"}
	}
	if len([]byte(input.Message.Body)) > s.config.MaxBodyBytes {
		return Submission{}, InvalidInputError{Field: "message.body", Message: fmt.Sprintf("must be at most %d bytes", s.config.MaxBodyBytes)}
	}
	if len(input.IdempotencyKey) > 128 {
		return Submission{}, InvalidInputError{Field: "Idempotency-Key", Message: "must be at most 128 characters"}
	}

	seen := make(map[string]struct{}, len(input.Recipients))
	for index, recipient := range input.Recipients {
		recipient = strings.TrimSpace(recipient)
		if recipient == "" {
			return Submission{}, InvalidInputError{Field: fmt.Sprintf("recipients[%d]", index), Message: "must not be empty"}
		}
		if _, exists := seen[recipient]; exists {
			return Submission{}, InvalidInputError{Field: fmt.Sprintf("recipients[%d]", index), Message: "must not contain duplicates"}
		}
		seen[recipient] = struct{}{}
		input.Recipients[index] = recipient
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	return input, nil
}

func requestFingerprint(input Submission) string {
	payload, _ := json.Marshal(struct {
		Recipients []string             `json:"recipients"`
		Channel    notification.Channel `json:"channel"`
		Message    notification.Message `json:"message"`
	}{input.Recipients, input.Channel, input.Message})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
