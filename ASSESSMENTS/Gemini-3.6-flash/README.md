# Notification Delivery Service

A production-grade, highly resilient backend service written in **Go** responsible for orchestrating notification delivery across multiple communication channels (**Email**, **SMS**, **Push Notifications**) under high-concurrency traffic bursts.

---

## Key Features & Architecture

- **Multi-Channel Support**: Native support for Email, SMS, and Push Notifications. Easily extensible for future channels (e.g., Slack, WhatsApp, Webhooks) via interface polymorphism.
- **Fast-Failing Concurrent Validation Pipeline**: Executes independent validations (recipient format, rate limits, user opt-out preferences, message body rules) concurrently using Go channels & `context.WithCancel`. If any validation fails, remaining concurrent checks abort immediately to conserve resources.
- **Resilient Provider Ecosystem (Retries, Fallback & Circuit Breakers)**: External providers (AWS SES, SendGrid, Twilio, Infobip, FCM, APNS) are treated as unreliable. Each channel maintains a primary and fallback provider chain. Calls use exponential backoff with jitter and circuit breakers to prevent cascading failures.
- **Traffic Burst & Concurrency Management**: Asynchronous processing engine utilizing a bounded worker pool and job queue. Requests targeting multiple recipients return an immediate HTTP 202 Accepted, preventing HTTP thread starvation and ensuring predictable memory/CPU consumption.
- **Clean Architecture & SOLID Design**: Fully decoupled layers (Domain, Repository, Validator, Provider, Worker, Service, Handler).
- **Graceful Shutdown**: Intercepts OS signals (`SIGINT`, `SIGTERM`), drains active worker queues, and ensures zero lost jobs on deployment or restart.

---

## Directory Structure

```
.
├── cmd/
│   └── server/          # Main application entry point (HTTP server + lifecycle)
├── config/              # Configuration loader (Environment variables & defaults)
├── internal/
│   ├── domain/          # Core domain models, interfaces, & error definitions
│   ├── handler/         # HTTP handlers (Submit, GetStatus, List, Health)
│   ├── middleware/      # Request logger & panic recovery middlewares
│   ├── provider/        # Provider interface, Registry, Failover, & Simulated Providers
│   ├── repository/      # Repository interface & Thread-safe In-Memory implementation
│   ├── service/         # Orchestrator usecase service
│   ├── validator/       # Fast-failing validation pipeline & rule implementations
│   └── worker/          # Bounded worker pool & queue manager
├── pkg/
│   └── resilience/      # Exponential backoff retry & Circuit Breaker algorithms
├── docs/
│   ├── architecture.md  # Detailed architecture document & diagrams
│   └── openapi.yaml     # OpenAPI 3.0 API Specification
├── scripts/
│   └── demo_burst.go    # Automated traffic burst & load demo script
├── Dockerfile           # Multi-stage production container build
├── docker-compose.yml   # Multi-container orchestrator setup
├── Makefile             # CLI commands for build, test, run, and demo
└── README.md            # Service documentation
```

---

## Functional Requirements & API Usage

### 1. Submit Notification Request

**`POST /api/v1/notifications`**

Submits a notification request for one or multiple recipients.

#### Request Body
```json
{
  "channel": "email",
  "recipients": ["alice@example.com", "bob@example.com"],
  "subject": "System Maintenance Notice",
  "body": "Scheduled maintenance tonight at 00:00 UTC.",
  "metadata": {
    "tenant_id": "org_99"
  }
}
```

#### Response (`202 Accepted`)
```json
{
  "id": "e4a7c1b2-8f9d-4c3a-9e1f-2b5a6c7d8e9f",
  "channel": "email",
  "recipients": ["alice@example.com", "bob@example.com"],
  "subject": "System Maintenance Notice",
  "body": "Scheduled maintenance tonight at 00:00 UTC.",
  "status": "queued",
  "total_recipients": 2,
  "delivered_count": 0,
  "failed_count": 0,
  "created_at": "2026-07-21T19:00:00Z",
  "updated_at": "2026-07-21T19:00:00Z"
}
```

---

### 2. Retrieve Notification Status

**`GET /api/v1/notifications/{id}`**

Retrieves overall progress and individual delivery statuses per recipient.

#### Response (`200 OK`)
```json
{
  "id": "e4a7c1b2-8f9d-4c3a-9e1f-2b5a6c7d8e9f",
  "channel": "email",
  "recipients": ["alice@example.com", "bob@example.com"],
  "status": "delivered",
  "total_recipients": 2,
  "delivered_count": 2,
  "failed_count": 0,
  "recipient_details": [
    {
      "id": "d1c2b3a4-5678-90ab-cdef-1234567890ab",
      "request_id": "e4a7c1b2-8f9d-4c3a-9e1f-2b5a6c7d8e9f",
      "recipient": "alice@example.com",
      "channel": "email",
      "status": "delivered",
      "provider_used": "email-primary-aws-ses",
      "attempts": 1,
      "created_at": "2026-07-21T19:00:00Z",
      "updated_at": "2026-07-21T19:00:01Z"
    }
  ]
}
```

---

### 3. List Previously Submitted Notifications

**`GET /api/v1/notifications?channel=email&status=delivered&limit=10&offset=0`**

Retrieves a paginated list of notification requests with optional channel and status filtering.

---

### 4. Service Health Check

**`GET /health`**

#### Response (`200 OK`)
```json
{
  "service": "notification-service",
  "status": "UP",
  "timestamp": "2026-07-21T19:00:00Z"
}
```

---

## Design Decisions, Assumptions & Trade-Offs

### 1. Fast-Failing Validation Pipeline Architecture
- **Decision**: Validations run concurrently using a shared cancellable `context.Context`.
- **Rationale**: If a recipient format is invalid, continuing to query external user preference databases or rate-limit tables wastes network bandwidth and database pool connections.
- **Trade-off**: Slightly higher short-lived goroutine allocation during validation, offset by massive savings on downstream network/database calls.

### 2. Primary & Fallback Provider Routing with Resilience
- **Decision**: Each channel has a chain of providers registered in the `ProviderRegistry`. Calls are wrapped with exponential backoff retries and circuit breakers.
- **Rationale**: Third-party SMS/Email/Push APIs experience intermittent downtime or rate limiting. When a primary provider fails, the system automatically falls back to secondary providers seamlessly.

### 3. Bounded Worker Pool for Traffic Bursts
- **Decision**: Uses fixed worker pools fed by a bounded Go channel (`WORKER_QUEUE_CAPACITY=10000`).
- **Rationale**: Unbounded goroutines during a 100,000-recipient burst can cause Out-Of-Memory (OOM) crashes. A bounded pool caps memory and CPU overhead while completing jobs smoothly.

### 4. Storage Abstraction (Thread-Safe In-Memory with DB readiness)
- **Decision**: Implemented an in-memory repository protected by `sync.RWMutex`.
- **Rationale**: Allows running out-of-the-box with zero external database dependencies for evaluation while supporting easy replacement with PostgreSQL/MySQL/MongoDB via the `repository.Repository` interface.

---

## Quick Start & Verification

### Prerequisites
- **Go 1.22+** (or **Docker** & **docker-compose**)
- **Make** (optional)

---

### Running Locally

```bash
# 1. Run unit and integration tests
make test

# 2. Start the HTTP server
make run
```
The server will listen on `http://localhost:8080`.

---

### Running via Docker & Docker-Compose

```bash
# Build and run containers
docker-compose up --build
```

---

### Running Automated Traffic Burst Demo

In a separate terminal (while the server is running):
```bash
make demo
```
This script will:
1. Verify `/health`.
2. Concurrently submit 50 notification requests targeting 250 total recipients across Email, SMS, and Push.
3. Simulate validation failures and provider fallbacks.
4. Inspect request progress and print paginated results.

---

## Automated Tests

Run unit, integration, and race detector tests:
```bash
go test -v -race -cover ./...
```

---

## Future Improvements & Production Roadmap

1. **Persistent Database Driver**: Implement PostgreSQL driver using `pgx` and SQL migrations (`golang-migrate`).
2. **Distributed Queue Engine**: Replace local worker channel with Redis Streams, RabbitMQ, or Apache Kafka for multi-node horizontal scaling.
3. **Observability & Metrics**: Expose `/metrics` endpoint using `prometheus/client_golang` to monitor request latency, provider failure rates, and queue depth.
4. **Auth & Multitenancy**: Add API key authentication middleware and tenant-based quota enforcement.
