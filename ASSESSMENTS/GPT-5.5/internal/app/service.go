package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"notification-service/internal/domain"
	"notification-service/internal/provider"
	"notification-service/internal/store"
)

var (
	ErrInvalidRequest     = errors.New("invalid notification request")
	ErrUnsupportedChannel = errors.New("unsupported notification channel")
)

type Store interface {
	Save(ctx context.Context, notification domain.Notification) error
	Get(ctx context.Context, id string) (domain.Notification, error)
	List(ctx context.Context, filter store.ListFilter) ([]domain.Notification, error)
	Update(ctx context.Context, id string, update func(*domain.Notification)) (domain.Notification, error)
}

type Queue interface {
	Enqueue(ctx context.Context, notificationID string) error
}

type BatchQueue interface {
	EnqueueBatch(ctx context.Context, notificationIDs []string) error
}

type Service struct {
	store     Store
	queue     Queue
	providers *provider.Registry
}

func NewService(store Store, queue Queue, providers *provider.Registry) *Service {
	return &Service{store: store, queue: queue, providers: providers}
}

type SubmitCommand struct {
	Recipients []string
	Channel    domain.Channel
	Message    domain.Message
}

type SubmitResult struct {
	RequestID     string
	Notifications []domain.Notification
}

func (s *Service) Submit(ctx context.Context, cmd SubmitCommand) (SubmitResult, error) {
	if err := s.validateSubmitCommand(cmd); err != nil {
		return SubmitResult{}, err
	}

	now := time.Now().UTC()
	requestID, err := newID("req")
	if err != nil {
		return SubmitResult{}, err
	}

	notifications := make([]domain.Notification, 0, len(cmd.Recipients))
	notificationIDs := make([]string, 0, len(cmd.Recipients))
	for _, recipient := range cmd.Recipients {
		notificationID, err := newID("ntf")
		if err != nil {
			return SubmitResult{}, err
		}

		notification := domain.Notification{
			ID:        notificationID,
			RequestID: requestID,
			Recipient: strings.TrimSpace(recipient),
			Channel:   cmd.Channel,
			Message:   cloneMessage(cmd.Message),
			Status:    domain.StatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := s.store.Save(ctx, notification); err != nil {
			return SubmitResult{}, err
		}

		notifications = append(notifications, notification)
		notificationIDs = append(notificationIDs, notification.ID)
	}

	if err := s.enqueue(ctx, notificationIDs); err != nil {
		for _, notificationID := range notificationIDs {
			_, _ = s.store.Update(context.Background(), notificationID, func(n *domain.Notification) {
				n.Status = domain.StatusFailed
				n.FailureReason = err.Error()
			})
		}
		return SubmitResult{}, fmt.Errorf("enqueue notifications: %w", err)
	}

	return SubmitResult{RequestID: requestID, Notifications: notifications}, nil
}

func (s *Service) Get(ctx context.Context, id string) (domain.Notification, error) {
	return s.store.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, filter store.ListFilter) ([]domain.Notification, error) {
	return s.store.List(ctx, filter)
}

func (s *Service) enqueue(ctx context.Context, notificationIDs []string) error {
	if batchQueue, ok := s.queue.(BatchQueue); ok {
		return batchQueue.EnqueueBatch(ctx, notificationIDs)
	}
	for _, notificationID := range notificationIDs {
		if err := s.queue.Enqueue(ctx, notificationID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) validateSubmitCommand(cmd SubmitCommand) error {
	if len(cmd.Recipients) == 0 {
		return fmt.Errorf("%w: at least one recipient is required", ErrInvalidRequest)
	}
	if len(cmd.Recipients) > 100 {
		return fmt.Errorf("%w: a request can target at most 100 recipients", ErrInvalidRequest)
	}
	if cmd.Channel == "" {
		return fmt.Errorf("%w: channel is required", ErrInvalidRequest)
	}
	if !s.providers.Has(cmd.Channel) {
		return fmt.Errorf("%w: %s", ErrUnsupportedChannel, cmd.Channel)
	}
	if strings.TrimSpace(cmd.Message.Body) == "" {
		return fmt.Errorf("%w: message body is required", ErrInvalidRequest)
	}
	return nil
}

func cloneMessage(message domain.Message) domain.Message {
	copy := message
	if message.Data != nil {
		copy.Data = make(map[string]string, len(message.Data))
		for key, value := range message.Data {
			copy.Data[key] = value
		}
	}
	return copy
}

func newID(prefix string) (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(bytes[:]), nil
}
