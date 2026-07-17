package notification

import "testing"

func TestAggregateStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		deliveries []DeliveryStatus
		want       Status
	}{
		{"queued", []DeliveryStatus{DeliveryQueued, DeliveryQueued}, StatusQueued},
		{"processing", []DeliveryStatus{DeliveryDelivered, DeliveryRetrying}, StatusProcessing},
		{"delivered", []DeliveryStatus{DeliveryDelivered, DeliveryDelivered}, StatusDelivered},
		{"partial", []DeliveryStatus{DeliveryDelivered, DeliveryFailed}, StatusPartiallyDelivered},
		{"rejected", []DeliveryStatus{DeliveryRejected, DeliveryRejected}, StatusRejected},
		{"failed", []DeliveryStatus{DeliveryFailed, DeliveryRejected}, StatusFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deliveries := make([]Delivery, len(test.deliveries))
			for index, status := range test.deliveries {
				deliveries[index].Status = status
			}
			if got := AggregateStatus(deliveries); got != test.want {
				t.Fatalf("AggregateStatus() = %q, want %q", got, test.want)
			}
		})
	}
}
