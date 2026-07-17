# Design Decisions

## Architecture overview

```
                 ┌─────────────┐
  HTTP request → │   api       │  (chi router, DTOs, HTTP status mapping)
                 └──────┬──────┘
                        │ domain.BatchRequest
                 ┌──────▼──────┐
                 │  service    │  orchestrates the two phases below
                 └──────┬──────┘
           ┌────────────┼─────────────────┐
           │            │                 │
   ┌───────▼──────┐  ┌──▼───────┐   ┌─────▼──────┐
   │ validation   │  │  store   │   │  delivery  │
   │ (sync,       │  │ (repo:   │   │ (async:    │
   │  fail-fast)  │  │  create/ │   │  worker    │
   │              │  │  update/ │   │  pool →    │
   │              │  │  get/list│   │  dispatcher│
   └──────────────┘  └──────────┘   │  → provider│
                                     └────────────┘
```

A submitted batch is split into one `Notification` per recipient. Each
notification moves through two distinct phases:

1. **Validation** (synchronous, in the HTTP request): cheap, local checks
   run in-process against the request and each recipient. This lets the
   client get an immediate, per-recipient verdict and means we never
   enqueue work we already know is invalid.
2. **Delivery** (asynchronous, via a bounded worker pool): only
   notifications that passed validation are handed to a worker pool that
   calls the actual (simulated) provider, with retries and a circuit
   breaker, because *this* step is the one talking to unreliable external
   systems.

This split maps directly to the two problem areas in the brief
("Notification Processing" vs. "Providers") and lets each be optimized
independently — validation for latency and fail-fast, delivery for
resilience.

## Key architectural decisions

**Fan-out to one Notification per recipient, sharing a `batch_id`.**
A request with N recipients is not one unit of work — it's N independent
ones. Modeling it that way means one recipient's bad address or a
transient provider blip for one recipient never blocks, delays, or fails
the others. It also makes "get status" and "list" naturally granular
without needing a secondary index of "sub-statuses" inside a single row.

**Two-tier validation, ordered cheapest-first, fail-fast within each tier.**
The brief requires that a failed validation not waste resources on further
validation. Given that, validators are explicitly ordered from cheapest to
most expensive: format checks (regex, no I/O) before preference lookups
before rate limiting (a shared, mutable structure) — so the pipeline never
pays for an expensive check when a free one would already have rejected
the input. Request-level checks (message content) run once per batch, before
any recipient is even looked at, since a bad message invalidates the whole
batch regardless of recipient count.

Validators are plain interfaces (`RequestValidator`, `RecipientValidator`)
run through an ordered `Pipeline`; adding a new validation (e.g. content
moderation, blocklists) means implementing the interface and adding it to
the slice built in `main.go` — nothing else changes.

**Delivery decoupled from the HTTP request via a bounded worker pool.**
Providers are explicitly unreliable and can be slow; the HTTP handler
cannot afford to block on them, and a traffic burst cannot be allowed to
spawn unbounded goroutines or unbounded queued work. A fixed-size worker
pool draining a fixed-capacity channel bounds both: total concurrent
provider calls (predictable load on the provider and on this process) and
total buffered backlog (predictable memory use). When the queue is full,
`Enqueue` fails immediately rather than blocking the request or growing
without limit — that failure is recorded as the notification's terminal
state (see Trade-offs) instead of silently dropping work.

**Retry with jittered exponential backoff, per notification.** Retries
live inside `Dispatcher.Deliver`, which loops up to `MaxDeliveryAttempts`,
sleeping a jittered exponential delay between attempts (see Trade-offs for
why this sleep happens inside a worker goroutine rather than via a
delayed re-queue). Full jitter (delay drawn uniformly from `[0, cap]`
rather than a fixed value) avoids synchronized retry storms across many
notifications that failed around the same time.

**One circuit breaker per channel, shared across all notifications for
that channel.** A breaker is a statement about the *provider's* health,
not about any single notification, so it must be shared, not
per-notification. Breaking it out per channel means an unhealthy email
provider doesn't stop SMS or push from being attempted. When the breaker
is open, calls fail immediately without invoking the provider at all —
this is precisely what keeps a struggling provider from being hammered
further, and it also means workers aren't tied up waiting on a timeout for
a provider that's already known to be down.

**Provider abstraction via a small interface + explicit registry.**
`Provider` is `Channel() domain.Channel` + `Send(ctx, Notification) error`.
The registry is a simple `map[Channel]Provider` built explicitly at
startup (not reflection/plugin-based) — deliberately the simplest thing
that satisfies "add a channel with minimal changes" without introducing
dynamic loading machinery that this problem doesn't call for.

**Repository interface with an in-memory implementation.** All persistence
goes through `store.Repository`. The only implementation provided is an
in-memory, mutex-protected map, which keeps the assessment runnable with
zero setup and is sufficient to demonstrate the access patterns (create,
update, point lookup, filtered/paginated list) a real implementation would
need to support. See Trade-offs for what a production deployment would
need instead.

**Structured logging (`log/slog`), no framework.** Every HTTP request and
delivery attempt/failure/circuit-breaker transition is logged as
structured JSON with a request ID, which is the minimum needed to debug a
production incident without pulling in a logging framework.

## Assumptions

- **Recipient format is a plain string** whose meaning depends on the
  channel (email address, E.164 phone number, device token). Multi-field
  recipients (e.g. `{name, address}`) were out of scope for the brief and
  would be a straightforward DTO extension.
- **One channel per batch request.** The brief describes "a notification"
  as having one channel; a request that needs to reach one recipient over
  multiple channels is modeled as multiple requests. Supporting
  per-recipient channel overrides in a single request is a natural
  extension (see Future improvements).
- **"Best effort reliable delivery"** means bounded retries with backoff
  and a circuit breaker, not guaranteed eventual delivery — there is no
  outbox/dead-letter re-processing across process restarts (see
  Trade-offs). This matches "best effort" in the brief rather than an
  at-least-once guarantee, which would require durable queueing.
- **Validation set is illustrative, not exhaustive.** Recipient format,
  user preference, and rate limiting were chosen as the three examples
  named in the brief; preference and rate-limit checks are simulated
  in-memory (see `validation.PreferenceStore`) standing in for what would
  be a real preferences/quota service.
- **Idempotency of submission is out of scope.** Two identical POSTs
  create two independent batches; no idempotency-key deduplication is
  implemented (see Future improvements).
- **No authentication/authorization.** The brief doesn't mention multi-tenant
  or user-facing auth concerns; this is treated as an internal service
  callable by trusted platform components.

## Trade-offs

- **In-memory storage, not a database.** Simplicity and zero local setup
  vs. durability: all notification history is lost on restart, and `List`
  pagination is offset-based (cheap in memory, but not viable at real
  scale — cursor-based pagination needs a durably ordered key). Chosen
  because the brief allows external systems to be simulated and prioritizes
  engineering judgment over infra setup; the `Repository` interface is the
  seam a Postgres implementation would slot into without touching callers.

- **Retry backoff sleeps inside the worker goroutine**, rather than
  releasing the worker and re-enqueuing the notification for later
  (as a real "visibility timeout" queue like SQS would). Simpler to
  reason about and sufficient at the worker-pool scale used here, but it
  means a notification that's mid-backoff occupies a worker slot it isn't
  actively using — under sustained high failure rates this reduces
  effective throughput. A production version processing high volume would
  move backoff into a delayed re-queue instead.

- **Queue-full is a terminal failure, not backpressure to the client.**
  When the bounded delivery queue is full, the affected notification is
  immediately marked `failed` with a descriptive error rather than the
  HTTP call blocking or returning a retryable `429`. This keeps the
  request/response cycle fast and bounded, at the cost of pushing the
  retry decision to the caller (who can resubmit). An alternative — surfacing
  `429`/`Retry-After` to let the client's own retry logic handle it — is a
  reasonable variant; documented here as a deliberate choice rather than
  an oversight.

- **Sequential per-recipient validation in `Submit`.** Recipients in a
  batch are validated one at a time in a loop, not concurrently. Given the
  validators here are in-memory and sub-millisecond, this is simple and
  fast enough; a batch with very large recipient counts would benefit from
  bounded concurrent validation (e.g. an errgroup with a worker cap),
  deliberately not built since it adds complexity the current validators
  don't need.

- **chi, gobreaker, golang.org/x/time/rate and testify are the only
  third-party dependencies.** Each replaces something that would otherwise
  be meaningfully reinvented (routing with path params, circuit breaking,
  a token bucket, test assertions) without pulling in a heavier framework
  (no web framework beyond routing, no ORM, no DI container) — in keeping
  with the brief's preference for simple, well-structured solutions.

## Future improvements

- **Durable storage** (Postgres/etc.) behind the existing `Repository`
  interface, with real cursor-based pagination.
- **Durable delivery queue** (SQS/Kafka/etc.) so in-flight work survives a
  process restart, plus a delayed-retry mechanism instead of in-goroutine
  sleeping.
- **Idempotency keys** on submission so client-side retries of the same
  logical request don't create duplicate batches.
- **Webhook/callback support** so callers are notified on status
  transitions instead of having to poll `GET /v1/notifications/{id}`.
- **Per-recipient channel overrides** within a single batch request.
- **Metrics** (Prometheus) for queue depth, delivery latency, retry counts,
  and circuit breaker state, which is what an on-call engineer would
  actually need beyond structured logs.
- **AuthN/AuthZ and multi-tenancy**, if this service is ever exposed beyond
  trusted internal callers.
- **Dead-letter visibility**: a way to list/inspect/manually retry
  notifications that exhausted all delivery attempts, beyond filtering by
  `status=failed`.
