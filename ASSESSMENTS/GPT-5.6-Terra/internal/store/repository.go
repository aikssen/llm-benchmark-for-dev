package store

import (
	"errors"
	"sync"
	"time"

	"github.com/example/notification-orchestrator/internal/domain"
)

var (
	ErrNotFound            = errors.New("notification not found")
	ErrIdempotencyConflict = errors.New("idempotency key was used with a different request")
)

type ListOptions struct {
	Limit  int
	Cursor string
}

type Page struct {
	Items      []*domain.Notification
	NextCursor string
}

// Repository deliberately hides persistence. A transactional SQL implementation can
// preserve this contract while adding an outbox and durable idempotency records.
type Repository interface {
	CreateOrGet(*domain.Notification) (notification *domain.Notification, created bool, err error)
	FindByIdempotency(key string) (*domain.Notification, bool)
	Get(id string) (*domain.Notification, error)
	List(ListOptions) Page
	UpdateDelivery(notificationID, deliveryID string, update func(*domain.Delivery)) (*domain.Notification, error)
}

func (r *MemoryRepository) FindByIdempotency(key string) (*domain.Notification, bool) {
	if key == "" {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.idempotency[key]
	if !ok {
		return nil, false
	}
	return domain.CloneNotification(r.notifications[id]), true
}

type MemoryRepository struct {
	mu            sync.RWMutex
	notifications map[string]*domain.Notification
	order         []string
	idempotency   map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		notifications: make(map[string]*domain.Notification),
		idempotency:   make(map[string]string),
	}
}

func (r *MemoryRepository) CreateOrGet(candidate *domain.Notification) (*domain.Notification, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if candidate.IdempotencyKey != "" {
		if id, ok := r.idempotency[candidate.IdempotencyKey]; ok {
			existing := r.notifications[id]
			if sameRequest(existing, candidate) {
				return domain.CloneNotification(existing), false, nil
			}
			return nil, false, ErrIdempotencyConflict
		}
	}
	r.notifications[candidate.ID] = domain.CloneNotification(candidate)
	r.order = append(r.order, candidate.ID)
	if candidate.IdempotencyKey != "" {
		r.idempotency[candidate.IdempotencyKey] = candidate.ID
	}
	return domain.CloneNotification(candidate), true, nil
}

func (r *MemoryRepository) Get(id string) (*domain.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	notification, ok := r.notifications[id]
	if !ok {
		return nil, ErrNotFound
	}
	return domain.CloneNotification(notification), nil
}

func (r *MemoryRepository) List(options ListOptions) Page {
	r.mu.RLock()
	defer r.mu.RUnlock()

	limit := options.Limit
	if limit <= 0 {
		limit = 20
	}
	start := len(r.order) - 1
	if options.Cursor != "" {
		start = -1
		for index := len(r.order) - 1; index >= 0; index-- {
			if r.order[index] == options.Cursor {
				start = index - 1
				break
			}
		}
	}

	page := Page{Items: make([]*domain.Notification, 0, limit)}
	for index := start; index >= 0 && len(page.Items) < limit; index-- {
		page.Items = append(page.Items, domain.CloneNotification(r.notifications[r.order[index]]))
	}
	if start >= 0 && start-len(page.Items) >= 0 && len(page.Items) > 0 {
		page.NextCursor = page.Items[len(page.Items)-1].ID
	}
	return page
}

func (r *MemoryRepository) UpdateDelivery(notificationID, deliveryID string, update func(*domain.Delivery)) (*domain.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	notification, ok := r.notifications[notificationID]
	if !ok {
		return nil, ErrNotFound
	}
	for index := range notification.Deliveries {
		if notification.Deliveries[index].ID == deliveryID {
			update(&notification.Deliveries[index])
			notification.RecalculateStatus()
			notification.UpdatedAt = time.Now().UTC()
			return domain.CloneNotification(notification), nil
		}
	}
	return nil, ErrNotFound
}

func sameRequest(a, b *domain.Notification) bool {
	if a.Channel != b.Channel || a.Message != b.Message || len(a.Deliveries) != len(b.Deliveries) {
		return false
	}
	for i := range a.Deliveries {
		if a.Deliveries[i].Recipient != b.Deliveries[i].Recipient {
			return false
		}
	}
	return true
}
