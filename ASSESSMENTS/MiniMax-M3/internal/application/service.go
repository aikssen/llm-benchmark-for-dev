package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anomalyco/notification-service/internal/domain"
	"github.com/anomalyco/notification-service/internal/infrastructure/repository"
)

type SubmitRequest struct {
	Recipient string
	Channel   domain.Channel
	Message   domain.Message
}

type SubmitResult struct {
	Notifications []*domain.Notification
}

type NotificationService struct {
	repo       repository.NotificationRepository
	dispatcher *Dispatcher
	log        *slog.Logger
	now        func() time.Time
}

func NewNotificationService(
	repo repository.NotificationRepository,
	dispatcher *Dispatcher,
	log *slog.Logger,
) *NotificationService {
	return &NotificationService{
		repo:       repo,
		dispatcher: dispatcher,
		log:        log,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("id-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func (s *NotificationService) Submit(ctx context.Context, recipients []string, channel domain.Channel, message domain.Message) (*SubmitResult, error) {
	if len(recipients) == 0 {
		return nil, domain.NewValidationError("recipients", "at least one recipient is required")
	}
	if !channel.Valid() {
		return nil, fmt.Errorf("%w: %q", domain.ErrUnknownChannel, channel)
	}
	if strings.TrimSpace(message.Body) == "" {
		return nil, domain.NewValidationError("message.body", "must not be empty")
	}

	now := s.now()
	out := make([]*domain.Notification, 0, len(recipients))
	for _, r := range recipients {
		r = strings.TrimSpace(r)
		if r == "" {
			return nil, domain.NewValidationError("recipients", "contains an empty entry")
		}
		n := domain.NewNotification(newID(), r, channel, message, now)
		if err := s.repo.Create(ctx, n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}

	for _, n := range out {
		if err := s.dispatcher.Enqueue(ctx, n.ID); err != nil {
			s.log.Error("enqueue failed", "id", n.ID, "err", err)
			n.MarkStatus(domain.StatusFailed, s.now())
			n.Reason = "service overloaded"
			_ = s.repo.Update(ctx, n)
		}
	}
	return &SubmitResult{Notifications: out}, nil
}

func (s *NotificationService) Get(ctx context.Context, id string) (*domain.Notification, error) {
	if strings.TrimSpace(id) == "" {
		return nil, domain.NewValidationError("id", "must not be empty")
	}
	return s.repo.Get(ctx, id)
}

func (s *NotificationService) List(ctx context.Context, f repository.ListFilter) ([]*domain.Notification, error) {
	return s.repo.List(ctx, f)
}
