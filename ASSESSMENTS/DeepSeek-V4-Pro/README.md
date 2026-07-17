# Notification Delivery Orchestrator

A backend service for orchestrating notification delivery across multiple communication channels (Email, SMS, Push Notifications).

## Build & Run

```bash
# Build
make build

# Run
make run

# Run in development mode
make dev

# Run tests
make test

# Run vet
make vet
```

The server starts on port `8080` by default. Configure via environment variables (see Configuration section).

## API

### Submit a Notification

```
POST /api/v1/notifications
Content-Type: application/json

{
  "channel": "email",
  "recipients": [
    {"id": "user@example.com", "type": "email"}
  ],
  "message": {
    "subject": "Subject line",
    "body": "Notification body"
  }
}
```

Response: `202 Accepted`
```json
{
  "id": "uuid",
  "status": "pending"
}
```

Valid channels: `email`, `sms`, `push`

### Get Notification Status

```
GET /api/v1/notifications/{id}
```

Response: `200 OK`
```json
{
  "id": "uuid",
  "channel": "email",
  "recipients": [{"id": "user@example.com", "type": "email"}],
  "message": {"subject": "...", "body": "..."},
  "status": "sent",
  "delivery_results": [
    {"recipient_id": "user@example.com", "success": true, "sent_at": "2024-01-01T00:00:00Z"}
  ],
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

Possible statuses: `pending`, `validating`, `sending`, `sent`, `partial_sent`, `failed`, `validation_error`

### List Notifications

```
GET /api/v1/notifications?offset=0&limit=20
```

Response: `200 OK`
```json
{
  "notifications": [...],
  "total": 42,
  "offset": 0,
  "limit": 20
}
```

## Configuration

All settings are configured via environment variables:

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8080` | HTTP server port |
| `MAX_WORKERS` | `10` | Number of concurrent notification processors |
| `MAX_RETRIES` | `3` | Retry attempts per recipient delivery |
| `RETRY_BACKOFF_MS` | `300` | Initial backoff between retries (milliseconds, exponential) |
| `REQUEST_BUFFER` | `100` | Bounded channel buffer for incoming jobs (backpressure) |
| `RATE_LIMIT` | `5` | Max notifications per recipient-channel pair per window |
| `RATE_LIMIT_WINDOW_SEC` | `60` | Rate limit window duration (seconds) |

## Project Structure

```
cmd/server/              Entry point
internal/
  api/                   HTTP handlers, DTOs, middleware, router
  config/                Environment-based configuration
  domain/                Core domain types and interfaces
  orchestrator/          Notification processing with worker pool and retries
  provider/              Simulated external providers + registry
  store/                 Data store interface and in-memory implementation
  validator/             Validation pipeline and validators
```

## Architecture

### Design Decisions

**Clean Architecture / Dependency Inversion**: The domain package defines interfaces (`Provider`, `Validator`, `Store`) that are implemented by inner packages. This keeps the core domain free of infrastructure concerns and makes it easy to swap implementations (e.g., replacing in-memory store with PostgreSQL).

**Interface-based extensibility**: Both `Provider` and `Validator` are Go interfaces. Adding a new channel (e.g., WhatsApp) requires only a new provider implementation and registration. Adding a new validation rule requires only a new `Validator` implementation added to the pipeline.

**Worker pool with bounded channel**: The orchestrator uses a buffered channel as a job queue and a fixed pool of goroutine workers. This provides backpressure — if the channel is full, `Submit` blocks until space is available, preventing unbounded memory growth during traffic bursts.

**Concurrent validation with fail-fast**: Validators run concurrently via goroutines. On first failure, a shared context is cancelled to signal remaining validators to stop early. This satisfies both "validations are independent" and "stop consuming unnecessary resources on failure."

**Exponential backoff with retries**: Provider delivery retries use exponential backoff (`backoff * 2^attempt`). This handles transient provider failures gracefully without overwhelming recovering systems.

**Semaphore-based concurrency control for delivery**: Per-notification delivery to multiple recipients is bounded by a semaphore (default 5 concurrent sends) to avoid overwhelming providers when processing requests with many recipients.

**Go 1.22+ routing**: Uses the enhanced `http.ServeMux` with method and path value patterns (`POST /api/v1/notifications`, `GET /api/v1/notifications/{id}`), avoiding external router dependencies.

### Assumptions

- The service is a single-instance deployment (in-memory store, in-memory rate limits). A production version would use a persistent database and distributed rate limiter.
- Recipient IDs are opaque strings whose format depends on the channel (email address, phone number, device token).
- `Subject` on `Message` is optional and only meaningful for email channel.
- A `partial_sent` status is introduced to represent cases where some recipients received the notification but others failed.
- Provider simulations use random failure rates; in production, these would be replaced with real API clients.
- The service uses graceful shutdown to drain in-flight requests and complete queued jobs.

### Trade-offs

| Decision | Trade-off |
|---|---|
| In-memory store | Fast and simple, but not durable. Chosen for demonstration; a real deployment would use PostgreSQL or similar. |
| In-memory rate limiting | Works for single-instance; a distributed deployment would need Redis-based rate limiting. |
| Sequential validations after fail-fast | Validators are launched concurrently which provides parallelism but means some work may be done on cancelled validators before they check the context. The overhead is minimal for validation workloads. |
| No message queue | Jobs are processed in-process. For production, an external queue (Redis, RabbitMQ) would provide durability and allow horizontal scaling. |
| No OpenTelemetry/metrics | Keeps scope focused. Production would add observability (metrics, tracing, structured logging). |

### Future Improvements

1. **Persistent storage**: Replace in-memory store with PostgreSQL using migrations and connection pooling.
2. **Message queue**: Introduce Redis or RabbitMQ for job persistence and distributed processing.
3. **Distributed rate limiting**: Use Redis with sliding window algorithm.
4. **Observability**: Add Prometheus metrics, OpenTelemetry tracing, and structured logging (e.g., `zerolog`).
5. **Idempotency**: Add idempotency keys to prevent duplicate notifications.
6. **Scheduled notifications**: Support delayed/scheduled delivery.
7. **Webhook callbacks**: Notify clients of delivery results asynchronously.
8. **Authentication/Authorization**: API keys or OAuth for client identity.
9. **Dead letter queue**: Persistent storage for permanently failed deliveries for manual review.
10. **Provider health checks**: Circuit breaker pattern for failing providers.

## Testing

Tests cover:
- Domain validation logic (unit)
- Store operations (unit)
- Individual validators (unit)
- Validation pipeline with fail-fast behavior (unit)
- Provider sending and registry (unit)
- Orchestrator submission and processing flows (integration)
- HTTP handlers with request/response assertions (integration)

```bash
make test
```
