# Evaluation — MiniMax-M3

> Harness: **OpenCode**.

## Executive Summary

A well-structured submission whose architecture is genuinely strong but whose delivery
path carries a cluster of correctness defects, the most serious of which discards write
errors on every status transition.

The gains are real. The package layout is a clean `domain` / `application` /
`infrastructure` split with a genuine service layer and thin handlers. Configuration
is **validated at load** and aborts startup on bad input. A **background sweeper**
evicts terminal notifications past a TTL — one of only two submissions in the field to
address the unbounded in-memory map that nearly everyone else ships. And the
**shutdown drains correctly**: `main` calls `dispatcher.Shutdown()` *before*
`cancel()`, which is the ordering three other submissions in this benchmark get
backwards. A probe confirmed **20 of 20 queued notifications processed** on shutdown.

The problem is the delivery path. **All nine `_ = d.repo.Update(...)` sites in the
dispatcher discard their write error**, including the terminal transitions — a silent
lost-transition bug the moment the repository is backed by a database. Alongside it: a
fully-implemented **parallel validation pipeline that production never calls**, an
`errors.Join` misuse that emits literal newlines into a client-facing API field, a
queue-full path that returns `202` for work it never queued, and an
`Enqueue`-after-`Shutdown` panic.

Verified locally with `go1.26.3` (module declares `go 1.22`):
- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l .` — **clean**
- `go test ./...` — all packages pass (29 tests)
- `go test -race ./...` — all packages pass (no data races)
- Coverage: repository 94.4%, application 79.7%, providers 71.9%, validation 70.3%,
  domain 69.6%, httpapi 64.4%
- End-to-end: 40 concurrent requests × 5 recipients (200 notifications) at a 20%
  failure / 5% unavailable rate → **200/200 delivered**; edge cases return 400/404
  correctly; `SIGTERM` drains cleanly

---

## Requirement Coverage

| Requirement | Status | Evidence |
|-------------|--------|----------|
| Submit a notification | Implemented | `POST /v1/notifications` → `Handler.Submit` → `NotificationService.Submit` (202) |
| Retrieve status | Implemented | `GET /v1/notifications/{id}` → `Handler.Get` |
| List notifications | Implemented | `GET /v1/notifications` — `channel`/`status` filters + `limit`/`offset` |
| Recipient, channel, message | Implemented | `domain.Notification{Recipient, Channel, Message{Subject,Body,Metadata}}` |
| Independent validations | Implemented | `Pipeline` with content, preference and rate-limit validators |
| Stop early on first failure | Implemented | `Pipeline.Run` is sequential and returns on the first failure |
| Deliver only if all validations pass | Implemented | `dispatchWithRetry` entered only after `pipeline.Run` returns nil |
| Multiple recipients | Implemented | Fan-out to one notification/job per recipient |
| Efficient/predictable under bursts | Partial | Bounded pool + bounded queue + drain; **but no `503` — queue-full returns `202` with the record marked `failed`** |
| Unreliable providers | Implemented | `Simulated` provider with latency, failure and unavailability rates |
| Best-effort reliable delivery | Implemented | Retry with capped exponential backoff + jitter, per-attempt timeout |
| Add providers with minimal change | Implemented | Implement `providers.Provider` + `Registry.Register` |
| Runnable locally | Implemented | `make run`; `scripts/smoke.sh`; **no Dockerfile** |
| README / build & run / tests / config | Implemented | 284-line README + `.env.example` + Makefile |

All three endpoints plus `/healthz` are implemented.

---

## Benchmark Review

### 1. Correctness (15) — Score: 12
**Strengths:** Verified end-to-end at 200/200 under a concurrent burst with a 20%
failure and 5% unavailability rate — retries absorb them. Full lifecycle
(`pending → validating → validated → sending → delivered`, with `rejected`/`failed`),
`Status.Terminal()` guard against duplicate processing, `DisallowUnknownFields`,
config validated at load, TTL sweeper, and a **shutdown that genuinely drains (20/20
verified)**.
**Weaknesses:** **No backpressure signal.** When the queue is full, `Submit` marks the
notification `failed` with reason "service overloaded" **but still returns `202` with
the notification listed** — the client is told its request was accepted for work that
was never queued. **`List` ordering is nondeterministic**: `sort.Slice` (not stable)
on `CreatedAt`, and every recipient in a batch shares one `now` timestamp, so ties are
ordered arbitrarily and pagination can skip or duplicate records across pages.
**Evidence:** `application/service.go:79–86`; `repository/memory.go:82`;
`application/service.go:65`; drain probe.

### 2. Architecture & Project Organization (30) — Score: 25
**Strengths:** A clean three-layer split: `domain` (pure),
`application` (service + dispatcher + validation), `infrastructure` (repository,
providers, httpapi). Handlers are thin — decode, delegate, encode. The service and
dispatcher are properly separated, and the composition root is confined to `main`.
**Weaknesses:** **The dependency direction inverts at the application boundary.**
`application/dispatcher.go` imports `internal/infrastructure/repository` and
`internal/infrastructure/providers` — the `NotificationRepository` interface is
*defined in the infrastructure package* and consumed by application, so the inner
layer depends on the outer one. For a layout that otherwise advertises DDD naming,
this is the one seam that undercuts it. The `ValidationPipeline` interface declares a
`RunParallel` method no caller uses.
**Evidence:** `application/dispatcher.go:12–15`; `repository/memory.go:12–17`;
`application/dispatcher.go:17–20`.

### 3. Dependency Injection & Dependency Management (25) — Score: 21
**Strengths:** Constructor injection throughout, single composition root, interfaces at
several seams (`NotificationRepository`, `RateLimiter`, `ValidationPipeline`), an
injectable `now` clock on the service, and zero third-party dependencies.
**Weaknesses:** `slog.SetDefault(logger)` sets global state in `main`.
`NotificationService` takes a concrete `*Dispatcher` and `Dispatcher` a concrete
`*providers.Registry`, so neither seam can be substituted. `DispatcherConfig` is
passed whole rather than as the values each component needs.
**Evidence:** `cmd/server/main.go:25`; `application/service.go:26–31`;
`application/dispatcher.go:44–55`.

### 4. Go Idiomatic Practices (30) — Score: 22
**Strengths:** `gofmt` and `vet` clean, `log/slog`, Go 1.22 method-pattern `ServeMux`,
sentinel errors in `domain/errors.go`, `errors.As` for the typed `ValidationError`,
`DisallowUnknownFields`, a mutex-guarded jitter RNG.
**Weaknesses:** **`errors.Join` is misused as string concatenation** —
`errors.Join(errors.New(v.Name()+": "), err)` — and since `Join` separates with `\n`,
this emits a literal newline into a client-facing field. Verified live:
`"reason": "content: \ninvalid email recipient: \"not-an-email\""`. `newID()` **swallows
a `crypto/rand` failure** and silently falls back to a timestamp-derived ID, trading an
error for a collision risk. Handlers re-check `r.Method` even though the `ServeMux`
patterns already constrain it. **Exported symbols carry almost no doc comments** — zero
in `domain/notification.go` and `application/dispatcher.go`.
**Evidence:** `validation/pipeline.go:34,46,70`; `application/service.go:46–52`;
`httpapi/handlers.go:54,84,102`; e2e API response.

### 5. Concurrency (30) — Score: 24
**Strengths:** Bounded worker pool with a bounded queue keeps memory predictable.
**The shutdown drains** — `main` sequences `srv.Shutdown()` → `dispatcher.Shutdown()`
→ `cancel()`, closing the queue before cancelling so workers exhaust it; a probe
measured **20/20 processed**, and `Shutdown` bounds the wait with a timeout. This is
the ordering three other submissions in this benchmark get backwards. Per-attempt
timeouts, capped exponential backoff with mutex-guarded jitter, repository clones on
read and write, `-race` clean.
**Weaknesses:** **`Enqueue` after `Shutdown` panics** (`send on closed channel`,
reproduced) — `Shutdown` closes `d.queue` with no `stopped` guard, while `Enqueue`
remains public and sending. Reachable if `srv.Shutdown` hits its timeout with handlers
still in flight. `drainQueue` ranges over `d.queue`, which blocks forever if the
context is cancelled without the queue ever being closed. No `503` backpressure.
**Evidence:** `application/dispatcher.go:88–111,130–134`; drain and panic probes.

### 6. Error Handling (20) — Score: 14
**Strengths:** Centralized sentinels, a typed `ValidationError{Field, Message}`
surfaced with `errors.As` so the API can return the offending field, and provider
errors distinguished from timeouts in logging.
**Weaknesses:** **Nine `_ = d.repo.Update(...)` sites in `Dispatcher.process` and
`dispatchWithRetry`, plus one in `service.Submit`** — every status transition,
including the terminal ones, discards its write error. Harmless against the in-memory
map, but a silent lost-transition bug the moment the repository is backed by a
database. `newID` swallows a `crypto/rand` error. No panic-recovery middleware on the
HTTP layer. **Penalty −5.**
**Evidence:** `application/dispatcher.go:148,153,158,163,168,180,188,193`;
`application/service.go:84`.

### 7. Code Quality & Readability (20) — Score: 15
**Strengths:** `gofmt` clean, small functions, consistent naming, clear package
boundaries.
**Weaknesses:** **Exported symbols are essentially undocumented** (`domain` and
`dispatcher` have zero doc comments) — a real maintainability cost for a codebase
pitched as a long-lived foundation. Dead `RunParallel`/`Pipeline.Add`/`repo.Len`.
`ContentValidator` is misnamed: it validates the **recipient** as well as the message,
merging two of the checks the brief describes as independent. Redundant method guards
in every handler. A compiled `bin/notification-server` binary is committed.
**Evidence:** `validation/content.go:14–24`; `httpapi/handlers.go:54`;
`grep -c '^// ' internal/domain/notification.go` → 0.

### 8. Testing (25) — Score: 19
**Strengths:** 29 tests across every layer, coverage 64–94%. Meaningful cases: retry
eventually-succeeds and exhausts-attempts, validation rejection, no-provider failure,
list pagination, sweeper eviction, HTTP timeout handling, token-bucket limiting.
**Weaknesses:** **No shutdown/drain test and no burst test** — the drain is correct,
but by construction rather than by proof, so the ordering that makes it work is
unprotected against a future refactor. `TestPipeline_RunParallel_CancelsOnError` tests
a method production never calls. Nothing covers the queue-full path.
**Evidence:** `application/service_test.go`; `validation/validation_test.go:36`.

### 9. Design Patterns & Extensibility (20) — Score: 16
**Strengths:** Registry, Repository, Strategy (`Validator`), pipeline and dispatcher —
each justified, none gratuitous. A single simulated provider serving three channels is
a sensible simplification. The TTL sweeper is a well-judged addition.
**Weaknesses:** The `RunParallel` pipeline is a complete, tested implementation of the
brief's "independent validations" that **the dispatcher never calls** — an abstraction
built and abandoned. `ContentValidator` conflates recipient and message validation,
weakening the Strategy split it advertises.
**Evidence:** `application/dispatcher.go:150`; `validation/pipeline.go:40–77`.

### 10. Production Readiness (20) — Score: 15
**Strengths:** One of the strongest axes of the submission. **Config validated at load**
(`Config.Validate`) with startup aborted on bad input, `.env.example` documenting every
knob, a Makefile with a `smoke` target plus `scripts/smoke.sh`, `slog` JSON logging,
`/healthz`, full server timeouts (`ReadHeaderTimeout`/`Read`/`Write`/`Idle`), a
**graceful drain that works**, and a **TTL sweeper** bounding memory growth.
**Weaknesses:** **No Dockerfile.** No panic-recovery
middleware, no metrics or tracing. A compiled binary is committed.
**Evidence:** `config/config.go:57–70`; `cmd/server/main.go:72–75,106–115`; `Makefile`.

### 11. Documentation (10) — Score: 9
**Strengths:** A 284-line README: requirements, quick start, API examples for all three
endpoints, a configuration table, architecture, a concurrency-model section, validation
pipeline, provider abstraction, reliability features, an **extensibility checklist**, a
design-decisions table, assumptions and trade-offs, and future improvements.
**Weaknesses:** Documents a "validation pipeline" whose parallel implementation is not
the one that runs. Does not disclose that a full queue yields `202` rather than a
rejection. The README carries the codebase's documentation load almost alone, since the
source has virtually no doc comments.
**Evidence:** `README.md`; `application/dispatcher.go:150`.

### 12. Engineering Judgment (35) — Score: 26
**Strengths:** The production instincts are good: config
validated rather than silently defaulted, a sweeper that bounds memory, a shutdown
ordering that actually drains, an extensibility checklist in the README. The
`domain`/`application`/`infrastructure` split is a real structural asset.
**Weaknesses:** The defining weakness sits on the delivery path. The nine discarded
`repo.Update` errors run through every status transition in `Dispatcher.process` and
`dispatchWithRetry`, including the terminal ones. Around them sit a parallel pipeline
built and left unwired, an `errors.Join` misuse that puts raw newlines in an API field,
a `202` returned for work that was never queued, and a `close`-without-guard panic. Each
is individually small; together they say the work optimized structure over rigor, and
that a single pass over the delivery path — the feedback loop that would have caught
them — was not run.
**Evidence:** `application/dispatcher.go`; `validation/pipeline.go:34`;
`application/service.go:79–86`.

---

## Critical Findings

- **Ignored errors — penalty −5.** Nine `_ = d.repo.Update(ctx, n)` calls across
  `Dispatcher.process` and `dispatchWithRetry` cover every status transition including
  the terminal ones, plus `_ = s.repo.Update(ctx, n)` in `service.Submit`'s
  queue-full path. Harmless against the in-memory map; a silent lost-transition bug the
  moment the repository is backed by a database.
- **Dead code — penalty −2.** `Pipeline.RunParallel` is implemented (37 lines),
  declared in the `ValidationPipeline` interface, and tested — but `Dispatcher.process`
  calls only `Run`, so it never executes in production. `Pipeline.Add` has no callers
  at all. `MemoryRepository.Len` is used only by tests.
- **Panic path (no numeric penalty; scored in Concurrency).** `Dispatcher.Shutdown`
  closes `d.queue` while the public `Enqueue` may still be sending, with no `stopped`
  guard: reproduced `panic: send on closed channel`. It requires a shutdown-timeout
  race rather than the normal path, so it is scored rather than penalized — consistent
  with the treatment of comparable shutdown-race panics elsewhere in this benchmark.
- **API formatting defect (no numeric penalty; scored in Idiom/Correctness).**
  `errors.Join(errors.New(name+": "), err)` uses `Join` as string concatenation; `Join`
  separates with `\n`, so the client-facing `reason` field contains a literal newline.
  Verified: `"reason": "content: \ninvalid email recipient: \"not-an-email\""`.
- **Race conditions:** none — `-race` clean; the repository clones on read and write.
- **Business logic in handlers:** none — handlers decode, delegate, encode.
- **Circular dependencies:** none, though `application` importing
  `infrastructure/repository` and `infrastructure/providers` inverts the intended
  dependency direction (scored in Architecture).
- **Hidden dependencies / service locator / hardcoded secrets:** none found.
- **Global mutable state:** `slog.SetDefault` in `main` only; the logger is injected
  explicitly everywhere. Not penalized.
- **Hygiene (no numeric penalty):** a compiled `bin/notification-server` binary is
  committed; exported symbols are essentially undocumented; no Dockerfile.

Total penalties: **−7**.

---

## Overall Strengths

1. **A shutdown that actually drains** — `Shutdown()` before `cancel()`, verified 20/20,
   which three other submissions in this benchmark get backwards.
2. **Config validated at load**, aborting startup rather than silently defaulting.
3. **A TTL sweeper** evicting terminal notifications — one of only two submissions to
   bound in-memory growth.
4. Clean `domain`/`application`/`infrastructure` layering with thin handlers and a real
   service layer.
5. Zero third-party dependencies; `gofmt`, `vet` and `-race` all clean; 200/200
   end-to-end under a burst.
6. A thorough README with an extensibility checklist and an honest design-decisions
   table.

---

## Highest Priority Improvements

1. **Stop discarding `repo.Update` errors** — all nine sites. This is the single
   highest-leverage fix available.
2. **Either wire `RunParallel` or delete it.** The brief calls the validations
   independent; the parallel implementation exists, is tested, and is unreachable.
3. **Return `503` when the queue is full**, instead of `202` for a notification
   immediately marked `failed`.
4. **Replace `errors.Join(errors.New(name+": "), err)`** with `fmt.Errorf("%s: %w", ...)`
   so the API stops emitting embedded newlines.
5. **Guard `Enqueue` against a closed queue** with a `stopped` flag under the same mutex
   as `Shutdown`.
6. Make `List` ordering deterministic (stable sort plus a tiebreaker on ID); add doc
   comments to exported symbols; add a drain test to protect the shutdown ordering.

---

## Hiring Recommendation

**Hire.**

The engineering here is solid and the production instincts are better than the field
average: configuration is validated rather than silently defaulted, a sweeper bounds
memory growth, the layering is clean with a real service layer and thin handlers, and —
notably — the shutdown ordering actually drains the queue, which three other
submissions in this benchmark get wrong. It builds clean, passes `-race`, and delivers
200/200 under a genuine concurrent burst.

What holds it back is the delivery path. Structure is strong; substance on the write
path is not: the nine discarded `repo.Update` errors run through every status
transition in `Dispatcher.process` and `dispatchWithRetry`, including the terminal
ones. Beside them sit a parallel validation pipeline that was written, tested, and never
wired in; an `errors.Join` misuse that puts raw newlines into a client-facing field; a
queue-full path that answers `202` for work it never queued; and a channel closed under
a still-public sender. Each is a small thing, and every one of them is the kind a single
pass over the delivery path would surface.

This is a capable engineer who architects readily and verifies reluctantly. That is a
Hire — but the distance to the tier above is entirely rigor, and this submission is the
clearest evidence in the benchmark that rigor is the harder half.

---

## Final Score

**Overall Score: 211 / 280**

| # | Category | Score | Max |
|---|----------|------:|----:|
| 1 | Correctness | 12 | 15 |
| 2 | Architecture & Project Organization | 25 | 30 |
| 3 | Dependency Injection & Dependency Management | 21 | 25 |
| 4 | Go Idiomatic Practices | 22 | 30 |
| 5 | Concurrency | 24 | 30 |
| 6 | Error Handling | 14 | 20 |
| 7 | Code Quality & Readability | 15 | 20 |
| 8 | Testing | 19 | 25 |
| 9 | Design Patterns & Extensibility | 16 | 20 |
| 10 | Production Readiness | 15 | 20 |
| 11 | Documentation | 9 | 10 |
| 12 | Engineering Judgment | 26 | 35 |
| | **Subtotal** | **218** | **280** |
| | Critical penalties | −7 | |
| | **Total** | **211** | **280** |

### Justification

A well-architected submission scored on its own merits. Real
gains — `domain`/`application`/`infrastructure` layering, config validated at load, a TTL
sweeper, and a shutdown that genuinely drains (20/20 verified) — are offset by a
delivery-path defect: nine discarded `repo.Update` errors in the worker path (−5). Add a
parallel validation pipeline built, tested, and never wired (−2), an `errors.Join` misuse
emitting literal newlines into a client-facing field, a `202` returned for work the queue
rejected, an `Enqueue`-after-`Shutdown` panic, and essentially no doc comments on exported
symbols. Builds, `gofmt`/`vet` clean, passes `-race`, 200/200 end-to-end. **Hire.**
