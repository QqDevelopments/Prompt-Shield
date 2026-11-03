# Hosted admission ingress

The optional Go ingress rejects excess anonymous traffic before the application
gateway. It handles browser CORS preflight locally, forwards the verified client
address, and streams the original response without buffering. Nginx remains the
TLS entry point. Keep the operator IP allowlist until hosted acceptance passes.

## Admission contract

- Outside development, `PUBLIC_DAILY_IP_LIMIT_ENABLED=true` or an unset value
  permits five anonymous POST actions per client IP per UTC day. `false` disables
  only this daily check. Gateway minute/concurrency, core capacity, authentication
  and tenant quotas remain independent.
- Community shares the allowance across `/public/v1/models`, `/public/v1/inspect`,
  `/public/v1/chat/completions`, `/public/v1/messages` and
  `/public/v1/gemini/chat/completions`.
- `INGRESS_PRODUCT=enterprise` shares an anonymous allowance between POST
  `/public/v1/applications`, POST `/public/v1/application-access`, and POST
  `/public/v1/application-access/consume`. It does not
  cap authenticated Enterprise chat. The supplied Community Compose overlay
  deploys Community only; Enterprise has a separate
  hosted overlay (`deploy/compose.ingress.yml` in `prompt-shield-enterprise`)
  with an isolated ingress store.
- The ingress counter and gateway counter occupy separate namespaces. Each
  admits at most five actions; a single request is not charged twice to one
  counter. Gateway admission remains a backstop. Existing gateway consumption can
  make the effective allowance smaller during migration. Do not reset live quotas
  to force five new actions; validate a fresh UTC day or an isolated test instance.
- Redis admission is atomic across ingress instances. Persistent AOF storage is
  configured with `appendfsync always`. Do not delete the ingress data volume.
  Failed persistence, a full store or unavailable Redis fails admission closed.
- Once exhaustion is known, subsequent POST and matching OPTIONS requests return
  429 from a bounded local HMAC cache until UTC midnight, with no gateway or Redis
  call. After a restart, on another replica or when the cache is full, Redis may
  be consulted again, but a denied POST still never reaches the gateway.
- OPTIONS does not consume quota and never reaches the gateway or Redis. A cold
  cache may answer 204 for an exhausted IP; its following POST is rejected by
  Redis admission before gateway dispatch. Unsupported headers/origins are denied.
- Counter keys contain HMAC-derived IP identifiers, product and UTC date. The
  service does not log addresses, request URLs, prompts, provider keys or cookies.
  Nginx logging remains an independent operator responsibility.

## Community deployment

Use `deploy/docker-compose.yml`, the appropriate hosted configuration, then
`deploy/compose.ingress.yml` **last**. The last file changes gateway trust to the
specific ingress IP. Its default dedicated network is `172.30.90.0/29`, with ingress
at `.2`, gateway at `.3`, and Redis at `.4`. A separate host-facing bridge defaults
to `172.30.91.0/29`, with ingress at `.2` and its host bridge at `.1`; this permits
Docker to publish the ingress port on host loopback. Admission and Redis remain on
the internal network. Check both subnets for conflicts before deployment; override
`INGRESS_SUBNET`, `INGRESS_PRIVATE_IP`, `INGRESS_GATEWAY_IP`, `INGRESS_REDIS_IP`,
`INGRESS_EDGE_SUBNET`, `INGRESS_EDGE_IP` and `INGRESS_HOST_PROXY_CIDR` together when
needed. Measure the actual Nginx peer as in the acceptance below.

Set the following in the selected environment file, preserving all existing
database, operator, engine and HMAC secrets:

```dotenv
PUBLIC_ORIGIN=https://prompt-shield.qqdevelopments.com
METRICS_TOKEN=<independent random secret of at least 32 characters>
PUBLIC_DAILY_IP_LIMIT_ENABLED=true
```

Do not place backend secrets in Vercel. The metrics token may be generated locally
and inserted without printing it; keep `.env` mode 0600. The Redis ingress has no
published port. Do not publish the gateway directly on any external address.

For the standard hosted reference, from the Community repository root:

```bash
docker compose --env-file .env \
  -f deploy/docker-compose.yml -f deploy/compose.hosted.yml \
  -f deploy/compose.ingress.yml build ingress gateway

docker compose --env-file .env \
  -f deploy/docker-compose.yml -f deploy/compose.hosted.yml \
  -f deploy/compose.ingress.yml up -d --wait
```

For the independently configured server in this rollout, retain its project name,
environment file, and existing production override. Do not run the standard
reference command against that server. After the user pushes the release and
pulls it on the server:

```bash
cd /opt/prompt-shield/community

docker compose -p community-production \
  --env-file /opt/prompt-shield/community-production/.env \
  -f deploy/docker-compose.yml \
  -f /opt/prompt-shield/community-production/compose.production.yml \
  -f deploy/compose.ingress.yml build ingress

docker compose -p community-production \
  --env-file /opt/prompt-shield/community-production/.env \
  -f deploy/docker-compose.yml \
  -f /opt/prompt-shield/community-production/compose.production.yml \
  -f deploy/compose.ingress.yml up -d --no-build --wait
```

The existing gateway image is reused in this second path; ingress needs a new
build. Keep the existing IP allowlist. Merge
[`nginx-api-ingress.conf.example`](../deploy/nginx-api-ingress.conf.example) into
the current API TLS virtual host: use loopback `8084` for `proxy_pass` and deny
public `/ingress/` access. Retain certificate paths, HTTP ACME challenge handling
and allowlist rules. Run `nginx -t` before reload. Every future Compose command for
this stack must include the ingress overlay; omitting it can remove the trust
settings/network. Do not return Nginx to the old gateway port as an implicit fallback.

`/ingress/healthz` indicates process availability, not complete application/store
readiness. `/readyz` is forwarded to the application and does not consume quota.
Local `/ingress/metrics` requires `Authorization: Bearer <METRICS_TOKEN>` and exposes
only `forwarded`, `daily_denied`, `preflights` and `store_errors` totals. Nginx must
exclude these internal ingress endpoints from public routing.

## Acceptance and rollback

1. Preserve the existing Nginx file before changing it. Confirm Compose shows
   healthy gateway/ingress/Redis and loopback-only host bindings. Inspect the
   actual ingress peer and verify that the trust CIDR is its exact host bridge IP.
2. From the owned frontend origin, validate preflight with both `content-type` and
   `x-prompt-shield-progress`. Validate rejected origins and unsupported headers.
   A client must not change its quota by supplying `X-Forwarded-For`.
3. Record the protected metrics before and after five synthetic actions and the
   sixth denial. The sixth action must return `429 daily_limit_exceeded`, a
   `Retry-After` value to UTC reset and a browser-readable CORS response, while
   `forwarded` remains unchanged. An exhausted preflight must not reach the gateway.
4. Restart ingress while retaining Redis/volume; the allowance must remain spent.
   Stop the isolated ingress Redis to verify new admissions fail closed. Restore
   it and verify recovery. Validate UTC rollover in an isolated deterministic test,
   rather than changing the production host clock.
5. Validate a real POST SSE stream and browser cancellation through Nginx. Confirm
   the minute/IP switch, disabled daily switch, global/core capacity and tenant
   quotas retain their separate boundaries.
6. A local passing test is not hosted acceptance. Keep production IP restrictions
   until these checks and the remaining [hosting gates](HOSTING.md) pass.

For rollback, keep the external API restricted or unavailable, restore the previous
Nginx configuration and tested Compose/image set deliberately, then recheck proxy
trust. Preserve ingress Redis data so a rollback/redeploy does not grant a fresh
allowance. Do not delete volumes or clear counters as part of rollback.

## Verification

```bash
cd services/gateway
go test ./internal/ingress ./cmd/ingress -count=1
TEST_REDIS_URL=redis://127.0.0.1:6379/0 go test ./internal/ingress -count=1
INGRESS_SLOW_TEST=1 go test ./internal/ingress -run '^TestSlowUpstreamResponseHeaders$' -count=1
```

Use an isolated Redis for integration tests. The tests exercise the production HTTP
handler, SSE flush/cancellation, concurrent shared-counter behavior, safe forwarding,
UTC rollover, daily disablement, Enterprise action scope and Redis Lua admission.
The opt-in slow check waits 46 seconds before response headers to cover the former
premature 45-second upstream timeout.
They do not inspect implementation source text or establish production host capacity.
