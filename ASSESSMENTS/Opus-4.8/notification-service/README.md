# Notification Orchestration Service

A backend service that orchestrates the delivery of notifications across multiple
channels (email, SMS, push), designed to remain reliable in the presence of slow
or failing external providers.

It accepts notification requests over HTTP, validates them, and delivers them
asynchronously through a bounded worker pool with retries, timeouts and circuit
breaking. Clients can query the status of any notification and list previously
submitted ones.

> This is written to be the *first production-ready version* of a service a team
> will maintain and extend: small dependency surface, clear layering, thorough
> tests, and explicit extension points for new channels and validations.

---

## Table of contents

- [Quick start](#quick-start)
- [API](#api)
- [Architecture](#architecture)
- [How the requirements are met](#how-the-requirements-are-met)
- [Configuration](#configuration)
- [Testing](#testing)
- [Design docs](#design-docs)

---

## Quick start

Requirements: **Go 1.26+** (uses the standard library's method-aware routing and
a couple of recent stdlib helpers). No other tooling required.

```bash
# from the repository root
make run          # starts the server on :8080
# or:
go run ./cmd/server
```

You should see structured JSON logs, starting with the worker pool and HTTP
server coming up. Then, in another terminal:

```bash
# Submit a notification to two recipients
curl -s -X POST localhost:8080/v1/notifications \
  -H 'Content-Type: application/json' \
  -d '{
        "channel": "email",
        "recipients": ["alice@example.com", "bob@example.com"],
        "message": { "subject": "Welcome", "body": "Hello there!" }
      }' | jq

# Note an "id" from the response, then poll its status
curl -s localhost:8080/v1/notifications/<id> | jq

# List notifications (filter + paginate)
curl -s 'localhost:8080/v1/notifications?channel=email&limit=10' | jq
```

The default configuration runs the **simulated providers with a 20% transient
failure rate** so you can watch retries and (occasionally) the circuit breaker in
the logs. Set `NS_PROVIDER_FAILURE_RATE=0` for deterministic success while
experimenting. See [Configuration](#configuration).

---

## API

All responses are JSON. Errors use a uniform envelope: `{"error": "..."}`.

### `POST /v1/notifications` — submit

Submits a notification for one or more recipients. Returns **202 Accepted**:
delivery happens asynchronously, so acceptance only means the request was
validated and queued.

Request body:

| Field        | Type       | Notes                                             |
|--------------|------------|---------------------------------------------------|
| `channel`    | string     | `email`, `sms`, or `push`                         |
| `recipients` | string[]   | one or more; each becomes an independent notification |
| `message.subject` | string | optional (ignored by SMS)                       |
| `message.body`    | string | required                                        |

Response `202`:

```json
{
  "request_id": "b1c2...",
  "count": 2,
  "notifications": [
    { "id": "a1...", "recipient": "alice@example.com", "status": "pending", ... },
    { "id": "a2...", "recipient": "bob@example.com",   "status": "pending", ... }
  ]
}
```

A single request fans out into **one notification per recipient**, each with its
own ID and lifecycle, so a failure for one recipient never affects the others.

### `GET /v1/notifications/{id}` — status

Returns the current state of a notification. `404` if unknown.

```json
{
  "id": "a1...",
  "request_id": "b1c2...",
  "channel": "email",
  "recipient": "alice@example.com",
  "message": { "subject": "Welcome", "body": "Hello there!" },
  "status": "delivered",
  "attempts": 2,
  "created_at": "2026-07-06T12:00:00.000Z",
  "updated_at": "2026-07-06T12:00:00.400Z"
}
```

Status lifecycle: `pending → validating → delivering → delivered`, or `→ failed`
at either the validation or delivery stage. `last_error` explains a failure.

### `GET /v1/notifications` — list

Returns notifications newest-first, with optional filtering and pagination.

Query parameters (all optional): `channel`, `status`, `recipient`, `limit`
(default 50), `offset` (default 0).

```json
{ "total": 42, "limit": 50, "offset": 0, "notifications": [ ... ] }
```

### `GET /healthz` — liveness

Returns `200 {"status":"ok"}`.

---

## Architecture

The service is layered so that dependencies point **inward** toward the domain,
and each concern is isolated behind an interface. This is what makes it both
testable and extensible.

```
            HTTP (internal/api)
                  │  translates requests ⇄ responses; no business logic
                  ▼
        Service (internal/service)         ← orchestration / use-cases
          │            │            │
          ▼            ▼            ▼
   Validation      Worker pool    Delivery
   (pipeline)      (queue+pool)   (retry + circuit breaker)
                                     │
                                     ▼
                                  Providers  (email / sms / push, pluggable)

        Store (internal/store)  ← persistence boundary (in-memory impl)
```

Request flow for a submit:

1. **API** decodes/validates the HTTP request and calls the service.
2. **Service** fans the request out into one `Notification` per recipient,
   persists each as `pending`, and hands the IDs to the worker pool. It returns
   immediately (**202**).
3. A **worker** picks up each notification and runs:
   - the **validation pipeline** — independent checks run *concurrently* and
     *fail fast* (the first failure cancels the rest);
   - if valid, the **delivery** layer — a single best-effort send wrapped in a
     per-attempt timeout, bounded retries with exponential backoff + jitter, and
     a per-provider **circuit breaker**.
4. The notification's status is updated at each step and is queryable via the API.

### Packages

| Package | Responsibility |
|---------|----------------|
| `internal/domain`     | Core entities (`Notification`, `Channel`, `Status`) and sentinel errors. No external deps. |
| `internal/store`      | `Store` persistence interface + concurrency-safe in-memory implementation. |
| `internal/validation` | `Validator` interface, concurrent fail-fast `Pipeline`, and concrete validators (recipient, content, preferences, rate limit). |
| `internal/provider`   | `Provider` interface, `Registry`, retryable-error classification, and a configurable simulated provider. |
| `internal/delivery`   | Retry policy, circuit breaker, and the `Deliverer` that composes them over a provider. |
| `internal/worker`     | Bounded worker pool with backpressure and graceful drain. |
| `internal/service`    | Orchestration: submit fan-out, the worker `Process` state machine, get/list. |
| `internal/api`        | HTTP transport: routing, DTOs, middleware (logging, panic recovery). |
| `internal/config`     | Environment-based configuration with defaults. |
| `internal/app`        | Composition root — wires everything from `Config`. Shared by `main` and integration tests. |
| `cmd/server`          | Process lifecycle: logging, signals, graceful shutdown. |

---

## How the requirements are met

| Requirement | How |
|-------------|-----|
| Submit / get status / list | `POST` / `GET {id}` / `GET` list endpoints. |
| Recipient, channel, contents | `Notification` fields; channel-aware validation. |
| Validations are independent & stop early on first failure | `validation.Pipeline` runs validators concurrently via `errgroup` and cancels the shared context on the first error — siblings observe cancellation and return without doing more work. |
| Deliver only if all validations pass | The worker only enters the delivery phase after the pipeline returns nil. |
| Unreliable providers (slow / failing / unavailable / intermittent) | Per-attempt timeouts, bounded retries with backoff + jitter (transient errors only), and a per-provider circuit breaker for sustained outages. |
| Best-effort reliable delivery | The delivery layer retries transient failures and records attempts; permanent failures stop immediately. |
| Multiple recipients, efficient & predictable resources | Fan-out to one notification per recipient, processed by a **fixed-size worker pool** draining a **bounded queue** — bursts are buffered, sustained overload applies backpressure. |
| Bursts of traffic | Buffered queue absorbs spikes; `Submit` blocks (bounded by a timeout) when full instead of unbounded goroutine growth. |
| Add new providers with minimal changes | Implement `provider.Provider` and register it in the composition root. Nothing else changes. |
| Add new channels/validations | New `Channel` constant + provider; new `Validator` added to the pipeline. |
| Production readiness | Structured logging, graceful shutdown, panic recovery, request size limits, strict JSON decoding, config via env, high test coverage incl. race detector. |

---

## Configuration

Configuration is read from environment variables at startup; every value has a
sensible default (so it runs with zero config). See
[`config.example.env`](./config.example.env) for the full list with explanations.
Frequently useful ones:

| Variable | Default | Meaning |
|----------|---------|---------|
| `NS_ADDR` | `:8080` | HTTP listen address |
| `NS_WORKERS` | `8` | Worker pool size |
| `NS_QUEUE_SIZE` | `1024` | Bounded queue capacity |
| `NS_MAX_ATTEMPTS` | `4` | Max delivery attempts (incl. first) |
| `NS_PROVIDER_FAILURE_RATE` | `0.2` | Simulated transient failure probability |
| `NS_RATE_LIMIT` | `20` | Max sends per recipient/channel per window (`0` disables) |

Invalid values fail fast at startup with a clear error rather than being silently
ignored.

---

## Testing

```bash
make test     # run all tests
make race     # run all tests under the race detector
make cover    # print coverage
make check    # fmt + vet + race (the pre-commit gate)
```

The suite covers each layer in isolation with fakes, plus an **end-to-end
integration test** (`internal/app`) that boots the full stack over `httptest` and
drives submit → poll → deliver. Highlights:

- **Fail-fast validation** is verified by proving a slow sibling validator is
  cancelled (not run to completion) when another fails.
- **Delivery** tests cover success, transient-retry-then-success, permanent-stop,
  retry exhaustion, per-attempt timeout, circuit-open fast-fail, and cancellation.
- **Worker pool** tests cover full processing, backpressure/timeout on a full
  queue, shutdown draining buffered jobs, and submit-after-close.
- All concurrency-sensitive tests pass under `-race`.

See [`docs/TESTING.md`](./docs/TESTING.md) for the testing strategy.

---

## Design docs

- [`docs/DESIGN.md`](./docs/DESIGN.md) — architectural decisions, assumptions,
  trade-offs, and future improvements.
- [`docs/TESTING.md`](./docs/TESTING.md) — testing strategy.
