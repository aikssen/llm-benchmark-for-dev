# Notification Service

A production-ready notification orchestration service built in Go. This service handles notification delivery across multiple channels (Email, SMS, Push) with reliability, extensibility, and maintainability as core design principles.

## Features

- **Multi-channel support**: Email, SMS, and Push notifications
- **Asynchronous processing**: Non-blocking notification delivery with worker pool
- **Retry mechanism**: Exponential backoff for failed deliveries
- **Validation pipeline**: Early-termination validation chain
- **Rate limiting**: Per-recipient rate limiting
- **User preferences**: Opt-out support per channel
- **Extensible architecture**: Easy to add new channels and providers

## Architecture

### Project Structure

```
.
├── cmd/server/          # Application entry point
├── internal/
│   ├── api/            # HTTP handlers and routing
│   ├── domain/         # Core domain models and errors
│   ├── provider/       # Notification provider implementations
│   ├── service/        # Business logic and async processor
│   ├── storage/        # Data persistence layer
│   └── validator/      # Validation logic
├── Dockerfile
├── Makefile
└── README.md
```

### Key Components

1. **Domain Layer** (`internal/domain`): Core business entities and error types
2. **Storage Layer** (`internal/storage`): Interface-based persistence with in-memory implementation
3. **Provider Layer** (`internal/provider`): Pluggable notification providers with registry pattern
4. **Validator Layer** (`internal/validator`): Chain-of-responsibility validation pipeline
5. **Service Layer** (`internal/service`): Business logic and async job processor
6. **API Layer** (`internal/api`): HTTP handlers and middleware

## API Endpoints

### Submit Notification
```http
POST /api/v1/notifications
Content-Type: application/json

{
  "channel": "email",
  "subject": "Welcome!",
  "message": "Thank you for joining us.",
  "recipients": ["user@example.com", "admin@example.com"]
}
```

**Response:** `202 Accepted`
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending",
  "channel": "email",
  "created_at": "2024-01-15T10:30:00Z"
}
```

### Get Notification Status
```http
GET /api/v1/notifications/{id}
```

**Response:** `200 OK`
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "channel": "email",
  "subject": "Welcome!",
  "message": "Thank you for joining us.",
  "status": "delivered",
  "recipients": [
    {
      "recipient": "user@example.com",
      "status": "delivered",
      "delivered_at": "2024-01-15T10:30:05Z"
    }
  ],
  "created_at": "2024-01-15T10:30:00Z",
  "updated_at": "2024-01-15T10:30:05Z"
}
```

### List Notifications
```http
GET /api/v1/notifications?channel=email&status=delivered&limit=50&offset=0
```

**Response:** `200 OK`
```json
{
  "notifications": [...],
  "total": 150,
  "limit": 50,
  "offset": 0
}
```

### Health Check
```http
GET /health
```

## Getting Started

### Prerequisites

- Go 1.22 or higher

### Running Locally

```bash
# Build the application
make build

# Run the server
make run

# Or run directly
go run ./cmd/server
```

The server will start on `http://localhost:8080`

### Running Tests

```bash
# Run all tests
make test

# Run tests with coverage
make test-cover
```

### Using Docker

```bash
# Build the image
docker build -t notification-service .

# Run the container
docker run -p 8080:8080 notification-service
```

## Design Decisions

### Architectural Decisions

1. **Clean Architecture**: Separation of concerns with clear layer boundaries
   - Domain layer has no external dependencies
   - Business logic is isolated from HTTP concerns
   - Interfaces enable dependency injection and testing

2. **Asynchronous Processing**: Notifications are processed in background
   - API returns immediately with `202 Accepted`
   - Worker pool handles delivery with configurable concurrency
   - Bounded queue prevents memory exhaustion under load

3. **Provider Registry Pattern**: Pluggable notification providers
   - New channels can be added without modifying existing code
   - Providers are registered at startup
   - Interface-based design enables easy mocking and testing

4. **Validation Chain**: Chain-of-responsibility pattern for validations
   - Early termination on first failure (as required)
   - Each validator is independent and testable
   - Easy to add new validation rules

### Assumptions

1. **In-Memory Storage**: Using in-memory storage for simplicity
   - Suitable for development and testing
   - Production would use PostgreSQL/Redis
   - Interface allows easy swap to persistent storage

2. **Simulated Providers**: Providers simulate delivery with random failures
   - 10% failure rate to demonstrate retry logic
   - 50-300ms latency to simulate real-world conditions
   - Production would integrate with real providers (SendGrid, Twilio, etc.)

3. **Rate Limiting**: Simple sliding window rate limiter
   - 100 requests per minute per recipient (configurable)
   - Production might use Redis for distributed rate limiting

4. **No Authentication**: API is open for simplicity
   - Production would require API keys or JWT tokens
   - Rate limiting would be per-client, not just per-recipient

### Trade-offs

1. **Simplicity vs. Features**: Chose simplicity over advanced features
   - No dead letter queue (failed notifications are marked as failed)
   - No webhook callbacks for delivery status
   - No scheduling/delayed delivery

2. **Standard Library vs. Frameworks**: Used Go's standard library
   - `net/http` instead of Gin/Echo/Chi
   - Fewer dependencies, easier to maintain
   - Go 1.22+ has excellent routing support

3. **Synchronous Validation**: Validations run synchronously before enqueueing
   - Ensures invalid requests fail fast
   - Could be optimized with parallel validation for multiple recipients

4. **Worker Pool Size**: Fixed at 10 workers
   - Production would make this configurable
   - Could implement auto-scaling based on queue depth

### Future Improvements

1. **Persistence**: Replace in-memory storage with PostgreSQL
   - Add migrations
   - Implement connection pooling
   - Add indexes for efficient querying

2. **Observability**: Add metrics and distributed tracing
   - Prometheus metrics for queue depth, delivery rates, latencies
   - OpenTelemetry for distributed tracing
   - Structured logging with correlation IDs

3. **Resilience**: Improve failure handling
   - Dead letter queue for permanently failed notifications
   - Circuit breaker pattern for provider failures
   - Bulkhead pattern to isolate provider failures

4. **Scalability**: Horizontal scaling support
   - Use Redis/RabbitMQ for distributed job queue
   - Implement leader election for coordination
   - Add horizontal pod autoscaling

5. **Features**: Additional capabilities
   - Notification templates with variable substitution
   - Scheduled/delayed delivery
   - Batch notifications
   - Webhook callbacks for delivery status
   - User preference management API

6. **Security**: Production hardening
   - API authentication (JWT/API keys)
   - Request signing for webhooks
   - Rate limiting per client
   - Input sanitization and validation

## Testing Strategy

The test suite includes:

1. **Unit Tests**: Individual components tested in isolation
   - Validators with various input scenarios
   - Storage operations
   - Provider behavior

2. **Integration Tests**: API endpoint testing
   - Full request/response cycles
   - Error handling
   - Filtering and pagination

3. **Concurrency Tests**: Race condition detection
   - Tests run with `-race` flag
   - Thread-safe operations verified

Run tests with:
```bash
go test -v -race ./...
```

## Configuration

Currently hardcoded for simplicity. Production would use environment variables or config files:

- Server port: `8080`
- Worker pool size: `10`
- Queue size: `1000`
- Rate limit: `100 requests/minute`
- Max retries: `3`
- Retry backoff: Exponential (100ms, 200ms, 400ms)

## License

MIT
