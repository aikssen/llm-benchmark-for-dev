# Notification Service

A backend service that orchestrates notification delivery across multiple
channels (email, SMS, push), designed for unreliable external providers and
bursty traffic.

## Quick start

Requirements: Go 1.25+ (no other dependencies; external systems are simulated).

```bash
make run            # starts the service on :8080
make test           # runs all tests with the race detector
make check          # gofmt + go vet + tests
```

Or with Docker:

```bash
docker build -t notification-service .
docker run --rm -p 8080:8080 notification-service
```

### Try it

```bash
# Submit a notification (multiple recipients)
curl -s -X POST localhost:8080/v1/notifications \
  -H 'Content-Type: application/json' \
  -d '{
    "channel": "email",
    "recipients": ["ada@example.com", "grace@example.com"],
    "message": {"subject": "Welcome", "body": "Hello!"}
  }' | jq

# Check its status (use the id from the previous response)
curl -s localhost:8080/v1/notifications/<id> | jq

# List notifications
curl -s 'localhost:8080/v1/notifications?limit=10&status=delivered' | jq
```

## API

| Method | Path                     | Description                                            |
|--------|--------------------------|--------------------------------------------------------|
| POST   | `/v1/notifications`      | Submit a notification. Returns `202 Accepted`.         |
| GET    | `/v1/notifications/{id}` | Current status, including per-recipient deliveries.    |
| GET    | `/v1/notifications`      | List notifications. `limit`, `offset`, `status`, `channel`. |
| GET    | `/healthz`               | Liveness probe.                                        |

**Submit request:**

```json
{
  "channel": "email | sms | push",
  "recipients": ["ada@example.com"],
  "message": {"subject": "optional except for email", "body": "required"}
}
```

**Notification response** (submit / get / list items):

```json
{
  "id": "ntf_9f2c...",
  "channel": "email",
  "status": "pending | processing | delivered | partially_delivered | failed",
  "message": {"subject": "Welcome", "body": "Hello!"},
  "created_at": "2026-07-06T12:00:00Z",
  "deliveries": [
    {
      "id": "dlv_a1b2...",
      "recipient": "ada@example.com",
      "status": "pending | processing | delivered | failed",
      "attempts": 1,
      "error": "present when failed",
      "updated_at": "2026-07-06T12:00:01Z"
    }
  ]
}
```

**Errors** are always `{"error": {"code": "...", "message": "..."}}`. Notable
codes: `invalid_request` / `invalid_body` (400), `not_found` (404),
`queue_full` (429, with `Retry-After`).

## Architecture

```
                 ┌────────────────────────────────────────────────────┐
 POST /v1/…  ───►│ API: structural validation, persist, enqueue       │
                 │  └── 429 + Retry-After when the queue is full      │
                 └───────────────┬────────────────────────────────────┘
                                 │ 1 job per recipient (IDs only)
                                 ▼
                 ┌────────────────────────────────────────────────────┐
                 │ Bounded queue (QUEUE_CAPACITY)                     │
                 └───────────────┬────────────────────────────────────┘
                                 │ fixed worker pool (WORKERS)
                                 ▼
                 ┌────────────────────────────────────────────────────┐
                 │ Per delivery:                                      │
                 │  1. validations — concurrent, fail-fast            │
                 │     (recipient, content, preferences, rate limit)  │
                 │  2. provider send — retries w/ exponential backoff │
                 │     + jitter; permanent errors are not retried     │
                 │  3. terminal status recorded in the store          │
                 └───────────────┬────────────────────────────────────┘
                                 ▼
                 ┌────────────────────────────────────────────────────┐
                 │ Provider registry: email / sms / push (simulated)  │
                 └────────────────────────────────────────────────────┘
```

Packages (`internal/`):

- **`domain`** — core entities (`Notification`, `Delivery`, statuses); no
  dependencies on other layers.
- **`api`** — HTTP transport: routing, DTOs, structural validation,
  backpressure handling, logging/recovery middleware.
- **`worker`** — bounded queue + fixed worker pool; retry/backoff policy;
  the delivery state machine.
- **`validation`** — the fail-fast concurrent validation pipeline and the
  individual validators.
- **`provider`** — the provider interface, per-channel registry,
  permanent-vs-transient error classification, and the simulated providers.
- **`store`** — the persistence interface and an in-memory implementation.
- **`config`** — environment configuration with defaults.

## Design decisions

### Asynchronous processing with explicit backpressure

Submissions return `202 Accepted` immediately; delivery happens in the
background. Given slow/unreliable providers, holding HTTP connections open
while retrying for seconds is not viable.

Bursts are absorbed by a **bounded** queue drained by a **fixed** pool of
workers. Memory and provider concurrency therefore have a hard ceiling
regardless of traffic — "predictable resource usage". When a burst exceeds
capacity the API sheds load explicitly (`429` + `Retry-After`) instead of
degrading silently. Enqueueing is all-or-nothing per request: a request is
either fully accepted or fully rejected (and rolled back), never half-queued.

### The delivery (notification × recipient) is the unit of work

A multi-recipient request fans out into one `Delivery` per recipient, each
with its own status, attempts and error. Recipients fail or succeed
independently — one invalid email doesn't block the other 99 — and the
notification exposes an aggregate status (`partially_delivered` when
outcomes are mixed) derived from its deliveries.

### Fail-fast concurrent validations

Validations are independent, so they run concurrently per delivery
(`errgroup`). The first failure cancels the shared context, and every
validator honors cancellation, so remaining checks stop consuming resources
— this is the exact semantics the requirement asks for. Validation failures
are terminal (no retry): retrying won't fix an opted-out recipient.

Cheap *structural* validation (valid channel, non-empty body, recipient
count) still happens synchronously in the API so obviously malformed
requests fail with a `400` instead of occupying queue capacity.

### Retries: transient vs permanent errors

Providers signal permanent failures (hard bounce, invalid recipient) by
wrapping errors with `provider.Permanent`. The worker retries only transient
errors, with exponential backoff capped at a maximum and ±25% jitter to avoid
synchronized retry storms against an already struggling provider. Every send
attempt has its own timeout so a hung provider cannot pin a worker.

### Extensibility seams

- **New channel/provider**: implement the 3-method `provider.Provider`
  interface and register it in `main.go`. Nothing else changes.
- **New validation**: implement `validation.Validator` and add it to the
  pipeline in `main.go`.
- **Real database**: implement the `store.Store` interface; the in-memory
  version exists behind it already.
- **External queue**: jobs carry only IDs (state lives in the store), so the
  channel-based queue can be replaced by SQS/Kafka/etc. without changing job
  semantics.

### Technology choices

Standard library first: `net/http` with Go 1.22+ method routing (no web
framework), `log/slog` for structured logging, `sync`/channels for
concurrency. The only third-party dependency is `golang.org/x/sync/errgroup`.
For a service this size a framework adds surface area without solving a
problem we have.

## Assumptions

- **A notification targets one channel**; multi-channel fan-out can be
  composed client-side or added later as a routing layer above this API.
- **At-most-once delivery per attempt scope**: with the in-memory store,
  jobs queued (not yet finished) at shutdown are dropped, and a crash loses
  state. Accepted for this exercise; the durable path is documented below.
- **Simulated external systems**: providers (configurable latency and
  transient/permanent failure rates) and the preference service (static
  opt-out list, `OPTED_OUT_RECIPIENTS`) are simulations behind real
  interfaces.
- **Rate limiting protects recipients** (N per recipient per channel per
  window), not API clients; client throttling/auth is out of scope.
- **No authentication**: assumed to run behind a gateway that handles it.

## Trade-offs

- **In-memory store and queue** keep the project runnable with zero
  dependencies and make the interfaces the contract. The cost is durability;
  see future improvements.
- **Reject-on-full (429)** instead of blocking submitters: bounded latency
  and honest signaling under overload, at the cost of pushing retry
  responsibility to clients (mitigated by `Retry-After`).
- **Offset pagination** is simple and fine at this scale; a cursor would be
  needed for large datasets.
- **Validation runs in the worker**, not the API: keeps submits fast and
  uniform, but means an invalid recipient is only visible via the status
  endpoint, not at submit time.
- **One provider per channel** keeps routing trivial; provider failover is
  an explicit future improvement.

## Testing strategy

- **Unit tests** per package: aggregate status derivation, every validator
  (table-driven), retry/backoff classification, registry, store semantics
  (including copy-on-read isolation).
- **Concurrency-sensitive tests**: the fail-fast pipeline test asserts that
  a failure actually cancels the slow validator; queue tests assert atomic
  all-or-nothing enqueueing; everything runs under `-race` in CI/`make test`.
- **HTTP tests** with `httptest` cover contracts: status codes, error codes,
  deduplication, rollback on backpressure.
- **End-to-end test** wires the real API + workers + pipeline + simulated
  providers and drives submit → process → query, including a 50-request
  burst asserting every request gets either `202` or a clean `429`.

## Configuration

All via environment variables (defaults in parentheses):

| Variable | Default | Description |
|---|---|---|
| `ADDR` | `:8080` | HTTP listen address |
| `WORKERS` | `8` | Concurrent delivery processors |
| `QUEUE_CAPACITY` | `1024` | Max queued deliveries before `429` |
| `MAX_ATTEMPTS` | `4` | Send attempts per delivery (incl. first) |
| `RETRY_BASE_DELAY` / `RETRY_MAX_DELAY` | `200ms` / `5s` | Backoff bounds |
| `VALIDATION_TIMEOUT` | `2s` | Budget for the whole validation pipeline |
| `SEND_TIMEOUT` | `5s` | Budget per provider send attempt |
| `MAX_RECIPIENTS` | `100` | Max recipients per request |
| `MAX_BODY_BYTES` | `1048576` | Max request body size |
| `DEFAULT_PAGE_SIZE` / `MAX_PAGE_SIZE` | `20` / `100` | List pagination |
| `RATE_LIMIT` / `RATE_LIMIT_WINDOW` | `10` / `1m` | Per recipient+channel |
| `PROVIDER_MIN_LATENCY` / `PROVIDER_MAX_LATENCY` | `50ms` / `300ms` | Simulated latency |
| `PROVIDER_TRANSIENT_FAILURE_RATE` | `0.15` | Simulated retryable failures [0,1] |
| `PROVIDER_PERMANENT_FAILURE_RATE` | `0.02` | Simulated hard failures [0,1] |
| `OPTED_OUT_RECIPIENTS` | — | Comma-separated `recipient` or `channel:recipient` |
| `SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown budget |

Example — a chaotic environment to watch retries in the logs:

```bash
PROVIDER_TRANSIENT_FAILURE_RATE=0.6 OPTED_OUT_RECIPIENTS=optout@example.com make run
```

## Future improvements

Roughly in priority order for a real production rollout:

1. **Durable store + outbox**: Postgres implementation of `store.Store`;
   enqueue via a transactional outbox and recover in-flight/queued
   deliveries on startup, upgrading delivery semantics from best-effort to
   at-least-once (providers with idempotency keys would prevent duplicates).
2. **Circuit breaker per provider**: stop hammering a provider that is hard
   down; fail fast and let retries resume after a probe succeeds.
3. **Dead-letter queue** for deliveries that exhausted retries, enabling
   inspection and manual/automated redrive.
4. **Observability**: Prometheus metrics (queue depth, attempts,
   success/failure rates per provider, latency histograms) and OpenTelemetry
   tracing across submit → validate → send.
5. **Provider failover / weighted routing**: multiple providers per channel
   with health-based selection.
6. **Idempotency keys** on submission so client retries of `POST` don't
   double-send.
7. **Distributed rate limiting** (e.g. Redis) once the service scales
   horizontally; per-client API quotas and authentication.
8. **Scheduled/delayed notifications and templates**, both natural
   extensions of the current model.
