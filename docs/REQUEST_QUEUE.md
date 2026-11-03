# Protection queue and public admission

Community owns the queue. Inspection, protected chat and the Community embeddings
API share one FIFO and running capacity per core. Enterprise authorizes the caller,
model and tenant quotas before forwarding to that core; it has no second queue.
Login, model discovery, application workflows and reports are outside the queue.

## Configuration

| Variable | Default | Contract |
| --- | --- | --- |
| `PUBLIC_DAILY_IP_LIMIT_ENABLED` | `true` | Enables each product's existing anonymous allowance of five actions/IP/UTC day outside development |
| `PUBLIC_IP_RATE_LIMIT_ENABLED` | `true` | Community public minute and per-IP concurrency checks; independent of the daily switch |
| `QUEUE_MAX_RUNNING` | `2` | Shared running jobs per core; integer 1–64 |
| `QUEUE_MAX_WAITING` | `20` | Shared waiting jobs per core; integer 0–256 |
| `QUEUE_WAIT_TIMEOUT_SECONDS` | `30` | Maximum queue wait; integer 1–300; also set on Enterprise to size its upstream/HTTP timeout |

Booleans accept only `true` or `false` (unset uses the default). Invalid values
fail startup. Changes require controlled service restarts. Development is always
exempt from daily admission; minute/per-IP controls still follow their own switch.
Enterprise authenticated chat remains governed by tenant/department quotas, not
the anonymous five-action allowance. Enterprise has no minute/IP switch.

Disabling IP checks preserves public global admission, core capacity, authentication,
policy, role/model permissions and token quotas. Daily admission counts existing
anonymous actions, including Community model discovery. Queue notifications and
heartbeats are part of the original admitted request and consume no extra allowance.
Token reservation occurs once before queue admission and conservatively estimates
original request bytes, message overhead and requested output; failed/cancelled work
does not refund this reservation or an already consumed anonymous action.

## Coordination and lifecycle

Production requires Redis. A single development gateway without `REDIS_URL` uses
an in-memory implementation; set Redis to test multiple development replicas.
Replicas of one core must share Redis database, `ORGANIZATION_ID`, HMAC secret,
queue settings, application metadata and policies. Independent cores must have
distinct organization IDs or isolated Redis databases. Operator browser sessions
remain process-local, so existing sticky-session requirements still apply.

Atomic Redis Lua transitions admit tickets, enforce running/waiting capacity and
promote the oldest surviving waiter. Only random ticket IDs, ordering, lease
timestamps, sequence and configuration metadata enter Redis. Prompt text, provider
credentials, results and restoration mappings remain in the owning HTTP request's
memory. A slot is held through inspection, provider communication and restoration.

Pub/Sub is a wakeup hint, not the state authority. Each active gateway reconciles
its local tickets against Redis every two seconds while it has waiting/running work;
the loop stops when idle. Failed removals retain only bounded opaque cleanup
tickets in the gateway; the same reconciliation loop retries their removal in a
single batch after Redis recovers. Waiting ownership expires after six seconds without
renewal. Running ownership is bounded by a 70-second processing budget plus a
five-second cleanup margin; it is not extended by progress messages. Graceful
shutdown cancels queued/running contexts and removes their tickets. After an abrupt
crash or failed Redis cleanup, capacity recovers when the bounded leases expire;
other waiters may time out in the meantime. Jobs are never moved or retried.

Drain traffic on **all replicas**, stop them, and let the old configuration lease
expire (at most 80 seconds after the last transition/readiness check) before
restarting with changed queue settings. A conflicting active configuration fails
closed. Do not flush shared Redis while gateways are serving requests. Redis state
loss requires verifying that all prior work has terminated, or quiescing all core
replicas for the previous running-lease horizon before reopening traffic. This
transient queue is not a failover job system.

Redis failures reject new protection work; reconciliation cancels active request
contexts if ownership cannot be verified. Client cancellation/disconnection removes
waiting tickets and propagates through Enterprise, Community, Rust and the provider.
Existing per-call engine/provider deadlines remain; the complete processing phase
is capped at 70 seconds. Gateway and Enterprise HTTP budgets also include queue wait.

## Opt-in progress protocol

Send `X-Prompt-Shield-Progress: v1` on the existing protection POST with a JSON
response request (`stream` omitted/false). A different header value, repeated header
or `stream=true` together with this header returns HTTP 400. There is no status URL,
polling, reconnect or automatic replay. Requests without the header retain normal
JSON and provider-native SSE response formats and still share queue capacity.

```text
event: queued
data: {"position":2}

event: processing
data: {"state":"processing"}

event: result
data: {"choices":[{"message":{"content":"Restored response"}}]}

```

`queued` appears only while waiting; its positive position may change as preceding
tickets leave. `processing` means a slot was acquired, not that a provider has been
called. `result` contains the original JSON response. Inspection can return a
successful result with `is_blocked=true`; protected chat instead reports the existing
safe blocked-request error. `error` replaces a result on failure:

```text
event: error
data: {"http_status":503,"response":{"error":{"code":"queue_timeout","type":"prompt_shield_error","message":"queue_timeout"}}}

```

Before SSE starts, errors use their ordinary HTTP status/JSON envelope. Once it
starts, HTTP stays 200 and clients must inspect the error event's `http_status` and
safe code. Full queue: `429 queue_full`; expired wait: `503 queue_timeout`;
coordination failure: `503 queue_unavailable` or `queue_configuration_mismatch`.
The opt-in processing deadline reports `503 request_timeout`. Ten-second SSE
comments keep waiting/processing connections active without changing UI state.

The shared Community frontend package reads split UTF-8/CRLF frames, rejects
incomplete/oversized streams, handles ordinary JSON errors and observes aborts.
Both products show sending, actual queue position, processing and completion/error.
Cancelled or superseded requests cannot restore old conversation/key/model state.

## Deployment and acceptance

Deploy Community and Enterprise backends first, then rebuild/deploy both frontends
with the same versioned shared package. Configure ingress to allow the progress
header, flush SSE without buffering, propagate disconnects and permit at least
queue wait + 100 seconds end to end. Mirror the configured daily/IP admission at
ingress; the application Redis check alone is not zero backend interaction.

Queueing bounds concurrent work and absorbs bursts; it does not promise lower total
provider cost for the same workload. Host capacity, CPU/RAM, Redis recovery, ingress
buffering/timeouts and cost require separate hosted measurements. See the new C29/E41
manual rows and the [local verification record](evidence/request-queue-20261030.json).
