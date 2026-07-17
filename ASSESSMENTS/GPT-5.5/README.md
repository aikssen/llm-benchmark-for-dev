# Notification Service

Backend service in Go for orchestrating notification delivery across email, SMS, and push notification channels.

The service exposes an HTTP API to submit notifications, retrieve a notification status, and list submitted notifications. Delivery is asynchronous: incoming requests are persisted in an in-memory store, queued, validated, and sent by bounded workers using simulated unreliable providers.

## Run locally

Requirements:

- Go 1.22+

```bash
go test ./...
go run ./cmd/notification-service
```

The server listens on `:8080` by default.

Configurable environment variables:

| Variable | Default | Description |
| --- | ---: | --- |
| `ADDR` | `:8080` | HTTP listen address |
| `WORKERS` | `8` | Number of delivery workers |
| `QUEUE_SIZE` | `1000` | Max pending jobs in memory |
| `PROVIDER_FAILURE_RATE` | `0.20` | Failure rate for simulated providers, from `0` to `1` |

Example with deterministic successful providers:

```bash
PROVIDER_FAILURE_RATE=0 go run ./cmd/notification-service
```

## API

### Submit a notification

`POST /notifications`

Single recipient:

```bash
curl -i -X POST localhost:8080/notifications \
  -H 'Content-Type: application/json' \
  -d '{
    "recipient": "user@example.com",
    "channel": "email",
    "message": {
      "subject": "Welcome",
      "body": "Thanks for signing up"
    }
  }'
```

Multiple recipients:

```bash
curl -i -X POST localhost:8080/notifications \
  -H 'Content-Type: application/json' \
  -d '{
    "recipients": ["user1@example.com", "user2@example.com"],
    "channel": "email",
    "message": {
      "subject": "Maintenance",
      "body": "Scheduled maintenance starts at 21:00 UTC"
    }
  }'
```

Response:

```json
{
  "request_id": "req_...",
  "notifications": [
    {
      "id": "ntf_...",
      "recipient": "user@example.com",
      "channel": "email",
      "status": "pending"
    }
  ]
}
```

### Get status

`GET /notifications/{id}`

```bash
curl localhost:8080/notifications/ntf_...
```

### List notifications

`GET /notifications?limit=50&status=delivered&channel=email`

All query parameters are optional. `limit` is capped at 100.

```bash
curl localhost:8080/notifications
```

### Health check

`GET /healthz`

## Architecture

The implementation is intentionally small and dependency-free while keeping production-oriented boundaries:

- `internal/api`: HTTP transport, JSON request/response handling.
- `internal/app`: application service for request-level validation, persistence, and queue submission.
- `internal/domain`: core notification model and status/channel definitions.
- `internal/processing`: bounded asynchronous worker pool, validation pipeline, retry/backoff delivery orchestration.
- `internal/provider`: provider interface, registry, and simulated unreliable providers.
- `internal/store`: in-memory persistence.
- `internal/validation`: independent validators for recipient, message, preferences, and rate limits.

## Processing model

1. A request is accepted through `POST /notifications`.
2. Each recipient becomes an individual notification under the same `request_id`.
3. Notifications are saved with `pending` status.
4. IDs are added to a bounded in-memory queue.
5. Workers process jobs asynchronously:
   - set status to `validating`
   - run validators in sequence
   - stop immediately on the first validation failure
   - send with the provider for the selected channel
   - retry transient provider failures with bounded attempts and backoff
   - set terminal status to `delivered`, `rejected`, or `failed`

Status values:

- `pending`: accepted and waiting in the queue.
- `validating`: validation is running.
- `sending`: provider delivery is in progress or being retried.
- `delivered`: provider send succeeded.
- `rejected`: validation failed.
- `failed`: queueing or delivery failed.

## Design decisions

### One notification per recipient

Multi-recipient requests return one `request_id` and one notification ID per recipient. This keeps status tracking precise and makes retry/failure behavior explicit per recipient.

### Bounded queue and worker pool

The queue and worker count are bounded to handle bursts without unbounded goroutine or memory growth. If the queue is full, submission fails with `503 Service Unavailable`.

### Sequential validation with short-circuit

The validation rules are independent, but they are executed sequentially. This satisfies the requirement that once a validation fails, later validations do not consume resources. The order places cheap syntactic checks before simulated business checks and rate limiting.

### Provider abstraction

Providers implement a small `Sender` interface and are registered per channel. Adding a new channel requires:

1. Add a channel constant.
2. Register a provider for that channel.
3. Add or reuse recipient validation logic.

The processing pipeline does not need to change.

### In-memory persistence

Persistence is in-memory to keep the assessment runnable locally without infrastructure. The store is protected by locks and exposed behind an interface so it can be replaced by PostgreSQL, DynamoDB, Redis streams, or another durable system later.

## Assumptions

- The API accepts either `recipient` or `recipients`. If both are supplied, they are combined.
- A single request can target up to 100 recipients to protect memory and queue capacity.
- Providers are simulated and failures are treated as retryable until attempts are exhausted.
- Recipient preferences are simulated: recipients containing `blocked` or `unsubscribed` are rejected.
- Rate limiting is in-memory per `channel:recipient`.
- Message body is required and limited to 4096 bytes; subject is limited to 256 bytes.

## Trade-offs

- In-memory storage makes local execution simple but loses data on restart.
- The queue is process-local. A production deployment with multiple replicas should use durable queueing or a database-backed outbox pattern.
- Validators run sequentially instead of concurrently to guarantee short-circuit behavior and keep resource consumption predictable.
- Provider errors are currently classified generically. Real providers should distinguish retryable failures, permanent failures, throttling, and idempotency conflicts.

## Future improvements

- Durable persistence and migrations.
- Transactional outbox or durable queue for at-least-once processing.
- Idempotency keys on submission.
- Per-tenant quotas and distributed rate limiting.
- Provider-specific error classification.
- Observability: structured request logs, metrics, tracing, dead-letter queue, and alerting.
- Authentication/authorization.
- Pagination cursors for list endpoints.
- OpenAPI specification.
