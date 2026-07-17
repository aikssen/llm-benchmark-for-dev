package delivery_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/aikssen/notification-service/internal/delivery"
)

func TestBackoffPolicy_Delay(t *testing.T) {
	policy := delivery.BackoffPolicy{Base: 10 * time.Millisecond, Max: 200 * time.Millisecond}

	assert.Equal(t, time.Duration(0), policy.Delay(0))
	assert.Equal(t, time.Duration(0), policy.Delay(1))

	for attempt := 2; attempt <= 10; attempt++ {
		d := policy.Delay(attempt)
		assert.GreaterOrEqual(t, d, time.Duration(0))
		assert.LessOrEqual(t, d, policy.Max, "delay must never exceed the configured ceiling")
	}
}
