// Package config loads configuration from the environment.
//
// Environment variables are the deployment interface: they work identically
// in a shell, a container and a scheduler, and they keep secrets out of the
// repository. Every setting has a default that makes the service runnable
// with no configuration at all.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the full configuration of the service.
type Config struct {
	Server    Server
	Worker    Worker
	Provider  Provider
	Retry     Retry
	Breaker   Breaker
	RateLimit RateLimit
	Log       Log
	Limits    Limits
}

// Server holds HTTP server settings.
type Server struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	RequestTimeout  time.Duration
	MaxRequestBytes int64
	// ShutdownTimeout bounds graceful shutdown: how long the service waits
	// for in-flight requests and the queue backlog before giving up.
	ShutdownTimeout time.Duration
}

// Worker holds the processing pool settings.
type Worker struct {
	// Count is the number of concurrent deliveries in flight.
	Count int
	// QueueCapacity is how much burst the service absorbs before shedding.
	QueueCapacity int
}

// Provider holds the simulated providers' behaviour.
type Provider struct {
	CallTimeout          time.Duration
	MinLatency           time.Duration
	MaxLatency           time.Duration
	TransientFailureRate float64
	PermanentFailureRate float64
}

// Retry holds the retry policy.
type Retry struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
	Jitter         float64
}

// Breaker holds the circuit breaker policy.
type Breaker struct {
	FailureThreshold int
	OpenDuration     time.Duration
	HalfOpenMaxCalls int
	SuccessThreshold int
}

// RateLimit holds the per-recipient throttle.
type RateLimit struct {
	Rate     int
	Interval time.Duration
	Burst    int
}

// Log holds logging settings.
type Log struct {
	Level  string
	Format string
}

// Limits holds request limits.
type Limits struct {
	MaxRecipients int
}

// Default returns the configuration used when nothing is set.
func Default() Config {
	return Config{
		Server: Server{
			Addr:            ":8080",
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    15 * time.Second,
			IdleTimeout:     60 * time.Second,
			RequestTimeout:  10 * time.Second,
			MaxRequestBytes: 1 << 20,
			ShutdownTimeout: 20 * time.Second,
		},
		Worker: Worker{
			Count:         16,
			QueueCapacity: 4096,
		},
		Provider: Provider{
			CallTimeout:          3 * time.Second,
			MinLatency:           20 * time.Millisecond,
			MaxLatency:           250 * time.Millisecond,
			TransientFailureRate: 0.15,
			PermanentFailureRate: 0.02,
		},
		Retry: Retry{
			MaxAttempts:    3,
			InitialBackoff: 100 * time.Millisecond,
			MaxBackoff:     2 * time.Second,
			Multiplier:     2,
			Jitter:         0.2,
		},
		Breaker: Breaker{
			FailureThreshold: 10,
			OpenDuration:     5 * time.Second,
			HalfOpenMaxCalls: 2,
			SuccessThreshold: 2,
		},
		RateLimit: RateLimit{
			Rate:     60,
			Interval: time.Minute,
			Burst:    10,
		},
		Log: Log{Level: "info", Format: "json"},
		Limits: Limits{
			MaxRecipients: 1000,
		},
	}
}

// Load reads configuration from the environment on top of the defaults.
//
// Every problem found is reported at once: making an operator fix a bad value
// only to hit the next one on the following start is needlessly slow. This is
// the opposite of the request-validation policy on purpose — the cost here is
// a restart, not wasted per-request work.
func Load() (Config, error) {
	cfg := Default()
	var errs []error

	env := func(key string) (string, bool) {
		v, ok := os.LookupEnv(key)
		if !ok {
			return "", false
		}
		v = strings.TrimSpace(v)
		return v, v != ""
	}

	str := func(key string, target *string) {
		if v, ok := env(key); ok {
			*target = v
		}
	}
	integer := func(key string, target *int, minimum int) {
		v, ok := env(key)
		if !ok {
			return
		}
		n, err := strconv.Atoi(v)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %q is not an integer", key, v))
		case n < minimum:
			errs = append(errs, fmt.Errorf("%s: must be at least %d, got %d", key, minimum, n))
		default:
			*target = n
		}
	}
	dur := func(key string, target *time.Duration, allowZero bool) {
		v, ok := env(key)
		if !ok {
			return
		}
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %q is not a duration (e.g. 250ms, 5s)", key, v))
		case d < 0 || (d == 0 && !allowZero):
			errs = append(errs, fmt.Errorf("%s: must be positive, got %s", key, v))
		default:
			*target = d
		}
	}
	ratio := func(key string, target *float64) {
		v, ok := env(key)
		if !ok {
			return
		}
		f, err := strconv.ParseFloat(v, 64)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %q is not a number", key, v))
		case f < 0 || f > 1:
			errs = append(errs, fmt.Errorf("%s: must be between 0 and 1, got %s", key, v))
		default:
			*target = f
		}
	}
	number := func(key string, target *float64, minimum float64) {
		v, ok := env(key)
		if !ok {
			return
		}
		f, err := strconv.ParseFloat(v, 64)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %q is not a number", key, v))
		case f < minimum:
			errs = append(errs, fmt.Errorf("%s: must be at least %v, got %s", key, minimum, v))
		default:
			*target = f
		}
	}

	str("SERVER_ADDR", &cfg.Server.Addr)
	dur("SERVER_READ_TIMEOUT", &cfg.Server.ReadTimeout, false)
	dur("SERVER_WRITE_TIMEOUT", &cfg.Server.WriteTimeout, false)
	dur("SERVER_IDLE_TIMEOUT", &cfg.Server.IdleTimeout, false)
	dur("SERVER_REQUEST_TIMEOUT", &cfg.Server.RequestTimeout, false)
	dur("SERVER_SHUTDOWN_TIMEOUT", &cfg.Server.ShutdownTimeout, false)

	maxBytes := int(cfg.Server.MaxRequestBytes)
	integer("SERVER_MAX_REQUEST_BYTES", &maxBytes, 1024)
	cfg.Server.MaxRequestBytes = int64(maxBytes)

	integer("WORKER_COUNT", &cfg.Worker.Count, 1)
	integer("QUEUE_CAPACITY", &cfg.Worker.QueueCapacity, 1)
	integer("MAX_RECIPIENTS_PER_REQUEST", &cfg.Limits.MaxRecipients, 1)

	dur("PROVIDER_CALL_TIMEOUT", &cfg.Provider.CallTimeout, false)
	dur("PROVIDER_MIN_LATENCY", &cfg.Provider.MinLatency, true)
	dur("PROVIDER_MAX_LATENCY", &cfg.Provider.MaxLatency, true)
	ratio("PROVIDER_TRANSIENT_FAILURE_RATE", &cfg.Provider.TransientFailureRate)
	ratio("PROVIDER_PERMANENT_FAILURE_RATE", &cfg.Provider.PermanentFailureRate)

	integer("RETRY_MAX_ATTEMPTS", &cfg.Retry.MaxAttempts, 1)
	dur("RETRY_INITIAL_BACKOFF", &cfg.Retry.InitialBackoff, true)
	dur("RETRY_MAX_BACKOFF", &cfg.Retry.MaxBackoff, true)
	number("RETRY_MULTIPLIER", &cfg.Retry.Multiplier, 1)
	ratio("RETRY_JITTER", &cfg.Retry.Jitter)

	integer("BREAKER_FAILURE_THRESHOLD", &cfg.Breaker.FailureThreshold, 0)
	dur("BREAKER_OPEN_DURATION", &cfg.Breaker.OpenDuration, false)
	integer("BREAKER_HALF_OPEN_MAX_CALLS", &cfg.Breaker.HalfOpenMaxCalls, 1)
	integer("BREAKER_SUCCESS_THRESHOLD", &cfg.Breaker.SuccessThreshold, 1)

	integer("RATE_LIMIT_RATE", &cfg.RateLimit.Rate, 0)
	dur("RATE_LIMIT_INTERVAL", &cfg.RateLimit.Interval, false)
	integer("RATE_LIMIT_BURST", &cfg.RateLimit.Burst, 0)

	str("LOG_LEVEL", &cfg.Log.Level)
	str("LOG_FORMAT", &cfg.Log.Format)

	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

// validate catches combinations that are individually fine but jointly wrong.
func (c Config) validate() error {
	var errs []error

	if c.Provider.MaxLatency < c.Provider.MinLatency {
		errs = append(errs, fmt.Errorf(
			"PROVIDER_MAX_LATENCY (%s) must not be smaller than PROVIDER_MIN_LATENCY (%s)",
			c.Provider.MaxLatency, c.Provider.MinLatency))
	}
	if c.Provider.TransientFailureRate+c.Provider.PermanentFailureRate > 1 {
		errs = append(errs, errors.New(
			"PROVIDER_TRANSIENT_FAILURE_RATE and PROVIDER_PERMANENT_FAILURE_RATE must sum to at most 1"))
	}
	if c.Retry.MaxBackoff > 0 && c.Retry.MaxBackoff < c.Retry.InitialBackoff {
		errs = append(errs, fmt.Errorf(
			"RETRY_MAX_BACKOFF (%s) must not be smaller than RETRY_INITIAL_BACKOFF (%s)",
			c.Retry.MaxBackoff, c.Retry.InitialBackoff))
	}
	// A request larger than the queue could never be admitted, which would
	// surface as a confusing runtime rejection rather than a config error.
	if c.Limits.MaxRecipients > c.Worker.QueueCapacity {
		errs = append(errs, fmt.Errorf(
			"MAX_RECIPIENTS_PER_REQUEST (%d) must not exceed QUEUE_CAPACITY (%d)",
			c.Limits.MaxRecipients, c.Worker.QueueCapacity))
	}
	switch strings.ToLower(c.Log.Format) {
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("LOG_FORMAT: must be \"json\" or \"text\", got %q", c.Log.Format))
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("LOG_LEVEL: must be one of debug, info, warn, error, got %q", c.Log.Level))
	}
	return errors.Join(errs...)
}
