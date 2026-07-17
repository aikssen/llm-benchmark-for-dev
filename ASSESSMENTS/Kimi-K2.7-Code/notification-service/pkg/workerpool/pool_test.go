package workerpool_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"notification-service/pkg/workerpool"
)

func TestPool_ProcessesJobs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	processed := []string{}
	done := make(chan struct{})

	pool := workerpool.NewPool(2, 10)
	pool.Start(ctx, func(_ context.Context, job string) {
		mu.Lock()
		processed = append(processed, job)
		if len(processed) == 3 {
			close(done)
		}
		mu.Unlock()
	})

	for _, id := range []string{"a", "b", "c"} {
		require.True(t, pool.Enqueue(id))
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("jobs were not processed in time")
	}

	assert.Len(t, processed, 3)
}

func TestPool_GracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := workerpool.NewPool(1, 10)
	pool.Start(ctx, func(_ context.Context, _ string) {})

	pool.Shutdown(time.Second)
	assert.False(t, pool.Enqueue("x"))
}
