package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/example/notification-orchestrator/internal/domain"
	"github.com/example/notification-orchestrator/internal/provider"
	"github.com/example/notification-orchestrator/internal/store"
	"github.com/example/notification-orchestrator/internal/validation"
)

var (
	ErrQueueFull      = errors.New("notification queue is full")
	ErrStopped        = errors.New("notification dispatcher is stopping")
	ErrInvalidRequest = errors.New("invalid notification request")
)

type Config struct {
	QueueCapacity       int
	NotificationWorkers int
	RecipientWorkers    int
	MaxRecipients       int
	MaxAttempts         int
	AttemptTimeout      time.Duration
	RetryBaseDelay      time.Duration
}

type CreateRequest struct {
	Recipients []string
	Channel    domain.Channel
	Message    string
}

type Service struct {
	repository store.Repository
	providers  *provider.Registry
	validators *validation.Pipeline
	config     Config
	logger     *log.Logger

	queue       chan string
	submitMu    sync.Mutex
	stopped     bool
	startOnce   sync.Once
	stopOnce    sync.Once
	workerGroup sync.WaitGroup
}

func New(repository store.Repository, providers *provider.Registry, validators *validation.Pipeline, config Config, logger *log.Logger) *Service {
	return &Service{
		repository: repository,
		providers:  providers,
		validators: validators,
		config:     config,
		logger:     logger,
		queue:      make(chan string, config.QueueCapacity),
	}
}

func (s *Service) Start() {
	s.startOnce.Do(func() {
		for i := 0; i < s.config.NotificationWorkers; i++ {
			s.workerGroup.Add(1)
			go s.worker()
		}
	})
}

func (s *Service) Submit(request CreateRequest, idempotencyKey string) (*domain.Notification, bool, error) {
	if err := s.validateRequest(request); err != nil {
		return nil, false, err
	}

	s.submitMu.Lock()
	defer s.submitMu.Unlock()
	if s.stopped {
		return nil, false, ErrStopped
	}
	candidate := newNotification(request, idempotencyKey)
	if existing, ok := s.repository.FindByIdempotency(idempotencyKey); ok {
		if sameRequest(existing, candidate) {
			return existing, false, nil
		}
		return nil, false, store.ErrIdempotencyConflict
	}

	// The API handler is the only queue producer and submitMu serializes it. A
	// failed capacity check therefore cannot race another enqueue and, critically,
	// avoids persisting a notification that this in-memory implementation cannot run.
	if len(s.queue) == cap(s.queue) {
		return nil, false, ErrQueueFull
	}

	notification, created, err := s.repository.CreateOrGet(candidate)
	if err != nil || !created {
		return notification, created, err
	}

	s.queue <- notification.ID
	return notification, true, nil
}

func (s *Service) Get(id string) (*domain.Notification, error) {
	return s.repository.Get(id)
}

func (s *Service) List(options store.ListOptions) store.Page {
	return s.repository.List(options)
}

func (s *Service) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.submitMu.Lock()
		s.stopped = true
		close(s.queue)
		s.submitMu.Unlock()
	})
	done := make(chan struct{})
	go func() {
		s.workerGroup.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) worker() {
	defer s.workerGroup.Done()
	for notificationID := range s.queue {
		notification, err := s.repository.Get(notificationID)
		if err != nil {
			s.logger.Printf("queued notification %s disappeared: %v", notificationID, err)
			continue
		}
		s.processNotification(notification)
	}
}

func (s *Service) processNotification(notification *domain.Notification) {
	jobs := make(chan domain.Delivery)
	var group sync.WaitGroup
	workers := s.config.RecipientWorkers
	if workers > len(notification.Deliveries) {
		workers = len(notification.Deliveries)
	}
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for delivery := range jobs {
				s.processDelivery(notification, delivery)
			}
		}()
	}
	for _, delivery := range notification.Deliveries {
		jobs <- delivery
	}
	close(jobs)
	group.Wait()
}

func (s *Service) processDelivery(notification *domain.Notification, delivery domain.Delivery) {
	_, _ = s.repository.UpdateDelivery(notification.ID, delivery.ID, func(current *domain.Delivery) {
		current.Status = domain.DeliveryProcessing
		current.LastError = ""
	})
	payload := provider.Payload{
		NotificationID: notification.ID,
		DeliveryID:     delivery.ID,
		Recipient:      delivery.Recipient,
		Channel:        notification.Channel,
		Message:        notification.Message,
	}
	if err := s.validators.Validate(context.Background(), payload); err != nil {
		s.failDelivery(notification.ID, delivery.ID, err)
		return
	}

	sender, err := s.providers.For(notification.Channel)
	if err != nil {
		s.failDelivery(notification.ID, delivery.ID, err)
		return
	}

	for attempt := 1; attempt <= s.config.MaxAttempts; attempt++ {
		_, _ = s.repository.UpdateDelivery(notification.ID, delivery.ID, func(current *domain.Delivery) {
			current.Attempts = attempt
		})
		attemptCtx, cancel := context.WithTimeout(context.Background(), s.config.AttemptTimeout)
		err = sender.Send(attemptCtx, payload)
		cancel()
		if err == nil {
			_, _ = s.repository.UpdateDelivery(notification.ID, delivery.ID, func(current *domain.Delivery) {
				current.Status = domain.DeliveryDelivered
				current.LastError = ""
			})
			return
		}
		if provider.IsPermanent(err) || attempt == s.config.MaxAttempts {
			s.failDelivery(notification.ID, delivery.ID, err)
			return
		}
		if !sleep(context.Background(), retryDelay(s.config.RetryBaseDelay, attempt)) {
			s.failDelivery(notification.ID, delivery.ID, context.Canceled)
			return
		}
	}
}

func (s *Service) failDelivery(notificationID, deliveryID string, err error) {
	_, updateErr := s.repository.UpdateDelivery(notificationID, deliveryID, func(current *domain.Delivery) {
		current.Status = domain.DeliveryFailed
		current.LastError = safeError(err)
	})
	if updateErr != nil {
		s.logger.Printf("could not mark delivery %s as failed: %v", deliveryID, updateErr)
	}
}

func (s *Service) validateRequest(request CreateRequest) error {
	if !request.Channel.Valid() || len(request.Recipients) == 0 || len(request.Recipients) > s.config.MaxRecipients || strings.TrimSpace(request.Message) == "" {
		return ErrInvalidRequest
	}
	for _, recipient := range request.Recipients {
		if strings.TrimSpace(recipient) == "" {
			return ErrInvalidRequest
		}
	}
	return nil
}

func newNotification(request CreateRequest, idempotencyKey string) *domain.Notification {
	now := time.Now().UTC()
	notification := &domain.Notification{
		ID:             newID(),
		IdempotencyKey: idempotencyKey,
		Channel:        request.Channel,
		Message:        request.Message,
		Status:         domain.StatusQueued,
		CreatedAt:      now,
		UpdatedAt:      now,
		Deliveries:     make([]domain.Delivery, len(request.Recipients)),
	}
	for i, recipient := range request.Recipients {
		notification.Deliveries[i] = domain.Delivery{ID: newID(), Recipient: recipient, Status: domain.DeliveryQueued}
	}
	return notification
}

func sameRequest(notification *domain.Notification, candidate *domain.Notification) bool {
	if notification.Channel != candidate.Channel || notification.Message != candidate.Message || len(notification.Deliveries) != len(candidate.Deliveries) {
		return false
	}
	for index := range notification.Deliveries {
		if notification.Deliveries[index].Recipient != candidate.Deliveries[index].Recipient {
			return false
		}
	}
	return true
}

func newID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return hex.EncodeToString(bytes)
}

func retryDelay(base time.Duration, attempt int) time.Duration {
	return base * time.Duration(1<<(attempt-1))
}

func sleep(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func safeError(err error) string {
	const maximumLength = 500
	message := err.Error()
	if len(message) > maximumLength {
		return message[:maximumLength]
	}
	return message
}
