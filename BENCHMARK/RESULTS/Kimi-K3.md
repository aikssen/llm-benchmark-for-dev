# Evaluation — Kimi-K3

> Harness: **OpenCode**.

## Executive Summary

The strongest submission outside the top four. A 3,040-LOC
notification-orchestration service with
**zero third-party dependencies**, built around a bounded queue and fixed worker
pool, a fail-fast concurrent validation pipeline, and a resilience stack
(`ResilientProvider`: per-attempt timeout + retry with exponential backoff and
jitter + circuit breaker) that is squarely justified by the brief's "providers
are unreliable" premise.

Three things set it apart from the rest of the field:

1. **Atomic all-or-nothing batch admission** (`Dispatcher.EnqueueBatch`) — a
   multi-recipient request is either fully queued or fully rejected with `503 +
   Retry-After`. Only GPT-5.6-Sol shipped the same property.
2. **It does not swallow store-write errors.** `Dispatcher.transition` logs every
   failed `Upsert`. Every other async submission in this benchmark — GPT-5.6-Sol,
   GPT-5.6-Terra, Gemini-3.5-Flash, GPT-5.5, MiniMax, Grok-4.5 — discards at
   least the terminal status write. This is the only one that does not, and the
   only one to which the near-universal `−5` does not apply.
3. **The rate limiter evicts idle buckets** (`Limiter.maybeSweep`). Nearly every
   other submission ships an unbounded in-memory limiter map.

Verified locally with `go1.26.3` (module declares `go 1.26.3`):
- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l .` — **clean** (no files)
- `go test ./...` — all packages pass (44 tests)
- `go test -race ./...` — all packages pass (no data races)
- Coverage: store 97.4%, ratelimit 91.7%, config 90.7%, dispatcher 82.8%,
  provider 81.0%, validation 79.5%, httpapi 77.2%
- End-to-end: 40 concurrent requests × 5 recipients (200 notifications) at a 30%
  simulated transient error rate → **200/200 delivered**; edge cases return
  400/404 correctly; `SIGTERM` drains cleanly

**The burst path is deadlock-free.** `Dispatcher.process` is self-contained — the
validation pipeline spawns its own goroutines rather than submitting sub-tasks to
the bounded pool — so workers never block waiting on the pool that owns them, and
the burst path was verified end-to-end under a concurrent burst.

What holds it below the Strong-Hire tier is architectural, not operational:
**there is no application layer**. The submit use case (build N aggregates,
persist them, enqueue atomically) lives inside `httpapi.handleCreate`, so a
second transport would have to duplicate it. Plus `−2` for two dead exported
methods.

---

## Requirement Coverage

| Requirement | Status | Evidence |
|-------------|--------|----------|
| Submit a notification | Implemented | `POST /api/v1/notifications` → `Server.handleCreate` (202) |
| Retrieve status | Implemented | `GET /api/v1/notifications/{id}` → `Server.handleGet` |
| List notifications | Implemented | `GET /api/v1/notifications` — filters (`status`, `channel`, `request_id`) + `limit`/`offset` |
| Recipient, channel, message | Implemented | `domain.Notification` with `Content{Subject, Body}`, grouped by `RequestID` |
| Independent validations | Implemented | Four independent `Validator`s in `validation.Pipeline` |
| Stop early on first failure | Implemented | Concurrent pipeline; first failure cancels the shared context — proven by `TestPipelineFailFastCancelsSlowValidators` |
| Deliver only if all validations pass | Implemented | `Dispatcher.process` enters delivery only after `pipeline.Validate` returns nil |
| Multiple recipients | Implemented | One notification per recipient, sharing a `RequestID`; queryable via `?request_id=` |
| Efficient/predictable under bursts | Implemented | Bounded queue + fixed pool + **atomic batch admission** + `503 Retry-After` |
| Unreliable providers | Implemented | `ResilientProvider`: per-attempt timeout, retry/backoff/jitter, circuit breaker |
| Best-effort reliable delivery | Implemented | Temporary vs permanent classification; unknown errors treated as temporary (fail-safe) |
| Add providers with minimal change | Implemented | Implement `provider.Provider` + register; `Registry.Supports` makes the API accept the channel automatically |
| Runnable locally | Implemented | `make run`; **no Dockerfile** |
| README / build & run / tests / config | Implemented | Outstanding README + Makefile + 20 env knobs with validation |

All three endpoints plus `/healthz` are implemented, with the most complete List
filtering in the field (it is the only submission that can group results by
`request_id`).

---

## Benchmark Review

### 1. Correctness (15) — Score: 13
**Strengths:** All endpoints verified end-to-end at 200/200 under a concurrent
burst with a 30% transient error rate — retries genuinely absorb the failures.
Complete lifecycle with a `Status.Terminal()` predicate, per-recipient records
grouped by a queryable `RequestID`, `Status.Valid()` rejecting unknown filter
values with 400, `MaxBytesReader` capping the body, permanent failures
(`undeliverable`) skipping retries, and a duplicate-enqueue guard
(`if n.Status.Terminal() { return nil }`).
**Weaknesses:** No recipient de-duplication within a request — the same address
twice produces two deliveries. `StatusSending` is persisted *before* the attempt
counter is incremented by `ResilientProvider`, so a status read mid-delivery
shows `sending` with `attempts: 0`. On a mid-loop `Upsert` failure the handler
returns 500 with some records already persisted (unreachable with the in-memory
store).
**Evidence:** `httpapi/handlers.go` `handleCreate`; `dispatcher/dispatcher.go`
`process`; e2e burst run.

### 2. Architecture & Project Organization (30) — Score: 25
**Strengths:** Nine cohesive packages with correct inward dependency direction:
`domain` depends on nothing, `store`/`preferences`/`provider` expose interfaces,
and `cmd/server/main.go` is the only place that knows the wiring. Each package
carries a doc comment stating its responsibility. Extension points are real and
documented: `Registry.Supports` means a newly registered channel becomes a valid
API input with no handler change.
**Weaknesses:** **No application/service layer.** `handleCreate` owns the submit
use case — it constructs the domain aggregates, persists them, and drives
`EnqueueBatch`. The use case is therefore reachable only through HTTP; a gRPC
entry point or a CLI would duplicate it, and testing it requires an HTTP round
trip. `httpapi.Server` also depends on three concretes (`*Dispatcher`,
`*Registry`, plus `store.Store`).
**Evidence:** `httpapi/handlers.go:47–99`; `httpapi/server.go` `Server` struct;
package layout under `internal/*`.

### 3. Dependency Injection & Dependency Management (25) — Score: 22
**Strengths:** Constructor injection throughout with an explicit composition root
(`cmd/server/main.go` `run`, labelled as such). Interfaces sit at exactly the
seams that need them: `store.Store`, `provider.Provider`, `preferences.Store`,
`validation.Validator`, `ratelimit` behind the validator. Test seams are
first-class and injected rather than global: `CircuitBreaker.now`,
`Limiter.now`, `ResilientProvider.sleep`, `NewSimulatedWithRand`. Zero
third-party dependencies. No service locator, no singletons.
**Weaknesses:** `main` calls `slog.SetDefault(logger)`, setting global state —
benign, since the logger is also injected explicitly everywhere and nothing
reads the default. `Dispatcher` and `httpapi.Server` take concrete
`*validation.Pipeline`/`*provider.Registry`.
**Evidence:** `cmd/server/main.go:41–89`; `provider/resilient.go:27–29`;
`ratelimit/limiter.go:17`.

### 4. Go Idiomatic Practices (30) — Score: 26
**Strengths:** `gofmt` and `vet` clean. `log/slog`, Go 1.22 method-based
`ServeMux` patterns, `errors.Is`/`errors.As`/`errors.Join`, `sync.Once`,
`sync/atomic` in tests, `MaxBytesReader`, `crypto/rand` for IDs. Interfaces are
small (`Provider` is two methods, `Store` three, `preferences.Store` one) and
**composition is used correctly**: `ResilientProvider` decorates any `Provider`
without inheritance or conditionals, which is the cleanest expression of the
"unreliable providers" requirement in the field. Context is propagated
throughout, and the `Validator` contract explicitly documents the cancellation
obligation.
**Weaknesses:** `domain.NewID` panics if `crypto/rand` fails — documented and
effectively unreachable on modern Go, but a `panic` in library code nonetheless.
`validation.flatten` hand-rolls unwrapping of `errors.Join` trees. The
`ctx.Err() != nil && errors.Is(err, ctx.Err())` guard is correct but awkward.
**Evidence:** `gofmt -l` (clean); `provider/resilient.go`;
`domain/notification.go:102–110`; `validation/pipeline.go:55–65`.

### 5. Concurrency (30) — Score: 27
**Strengths:** The best concurrency story outside the top four, and every claim
is backed by a test.
- **Atomic all-or-nothing admission**: `EnqueueBatch` reserves capacity for the
  whole batch under a mutex before sending, so a request is never half-accepted
  (`TestEnqueueBatchIsAllOrNothing`).
- **Real graceful drain**: `Stop(ctx)` closes the queue and waits for workers to
  exhaust it, with a deadline (`TestStopDrainsQueue`); `main` sequences HTTP
  drain then queue drain, each with its own budget. Verified via `SIGTERM`.
- **Workers use `context.Background()`**, correctly decoupling delivery from the
  HTTP request that queued it — a subtlety much of the field got wrong.
- **Stop/Enqueue coordination is safe**: the `stopped` flag under the same mutex
  guarantees no send on a closed channel.
- Fail-fast validation cancels siblings mid-flight
  (`TestPipelineFailFastCancelsSlowValidators`), per-attempt timeouts stop slow
  providers occupying workers, the breaker stops hammering dead ones.
- Race-clean; the store hands out `Clone()`s so no shared pointer escapes.
**Weaknesses:** The pipeline spawns four goroutines per notification, but with
the shipped configuration (`PrefsLatency` defaults to `0`) all four validators
are cheap in-memory checks — the parallelism buys nothing by default. It is
documented and defensible (a production preferences lookup is remote I/O, and
`NOTIF_PREFS_LATENCY` exists to model it), so this is scored rather than
penalized. Errors are collected by hand-rolled `WaitGroup` + indexed slice
rather than `errgroup` — correct and dependency-free, but more code.
**Evidence:** `dispatcher/dispatcher.go:84–123`; `validation/pipeline.go:83–107`;
`dispatcher/dispatcher_test.go`; shutdown probe via `SIGTERM`.

### 6. Error Handling (20) — Score: 18
**Strengths:** The most rigorous in the benchmark. A typed `provider.Error` with
a `Temporary` flag drives retry decisions via `errors.As`, and `IsTemporary`
defaults unknown errors to *temporary* — an explicitly fail-safe choice ("prefer
retry over silently dropping a notification"). The validation pipeline returns
`errors.Join`ed `*Rejection` values inspectable with `errors.As`, and filters out
its own cancellation artifacts so a rejection reason is never "context
canceled" (`TestPipelineContextErrorFromCancellationIsNotReported`).
**Crucially, `Dispatcher.transition` logs every failed `Upsert` rather than
discarding it** — the one submission in this field that does not swallow the
terminal status write. Config errors propagate out of `Load` and abort startup.
Handlers map errors with `errors.Is` to a consistent
`{"error":{"code","message"}}` envelope. Panic-recovery middleware with stack
capture.
**Weaknesses:** `transition` can only log, not propagate — acceptable for a
background worker, but a failed transition still leaves a stale record with no
retry or alert. `_ = enc.Encode(v)` in `writeJSON` (universal and harmless).
**Evidence:** `provider/provider.go:20–48`; `dispatcher/dispatcher.go:177–187`;
`validation/pipeline.go:94–102`; `httpapi/middleware.go` `recoverer`.

### 7. Code Quality & Readability (20) — Score: 18
**Strengths:** `gofmt` clean. Every package opens with a doc comment explaining
its responsibility and, often, its rationale; every exported symbol is
documented. Functions are small and single-purpose, names are precise
(`ResilientProvider`, `EnqueueBatch`, `maybeSweep`, `RejectionSummary`), and the
comments explain *why* rather than restating the code (e.g. "Workers use a fresh
background context: processing must not be tied to the lifecycle of the HTTP
request that queued the work"). No duplication.
**Weaknesses:** Two dead exported methods (below). A compiled 9.1 MB `bin/server`
binary is shipped in the tree. `NewSimulatedWithRand` is exported and documented
as the deterministic test seam, but no test uses it.
**Evidence:** `dispatcher/dispatcher.go:125–135`; `bin/server`.

### 8. Testing (25) — Score: 22
**Strengths:** 44 tests, and they are **behavioral rather than incidental** —
each one pins a property the design claims:
`TestStopDrainsQueue`, `TestEnqueueBatchIsAllOrNothing`,
`TestPipelineFailFastCancelsSlowValidators` (proves a blocked validator is
interrupted mid-flight, not merely that the call returns),
`TestCircuitBreakerProbeFailureReopens`,
`TestResilientDoesNotRetryPermanentErrors`, `TestResilientHonoursAttemptTimeout`,
`TestReturnedCopiesAreIsolated`, `TestConcurrentAccess`, plus end-to-end HTTP
delivery/rejection tests. Determinism is engineered, not hoped for: injected
clocks and a no-op `sleep` mean the resilience tests neither wait on real
backoff nor flake. Coverage 77–97% across every logic package. Green under
`-race`.
**Weaknesses:** `waitStatus` in the dispatcher and httpapi tests polls with a
wall-clock deadline. The `NewSimulatedWithRand` seam is unused, so the simulated
provider's randomness is never pinned. No load/burst test in the suite (the
burst path was verified externally).
**Evidence:** `dispatcher/dispatcher_test.go`; `provider/provider_test.go`;
`validation/pipeline_test.go`; `store/memory_test.go`.

### 9. Design Patterns & Extensibility (20) — Score: 18
**Strengths:** Every pattern earns its place against the brief: **Decorator**
(`Wrap(provider, cfg)` composing resilience onto any `Provider` — the standout
choice), **Circuit Breaker**, **Registry**, **Repository** (`store.Store`),
**Strategy** (`Validator`), and a token-bucket limiter. Nothing speculative. The
README quantifies extension cost concretely ("adding a channel: implement
`Provider` (~20 lines), register it in `main.go`, add its rules to the two
validators"), and `Registry.Supports` means the API accepts the new channel with
no transport change.
**Weaknesses:** `preferences.Store` has a single implementation, though the
interface is justified — it explicitly stands in for a remote service and is the
seam the `Latency` knob models. Adding a channel still requires editing two
validators' switch statements, a small open/closed leak the README is honest
about.
**Evidence:** `provider/resilient.go`; `provider/circuitbreaker.go`;
`provider/provider.go:86–89`.

### 10. Production Readiness (20) — Score: 16
**Strengths:** 20 env knobs, all documented in the README, and — uniquely in this
field — **validated at load** (`NOTIF_SIM_ERROR_RATE` bounds, `NOTIF_WORKERS`/
`NOTIF_QUEUE_CAPACITY` ≥ 1), with failures aborting startup rather than silently
falling back. `slog` JSON logging, request-ID middleware (honoring an inbound
`X-Request-ID`, length-bounded against log abuse), access logging,
panic-recovery, `/healthz`, full server timeouts, and a **two-phase graceful
shutdown** with independent budgets. `main` is structured as
`main`/`run() error` so nothing calls `os.Exit` deep in the stack.
**Weaknesses:** **No Dockerfile.** No metrics, tracing, or API contract
(OpenAPI) — though the request-ID middleware is explicitly identified as the
seam for them. A compiled binary is shipped in the tree.
**Evidence:** `config/config.go:97–106`; `cmd/server/main.go:110–125`;
`httpapi/middleware.go`.

### 11. Documentation (10) — Score: 10
**Strengths:** The best README in the field. Architecture diagram, per-package
responsibility table, **seven numbered design decisions each with its rationale**,
five assumptions, four trade-offs, seven future improvements, a complete config
table, a lifecycle diagram, and a "simulated behaviour knobs" table that makes
the demo reproducible (`optout`, `optout-<channel>`, `undeliverable`). The
trade-offs are unusually honest — it volunteers that the rate-limit token is
consumed even when a later validator rejects the message, calling it "slightly
conservative counting, much simpler than two-phase commit". Claims are traceable
to code: it cites `TestPipelineFailFastCancelsSlowValidators` as the proof of
fail-fast, and that test does prove it.
**Weaknesses:** Says "Requires Go 1.22+" while `go.mod` declares `go 1.26.3`. The
architecture section does not mention the absent application layer.
**Evidence:** `README.md`; `go.mod`.

### 12. Engineering Judgment (35) — Score: 29
**Strengths:** This reads like an engineer who has operated a service like this.
The judgment shows in the details others missed: workers deliberately decoupled
from the request context; unknown provider errors treated as temporary because
dropping a notification is worse than a redundant retry; the rate limiter
sweeping idle buckets; config validated at load rather than silently defaulted;
`Stop`/`Enqueue` coordinated so a shutdown can never panic a producer; store
errors logged rather than discarded. The resilience stack is exactly as large as
the brief's "providers are unreliable" premise warrants and no larger, zero
dependencies is argued on supply-chain grounds rather than asserted, and the
tests pin the risky properties rather than the easy ones. **The burst path is
deadlock-free by construction**: `process` is self-contained rather than
resubmitting sub-tasks into the same pool that owns its workers, and the drain is
a tested property.
**Weaknesses:** The missing application layer is a real long-term evolution cost
and the clearest thing separating this from the top four: today the submit use
case cannot be reached, reused, or unit-tested without HTTP. Two dead exported
methods and an unused test seam suggest the final sweep was skipped. Four
goroutines per notification to run four regexes is over-machinery for the shipped
configuration. Shipping a 9.1 MB binary in the tree is a hygiene miss.
**Evidence:** `dispatcher/dispatcher.go`; `provider/provider.go:39–48`;
`ratelimit/limiter.go:69–81`; `config/config.go:97–106`.

---

## Critical Findings

- **Dead code — penalty −2.** Two exported methods are never called by any
  caller, in production or tests: `provider.Registry.Channels()`
  (`provider/provider.go:92`) and `dispatcher.Dispatcher.QueueCapacity()`
  (`dispatcher/dispatcher.go:70`). `main` reads `cfg.QueueCapacity` directly
  rather than the accessor. Additionally `validation.IsRejection` is exported but
  used only by its own test, and `provider.NewSimulatedWithRand` — documented as
  the deterministic test seam — is used by no test.
- **Business logic inside HTTP handlers — considered, not penalized.**
  `handleCreate` owns the submit use case (build aggregates → persist → enqueue).
  The rubric's `−5` targets *business rules* in the transport layer; here the
  rules live correctly in `validation` and `dispatcher`, and the handler performs
  thin orchestration only. Extracting it would add a near-pass-through layer, and
  the evaluator guide directs preferring simplicity. Scored as a weakness in
  Architecture (25/30) and Engineering Judgment (29/35) instead.
- **Panic in normal flow — considered, not penalized.** `domain.NewID` panics if
  `crypto/rand.Read` fails. It is documented, effectively unreachable, and not on
  any normal path. Noted under Go Idiomatic Practices.
- **Unnecessary concurrency — considered, not penalized.** Four goroutines per
  notification for validators that are all cheap in-memory checks under the
  default config. The design is documented, the `NOTIF_PREFS_LATENCY` seam models
  the production I/O case, and cancellation is genuinely honored — so it is
  scored in Concurrency (27/30) rather than penalized.
- **Race conditions:** none — `-race` clean across all nine packages. The store
  returns `Clone()`s on read and stores `Clone()`s on write; `TestConcurrentAccess`
  and `TestReturnedCopiesAreIsolated` pin the boundary.
- **Deadlock:** **none — verified.** `Dispatcher.process` is self-contained; the
  validation pipeline spawns its own goroutines rather than submitting sub-tasks to
  the bounded pool, so workers never block on the pool that owns them. The burst
  path was verified end-to-end at 200/200.
- **Ignored errors:** none — `transition` logs every failed `Upsert`; `Upsert`
  errors in `handleCreate` are checked and surfaced as 500.
- **Hidden dependencies / service locator / circular dependencies / hardcoded
  secrets:** none found.
- **Global mutable state:** only `slog.SetDefault` in `main`; the logger is
  injected explicitly everywhere and nothing reads the default. Not penalized.
- **Hygiene (no numeric penalty):** a compiled 9.1 MB `bin/server` binary is
  shipped in the tree; no `.gitignore`; README states Go 1.22+ while `go.mod`
  requires 1.26.3.

Total penalties: **−2**.

---

## Overall Strengths

1. **Atomic all-or-nothing batch admission** — a multi-recipient request is
   never half-accepted. Matched only by GPT-5.6-Sol.
2. **Error handling that does not swallow store writes** — the only submission in
   the field to get this right, and the reason the near-universal `−5` does not
   apply.
3. **A tested graceful drain**, plus `Stop`/`Enqueue` coordination that makes a
   send-on-closed-channel panic structurally impossible.
4. **A resilience stack sized to the brief**: decorator-composed per-attempt
   timeout, retry with backoff and jitter, circuit breaker, and temporary vs
   permanent classification with a fail-safe default.
5. **A test suite that pins properties, not lines** — 44 tests with injected
   clocks and sleeps, proving fail-fast cancellation mid-flight, breaker
   half-open transitions, drain, and copy isolation.
6. **Operational detail others missed**: rate-limiter bucket eviction, config
   validated at load, request-ID middleware, two-phase shutdown, zero
   dependencies.
7. **The best documentation in the field**, honest enough to volunteer its own
   trade-off quirks.

---

## Highest Priority Improvements

1. **Extract an application layer.** Move the submit use case out of
   `handleCreate` into e.g. `internal/app` so it can be unit-tested and reused by
   a second transport. This is the single change that would close most of the gap
   to the top four.
2. **Delete the dead code** — `Registry.Channels()`, `Dispatcher.QueueCapacity()`
   — and either use `NewSimulatedWithRand` in tests to pin the simulated
   provider's randomness, or drop it.
3. **Add a Dockerfile** and a `.gitignore`; stop shipping `bin/server`.
4. **De-duplicate recipients** within a request, and persist `Attempts` on the
   `sending` transition so an in-flight read is not misleading.
5. Reconsider the four-goroutine pipeline: run cheap validators inline and
   parallelize only the I/O-bound ones, or make `PrefsLatency` non-zero by
   default so the shipped configuration matches the documented rationale.
6. Add metrics (queue depth, attempt histograms, breaker state) on the request-ID
   seam the README already identifies.

---

## Hiring Recommendation

**Hire** — at the top of that band, a hair below the Strong-Hire tier.

On every operational axis this is top-four work. It is race-clean, it drains, it
admits batches atomically, it classifies provider failures and retries only what
is worth retrying, it evicts its limiter buckets, it validates its config at
startup, and it is the only submission in the entire field that does not throw
away store-write errors. The tests are the kind that would actually catch a
regression — they prove a blocked validator gets interrupted mid-flight and that
the queue really drains — rather than the kind that pad coverage. The
documentation is honest to the point of volunteering its own rough edges. All of
this with zero third-party dependencies, `gofmt` and `vet` clean.

The handling of the burst path is the strongest signal in the file. Bursts — the
one thing the assessment is centrally about — are where a bounded worker pool most
easily deadlocks, and this design avoids it structurally rather than by patch:
`process` is self-contained and never resubmits sub-tasks into the same pool that
owns its workers, and that behavior is pinned with a test.

What keeps it out of the Strong-Hire tier is a genuine architectural gap rather
than a nit: **the submit use case lives in the HTTP handler**. For a service
explicitly framed as one "your team will maintain and extend over the coming
years", the entry point for the system's central operation should not be
reachable only through a transport. The top four all keep that boundary. Add the
two dead exported methods and an unused test seam it advertises, and the picture
is of an engineer with excellent instincts who did not quite make the final pass.

That is a much smaller gap than the 5-point distance to GPT-5.6-Sol suggests, and
in several specific respects — error rigor, resource hygiene, test quality — this
submission is the better of the two.

---

## Final Score

**Overall Score: 242 / 280**

| # | Category | Score | Max |
|---|----------|------:|----:|
| 1 | Correctness | 13 | 15 |
| 2 | Architecture & Project Organization | 25 | 30 |
| 3 | Dependency Injection & Dependency Management | 22 | 25 |
| 4 | Go Idiomatic Practices | 26 | 30 |
| 5 | Concurrency | 27 | 30 |
| 6 | Error Handling | 18 | 20 |
| 7 | Code Quality & Readability | 18 | 20 |
| 8 | Testing | 22 | 25 |
| 9 | Design Patterns & Extensibility | 18 | 20 |
| 10 | Production Readiness | 16 | 20 |
| 11 | Documentation | 10 | 10 |
| 12 | Engineering Judgment | 29 | 35 |
| | **Subtotal** | **244** | **280** |
| | Critical penalties | −2 | |
| | **Total** | **242** | **280** |

### Justification

The strongest submission outside the top four. The burst path is deadlock-free by
construction — `process` is self-contained rather than resubmitting sub-tasks into
the same worker pool — and was verified at 200/200 under a 30% error rate. It
ships the two properties that separated the top tier — atomic batch admission and
a tested graceful drain — plus the only error handling in the benchmark that does not
swallow store writes, a resilience stack justified by the brief, a rate limiter
that actually evicts, config validated at load, 44 property-pinning tests, zero
dependencies, and the best documentation submitted. Held below Strong Hire by one
real architectural gap — the submit use case lives in the HTTP handler, with no
application layer — plus `−2` for two dead exported methods. Builds, `gofmt`/`vet`
clean, and passes all tests including `-race`. **Hire.**
