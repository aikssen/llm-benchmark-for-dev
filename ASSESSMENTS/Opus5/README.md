# Notification Orchestration Service

A Go service that accepts notification requests and orchestrates their delivery
across multiple channels (email, SMS, push) through unreliable external
providers.

Submission is synchronous and cheap; delivery is asynchronous, validated,
retried and bounded. Adding a channel means writing one provider and
registering it.

- **Design rationale, assumptions and trade-offs:** [`docs/DESIGN.md`](docs/DESIGN.md)
- **Go 1.24+**, standard library only — no third-party dependencies.

---

## Quick start

```bash
make run          # start on :8080 with the simulated providers
make test         # full suite, race detector on
make demo         # exercise a running instance through its API
```

Or with Docker:

```bash
make docker-run   # build and run on :8080
```

Then:

```bash
curl -X POST localhost:8080/v1/notifications \
  -H 'Content-Type: application/json' \
  -d '{
        "channel": "email",
        "recipients": [{"id": "user-1", "address": "alice@example.com"}],
        "message": {"subject": "Welcome", "body": "Thanks for signing up."}
      }'
```

`make help` lists every target.

---

## How it works

```
        HTTP request
             │
             ▼
   ┌───────────────────┐   fast, synchronous checks: channel supported,
   │  Request validation│   body present, address syntax, duplicates.
   └─────────┬─────────┘   Stops at the first failure → 400.
             │
             ▼
   ┌───────────────────┐   Reserve N slots, all or nothing.
   │  Queue admission   │   No room → 429 with Retry-After.
   └─────────┬─────────┘
             │  persist, then publish one job per recipient
             ▼                                        202 Accepted ──▶ client
   ┌────────────────────────────────────────┐
   │  Bounded queue  ──▶  Worker pool (N)   │
   └────────────────────┬───────────────────┘
                        │  one job = one recipient
                        ▼
        ┌───────────────────────────────┐
        │  Validation pipeline           │  recipient · preferences
        │  (concurrent, fail-fast)       │  rate limit · content
        └───────────────┬───────────────┘   first failure cancels the rest
                        │ all passed
                        ▼
        ┌───────────────────────────────┐
        │  Dispatcher                    │  timeout → retry w/ backoff+jitter
        │   └─ circuit breaker ─▶ Provider│  → circuit breaker per provider
        └───────────────┬───────────────┘
                        ▼
                  Delivery outcome persisted
                  (delivered · rejected · failed)
```

A request with N recipients becomes **N independent deliveries**. Each is
validated and delivered on its own, so one opted-out or unreachable recipient
never blocks the rest of the batch. The notification's status is derived from
its deliveries: `delivered`, `partially_delivered` or `failed`.

---

## API

Base path `/v1`. All responses are JSON.

| Method | Path                     | Purpose                                    |
| ------ | ------------------------ | ------------------------------------------ |
| `POST` | `/v1/notifications`      | Submit a notification (1..N recipients)    |
| `GET`  | `/v1/notifications/{id}` | Retrieve status, including every delivery  |
| `GET`  | `/v1/notifications`      | List notifications, newest first           |
| `GET`  | `/v1/channels`           | Channels this deployment supports          |
| `GET`  | `/healthz`               | Liveness — is the process up?              |
| `GET`  | `/readyz`                | Readiness — queue depth, breaker states    |

### Submit a notification

`POST /v1/notifications` → **`202 Accepted`** with a `Location` header.

202, not 201: the request has been accepted for delivery, which has not
happened yet. Clients poll `GET /v1/notifications/{id}` for the outcome.

```json
{
  "channel": "email",
  "recipients": [
    {"id": "user-1", "address": "alice@example.com"},
    {"id": "user-2", "address": "bob@example.com"}
  ],
  "message": {"subject": "Welcome", "body": "Thanks for signing up."},
  "metadata": {"campaign": "onboarding"}
}
```

| Field                  | Required | Notes                                                              |
| ---------------------- | -------- | ------------------------------------------------------------------ |
| `channel`              | yes      | Any channel returned by `GET /v1/channels`                          |
| `recipients[].address` | yes      | Format is channel-specific and enforced by that channel's provider  |
| `recipients[].id`      | no       | Platform user id; keys preferences and rate limits. Falls back to the address |
| `message.body`         | yes      | Length limits are per channel                                       |
| `message.subject`      | channel  | Required for `email` and `push`                                     |
| `metadata`             | no       | Opaque key/value pairs, echoed back and passed to the provider      |

Address formats: `email` — a bare RFC 5322 address; `sms` — E.164
(`+14155550123`); `push` — a device token of 16–256 characters.

**`Idempotency-Key` header** *(recommended)* — resubmitting the same key
returns the original notification instead of sending twice. A client that
times out mid-request can safely retry.

Unknown JSON fields are rejected, so a typo like `"recipeints"` fails loudly
instead of silently sending the wrong thing.

### Retrieve a notification

`GET /v1/notifications/{id}` → `200 OK`

```json
{
  "id": "ntf_9c3499a3b7575c10411c4c4ab32b174a",
  "channel": "email",
  "status": "partially_delivered",
  "message": {"subject": "Welcome", "body": "Thanks for signing up."},
  "summary": {"total": 2, "pending": 0, "sending": 0, "delivered": 1, "rejected": 1, "failed": 0},
  "deliveries": [
    {
      "id": "dlv_642ac7cf324e1831eba9d53b5d57f360",
      "recipient": {"id": "user-1", "address": "alice@example.com"},
      "status": "delivered",
      "attempts": 2,
      "provider": "simulated-email",
      "provider_message_id": "simulated-email-17",
      "created_at": "2026-07-24T18:53:17Z",
      "updated_at": "2026-07-24T18:53:17Z",
      "completed_at": "2026-07-24T18:53:17Z"
    },
    {
      "id": "dlv_f12052fc19b8aed73f55046c1fd49bc5",
      "recipient": {"id": "user-2", "address": "bob@example.com"},
      "status": "rejected",
      "attempts": 0,
      "failure_code": "recipient_opted_out",
      "failure_reason": "recipient has opted out of the \"email\" channel"
    }
  ],
  "created_at": "2026-07-24T18:53:17Z",
  "updated_at": "2026-07-24T18:53:17Z"
}
```

**Notification status** (derived from the deliveries):

| Status                 | Meaning                                       |
| ---------------------- | --------------------------------------------- |
| `pending`              | Queued, nothing picked up yet                 |
| `processing`           | At least one delivery is in flight            |
| `delivered`            | Every delivery succeeded                      |
| `partially_delivered`  | Some succeeded, some did not                  |
| `failed`               | None succeeded                                |

**Delivery status:**

| Status      | Terminal | Meaning                                                        |
| ----------- | -------- | -------------------------------------------------------------- |
| `pending`   | no       | Queued                                                          |
| `sending`   | no       | Being validated or sent                                         |
| `delivered` | yes      | The provider accepted it; see `provider_message_id`             |
| `rejected`  | yes      | A validation rule refused it — never retried, `attempts` is `0` |
| `failed`    | yes      | The provider could not be reached; the retry budget ran out     |

`attempts` counts calls that actually reached the provider. It is `0` for a
rejected delivery, and also for one shed by an open circuit breaker.

### List notifications

`GET /v1/notifications?channel=email&status=delivered&limit=20&offset=0`

All parameters are optional. `limit` defaults to 20 and is capped at 100.

```json
{
  "items": [ /* notification objects */ ],
  "pagination": {"total": 137, "limit": 20, "offset": 0}
}
```

### Errors

One shape for every failure, so clients write a single parser and branch on
`code` rather than on prose:

```json
{
  "error": {
    "code": "invalid_request",
    "message": "must be a phone number in E.164 format, e.g. +14155550123",
    "field": "recipients[1].address",
    "request_id": "req_e56fcbdae6960d8bac9f3d2fa2f45b4d"
  }
}
```

| Status | `code`               | When                                                    |
| ------ | -------------------- | ------------------------------------------------------- |
| 400    | `invalid_request`    | A field is invalid; `field` says which                  |
| 400    | `malformed_json`     | Body is not a single valid JSON object                  |
| 404    | `not_found`          | No such notification, or no such route                  |
| 405    | `method_not_allowed` | Route exists, method does not; see the `Allow` header   |
| 413    | `payload_too_large`  | Body exceeds `SERVER_MAX_REQUEST_BYTES`                 |
| 429    | `service_overloaded` | Queue is full; retry after the `Retry-After` header     |
| 500    | `internal_error`     | Unexpected; details are logged, never returned          |

`request_id` is echoed in the `X-Request-ID` header and appears in every log
line for that request. An incoming `X-Request-ID` is preserved, so a trace can
span services.

**Validation stops at the first failure.** Only one field is ever reported —
a deliberate choice, discussed in [`docs/DESIGN.md`](docs/DESIGN.md).

---

## Configuration

Everything is configured through environment variables, and everything has a
default: the service runs with no configuration at all.
[`.env.example`](.env.example) documents each one.

The settings that matter most in production:

| Variable                          | Default | What it controls                                   |
| --------------------------------- | ------- | -------------------------------------------------- |
| `WORKER_COUNT`                    | `16`    | Cap on concurrent deliveries and provider calls     |
| `QUEUE_CAPACITY`                  | `4096`  | How much burst is absorbed before shedding (429)    |
| `MAX_RECIPIENTS_PER_REQUEST`      | `1000`  | Fan-out cap for one request                         |
| `PROVIDER_CALL_TIMEOUT`           | `3s`    | Bounds a single provider call                       |
| `RETRY_MAX_ATTEMPTS`              | `3`     | Total tries per delivery, including the first       |
| `BREAKER_FAILURE_THRESHOLD`       | `10`    | Consecutive transient failures that trip a breaker  |
| `RATE_LIMIT_RATE` / `_INTERVAL`   | `60`/`1m` | Per recipient, per channel                        |

Invalid configuration fails at start-up and reports **every** problem at once,
rather than one restart per typo.

### Simulating provider trouble

The simulated providers are tunable, which makes the reliability behaviour easy
to observe:

```bash
# A provider that is completely down: watch retries exhaust, then the
# breaker trip and start shedding load.
PROVIDER_TRANSIENT_FAILURE_RATE=1.0 LOG_FORMAT=text make run

# A slow provider: watch the per-call timeout keep workers from being pinned.
PROVIDER_MIN_LATENCY=5s PROVIDER_MAX_LATENCY=5s make run
```

`GET /readyz` reports live queue depth and per-provider breaker state.

---

## Testing

```bash
make test        # everything, with -race
make cover       # total statement coverage (88.6%)
make cover-html  # line-by-line report in a browser
```

The suite is layered:

- **Unit tests** for each component in isolation — the validation pipeline's
  fail-fast cancellation, token-bucket refill, circuit-breaker transitions,
  retry classification and backoff, queue admission, store isolation.
- **Concurrency tests** under `-race`: bounded pool concurrency, all-or-nothing
  admission under contention, concurrent updates to the same notification.
- **Integration tests** (`internal/app`) that drive the fully wired service
  through its HTTP surface: delivery end to end, mixed rejection outcomes,
  retry-then-fail against a dead provider, breaker load shedding, rate-limit
  rejection, burst shedding, graceful drain, and adding a brand-new channel.

Time and randomness are injected everywhere they appear, so no test sleeps
waiting for a clock.

---

## Project layout

```
cmd/notifier/          entry point: config, signals, graceful shutdown
internal/
  domain/              entities, statuses, invariants — no dependencies
  service/             application layer: submit, get, list
  validation/          concurrent fail-fast pipeline + the checks
  dispatcher/          provider call wrapped in timeout, retry, breaker
  processing/          the per-delivery lifecycle a worker runs
  provider/            Provider port + registry  ← extension point
    simulated/         stand-in email / SMS / push integrations
  queue/               bounded queue with all-or-nothing admission
  worker/              fixed-size pool
  store/               persistence port
    memory/            in-memory adapter
  resilience/          retry policy, circuit breaker
  ratelimit/           token bucket
  preferences/         recipient preference port + in-memory adapter
  httpapi/             HTTP transport: routing, DTOs, middleware
  config/              environment configuration
  app/                 composition root
  platform/            logging, id generation
docs/DESIGN.md         architecture, assumptions, trade-offs, next steps
```

Dependencies point inward: `domain` imports nothing, and no inner layer knows
about HTTP.

---

## Adding a channel

Implement `provider.Provider` and register it. Nothing else changes — not the
API, not validation, not the worker pool.

```go
type SlackProvider struct{ /* ... */ }

func (p *SlackProvider) Name() string            { return "slack" }
func (p *SlackProvider) Channel() domain.Channel { return domain.Channel("slack") }

// The channel brings its own address rules.
func (p *SlackProvider) ValidateAddress(address string) error {
    if !strings.HasPrefix(address, "C") {
        return errors.New("must be a Slack channel id")
    }
    return nil
}

func (p *SlackProvider) Send(ctx context.Context, req provider.SendRequest) (provider.SendResult, error) {
    // Return provider.Transient(...) for retryable failures,
    // provider.Permanent(...) for ones that will never succeed.
}
```

Then add it in `internal/app.defaultRegistry`. The new channel is immediately
accepted by `POST /v1/notifications`, advertised by `GET /v1/channels`, gets
its own circuit breaker, and inherits the timeout and retry policy.
`TestANewChannelNeedsOnlyAProvider` in `internal/app/integration_test.go`
verifies exactly this.

---

## Operational notes

- **Graceful shutdown.** `SIGINT`/`SIGTERM` stops the listener first, then
  drains the accepted backlog within `SERVER_SHUTDOWN_TIMEOUT`. Work already
  accepted from clients is finished, not dropped. Requests arriving during the
  drain get a `429`, and `/readyz` reports `503` so a load balancer removes the
  instance.
- **Logging.** Structured JSON via `log/slog`, with `notification_id`,
  `delivery_id`, `channel` and `request_id` on the lines that matter — the
  fields you need to answer "what happened to this message?". Use
  `LOG_FORMAT=text` locally.
- **State is in memory.** Restarting loses notification history and any queued
  work. This is the single most important limitation to fix before production;
  see [`docs/DESIGN.md`](docs/DESIGN.md).
