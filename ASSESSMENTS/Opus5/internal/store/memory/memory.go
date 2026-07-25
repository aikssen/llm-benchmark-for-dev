// Package memory is an in-memory implementation of store.Store.
//
// It keeps the project runnable with no external dependencies. It is not a
// production store: state is lost on restart and does not survive more than
// one instance. See docs/DESIGN.md for the migration path.
package memory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/evercifuentes/notifier/internal/domain"
	"github.com/evercifuentes/notifier/internal/store"
)

// ErrDuplicate is returned when an ID or idempotency key is already in use.
var ErrDuplicate = errors.New("notification already exists")

// record is the stored aggregate plus the indexes needed to update a single
// delivery without scanning.
type record struct {
	notification *domain.Notification
	byDeliveryID map[string]*domain.Delivery
}

// Store is a concurrency-safe in-memory Store.
type Store struct {
	mu sync.RWMutex
	// byID owns the records.
	byID map[string]*record
	// order holds notification IDs in insertion order, so listing is stable
	// and does not need to sort on every call.
	order []string
	// byIdempotencyKey maps a client key to a notification ID.
	byIdempotencyKey map[string]string

	now func() time.Time
}

// Compile-time proof that the adapter satisfies the port.
var _ store.Store = (*Store)(nil)

// Option customises the store.
type Option func(*Store)

// WithClock replaces the time source used to stamp updates.
func WithClock(now func() time.Time) Option {
	return func(s *Store) { s.now = now }
}

// New builds an empty store.
func New(opts ...Option) *Store {
	s := &Store{
		byID:             make(map[string]*record),
		byIdempotencyKey: make(map[string]string),
		now:              time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create stores a deep copy of n.
func (s *Store) Create(_ context.Context, n *domain.Notification) error {
	if n == nil || n.ID == "" {
		return fmt.Errorf("notification must have an ID")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.byID[n.ID]; exists {
		return fmt.Errorf("%w: id %q", ErrDuplicate, n.ID)
	}
	if n.IdempotencyKey != "" {
		if _, exists := s.byIdempotencyKey[n.IdempotencyKey]; exists {
			return fmt.Errorf("%w: idempotency key %q", ErrDuplicate, n.IdempotencyKey)
		}
	}

	stored := n.Clone()
	rec := &record{
		notification: stored,
		byDeliveryID: make(map[string]*domain.Delivery, len(stored.Deliveries)),
	}
	for _, d := range stored.Deliveries {
		rec.byDeliveryID[d.ID] = d
	}

	s.byID[stored.ID] = rec
	s.order = append(s.order, stored.ID)
	if stored.IdempotencyKey != "" {
		s.byIdempotencyKey[stored.IdempotencyKey] = stored.ID
	}
	return nil
}

// Get returns a deep copy of the notification.
func (s *Store) Get(_ context.Context, id string) (*domain.Notification, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", domain.ErrNotFound, id)
	}
	return rec.notification.Clone(), nil
}

// FindByIdempotencyKey returns the notification created under key.
func (s *Store) FindByIdempotencyKey(_ context.Context, key string) (*domain.Notification, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: empty idempotency key", domain.ErrNotFound)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.byIdempotencyKey[key]
	if !ok {
		return nil, fmt.Errorf("%w: idempotency key %q", domain.ErrNotFound, key)
	}
	return s.byID[id].notification.Clone(), nil
}

// UpdateDelivery applies mutate under the store's lock and refreshes the
// aggregate's UpdatedAt stamp.
func (s *Store) UpdateDelivery(_ context.Context, notificationID, deliveryID string, mutate func(*domain.Delivery)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.byID[notificationID]
	if !ok {
		return fmt.Errorf("%w: %q", domain.ErrNotFound, notificationID)
	}
	d, ok := rec.byDeliveryID[deliveryID]
	if !ok {
		return fmt.Errorf("%w: delivery %q of notification %q", domain.ErrNotFound, deliveryID, notificationID)
	}

	mutate(d)
	now := s.now()
	d.UpdatedAt = now
	rec.notification.UpdatedAt = now
	return nil
}

// List returns notifications newest first, filtered and paged.
func (s *Store) List(_ context.Context, f store.Filter) (store.Page, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := max(f.Offset, 0)

	s.mu.RLock()
	defer s.mu.RUnlock()

	page := store.Page{Limit: limit, Offset: offset, Items: []*domain.Notification{}}

	// Walk insertion order backwards: newest first, no sorting required.
	for _, id := range slices.Backward(s.order) {
		n := s.byID[id].notification
		if !matches(n, f) {
			continue
		}
		page.Total++
		switch {
		case page.Total <= offset:
			// Skipped by the offset.
		case len(page.Items) < limit:
			page.Items = append(page.Items, n.Clone())
		}
	}
	return page, nil
}

func matches(n *domain.Notification, f store.Filter) bool {
	if f.Channel != "" && n.Channel != f.Channel {
		return false
	}
	// Status is derived, so it is evaluated here rather than indexed. With a
	// SQL store this becomes a persisted, indexed column.
	if f.Status != "" && n.Status() != f.Status {
		return false
	}
	return true
}
