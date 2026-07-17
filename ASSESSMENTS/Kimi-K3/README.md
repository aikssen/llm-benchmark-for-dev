# Notification Service

Backend service that orchestrates the delivery of notifications across
multiple channels (email, SMS, push), built to be the first
production-ready version of a service a team will maintain and extend.

- **Language:** Go (stdlib only — zero third-party dependencies)
- **Style:** asynchronous processing, fail-fast validation pipeline,
  resilient provider calls (timeout + retry/backoff + circuit breaker),
  bounded queues, graceful shutdown.

---

## Quick start

Requires Go 1.22+ (developed on Go 1.26).

```bash
make run        # build and start on :8080
# or
go build -o bin/server ./cmd/server && ./bin/server
```

Run the tests:

```bash
make test       # go test -race ./...
```

Try it:

```bash
# Submit (one or many recipients)
curl -X POST localhost:8080/api/v1/notifications \
  -H 'Content-Type: application/json' \
  -d '{"channel":"email","recipients":["alice@example.com"],"content":{"subject":"Hi","body":"Hello"}}'

# Check status
curl localhost:8080/api/v1/notifications/<id>

# List (filters: status, channel, request_id, limit, offset)
curl 'localhost:8080/api/v1/notifications?status=delivered&channel=email'
```

### Simulated behaviour knobs (useful for the demo)

| Recipient contains    | Effect                                                        |
|-----------------------|---------------------------------------------------------------|
| `optout`              | rejected by the preferences validator (all channels)          |
| `optout-<channel>`    | rejected by preferences only on that channel                  |
| `undeliverable`       | provider returns a **permanent** error (no retries)           |
| anything else         | random latency 20–150 ms, 30 % transient errors (default)     |

Transient errors are absorbed by retries, so most notifications end up
`delivered` after 1–4 attempts.

---

## API

Base path: `/api/v1`. All errors use the envelope
`{"error": {"code": "...", "message": "..."}}`.

### `POST /api/v1/notifications`

Submits a notification for **one or many recipients**. One notification
record is created per recipient; all share the returned `request_id`.

```json
{
  "channel": "email",                    // email | sms | push
  "recipients": ["a@example.com", "b@example.com"],
  "content": {"subject": "Hi", "body": "Hello"}
}
```

* `202 Accepted` — queued for processing. Body:
  `{"request_id": "...", "notifications": [{"id","recipient","channel","status"}]}`
* `400` — malformed request (bad JSON, unknown channel, 0 or >100
  recipients, empty body).
* `503` + `Retry-After: 1` — processing queue full (backpressure).

### `GET /api/v1/notifications/{id}`

Returns the full notification, including `status`, `attempts` and
`last_error`.

### `GET /api/v1/notifications`

Query params: `status`, `channel`, `request_id`, `limit` (default 50,
max 200), `offset`. Returns `{"items": [...], "count", "limit", "offset"}`.

### `GET /healthz`

Liveness probe.

### Notification lifecycle

```
pending ──▶ validating ──▶ sending ──▶ delivered   (terminal)
               │               │
               ▼               ▼
            rejected        failed              (terminal)
```

* `rejected` — a validation failed; `last_error` explains why.
* `failed` — delivery attempts exhausted or permanent provider error.

---

## Architecture

```
            ┌────────────────────────── cmd/server (composition root)
            │
 HTTP API ──┤   POST 202 ─▶ store (pending) ─▶ EnqueueBatch(ids)
 (httpapi)  │                                   │ bounded channel
            │   GET status/list ◀── store       ▼
            │                          ┌─────────────────────┐
            │                          │ dispatcher: N workers│
            │                          │  1. validation pipeline (fail-fast)
            │                          │  2. provider.Send    │
            │                          └─────────┬───────────┘
            │                                    ▼
            │                 ResilientProvider (timeout, retry/backoff,
            │                 circuit breaker) ▶ Provider (simulated)
            ▼
        store.Store  ◀── interface; in-memory impl today, DB tomorrow
```

Packages (under `internal/`):

| Package       | Responsibility |
|---------------|----------------|
| `domain`      | Core types: `Notification`, `Channel`, `Status`. No dependencies. |
| `httpapi`     | HTTP handlers, middleware (request ID, logging, recovery), JSON envelope. |
| `dispatcher`  | Bounded queue + fixed worker pool; drives the lifecycle of each notification. |
| `validation`  | `Validator` interface + concurrent fail-fast `Pipeline` + the four validators. |
| `provider`    | `Provider` interface, `Registry`, simulated providers, `ResilientProvider` (retry/timeout/circuit breaker). |
| `store`       | `Store` interface + goroutine-safe in-memory implementation. |
| `ratelimit`   | Per-key token bucket used by the rate-limit validator. |
| `preferences` | Simulated user-preference (opt-out) service behind an interface. |
| `config`      | Environment-based configuration with defaults. |

The composition root (`cmd/server/main.go`) is the only place that knows
how everything is wired.

---

## Design decisions

### 1. Asynchronous processing with 202 Accepted

Delivery can take seconds (retries, slow providers), so the API accepts
work, persists it as `pending`, enqueues it, and returns immediately.
Clients poll the status endpoint. This is the standard pattern for
notification systems and keeps request latency independent of provider
behaviour.

### 2. Bounded queue + fixed worker pool (bursts, predictable resources)

* The queue is a buffered channel (`NOTIF_QUEUE_CAPACITY`, default 1024)
  and the pool has a fixed size (`NOTIF_WORKERS`, default 8). Memory and
  concurrency stay bounded no matter how bursty the traffic is.
* When the queue is full the API applies **backpressure**
  (`503 + Retry-After`) instead of growing memory. Enqueueing is
  **all-or-nothing per request**: a batch is either fully accepted or
  fully rejected, so a request never ends up half-processed.
* One request with N recipients becomes N independent notifications that
  are processed in parallel by the pool — a single bad recipient never
  blocks the others, and per-recipient status stays accurate.

### 3. Fail-fast concurrent validation pipeline

Requirements state that validations are independent and that once one
fails, the rest must stop consuming resources. The pipeline therefore
runs validators **concurrently** sharing a context; the first failure
cancels the context and every validator returns promptly (the
`Validator` contract requires honouring cancellation). A test
(`TestPipelineFailFastCancelsSlowValidators`) proves a blocked validator
is interrupted mid-flight.

Shipped validators: recipient format (per channel), content rules
(per channel), user preferences/opt-out, per-recipient rate limit.

### 4. Resilient provider calls

Each provider is wrapped in `ResilientProvider`:

* **Per-attempt timeout** (`NOTIF_PROVIDER_TIMEOUT`) — slow providers
  can't occupy a worker forever.
* **Retry with exponential backoff + jitter** (`NOTIF_RETRY_*`) — only
  for errors classified *temporary*; permanent errors (e.g. unknown
  recipient) fail immediately. Attempts are recorded on the notification.
* **Circuit breaker** (`NOTIF_BREAKER_*`) — after N consecutive failures
  calls fail fast for a cool-down window, then a single half-open probe
  decides. Prevents wasting worker time on a provider that is down.

### 5. Extensibility: channels and providers

Adding a channel (e.g. WhatsApp) requires:

1. implement `provider.Provider` (~20 lines for a simulated one),
2. register it in `main.go`,
3. add its recipient/content rules to the two validators.

The API accepts the new channel automatically (`Registry.Supports`), the
dispatcher, pipeline, store and status endpoints are channel-agnostic.
Adding a *validation rule* is implementing `Validator` and registering
it in the pipeline. No existing code is modified except the
registration point (Open/Closed principle).

### 6. Storage behind an interface

`store.Store` is deliberately tiny (`Upsert`, `Get`, `List`). The
in-memory implementation returns **copies**, so callers can never mutate
shared state without going through the store (race safety, verified with
`go test -race`). Swapping in Postgres/Redis touches only this package.

### 7. Zero third-party dependencies

The standard library covers routing (Go 1.22 `ServeMux` patterns),
structured logging (`log/slog`), and concurrency. Fewer dependencies =
smaller supply chain and simpler maintenance; nothing was needed that
the stdlib doesn't do well.

---

## Assumptions

* **Recipient-level business validation is asynchronous.** The API
  validates request *shape* synchronously (JSON, known channel, recipient
  count/size, non-empty body) but recipient format, preferences, rate
  limits and content rules run in the pipeline, and their outcome is
  reported via the notification status (`rejected` + reason). Rationale:
  a single uniform validation phase; cheap for clients (one code path),
  and the assignment lists "validating the recipient" as a pipeline step.
* **One notification per recipient.** A multi-recipient request is a
  batch of independent deliveries sharing a `request_id`; delivery
  status is per recipient.
* **Simulated external systems are deterministic where it matters:**
  opt-outs and permanent failures are triggered by documented substrings
  so behaviour is reproducible; latency/transient errors are random
  within configured bounds.
* **In-memory storage is acceptable for v1** (explicitly allowed:
  "external systems may be simulated"). The service is stateful only
  through the `Store` interface.
* **No authentication/tenancy in v1** — out of scope; noted below.

## Trade-offs

* **At-most-once in-memory queue.** If the process crashes, `pending`
  notifications are lost. Accepted for v1 (simulation); the fix is a
  durable store + re-enqueuer (below).
* **Backpressure over unbounded acceptance.** Clients may see 503 during
  extreme bursts; this is deliberate — predictable resource usage was
  prioritised over never saying no.
* **Fail-fast pipeline reports the first failure(s), not all of them.**
  This is exactly the required behaviour (don't consume resources once a
  validation failed); validators racing to fail may yield >1 reason.
* **Rate-limit token is consumed during validation**, even if a later
  validator rejects the message. Slightly conservative counting, much
  simpler than two-phase commit; documented and acceptable.

## Future improvements

1. **Durable storage + outbox/re-enqueuer** — survive restarts; sweep
   `pending`/`sending` records on boot.
2. **Horizontal scaling** — replace the in-process queue with a broker
   (Kafka/SQS/NATS); workers become consumers. The dispatcher interface
   already isolates this.
3. **Idempotency keys** on submission (client-supplied request IDs) to
   make client retries safe.
4. **Per-provider rate limits and bulk APIs** (real providers batch).
5. **Observability** — Prometheus metrics (queue depth, attempt
   histograms, breaker state) and OpenTelemetry tracing; the request-ID
   middleware is the seam.
6. **AuthN/AuthZ and multi-tenancy** for the API.
7. **Scheduled delivery and templates** (send-at, i18n templates).

---

## Configuration

All settings are environment variables (defaults are local-friendly):

| Variable | Default | Meaning |
|----------|---------|---------|
| `NOTIF_PORT` | `8080` | HTTP listen port |
| `NOTIF_WORKERS` | `8` | worker pool size |
| `NOTIF_QUEUE_CAPACITY` | `1024` | bounded queue size |
| `NOTIF_MAX_RECIPIENTS` | `100` | max recipients per request |
| `NOTIF_SHUTDOWN_DRAIN` | `10s` | graceful shutdown budget |
| `NOTIF_PROVIDER_TIMEOUT` | `2s` | per-attempt provider timeout |
| `NOTIF_RETRY_MAX_ATTEMPTS` | `4` | delivery attempts per notification |
| `NOTIF_RETRY_BASE_DELAY` / `NOTIF_RETRY_MAX_DELAY` | `100ms` / `2s` | backoff bounds |
| `NOTIF_BREAKER_THRESHOLD` / `NOTIF_BREAKER_RESET_WAIT` | `5` / `5s` | circuit breaker knobs |
| `NOTIF_RATE_LIMIT` / `NOTIF_RATE_LIMIT_WINDOW` | `10` / `1m` | per-recipient limit |
| `NOTIF_SIM_MIN_LATENCY` / `NOTIF_SIM_MAX_LATENCY` | `20ms` / `150ms` | simulated provider latency |
| `NOTIF_SIM_ERROR_RATE` | `0.30` | transient error probability per attempt |

## Project layout

```
cmd/server/            composition root + graceful shutdown
internal/
  domain/              core types
  httpapi/             HTTP handlers & middleware
  dispatcher/          queue + worker pool
  validation/          validators + fail-fast pipeline
  provider/            provider iface, registry, retry, breaker, simulated
  store/               store iface + in-memory impl
  ratelimit/           token bucket
  preferences/         simulated opt-out service
  config/              env configuration
```
