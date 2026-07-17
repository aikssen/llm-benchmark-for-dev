# Notification Service

A production-oriented Go backend service that orchestrates notification delivery across multiple channels (Email, SMS, Push).

## Overview

The service accepts notification requests via HTTP, validates them concurrently, and dispatches delivery to simulated external providers using a bounded worker pool. It exposes endpoints to submit a notification, retrieve its status, and list previously submitted notifications.

## Project Structure

```
notification-service/
├── cmd/server/                 # Application entry point
├── internal/
│   ├── domain/                 # Core business entities and repository contract
│   ├── application/            # Use cases, validators, and orchestration
│   ├── infrastructure/
│   │   ├── http/               # HTTP handlers and routing
│   │   ├── memory/             # In-memory repository implementation
│   │   └── providers/          # Provider interface and channel implementations
│   └── config/                 # Environment-based configuration
├── pkg/workerpool/             # Reusable bounded worker pool
├── go.mod
├── Makefile
└── README.md
```

## Requirements

- Go 1.22 or later

## Build & Run

```bash
# Run tests
go test ./...

# Build the server
go build -o bin/server ./cmd/server

# Run the server
./bin/server
```

Or using Make:

```bash
make test
make build
make run
```

The server listens on `:8080` by default. Override with the `HTTP_PORT` environment variable.

## Configuration

All configuration is read from environment variables with sensible defaults:

| Variable            | Default       | Description                           |
|---------------------|---------------|---------------------------------------|
| `HTTP_PORT`         | `8080`        | HTTP listen port                      |
| `WORKERS`           | `4`           | Number of background workers          |
| `QUEUE_SIZE`        | `1000`        | Bounded job queue size                |
| `READ_TIMEOUT`      | `5s`          | HTTP read timeout                     |
| `WRITE_TIMEOUT`     | `10s`         | HTTP write timeout                    |
| `SHUTDOWN_TIMEOUT`  | `10s`         | Graceful shutdown timeout             |

## API

### Submit a notification

```http
POST /notifications
Content-Type: application/json

{
  "channel": "email",
  "recipients": ["alice@example.com"],
  "subject": "Hello",
  "body": "World"
}
```

Response `202 Accepted`:

```json
{
  "id": "<uuid>",
  "status": "pending"
}
```

### Get notification status

```http
GET /notifications/{id}
```

Response `200 OK`:

```json
{
  "id": "<uuid>",
  "channel": "email",
  "recipients": [{"address": "alice@example.com"}],
  "content": {"subject": "Hello", "body": "World"},
  "status": "sent",
  "result": {
    "recipient_results": [
      {"recipient": {"address": "alice@example.com"}, "success": true, "provider": "simulated-email"}
    ]
  },
  "created_at": "...",
  "updated_at": "..."
}
```

### List notifications

```http
GET /notifications?limit=20&offset=0
```

Response `200 OK`:

```json
{
  "notifications": [...],
  "total": 1
}
```

### Health check

```http
GET /health
```

## Architectural Decisions

### Layered architecture

The codebase is organized around a layered/hexagonal style:

- **Domain**: pure entities, value objects, and repository interfaces. No external dependencies.
- **Application**: use cases (submit, status, list), validation pipeline, and provider orchestration.
- **Infrastructure**: concrete HTTP server, in-memory repository, and simulated providers.

This separation makes the core business logic easy to unit test and allows swapping infrastructure (e.g. PostgreSQL, Kafka, real provider SDKs) without touching use cases.

### Asynchronous processing with bounded worker pool

Notifications are accepted synchronously, persisted, and then enqueued for background processing. A fixed-size worker pool limits memory and goroutine usage during traffic bursts, satisfying the requirement for predictable resource usage.

### Concurrent validations with early exit

All validators run concurrently using the worker pool. As soon as one validation fails, the remaining validators are cancelled via `context.Context`. This meets the requirement that "if one validation fails, the remaining validations should not continue consuming unnecessary resources."

### Provider abstraction

Each channel has a `Provider` implementation registered in a `Registry`. Adding a new channel means creating a new provider and registering it; no existing code needs to change. Simulated providers include randomized latency and intermittent failures, plus retries with exponential back-off.

### Error handling

Domain errors are explicit sentinel values. HTTP handlers map domain errors to appropriate status codes. Internal errors are logged with structured logging but not leaked to clients.

## Assumptions

1. **In-memory storage is acceptable for this assessment.** A production deployment would use a persistent database and likely a message broker (e.g. SQS, RabbitMQ, Kafka) for durability and horizontal scaling.
2. **Simulated providers are sufficient.** Real integrations would use third-party SDKs or HTTP clients, with circuit breakers, timeouts, and idempotency keys.
3. **User preferences are simulated by an in-memory blocked list.** In production this would query a preference service or database.
4. **Rate limiting is local and in-memory.** Production would use a distributed rate limiter (e.g. Redis).
5. **A notification is delivered to every recipient using the same provider/channel.** Per-recipient channel preference is out of scope.
6. **Partial delivery is acceptable.** A notification is considered "sent" if at least one recipient succeeds; per-recipient results are recorded.

## Trade-offs

- **In-memory repository** keeps the project runnable locally with no external dependencies, but it is not durable or horizontally scalable.
- **net/http instead of a framework** keeps dependencies minimal and leverages Go's standard library, at the cost of writing a bit more boilerplate.
- **Fixed worker pool** provides predictable resource usage but may introduce queue latency under extreme load. Horizontal scaling or back-pressure policies would be needed beyond the initial design.
- **Concurrent validators** reduce latency but may briefly consume more CPU. Cancellation on first failure bounds the extra cost.
- **Providers share the same worker pool** used for job processing, which simplifies resource management but means slow providers can contend with validators. A future improvement is separate pools per concern.

## Future Improvements

1. **Persistence**: replace the in-memory repository with PostgreSQL or DynamoDB.
2. **Message broker**: move background jobs to SQS/RabbitMQ/Kafka for durability and retry semantics.
3. **Circuit breaker**: wrap providers to fail fast when an external provider is unhealthy.
4. **Metrics & tracing**: expose Prometheus metrics and OpenTelemetry traces.
5. **Idempotency**: accept client-provided idempotency keys to avoid duplicate submissions.
6. **Templating**: support message templates and per-channel rendering.
7. **Retry policy configuration**: make retry counts and back-off configurable per provider.
8. **Authentication/authorization**: add API keys or OAuth2 for client requests.
9. **OpenAPI**: generate API documentation from the HTTP layer.
10. **Multi-tenancy**: namespace notifications by tenant or application.

## Testing

Run the test suite with:

```bash
go test ./...
```

The tests cover:

- Domain behavior (channel parsing)
- Repository CRUD and pagination
- Worker pool processing and shutdown
- Provider registry and simulated delivery
- Application service: submit, validation, status, list
- HTTP handlers and routing
