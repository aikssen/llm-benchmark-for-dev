# Design

Why this service is built the way it is: the decisions, what they assume, what
they cost, and what I would do next.

The brief left the API and the implementation open, so this document is the
record of the judgement calls. It is written for the engineer who inherits the
service — including me in six months.

---

## 1. The shape of the problem

Three facts drove every other decision:

1. **Providers are unreliable and slow.** They time out, fail intermittently
   and go down. Anything that makes a client wait on them inherits their
   reliability.
2. **One request can target many recipients.** Whatever the unit of work is,
   it has to be the recipient, not the request — otherwise a 500-recipient
   batch is a single lump that fails as a whole and blocks a worker for
   minutes.
3. **Traffic arrives in bursts.** The system must degrade predictably instead
   of accumulating unbounded work.

So: **accept fast, deliver asynchronously, bound everything.**

---

## 2. Architectural decisions

### 2.1 Ports and adapters, with dependencies pointing inward

`domain` imports nothing. `service` depends on interfaces (`store.Store`,
`provider.Provider`, `preferences.Store`, `validation.Limiter`), never on
concrete implementations. `internal/app` is the only package that decides which
implementations get used.

**Why.** The in-memory store and the simulated providers are both known
temporary. Isolating them behind ports means replacing them is a change in one
file rather than a refactor. It also makes the tests cheap: the integration
tests build the *entire* application with a fake preference store or a
one-token rate limiter injected, and exercise it through real HTTP.

**Cost.** More packages and more interfaces than a single-package service
would need. For something this small that is a real cost; it pays for itself
the first time a real provider or a database arrives.

### 2.2 Asynchronous delivery, synchronous acceptance

`POST /v1/notifications` validates, persists, queues, and returns `202` —
typically in well under a millisecond. It does not wait for providers.

**Why.** A provider call takes 20ms–3s and may fail. Making clients hold a
connection through retries would give the API the worst latency and
availability of every provider it talks to, and would cap throughput at the
slowest downstream.

**Cost.** Clients must poll `GET /v1/notifications/{id}` for the outcome. That
is the right trade for notification delivery, which is inherently
fire-and-observe. Webhooks/SSE are the obvious next step (§6).

**Assumption.** Clients care that the request was *accepted* synchronously, and
can learn the outcome asynchronously. If a caller genuinely needed a synchronous
send-and-confirm, that would be a different endpoint with a different contract.

### 2.3 The delivery is the unit of work

A request with N recipients creates one `Notification` and N `Delivery`
records, and enqueues **N jobs**.

**Why.** Recipients are independent. One opted-out user, one malformed device
token or one unreachable address must not affect the other 499. Per-recipient
jobs also spread a large batch across the whole pool instead of pinning one
worker.

**Consequence.** The notification's status is a *derived* rollup —
`delivered`, `partially_delivered`, `failed` — and the API returns a `summary`
so a client does not have to count the deliveries itself.

### 2.4 Notification status is derived, not stored

`Notification.Status()` computes the aggregate from the deliveries on every
read, rather than maintaining a stored field.

**Why.** A stored status has to be updated on every delivery transition, by
concurrent workers, and any missed update leaves the two permanently
disagreeing — a class of bug that is miserable to debug. Deriving it makes
disagreement impossible.

**Cost.** Filtering by status in `List` is a scan rather than an index lookup.
Fine for an in-memory store; with a SQL store, this becomes a persisted,
indexed column maintained in the same transaction as the delivery update, and
`Notification.Status()` stays as the single definition of the rule.

### 2.5 Jobs are self-contained snapshots

A queue job carries the channel, message, recipient and metadata — not just a
pair of IDs.

**Why.** The alternative is a store read per job. For a 5,000-recipient
notification that means re-reading the same aggregate 5,000 times, which with
the current store also means 5,000 deep copies: accidentally quadratic. It also
mirrors what a message on a real broker looks like, so moving to one later does
not change the worker.

**Cost.** The message body is held once per in-flight job. Bounded by
`QUEUE_CAPACITY`, and `SERVER_MAX_REQUEST_BYTES` bounds the body, so worst-case
memory is a knowable product of two configured numbers rather than a surprise.

### 2.6 Bounded queue with all-or-nothing admission

`queue.Reserve(n)` claims N slots atomically or fails. Only then is the
notification persisted and the jobs published, so `Publish` is guaranteed never
to block.

**Why.** Two properties fall out of this:

- *Predictable resource usage.* Queue depth never exceeds `QUEUE_CAPACITY`, and
  in-flight work never exceeds `WORKER_COUNT`. Both are configured, not
  emergent. An unbounded queue turns a traffic spike into unbounded memory
  growth and ever-growing latency — the service looks healthy right up until it
  is not.
- *No partially-queued requests.* Reserving per recipient could accept 300 of
  500 and fail on the rest, leaving the client with a notification silently
  missing recipients. All or nothing means the client's mental model is
  correct.

A refused request gets `429` with `Retry-After` — honest backpressure. A request
larger than the entire queue gets `400` instead, because retrying can never
help; it is the request that must change.

**Cost.** A large request can be refused while capacity exists for a small one.
Acceptable: `MAX_RECIPIENTS_PER_REQUEST` is validated at start-up to be no
larger than `QUEUE_CAPACITY`, so the pathological case cannot be configured.

### 2.7 Fixed-size worker pool

`WORKER_COUNT` goroutines consume the queue.

**Why.** The pool size *is* the cap on concurrent provider calls. Spawning a
goroutine per delivery would let a burst translate directly into thousands of
simultaneous calls to a provider that is already struggling — the surest way to
turn a spike into an outage.

**Cost.** Throughput is capped by `WORKER_COUNT × (1 / provider latency)`.
Tuning is one number, and queue depth on `/readyz` says when to turn it up.

### 2.8 Validation runs concurrently and fails fast

The brief was explicit: the checks are independent, and *"if one validation
fails, the remaining validations should not continue consuming unnecessary
resources."*

`validation.Pipeline.Run` starts every validator on its own goroutine against a
shared derived context. The first failure records the error and **cancels the
context**, so the others observe cancellation and abandon their work mid-flight.

```go
if err := v.Validate(runCtx, s); err != nil {
    once.Do(func() {
        firstErr = attribute(v, err)
        cancel() // stop the rest: their answer cannot change the outcome
    })
}
```

Two details that matter:

- **`Run` still waits for the cancelled goroutines.** Returning while they run
  would make resource usage unpredictable — the opposite of the goal — and would
  leak work into the next delivery's time slice.
- **Latency is the slowest check, not their sum.** With a preference lookup over
  the network, that is the difference between 20ms and 80ms per delivery.

The mirror-image decision is that *request-level* validation (§3.1) fails fast
**sequentially**. Those checks are microseconds of pure CPU; goroutines would
cost more than they save. Same principle — do no unnecessary work — opposite
mechanism, because the inputs are different.

### 2.9 Rejection ≠ error

A validator can fail two ways, and conflating them causes real damage:

| Outcome                                | Meaning                       | Delivery status | Retried |
| -------------------------------------- | ----------------------------- | --------------- | ------- |
| `*validation.Rejection`                | The delivery breaks a rule    | `rejected`      | never   |
| Any other error                        | The *check* could not be done | `failed`        | yes     |

If the preference service is unreachable and that were recorded as "opted out",
the service would permanently discard legitimate notifications because of a
transient outage. So `PreferenceValidator` returns a wrapped error, not a
rejection, when its lookup fails.

The same distinction runs through the provider layer: `provider.Error` carries
an explicit `Retry` flag, and `resilience.IsRetryable` treats **unclassified
errors as permanent**. Retrying an error nobody understood is how a service
sends duplicates or spins on a bug.

### 2.10 Providers own their channel

`provider.Provider` has four methods: `Name`, `Channel`, `ValidateAddress`,
`Send`. Crucially, `ValidateAddress` lives here.

**Why.** The shape of a valid destination is a property of the integration, not
of the core model. Putting it in the provider means a new channel brings its own
address rules with it — nothing in `domain`, `service` or `validation` learns
about SMS to support SMS.

`domain.Channel` is a plain string type rather than a closed enum, and the
**registry is the authority** on which channels exist. Registering a provider is
therefore sufficient to make a channel submittable, advertised on
`/v1/channels`, breaker-protected, and covered by the retry policy.
`TestANewChannelNeedsOnlyAProvider` asserts this end to end, using a `webhook`
channel that exists nowhere in the production code.

Registering two providers for one channel is an error, not a silent overwrite:
that mistake would otherwise stay invisible until messages went to the wrong
place.

### 2.11 Reliability lives in the dispatcher, not in providers

Per-call timeout → bounded retries with exponential backoff and jitter →
circuit breaker, all applied uniformly in `internal/dispatcher`.

**Why.** A new integration inherits every policy for free and cannot
accidentally opt out of it. Provider code stays small and honest: make the call,
classify the error.

- **Timeout.** Without one, a hanging provider pins workers indefinitely and
  eventually stalls every channel, not just its own.
- **Jitter.** Deliveries that fail together retry together; without
  randomisation they re-create the exact spike that caused the failure.
- **Breaker, per provider.** Once a provider is clearly down, retrying every
  delivery three times just adds load to a struggling system and slows the
  queue. The breaker fails those deliveries immediately with
  `provider_unavailable`. Only **transient** failures count against it — one
  malformed address must not take a channel down for everyone.

An open breaker records `attempts: 0`, because the provider genuinely was not
called. (This started as `attempts: 1`; an integration test caught it. Reporting
an attempt that never happened would tell an operator the provider was tried
when it was not.)

### 2.12 Every path writes a terminal state

`processing.Processor.Process` has one job: no matter which branch it takes —
rejection, broken validator, provider failure, cancellation — the delivery ends
`delivered`, `rejected` or `failed`.

Outcomes are persisted with `context.WithoutCancel`, so a shutdown mid-delivery
still records what happened. Otherwise a drain would leave deliveries stuck in
`sending` forever: invisible to clients, invisible to alerting.
`TestProcessAlwaysReachesATerminalState` covers every branch.

### 2.13 No third-party dependencies

Standard library only.

`net/http`'s method-aware routing (Go 1.22+) covers this API completely;
`log/slog` is structured logging; `errors.As`/`Is` is the classification
mechanism; a UUID library buys nothing over 128 bits of `crypto/rand` rendered
as a prefixed hex string (`ntf_…`, `dlv_…`, which are also far easier to read in
a log than a bare UUID). A router, a logging framework and an assertion library
would each be weight without benefit here.

The trade-off is real and I would not hold it as dogma: the moment this service
needs Prometheus metrics, OpenTelemetry tracing or a Postgres driver, those are
dependencies worth taking. The point is that each one should have to earn its
place.

---

## 3. Assumptions

Where the brief was silent, I chose — and this is what I chose.

### 3.1 Two layers of validation, deliberately

| | Request validation (synchronous) | Delivery validation (asynchronous) |
| --- | --- | --- |
| **Where** | `service.validateCommand` | `validation.Pipeline` in the worker |
| **Checks** | channel supported, body present, address syntax, duplicates, fan-out cap | recipient, preferences, rate limit, content limits |
| **Cost** | microseconds, pure CPU | I/O and stateful budgets |
| **On failure** | `400`, nothing stored | delivery `rejected`, recorded per recipient |
| **Execution** | sequential, fail-fast | concurrent, fail-fast |

**Why split them.** A client that sends `"channel": "carrier-pigeon"` or a
malformed phone number should get a `400` it can fix, immediately — not a `202`
followed by a rejected delivery it has to poll for. Conversely, per-recipient
checks that touch state (preferences, rate limits) belong where the work is
done, so their result is recorded per recipient rather than failing a whole
batch.

**Why only the first error is reported.** The requirement says to stop
consuming resources on the first failure, and a caller cannot act on the second
error until the first is fixed anyway. The error envelope already carries a
`field`, so returning a list later would be an additive change.

### 3.2 Recipients, preferences and identity

- `recipients[].id` is the platform user id and keys preferences and rate
  limits; when absent, the address is used. This keeps the API usable for
  one-off transactional sends to addresses with no user record.
- **Default is opt-in.** An unknown recipient is allowed. These are
  transactional notifications; refusing a password reset because the user never
  set a preference would be wrong. A marketing-grade service would invert this.
- **Opt-out is per channel.** Muting email must not mute SMS.
- **`Blocked`** suppresses every channel — the hard-bounce / account-closed /
  abuse case.
- **Duplicate recipients in one request are rejected**, rather than silently
  deduplicated. Sending the same person the same message twice is almost
  certainly a bug in the caller, and saying so is more useful than papering
  over it.

### 3.3 Rate limits are per recipient, per channel

A token bucket, not a fixed window: fixed windows allow twice the limit across
a boundary, which is exactly wrong for the bursty traffic this service expects.
Buckets idle longer than ten intervals are reclaimed, so a long-running process
does not accumulate one entry per recipient it has ever seen.

**Limits are per instance.** Two replicas allow twice the configured rate. This
is documented rather than hidden; §6 has the fix.

### 3.4 Content limits are per channel

SMS is billed per segment, push payloads are capped by the mobile platforms,
email bodies are effectively unbounded but still need a ceiling. Limits are
counted in **characters, not bytes**, because a "1600 character" SMS limit is
about characters — a body of 10 emoji is 10 characters, not 40.

An unrecognised channel falls back to a conservative default, so a new channel
is bounded from the moment it is registered.

### 3.5 Idempotency is opt-in via header

`Idempotency-Key` is a header, not a body field: it describes the delivery of
the *request*, not the notification. Replaying a key returns the original
notification and enqueues nothing.

**Limitation.** Two concurrent requests with the same key can both pass the
lookup before either has stored, and one then fails on the store's uniqueness
constraint. Correctly fixed by a conditional insert plus retry-on-conflict once
there is a real database; the current in-memory store does enforce uniqueness,
so nothing is double-sent — one request just gets a 500 instead of the original
notification.

### 3.6 Requirements I added

Not asked for, but a production service is not credible without them, and each
is small:

- **Idempotency keys** — clients time out and retry; duplicate notifications are
  a user-visible defect.
- **Graceful shutdown that drains** — deploys happen constantly; silently
  dropping accepted work at every rollout is not acceptable.
- **Liveness vs readiness** — they cause different scheduler actions: restart
  the process vs remove it from the load balancer. `/readyz` also reports queue
  depth and breaker states, which is what you actually want during an incident.
- **Request IDs** — with per-recipient fan-out and retries, correlation is the
  only way to answer "what happened to this message?". Incoming
  `X-Request-ID` is preserved so traces span services.
- **Strict JSON decoding** — `"recipeints"` silently ignored means sending the
  wrong notification.
- **Body size limit, request timeout, panic recovery** — table stakes for
  anything facing a network.
- **`GET /v1/channels`** — clients should discover what a deployment supports
  rather than hard-coding a list that drifts.

---

## 4. Trade-offs, stated plainly

| Decision | Bought | Cost | Why it is right *for now* |
| --- | --- | --- | --- |
| In-memory store | Zero setup; `make run` just works | **Not durable**; single instance | The port is the design; the adapter is a detail (§6.1) |
| In-process queue | No broker to operate | Queued work is lost on a hard kill | Graceful shutdown drains; §6.1 makes it durable |
| Retries in-process | Simple, readable, one code path | A retrying delivery occupies its worker | Backoff is capped low (≤2s); a delay queue is §6.2 |
| No redelivery after final failure | Bounded, predictable behaviour | A provider outage longer than the retry budget loses those deliveries | Honest terminal state beats a fake guarantee; §6.2 |
| Derived status | Cannot disagree with the deliveries | Status filtering scans | Correctness first; an indexed column is a store change |
| Fail-fast validation | Meets the requirement; less work | Client sees one error at a time | Envelope already supports a list |
| Per-instance rate limits | No shared infrastructure | N replicas ⇒ N× the limit | Interface makes Redis a drop-in (§6.3) |
| Standard library only | No supply chain, fast builds, no churn | Some hand-written plumbing | Each dependency should earn its place |
| Simulated providers | Runnable and testable with no credentials | Not proof against a real API | The port is what matters; behaviour is tunable |

**The honest summary:** *durability* is what separates this from production.
Everything else is tuning. That is deliberate — the structure makes durability
an adapter swap rather than a rewrite.

---

## 5. Testing strategy

88.6% statement coverage, but the number is not the point; what is tested is.

**Behaviour, not implementation.** Tests assert what a caller can observe:
status codes, error codes, delivery states, whether the provider was called at
all. Refactors do not break them.

**The concurrency claims are tested as claims, not hoped for.**

- `TestPipelineCancelsRemainingValidatorsOnFirstFailure` blocks every validator
  until all three are confirmed running, releases only the failing one, and
  asserts the others *observed cancellation and did not finish*. It fails if
  fail-fast is ever quietly removed.
- `TestPipelineRunsValidatorsConcurrently` deadlocks if the pipeline ever
  becomes sequential.
- `TestPoolBoundsConcurrency` measures peak in-flight work against the pool size.
- `TestConcurrentReservationsRespectTheBound` races 200 goroutines at a 64-slot
  queue and asserts exactly 64 slots are granted.
- Everything runs under `-race` in CI (`make test`).

**Time and randomness are injected**, never slept on: `WithClock`, `WithRandom`,
`WithSleep`. Circuit-breaker cooldowns and token-bucket refills are tested by
advancing a fake clock, so the suite is fast and not flaky.

**Integration tests build the real thing.** `internal/app` wires the entire
application and drives it over HTTP, covering the interactions unit tests
deliberately isolate: end-to-end delivery, mixed rejection outcomes,
retry-then-fail against a dead provider, breaker load shedding, rate-limit
rejection, burst shedding with backpressure, graceful drain, and adding a new
channel.

**A test caught a real bug.** `TestTheCircuitBreakerShedsLoadFromADeadProvider`
failed on the first run because deliveries shed by an open breaker were
recording `attempts: 1` for a call that never left the process. That is exactly
the kind of quiet lie that misleads an operator at 3am — see §2.11.

---

## 6. What I would do next

In priority order. The first item is the one that matters.

### 6.1 Durability: Postgres + a real broker

**The gap:** a restart loses notification history and any queued work.

- Implement `store.Store` over Postgres. `UpdateDelivery` is already a targeted,
  atomic operation rather than read-modify-write, so it maps to a single `UPDATE`
  with no lost-update risk. Persist `status` as an indexed column maintained in
  the same transaction, keeping `Notification.Status()` as the rule's definition.
- Replace the in-process queue with SQS/Kafka/NATS, or the transactional-outbox
  pattern for exactly-once-ish semantics without a second system. `queue.Job` is
  already a self-contained message, and the pool already treats the queue as a
  stream, so this is largely a new adapter.
- Add a dead-letter queue for deliveries that exhaust their budget, plus an
  endpoint to replay them.

### 6.2 Scheduled redelivery

Today a delivery that exhausts its retry budget is terminal. A provider outage
lasting longer than a few seconds therefore loses those deliveries. With durable
storage, a background reconciler can pick up `failed` deliveries whose failure
was transient and re-enqueue them on a longer backoff (minutes, then hours),
with a per-notification expiry. This is the real "best effort" guarantee; the
current implementation is honest about only offering the in-request version.

### 6.3 Shared rate limiting

Move the token bucket to Redis so limits hold across replicas. `validation.Limiter`
is a single-method interface, so this is a new adapter and a wiring change.

### 6.4 Observability beyond logs

Prometheus metrics — deliveries by channel and outcome, provider latency
histograms, retry counts, queue depth, breaker state — plus OpenTelemetry
tracing from HTTP request through validation to the provider call. Structured
logging already carries the correlation IDs these need. This is where I would
take the first dependencies.

### 6.5 API and product surface

- **Webhooks or SSE** for delivery outcomes, so clients stop polling.
- **Templating** — clients almost always want `template_id` + variables rather
  than pre-rendered bodies, with per-channel and per-locale rendering.
- **Scheduled sends** (`send_at`) and **cancellation** of pending deliveries.
- **Cursor pagination** for `List`; offset pagination skips and duplicates rows
  when the underlying set changes under a client.
- **Provider fallback** — a secondary provider per channel, used when the primary's
  breaker is open. The registry becomes channel → ordered list; the dispatcher
  walks it. Deliberately left out for now: it needs a real dedupe story so a
  timeout followed by a fallback does not send twice.

### 6.6 Security and multi-tenancy

Authentication (API keys or OAuth), per-tenant quotas and per-tenant data
isolation. `metadata` is the natural place for a tenant id today, but real
isolation means it belongs on the aggregate and in every query. Notification
bodies are PII and would need encryption at rest and redaction in logs.

### 6.7 Operational hardening

Per-provider concurrency limits (a semaphore in the dispatcher) so one channel
cannot starve the others; a priority queue so transactional messages overtake
bulk ones; load tests establishing the real throughput ceiling per worker count.
