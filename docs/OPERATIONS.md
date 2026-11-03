# Operator guide

## Local Compose

Copy .env.example to .env and set distinct random OPERATOR_PASSWORD, ENGINE_TOKEN, POSTGRES_PASSWORD and PUBLIC_HMAC_KEY values. Use a URL-safe hexadecimal database password because Compose places it in the connection URL. Keep .env out of version control. The operator password must be 16-72 bytes; the engine token at least 32 ASCII characters and public HMAC key at least 32 characters. Environment injection is intended for local setup; production operators should supply secrets through their deployment environment and restrict Docker/host access.

```powershell
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build --wait
docker compose --env-file .env -f deploy/docker-compose.yml ps
```

The gateway publishes 127.0.0.1:8080. Engine, PostgreSQL and Redis use a private network. Gateway readiness tests all dependencies, including authenticated engine access. PostgreSQL/Redis have health checks. The gateway runs as nonroot with a read-only filesystem, dropped capabilities and no new privileges; engine uses the same restrictions. PostgreSQL data is kept in a named volume. Optional NER uses the host's models directory read-only.

Base Compose builds only the backend. Start the frontend separately with npm ci and npm run dev in apps/web-console. Set PUBLIC_ORIGIN=http://127.0.0.1:5173, then use http://127.0.0.1:5173 for account-free inspection/chat and /admin for operator login. Vite proxies API requests to port 8080. Configure policy and create integration keys in administration. Public chat requires a temporary user provider key. Provider URLs default to the official APIs; self-hosted native processes support OPENAI_BASE_URL and ANTHROPIC_BASE_URL for operator-controlled compatible endpoints. Hosted mode requires official provider URLs and rejects environment provider keys. The optional combined overlay is documented in [local development](HOSTING.md#local-development).

To enable NER, run `node --no-warnings --experimental-strip-types scripts/fetch-model.ts`, set NER_ENABLED=true and recreate services. Offline deployment requires provisioning the same verified artifacts in models/general-ner before startup. The build still requires its dependency registry/image access unless separately cached.

## Remote access and HTTPS

Keep gateway ingress on loopback behind a host reverse proxy. Set ENVIRONMENT=production and PUBLIC_ORIGIN to the exact frontend HTTPS origin, recreate the gateway, terminate TLS at the reverse proxy and forward requests to 127.0.0.1:8080. Do not disable Secure cookies to work around TLS configuration. Disable request/response body logging, preserve Origin, restrict administration access and configure streaming without response buffering. Forwarded headers are ignored by default; set TRUSTED_PROXY_CIDRS only for the actual ingress chain after choosing the backend infrastructure.

This repository does not provision certificates or deploy a public reverse proxy. Keep gRPC on the private engine network. Separate Vercel frontends are supported with exact CORS origins, credentialed requests and CSRF; follow [HOSTING.md](HOSTING.md) for the approved domain map and preview isolation. Operator sessions are local to a gateway process, so restarts require login again and multiple replicas need sticky routing or a separately designed shared-session solution. The backend provider is undecided.

Outside development, public traffic defaults to **5 requests/IP/UTC day** with `PUBLIC_DAILY_IP_LIMIT_ENABLED=true`; `false` disables daily admission. `PUBLIC_IP_RATE_LIMIT_ENABLED` independently controls Community minute/per-IP concurrency admission. Global capacity, core queue capacity and token quotas remain enforced. The daily Redis check rejects exhausted identities before policy, Rust, audit and provider work. Mirror the configured switches at ingress. Queue configuration, controlled restarts and recovery limits are defined in [REQUEST_QUEUE.md](REQUEST_QUEUE.md).

## Configuration

| Variable | Default / effect |
| --- | --- |
| ENVIRONMENT | Explicit development enables HTTP; any other value requires HTTPS and database/cache |
| PUBLIC_ORIGIN | Base Compose: http://127.0.0.1:5173; exact browser frontend origin |
| ORGANIZATION_ID | community; console scope |
| OPERATOR_PASSWORD, ENGINE_TOKEN | Required; no reusable defaults |
| POSTGRES_PASSWORD | Required by Compose |
| DATABASE_URL, REDIS_URL | Compose fills internal URLs; native production requires both |
| GATEWAY_LISTEN / ENGINE_LISTEN | Native loopback defaults; images bind their internal interfaces |
| RUST_ENGINE_ADDR | Native 127.0.0.1:50051; Compose engine:50051 |
| OPENAI_API_KEY / ANTHROPIC_API_KEY / GEMINI_API_KEY | Optional self-hosted authenticated integration fallback; rejected in hosted mode |
| GEMINI_BASE_URL | https://generativelanguage.googleapis.com/v1beta/openai; hosted mode requires this official URL |
| DEPLOYMENT_MODE | self-hosted by default; hosted rejects environment provider credentials and nonofficial upstream URLs |
| PUBLIC_ENABLED / PUBLIC_HMAC_KEY | Account-free endpoints; require an independent random 32+ character HMAC key |
| ALLOWED_ORIGINS / TRUSTED_PROXY_CIDRS | Additional exact frontend origins / actual trusted ingress networks; empty by default |
| NER_ENABLED | false; requires verified model/tokenizer and Rust ner feature |
| NER_SESSION_COUNT / NER_INTRA_THREADS | 1 / 4; each must be 1, 2 or 4, with product at most eight; validated at startup |
| NER_SPINNING | true; accepts true/false; tune only with the manual performance reference matrix |
| ONNX_MODEL_PATH / TOKENIZER_CONFIG_PATH | Required when NER enabled; Compose fills mounted paths |
| DAILY_TOKEN_BUDGET | 1000000 conservative reservation units per organization/UTC day |
| AUDIT_RETENTION_DAYS | 30, valid range 1-3650; PostgreSQL prune runs hourly |
| METRICS_TOKEN | Empty disables metrics access; at least 32 characters to scrape |
| CONSOLE_ASSETS | Native ../../apps/web-console/dist; image /console |

Native development without DATABASE_URL explicitly uses ephemeral storage: metadata, keys, policies and quotas disappear at process exit. It retains at most 10000 events and 1000 keys per organization. It is not the persistent Compose mode.

NER settings are read and validated when contextual inference is enabled at startup. Changing them requires recreating the engine; they are not live policy settings. The pool has four running/waiting NER slots regardless of session count, separate from four prefilter slots. Cancellation signals active native work, but its lease is retained until that work exits. Do not raise session counts solely from a short benchmark: the measured four-session mixed soak peaked at 1,005.543 MiB under a 1,024 MiB limit. Keep the documented defaults unless performing a controlled evaluation of quality, safety, recovery and resource use. The short-NER P99 <50 ms goal is advisory, not a mandatory release, acceptance or default-promotion gate. See [performance results](PERFORMANCE_RESULTS.md) and [manual reference commands](VERIFICATION.md#manual-performance-reference).

## Upgrade, backup and recovery

Take a PostgreSQL backup with pg_dump using your normal protected backup destination, verify restore into a separate database, and retain the deployment configuration/model manifest. Backups contain operator configuration, key hashes and audit metadata; treat them as sensitive even without prompt bodies.

Migrations run at gateway startup under a PostgreSQL advisory lock and record applied versions. Back up before an upgrade, build/recreate services, then verify readiness, login, policy, key authentication and a synthetic inspection. Downgrade migrations are not provided: recovery uses a tested database backup with the matching application version.

Do not use docker compose down -v as routine shutdown; that removes the persistent database. Ordinary down preserves volumes. Rotating OPERATOR_PASSWORD invalidates practical access on gateway restart; existing in-memory sessions are removed. Rotate ENGINE_TOKEN on engine and gateway together. Revoked Community keys stop authenticating on the next request. Replace upstream credentials and recreate the gateway.

The supplied Redis service is nonpersistent. Restarting it can reset quotas/rate limits. Production operators requiring durable budget accounting must configure Redis persistence/backup and appropriate availability; provider usage records remain in PostgreSQL.

## CLI

From services/gateway, configure DATABASE_URL and use:

```powershell
go run ./cmd/admin -action policy -org community
go run ./cmd/admin -action create-key -org community -name "Application"
go run ./cmd/admin -action revoke-key -org community -id KEY_ID
go run ./cmd/admin -action prune -retention-days 30
```

Create-key prints the secret once: avoid captured/shared terminals. Prune applies the installation retention cutoff across organizations. Policy edits use the authenticated API/console and optimistic version checks.

## Metrics

deploy/prometheus.yml is a scrape configuration template. Mount the same METRICS_TOKEN at /run/secrets/metrics-token in your Prometheus deployment and join a network that can reach gateway:8080. Prometheus itself is not started by default Compose.

Counters have fixed labels only: security decisions, inspection count/duration sum and audit failures. Import deploy/grafana/community.json into your Grafana instance and select the Prometheus datasource. It charts decision rate, mean duration, audit failures and scrape availability. Scrape availability is not dependency readiness; monitor /readyz separately. No histogram quantiles, per-tenant secret labels or latency SLA are claimed.

The dashboard follows Grafana's documented [JSON model](https://grafana.com/docs/grafana/latest/visualizations/dashboards/build-dashboards/view-dashboard-json-model/). It is a supplied import artifact; Grafana/Prometheus are not deployed or runtime-verified by the local application tests.

## Verification environments

deploy/compose.dependencies.yml is an isolated synthetic PostgreSQL/Redis fixture on localhost ports 15432/16379. deploy/compose.verify.yml is a test-only override which attaches engine egress and publishes gRPC on localhost:15052; never include it in normal deployments. Integration scripts use only synthetic text. See IMPLEMENTATION_STATUS.md for commands and results.
