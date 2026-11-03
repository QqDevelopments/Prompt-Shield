# Community API v0.1

The gateway serves JSON APIs and optionally the console assets. The hosted Vercel frontend calls the separately configured API origin directly; see [HOSTING.md](HOSTING.md). Identifiers/error codes are stable protocol values and are not localized. UI translations resolve those codes independently. This is a restricted text contract, not a universal drop-in replacement.

## Authentication and console

| Endpoint | Contract |
| --- | --- |
| GET /healthz | Process liveness; no authentication |
| GET /readyz | Database, Redis and authenticated engine readiness; rules-only/hybrid mode |
| GET /metrics | Prometheus; Bearer METRICS_TOKEN, at least 32 characters |
| POST /api/login | JSON password; exact configured Origin; session cookie and csrf_token |
| GET /api/session | Current session and CSRF token |
| POST /api/logout | Invalidate current session |
| GET /api/overview | Readiness, provider configuration and counts over at most the latest 100 organization events |
| GET/PUT /api/policy | Versioned current-organization policy |
| GET/POST /api/keys | List metadata; create with name, return key metadata and one-time secret |
| DELETE /api/keys/{id} | Revoke a current-organization key |
| GET /api/events?offset=0&decision=masked | Pages of 50; optional allowed/masked/blocked/failed decision filter |
| POST /api/inspect | JSON text; sanitized_text, is_blocked, reason_code, entity_types/count and processing_time_micros |

Console reads require a session. Mutations also require an explicitly allowed frontend Origin and X-CSRF-Token. Login accepts a configured 16-72 byte operator password; ten attempts per minute are allowed per resolved client IP. Forwarded headers are used only through explicitly trusted proxy CIDRs. PUBLIC_ORIGIN must exactly match the frontend address used in the browser, including scheme and port; ALLOWED_ORIGINS may add exact trusted origins.

## Account-free hosted access

POST /public/v1/models accepts {"provider":"openai"}, {"provider":"anthropic"} or {"provider":"gemini"} and requires X-Provider-API-Key, with no server-key fallback. It returns {"data":[{"id":"model-id","name":"Model name"}]} from the configured provider's GET /models endpoint, newest release/creation first. Model-list membership does not guarantee compatibility with the restricted text-chat contract. Discovery shares public rate/concurrency limits, follows no redirects, has a 15-second total deadline, and limits responses to 2 MiB, 2,000 unique models and ten pages. Credentials and results are not cached or persisted. Provider 401 clears the browser key; 403 reports missing model-list permission without discarding it. Gemini's structured API_KEY_INVALID errors are normalized to 401 provider_key_rejected. No prompt or completion request is sent during discovery.

When PUBLIC_ENABLED is enabled, POST /public/v1/inspect accepts text without login or provider credentials. POST /public/v1/chat/completions, POST /public/v1/messages and POST /public/v1/gemini/chat/completions require X-Provider-API-Key and expose the corresponding restricted text/streaming contracts below. Public users cannot manage policies, keys or events. Missing credentials never fall back to server keys. Public IP rate/concurrency admission is separate from authenticated integration limits; see [hosting configuration](HOSTING.md).

### Google AI Studio Gemini

`POST /v1/gemini/chat/completions` requires a Prompt Shield application Bearer key; the public equivalent is `/public/v1/gemini/chat/completions`. Supply the Google AI Studio key separately in `X-Provider-API-Key`. OpenAI-compatible clients can use the gateway's `/v1/gemini` base URL for text chat. The model ID does not select the provider; only the route does.

Gemini uses the existing restricted OpenAI text request, JSON response and SSE restoration pipeline. `max_tokens` is sent upstream; a lone `max_completion_tokens` is normalized to it. Both nonzero fields together are rejected. Existing output limits and conservative quota reservation apply. The upstream default is `https://generativelanguage.googleapis.com/v1beta/openai`; authentication is Bearer with only the provider key. Google `API_KEY_INVALID` is returned as `401 provider_key_rejected`, quota rejection as `429 rate_limited`, and other upstream failures use the safe existing error envelope. Native Gemini, Vertex AI, tools, multimodal inputs and Gemini embeddings are outside this contract.

POST /v1/inspect provides inspection using an application access key for private integrations. Hosted /v1 provider requests also require X-Provider-API-Key; only self-hosted authenticated integrations may use environment credentials.

Policy:

```json
{"version":1,"mask_pii":true,"block_secrets":true,"contextual_pii":false,"blocked_keywords":[]}
```

PUT must carry the version returned by GET. Successful saves increment it; stale saves return 409 policy_conflict. Recognized critical secrets always block and block_secrets=false is rejected. Keyword entries must be nonempty and at most 128 UTF-8 bytes, with at most 64 entries. Contextual policy requires an available model. A missing required model later fails inspection closed.

mask_pii enables deterministic rule masking; contextual_pii independently enables contextual NER masking. Turning off one does not turn off the other.

## Provider matrix

| Endpoint | Supported request | Response/restoration | Unsupported |
| --- | --- | --- | --- |
| POST /v1/chat/completions | model, string-content messages with user/assistant/system/developer role, max_tokens or max_completion_tokens, temperature, stream | JSON message content and SSE delta.content, independent choice indexes | tools, function calls, structured output, multimodal arrays, Responses API |
| POST /v1/messages | model, user/assistant string-content messages, optional string system, max_tokens, temperature, stream | Text content blocks and text SSE blocks | tools, thinking, image/audio/document blocks, structured system/content arrays |
| POST /v1/embeddings | model, input string or array of at most 128 strings, optional encoding_format float/base64 | Numeric/encoded vectors passed through; no text restoration | token-ID arrays, streaming, dimensions and other unlisted fields |

Use Authorization: Bearer with the Community API key. Anthropic also accepts x-api-key, which takes precedence when both are supplied. The gateway chooses the provider based on the endpoint, replaces upstream credentials, and uses anthropic-version 2023-06-01. External callers cannot set provider URLs or organization scope.

For text completions, the output-token limit defaults to 4096 and is capped at 32768. Zero is treated as an omitted limit. OpenAI GPT-5 family requests (`gpt-5`, `gpt-5.*`, `gpt-5-*`) use `max_completion_tokens`; the gateway accepts legacy `max_tokens` for these models and normalizes it before dispatch. Other OpenAI chat models default to `max_tokens`, but an explicit `max_completion_tokens` is forwarded unchanged. Supplying both limits with nonzero values returns 400 `invalid_request`. The selected limit is included in the daily token reservation. Anthropic messages use only `max_tokens`; embeddings accept neither nonzero limit. Provider-specific model and parameter support still applies.

Each text field must fit 128 KiB, complete request JSON 1 MiB. NER additionally rejects fields over 512 tokenizer tokens. Embedding masking changes the vector's meaning; applications must account for that.

The upstream credential must be configured even for a request which would subsequently be blocked; an unconfigured provider returns 503 provider_not_configured before scanning. Inspection lab remains available without provider credentials.

Streaming supports text-only choice/block indexes 0-127, UTF-8 JSON content, split placeholders and bounded backpressure. Completion must be explicit; an abrupt stream produces a safe SSE error instead of a successful terminator. Requests are never retried after output begins. Limits are 1 MiB per event and 8 MiB overall.

## SDK examples

Verified locally with Python openai 2.45.0 and anthropic 1.4.0, using a real Rust engine and deterministic test-owned upstream. Provider model availability and live service behavior are not established by these tests.

Set `PROMPT_SHIELD_KEY` to the application access key and `PROVIDER_API_KEY` to the temporary credential for the selected provider. The examples pass both explicitly and disable SDK retries. Replace the local base URL with the configured gateway origin for hosted use; retain `/v1` for OpenAI and the origin alone for Anthropic.

```python
import os
from openai import OpenAI
client = OpenAI(
    base_url="http://127.0.0.1:8080/v1",
    api_key=os.environ["PROMPT_SHIELD_KEY"],
    default_headers={"X-Provider-API-Key": os.environ["PROVIDER_API_KEY"]},
    max_retries=0,
)
answer = client.chat.completions.create(
    model=os.environ["UPSTREAM_MODEL"],
    messages=[{"role": "user", "content": "Contact alice@example.com"}],
)
print(answer.choices[0].message.content)
```

```python
import os
from anthropic import Anthropic
client = Anthropic(
    base_url="http://127.0.0.1:8080",
    api_key=os.environ["PROMPT_SHIELD_KEY"],
    default_headers={"X-Provider-API-Key": os.environ["PROVIDER_API_KEY"]},
    max_retries=0,
)
answer = client.messages.create(
    model=os.environ["UPSTREAM_MODEL"], max_tokens=128,
    messages=[{"role": "user", "content": "Contact alice@example.com"}],
)
print(answer.content[0].text)
```

## Limits and failures

API traffic has both an organization-wide limit of 120 requests and a per-key limit of 120 requests per fixed minute window. Daily UTC quota is shared across an organization and reserves a conservative estimate using sanitized input bytes, request envelope and maximum output tokens. Reservations are not refunds or provider billing; failed calls can consume quota. Reported input/output token counts are recorded separately when supplied by the provider.

Authenticated console requests share a separate 120-request minute limit per organization. Fixed-minute windows begin with the first request, not necessarily at a clock-minute boundary.

### Server-Side Public Rate Limiting & Cost Reduction

- **Environment Separation:** Local development is exempt from IP-based daily quotas to enable local development and functional testing.
- **Daily IP Quota on Server:** With `PUBLIC_DAILY_IP_LIMIT_ENABLED=true` (default), public endpoints, including model discovery and refresh, share **5 requests/IP/UTC day** outside development. Admitted responses include `X-RateLimit-Limit: 5`; exhausted identities receive `429 daily_limit_exceeded` and `Retry-After` until reset. `false` disables this daily check. Community's independent `PUBLIC_IP_RATE_LIMIT_ENABLED` controls minute/per-IP concurrency checks; global capacity and token quotas remain enabled.
- **Fail-fast boundary:** The daily Redis admission check precedes minute/concurrency admission, policy lookup, Rust inspection, audit storage and provider dispatch. A rejected sixth request therefore does not consume those downstream resources. An ingress-level equivalent remains recommended to prevent even the quota-store request from reaching the application.

Errors have an envelope containing error.code, error.type=prompt_shield_error and a safe error.message equal to the code. Typical statuses: 400 invalid/unsupported/blocked input, 401 missing key/session, 403 bad Origin/CSRF, 409 stale policy, 422 unavailable contextual capability, 429 rate/quota exceeded, 502 upstream failure, 503 engine busy/unavailable or storage/audit unavailable. `503 inspection_busy` is a retryable capacity response and includes `Retry-After`; other inspection failures remain `inspection_unavailable`. Mid-stream errors follow SSE instead of changing an already-started HTTP status. Rate-limit rejection includes Retry-After; remaining/reset headers are not advertised.

## Protection queue

Inspection, chat and embeddings share the [core request queue](REQUEST_QUEUE.md).
`X-Prompt-Shield-Progress: v1` opts JSON requests into queue/processing/result/error
SSE events on the same POST. It cannot be combined with `stream=true`. Headerless
JSON/provider SSE formats remain unchanged. Queue errors are `429 queue_full`,
`503 queue_timeout` and `503 queue_unavailable`; after SSE starts, the error event
carries the original status. Model discovery is not queued.

## Internal gRPC

The single source is proto/shield.proto, package promptshield.v1. Both methods require x-engine-token:

- InspectAndSanitize takes request_id, organization_id, UTF-8 text and policy. Returned offsets are zero-based UTF-8 byte ranges with exclusive end. Blocking overrides masking; otherwise merged overlapping spans are replaced with opaque per-request placeholders. Clean rule inspection borrows text internally.
- DeAnonymize takes the same scope, response text and signed mappings. It verifies the scope/signatures and performs one nonrecursive replacement pass. Mapping proofs and originals are internal, never console responses or audit fields.

Do not expose gRPC over an untrusted network. The supplied transport has no external TLS configuration.
