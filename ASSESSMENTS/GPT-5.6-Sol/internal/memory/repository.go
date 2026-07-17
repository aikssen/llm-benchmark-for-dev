package memory

import (
	"context"
	"sort"
	"sync"

	"notification/internal/notification"
)

type idempotencyRecord struct {
	id          string
	fingerprint string
}

type Repository struct {
	mu          sync.RWMutex
	values      map[string]notification.Notification
	idempotency map[string]idempotencyRecord
}

func NewRepository() *Repository {
	return &Repository{
		values:      make(map[string]notification.Notification),
		idempotency: make(map[string]idempotencyRecord),
	}
}

func (r *Repository) Create(_ context.Context, value notification.Notification, key, fingerprint string) (notification.Notification, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if key != "" {
		if record, ok := r.idempotency[key]; ok {
			if record.fingerprint != fingerprint {
				return notification.Notification{}, false, notification.ErrIdempotencyConflict
			}
			return clone(r.values[record.id]), false, nil
		}
	}

	r.values[value.ID] = clone(value)
	if key != "" {
		r.idempotency[key] = idempotencyRecord{id: value.ID, fingerprint: fingerprint}
	}
	return clone(value), true, nil
}

func (r *Repository) Get(_ context.Context, id string) (notification.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.values[id]
	if !ok {
		return notification.Notification{}, notification.ErrNotFound
	}
	return clone(value), nil
}

func (r *Repository) GetByIdempotencyKey(_ context.Context, key, fingerprint string) (notification.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.idempotency[key]
	if !ok {
		return notification.Notification{}, notification.ErrNotFound
	}
	if record.fingerprint != fingerprint {
		return notification.Notification{}, notification.ErrIdempotencyConflict
	}
	return clone(r.values[record.id]), nil
}

func (r *Repository) SaveDelivery(_ context.Context, id string, index int, delivery notification.Delivery) (notification.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.values[id]
	if !ok || index < 0 || index >= len(value.Deliveries) {
		return notification.Notification{}, notification.ErrNotFound
	}
	value.Deliveries[index] = delivery
	value.Status = notification.AggregateStatus(value.Deliveries)
	if delivery.UpdatedAt.After(value.UpdatedAt) {
		value.UpdatedAt = delivery.UpdatedAt
	}
	r.values[id] = value
	return clone(value), nil
}

func (r *Repository) List(_ context.Context, limit, offset int) (notification.Page, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make([]notification.Notification, 0, len(r.values))
	for _, value := range r.values {
		values = append(values, clone(value))
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID > values[j].ID
		}
		return values[i].CreatedAt.After(values[j].CreatedAt)
	})
	total := len(values)
	if offset >= total {
		values = []notification.Notification{}
	} else {
		end := min(offset+limit, total)
		values = values[offset:end]
	}
	return notification.Page{Items: values, Total: total, Limit: limit, Offset: offset}, nil
}

func clone(value notification.Notification) notification.Notification {
	value.Deliveries = append([]notification.Delivery(nil), value.Deliveries...)
	return value
}
