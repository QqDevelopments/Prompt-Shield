# Prompt Shield Community

Self-hosted or account-free hosted text inspection and a restricted OpenAI/Anthropic/Google Gemini proxy by **Qq Developments**.

Community includes a Rust gRPC engine, Go gateway, PostgreSQL audit metadata, Redis limits, and an authenticated React operator console. The console supports **English and Turkish**, with **light, dark, and system** appearance. The [current verification record](docs/IMPLEMENTATION_STATUS.md#recorded-ci-verification) includes the completed sustained CI run; [local measured performance](docs/PERFORMANCE_RESULTS.md) remains a separate reference. Neither establishes production certification or universal prompt-injection protection.

## Start locally

Install Docker with Compose and Node.js 24. Run from this repository root:

```powershell
if (!(Test-Path .env)) { Copy-Item .env.example .env }
# Supply four different random secrets, including PUBLIC_HMAC_KEY.
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build --wait
```

The command starts only Go Gateway, Rust Engine, PostgreSQL and Redis. Set `PUBLIC_ORIGIN=http://127.0.0.1:5173` in `.env`. In another terminal, start the frontend:

```powershell
cd apps/web-console
npm ci
npm run dev
```

Open [Community](http://127.0.0.1:5173) for account-free inspection and protected chat with your temporary provider key. Open [administration](http://127.0.0.1:5173/admin) for operator login. Vite forwards API requests to `127.0.0.1:8080`; use the exact `127.0.0.1:5173` frontend address so its Origin matches the backend configuration. There is no default password. Inspection does not call an external provider. Authenticated `/v1` clients use a Prompt Shield access key plus `X-Provider-API-Key`; only self-hosted integrations may fall back to gateway environment credentials.

If the gateway exits with `PUBLIC_HMAC_KEY requires at least 32 characters`, populate that entry in `.env` with an independent random secret and rerun Compose. Enabling public endpoints requires this key even for local use. See [local setup and optional combined installation](docs/HOSTING.md#local-development).

After entering a provider session key, the Community model dropdown loads your account's models. Refresh models reloads the list. The newest listed Luna model is preferred for OpenAI and Sonnet for Claude; Gemini requires explicit model selection. Choose a model supporting text chat; listing access alone does not establish endpoint compatibility. Model lists stay in page memory and are discarded when the key expires, is forgotten, or the provider changes. Discovery uses the [OpenAI model list](https://developers.openai.com/api/reference/resources/models/methods/list), [Claude model list](https://platform.claude.com/docs/en/api/models/list) and [Google AI Studio OpenAI-compatible API](https://ai.google.dev/gemini-api/docs/openai) contracts.

The planned frontend is `prompt-shield.qqdevelopments.com` on Vercel, with direct API requests to `api.prompt-shield.qqdevelopments.com`. The backend provider is undecided. See the [hosting plan and Vercel settings](docs/HOSTING.md) and [implementation status](docs/IMPLEMENTATION_STATUS.md) for independent builds and pending deployment acceptance.

The default mode uses deterministic rules. To enable contextual English name/organization/location detection:

```powershell
node --no-warnings --experimental-strip-types scripts/fetch-model.ts
# Set NER_ENABLED=true in .env.
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build --wait
```

Then enable contextual detection on the Security policy screen. Downloaded artifacts are pinned and verified by SHA-256. A required but unavailable model stops contextual requests. Turkish interface language does not imply Turkish NER support.

Both the current BERT INT8 artifact and its independently generated BERT INT8 conversion pass the unchanged accuracy-loss limits relative to FP32 after the whole-word masking correction. The two separately evaluated smaller DistilBERT models failed protection gates. The default artifact is unchanged; see [model evaluation evidence](docs/MODEL.md).

The **short-NER gRPC P99 <50 ms optimization goal is advisory**, not a mandatory release or acceptance gate. The latest three local baseline repetitions measured 40.4–49.3 ms P99 under the documented workload. Passing CI does not establish that latency or sustained capacity on another host; see [measured performance](docs/PERFORMANCE_RESULTS.md).

Compose binds HTTP to the local machine. Before remote use, follow the [operator guide](docs/OPERATIONS.md) for HTTPS, secrets, access, backups, and retention. Development mode permits HTTP cookies and must not be exposed as an Internet deployment.

## Implemented scope

| Area | Behavior |
| --- | --- |
| Rules | Email, validated card/IBAN/Turkish ID, limited phone formats, selected key patterns, custom keywords and four injection phrases |
| Optional NER | Pinned INT8 BERT NER; English PERSON, ORGANIZATION, LOCATION; at most 512 tokenizer tokens |
| Proxy | OpenAI chat/embeddings, Anthropic text messages and Google AI Studio Gemini text chat; bounded SSE streaming/restoration |
| Privacy | Request-scoped opaque placeholders; signed restoration mappings; metadata-only application audit |
| Operations | Organization-scoped keys/versioned policies; minute limits; conservative daily quota reservations; retention |
| Console | Readiness, inspection, policies, one-time key creation/revocation, paginated filtered events, integration and preferences |

Unsupported tool calls, image/audio content, structured output, Responses API, and unsupported JSON fields are rejected. See the [API contract](docs/API.md).

## Community and Enterprise

| Feature | Community | Enterprise |
| --- | :---: | :---: |
| PII masking, secret blocking and protected chat | ✅ | ✅ |
| OpenAI, Anthropic and Google Gemini text chat | ✅ | ✅ |
| Shared core request queue and live progress | ✅ | ✅ |
| Configurable anonymous daily IP admission | ✅ | ✅ |
| OpenAI embeddings API | ✅ | ❌ |
| Application keys and security event views | ✅ | ✅ |
| Multiple organizations with separate tenant access | ❌ | ✅ |
| Keycloak identity and email-bound invitations | ❌ | ✅ |
| Organization and department roles, model permissions and quotas | ❌ | ✅ |
| Application verification, review and organization provisioning | ❌ | ✅ |
| Subscription expiry, suspension and renewal controls | ❌ | ✅ |
| SCIM Users and Groups API | ❌ | ✅ |
| Signed JSON metadata reports and browser print view | ❌ | ✅ |
| Splunk/Datadog event delivery | ❌ | ✅ |
| Customer self-hosting | ✅ | ❌ |

Community operates one configured organization. Enterprise is a Qq-operated service and uses the Community protection core for each organization; local Enterprise development is supported. Enterprise implements its own tenant-specific policies, application keys and event management. The SCIM API implements a bounded subset, and report printouts are not compliance certificates. Sector-specific models, compliance PDF reports, automated billing and automatic customer infrastructure are not delivered by either repository. See Enterprise capabilities and its acceptance matrix in the companion `prompt-shield-enterprise` repository for implementation and test boundaries.

## Development and verification

Shared browser credential, language-route, preference and queue-progress helpers are maintained in `packages/web-core` and consumed by both frontends as the same versioned archive. From `apps/web-console`, run `npm run shared:update -- --version 0.1.2` to rebuild and update both consumers. Choose a new version for changed package content. See the [shared-core workflow](docs/SHARED_CORE_PLAN.md) and [request queue contract](docs/REQUEST_QUEUE.md).

The gateway module targets Go 1.25; the current CI and verification reference use Go 1.27. Use Rust 1.90+ and Node.js 24. Lockfiles resolve dependencies. See the [verification report](docs/IMPLEMENTATION_STATUS.md) for tested versions and integration setup. `npm run verify` from `apps/web-console` includes frontend checks, a full npm audit, and Rust/Go dependency security scans. Scanner setup and separate frontend/backend commands are maintained in the [dependency security gates](docs/VERIFICATION.md#automated-dependency-security-gates).

```powershell
cd services/engine
cargo test --features ner
cargo clippy --all-targets --features ner -- -D warnings
# After fetching the pinned model, explicitly exercise model and pool boundaries.
cargo test --locked --features evaluation --test ner --test pool -- --include-ignored --test-threads=1
cd ../gateway
go test ./...
go vet ./...
cd ../../apps/web-console
npm ci
npm run lint
npm test
npm run build
npx playwright install chromium
npm run test:e2e
```

From the root, `./scripts/proto.ps1` generates Go bindings with pinned plugins. Rust bindings are generated at build time from the same [protocol](proto/shield.proto). Unix `make proto` requires protoc and the plugins pinned in that script. Database, Redis, real-engine and Python SDK tests are explicitly opt-in; ordinary test success does not imply those boundaries ran.

- [Hosting plan and domain map](docs/HOSTING.md)
- [Verification and remaining release gates](docs/IMPLEMENTATION_STATUS.md)
- [Reproducible model, load and clean-install commands](docs/VERIFICATION.md)
- [Manual workflow guide](docs/MANUAL_TEST_GUIDE_TR.md), [acceptance matrix](docs/MANUAL_TEST_MATRIX.md), and [synthetic test examples](docs/MANUAL_TEST_EXAMPLES.md)
- [Measured performance](docs/PERFORMANCE_RESULTS.md)
- [Security boundaries](docs/SECURITY.md)
- [Model provenance](docs/MODEL.md)
- [Operator guide](docs/OPERATIONS.md)

## License and contributions

Community remains [AGPLv3](LICENSE), including self-hosted use. Enterprise has separate hosted-service terms. Verify shared-core licensing rights before proprietary release; repository separation does not imply an exemption.

External contributions require the project's Contributor License Agreement. Contact the maintainers for the current signing process; no CLA automation is claimed here.

Editor recommendations and settings are maintained in [.vscode/extensions.json](.vscode/extensions.json) and [.vscode/settings.json](.vscode/settings.json); coding-agent instructions are in [AGENTS.md](AGENTS.md).

See [recommended VS Code extensions](docs/RECOMMENDED_EXTENSIONS.md) for installation and [coding-agent skills](docs/RECOMMENDED_SKILLS.md) for the separate shared-workspace instructions.

Contact **Qq Developments** at [qqdevelopments@gmail.com](mailto:qqdevelopments@gmail.com).
