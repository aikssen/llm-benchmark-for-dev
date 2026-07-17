# notification-service

A production-ready backend service that orchestrates notification delivery
across multiple channels (email, SMS, push), designed for extensibility and
reliable best-effort delivery against unreliable external providers.

The implementation language is **Go** (standard library + `golang.org/x/sync`).

---

## Table of contents

- [Quick start](#quick-start)
- [API](#api)
- [Configuration](#configuration)
- [Architecture](#architecture)
- [Notification processing](#notification-processing)
- [Reliability: providers, retry, circuit breaker](#reliability-providers-retry-circuit-breaker)
- [Concurrency and resource usage](#concurrency-and-resource-usage)
- [Project layout](#project-layout)
- [Testing](#testing)
- [Design decisions](#design-decisions)
- [Assumptions](#assumptions)
- [Trade-offs](#trade-offs)
- [Future improvements](#future-improvements)

---

## Quick start

Requirements: Go 1.22+ (developed with Go 1.26).

```bash
# build
make build            # produces ./bin/notification-service

# run (defaults are sane; no config required)
make run              # or: ./bin/notification-service

# in another terminal
curl -s localhost:8080/api/v1/health
```

Submit a notification:

```bash
curl -s -X POST localhost:8080/api/v1/notifications \
  -H 'Content-Type: application/json' \
  -d '{
        "channel": "email",
        "recipients": [
          {"id": "u1", "email": "alice@example.com"},
          {"id": "u2", "email": "bob@example.com"}
        ],
        "message": {"subject": "Welcome", "body": "Hello there!"}
      }'
```

Response (`202 Accepted`):

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "channel": "email",
  "status": "accepted",
  "recipients": [...],
  "message": {...},
  "results": [
    {"recipient_id": "u1", "status": "accepted", ...},
    {"recipient_id": "u2", "status": "accepted", ...}
  ],
  "created_at": "...",
  "updated_at": "..."
}
```

Poll the status (processing is asynchronous):

```bash
curl -s localhost:8080/api/v1/notifications/550e8400-e29b-41d4-a716-446655440000
```

List notifications:

```bash
curl -s 'localhost:8080/api/v1/notifications?limit=10&offset=0'
```

---

## API

Versioned under `/api/v1`.

| Method | Path                            | Description                              |
|--------|---------------------------------|------------------------------------------|
| POST   | `/notifications`                | Submit a notification request            |
| GET    | `/notifications/{id}`           | Retrieve the status of one notification  |
| GET    | `/notifications?limit&offset`   | List notifications (newest first)        |
| GET    | `/health`                       | Liveness probe                           |

### Request body (`POST /notifications`)

```jsonc
{
  "channel": "email | sms | push",
  "recipients": [
    {
      "id": "user-id",        // required; used for preferences + rate limiting
      "email": "...",         // required for email
      "phone": "...",         // required for sms (E.164-like, e.g. +14155550123)
      "push_token": "..."     // required for push
    }
  ],
  "message": {
    "subject": "...",         // required for email
    "body": "...",            // required for email/sms; optional for push if data present
    "data": { "key": "val" }  // optional; extra payload for push
  }
}
```

### Status values

Overall: `accepted`, `processing`, `delivered`, `partial`, `failed`.
Per recipient: `accepted`, `sending`, `delivered`, `failed`,
`validation_failed`.

`partial` means some recipients were delivered and some failed.

### Errors

Errors are returned as a structured body with a stable `code`:

```json
{
  "error": {
    "code": "invalid_email",
    "message": "email address is not valid",
    "validation_errors": [
      {"code": "invalid_email", "field": "email", "message": "email address is not valid"}
    ]
  }
}
```

| HTTP | code                 | Meaning                                            |
|------|----------------------|----------------------------------------------------|
| 400  | `invalid_json`       | Malformed/oversized JSON body                       |
| 400  | `unsupported_channel`| Channel not supported                               |
| 400  | `missing_recipients` | Empty recipient list                                |
| 404  | `not_found`          | Notification id does not exist                      |
| 503  | `queue_full`         | Processing queue full (backpressure; retry later)   |
| 500  | `internal_error`     | Unexpected error (logged with request id)           |

Per-recipient validation failures are **not** HTTP errors: the request is still
accepted (`202`) and the failing recipients are reported in `results` with
`validation_errors`. This reflects that a multi-recipient request is partially
successful and the client should poll for the per-recipient outcome.

---

## Configuration

All settings come from environment variables with safe defaults (see
`internal/config/config.go`). The service runs with no configuration.

| Variable                  | Default        | Description                                  |
|---------------------------|----------------|----------------------------------------------|
| `HTTP_ADDR`               | `:8080`        | HTTP listen address                          |
| `QUEUE_CAPACITY`          | `1024`         | Bounded queue size (backpressure threshold)  |
| `WORKERS`                 | `8`            | Queue worker goroutines                      |
| `RECIPIENT_CONCURRENCY`   | `16`           | Max recipients processed in parallel per job |
| `SEND_TIMEOUT`            | `5s`           | Per-send timeout                             |
| `RETRY_ATTEMPTS`          | `3`            | Total send attempts (incl. first)            |
| `RETRY_BASE`              | `100ms`        | Base backoff                                 |
| `RETRY_MAX`               | `2s`           | Max backoff                                  |
| `CIRCUIT_FAILURES`        | `5`            | Consecutive failures to open the circuit     |
| `CIRCUIT_COOLDOWN`        | `10s`          | Open-circuit cooldown before a probe         |
| `RATE_LIMIT_WINDOW`       | `1m`           | Per-recipient rate-limit window              |
| `RATE_LIMIT_MAX`          | `60`           | Max sends per recipient per window per channel|
| `LIST_DEFAULT_LIMIT`      | `20`           | Default page size for list                   |
| `LIST_MAX_LIMIT`          | `100`          | Max page size for list                       |
| `PROVIDER_FAILURE_PROB`   | `0.1`          | Simulated provider transient failure prob.   |
| `PROVIDER_SLOW_PROB`      | `0.1`          | Simulated slow-response prob.                |
| `PROVIDER_SLOW_DELAY`     | `2s`           | Extra delay for slow responses               |
| `PROVIDER_MIN_DELAY`      | `20ms`         | Baseline provider latency                    |
| `LOG_LEVEL`               | `info`         | debug \| info \| warn \| error               |

See `.env.example` for a ready-to-source file.

---

## Architecture

The service follows a layered, dependency-inverted design (hexagonal-ish).
Dependencies point inward: transport and infrastructure depend on the domain
and on ports (interfaces); the domain depends on nothing.

```
cmd/server            composition root + lifecycle (graceful shutdown)
internal/domain       core entities, statuses, errors (no dependencies)
internal/validation   validation pipeline + validators (depends on domain)
internal/provider     Provider interface + middleware (retry, CB, timeout)
internal/channel      channel -> provider mapping + registry
internal/processing   bounded queue, worker pool, per-recipient dispatch
internal/storage      Repository interface + in-memory implementation
internal/preference   in-memory preference store (implements a port)
internal/service      use cases (submit/get/list) - composes storage + queue
internal/transport/http REST/JSON handlers, middleware, router (stdlib)
internal/config       env-driven configuration
internal/observability structured logging (log/slog)
internal/id           collision-resistant id generation
```

### Adding a new channel

1. Implement `channel.Channel` (map the recipient address + message to a
   `provider.Outbound` and call a `Provider`), or reuse an existing channel
   with a new provider.
2. Register it in `cmd/server/main.go`:
   ```go
   channels.Register(channel.NewFoo(buildProvider("foo-simulator")))
   ```
3. Add the channel to the `supported` list passed to the service so the API
   accepts it.

No existing channel or validator needs to change.

### Adding a new validation

Implement `validation.Validator` and append it to the pipeline in the
composition root. Validators are independent and run concurrently with
fail-fast, so no change to the pipeline is needed.

---

## Notification processing

Before delivery, a set of **independent** validations runs per recipient:

- **recipient** — channel-specific address validity (email format, E.164-like
  phone, push token presence).
- **content** — message payload rules per channel (email needs subject, SMS
  body length, push needs body or data).
- **preferences** — recipient opt-in for the channel.
- **rate limit** — per-recipient + per-channel sliding-window limit.

### Fail-fast with concurrent validators

> *"If one validation fails, the remaining validations should not continue
> consuming unnecessary resources."*

Validators are independent, so the `Pipeline` runs them concurrently with
`errgroup.WithContext`. The first failure cancels the shared context, and every
validator respects `ctx` so it aborts promptly instead of running to
completion. `TestPipelineFailFast` verifies a slow validator is cancelled when a
fast one fails.

A notification is delivered to a recipient **only if all validations pass**.

---

## Reliability: providers, retry, circuit breaker

External providers are unreliable (slow, transient failures, unavailable). The
`Provider` interface is wrapped with composable middleware:

```
circuit-breaker( retry( timeout( attempts( baseProvider ) ) ) )
```

- **timeout** — bounds each send (`SEND_TIMEOUT`).
- **retry** — exponential backoff with jitter; skips permanent errors
  (`provider.Permanent`); respects ctx so cancellation aborts pending waits.
- **circuit breaker** — opens after N consecutive failures, fast-fails with
  `ErrCircuitOpen` for the cooldown, then allows a single probe (half-open);
  closes on success. Prevents hammering an unavailable provider.
- **attempts** — innermost wrapper counts actual provider calls per logical
  send (carried via context) so per-recipient `attempts` is reported.

Providers are simulated (`provider.Simulator`) with configurable failure/slow
probabilities to exercise these paths. Real vendors (e.g. SES, Twilio, FCM)
drop in by implementing `Provider` and wrapping with the same middleware.

---

## Concurrency and resource usage

- **Bounded queue + backpressure**: a fixed-capacity channel buffers
  notification ids. When full, `POST` returns `503 queue_full` immediately
  rather than queueing unbounded work — this is how bursts are absorbed without
  runaway memory.
- **Fixed worker pool**: a configurable number of goroutines drains the queue,
  bounding CPU/goroutine usage.
- **Bounded per-notification fan-out**: within a notification, recipients are
  processed concurrently up to `RECIPIENT_CONCURRENCY` (semaphore). One
  recipient's failure never affects another (we deliberately do **not** use
  `errgroup` here, since it cancels siblings on first error).
- **Repository snapshots**: the in-memory repository returns deep copies on
  read so concurrent processor writes never race with HTTP reads (verified with
  `go test -race`).

---

## Project layout

See the [Architecture](#architecture) section. Tests live next to the code
(`*_test.go`).

---

## Testing

```bash
make test            # unit + integration tests
make test-race       # with the race detector
make cover           # coverage report (HTML)
make vet             # go vet
make fmt             # go fmt
```

Highlights:

- `validation` — each validator + pipeline fail-fast (slow validator cancelled)
  + ctx cancellation.
- `provider` — retry succeeds after transient failures, exhausts on permanent
  errors, respects ctx; circuit breaker opens/closes/half-open; attempts
  recorded; timeout cancels slow providers.
- `processing` — end-to-end delivery of multiple recipients, validation
  failures (partial), delivery failures with retry attempts, circuit-breaker
  fast-fail, queue backpressure.
- `transport/http` — full in-process stack via `httptest`: create/get/list,
  404, unsupported channel, invalid JSON, validation errors reported, health,
  panic recovery.
- All packages pass `go test -race`.

---

## Design decisions

- **Dependency inversion via small interfaces defined by consumers.**
  `validation.PreferenceStore`, `validation.RateLimiter`, `storage.Repository`,
  and `provider.Provider` are declared where they are consumed, following the
  Go "accept interfaces, return structs" idiom. This keeps packages decoupled
  and substitutes cheap (e.g. swap the in-memory repo for SQL).
- **Async processing with synchronous acknowledgment.** `POST` persists the
  notification and enqueues it, returning `202` immediately. This decouples
  ingest latency from delivery latency and lets bursts be smoothed by the
  bounded queue. Status is observed by polling `GET`.
- **Composition root in `main.go`.** All wiring (which validators, which
  channels, which middleware) happens in one place; packages don't know about
  each other's concrete types.
- **Stable error codes.** Validation and HTTP errors carry machine-readable
  `code`s so clients can react programmatically instead of parsing messages.
- **Standard library first.** HTTP routing uses the Go 1.22+
  method-pattern `ServeMux`; logging uses `log/slog`. The only external
  dependency is `golang.org/x/sync/errgroup`, used specifically for the
  fail-fast concurrent validation semantics.
- **Per-recipient results as the source of truth.** The aggregate status is
  derived from recipient results, so multi-recipient requests report partial
  outcomes rather than a single misleading status.

---

## Assumptions

- **Opt-in by default.** Recipients without explicit preference entries are
  allowed on all channels (the `MemoryStore` is initialized with `allowAll`).
  A real deployment would likely default to the channel set collected at
  sign-up. Configurable in `preference.NewMemoryStore`.
- **Single-process, in-memory state** for this deliverable. The repository
  interface is designed so a SQL/Redis implementation can replace it without
  touching business logic.
- **Polling for status.** No webhook/SSE push of status updates in this
  version; clients poll `GET /notifications/{id}`.
- **E.164-like phone validation** (regex `^\+?[1-9]\d{6,14}$`) is a pragmatic
  approximation, not full libphonenumber validation.
- **Idempotency / deduplication** is not implemented; clients should not retry
  `POST` blindly (the response gives the assigned id). See Future improvements.
- **Rate limiting** is per-recipient + per-channel in-memory sliding window;
  not distributed.

---

## Trade-offs

- **In-memory storage** keeps the solution self-contained and runnable with no
  external services, at the cost of durability and horizontal scale. The
  `Repository` interface is the escape hatch.
- **Polling vs. push:** polling is simpler and good enough for a first version;
  it adds load proportional to client polling frequency. Webhooks/SSE would
  reduce load but add operational complexity.
- **Fail-fast concurrent validation** vs. running all validators to collect
  every error: the requirements explicitly ask to stop consuming resources on
  first failure, so we cancel siblings. We still return any error that raced in
  just before cancellation propagated.
- **Retry at the provider level** (vs. at the processor level) keeps the
  processor simple and makes reliability composable per provider. The downside
  is that per-recipient retry state lives inside the middleware and is not
  resumable across process restarts.
- **Circuit breaker is per provider instance** (shared across recipients). This
  protects a flaky vendor quickly but means one recipient's failures can
  fast-fail others using the same provider — desirable for protecting the
  system, with the trade-off of reduced delivery during outages.

---

## Future improvements

- **Durable persistence** (Postgres) behind the existing `Repository`
  interface, plus a durable work queue (e.g. a `jobs` table or Kafka) so
  in-flight notifications survive restarts.
- **Idempotency keys** on `POST` so safe client retries don't create duplicate
  notifications.
- **Webhooks / SSE** for status updates to remove polling load.
- **Per-recipient retry queues with exponential backoff and jitter across
  minutes/hours** for truly transient outages, decoupled from the synchronous
  request path.
- **Observability**: Prometheus metrics (queue depth, worker utilization,
  per-provider success/latency, circuit state), distributed tracing.
- **Authentication / authorization**, per-tenant isolation, and rate limiting
  at the API ingress.
- **Real provider implementations** (SES/SendGrid, Twilio, FCM/APNS) behind the
  same `Provider` interface, with per-provider config and credentials.
- **Content templating** and localization.
- **Dead-letter queue** for notifications that exhaust retries, with an admin
  replay API.
- **Stricter phone validation** (libphonenumber) and email deliverability
  checks (MX/SMTP probe) where appropriate.
