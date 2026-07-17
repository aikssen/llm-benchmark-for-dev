# Assessment Evaluation — Comparative Summary

Backend Engineering Assessment (Go notification-orchestration service).
Each submission scored against `benchmark.md` (12 categories, **280 pts**) following `evaluator.md`. Every project was built and its tests run for real, including `go test -race`.

Individual reports live alongside this file: `RESULTS/<participant>.md`.

**Harness.** Each model was driven by an agent/CLI: **Claude Code** (Fable5, Sonnet5, Opus-4.8), **Codex** (GPT-5.6-Sol, GPT-5.6-Terra, GPT-5.5), **Antigravity** (Gemini-3.5-Flash), and **OpenCode** (everyone else). The harness is recorded per row below; scores reflect the submitted code, not the tool.

---

## Leaderboard

| # | Participant | Score /280 | Recommendation | Harness | Builds | Tests pass | `-race` clean |
|---|-------------|-----------:|----------------|---------|:------:|:----------:|:-------------:|
| 1 | **Fable5** | **256** | Strong Hire | Claude Code | ✅ | ✅ | ✅ |
| 2 | **Sonnet5** | **254** | Strong Hire | Claude Code | ✅ | ✅ | ✅ |
| 3 | **Opus-4.8** | **252** | Strong Hire | Claude Code | ✅ | ✅ | ✅ |
| 4 | **GPT-5.6-Sol** | **247** | Strong Hire | Codex | ✅ | ✅ | ✅ |
| 5 | **Kimi-K3** | **242** | Hire | OpenCode | ✅ | ✅ | ✅ |
| 6 | **GLM-5.2** | **225** | Hire | OpenCode | ✅ | ✅ | ✅ |
| 7 | **Gemini-3.5-Flash** | **214** | Hire | Antigravity | ✅ | ✅ | ✅ |
| 8 | **MiniMax-M3** | **211** | Hire | OpenCode | ✅ | ✅ | ✅ |
| 9 | **GPT-5.5** | **209** | Lean Hire | Codex | ✅ | ✅ | ✅ |
| 10 | **Grok-4.5** | **206** | Lean Hire | OpenCode | ✅ | ✅ | ✅ |
| 11 | **GPT-5.6-Terra** | **203** | Lean Hire | Codex | ✅ | ✅ | ✅ |
| 12 | **DeepSeek-V4-Pro** | **164** | Lean Hire | OpenCode | ✅ | ✅ | ❌ race |
| 13 | **Qwen3.7-Max** | **161** | Lean Hire | OpenCode | ✅ | ✅ | ❌ race* |
| 14 | **MiMo-V2.5-Pro** | **157** | Lean No Hire | OpenCode | ✅ | ✅ | ❌ race |
| 15 | **Kimi-K2.7-Code** | **153** | Lean No Hire | OpenCode | ✅ | ✅ | ⚠️ deadlock** |

\* Qwen: shipped suite passes `-race` but never exercises the concurrent path; a targeted probe reproduces the race.
\** Kimi-K2.7: passes `-race`, but the concurrent-burst path deterministically **deadlocks** (workers block on sub-tasks submitted to the same bounded pool); not caught by the suite.

> **Scope.** Models were added to the benchmark as they were released — `Grok-4.5`, `Kimi-K3`, `GPT-5.6-Sol`/`Terra` and `Gemini-3.5-Flash` joined later rounds. Every submission was evaluated the same way against the same rubric. `Kimi-K3` and `Kimi-K2.7-Code` are distinct models.
>
> † An earlier run, `Gemini3-Pro`, aborted mid-generation and never produced a runnable service; it was superseded by `Gemini-3.5-Flash` and is no longer included in the results.

---

## Per-category breakdown

Max: Corr 15 · Arch 30 · DI 25 · Idiom 30 · Conc 30 · Err 20 · Qual 20 · Test 25 · Design 20 · Prod 20 · Docs 10 · Judg 35

| Participant | Corr | Arch | DI | Idiom | Conc | Err | Qual | Test | Design | Prod | Docs | Judg | Subtot | Penalty | **Total** |
|---|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|
| Fable5 | 14 | 28 | 23 | 28 | 28 | 18 | 18 | 22 | 18 | 17 | 10 | 32 | 256 | 0 | **256** |
| Sonnet5 | 14 | 28 | 24 | 27 | 26 | 18 | 18 | 23 | 19 | 15 | 10 | 32 | 254 | 0 | **254** |
| Opus-4.8 | 14 | 27 | 22 | 28 | 27 | 18 | 18 | 23 | 18 | 16 | 10 | 31 | 252 | 0 | **252** |
| GPT-5.6-Sol | 14 | 28 | 23 | 27 | 27 | 18 | 18 | 20 | 18 | 18 | 10 | 31 | 252 | −5 | **247** |
| Kimi-K3 | 13 | 25 | 22 | 26 | 27 | 18 | 18 | 22 | 18 | 16 | 10 | 29 | 244 | −2 | **242** |
| GLM-5.2 | 12 | 27 | 22 | 27 | 23 | 15 | 17 | 21 | 18 | 14 | 10 | 26 | 232 | −7 | **225** |
| Gemini-3.5-Flash | 11 | 27 | 21 | 22 | 25 | 13 | 15 | 21 | 18 | 12 | 9 | 25 | 219 | −5 | **214** |
| MiniMax-M3 | 12 | 25 | 21 | 22 | 24 | 14 | 15 | 19 | 16 | 15 | 9 | 26 | 218 | −7 | **211** |
| GPT-5.5 | 11 | 26 | 22 | 25 | 20 | 14 | 16 | 17 | 16 | 13 | 8 | 26 | 214 | −5 | **209** |
| Grok-4.5 | 12 | 25 | 21 | 23 | 21 | 13 | 15 | 17 | 17 | 14 | 9 | 24 | 211 | −5 | **206** |
| GPT-5.6-Terra | 11 | 24 | 21 | 22 | 24 | 15 | 16 | 15 | 16 | 14 | 8 | 24 | 210 | −7 | **203** |
| DeepSeek-V4-Pro | 11 | 22 | 19 | 18 | 15 | 11 | 13 | 13 | 15 | 12 | 8 | 22 | 179 | −15 | **164** |
| Qwen3.7-Max | 10 | 22 | 18 | 21 | 12 | 13 | 13 | 13 | 15 | 9 | 8 | 19 | 173 | −12 | **161** |
| MiMo-V2.5-Pro | 9 | 22 | 18 | 20 | 11 | 13 | 15 | 12 | 15 | 12 | 8 | 20 | 175 | −18 | **157** |
| Kimi-K2.7-Code | 7 | 20 | 18 | 17 | 7 | 14 | 13 | 15 | 15 | 12 | 8 | 17 | 163 | −10 | **153** |

### Critical penalties applied
- **GPT-5.6-Sol** −5: ignored terminal `SaveDelivery` errors in worker path
- **Kimi-K3** −2: dead code — two exported methods never called (`Registry.Channels`, `Dispatcher.QueueCapacity`)
- **GLM-5.2** −7: ignored errors — terminal `SetStatus` on the queue-full path (−5), dead code — `IsValidation`, `WithIDGenerator`, unwired `RetryOptions.Jitter` (−2)
- **Gemini-3.5-Flash** −5: ignored `repo.Update` errors in worker path
- **MiniMax-M3** −7: ignored errors — all 9 `repo.Update` transitions in the dispatcher (−5), dead code — `RunParallel` implemented, tested and never called (−2)
- **GPT-5.5** −5: ignored errors — four terminal failure writes still discard their result (the terminal `delivered` write was fixed)
- **Grok-4.5** −5: ignored `store.Update` errors — all 12 transitions in `worker.process`
- **GPT-5.6-Terra** −7: ignored errors — terminal `delivered` write still discards its result (−5), dead code — unreachable retry branch, `sleep(context.Background())` can never return false (−2)
- **DeepSeek-V4-Pro** −15: data race (−10), swallowed store errors (−5)
- **Qwen3.7-Max** −12: data race (−10), dead code (−2)
- **MiMo-V2.5-Pro** −18: race (−10), ignored error (−5), unnecessary concurrency (−3)
- **Kimi-K2.7-Code** −10: reproducible worker-pool deadlock

---

## Tiers

**Top tier — Strong Hire (247–256).** Fable5, Sonnet5, Opus-4.8, and GPT-5.6-Sol. All four: clean inward-pointing layered architecture, correct **race-clean** concurrency (bounded worker pool + backpressure + **graceful queue drain**), idiomatic modern Go, high-quality deterministic tests, and excellent docs. GPT-5.6-Sol reaches this tier with atomic whole-batch admission, retryable-error classification, capped backoff with deterministic jitter, idempotency keys (409 on conflict), validated config, Docker, health/ready, and an OpenAPI contract — held a hair below the top three only by discarded terminal store-write errors (−5) and moderate test coverage. The others' weaknesses are all minor/operational (mostly: no Dockerfile, unbounded rate-limiter maps).

**Near-top — Hire (242).** Kimi-K3, the strongest submission outside the top tier. It ships the two properties that defined the Strong-Hire tier — **atomic all-or-nothing batch admission** and a **tested graceful drain** — plus a resilience stack sized to the brief (decorator-composed per-attempt timeout, retry/backoff/jitter, circuit breaker, temporary-vs-permanent classification), 44 property-pinning tests with injected clocks, zero third-party dependencies, and the best documentation submitted. It is the **only submission in the entire field that does not swallow store-write errors**, and the only one whose rate limiter evicts idle buckets. Held below Strong Hire by one real architectural gap — **no application layer**: the submit use case lives inside `httpapi.handleCreate`, reachable and testable only through HTTP — plus −2 for two dead exported methods, no Dockerfile, and a 9.1 MB binary shipped in the tree.

**Upper-middle — Hire (225).** GLM-5.2 is high-craft work: a **provider middleware stack** (`CircuitBreaker ∘ Retry ∘ Timeout ∘ Attempts`) that is the cleanest answer to the "unreliable providers" brief submitted; **`errgroup` used exactly where the benchmark says to reward it**, and correctly — it filters `context.Canceled` so fail-fast never leaks a bogus rejection reason; and **per-recipient results with structured error codes and a `partial` aggregate status**, the most expressive delivery reporting in the field (verified live: one invalid + one valid recipient → `partial`, with `{"code":"invalid_email","field":"email"}` on the failure). Real `503` backpressure, panic recovery, 44 tests, and the field's most thorough README. Held below Kimi-K3 by a shutdown that **abandons 16–18 of 20 queued notifications** (`Stop()` cancels before it closes, and `Queue.Dequeue` selects on `ctx.Done()`) — with a test, `TestQueueDequeueRespectsContext`, that pins the very mechanism causing the loss — plus a discarded terminal write on the backpressure path (−5), two dead exported symbols and an unwired `Jitter` field (−2), an `http.Server` missing three of its four timeouts, and no Dockerfile.

**Solid middle — Hire (211–214).** Gemini-3.5-Flash and MiniMax-M3 are correct, race-clean, and cleanly layered. Gemini adds genuinely good ideas — **provider failover** (primary + fallback per channel) and a concurrent validation engine whose parallelism is actually warranted — with high test coverage, but is pulled down by a cluster of avoidable rigor and hygiene problems: systematically ignored `repo.Update` errors (−5), HTTP status classified by string-matching error text, a missing health endpoint and external config, a List without filters, and unformatted test files. MiniMax brings a validated config, a **TTL sweeper** bounding memory, and a **shutdown that genuinely drains (20/20 verified)** — the ordering three other submissions get backwards — but discards all nine `repo.Update` errors in its dispatcher (−5), builds and tests a **parallel validation pipeline it never calls** (−2), answers `202` for work a full queue never accepted, and misuses `errors.Join` as string concatenation, emitting literal newlines into a client-facing field.

**Lower-middle — Lean Hire (161–209).** GPT-5.5, Grok-4.5, GPT-5.6-Terra, DeepSeek-V4-Pro, Qwen3.7-Max. GPT-5.5, Grok-4.5 and GPT-5.6-Terra are correct and race-clean but light on the production/rigor axes. GPT-5.5 owns the field's strongest copy boundary — the store holds notifications **by value** with a clone on read and mutates under the lock through an `Update(id, func(*N))` callback, making the shared-pointer race structurally impossible rather than merely avoided — and pairs it with **atomic batch admission** and a clean seven-package layout with consumer-defined interfaces. It sits in this band because four terminal failure writes discard their errors (−5) and because its shutdown loses accepted work twice over: workers are parented to the signal context and die on the first signal (**19/20 abandoned**), while `main` defers `Stop()` until after `server.Shutdown()`, so the API answers `202` for ten more seconds with no worker alive (verified: status stuck `pending`). `store` and `provider` have no tests, List has no pagination, and there is no Dockerfile. GPT-5.6-Terra drains 20/20 with a deadline **and** guards the producer with a `stopped` flag so a post-shutdown submit can never panic — a correctness property only Kimi-K3 also achieves — and ships validated env config plus the field's best idempotency (`202`/`200`/`409`, verified). It lands low anyway because it does not model the brief's opening premise: its only provider, `LoggingProvider`, **always returns `nil`**, so the retry, per-attempt-timeout and permanent-error paths never execute at runtime; preferences and rate limiting ship as `AllowAll*` no-ops; the terminal `delivered` write discards its error (−5); an unreachable retry branch adds −2; and its **six tests** are the fewest in the field, with `store`, `provider`, `domain` and `config` untested — including its own idempotency and cursor-pagination logic. Its README is the honest counterpoint: it names every one of these gaps rather than papering over them. Grok-4.5 is the best-equipped of the three operationally — sentinel errors mapped to HTTP status via `errors.Is` (not string-matching), real `503` backpressure, 9 env knobs, health endpoint, full timeouts — but its rigor gaps all land on the async delivery path the assessment centers on: `Pool.Stop()` never closes the jobs channel and workers exit on context cancellation, so a probe found **18 of 20 queued jobs silently abandoned** on shutdown (left `pending` forever); all 12 `store.Update` calls in `worker.process` discard their error (−5); a mid-fan-out `Enqueue` failure returns `503` with no IDs while earlier recipients are already in flight; and the suite exercises neither concurrency, backpressure, nor shutdown. Two non-test source files are not `gofmt`-clean. DeepSeek-V4-Pro and Qwen3.7-Max each ship a **real data race** in the core read path (shared `*Notification`/slice handed back and mutated concurrently) plus other correctness gaps — Qwen validates *after* returning 202 and has broken Docker.

**Bottom — Lean No Hire (153–157).** MiMo-V2.5-Pro (data race + unbounded goroutine fan-out + broken Dockerfile) and Kimi-K2.7-Code (deterministic worker-pool **deadlock** on the burst path — the one thing the assessment centers on).

---

## Cross-cutting observations

- **Concurrency was the great separator.** The assessment is fundamentally about safe concurrent multi-recipient delivery under bursts. The Strong-Hire tier is race-clean **and** exercises the concurrent path in tests. Three submissions still ship a data race or deadlock that their own test suites fail to catch — a reminder that passing `go test -race` only means something if the tests actually drive concurrency.
- **The shared-pointer trap.** The most common defect (DeepSeek, Qwen, MiMo) is the in-memory store returning the same `*Notification` a worker mutates, with no copy boundary — reads race with writes. Every submission that copies on read is race-clean; every one that doesn't, isn't. It is the single most mechanical divide in the benchmark.
- **Shutdown that abandons accepted work is the most under-tested defect in the field.** Grok-4.5, GLM-5.2 and GPT-5.5 all cancel the worker context instead of draining the queue, so notifications already answered with `202` are silently dropped on every deploy (probes: Grok 18/20, GLM 16–18/20, GPT-5.5 19/20). **None of the three has a drain test** — and two are cautionary in their own way: GLM ships a test (`TestQueueDequeueRespectsContext`) that pins the *exact mechanism* causing its own data loss, while GPT-5.5 **defers** `processor.Stop()` until after `server.Shutdown()`, so for ten seconds its workers are dead while the API keeps answering `202` (verified: the accepted notification stays `pending` forever). The submissions that drain are the ones that thought about it: Kimi-K3 proves it with `TestStopDrainsQueue`, MiniMax gets the `Shutdown()`-before-`cancel()` ordering right, and GPT-5.6-Terra closes the queue and drains with a deadline (all verified 20/20).
- **Swallowed store-write errors were near-universal among the async designs — with one exception.** GPT-5.6-Sol, GPT-5.6-Terra, Gemini-3.5-Flash, GPT-5.5, MiniMax, Grok-4.5 and GLM-5.2 all discard at least one delivery-status write in the worker path (Grok-4.5 discards all 12 transitions; MiniMax all 9) — harmless against an in-memory map, but a silent data-loss bug the moment the repository interface is backed by a real database. **Kimi-K3 alone logs every failed `Upsert`**, and is the only submission to which this −5 does not apply.
- **Atomic batch admission separated the top from the middle.** A multi-recipient request that fails to enqueue partway leaves the client unable to know what was accepted. **GPT-5.6-Sol**, **Kimi-K3** and **GPT-5.5** reserve capacity for the whole batch under a lock before enqueueing any of it, and **GLM-5.2 sidesteps the problem by design** — one notification with N recipients is one queue entry, so admission is atomic for free. The rest, including Grok-4.5 and Gemini-3.5-Flash, can accept a request partially and still report failure; MiniMax is worse still, answering `202` for work it never queued.
- **Closing a channel other goroutines send on.** MiniMax `close()`s the job queue in `Shutdown` while a public `Enqueue` remains live, and reproduces `panic: send on closed channel`. **Kimi-K3**, **GPT-5.6-Terra** and **GPT-5.5** avoid it — the first two with the same correct pattern (a `stopped` flag under the mutex that serializes the producer, so a post-shutdown submit returns an error instead of panicking), GPT-5.5 by never closing the channel at all.
- **Docker/verification hygiene.** Several submissions (MiMo, Qwen) pin the wrong Go version in the Dockerfile so the image can't build; four (Gemini-3.5-Flash, Kimi-K3, GLM-5.2, MiniMax) ship a compiled binary in the tree; Gemini-3.5-Flash ships unformatted test files and Grok-4.5 two unformatted **source** files. Notably, **six of the fifteen submissions have no Dockerfile at all** — and MiniMax ships no Dockerfile despite the surrounding tooling.
- **Unbounded maps — two submissions noticed.** Nearly every submission has an in-memory rate-limiter/preferences map with no eviction, a latent leak on long uptime. **Kimi-K3's limiter sweeps idle buckets** (`maybeSweep`) and **MiniMax runs a TTL sweeper** over terminal notifications. Nobody else bounds memory growth at all.
- **Docs were consistently strong — and that made doc *accuracy* the discriminator.** Almost every real submission scored 8–10/10 with honest assumptions/trade-offs sections, so documentation quality did **not** separate the field; engineering correctness did. Where docs mattered was when they lied: Grok-4.5's `Pool.Stop` and GLM-5.2's `Queue.Close` both document a queue drain neither implements.
