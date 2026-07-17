# Notification Service

A backend service that orchestrates notification delivery across multiple
channels (email, SMS, push) on behalf of a platform whose downstream
providers are slow, flaky, and occasionally unavailable.

See [DESIGN.md](DESIGN.md) for architectural decisions, assumptions, and
trade-offs.

## Requirements

- Go 1.23+ (developed against 1.26; the module uses `min`/`max` builtins from 1.21+)

## Running locally

```bash
go run ./cmd/server
```

The server listens on `:8080` by default. All configuration is via
environment variables (see [Configuration](#configuration)); it runs with
no external dependencies — everything (storage, providers) is in-process.

## API

### Submit a notification batch

```
POST /v1/notifications
```

```json
{
  "channel": "email",
  "recipients": ["alice@example.com", "bob@example.com"],
  "message": {
    "subject": "Your order shipped",
    "body": "Track it at ..."
  }
}
```

`channel` is one of `email`, `sms`, `push`. `recipients` may contain one or
many addresses — each becomes an independently tracked notification sharing
a `batch_id`. `message.subject` is required for `email`; `message.data` is
a free-form string map available to channels like `push` that need
structured payloads.

Response — `202 Accepted` (the batch was accepted; per-recipient outcomes
may differ):

```json
{
  "batch_id": "8bcb6169-...",
  "notifications": [
    { "id": "d5d355af-...", "recipient": "alice@example.com", "status": "pending", "attempts": 0, ... },
    { "id": "acc8beab-...", "recipient": "bob@example.com", "status": "pending", "attempts": 0, ... }
  ]
}
```

A recipient that fails validation (bad address, opted out, rate limited)
is reported with `"status": "failed"` and a `last_error` in the same
response — it does not block its siblings, and the request as a whole is
still `202`. Only a failure that applies to the *whole* batch (empty
message body, missing email subject, no recipients, unknown channel)
produces a `400` and creates nothing.

### Get notification status

```
GET /v1/notifications/{id}
```

Returns the current state of a single notification: `pending` →
`processing` → `delivered` | `failed`.

### List notifications

```
GET /v1/notifications?status=failed&channel=email&recipient=alice@example.com&batch_id=...&limit=20&offset=0
```

All query parameters are optional filters. `limit` defaults to 20, capped
at 100. Response includes `total` (matching the filter, ignoring
pagination) for building pagination UIs.

### Health check

```
GET /healthz
```

## Configuration

All values have production-sensible defaults tuned to also be responsive
for local testing; override via environment variables:

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `WORKER_POOL_SIZE` | `16` | Concurrent delivery workers |
| `QUEUE_CAPACITY` | `1000` | Max notifications buffered awaiting delivery |
| `MAX_DELIVERY_ATTEMPTS` | `4` | Attempts per notification before giving up |
| `BASE_BACKOFF` | `100ms` | First retry backoff |
| `MAX_BACKOFF` | `2s` | Backoff ceiling |
| `PROVIDER_TIMEOUT` | `3s` | Per-attempt context timeout against a provider |
| `BREAKER_FAILURE_THRESHOLD` | `5` | Consecutive provider failures before its circuit breaker trips |
| `BREAKER_OPEN_DURATION` | `5s` | How long a tripped breaker stays open |
| `RATE_LIMIT_PER_MINUTE` | `60` | Max notifications per recipient per minute |
| `PROVIDER_FAILURE_RATE` | `0.2` | Simulated provider failure probability (0..1) |
| `PROVIDER_MIN_LATENCY` / `PROVIDER_MAX_LATENCY` | `20ms` / `150ms` | Simulated provider latency range |

## Testing

```bash
go test ./...
go test ./... -race   # concurrency-sensitive paths: worker pool, circuit breaker, rate limiter
```

Coverage includes: validation pipeline ordering/short-circuiting, the
in-memory repository (filters, pagination, isolation from caller
mutation), retry backoff bounds, the dispatcher's retry/circuit-breaker
behavior against fake providers, the worker pool's backpressure and
processing, the service layer's per-recipient independence, and
end-to-end HTTP tests (including polling a batch through to `delivered`
and to `failed`-after-retries).

## Project layout

```
cmd/server/            entrypoint: wiring + graceful shutdown
internal/domain/        core entities (Notification, Channel, Status, Message) — no dependencies
internal/config/        environment-driven configuration
internal/validation/    request- and recipient-level validation pipeline
internal/provider/      Provider interface, registry, simulated implementations
internal/delivery/      retry policy, circuit breaker, bounded worker pool
internal/store/         Repository interface + in-memory implementation
internal/service/       use-case orchestration (submit / get / list)
internal/api/           HTTP handlers, routing, DTOs
```

Dependencies point inward: `api` → `service` → `{validation, delivery, store}` → `domain`.
`domain` has no internal dependencies, so it never needs to change when the
API, storage, or provider layers do.

## Adding a new channel

1. Add a `domain.Channel` constant.
2. Implement `provider.Provider` for it (or reuse `provider.NewSimulated` for a demo).
3. If the recipient address has a distinct format, add a case to `validation.FormatValidator`.
4. Register the provider in `cmd/server/main.go`.

No other code changes are required — the validation pipeline, dispatcher,
worker pool, storage, and API are all channel-agnostic.
