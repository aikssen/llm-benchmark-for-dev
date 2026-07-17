package delivery

import (
	"math/rand/v2"
	"time"
)

// BackoffPolicy computes retry delays using exponential backoff with full
// jitter (delay is chosen uniformly from [0, cap]), which spreads out
// retries from a burst of failing notifications instead of having them
// all hammer the provider again in lockstep.
type BackoffPolicy struct {
	Base time.Duration
	Max  time.Duration
}

// Delay returns the wait before the given attempt (1-indexed: the delay
// before attempt 2, 3, ...). attempt <= 1 waits zero time.
func (p BackoffPolicy) Delay(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	ceiling := p.Base << (attempt - 2)   // Base * 2^(attempt-2)
	if ceiling <= 0 || ceiling > p.Max { // guard against overflow from large attempt counts
		ceiling = p.Max
	}
	if ceiling <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(ceiling)))
}
