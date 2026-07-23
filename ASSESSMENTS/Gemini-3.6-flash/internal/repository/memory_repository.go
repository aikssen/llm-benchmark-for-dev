package repository

import (
	"context"
	"sort"
	"sync"
	"time"

	"notification-service/internal/domain"
)

type MemoryRepository struct {
	mu               sync.RWMutex
	requests         map[string]*domain.NotificationRequest
	recipientStatuses map[string]*domain.NotificationRecipientStatus
	requestRecipients map[string][]string // requestID -> list of recipientStatus IDs
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		requests:          make(map[string]*domain.NotificationRequest),
		recipientStatuses: make(map[string]*domain.NotificationRecipientStatus),
		requestRecipients: make(map[string][]string),
	}
}

func (m *MemoryRepository) SaveRequest(ctx context.Context, req *domain.NotificationRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clone request
	reqCopy := *req
	reqCopy.Recipients = append([]string(nil), req.Recipients...)
	if req.Metadata != nil {
		reqCopy.Metadata = make(map[string]string)
		for k, v := range req.Metadata {
			reqCopy.Metadata[k] = v
		}
	}

	m.requests[req.ID] = &reqCopy
	return nil
}

func (m *MemoryRepository) GetRequestByID(ctx context.Context, id string) (*domain.NotificationRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	req, exists := m.requests[id]
	if !exists {
		return nil, domain.ErrNotificationNotFound
	}

	// Copy struct
	reqCopy := *req
	reqCopy.Recipients = append([]string(nil), req.Recipients...)

	// Fetch recipient details
	recIDs := m.requestRecipients[id]
	var details []domain.NotificationRecipientStatus
	for _, rID := range recIDs {
		if detail, ok := m.recipientStatuses[rID]; ok {
			details = append(details, *detail)
		}
	}
	reqCopy.RecipientDetail = details

	return &reqCopy, nil
}

func (m *MemoryRepository) ListRequests(ctx context.Context, filter domain.NotificationFilter) (*domain.ListResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if filter.Limit <= 0 {
		filter.Limit = 10
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	var matched []*domain.NotificationRequest
	for _, req := range m.requests {
		if filter.Channel != "" && req.Channel.Normalized() != filter.Channel.Normalized() {
			continue
		}
		if filter.Status != "" && req.Status != filter.Status {
			continue
		}
		reqCopy := *req
		reqCopy.Recipients = append([]string(nil), req.Recipients...)
		matched = append(matched, &reqCopy)
	}

	// Sort by CreatedAt descending
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})

	total := len(matched)
	start := filter.Offset
	if start > total {
		start = total
	}
	end := start + filter.Limit
	if end > total {
		end = total
	}

	paginated := matched[start:end]

	return &domain.ListResult{
		Notifications: paginated,
		Total:         total,
		Limit:         filter.Limit,
		Offset:        filter.Offset,
	}, nil
}

func (m *MemoryRepository) SaveRecipientStatus(ctx context.Context, status *domain.NotificationRecipientStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	statusCopy := *status
	m.recipientStatuses[status.ID] = &statusCopy
	m.requestRecipients[status.RequestID] = append(m.requestRecipients[status.RequestID], status.ID)
	return nil
}

func (m *MemoryRepository) UpdateRecipientStatus(ctx context.Context, id string, status domain.Status, providerUsed, errStr string, attempts int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, exists := m.recipientStatuses[id]
	if !exists {
		return domain.ErrNotificationNotFound
	}

	r.Status = status
	r.ProviderUsed = providerUsed
	r.Error = errStr
	r.Attempts = attempts
	r.UpdatedAt = time.Now()

	return nil
}

func (m *MemoryRepository) UpdateAggregateStatus(ctx context.Context, requestID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	req, exists := m.requests[requestID]
	if !exists {
		return domain.ErrNotificationNotFound
	}

	recIDs := m.requestRecipients[requestID]
	delivered := 0
	failed := 0
	pendingOrProcessing := 0

	for _, rID := range recIDs {
		detail, ok := m.recipientStatuses[rID]
		if !ok {
			continue
		}
		switch detail.Status {
		case domain.StatusDelivered:
			delivered++
		case domain.StatusFailed, domain.StatusInvalid:
			failed++
		default:
			pendingOrProcessing++
		}
	}

	req.DeliveredCount = delivered
	req.FailedCount = failed
	req.UpdatedAt = time.Now()

	if pendingOrProcessing == 0 {
		if delivered == req.TotalRecipients {
			req.Status = domain.StatusDelivered
		} else if failed == req.TotalRecipients {
			req.Status = domain.StatusFailed
		} else {
			req.Status = domain.StatusPartiallyFailed
		}
	} else {
		req.Status = domain.StatusProcessing
	}

	return nil
}
