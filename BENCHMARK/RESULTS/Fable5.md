# Evaluation — Fable5

## Executive Summary

This is an exemplary submission: a notification orchestration service that
reads like the first production version of a system a team would maintain for
years. It implements every functional requirement, compiles cleanly, and its
full test suite (including `go test -race ./...`) passes. The architecture is
cleanly layered around a dependency-free `domain` package, business logic is
kept out of the transport layer, concurrency is bounded and purposeful, and the
engineering trade-offs are documented and justified in a genuinely excellent
README.

The design centers on a sound idea — the `(notification × recipient)` *delivery*
is the unit of work — and follows it through consistently: async submission with
explicit backpressure (`202` / `429 + Retry-After`), a bounded queue drained by a
fixed worker pool, fail-fast concurrent validation via `errgroup`, and
retry/backoff that distinguishes permanent from transient provider errors.
Standard-library-first (`net/http` method routing, `log/slog`, channels), with a
single justified third-party dependency (`golang.org/x/sync/errgroup`).

Weaknesses are minor: the in-memory rate-limiter map grows unboundedly, `go.mod`
is not tidied (`x/sync` marked `// indirect` despite a direct import), the config
and `id` packages are untested, and the module pins a bleeding-edge Go
(`go 1.26.3`, using `errors.AsType`/`strings.SplitSeq`). None are critical; no
penalty conditions apply.

## Requirement Coverage

All functional requirements are implemented and verifiable:

- **Submit a notification** — `POST /v1/notifications` (`api.createNotification`),
  returns `202 Accepted` with the created resource.
- **Retrieve status** — `GET /v1/notifications/{id}` (`api.getNotification`),
  including per-recipient deliveries and errors.
- **List notifications** — `GET /v1/notifications` with `limit`/`offset`/`status`/
  `channel` filters (`api.listNotifications`, `store.Memory.ListNotifications`).
- **At least recipient, channel, message** — modeled in `domain.Notification` /
  `domain.Delivery` / `domain.Message`.
- **Fail-fast validation** — `validation.Pipeline.Run` uses `errgroup`; the first
  failure cancels the shared context and every validator honors cancellation.
  Directly satisfies "the remaining validations should not continue consuming
  unnecessary resources," and is proven by `TestPipelineFailFast`.
- **Independent validations** — `Recipient`, `Content`, `Preferences`,
  `RateLimit`, each an isolated `Validator`.
- **Deliver only if all validations pass** — enforced in `worker.Pool.process`.
- **Multiple recipients / efficient + predictable resource use** — fan-out to one
  `Delivery` per recipient; bounded queue + fixed pool cap memory and provider
  concurrency; bursts shed load with `429`.
- **Unreliable providers** — `provider.Simulated` injects latency and
  transient/permanent failures; worker retries transient errors with capped,
  jittered exponential backoff and per-attempt timeouts.
- **Extensible providers/channels** — `provider.Provider` interface +
  `provider.Registry`, registered in `main.go`.

Also present: `GET /healthz`, graceful shutdown, structured logging, Docker,
Makefile, config-by-environment. No requested functionality is missing.

## Benchmark Review

### 1. Correctness (15 pts) — Score: 14

**Strengths.** All endpoints behave correctly and are covered by tests; `go
build`, `go vet`, `go test`, and `go test -race` all pass. The notification
lifecycle (pending → processing → delivered/failed/partially_delivered) is
derived purely from delivery states (`domain.AggregateStatus`, table-tested).
Recipient deduplication (`validateCreate`), atomic all-or-nothing enqueue with
rollback (`createNotification` + `EnqueueBatch`), and permanent-vs-transient
retry logic are all correct and tested end to end (`e2e_test.go`).

**Weaknesses.** Two subtle wrinkles: (a) because validators run concurrently, the
`RateLimit` validator records a hit even when another validator fails first, so a
rejected delivery still consumes a rate-limit token; (b) the rate-limiter map
never evicts keys for quiet recipients (unbounded growth). Both are minor.

**Evidence.** `internal/domain/notification.go:97`, `internal/api/api.go:70-130`,
`internal/worker/pool.go:121-209`, `internal/validation/validators.go:188-208`.

### 2. Architecture & Project Organization (30 pts) — Score: 28

**Strengths.** Textbook layering with correct dependency direction: `domain` has
zero internal dependencies; `api` (transport), `worker` (processing),
`validation`, `provider`, `store`, `config`, and `id` all depend inward. Boundary
interfaces (`store.Store`, `provider.Provider`, `validation.Validator`,
`api.Enqueuer`) decouple layers, and the `Enqueuer` interface is defined by its
consumer (`api`) — idiomatic. Extensibility seams are real and documented (new
channel = implement `Provider` + register; new validation = implement `Validator`;
real DB = implement `Store`; external queue = jobs carry only IDs).

**Weaknesses.** Almost none at this scale. One could argue `worker` mixes queue
management, the delivery state machine, and retry policy in one package, but the
cohesion is reasonable and the file stays readable.

**Evidence.** package layout under `internal/`; `store/store.go:37`,
`api/api.go:28`, `worker/pool.go:30` (jobs carry IDs only).

### 3. Dependency Injection & Dependency Management (25 pts) — Score: 23

**Strengths.** Pure constructor injection throughout (`api.New`, `worker.New`,
`provider.NewRegistry`, `validation.NewPipeline`, `store.NewMemory`), with a
single clear composition root in `main.run()`. Dependencies are explicit and
narrow; the `Enqueuer` interface exposes only what `api` needs. No service
locator, no hidden globals. `now func() time.Time` and `rand func() float64` are
injected for deterministic tests.

**Weaknesses.** `go.mod` marks `golang.org/x/sync` as `// indirect` even though
`validation` imports `errgroup` directly — `go mod tidy` was not run, a small
dependency-hygiene lapse. `slog.SetDefault(logger)` sets process-global default
logging, though every component still receives its logger explicitly, so it is
benign.

**Evidence.** `cmd/server/main.go:42-81`, `go.mod:5`,
`internal/validation/validators.go:169` (`now`), `internal/provider/simulated.go:47`.

### 4. Go Idiomatic Practices (30 pts) — Score: 28

**Strengths.** Modern and idiomatic: `net/http` with Go 1.22+ method-routed
`ServeMux`, `log/slog`, `errors.Is`/`errors.As`, `errgroup`, `context`
propagation (including `context.WithoutCancel` so terminal status writes survive
shutdown cancellation — a sophisticated, correct touch), `math/rand/v2`,
`slices`, `sync.WaitGroup.Go`, generics in test helpers (`decodeBody[T]`), small
interfaces, composition over inheritance. Naming and doc comments are exemplary.

**Weaknesses.** The module requires an extremely new toolchain (`go 1.26.3`,
`errors.AsType`, `strings.SplitSeq`); for a "production foundation" a more widely
available Go would be safer, and the README's "Go 1.25+" claim understates the
actual `errors.AsType` (1.26) requirement.

**Evidence.** `api/api.go:56-64`, `worker/pool.go:214` (`WithoutCancel`),
`api/dto.go:102` (`errors.AsType`), `config/config.go:129` (`strings.SplitSeq`).

### 5. Concurrency (30 pts) — Score: 28

**Strengths.** The concurrency model fits the problem precisely. A bounded
channel queue + fixed worker pool gives a hard ceiling on memory and provider
concurrency; `EnqueueBatch` serializes producers with `enqueueMu` so the
capacity check is atomic for a whole batch (proven by
`TestEnqueueBatchBackpressure`). Validation uses `errgroup` for cooperative
fail-fast cancellation — exactly where concurrency earns its keep. Per-attempt
`SendTimeout`, jittered capped backoff, and graceful shutdown (drain HTTP, then
cancel workers, then `Wait`) are all correct. `-race` is clean and wired into
`make test`.

**Weaknesses.** Running four mostly-cheap validators concurrently per delivery is
borderline over-engineering, but it is justified by the explicit "stop consuming
resources on first failure" requirement and the simulated remote preference
lookup, so it is defensible rather than gratuitous.

**Evidence.** `worker/pool.go:83-117,168-209`, `validation/validation.go:58-69`,
`cmd/server/main.go:85-123`.

### 6. Error Handling (20 pts) — Score: 18

**Strengths.** Sentinel domain errors (`store.ErrNotFound`, `worker.ErrQueueFull`,
`provider.ErrNoProvider`) surfaced via `errors.Is`; a permanent-error marker type
(`provider.permanentError` + `Permanent`/`IsPermanent`) with `Unwrap` so the
chain is preserved (`TestPermanentErrors`); a `validation.Error` wrapper naming
the failing validator. Retry/timeout/rollback paths are all handled, and enqueue
failure rolls back the persisted notification.

**Weaknesses.** `writeJSON` discards the encoder error (`_ = ...Encode`) and
`id.New` ignores `rand.Read`'s error — both are conventional and documented, but
strictly they are swallowed errors. No genuine panic-driven flow.

**Evidence.** `provider/provider.go:69-89`, `validation/validation.go:36-42`,
`api/api.go:114-127`, `api/dto.go:116`.

### 7. Code Quality & Readability (20 pts) — Score: 18

**Strengths.** Small, single-purpose functions; precise naming; consistent style;
`gofmt`-clean; high-signal doc comments that explain *why* (e.g. the
`WithoutCancel` rationale, the `d <= 0` shift-overflow guard in `backoff`). Very
low cognitive load.

**Weaknesses.** Very few. The `worker.send` retry loop packs several
responsibilities (attempt accounting, classification, backoff, shutdown) into one
function; readable, but the densest spot in the codebase.

**Evidence.** `worker/pool.go:237-244` (documented overflow guard),
`store/memory.go:124-129`.

### 8. Testing (25 pts) — Score: 22

**Strengths.** Genuinely high-quality tests, not coverage theater. Table-driven
suites for validators, aggregate status, and store listing; meaningful edge
cases (leading-zero E.164, display-name emails, window slide, offset beyond
matches); real fakes/stubs (`fakeEnqueuer`, `scriptedProvider`, `simWithRoll`);
concurrency-specific assertions (`TestPipelineFailFast` proves the slow validator
is actually cancelled; `TestEnqueueBatchBackpressure` proves atomic enqueue;
copy-on-read isolation test proves no state leak); and a real end-to-end test
wiring API + workers + pipeline + simulated providers plus a 50-request burst
asserting every request gets `202` or a clean `429`. Everything runs under `-race`.

**Weaknesses.** `config` (which contains non-trivial parsing/validation logic) and
`id` have no tests. The burst test only asserts `accepted > 0`, so with capacity
64 vs 50 requests the `429` branch may not exercise.

**Evidence.** `validation/validation_test.go:49-83`, `worker/pool_test.go:204-238`,
`store/memory_test.go:45-50`, `api/e2e_test.go`. No test files in `internal/config`,
`internal/id`.

### 9. Design Patterns & Extensibility (20 pts) — Score: 18

**Strengths.** Patterns appear only where they earn their place: Registry
(`provider.Registry`), Strategy (`validation.Validator` set), Repository
(`store.Store`), Adapter (`provider.Simulated` behind `Provider`), and a
permanent-error marker. Each maps to a documented extension point and the
assessment's explicit extensibility requirement. No speculative abstraction.

**Weaknesses.** `store.Store` and `PreferenceChecker` currently have a single
implementation each, but both are justified seams (documented Postgres path;
simulated remote preference service), so no "interface without justification"
penalty applies.

**Evidence.** `provider/provider.go:21-67`, `validation/validators.go:102-157`,
`README` "Extensibility seams".

### 10. Production Readiness (20 pts) — Score: 17

**Strengths.** Env-based config with validation and sane defaults
(`config.Load`), structured `slog` logging with a request-logging + panic-recovery
middleware chain, graceful shutdown with a bounded budget, `/healthz`,
`ReadHeaderTimeout` plus per-operation timeouts, `MaxBytesReader` body caps,
explicit backpressure, and a proper multi-stage distroless-nonroot `Dockerfile`
with a `Makefile` (`run`/`build`/`test`/`check`).

**Weaknesses.** The rate-limiter map is an unbounded in-process structure (memory
leak over long uptime); only liveness (`/healthz`), no readiness probe; no
metrics/tracing or request-correlation IDs (all acknowledged as future work). For
a service explicitly framed as a multi-year foundation, the rate-limiter growth is
the one real operational gap.

**Evidence.** `config/config.go:55-149`, `api/middleware.go`,
`cmd/server/main.go:92-123`, `Dockerfile`, `validation/validators.go:166-208`.

### 11. Documentation (10 pts) — Score: 10

**Strengths.** The README is outstanding and covers exactly what the rubric asks:
an ASCII architecture diagram, per-decision rationale (async + backpressure,
delivery-as-unit-of-work, fail-fast validation, retry classification,
extensibility seams, technology choices), explicit assumptions, trade-offs, a
testing-strategy section, a complete configuration table, and a prioritized
future-improvements list. Package- and function-level doc comments reinforce it
throughout.

**Weaknesses.** Trivial: README says "Go 1.25+" while the code needs 1.26.

**Evidence.** `README.md` in full; package doc comments in every `internal/*`.

### 12. Engineering Judgment (35 pts) — Score: 32

**Strengths.** Consistently mature decisions: standard-library-first with a single
justified dependency; async + bounded backpressure chosen deliberately over
holding connections open against slow providers; delivery modeled as the unit of
work so recipients fail independently; validation deliberately placed in the
worker with the trade-off (invalid recipient only visible via status) called out;
in-memory store/queue kept simple with the durable path (outbox, DLQ, circuit
breaker) documented as future work. It solves the problem with the least
machinery that fully meets the requirements and avoids overengineering.

**Weaknesses.** A few small maturity nits keep it from a perfect score: pinning to
a bleeding-edge Go for a "production foundation," an untidied `go.mod`, and the
unbounded rate-limiter map that a staff engineer would have bounded from day one.

**Evidence.** `README.md` "Design decisions" / "Trade-offs" / "Future
improvements"; overall restraint in package count and abstraction.

## Critical Findings

No critical issues were found. Specifically checked and **not** present:

- **Race conditions** — none; `store.Memory` uses `RWMutex`, `RateLimit` uses a
  `Mutex`, batch enqueue is serialized by `enqueueMu`, and `go test -race ./...`
  passes.
- **Hardcoded secrets** — none.
- **Circular dependencies** — none; `domain` is dependency-free and all layers
  point inward.
- **Hidden dependencies / Service Locator** — none; pure constructor injection
  with a single composition root.
- **Global mutable state** — none material; only `slog.SetDefault`, with loggers
  still injected everywhere.
- **Ignored errors** — only conventional discards (`writeJSON` encode, `id`
  `rand.Read`), both documented; not penalty-worthy.
- **Panic in normal flow** — none; the panic-recovery middleware is purely
  defensive.
- **Business logic in HTTP handlers** — none; handlers do only structural
  validation/DTO mapping; business validation lives in `validation`/`worker`.
- **Dead code** — none; the `bin/server` binary present on disk is `.gitignore`d.

No penalty conditions from the Critical Issues table apply.

## Overall Strengths

- Clean, correctly-directed layering around a dependency-free domain.
- Concurrency that fits the problem: bounded pool, atomic batch enqueue,
  `errgroup` fail-fast validation, graceful shutdown, race-clean.
- Sophisticated error modeling (permanent/transient, sentinel domain errors,
  wrapped validation errors).
- High-quality, behavior-focused tests including concurrency and e2e coverage.
- Exceptional documentation and consistently sound engineering judgment.

## Highest Priority Improvements

1. **Bound the rate-limiter memory.** Evict empty/idle keys (or use a windowed
   counter / sharded LRU) so `RateLimit.hits` cannot grow unboundedly over long
   uptime.
2. **Run `go mod tidy`** so `golang.org/x/sync` is recorded as a direct
   dependency, and relax the `go` directive / align the README to the real
   minimum toolchain (`errors.AsType` needs 1.26).
3. **Test the `config` package** — its parsing and cross-field validation
   (latency ordering, failure-rate sum, page-size bounds) is logic worth locking
   down.
4. **Reconsider rate-limiting as a concurrent validator** so it does not consume a
   token when a co-running validator fails first; or move it to a post-validation
   gate.
5. **Add a readiness probe** distinct from liveness, plus the documented metrics
   for production operability.

## Hiring Recommendation

**Strong Hire.**

The submission demonstrates the judgment, restraint, and production mindset
expected of a senior/staff Go engineer. It fully satisfies the assessment, is
correct and race-free under test, and every significant decision is deliberate,
justified, and documented. The abstractions are the right ones and no more; the
concurrency is purposeful; the error handling and testing are mature. The
remaining weaknesses (unbounded rate-limiter map, untidied `go.mod`, missing
config tests, bleeding-edge Go pin) are minor and easily addressed, and none rise
to a critical concern. This is among the strongest possible responses to this
assessment.

## Final Score

**Overall Score: 256 / 280**

| # | Category | Score | Max |
|---|----------|------:|----:|
| 1 | Correctness | 14 | 15 |
| 2 | Architecture & Project Organization | 28 | 30 |
| 3 | Dependency Injection & Dependency Management | 23 | 25 |
| 4 | Go Idiomatic Practices | 28 | 30 |
| 5 | Concurrency | 28 | 30 |
| 6 | Error Handling | 18 | 20 |
| 7 | Code Quality & Readability | 18 | 20 |
| 8 | Testing | 22 | 25 |
| 9 | Design Patterns & Extensibility | 18 | 20 |
| 10 | Production Readiness | 17 | 20 |
| 11 | Documentation | 10 | 10 |
| 12 | Engineering Judgment | 32 | 35 |
| | **Subtotal** | **256** | **280** |
| | Critical penalties | 0 | |
| | **Total** | **256** | **280** |

**Justification.** The score reflects a submission that is production-quality
across every dimension, verified by a clean `go build` / `go vet` / `go test
-race`. Points are withheld only for genuinely minor issues — an unbounded
rate-limiter map, an untidied `go.mod`, missing `config`/`id` tests, and a
bleeding-edge toolchain pin — none of which are critical. No penalties apply.
The result is a high-confidence Strong Hire.
