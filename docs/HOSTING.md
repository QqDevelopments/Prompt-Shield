# Hosting plan: Qq Developments

Updated: 2026-10-07. This is the maintained deployment plan for both Prompt Shield repositories. Frontends use Vercel. The earlier backend evaluation recorded **OVHcloud VPS-1 2027**, 2 vCore / 4 GB RAM / Germany - Limburg, as a reference candidate; purchase and provisioning remain pending. No AWS service or other paid infrastructure is selected or provisioned.

## Domain map

| Vercel project / product | Frontend hostname |
| --- | --- |
| Qq Developments main site | qqdevelopments.com |
| Prompt Shield | prompt-shield.qqdevelopments.com |
| Prompt Shield Enterprise | prompt-shield-enterprise.qqdevelopments.com |

These are planned addresses, not evidence of domain ownership or publication. The main site is separate work; this workspace prepares only the two Prompt Shield products.

| Product | Repository | Frontend root | Backend source | Planned API hostname |
| --- | --- | --- | --- | --- |
| Community | prompt-shield | apps/web-console | services/gateway + services/engine | api.prompt-shield.qqdevelopments.com |
| Enterprise | prompt-shield-enterprise | apps/web-dashboard | apps/enterprise-gateway | api.prompt-shield-enterprise.qqdevelopments.com |

Keep two repositories and four independently deployable outputs. DNS will point each frontend to its Vercel project and each API to the eventual backend ingress. The API hostname does not depend on the server provider. Rust gRPC, PostgreSQL and Redis stay private. Enterprise organizations use separately controlled core gateways, not the public Community installation.

## Current deployment state

- Standalone frontend builds, backend images, runtime API-origin packaging and local Compose/Vite flows are implemented.
- Frontend publication, backend purchase/provisioning and DNS remain pending external actions.
- Production activation still requires the acceptance gates in [IMPLEMENTATION_STATUS.md](IMPLEMENTATION_STATUS.md). Publishing a frontend alone does not make inspection, chat or SSO functional before its API is available.

## Local development

No domain, Vercel account or remote backend is required. From the Community repository root, create `.env` from `.env.example` only if it does not already exist. Supply independent OPERATOR_PASSWORD, ENGINE_TOKEN, POSTGRES_PASSWORD and PUBLIC_HMAC_KEY secrets; preserve existing database credentials and volumes. PUBLIC_HMAC_KEY needs at least 32 characters when PUBLIC_ENABLED=true. Keep ENVIRONMENT=development and PUBLIC_ORIGIN=http://127.0.0.1:5173.

```powershell
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build --wait
```

The base Compose file explicitly builds the backend target, without building or copying frontend assets. In another terminal:

```powershell
cd apps/web-console
npm ci
npm run dev
```

On Windows, if installation reports `EPERM` while replacing an `esbuild` or `rollup` binary, stop any running Vite, Playwright, or preview process that uses this folder and run `npm ci` again. Those files can be locked by the active development process; changing permissions or running a second install in parallel does not resolve the lock.

Use http://127.0.0.1:5173 and /admin. Vite proxies /api, /public and /readyz to the local gateway at 127.0.0.1:8080. Port 5173 is strict to avoid silently changing the allowed browser origin. Local public/config.json stays same-origin; do not use build:hosted or compose.hosted.yml for this flow.

For an optional single-origin installation that serves built frontend files from Docker, explicitly add the combined overlay:

```powershell
docker compose --env-file .env -f deploy/docker-compose.yml -f deploy/compose.combined.yml up -d --build --wait
```

That optional mode uses http://localhost:8080 (COMBINED_PUBLIC_ORIGIN can override it) and does not require Vite. The clean-install verifier selects it explicitly to retain its source-only bundled-installation checks. The hosted overlay continues to select the backend-only target.

## Vercel configuration

Create one Vercel project per frontend. Import each repository separately; do not use the workspace or backend directory as the Vercel root.

| Setting | Community | Enterprise |
| --- | --- | --- |
| Root Directory | apps/web-console | apps/web-dashboard |
| Framework | Vite | Vite |
| Node.js | 24.x | 24.x |
| Install Command | npm ci | npm ci |
| Build Command | npm run build:hosted | npm run build:hosted |
| Output Directory | dist | dist |
| Production API_BASE_URL | https://api.prompt-shield.qqdevelopments.com | https://api.prompt-shield-enterprise.qqdevelopments.com |

The checked-in vercel.json supplies framework/install/build/output settings, no-store runtime configuration, framing protection, no-referrer and nosniff. Community /admin routes fall back to index.html. API requests go directly from the browser to the configured backend; no Vercel Functions or API rewrites are required.

Community search discovery covers the public English and Turkish entry pages at `https://prompt-shield.qqdevelopments.com/en` and `/tr`. The root permanently redirects to `/en` on Vercel. The Vite metadata plugin emits separate HTML artifacts containing the corresponding HTML language, translated public title/description and initial visible summary, self-referencing canonical/Open Graph URL, reciprocal `hreflang` links and `x-default` pointing to English. The sitemap contains both language entries and their alternates. Vercel preview/development builds and the development server retain `noindex, nofollow`; preview sitemaps remain empty.

An explicit public language path overrides browser and saved preferences. Changing language updates the URL while preserving the query, fragment and current form/session state; Back/Forward restores the language. Operator `/admin` routes retain stored preferences and remain excluded from discovery. The public metadata behavior is exercised by `scripts/search-metadata.test.ts`, and public-path behavior by `src/lib/publicEntry.test.ts`. The frontend remains a portable static artifact: other hosts must map `/en` and `/tr` to their respective `dist/{locale}/index.html`, permanently redirect `/` to `/en` while preserving query parameters, and retain protected-route exclusion headers.
The Community `/admin` tree and `/config.json` receive `X-Robots-Tag: noindex, nofollow` from `vercel.json`. Robots allows crawling so engines can read those exclusion headers; this does not replace authentication. If serving the static output outside Vercel, configure the same headers on those paths. Enterprise remains entirely excluded from indexing as described in its hosting guide.

Local Chrome checks on 14 October 2026 verified Community language switching and Back navigation with a synthetic text draft preserved, alongside matching URL, HTML language and canonical. Enterprise entry checks used a synthetic unauthenticated API response and preserved a synthetic application-field value while switching and navigating Back; `noindex, nofollow` remained. These are local static-artifact checks, not hosted or live identity/backend acceptance. They do not change the user-owned manual matrix status.

After production deployment, submit `https://prompt-shield.qqdevelopments.com/sitemap.xml` in Search Console and inspect both language entry pages' live URLs and chosen canonical. Verify that `/admin` and its nested routes return the exclusion header before requesting indexing. Local builds cannot establish a Google indexing result or the live Vercel headers; the subdomain returned HTTP 404 during the 2026-10-07 pre-deployment check.

Set API_BASE_URL in Vercel's **Production** environment. It is a public address, never a credential. The hosted build checks it before compiling and writes only apiBaseURL and the fixed productMode to dist/config.json afterward. Missing, HTTP, loopback, credential-bearing or non-origin values fail the build. Standard npm run build retains local/same-origin configuration. A Vercel deployment is immutable: change the environment value and rebuild/redeploy to update its runtime file.

Local production-artifact check from the chosen frontend directory:

```powershell
$env:API_BASE_URL = 'https://api.prompt-shield.qqdevelopments.com'
npm run build:hosted
Remove-Item Env:API_BASE_URL
```

Use the Enterprise API hostname in the Enterprise directory. This command builds files; it does not contact the API or publish them.

For Preview, set a separate API_BASE_URL pointing to an isolated test backend. The build rejects both known production API hostnames when VERCEL_ENV is preview/development. Keep Vercel system environment variables enabled. This guard cannot detect aliases to production; operator routing and CORS remain authoritative.

Authenticated staging must use owned HTTPS frontend/API hostnames under qqdevelopments.com, such as staging-prompt-shield.qqdevelopments.com and api.staging-prompt-shield.qqdevelopments.com. A generated vercel.app preview address is cross-site to qqdevelopments.com and will not support the existing Strict session cookies. Do not weaken cookie settings or allow *.vercel.app on production. Use a stable custom staging hostname and isolated identity/database/cache/core services, or keep previews limited to synthetic UI fixtures.

## Backend configuration without a provider commitment

### Shared Community core releases

Enterprise forwards inspection and protected chat to the organization selected in its operator-controlled `TENANT_BACKENDS_JSON` configuration. Each entry supplies that organization's private Community gateway URL and core access key. Keep these addresses off the public ingress, keep anonymous core endpoints disabled, and preserve separate organization configuration, storage and access credentials. Tenant requests must pass Enterprise role, subscription, model and quota checks before forwarding. A missing or unavailable core must fail closed.

Record the repository revision and immutable gateway/engine image digests used by the public Community deployment and every Enterprise organization core. They use the same Community implementation with separate configuration; repository updates do not update running deployments. Roll out compatible pinned releases deliberately and retain the previous image/configuration references for rollback. This does not change the existing separate metadata and access boundaries.

Frontend sharing happens at build time through a checked-in archive, rather than through the public Community website. Both Vercel projects retain their existing independent roots and `npm ci` commands. Follow the [shared-core workflow](SHARED_CORE_PLAN.md) when updating that archive; no npm publication or sibling repository is required on Vercel.

Build the Community backend from the repository root:

```powershell
docker build --target backend -f services/gateway/Dockerfile -t prompt-shield-backend .
```

The engine has its own Dockerfile at services/engine/Dockerfile. Base Compose explicitly selects the gateway backend target. The Dockerfile also retains a combined target for the optional compose.combined.yml installation; direct docker build commands must specify --target backend when producing a backend-only image.

For the hosted Compose reference, copy .env.example to .env and supply independent random secrets. Set PUBLIC_ORIGIN=https://prompt-shield.qqdevelopments.com. Then:

```powershell
docker compose --env-file .env -f deploy/docker-compose.yml -f deploy/compose.hosted.yml up -d --build --wait
```

The overlay selects the backend-only gateway, ENVIRONMENT=production and DEPLOYMENT_MODE=hosted, enables public access, and excludes environment provider credentials. It does not provision DNS, certificates or a server. Keep loopback ingress behind an operator-configured HTTPS reverse proxy. Keep NER defaults unchanged until the reference acceptance gate passes.

PUBLIC_ORIGIN is the exact frontend origin, not the API origin. ALLOWED_ORIGINS may add explicitly trusted origins; wildcards are rejected. Configure TRUSTED_PROXY_CIDRS only after the actual ingress addresses and forwarding behavior are known. Never trust all Internet addresses. Community operator sessions are process-local, so restarts require login and multiple gateway replicas need sticky routing or a separately designed shared-session solution.

Enterprise build, environment and identity settings are maintained in the companion `prompt-shield-enterprise` repository hosting guide (`docs/HOSTING.md`). Both backends need persistent metadata storage, private cache/engine access, outbound provider connectivity, bounded streaming without proxy buffering, health monitoring, protected secrets and tested backups. These requirements can be met by a VPS/container host or managed services; no specific vendor is required.

### Evaluated reference backend host: OVHcloud VPS-1 (2027)

The following configuration is an earlier reference candidate for low/administrative traffic. Capacity for the combined Community, Enterprise, identity and tenant-core deployment has not been measured on this host. Recheck the recorded specifications and price before purchase:

| Parameter | Evaluated Specification |
| :--- | :--- |
| **Provider** | [OVHcloud](https://www.ovhcloud.com/en/vps/) |
| **Product Tier** | [VPS-1 2027](https://www.ovhcloud.com/en/vps/) |
| **Compute** | 2 vCore (KVM) |
| **Memory** | 4 GB RAM |
| **Storage** | 40 GB SSD NVMe (included) |
| **Bandwidth** | 500 Mbps unlimited public bandwidth |
| **Target Datacenter** | Europe (Germany - Limburg) |
| **OS / Image** | Debian 12 with pre-installed Docker & Docker Compose |
| **Backup** | Automated Backup Standard ($0.00 / included) |
| **Commitment** | No commitment (flexible monthly billing, ~$5.35/month ex. taxes) |

*Deployment rationale:*
* **Capacity assumption:** 4 GB RAM is a provisional budget for the operating system, reverse proxy, both Go gateways, Rust engines, PostgreSQL, Redis and Keycloak. Account for separately controlled Enterprise tenant cores and measure the complete stack under representative traffic before accepting this size. The pinned INT8 artifact is about 104 MiB on disk; runtime/session memory is a separate measurement. See [model provenance](MODEL.md) and [measured engine resources](PERFORMANCE_RESULTS.md). The eight-CPU engine reference does not establish capacity or OOM safety for this two-vCore combined host.
* **Network assumption:** Germany (Limburg) is a geographic candidate; measure user-to-API and provider latency on the actual deployment before making latency claims.
* **Docker setup:** A Docker-ready image simplifies host preparation. Application secrets, identity, tenant cores and the documented Compose configuration still require explicit setup.
* **Status:** Reference candidate only; purchase, provisioning and capacity acceptance remain pending.

## Product and credential boundaries

Community offers account-free inspection/chat at / and operator administration at /admin. The existing /api management and authenticated /v1 integrations remain. Enterprise is a private Qq-operated service for owner-created organizations and invited users, with server-side RBAC and subscription enforcement. Public registration, automated billing, customer installation packages and offline licenses remain outside scope.

Provider keys travel in X-Provider-API-Key, separately from application access keys and identity cookies. They live only in sessionStorage for 30 minutes idle / eight hours absolute, checked at startup and before sending; storage failure falls back to memory. F5 preserves a valid key but clears page-memory conversations. Forget, Enterprise logout and organization switch clear credentials. Browser session restoration can recover a still-valid session key.

Keys never enter public build configuration, URLs, cookies, durable browser storage, query caches, backend persistence, audit, Redis, queues or Rust gRPC. Hosted/public requests do not fall back to server environment provider keys. Self-hosted authenticated /v1 integrations retain that optional fallback. Upstream destinations are operator-controlled and redirects are not followed.

Public endpoints are POST /public/v1/models, /public/v1/inspect, /public/v1/chat/completions, /public/v1/messages and /public/v1/gemini/chat/completions. Model discovery shares public daily/minute/concurrency admission but is outside the protection queue. Public admission defaults are 10 requests/minute/IP, two concurrent/IP and 20 installation-wide, configured with PUBLIC_REQUESTS_PER_MINUTE, PUBLIC_CONCURRENT_PER_IP and PUBLIC_CONCURRENT_GLOBAL. The IP switches below control daily and minute/per-IP checks; global capacity remains. Redis stores expiring HMAC-derived IP identifiers and atomic leases and fails closed; CORS is not authentication or bot prevention.

### Server cost control & daily IP rate limiting

The optional [admission ingress](INGRESS.md) supplies pre-gateway daily admission,
local CORS preflight, persistent counters and streaming forwarding. Its Compose
overlay and Nginx example still require explicit deployment and hosted acceptance;
the presence of these files does not mean a running server has enabled the boundary.

- **Environment Scope:** Local development (`ENVIRONMENT=development`) does not enforce daily IP limits, ensuring uninterrupted local development, debugging, and testing.
- **Hosted Server Quota:** `PUBLIC_DAILY_IP_LIMIT_ENABLED=true` (default) enforces **5 requests/IP/UTC day** outside development; `false` disables it. `PUBLIC_IP_RATE_LIMIT_ENABLED=false` separately disables Community minute/per-IP concurrency admission. Public global capacity, tenant quotas and core capacity remain enabled. Apply the same switches at ingress when it is provisioned.
- **Fail-fast cost boundary:** The application performs one atomic Redis admission check, then rejects an exhausted IP before minute/concurrency admission, policy lookup, Rust inference, audit persistence, or upstream provider dispatch. Configure the same ceiling at ingress when available so even rejected application requests do not reach the service. Do not add polling, telemetry, preflight variants, or uncached calls that bypass this boundary.

Preserve Community AGPL/self-hosting rights and Enterprise hosted-service terms. Publication review must establish ownership, contribution and third-party licensing rights. Repository separation does not settle those obligations. Sector models remain research; no compliance certification or unmeasured performance is claimed.

### Shared protection capacity

Follow [REQUEST_QUEUE.md](REQUEST_QUEUE.md) for the canonical queue, env, failure and
progress protocol contract. Tenant Community cores and the public Community service
use the same code with separate organization/Redis namespaces. Enterprise forwards
after authorization; it never adds another queue. Keep Redis and queue settings
consistent across replicas, drain before reconfiguration, and preserve transient
key/mapping boundaries. Deploy backends before the updated shared frontend package.

The ingress must flush opt-in POST SSE without buffering, allow
`X-Prompt-Shield-Progress`, propagate cancellation, and size timeouts for queue wait
plus at least 100 seconds. Measure capacity, disconnects and cost on the selected
host; local queue acceptance does not establish these hosted properties.

## Portability and references

The frontend remains an ordinary dist directory. Other static hosts can use npm run build:hosted with API_BASE_URL or replace dist/config.json after npm run build. Never modify hashed application assets to change an API address. Existing Netlify settings and deploy/nginx-static.conf remain usable alternatives, not selected providers.

Apply an environment-specific CSP allowing the exact API origin before public activation; deploy/nginx-static.conf illustrates a restrictive policy. The Vercel baseline headers do not constitute a complete CSP rollout. Disable session replay, third-party analytics and request/body logging on credential flows.

Configuration references: [Vercel project configuration](https://vercel.com/docs/project-configuration), [Vercel system environment variables](https://vercel.com/docs/environment-variables/system-environment-variables), [Vercel domains](https://vercel.com/docs/domains/working-with-domains), [OVHcloud VPS](https://www.ovhcloud.com/en/vps/).
