package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/notifications-service/internal/domain"
	"github.com/notifications-service/internal/provider"
	"github.com/notifications-service/internal/storage"
	"github.com/notifications-service/internal/validator"
)

type NotificationService struct {
	store      storage.Store
	registry   *provider.Registry
	validator  *validator.Chain
	processor  *Processor
	logger     *slog.Logger
	idGen      IDGenerator
}

func NewNotificationService(
	store storage.Store,
	registry *provider.Registry,
	chain *validator.Chain,
	processor *Processor,
	logger *slog.Logger,
) *NotificationService {
	return &NotificationService{
		store:     store,
		registry:  registry,
		validator: chain,
		processor: processor,
		logger:    logger,
		idGen:     NewUUIDGenerator(),
	}
}

func (s *NotificationService) Submit(ctx context.Context, req *domain.NotificationRequest) (*domain.Notification, error) {
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	now := time.Now()
	notification := &domain.Notification{
		ID:        s.idGen.Generate(),
		Channel:   req.Channel,
		Subject:   req.Subject,
		Message:   req.Message,
		Status:    domain.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.Save(ctx, notification); err != nil {
		return nil, err
	}

	s.processor.Enqueue(DeliveryJob{
		NotificationID: notification.ID,
		Channel:        req.Channel,
		Subject:        req.Subject,
		Message:        req.Message,
		Recipients:     req.Recipients,
	})

	s.logger.Info("notification submitted",
		"notification_id", notification.ID,
		"channel", req.Channel,
		"recipients_count", len(req.Recipients),
	)

	return notification, nil
}

func (s *NotificationService) Get(ctx context.Context, id string) (*domain.NotificationResponse, error) {
	notification, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	recipientStats, err := s.store.GetRecipientStatuses(ctx, id)
	if err != nil {
		return nil, err
	}

	response := &domain.NotificationResponse{
		ID:        notification.ID,
		Channel:   notification.Channel,
		Subject:   notification.Subject,
		Message:   notification.Message,
		Status:    notification.Status,
		CreatedAt: notification.CreatedAt,
		UpdatedAt: notification.UpdatedAt,
	}

	for _, rs := range recipientStats {
		response.Recipients = append(response.Recipients, domain.RecipientStatus{
			Recipient:     rs.Recipient,
			Status:        rs.Status,
			FailureReason: rs.FailureReason,
			DeliveredAt:   rs.DeliveredAt,
		})
	}

	return response, nil
}

func (s *NotificationService) List(ctx context.Context, filter domain.ListFilter) ([]*domain.Notification, int, error) {
	return s.store.List(ctx, filter)
}

func (s *NotificationService) validateRequest(req *domain.NotificationRequest) error {
	if !req.Channel.IsValid() {
		return &domain.ValidationError{
			Field:   "channel",
			Message: "invalid notification channel",
		}
	}

	if len(req.Recipients) == 0 {
		return &domain.ValidationError{
			Field:   "recipients",
			Message: "at least one recipient is required",
		}
	}

	if _, ok := s.registry.Get(req.Channel); !ok {
		return &domain.ValidationError{
			Field:   "channel",
			Message: "no provider available for this channel",
		}
	}

	return nil
}

func (s *NotificationService) ValidateRecipient(ctx context.Context, recipient string, req *domain.NotificationRequest) error {
	return s.validator.Validate(ctx, recipient, req)
}

func (s *NotificationService) GetProvider(channel domain.Channel) (provider.Provider, error) {
	p, ok := s.registry.Get(channel)
	if !ok {
		return nil, errors.New("provider not found for channel: " + string(channel))
	}
	return p, nil
}

func (s *NotificationService) Store() storage.Store {
	return s.store
}
