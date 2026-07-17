# Evaluation — GPT-5.6-Terra

> Harness: **Codex** (OpenAI's agent).

## Executive Summary

A submission whose concurrency and idempotency work is genuinely strong, undercut by a
single decision at the center of the brief: the one provider it ships never fails, so the
premise the whole assessment is built around goes unmodelled.

The strengths are well-executed. **The shutdown drains**: `Stop(ctx)` closes the queue
and waits with a deadline; a probe confirmed **20 of 20 queued notifications processed**.
Better, it is **correctly guarded** — a `stopped` flag under the same mutex as the
producer means `Submit` after `Stop` returns `ErrStopped` instead of panicking on a
closed channel. Only this submission and Kimi-K3 get that right; MiniMax reproduces `send
on closed channel` in the same situation. Configuration comes from the environment **with
validation**. And **idempotency keys work exactly as they should**, verified over HTTP:
`202` on first submit, `200` on identical replay, `409` on a conflicting body — matching
GPT-5.6-Sol as the best in the field.

The gap is on the brief's central premise. **The only provider shipped is a
`LoggingProvider` that logs and returns `nil`. Always.** The assessment opens by stating
that providers "may respond slowly, temporarily fail, become unavailable, return
intermittent errors" — with this provider, the retry loop never runs twice,
`AttemptTimeout` never fires, and `IsPermanent` is never true. A burst of 200 deliveries
returned 240/240 delivered not because retries work, but because nothing ever fails.
Alongside it, two of the four validation examples the brief names — user preferences and
rate limits — are shipped as `AllowAllPreferences` and `AllowAllRateLimit` no-ops.

The README is honest about both, which is a real virtue in a field where several
submissions overclaim. But honesty about a gap does not close it.

Verified locally with `go1.26.3` (module declares `go 1.22`):
- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l .` — **clean**
- `go test ./...` — all packages pass (**6 tests** — the smallest suite in the field)
- `go test -race ./...` — all packages pass (no data races)
- Coverage: service 80.6%, validation 55.6%, httpapi 51.6%; **`store`, `provider`,
  `domain` and `config` have no test files at all**
- End-to-end: 40 concurrent requests × 5 recipients (200 deliveries) → 240/240
  `delivered`; idempotency `202`/`200`/`409` verified; edge cases return 400/404

Penalties: `−5` ignored errors (the terminal `delivered` write discards its result),
`−2` dead code (an unreachable retry branch).

---

## Requirement Coverage

| Requirement | Status | Evidence |
|-------------|--------|----------|
| Submit a notification | Implemented | `POST /v1/notifications` → `Service.Submit` (202; 200 on idempotent replay) |
| Retrieve status | Implemented | `GET /v1/notifications/{id}` — aggregate + per-delivery status |
| List notifications | Implemented | `GET /v1/notifications` — **cursor pagination** with `next_cursor`, limit 1–100 |
| Recipient, channel, message | Implemented | `domain.Notification{Channel, Message, Deliveries[]}` |
| Independent validations | **Partial** | Four `Validator`s registered, but `AllowAllPreferences` and `AllowAllRateLimit` are no-ops that always return nil |
| Stop early on first failure | Implemented | `Pipeline.Validate` cancels the shared context on first failure (`sync.Once` guards `firstError`) |
| Deliver only if all validations pass | Implemented | `processDelivery` returns before the provider loop when validation fails |
| Multiple recipients | Implemented | One `Delivery` per recipient; bounded recipient workers; `partially_failed` aggregate via `RecalculateStatus` |
| Efficient/predictable under bursts | Implemented | Bounded queue + bounded notification/recipient workers + `503` + `Retry-After`; **drain verified 20/20** |
| **Unreliable providers** | **Not implemented** | The only provider (`LoggingProvider`) always succeeds — no latency, no failures, no unavailability. |
| Best-effort reliable delivery | Partial | Retry/backoff/per-attempt-timeout/`Permanent` machinery exists and is unit-tested with fakes, but **never executes at runtime** because no provider ever fails |
| Add providers with minimal change | Implemented | Implement `provider.Provider` + register in `NewRegistry` |
| Runnable locally | Implemented | `go run ./cmd/server`; **no Dockerfile, no Makefile, no `.env.example`** |
| README / build & run / tests / config | Implemented | 85-line README (shortest in field) + validated env config |

Two requirement gaps stand out: the **unreliable-provider premise is unmodelled**, and
**two of the four named validation steps are permissive stubs**. Both are disclosed in
the README.

---

## Benchmark Review

### 1. Correctness (15) — Score: 11
**Strengths:** All endpoints verified end-to-end; 240/240 delivered under a concurrent
burst. **Idempotency is exemplary and verified over HTTP** — `202` first, `200` on
identical replay, `409` on a conflicting body, with `sameRequest` comparing channel,
message and recipients in order. Cursor pagination with `next_cursor`. Per-delivery
status with a derived aggregate (`RecalculateStatus`) including `partially_failed`.
Strict input handling: `MaxBytesReader`, `DisallowUnknownFields`, and a second decode
to reject trailing content. **Drain verified 20/20.**
**Weaknesses:** **The unreliable-provider requirement is unmodelled** —
`LoggingProvider.Send` logs and returns `nil` unconditionally, so retry, per-attempt
timeout and permanent-error classification never execute in the running system.
**Preferences and rate limiting are `AllowAll*` no-ops.**
**Evidence:** `provider/provider.go:75–77`; `cmd/server/main.go:39–40`;
`validation/validation.go:135–143`; e2e burst and idempotency runs; drain probe.

### 2. Architecture & Project Organization (30) — Score: 24
**Strengths:** Clean separation across `domain`, `store`, `validation`, `provider`,
`service`, `transport/httpapi`, `config`, with no cycles and thin handlers that decode,
delegate and encode. `store.Repository` is a small, well-documented contract whose doc
comment explains how a SQL implementation with an outbox would preserve it.
**Weaknesses:** **`validation` imports `provider`** to use `provider.Payload` as its
validation input — validation has no business depending on the provider package, and it
couples the two for a type that belongs in `domain`. `sameRequest` is **duplicated
verbatim** in `store/repository.go:141` and `service/service.go:262`. In
`store/repository.go`, `FindByIdempotency` is defined at line 36, **before the
`MemoryRepository` struct it hangs off** (line 49) — legal but disorienting.
**Evidence:** `validation/validation.go:12,17`; `store/repository.go:36,141`;
`service/service.go:262`.

### 3. Dependency Injection & Dependency Management (25) — Score: 21
**Strengths:** Constructor injection throughout with a single composition root in
`main`; `store.Repository`, `provider.Provider` and `validation.Validator` are
interfaces; config is loaded once and injected as a struct; zero third-party
dependencies.
**Weaknesses:** `Service` takes concrete `*provider.Registry` and
`*validation.Pipeline`, so neither seam is substitutable. The logger is a concrete
`*log.Logger` threaded through every constructor rather than an interface or a
structured handler.
**Evidence:** `service/service.go:57`; `cmd/server/main.go:30–51`.

### 4. Go Idiomatic Practices (30) — Score: 22
**Strengths:** `gofmt` and `vet` clean. Go 1.22 method-pattern `ServeMux`, a typed
`provider.Error` with `Unwrap` and `errors.As`-based `IsPermanent`, sentinel errors
mapped with `errors.Is`, `crypto/rand` IDs, `net/mail` for address parsing,
`MaxBytesReader`, `DisallowUnknownFields`, and the trailing-content decode check.
**Weaknesses:** **No `log/slog`** — the service uses the legacy `log.Logger`, which the
benchmark explicitly contrasts against the modern standard library; the README lists
"logs estructurados" as a *future* step, so this is a known, disclosed gap.
`FindByIdempotency` defined before its receiver type.
`sameRequest` duplicated across two packages. The `sleep` helper takes a `ctx` that its
only caller hardcodes to `context.Background()`, making the parameter vestigial and the
branch that consumes it unreachable.
**Evidence:** `cmd/server/main.go:23`; `service/service.go:215,286`; `README.md`
("Próximos pasos", item 4).

### 5. Concurrency (30) — Score: 24
**Strengths:** **Shutdown drains cleanly.** `Stop(ctx)` closes the queue and
waits for workers with a deadline — probe: **20/20 processed, queue empty, `Stop`
returns nil**. And it is **correctly guarded**: the `stopped` flag lives under the same
`submitMu` that serializes the producer, so `Submit` after `Stop` returns `ErrStopped`
rather than panicking on a closed channel — a correctness property only this submission
and Kimi-K3 achieve, while MiniMax reproduces
`send on closed channel` in the same situation. Bounded on three axes (queue capacity, notification workers,
recipient workers). Per-attempt timeout. Validation pipeline with bounded parallelism
and fail-fast via `sync.Once` + context cancellation. Race-clean; the repository clones
on read and write with a deep copy of the `Deliveries` slice.
**Weaknesses:** **Validation and the retry backoff both use `context.Background()`**, so
neither is cancellable — in-flight retries can outlive `Stop(ctx)`'s deadline, and the
`if !sleep(...)` branch is dead as a result. The queue-capacity check
(`len(s.queue) == cap(s.queue)`) is safe only under the invariant that the API handler
is the sole producer; the comment states this well, but nothing in the type enforces it.
**No test covers the drain, the queue-full path, or any burst.**
**Evidence:** `service/service.go:98,119–137,186,215`; drain and submit-after-stop
probes.

### 6. Error Handling (20) — Score: 15
**Strengths:** A typed `provider.Error{Err, Permanent}` with `Unwrap`, driving retry
decisions through `errors.As`. Sentinels (`ErrQueueFull`, `ErrStopped`,
`ErrInvalidRequest`, `ErrNotFound`, `ErrIdempotencyConflict`) mapped with `errors.Is` to
400/404/409/503. **`failDelivery` checks and logs its `UpdateDelivery` error.**
Panic-recovery middleware.
`safeError` truncates provider errors at 500 bytes before persisting them.
**Weaknesses:** **Three `_, _ = s.repository.UpdateDelivery(...)` sites
(lines 175, 198, 205), including line 205 — the terminal `delivered` write —
discard their result.** The failure path checks and logs its error; the success path does
not. `writeJSON` discards its encode error behind a comment.
**Penalty −5.**
**Evidence:** `service/service.go:175,198,205,222–230`; `httpapi/handler.go:131–137`.

### 7. Code Quality & Readability (20) — Score: 16
**Strengths:** `gofmt` clean, consistently readable, descriptive naming
(`processNotification`/`processDelivery`/`failDelivery`). The comment explaining why the
queue-capacity check cannot race — "The API handler is the only queue producer and
submitMu serializes it" — is the kind of comment that states an invariant the code
cannot, exactly as it should.
**Weaknesses:** `sameRequest` duplicated verbatim across `store` and `service`; the
idempotency check performed twice (service `FindByIdempotency` + repository
`CreateOrGet`, which re-checks internally); an unreachable retry branch;
`FindByIdempotency` placed before its receiver struct.
**Evidence:** `service/service.go:88–105,262`; `store/repository.go:36,63–82,141`.

### 8. Testing (25) — Score: 15
**Strengths:** The six tests that exist are well-targeted and deterministic:
retry-on-transient-failure (with a fake provider), partial-failure aggregation,
idempotency, pipeline cancellation after first failure, recipient-format validation, and
an HTTP create-and-get. Service coverage is 80.6%. Green under `-race`.
**Weaknesses:** **Six tests is the smallest suite in the field** (Kimi-K3 and GLM-5.2
ship 44). **`store`, `provider`, `domain` and `config` have no test files at all** — so
the two headline features, **idempotency and cursor pagination, are untested at the
repository level**, as is `RecalculateStatus`, the function that derives every aggregate
status. There is **no test for the drain path**, none for the
queue-full path, and none for concurrency or bursts.
**Evidence:** `go test -cover ./...`; `internal/store/` and `internal/provider/`
contain no `_test.go`.

### 9. Design Patterns & Extensibility (20) — Score: 16
**Strengths:** Repository, Registry, Strategy (`Validator`), a typed permanent-error
marker, and two bounded worker pools — each justified, none speculative. Adding a
channel is implement + register. The `Repository` doc comment explicitly describes the
SQL/outbox migration path the contract is designed to permit.
**Weaknesses:** `AllowAllPreferences` and `AllowAllRateLimit` are placeholder
implementations of two of the four validation steps the brief names — the Strategy
pattern is in place but two of its strategies do nothing. `validation` depending on
`provider.Payload` couples layers that should not know each other.
**Evidence:** `validation/validation.go:135–143,12`; `store/repository.go:26–34`.

### 10. Production Readiness (20) — Score: 14
**Strengths:** **Environment configuration with validation** —
`FromEnv` type-checks every variable before use and rejects non-positive limits. Full server timeouts
(`ReadHeaderTimeout`/`Read`/`Write`/`Idle`), `/healthz`, panic-recovery middleware,
`503` + `Retry-After` backpressure, idempotency keys, and a **graceful drain with a
20s deadline**.
**Weaknesses:** **Still no structured logging** — plain `log.Logger`, with the README
deferring `slog` to "próximos pasos". **No Dockerfile, no Makefile, no `.env.example`.**
No metrics, tracing or readiness endpoint.
**Evidence:** `config/config.go:24–66`; `cmd/server/main.go:23,54–61,80–87`.

### 11. Documentation (10) — Score: 8
**Strengths:** Honest and well-reasoned. The design-decisions section argues each choice
(async + bounds, per-recipient state, early-cancellation validation, extensible
adapters, at-least-once semantics), and the **at-least-once discussion is the most
thoughtful in the field** — it explains that a timeout leaves provider acceptance
ambiguous, so real adapters must propagate an idempotency key downstream. Crucially, it
**discloses its own gaps** rather than papering over them: log-only providers,
permissive preference/rate-limit validators, and missing structured logs all appear
explicitly. Complete config table and API reference.
**Weaknesses:** **85 lines — the shortest README in the field** (GLM ships 430, Kimi
300+). No architecture diagram, no project layout, and the concurrency model gets a
single sentence. The README is in Spanish while all code comments are in English — a
consistency wrinkle, not a defect.
**Evidence:** `README.md`.

### 12. Engineering Judgment (35) — Score: 24
**Strengths:** The shutdown handling is not just present but *correct* in the subtle way most
of the field missed — closing a channel while a producer is live is the trap that
catches MiniMax, and this submission guards it properly. Idempotency with
`200`/`409` semantics shows a real API-design instinct. Validated config, panic
recovery, `503` backpressure, and a README that names its own weaknesses instead of
overselling all point to an engineer with production judgment.
**Weaknesses:** The judgment call at the center of this submission is hard to defend:
given a brief whose opening paragraph is about unreliable providers,
it **ships no provider that simulates unreliability**. The resilience code is
untestable by inspection and unexercised by running the service; a reviewer starting it
sees nothing but success. Two of four named validation steps are no-ops. The terminal
`delivered` write discards its error while the failure write checks it — an inconsistency
that costs `−5`. And the suite that would have caught any of this is six tests, with the store
package that holds the idempotency logic entirely uncovered. Honest documentation of
these choices mitigates but does not resolve them.
**Evidence:** `provider/provider.go:75–77`; `validation/validation.go:135–143`;
`service/service.go:205`; test inventory.

---

## Critical Findings

- **Ignored errors — penalty −5.** Three `_, _ = s.repository.UpdateDelivery(...)` calls
  in `processDelivery` (lines 175, 198, 205) discard their result, **including line 205,
  the terminal `delivered` write**. `failDelivery` checks and logs its error; the success
  path does not.
- **Dead code — penalty −2.** `service.go:215` calls
  `sleep(context.Background(), ...)`. `context.Background().Done()` returns a nil
  channel, which blocks forever, so the `select` always resolves via `timer.C` and
  `sleep` always returns `true`. The `if !sleep(...)` branch (lines 216–217) is
  **unreachable**, and `sleep`'s `ctx` parameter is vestigial at its only call site.
- **Unreliable-provider requirement not implemented (no numeric penalty; scored in
  Correctness, Design and Judgment).** `LoggingProvider.Send` returns `nil`
  unconditionally — no latency, no failures, no unavailability — so the retry loop,
  per-attempt timeout and `IsPermanent` classification never execute at runtime.
- **Preference and rate-limit validation not implemented (no numeric penalty; scored in
  Correctness and Design).** `AllowAllPreferences` and `AllowAllRateLimit` always return
  nil. Both are disclosed in the README.
- **Race conditions:** none — `-race` clean; `domain.CloneNotification` deep-copies the
  `Deliveries` slice on every read and write.
- **Shutdown abandons queued work:** **none — the queue drains on `Stop`.** Probe:
  20/20 processed, `Stop` returns nil.
- **Send on closed channel:** **none.** The `stopped` flag under `submitMu` correctly
  guards the producer — verified: `Submit` after `Stop` returns `ErrStopped`, no panic.
- **Business logic in handlers:** none — handlers decode, delegate, encode.
- **Panic in normal flow:** `newID` panics if `crypto/rand` fails — documented as
  exceptional, effectively unreachable, and off the normal path. Not penalized, matching
  the treatment of the equivalent construct elsewhere in this benchmark.
- **Hidden dependencies / global mutable state / service locator / circular
  dependencies / hardcoded secrets:** none found.
- **Hygiene (no numeric penalty):** `sameRequest` duplicated verbatim across `store` and
  `service`; idempotency checked twice; `FindByIdempotency` defined before its receiver
  struct; no `log/slog`; no Dockerfile/Makefile/`.env.example`.

Total penalties: **−7**.

---

## Overall Strengths

1. **A drain that works and is correctly guarded** — `Stop(ctx)` with a deadline (20/20
   verified), plus a `stopped` flag under the producer's mutex so shutdown can never
   panic a sender. Only this submission and Kimi-K3 get both halves right.
2. **The best idempotency in the field alongside GPT-5.6-Sol** — `202`/`200`/`409`
   verified over HTTP, with conflict detection over channel, message and ordered
   recipients.
3. **Validated environment configuration** — `FromEnv` type-checks every variable and
   rejects non-positive limits.
4. Cursor pagination, per-delivery status with a derived `partially_failed` aggregate,
   `503` + `Retry-After` backpressure, panic recovery, and strict input handling.
5. **A README that names its own gaps** instead of overselling — the honest counterpoint
   to several submissions in this field whose docs describe behavior the code lacks.
6. Race-clean with a proper deep-copy boundary; zero third-party dependencies.

---

## Highest Priority Improvements

1. **Ship a provider that simulates unreliability** (latency, transient failures,
   unavailability). Without it the brief's central requirement is unmodelled and every
   line of retry/timeout/breaker code is dead at runtime.
2. **Stop discarding the terminal `delivered` write** at `service.go:205`, and the two
   other `_, _ = UpdateDelivery` sites.
3. **Test the store package**: idempotency conflict detection and cursor pagination are
   the headline features and have zero coverage. Add a drain test for the shutdown path.
4. **Implement real preference and rate-limit validators**, or state plainly that the
   requirement is out of scope rather than shipping `AllowAll*` stubs.
5. **Move to `log/slog`** — the README already lists it as a gap.
6. Pass a cancellable context into validation and the retry backoff (removing the dead
   branch); de-duplicate `sameRequest`; add a Dockerfile and Makefile.

---

## Hiring Recommendation

**Lean Hire.**

The concurrency work here is the best part of the submission. The queue drains on
shutdown with a deadline, and — the part most of this field gets wrong — it is guarded so
a producer can never send on a closed channel. Two of sixteen submissions manage that.
Idempotency is implemented properly and verified: `202`, then `200` on replay, then `409`
on conflict. Config is validated at load. The README argues its decisions and, unusually,
admits what it did not build.

What holds it back is a trade it should not have made. Given an assessment whose opening
paragraph establishes that providers "may respond slowly, temporarily fail, become
unavailable, return intermittent errors," the only provider it ships is a
`LoggingProvider` that always succeeds. The retry loop, the per-attempt timeout, the
permanent-vs-transient classification — all of it is unreachable in the running service.
Two of the four named validation steps are permissive stubs. The terminal `delivered`
write throws its error away while the failure write checks it. And the suite that might
have surfaced any of this is six tests, with the store package holding the idempotency
logic entirely untested.

This is careful engineering aimed slightly away from the target. The craft is Hire-level;
the requirement coverage is not.

---

## Final Score

**Overall Score: 203 / 280**

| # | Category | Score | Max |
|---|----------|------:|----:|
| 1 | Correctness | 11 | 15 |
| 2 | Architecture & Project Organization | 24 | 30 |
| 3 | Dependency Injection & Dependency Management | 21 | 25 |
| 4 | Go Idiomatic Practices | 22 | 30 |
| 5 | Concurrency | 24 | 30 |
| 6 | Error Handling | 15 | 20 |
| 7 | Code Quality & Readability | 16 | 20 |
| 8 | Testing | 15 | 25 |
| 9 | Design Patterns & Extensibility | 16 | 20 |
| 10 | Production Readiness | 14 | 20 |
| 11 | Documentation | 8 | 10 |
| 12 | Engineering Judgment | 24 | 35 |
| | **Subtotal** | **210** | **280** |
| | Critical penalties | −7 | |
| | **Total** | **203** | **280** |

### Justification

A submission with real strengths and one central gap. It handles shutdown properly —
draining 20/20 with a deadline and guarding the producer against a closed channel, a
correctness property only Kimi-K3 also achieves — and adds validated env config plus the
field's best idempotency (`202`/`200`/`409` verified). Those gains are offset by a miss on
the brief's central premise: the only provider shipped is a `LoggingProvider` that always
succeeds, leaving the entire retry/timeout/permanent stack unexercised at runtime, while
preferences and rate limiting ship as `AllowAll*` no-ops. The terminal `delivered` write
discards its error (`−5`); an unreachable retry branch adds `−2`. Six tests, with `store`,
`provider`, `domain` and `config` untested. Builds, `gofmt`/`vet` clean, passes `-race`.
**Lean Hire.**
