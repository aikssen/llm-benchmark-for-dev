# Notification Orchestration Service

Backend service written in Go that orchestrates notification delivery across **Email**, **SMS**, and **Push** channels.

Designed as a first production-ready foundation: extensible providers/validators, async processing with bounded concurrency, retries for unreliable external providers, and a simple HTTP API.

## Features

- Submit notifications for one or many recipients
- Fan-out to per-recipient delivery units
- Independent validation pipeline with fail-fast short-circuit
- Pluggable channel providers (simulated by default)
- Async worker pool with retries + exponential backoff
- Backpressure when the internal queue is saturated
- Query notification status and list history
- In-memory store suitable for local development

## Architecture

```
Client
  │
  ▼
HTTP API (internal/api)
  │
  ▼
Service (internal/service)  ──► Store (internal/store)
  │
  ▼
Worker Pool (internal/worker)
  │
  ├─► Validation Pipeline (internal/validation)
  │     - recipient
  │     - content
  │     - preferences
  │     - rate limit
  │
  └─► Provider Registry (internal/provider)
        - email / sms / push (simulated)
```

### Design choices

| Decision | Rationale |
|---|---|
| Async processing after accept | API stays fast under bursts; delivery is best-effort with retries |
| Bounded worker pool + queue | Predictable CPU/memory under load; explicit backpressure (`503`) |
| Per-recipient notifications | Independent status, retries, and validation outcomes |
| Provider interface + registry | New channels without touching orchestration code |
| Validation pipeline interface | Independent rules; short-circuit avoids wasted work |
| In-memory store | Zero-deps local run; `Store` interface allows swapping to Postgres/Redis later |
| Simulated providers | Model slow/failing external systems without real vendor SDKs |

## API

Base URL: `http://localhost:8080`

### Health

```http
GET /healthz
```

### Submit notification(s)

```http
POST /v1/notifications
Content-Type: application/json
```

```json
{
  "recipients": ["user@example.com", "other@example.com"],
  "channel": "email",
  "subject": "Welcome",
  "body": "Thanks for joining"
}
```

**Channels:** `email` | `sms` | `push`

**Recipient formats:**
- email: standard email address
- sms: E.164 (`+15551234567`)
- push: device token (min 8 chars)

**Response:** `202 Accepted`

```json
{
  "request_id": "...",
  "notification_ids": ["...", "..."],
  "status": "pending"
}
```

### Get notification

```http
GET /v1/notifications/{id}
```

### List notifications

```http
GET /v1/notifications?status=delivered&channel=email&limit=50&offset=0
```

### Status lifecycle

`pending` → `validating` → `queued` → `sending` → `delivered`

Terminal failure states: `rejected` (validation), `failed` (provider / retries exhausted / queue full).

## Run locally

Requirements: Go 1.22+

```bash
# from repository root
go mod tidy
go run ./cmd/server
```

Server listens on `:8080` by default.

### Example requests

```bash
# Email
curl -s -X POST http://localhost:8080/v1/notifications \
  -H 'Content-Type: application/json' \
  -d '{
    "recipients": ["alice@example.com"],
    "channel": "email",
    "subject": "Hello",
    "body": "World"
  }' | jq

# SMS (multi-recipient)
curl -s -X POST http://localhost:8080/v1/notifications \
  -H 'Content-Type: application/json' \
  -d '{
    "recipients": ["+15551234567", "+15557654321"],
    "channel": "sms",
    "body": "Your code is 1234"
  }' | jq

# Status
curl -s http://localhost:8080/v1/notifications/<id> | jq

# List
curl -s 'http://localhost:8080/v1/notifications?limit=20' | jq
```

## Configuration

| Env var | Default | Description |
|---|---|---|
| `HTTP_ADDR` | `:8080` | Listen address |
| `WORKER_COUNT` | `8` | Concurrent delivery workers |
| `QUEUE_SIZE` | `256` | Buffered job queue size |
| `MAX_RETRIES` | `3` | Provider retries on temporary failures |
| `RETRY_BASE_DELAY` | `200ms` | Base delay for exponential backoff |
| `PROVIDER_FAIL_RATE` | `0.2` | Simulated provider failure probability |
| `PROVIDER_MIN_LATENCY` | `20ms` | Simulated min provider latency |
| `PROVIDER_MAX_LATENCY` | `150ms` | Simulated max provider latency |
| `RATE_LIMIT_PER_MINUTE` | `60` | Per recipient+channel rate limit |

## Tests

```bash
go test ./...
go test ./... -race
```

## Assumptions

1. **Best-effort delivery is acceptable for v1** — accepted requests return `202`; delivery is async.
2. **In-memory persistence is enough for local/demo** — data is lost on restart. Production would use a durable store.
3. **One channel per request** — multi-channel fan-out can be added later without changing core models much.
4. **Recipient de-duplication** within a request is intentional to avoid double-send.
5. **SMS body limited to 160 characters** for classic SMS segments.
6. **Preferences default to opt-in** unless explicitly opted out (demo store).
7. **Provider temporary failures are retryable**; permanent/unknown failures fail the notification after retries.

## Trade-offs

- **Simplicity over distributed infra**: no Kafka/Redis/Postgres in v1 to keep the assignment focused and runnable.
- **Fail-fast validation** reduces resource use, but does not return all validation errors at once.
- **In-process queue** is not durable; process crash can lose pending jobs (documented for future improvement).
- **Global simulated fail rate** is good for demos; real providers would have independent SLAs/metrics.

## Future improvements

- Durable store (Postgres) + outbox pattern for at-least-once delivery
- External queue (SQS / NATS / Kafka) for horizontal scaling
- Idempotency keys on submit
- Structured per-channel templates and localization
- Metrics (Prometheus): queue depth, latency, success/failure rates
- OpenAPI/Swagger spec
- Authn/Authz for API clients
- Dead-letter handling and admin redrive
- Circuit breakers per provider
- Webhooks / callbacks for delivery status

## Project layout

```
cmd/server/           # application entrypoint
internal/
  api/                # HTTP handlers
  config/             # env configuration
  domain/             # domain models
  provider/           # channel providers + registry
  service/            # application orchestration
  store/              # persistence
  validation/         # independent validators + pipeline
  worker/             # async delivery workers
```

## License

MIT (assessment sample)
