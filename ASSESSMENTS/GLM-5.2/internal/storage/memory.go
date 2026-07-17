// Package storage defines the persistence boundary for notifications and
// provides an in-memory implementation suitable for local runs and tests.
//
// The Repository interface keeps the processing layer decoupled from storage
// details; a SQL implementation can be added behind the same interface.
package storage

import (
	"context"
	"sync"

	"github.com/example/notification-service/internal/domain"
)

// Repository persists notifications and supports granular status updates so
// the processing pipeline can stream per-recipient results as they complete.
type Repository interface {
	Create(ctx context.Context, n *domain.Notification) error
	Get(ctx context.Context, id domain.ID) (*domain.Notification, error)
	// List returns a page of notifications ordered by creation time desc, plus
	// the total count. limit <= 0 applies a sensible default; callers may cap.
	List(ctx context.Context, limit, offset int) ([]*domain.Notification, int, error)
	SetStatus(ctx context.Context, id domain.ID, status domain.Status) error
	UpsertResult(ctx context.Context, id domain.ID, result domain.RecipientResult) error
}

// Memory is a thread-safe in-memory Repository.
type Memory struct {
	mu      sync.RWMutex
	items   map[domain.ID]domain.Notification
	orderBy []domain.ID // creation order, for deterministic listing
}

// NewMemory builds an empty in-memory repository.
func NewMemory() *Memory {
	return &Memory{items: make(map[domain.ID]domain.Notification)}
}

func (m *Memory) Create(_ context.Context, n *domain.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[n.ID] = cloneNotification(n)
	m.orderBy = append(m.orderBy, n.ID)
	return nil
}

func (m *Memory) Get(_ context.Context, id domain.ID) (*domain.Notification, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	c := cloneNotification(&n)
	return &c, nil
}

func (m *Memory) List(_ context.Context, limit, offset int) ([]*domain.Notification, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.orderBy)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = total - offset
	}

	// Newest first: iterate orderBy in reverse.
	reversed := make([]domain.ID, 0, total)
	for i := len(m.orderBy) - 1; i >= 0; i-- {
		reversed = append(reversed, m.orderBy[i])
	}
	end := offset + limit
	if end > len(reversed) {
		end = len(reversed)
	}
	page := reversed[offset:end]

	out := make([]*domain.Notification, 0, len(page))
	for _, id := range page {
		n := m.items[id]
		c := cloneNotification(&n)
		out = append(out, &c)
	}
	return out, total, nil
}

func (m *Memory) SetStatus(_ context.Context, id domain.ID, status domain.Status) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.items[id]
	if !ok {
		return domain.ErrNotFound
	}
	n.Status = status
	m.items[id] = n
	return nil
}

func (m *Memory) UpsertResult(_ context.Context, id domain.ID, result domain.RecipientResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.items[id]
	if !ok {
		return domain.ErrNotFound
	}
	cloned := cloneRecipientResult(result)
	for i := range n.Results {
		if n.Results[i].RecipientID == cloned.RecipientID {
			n.Results[i] = cloned
			m.items[id] = n
			return nil
		}
	}
	n.Results = append(n.Results, cloned)
	m.items[id] = n
	return nil
}

// cloneNotification returns a fully independent deep copy of n so callers
// receive a snapshot that cannot race with in-flight mutations. Slice headers
// and the message data map are copied; element copies are sufficient because
// the element types are strings and primitive structs.
func cloneNotification(n *domain.Notification) domain.Notification {
	c := *n
	if n.Recipients != nil {
		c.Recipients = append([]domain.Recipient(nil), n.Recipients...)
	}
	if n.Results != nil {
		c.Results = make([]domain.RecipientResult, len(n.Results))
		for i := range n.Results {
			c.Results[i] = cloneRecipientResult(n.Results[i])
		}
	}
	if n.Message.Data != nil {
		c.Message.Data = make(map[string]string, len(n.Message.Data))
		for k, v := range n.Message.Data {
			c.Message.Data[k] = v
		}
	}
	return c
}

func cloneRecipientResult(r domain.RecipientResult) domain.RecipientResult {
	c := r
	if r.ValidationErrors != nil {
		c.ValidationErrors = append([]domain.ValidationError(nil), r.ValidationErrors...)
	}
	return c
}

var _ Repository = (*Memory)(nil)
