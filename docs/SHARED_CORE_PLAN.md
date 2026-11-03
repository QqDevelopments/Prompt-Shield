# Shared Community core and frontend package

Implemented on 2026-10-29. This document maintains the shared ownership and update workflow; it does not record hosted acceptance.

## Ownership

| Responsibility | Owner |
| --- | --- |
| Detection, masking, blocking, provider contracts and restoration | Community gateway and Rust engine; Enterprise forwards to its configured organization core |
| Identity, roles, organizations, subscriptions, department policies and quotas | Enterprise; checked before forwarding to the organization core |
| Member application keys, Enterprise metadata, reports and SIEM | Enterprise |
| Provider identifiers and types | Community `packages/web-core`; both frontends use the same versioned archive |
| Temporary browser credential validation, expiry, Forget and storage-failure fallback | Community `packages/web-core` |
| Public language paths and locale/theme normalization | Community `packages/web-core` |
| Request progress types and POST SSE reader | Community `packages/web-core`; product screens remain separate |
| Shared protection FIFO and capacity across gateway replicas | Community gateway/Redis; Enterprise uses its tenant core's queue |
| Product API paths, storage codecs/namespaces and Enterprise organization binding | Each frontend's thin adapters |
| React screens and contexts, translations, SEO and product session management | Each frontend's application code |

Sharing an implementation does not mean sharing all credentials, database records or service instances. Enterprise keys identify members and organizations; core keys authenticate the Enterprise gateway to its Community core. Existing backend APIs and embeddings availability are unchanged. Private core routing and deployment image pinning are maintained in [HOSTING.md](HOSTING.md#shared-community-core-releases).

## Frontend package

The current consumers use `0.1.2`, including the [request queue progress reader](REQUEST_QUEUE.md).

The package is `@qqdevelopments/prompt-shield-web-core`, initially version `0.1.0`. Its TypeScript source is maintained only under Community `packages/web-core`. It has no runtime dependencies or React dependency. Browser exports do not access storage or the DOM at module initialization. A separate Node-only `distribution` export verifies local archive metadata and lockfile integrity.

Both applications commit the same `vendor/qqdevelopments-prompt-shield-web-core-<version>.tgz`, `vendor/shared-core.json`, dependency entry and lockfile. The archive contains compiled ESM, type declarations, package metadata and the Community license. Generated JavaScript is build output inside the archive, not maintained script/test source.

Community preserves `ps.provider-session.v1` and its existing `key` record field. Enterprise preserves `pse.provider.v1`, the `value` field and `organization` binding. Valid existing records remain usable after upgrade. Both retain 30-minute idle/eight-hour absolute expiry, Forget and storage-failure memory fallback. Product preference storage, legacy Enterprise preference keys and translation files are unchanged.

## Update both frontends

Run from Community `apps/web-console` with Node.js 24 and both repositories available:

```text
npm run shared:update -- --version 0.1.2
```

For a source change after releasing `0.1.2`, choose a new version, for example `--version 0.1.3`. The helper updates the source package version/lock metadata after successful consumer updates. It refuses different content under an existing version. Running the same version with unchanged content is idempotent.

The default Enterprise location is the sibling `prompt-shield-enterprise` repository. Override it when needed:

```text
npm run shared:update -- --version 0.1.3 --enterprise-root "C:/path/to/prompt-shield-enterprise"
```

The helper validates both frontend paths before changes, installs/checks/tests the source package, builds and packs it once, copies identical bytes to both consumers and updates their dependency locks. Failed consumer updates restore the exact prior contents of files touched by the helper. Previous archive versions are retained. It never stages, commits, pushes, publishes or deploys.

After updating, run `npm ci` in each frontend to install the newly selected local archive, then run its normal checks and build. Review and commit the source package, affected adapters, archives, metadata and lockfiles through the normal user-owned Git workflow. Rebuild/redeploy each frontend to deliver the update to users.

## Checks and independent deployment

From Community `apps/web-console`:

```text
npm run shared:check
```

This rebuilds/tests the source package in temporary build output and compares the resulting archive with the consumer's recorded version/integrity. When the default sibling Enterprise repository exists, it also checks that consumer; an explicit invalid `--enterprise-root` always fails. It does not rewrite tracked package artifacts or consumer files.

From an independently checked-out frontend, after `npm ci`:

| Frontend | Consumer archive/lock check |
| --- | --- |
| Community | `npm run shared:check:consumer` |
| Enterprise | `npm run shared:check` |

These consumer checks need only that frontend's repository and committed archive. They verify archive bytes, package name/version, dependency path and lockfile integrity. Community CI additionally checks freshness against the source package; Enterprise CI verifies its pinned consumer artifact. Neither CI automatically updates packages. Both Vercel frontends continue to use their existing install/build settings.

Functional tests invoke the common functions and the real update helper with temporary consumer directories, including injected lock-command failure. Frontend adapter and browser regressions cover retained storage, provider/organization clearing and late-response cancellation. Manual acceptance matrices retain their recorded environment/revision boundaries; passing this refactor does not establish hosted deployment acceptance.

## Local verification and remaining acceptance

The [frontend verification record](evidence/shared-frontend-package-20261029.json) records passing package/helper tests, both frontend type checks/tests/builds, nine selected Chromium scenarios per product, and independent installs/builds without sibling checkout access. The Enterprise routing record (`docs/evidence/shared-backend-routing-20261029.json` in `prompt-shield-enterprise`) records nine passing top-level Go tests and 15 subtests with the race detector, using real PostgreSQL/Redis and synthetic HTTP core receivers. These are different boundaries.

The existing Windows pilot (`docs/LOCAL_PILOT.md` in `prompt-shield-enterprise`) supplies two actual Community gateways, two actual Rust rules engines, Enterprise, Keycloak, PostgreSQL/Redis and a synthetic provider. Its opt-in live profile covers both organizations and all three provider routes with JSON and SSE responses, checks restored response text and zero provider dispatch for blocked/denied requests, and exercises cancellation and engine outage/recovery. The live shared-core record (`docs/evidence/shared-core-live-20261029.json` in `prompt-shield-enterprise`) captures the result and exact environment/revision boundaries. The rules-only profile does not evaluate NER models or contact external providers.

No additional shared backend library or React abstraction is required for this scope. Preserve the current HTTP contract and separate product responsibilities. Hosted acceptance remains open until actual pinned gateway/engine deployments are recorded and two hosted organizations pass isolation, protected chat and streaming. Deployment and rollback requirements remain canonical in [HOSTING.md](HOSTING.md#shared-community-core-releases).
