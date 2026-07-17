package domain

import "testing"

func TestAggregateStatus(t *testing.T) {
	d := func(statuses ...DeliveryStatus) []Delivery {
		out := make([]Delivery, len(statuses))
		for i, s := range statuses {
			out[i] = Delivery{Status: s}
		}
		return out
	}

	tests := []struct {
		name       string
		deliveries []Delivery
		want       Status
	}{
		{"no deliveries", nil, StatusPending},
		{"all pending", d(DeliveryPending, DeliveryPending), StatusPending},
		{"all delivered", d(DeliveryDelivered, DeliveryDelivered), StatusDelivered},
		{"all failed", d(DeliveryFailed, DeliveryFailed), StatusFailed},
		{"mixed terminal", d(DeliveryDelivered, DeliveryFailed), StatusPartiallyDelivered},
		{"one processing", d(DeliveryProcessing, DeliveryDelivered), StatusProcessing},
		{"pending and delivered", d(DeliveryPending, DeliveryDelivered), StatusProcessing},
		{"single delivered", d(DeliveryDelivered), StatusDelivered},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AggregateStatus(tt.deliveries); got != tt.want {
				t.Errorf("AggregateStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChannelValid(t *testing.T) {
	for _, ch := range Channels() {
		if !ch.Valid() {
			t.Errorf("channel %q should be valid", ch)
		}
	}
	if Channel("carrier-pigeon").Valid() {
		t.Error("unknown channel should not be valid")
	}
}
