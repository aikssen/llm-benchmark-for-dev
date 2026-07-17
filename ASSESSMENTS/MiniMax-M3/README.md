# notification-service

A Go backend service that orchestrates the delivery of notifications across
multiple channels (email, SMS, push notifications) with validation, retries
and graceful failure handling.

## Requirements

- Go 1.22+ (uses the improved `http.ServeMux` pattern routing)

No third-party dependencies; the entire service is built on the Go standard
library so the supply chain surface is minimal and the build is fast.

## Quick start

```bash
# build
make build

# run (defaults to :8080)
make run

# or with custom env
HTTP_ADDR=:9090 PROVIDER_FAILURE_RATE=0.1 ./bin/notification-server
```

The server exposes:

| Method | Path                              | Description                              |
| ------ | --------------------------------- | ---------------------------------------- |
| GET    | `/healthz`                        | Liveness probe                           |
| POST   | `/v1/notifications`               | Submit a notification                    |
| GET    | `/v1/notifications`               | List notifications (filter + paginate)   |
| GET    | `/v1/notifications/{id}`          | Get a single notification by id          |

### Example: submit a notification

```bash
curl -X POST http://localhost:8080/v1/notifications \
  -H 'content-type: application/json' \
  -d '{
    "recipients": ["alice@example.com", "bob@example.com"],
    "channel": "email",
    "message": { "subject": "hi", "body": "hello" }
  }'
```

The response is `202 Accepted` with a list of created notifications, each
carrying its own id and initial `status` (`pending`). The service processes
them asynchronously in a worker pool.

### Example: list notifications

```bash
curl 'http://localhost:8080/v1/notifications?channel=email&limit=10&offset=0'
```

Supported query parameters: `channel` (email|sms|push), `status`,
`limit` (default 50, max unbounded), `offset` (default 0).

### Example: get a notification

```bash
curl http://localhost:8080/v1/notifications/<id>
```

## Configuration

All configuration is via environment variables. See `.env.example` for the
full list with defaults.

| Variable | Default | Description |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | HTTP listen address |
| `SHUTDOWN_TIMEOUT` | `15s` | Max time to wait for graceful shutdown |
| `WORKER_COUNT` | `8` | Number of dispatcher workers |
| `QUEUE_SIZE` | `256` | Bounded queue for pending notifications |
| `MAX_ATTEMPTS` | `4` | Max provider attempts before failing the notification |
| `BASE_BACKOFF` | `100ms` | Initial retry backoff (exponential, with jitter) |
| `MAX_BACKOFF` | `2s` | Upper bound for retry backoff |
| `PER_ATTEMPT_TIMEOUT` | `5s` | Timeout for each provider `Send` call |
| `RATE_LIMIT_CAPACITY` | `5` | Token bucket size per (channel, recipient) |
| `RATE_LIMIT_REFILL_PER_SEC` | `1` | Token bucket refill rate |
| `SWEEPER_INTERVAL` | `5m` | How often to garbage collect old notifications |
| `SWEEPER_TTL` | `24h` | Age after which terminal notifications are evicted |
| `SIMULATED_PROVIDER_ENABLED` | `true` | Enable the in-process simulated provider |
| `PROVIDER_LATENCY_MIN` / `PROVIDER_LATENCY_MAX` | `5ms` / `50ms` | Simulated provider latency window |
| `PROVIDER_FAILURE_RATE` | `0.2` | Probability of a transient provider error per call |
| `PROVIDER_UNAVAILABLE_RATE` | `0.05` | Probability of `ErrProviderUnavail` per call |
| `PROVIDER_SEED` | `42` | Seed for the simulated provider's RNG |

## Testing

```bash
make test          # standard
make test-race     # with -race
```

Tests cover:

- Domain validation (channels, recipients, messages, terminal status).
- In-memory repository CRUD, listing/pagination, sweeper.
- Validation pipeline (serial + parallel + cancellation).
- Rate limit token bucket.
- Provider registry and context cancellation.
- Application service: submit, list, retry, eventual success, retries
  exhausted, validation rejection, missing provider.
- HTTP layer: end-to-end submit/get/list with httptest, invalid channel
  handling, not-found, timeout-induced failure.

## Smoke test

A runnable end-to-end smoke script is included. With the server running:

```bash
make smoke
```

## Architecture

The service is structured as a small hexagonal (ports & adapters)
application. Each layer has a narrow responsibility and depends only on the
layers below it via interfaces.

```
cmd/server
  └─ main.go              composition root: wires config, repo, providers,
                          pipeline, dispatcher, service, HTTP server

internal/
  domain/                 core entities and value objects
    notification.go        Notification, Channel, Status, Message
    errors.go              sentinel errors, ValidationError

  application/            use cases / orchestration (no IO interfaces leak)
    service.go             NotificationService (Submit, Get, List)
    dispatcher.go          async worker pool, retry with backoff
    validation/
      pipeline.go          composable pipeline (serial + parallel)
      content.go           recipient + message validation
      preferences.go       user opt-out check
      preferences_store.go in-memory preference store
      ratelimit.go         rate-limit validator + token bucket

  infrastructure/         adapters
    repository/memory.go   thread-safe in-memory store + sweeper
    providers/provider.go  Provider interface, simulated provider, registry
    httpapi/               HTTP layer (handlers + router with logging)

  config/                 env-based configuration loader
```

### Concurrency model

1. The HTTP layer is synchronous: `Submit` validates the request, persists
   one notification per recipient and **enqueues** them in a bounded
   channel, returning `202 Accepted` immediately.
2. A pool of N workers (`WORKER_COUNT`) consumes from the queue.
3. For each notification, a worker runs the validation pipeline, then
   dispatches the notification to a provider with exponential backoff +
   jitter, capped at `MAX_ATTEMPTS`.
4. Per-attempt timeouts (`PER_ATTEMPT_TIMEOUT`) protect against providers
   that hang.
5. The status of each notification is persisted in the repository after
   every state transition so the API is fully observable mid-flight.
6. On shutdown, the queue is closed, workers drain remaining jobs with a
   bounded `SHUTDOWN_TIMEOUT`, and the HTTP server performs graceful
   `Shutdown` (in-flight requests finish; new ones are rejected).

### Validation pipeline

Validators implement a tiny interface:

```go
type Validator interface {
    Name() string
    Validate(ctx context.Context, n *domain.Notification) error
}
```

The pipeline is composable (`NewPipeline(v1, v2, ...)`). The default
pipeline (built in `main.go`) runs:

1. `ContentValidator` (recipient + message format)
2. `PreferenceValidator` (user opt-out for the given channel)
3. `RateLimitValidator` (token bucket per `channel:recipient`)

If any validator fails, the notification moves directly to `rejected` and
no provider call is made. The pipeline supports both serial and parallel
execution (cancelling siblings on first error), so adding a new validator
is a one-line change.

### Provider abstraction

External providers implement:

```go
type Provider interface {
    Name() string
    Channels() []domain.Channel
    Send(ctx context.Context, n *domain.Notification) error
}
```

A `Registry` indexes providers by channel. To add a new provider (e.g.
SendGrid, Twilio, FCM) implement the interface, register it in `main.go`
and the dispatcher will pick it up. The simulated provider is included
for local runs and is used in the smoke test.

### Reliability features

- **Exponential backoff with jitter** between retries (`BASE_BACKOFF`
  doubled each attempt, capped at `MAX_BACKOFF`, ±20% jitter).
- **Per-attempt timeout** enforced via `context.WithTimeout`.
- **Bounded queue** prevents memory blow-up under traffic bursts; if the
  queue is full, `Enqueue` returns an error and the notification is
  persisted as `failed` with reason `service overloaded`.
- **Graceful shutdown** drains in-flight notifications, finishes HTTP
  requests, and exits within `SHUTDOWN_TIMEOUT`.
- **Status persistence** on every transition so observers always see
  consistent state.

### Extensibility checklist

- New channel: add a value to `domain.Channel`, add recipient/message
  rules in `domain.ValidateRecipient` / `domain.ValidateMessage`, register
  a provider.
- New validator: implement `validation.Validator` and append it to the
  pipeline in `main.go`.
- New storage: implement `repository.NotificationRepository` and swap it
  in `main.go`.
- New HTTP transport: implement against the same `application.NotificationService`.

## Design decisions

| Decision | Rationale |
| --- | --- |
| Go stdlib only (no router, no uuid, no logger libs) | Reduces dependency surface; faster builds; nothing here needs a third party. |
| Hexagonal layout (`domain` / `application` / `infrastructure`) | Each layer testable in isolation; replacements (e.g. Postgres, Kafka) are local changes. |
| Async worker pool with bounded queue | Predictable resource usage under burst traffic. The HTTP layer never blocks on providers. |
| Per-attempt timeout + retries with jitter | Standard pattern for unreliable third-party APIs. Jitter prevents thundering herd. |
| Single repository abstraction | Today: in-memory. Tomorrow: Postgres or Redis by writing one adapter. |
| Pipeline pattern for validation | Independent, additive, easy to add/remove validators without touching business logic. |
| `context.Context` everywhere | Cancellation, deadlines, request-scoped values propagate cleanly. |
| `slog` structured logging | Stdlib, JSON-friendly, ready for log aggregation. |
| Go 1.22 ServeMux patterns | Avoids the need for a router while still supporting method routing. |

## Assumptions and trade-offs

- **In-memory storage.** A single instance is fine for the assessment and
  keeps the service self-contained. For production, swap in a persistent
  store behind `repository.NotificationRepository`.
- **Status is read-after-write on every transition.** Acceptable for the
  scale of this assessment; in a higher-throughput system a write-through
  cache or event log would be preferable.
- **One provider per channel.** The registry supports multiple providers
  per channel (it's a slice); the dispatcher currently picks the first.
  A more sophisticated selection (round-robin, least-latency, fallback
  chain) can be added without changing the abstraction.
- **Per-recipient rate limiting, not per-channel-global.** Avoids noisy
  users penalising quiet ones. A global per-channel limiter can be added
  with the same `RateLimiter` interface.
- **Parallel validation is available but not used by default.** Most
  validators are CPU-cheap and the serial form gives clearer error
  ordering. `Pipeline.RunParallel` is available for the case where a
  validator does I/O.
- **Synchronous API, asynchronous delivery.** Submit returns 202 to
  reflect that notifications are processed asynchronously. This is the
  model that scales.

## Future improvements

- **Persistent storage** (Postgres + an outbox for guaranteed delivery).
- **Pluggable provider selection** (weighted round-robin, fallback
  chain, circuit breaker per provider).
- **Idempotency keys** at the API layer for exactly-once semantics.
- **Dead-letter queue** for failed notifications with manual replay.
- **Metrics** (Prometheus) for queue depth, retry count, provider
  success/failure rate, validation rejection reasons.
- **Tracing** (OpenTelemetry) with propagation through the pipeline.
- **Webhook callbacks** for `delivered` / `failed` terminal events.
- **Per-tenant rate limits and quotas** via a richer `RateLimiter`.
- **Schema migration tooling** when the persistent store is introduced.
- **Graceful restart** with a persistent queue (Kafka, NATS, SQS).
