# Design decisions

This document records the architectural decisions, the assumptions made where the
brief was intentionally open, the trade-offs behind each decision, and what a
next iteration would add.

---

## 1. Guiding principles

- **Design for change.** The brief stresses future extensibility (new channels,
  new validations, evolving providers). Every volatile concern sits behind an
  interface, and the concrete wiring is isolated in a single composition root
  (`internal/app`). Adding a channel or a validation rule touches one or two
  files, never the core.
- **Dependencies point inward.** The domain has no imports; transport, storage
  and providers depend on it, not the reverse. This keeps the business rules
  independent of frameworks and I/O.
- **Small dependency surface.** The only third-party dependency is
  `golang.org/x/sync/errgroup` (a quasi-standard library). HTTP routing, IDs,
  JSON, logging and concurrency all use the standard library. Fewer dependencies
  means less to audit, patch and break over the years the service is maintained.
- **Prefer boring, explicit code.** No reflection-heavy frameworks, no hidden
  magic. A new engineer can follow a request end to end by reading top to bottom.

---

## 2. Key decisions

### 2.1 Asynchronous processing, synchronous acknowledgement

`POST /v1/notifications` validates the request shape, persists the notifications
as `pending`, enqueues them and returns **202 Accepted** immediately. Actual
validation and delivery happen on a background worker pool.

**Why:** delivery latency is dominated by unreliable external providers (the
brief's central constraint). Making the client wait for it would couple request
latency to provider health and make bursts impossible to absorb. Returning a
handle the client can poll is the standard pattern for reliable, retriable work.

**Trade-off:** clients must poll for the outcome rather than getting it inline.
That is acceptable and expected for delivery-style workloads; a future webhook or
streaming-status feature (see §5) removes the need to poll.

### 2.2 One notification per recipient (fan-out)

A request with N recipients becomes N independent `Notification` records.

**Why:** each recipient has its own validation result (preferences, rate limits
differ per recipient) and its own delivery outcome. Modeling them independently
gives per-recipient status, isolates failures, and makes retries and reporting
natural. It also maps cleanly onto the worker pool: N units of work.

**Trade-off:** a request to a million recipients creates a million records. For
the expected scale this is fine; at very large fan-out you would batch and stream
(see §5).

### 2.3 Concurrent, fail-fast validation

The validators for a notification run **concurrently** and **stop at the first
failure**. This is implemented with `errgroup.WithContext`: the group cancels a
shared context the moment any validator errors, and validators are written to
observe cancellation and return early.

**Why:** the brief explicitly asks that "if one validation fails, the remaining
validations should not continue consuming unnecessary resources," and states the
validations are independent. Independent + fail-fast is exactly what a cancelable
errgroup expresses. Concurrency also cuts latency when a validator does I/O (e.g.
a preferences lookup or a distributed rate-limit check).

**Trade-off:** concurrency has a small fixed overhead (goroutines, a context) that
is not worth it for a handful of pure in-memory checks *today*. It is worth it the
moment any validator becomes I/O-bound — which is the realistic near future
(preferences service, Redis rate limiter) — so the abstraction is chosen for where
the system is going, not only where it is. The overhead is negligible relative to
the network delivery that follows.

### 2.4 Reliability: timeout + retry + circuit breaker

Providers are assumed unreliable, so the `delivery` layer wraps every send with
three composable safeguards:

1. **Per-attempt timeout** — a slow provider cannot pin a worker indefinitely.
2. **Bounded retries with exponential backoff + full jitter** — transient errors
   get another chance; jitter prevents many workers from synchronising into
   retry storms against a recovering provider.
3. **Per-provider circuit breaker** — after a run of failures the breaker opens
   and calls fail fast locally for a cooldown, then a single half-open probe
   decides whether to close again. This converts a dead dependency into cheap
   local failures instead of a pile-up of doomed attempts, and contains the
   blast radius to the affected channel.

Providers signal whether a failure is **retryable** (transient) or **permanent**
via `provider.RetryableError`; permanent errors skip retries entirely. Unclassified
errors default to retryable, because for network-bound providers a spurious retry
is cheaper than dropping a deliverable notification.

**Why keep this out of the providers?** Providers stay thin (one `Send` attempt),
and the policy is applied *uniformly* across every channel. Adding a channel gets
all of this for free.

**Trade-off:** retries with in-memory state mean work does not survive a process
crash (see §3 on durability). The circuit breaker is per-instance, not shared
across replicas; that is the right default (each instance observes its own view of
provider health) but means the global picture is only eventually consistent.

### 2.5 Bounded worker pool + bounded queue for bursts

Processing runs on a fixed number of workers draining a bounded, buffered queue.
`Submit` blocks when the queue is full (backpressure), bounded by a configurable
timeout after which the caller is told the system is overloaded.

**Why:** the brief calls for "efficient processing with predictable resource
usage" under "bursts of incoming requests." A fixed pool + bounded queue is the
textbook answer: bursts are absorbed by the buffer, and sustained overload is
handled by backpressure rather than unbounded goroutine/memory growth that would
eventually OOM the process. Resource usage is a function of configuration, not of
incoming load.

**Trade-off:** under sustained overload some submissions are rejected (and
recorded as `failed` with a reason, so they are observable rather than lost
silently). That is the correct behaviour for a bounded system — the alternative is
unbounded latency and eventual collapse. A durable queue (see §5) would let us
accept and defer instead of reject.

### 2.6 In-memory store behind an interface

Persistence is defined by the `store.Store` interface; the shipped implementation
is an in-memory, concurrency-safe map. Notifications are stored and returned **by
value** so callers cannot mutate shared state through a retained pointer.

**Why:** the brief allows simulated externals and asks for a locally runnable
service. An interface keeps the door open for a real database with zero changes to
business logic — the whole point of the boundary.

**Trade-off:** state is lost on restart, so in-flight and historical notifications
do not survive a crash. This is the single most important thing to replace for
real production use (§3, §5).

### 2.7 Standard-library HTTP and routing

Routing uses the standard library's method-aware `http.ServeMux` (Go 1.22+),
avoiding a third-party router. DTOs are separate from domain types so the wire
format and the internal model can evolve independently. JSON decoding is strict
(unknown fields rejected, body size capped, single value enforced).

**Why:** for a handful of endpoints the standard library is more than enough and
carries no dependency or upgrade risk. Strict decoding catches client mistakes
early and closes off a class of abuse.

---

## 3. Assumptions

These fill gaps the brief intentionally left open; each is called out so a
reviewer can challenge it.

1. **At-most-once delivery is acceptable for v1.** Without durable storage and an
   idempotent provider protocol, a crash mid-delivery could drop a notification.
   Building the *interfaces* for durability now (Store, retryable errors) makes the
   move to at-least-once a contained change. True exactly-once requires provider
   idempotency keys, which real providers expose and the simulated ones do not.
2. **Recipients in one request share a channel and message.** This is the common
   "send this to these people" case and keeps the API simple. Multi-channel or
   per-recipient content would be a batch endpoint (§5).
3. **Validation rules are illustrative.** Recipient format, content limits, opt-out
   preferences and per-recipient rate limits are implemented as concrete examples
   of the *shape* of validation; real rules (localisation, quiet hours, dedup,
   templating) slot into the same pipeline.
4. **Providers are simulated** with configurable latency and transient/permanent
   failure rates, standing in for real gateways. The `Provider` interface is what a
   real integration implements.
5. **Single instance, in-process queue.** Horizontal scaling with a shared durable
   queue is out of scope for v1 but explicitly designed for (§5).
6. **AuthN/Z, quotas per tenant, and PII handling are out of scope** for this
   exercise but noted as required before real production exposure (§5).

---

## 4. Trade-offs summary

| Decision | Gained | Given up |
|----------|--------|----------|
| Async + 202 | Low, provider-independent request latency; burst absorption | Clients must poll for outcome |
| Fan-out per recipient | Per-recipient status & isolation | More records at very large fan-out |
| Concurrent fail-fast validation | Meets "stop early" requirement; low latency for I/O checks | Minor overhead for trivial in-memory checks |
| Retry + breaker in delivery layer | Thin providers; uniform, reusable policy | Per-instance breaker state; retries not crash-safe |
| Bounded pool + queue | Predictable resources; burst tolerance | Rejects under sustained overload |
| In-memory store | Simple, zero-setup local run | No durability across restarts |
| Stdlib HTTP/router | No dependency/upgrade risk | Fewer batteries than a full framework |

---

## 5. Future improvements

Ordered roughly by production priority:

1. **Durable storage & queue.** Back `store.Store` with PostgreSQL and replace the
   in-process queue with a durable broker (or a transactional outbox). This turns
   delivery into at-least-once and lets work survive restarts and scale across
   instances. The interfaces are already in place.
2. **Idempotency.** Accept a client `Idempotency-Key` on submit and use provider
   idempotency keys on send to make retries safe end-to-end (toward exactly-once).
3. **Horizontal scale.** With a shared queue and store, run many stateless
   instances; move rate limiting and circuit-breaker state to a shared store
   (e.g. Redis) so limits and health are global.
4. **Observability.** Prometheus metrics (queue depth, attempts, success/fail
   rates, breaker state, latency histograms) and OpenTelemetry tracing across the
   submit→deliver path. Structured logging is already in place as the foundation.
5. **Delivery guarantees & DLQ.** A dead-letter store for exhausted notifications,
   with a re-drive tool, plus scheduled/delayed delivery.
6. **Richer API.** Batch/multi-channel submit, per-recipient content/templating,
   webhook or SSE status callbacks so clients need not poll, and cursor-based
   pagination for large result sets.
7. **Security & multi-tenancy.** Authentication/authorization, per-tenant quotas
   and rate limits, input sanitisation for templated content, and PII-aware
   logging/redaction.
8. **Provider maturity.** Real provider integrations, per-provider concurrency
   limits and adaptive timeouts, and provider fail-over/routing (e.g. secondary
   SMS carrier when the primary's breaker is open).
