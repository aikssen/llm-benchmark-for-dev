package repository

import (
	"context"

	"notification-service/internal/domain"
)

type Repository interface {
	SaveRequest(ctx context.Context, req *domain.NotificationRequest) error
	GetRequestByID(ctx context.Context, id string) (*domain.NotificationRequest, error)
	ListRequests(ctx context.Context, filter domain.NotificationFilter) (*domain.ListResult, error)

	SaveRecipientStatus(ctx context.Context, status *domain.NotificationRecipientStatus) error
	UpdateRecipientStatus(ctx context.Context, id string, status domain.Status, providerUsed, err string, attempts int) error
	UpdateAggregateStatus(ctx context.Context, requestID string) error
}
