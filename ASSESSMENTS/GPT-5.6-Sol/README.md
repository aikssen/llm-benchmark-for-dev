# Notification Orchestrator

A small, production-minded Go service that accepts notification requests, validates each recipient, and asynchronously delivers through simulated email, SMS, and push providers.

The implementation uses only the Go standard library. It is intentionally straightforward to run and inspect while keeping persistence, providers, and validation behind interfaces so they can be replaced independently.

## Run it

Requirements: Go 1.23 or newer.

```bash
go test ./...
go run ./cmd/server
```

The server listens on `:8080` by default. To run it in a container:

```bash
docker build -t notification-service .
docker run --rm -p 8080:8080 notification-service
```

Submit an email notification:

```bash
curl --request POST http://localhost:8080/v1/notifications \
  --header 'Content-Type: application/json' \
  --header 'Idempotency-Key: welcome-user-42' \
  --data '{
    "recipients": ["first@example.com", "second@example.com"],
    "channel": "email",
    "message": {
      "subject": "Welcome",
      "body": "Thanks for joining us."
    }
  }'
```

The response is `202 Accepted` with a `Location` header. Read the current aggregate and per-recipient state with:

```bash
curl http://localhost:8080/v1/notifications/{id}
curl 'http://localhost:8080/v1/notifications?limit=20&offset=0'
```

Reusing an idempotency key with the same request returns the original notification with `200 OK` and `Idempotent-Replayed: true`. Reusing it for a different request returns `409 Conflict`.

Health endpoints are `GET /healthz` and `GET /readyz`. The full contract is in [api/openapi.yaml](api/openapi.yaml).

## Architecture

```text
HTTP API -> application service -> bounded delivery queue -> worker pool
                  |                         |
                  v                         v
              repository            validator chain -> provider registry
```

- `internal/notification` contains the domain model and ports.
- `internal/service` owns submission, capacity control, workers, fail-fast validation orchestration, timeouts, and retries.
- `internal/memory` is a concurrency-safe repository adapter.
- `internal/provider` contains a provider registry and configurable simulated providers.
- `internal/validation` contains independently replaceable validators.
- `internal/httpapi` maps the domain to a versioned HTTP/JSON API.
- `cmd/server` is the composition root and graceful-shutdown lifecycle.

Each recipient becomes one bounded queue job. A fixed number of workers supplies concurrency across both requests and recipients without spawning an unbounded goroutine per burst. A multi-recipient submission is admitted atomically: if the queue cannot fit the entire batch, the API returns `503` before persisting any part of it.

The validator chain is intentionally sequential and ordered from cheap to potentially stateful work: recipient format, message rules, preferences, then rate limit. Validators are logically independent, but running them concurrently would defeat the explicit fail-fast requirement and consume resources after a known failure. The first failure marks that recipient `rejected`; later validators and the provider are not called.

Provider calls have a per-attempt timeout. Retryable failures use capped exponential backoff with deterministic jitter; permanent errors fail immediately. Retry count and timing are configurable. The overall notification status is derived atomically whenever a recipient changes:

- `queued` or `processing` while any delivery is pending
- `delivered` when every recipient succeeds
- `partially_delivered` when at least one succeeds and another does not
- `rejected` when every recipient fails validation
- `failed` for other fully terminal unsuccessful combinations

## Configuration

All settings are optional environment variables.

| Variable | Default | Meaning |
|---|---:|---|
| `HTTP_ADDR` | `:8080` | Listen address |
| `WORKER_COUNT` | `8` | Concurrent delivery workers |
| `QUEUE_CAPACITY` | `1000` | Maximum queued recipient jobs |
| `MAX_RECIPIENTS` | `100` | Recipients accepted per request |
| `MAX_MESSAGE_BYTES` | `4096` | Maximum UTF-8 body size in bytes |
| `MAX_SUBJECT_BYTES` | `256` | Maximum subject size in bytes |
| `PROVIDER_TIMEOUT` | `2s` | Timeout for one provider attempt |
| `SIMULATED_PROVIDER_LATENCY` | `50ms` | Artificial provider latency |
| `SIMULATED_FAILURE_RATE` | `0.05` | Retryable provider failure probability, 0–1 |
| `RETRY_MAX_ATTEMPTS` | `3` | Total provider attempts |
| `RETRY_BASE_DELAY` | `100ms` | Initial retry delay |
| `RETRY_MAX_DELAY` | `2s` | Retry-delay cap |
| `RECIPIENT_RATE_LIMIT` | `20` | Accepted deliveries per recipient/window |
| `RATE_LIMIT_WINDOW` | `1m` | Fixed rate-limit window |
| `BLOCKED_RECIPIENTS` | empty | Comma-separated opt-out recipients |
| `SHUTDOWN_TIMEOUT` | `15s` | Time allowed to drain queued work |

Invalid configuration fails fast on startup. Set `SIMULATED_FAILURE_RATE=0` for deterministic manual demos.

## Assumptions and decisions

- A request has one channel and message, with 1–100 distinct recipients. Different channel/message combinations are separate requests.
- Email recipients use standard address parsing, SMS numbers use E.164, and push tokens are opaque non-whitespace strings of 8–512 bytes.
- Email requires a subject. SMS and push may omit it. An invalid recipient or subject is accepted into the asynchronous workflow and becomes `rejected`; malformed request shape, unsupported channel, empty/oversized body, duplicates, and excessive batch size fail synchronously with `400`.
- Rate limits and preferences apply per recipient. Their in-memory implementations demonstrate the port; a shared store or user-preferences service would replace them in a multi-instance deployment.
- Idempotency is optional. Clients that retry POST requests should send a stable key.
- The API intentionally returns message and recipient data. Authentication, authorization, tenant isolation, and redaction are expected at or before this service and would be mandatory in a real deployment.
- Offset pagination is sufficient for this assessment. A cursor based on `(created_at, id)` is preferable for large or rapidly changing datasets.

## Trade-offs

The bundled repository and queue are in process, which makes the project self-contained but means queued work and records do not survive a restart. This is the main deliberate production limitation. The domain does not depend on that adapter: a durable implementation can use PostgreSQL with an outbox/job table and `SELECT ... FOR UPDATE SKIP LOCKED`, or a database plus a durable broker. Idempotency key uniqueness and delivery state transitions should be transactional there.

The fixed-window limiter favors simplicity and predictable memory use for this version. A distributed token bucket would provide smoother behavior across replicas. Simulated providers share the same implementation but are separately registered per channel; real adapters can implement channel-specific payloads and error classification without changing the processor.

## Testing

```bash
go test ./...
go test -race ./...
go vet ./...
```

Tests cover aggregate state, atomic concurrent repository updates, idempotency conflicts, whole-batch queue admission, validation short-circuiting, retry/permanent-error behavior, strict JSON parsing, and the HTTP submission-to-delivery lifecycle. Fakes make retry and validation tests deterministic.

## Future improvements

Prioritized next steps for a real rollout:

1. Durable PostgreSQL repository and recoverable job/outbox processing, with migrations and retention policies.
2. Authentication, tenant-scoped authorization, encrypted sensitive fields, audit policy, and endpoint-specific rate limiting.
3. Metrics and traces for queue depth, validation outcomes, provider latency, retries, and terminal delivery status; propagate correlation IDs.
4. Provider circuit breakers and bulkheads, dead-letter handling, administrative replay, and provider fallback/routing policies.
5. Cursor pagination and filters, delivery callbacks/webhooks, scheduled notifications, templates, and localization.
6. Distributed preference/rate-limit adapters and cleanup of expired limiter keys.
7. Deployment manifests, readiness tied to dependencies, load tests, SLOs, and alerts.

