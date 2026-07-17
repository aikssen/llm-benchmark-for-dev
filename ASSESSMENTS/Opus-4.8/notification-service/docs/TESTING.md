# Testing strategy

The goal is confidence that the concurrency and reliability machinery behaves
correctly, not a coverage number for its own sake. Tests are fast, deterministic
and run under the race detector.

## Layers

- **Unit tests per package**, using small hand-written fakes rather than a mocking
  framework. Each package is tested against its own interface, so tests document
  the contract and stay decoupled from collaborators' internals.
- **Integration test** (`internal/app/app_test.go`) boots the full object graph
  over `httptest` — the same wiring `main` uses — and drives the real path:
  HTTP submit → queue → worker → validation → delivery → status → HTTP get/list.

## Determinism

Concurrency and timing are made deterministic instead of being papered over with
sleeps:

- **Injectable clocks.** The rate limiter and circuit breaker take a `now func()
  time.Time`, so time-based transitions (window rollover, cooldown expiry) are
  tested by advancing a fake clock — no wall-clock waiting, no flakiness.
- **Scripted provider.** Delivery tests use a provider that returns a preset
  sequence of errors and can block for a fixed duration, so retry counts,
  permanent-vs-transient handling and per-attempt timeouts are exact.
- **Deterministic integration config.** The end-to-end test sets provider failure
  rate and latency to zero so final-state assertions are stable; a separate test
  opts a recipient out to assert the `failed` path.
- **Synchronisation over sleeps.** Worker-pool tests use channels to coordinate
  (e.g. confirm a worker has picked up a job before releasing it) rather than
  timing guesses.

## What is specifically verified

- **Fail-fast validation:** a deliberately slow sibling validator is proven to be
  *cancelled* (it never runs to completion) when another validator fails, and the
  pipeline returns quickly — the core requirement that failing validation stops
  the rest.
- **Delivery reliability:** success on first try; transient-retry-then-success;
  permanent error stops immediately; retry exhaustion; per-attempt timeout is
  enforced; an open circuit breaker fails fast and makes no provider calls;
  pre-cancelled context is honoured.
- **Circuit breaker:** opens at threshold, blocks during cooldown, allows a single
  half-open probe, closes on probe success, re-opens on probe failure, and is a
  no-op when disabled.
- **Worker pool:** processes all jobs; applies backpressure and honours the submit
  context deadline when the queue is full; drains buffered jobs on graceful
  shutdown; rejects submits after close.
- **Store:** CRUD, not-found sentinels, filtering, newest-first sorting,
  pagination bounds, value semantics (no pointer aliasing), and concurrent access
  under `-race`.
- **HTTP layer:** status-code mapping (202/200/400/404/405), strict JSON decoding
  (malformed and unknown-field rejection), and query-param parsing.

## Running

```bash
make test    # fast feedback
make race    # race detector (CI gate)
make cover   # coverage summary
```

All concurrency-touching tests pass under `-race`.
