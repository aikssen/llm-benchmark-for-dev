# Notification Orchestration Service

A production-ready notification orchestration service built in Go. It supports multiple communication channels (Email, SMS, Push Notifications), concurrent validation with fail-fast cancellation, asynchronous worker pool execution, and delivery retry mechanisms with provider failover.

---

## Architecture Overview

The codebase is organized following Clean Architecture and Domain-Driven Design (DDD) principles:

- **`cmd/server/main.go`**: Service entrypoint. Handles dependency injection, configuration, server initialization, and OS signal listener for graceful shutdown.
- **`internal/domain/`**: Core domain logic and entity definitions. Contains the `Notification` model, channels, and statuses. Zero external dependencies.
- **`internal/repository/`**: Data access interface and implementations. Uses a thread-safe, in-memory repository mapping structure with deep copying to prevent data-race leaks.
- **`internal/validation/`**: Business validation logic. Features a concurrent validation `Engine` that runs checks (format, message length, user preferences, rate limiting) in parallel, cancelling outstanding checks as soon as any single validation fails.
- **`internal/provider/`**: Interface and wrappers for third-party delivery services (Email, SMS, Push). Includes a `Registry` allowing multiple providers to be registered per channel to support failover routing.
- **`internal/service/`**: Core orchestration layer. Handles submissions, coordinates status changes, and manages delivery retry logic.
- **`internal/worker/`**: Background execution engine. Bounded concurrency worker pool that processes notifications asynchronously and supports graceful draining on shutdown.
- **`internal/api/`**: HTTP transport layer. Exposes REST endpoints, validates raw JSON inputs, and handles panic recovery and request logging through standard middlewares.

---

## Design Decisions

### 1. Bounded Concurrency via Worker Pool
To prevent resource exhaustion (CPU/Memory) during bursts of incoming requests, incoming notifications are parsed and saved in a `Pending` state, then dispatched to an in-memory channel. A set of fixed worker goroutines listen to this channel to process tasks asynchronously. This ensures that the system's resource usage remains predictable under high load.

### 2. Concurrent Validation Engine with Fail-Fast Abort
Validation rules (e.g. checking formats, rate limits, user settings) are independent of each other. Running them sequentially is inefficient. The service runs all validations concurrently using separate goroutines. If one validation fails, it cancels a shared context, signaling the other validations to exit immediately. This stops further CPU/Network consumption.

### 3. Provider Failover and Resiliency
To handle unreliable external providers:
- **Registry & Fallbacks**: We register a primary and a backup provider for each channel.
- **Exponential Backoff**: When sending fails, we retry using exponential backoff to avoid overloading external systems.
- **Failover**: If the primary provider fails after all retry attempts (e.g. 3 attempts), the orchestrator automatically falls back to the next provider in the registry and tries delivering through it.

### 4. Graceful Shutdown
When the server receives `SIGINT` or `SIGTERM`, it performs a coordinated graceful shutdown:
1. **HTTP Server**: Stops accepting new HTTP connections and waits for active requests to finish.
2. **Worker Pool**: Closes the queue channel to block new submissions, and waits for workers to drain and finish processing notifications currently in the queue.
3. **Timeout Fallback**: If processing takes longer than the allotted grace period (e.g. 5 seconds), it cancels the context to force-close active connections.

---

## Assumptions

1. **In-Memory Storage**: For local testing and simulation, a thread-safe in-memory repository is sufficient. In production, this would be backed by a persistent database (e.g., PostgreSQL).
2. **Synchronous Validation, Asynchronous Delivery**: We assume that validating a notification request (checking preferences, rate limits) is fast enough to do as the first step of the background processing. Clients receive an immediate `202 Accepted` response with the notification IDs once validation and queueing are initiated, making the submission endpoint highly responsive.
3. **Recipient Formatting**: We assume phone numbers follow E.164 formatting (`+` followed by digits) and email addresses are valid RFC 5322 formats.

---

## Trade-offs

- **In-Memory Queue vs. Message Broker**: The in-memory channel queue is extremely fast and requires no external dependencies (like RabbitMQ or Redis). However, if the server crashes, all queued notifications are lost. In a real-world production deployment, we would replace the channel queue with a durable message broker (e.g. Kafka or RabbitMQ) or a database-backed polling queue.
- **Database Mutex Locking**: The in-memory repository uses a read-write mutex (`sync.RWMutex`). This simplifies testing but would cause locking bottlenecks under extremely high parallel write loads. A real database handles concurrency at the storage engine level.
- **Deep Copying overhead**: We deep copy notification structs upon repository retrieval and update. This adds slight memory allocation overhead but completely eliminates data race conditions and accidental side-effect mutations in memory.

---

## Future Improvements

1. **Persistent Message Queue**: Integrate a durable queue (like RabbitMQ or Amazon SQS) to guarantee delivery even if the server crashes.
2. **Persistent DB**: Swap the in-memory repository for a SQL database (like PostgreSQL) using Go migrations.
3. **Dynamic Rate Limiting**: Implement distributed rate limiting using Redis token bucket or sliding window algorithms rather than localized simulations.
4. **Idempotency**: Add an idempotency key header to the `POST /notifications` endpoint to prevent double-delivery of notifications when clients retry their requests.
5. **Prometheus Metrics**: Expose metrics for response time, queue latency, delivery success rate, and provider error count.

---

## Getting Started

### Prerequisites

- Go 1.22 or higher installed.

### Build and Run

1. Clone or navigate to the workspace directory.
2. Build the server binary:
   ```bash
   go build -o server ./cmd/server
   ```
3. Run the server:
   ```bash
   ./server
   ```
   The HTTP server will start on port `:8080`.

### Running Automated Tests

Run the full test suite with detailed logging:
```bash
go test -v ./...
```

To run test coverage:
```bash
go test -cover ./...
```

---

## API Documentation

### 1. Submit a Notification Request
Submits a notification to one or multiple recipients.

- **URL**: `POST /notifications`
- **Headers**: `Content-Type: application/json`
- **Request Body**:
  ```json
  {
    "recipients": ["+15551234567", "valid@email.com"],
    "channel": "sms",
    "content": "Alert: Your order has shipped!"
  }
  ```
  *(Note: Valid channels are `"email"`, `"sms"`, and `"push"`)*

- **Response (202 Accepted)**:
  ```json
  {
    "request_id": "4b68bb5a-a309-8431-d8ec-8fcf9a3e2a48",
    "notification_ids": [
      "9f2cf05d-e0f1-0988-d4ec-c4ec7b77f980",
      "932cf05d-b0a3-9823-d34e-dfef1c0e3a47"
    ]
  }
  ```

---

### 2. Retrieve Status of a Notification
Get the current processing status of a single notification.

- **URL**: `GET /notifications/{id}`
- **Response (200 OK)**:
  ```json
  {
    "id": "9f2cf05d-e0f1-0988-d4ec-c4ec7b77f980",
    "request_id": "4b68bb5a-a309-8431-d8ec-8fcf9a3e2a48",
    "recipient": "+15551234567",
    "channel": "sms",
    "content": "Alert: Your order has shipped!",
    "status": "sent",
    "retry_count": 0,
    "created_at": "2026-07-15T11:32:19.123456-05:00",
    "updated_at": "2026-07-15T11:32:19.183456-05:00"
  }
  ```

  Possible statuses:
  - `pending`: Waiting in the queue.
  - `validating`: Undergoing validation checks.
  - `failed_validation`: Failed one of the validation checks (checks details in `error_reason`).
  - `sending`: Validated and actively delivering through providers.
  - `sent`: Successfully delivered.
  - `failed`: Failed all delivery attempts and fallbacks (check details in `error_reason`).

---

### 3. List All Notifications
Lists previously submitted notifications, sorted with the newest first.

- **URL**: `GET /notifications`
- **Response (200 OK)**:
  ```json
  [
    {
      "id": "9f2cf05d-e0f1-0988-d4ec-c4ec7b77f980",
      "request_id": "4b68bb5a-a309-8431-d8ec-8fcf9a3e2a48",
      "recipient": "+15551234567",
      "channel": "sms",
      "content": "Alert: Your order has shipped!",
      "status": "sent",
      "retry_count": 0,
      "created_at": "2026-07-15T11:32:19.123456-05:00",
      "updated_at": "2026-07-15T11:32:19.183456-05:00"
    }
  ]
  ```
