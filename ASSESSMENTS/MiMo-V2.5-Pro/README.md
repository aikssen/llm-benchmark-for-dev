# Notification Service

A production-ready backend service for orchestrating notification delivery across multiple communication channels (Email, SMS, Push Notifications).

## Architecture

The service follows a clean architecture pattern with clear separation of concerns:

```
notification-service/
├── cmd/server/              # Application entry point
├── internal/
│   ├── config/              # Configuration management
│   ├── domain/              # Core business entities and interfaces
│   ├── handler/             # HTTP request handlers
│   ├── provider/            # Notification delivery providers
│   ├── repository/          # Data storage implementations
│   ├── service/             # Business logic layer
│   └── validator/           # Validation pipeline
├── pkg/
│   ├── errors/              # Custom error types
│   └── logger/              # Structured logging
└── tests/                   # Integration tests
```

### Key Design Decisions

1. **Clean Architecture**: Clear boundaries between layers with dependency inversion
2. **Provider Pattern**: Abstract notification delivery behind interfaces for easy extensibility
3. **Pipeline Pattern**: Independent validators run concurrently with short-circuit on failure
4. **Worker Pool**: Bounded concurrency for predictable resource usage
5. **Repository Pattern**: Abstract storage for testability and future database integration

## API Endpoints

### Create Notification
```
POST /api/v1/notifications
```

**Request Body:**
```json
{
  "channel": "email",
  "recipients": [
    {"value": "user@example.com"}
  ],
  "content": {
    "subject": "Welcome",
    "body": "Thank you for signing up!"
  },
  "metadata": {
    "priority": "high",
    "tags": ["welcome", "onboarding"]
  }
}
```

**Response (202 Accepted):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "channel": "email",
  "status": "queued",
  "created_at": "2024-01-15T10:30:00Z"
}
```

### Get Notification
```
GET /api/v1/notifications/{id}
```

**Response (200 OK):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "channel": "email",
  "status": "sent",
  "recipients": [{"value": "user@example.com"}],
  "results": [
    {
      "recipient": {"value": "user@example.com"},
      "status": "sent",
      "sent_at": "2024-01-15T10:30:01Z"
    }
  ]
}
```

### List Notifications
```
GET /api/v1/notifications?channel=email&status=sent&limit=20&offset=0
```

**Response (200 OK):**
```json
{
  "notifications": [...],
  "total": 42,
  "limit": 20,
  "offset": 0
}
```

### Health Check
```
GET /api/v1/health
```

## Supported Channels

| Channel | Recipient Format | Provider |
|---------|-----------------|----------|
| Email | email@domain.com | SMTP (simulated) |
| SMS | +1234567890 | Twilio (simulated) |
| Push | device_token (min 10 chars) | FCM (simulated) |

## Validation Pipeline

The service performs multiple independent validations concurrently:

1. **Channel Validator**: Ensures the notification channel is supported
2. **Recipient Validator**: Validates recipient format based on channel
3. **Content Validator**: Checks message content and channel-specific requirements
4. **Rate Limiter**: Enforces per-recipient rate limits

Validations short-circuit on the first failure to avoid unnecessary resource consumption.

## Configuration

Configuration is managed through environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_PORT` | `8080` | HTTP server port |
| `SERVER_READ_TIMEOUT` | `10s` | HTTP read timeout |
| `SERVER_WRITE_TIMEOUT` | `30s` | HTTP write timeout |
| `WORKER_COUNT` | `5` | Number of worker goroutines |
| `WORKER_QUEUE_SIZE` | `100` | Size of notification queue |
| `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |

## Getting Started

### Prerequisites

- Go 1.22 or later
- Make (optional)

### Build and Run

```bash
# Build the application
make build

# Run the application
make run

# Or directly with Go
go run cmd/server/main.go
```

### Run Tests

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage
```

### Docker

```bash
# Build Docker image
make docker-build

# Run Docker container
make docker-run
```

## Example Usage

### Send Email Notification

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "email",
    "recipients": [
      {"value": "user@example.com"},
      {"value": "admin@example.com"}
    ],
    "content": {
      "subject": "System Alert",
      "body": "Your account has been accessed from a new device."
    }
  }'
```

### Send SMS Notification

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "sms",
    "recipients": [
      {"value": "+1234567890"}
    ],
    "content": {
      "body": "Your verification code is 123456"
    }
  }'
```

### Check Notification Status

```bash
curl http://localhost:8080/api/v1/notifications/{notification-id}
```

### List Email Notifications

```bash
curl "http://localhost:8080/api/v1/notifications?channel=email&limit=10"
```

## Design Decisions

### Assumptions

1. **In-Memory Storage**: For simplicity and local development. The repository interface allows easy migration to persistent storage (PostgreSQL, MongoDB, etc.)
2. **Simulated Providers**: External providers are simulated with configurable failure rates and latency. In production, these would be replaced with real integrations.
3. **Single Service Instance**: The current design assumes a single service instance. For horizontal scaling, the repository and queue would need to be distributed.
4. **No Authentication**: API authentication is not implemented for simplicity. In production, JWT or API key authentication would be added.

### Trade-offs

1. **Simplicity vs. Scalability**: Chose simplicity for the initial implementation. The architecture supports future scaling with minimal changes.
2. **In-Memory vs. Persistent Storage**: In-memory storage is fast but not durable. The repository abstraction allows easy migration.
3. **Worker Pool vs. Serverless**: Worker pool provides predictable resource usage and is suitable for sustained workloads.

### Future Improvements

1. **Persistent Storage**: Add PostgreSQL or MongoDB repository implementation
2. **Message Queue**: Integrate RabbitMQ or Kafka for reliable message delivery
3. **Circuit Breaker**: Add circuit breaker pattern for provider resilience
4. **Metrics & Tracing**: Add Prometheus metrics and OpenTelemetry tracing
5. **Authentication**: Implement JWT-based API authentication
6. **Webhooks**: Add webhook support for delivery status callbacks
7. **Template Engine**: Add message template support with variable substitution
8. **Batch API**: Add batch notification endpoint for bulk operations
9. **Rate Limiting**: Implement distributed rate limiting with Redis
10. **Dead Letter Queue**: Add DLQ for failed notifications requiring manual intervention

## License

MIT
