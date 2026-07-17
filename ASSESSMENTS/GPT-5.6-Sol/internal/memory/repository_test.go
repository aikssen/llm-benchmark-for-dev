package memory

import (
	"context"
	"sync"
	"testing"
	"time"

	"notification/internal/notification"
)

func TestSaveDeliveryConcurrentUpdatesAggregate(t *testing.T) {
	t.Parallel()
	repository := NewRepository()
	now := time.Now().UTC()
	value := notification.Notification{
		ID: "n-1", Status: notification.StatusQueued, CreatedAt: now, UpdatedAt: now,
		Deliveries: []notification.Delivery{
			{Recipient: "one", Status: notification.DeliveryQueued, UpdatedAt: now},
			{Recipient: "two", Status: notification.DeliveryQueued, UpdatedAt: now},
		},
	}
	if _, _, err := repository.Create(context.Background(), value, "", ""); err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	for index := range value.Deliveries {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			delivery := value.Deliveries[index]
			delivery.Status = notification.DeliveryDelivered
			if _, err := repository.SaveDelivery(context.Background(), value.ID, index, delivery); err != nil {
				t.Errorf("SaveDelivery() error = %v", err)
			}
		}(index)
	}
	wait.Wait()

	got, err := repository.Get(context.Background(), value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != notification.StatusDelivered {
		t.Fatalf("status = %q, want %q", got.Status, notification.StatusDelivered)
	}
}

func TestCreateIdempotency(t *testing.T) {
	t.Parallel()
	repository := NewRepository()
	value := notification.Notification{ID: "n-1"}
	if _, created, err := repository.Create(context.Background(), value, "key", "fingerprint"); err != nil || !created {
		t.Fatalf("first Create() = created %v, error %v", created, err)
	}
	got, created, err := repository.Create(context.Background(), notification.Notification{ID: "n-2"}, "key", "fingerprint")
	if err != nil || created || got.ID != "n-1" {
		t.Fatalf("replayed Create() = %+v, created %v, error %v", got, created, err)
	}
	if _, _, err = repository.Create(context.Background(), notification.Notification{ID: "n-3"}, "key", "different"); err != notification.ErrIdempotencyConflict {
		t.Fatalf("conflicting Create() error = %v", err)
	}
}
