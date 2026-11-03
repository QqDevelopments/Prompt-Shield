# Community Manual Browser Test Matrix

This is the canonical checklist for Community manual acceptance. Use the [Turkish guide status](MANUAL_TEST_GUIDE_TR.md) and [synthetic examples](MANUAL_TEST_EXAMPLES.md). Executed observations and their browser/environment limits are recorded below.

## Status Legend

| Status | Meaning |
| --- | --- |
| 🟢 | Manual test passed (`PASS`); all stated checks have a run/evidence record. |
| ⚫ | Not applicable (`N/A`) in the recorded environment; include the reason. |
| ⚪ | Not yet tested (`NOT RUN`); no manual evidence for this boundary/revision. |
| 🟡 | Manual test scheduled or in progress (`IN PROGRESS`); acceptance is incomplete. |
| 🔴 | Manual test failed (`FAIL`); record reproduction and redacted evidence. |
| 🟠 | Test blocked (`BLOCKED`) by a named missing prerequisite. |

Local covers the development/loopback environment. Hosted covers the deployed HTTPS frontend/API/identity path and its real ingress, infrastructure and external integrations; a local pass does not accept those boundaries.

Local and hosted statuses are independent and belong to the identified revision, browser and environment in the [run record](#run-record). Automated test links describe coverage; they do not mark manual acceptance passed. A skipped test is untested. Mark a multi-check row 🟢 only after all its stated checks were observed; otherwise record the completed subset. Missing inputs block only their corresponding branches.

User-delegated technical acceptance can establish the recorded local API/process boundary through actual requests, dependency faults and recovery. It does not establish Chrome/Edge/Safari browser acceptance or hosted acceptance. Passing the automated suite alone remains automated evidence.

Chrome/Edge local acceptance is complete across the recorded runs: the [native completion run](#native-chrome-and-edge-completion-2026-10-29) closes CE-05 and CE-07 in the actual installed browsers. The other seven scenarios retain their earlier user acceptance; this is not a fresh rerun of all nine scenarios. Safari still needs macOS/Apple hardware. C26 local operations passed. C27 local quality, cancellation, resource and sustained-load checks passed; the user accepted C27 and set short-NER P99 <50 ms as an advisory optimization goal, not a mandatory release or acceptance gate. The recorded 40.4–49.3 ms local baseline meets this goal in its tested environment. The user confirmed no hosted backend/proxy/APM/backup infrastructure exists; hosted acceptance remains separate.

An [independent UI recheck](#independent-ui-recheck-2026-10-23) also covers the previously user-accepted scenarios. Its unrepeatable environment branches are explicit; the original user acceptance and browser cells remain unchanged.

## Browser Compatibility

| Feature | Local | Hosted | Automated Test |
| --- | :---: | :---: | --- |
| Chrome on Windows | 🟢 · 9/9 across recorded runs | ⚪ | [Native CE-05/CE-07 acceptance](#native-chrome-and-edge-completion-2026-10-29); automated coverage: [Chromium public flow](../apps/web-console/e2e/public.spec.ts), [workspace flow](../apps/web-console/e2e/workspace.spec.ts) |
| Edge on Windows | 🟢 · 9/9 across recorded runs | ⚪ | [Installed headed Edge CE-05/CE-07 acceptance](#native-chrome-and-edge-completion-2026-10-29); explicitly authorized Playwright driver with reviewed screenshots |
| Safari on macOS | 🟠 · requires a macOS/Apple-device run | ⚪ | [WebKit public smoke](../apps/web-console/e2e/smoke.spec.ts); does not establish Safari on an Apple device |

Record browser/OS versions and evidence references in each run. Firefox and WebKit smoke projects cover public layout, preferences and keyboard entry at 1440 and 390 px. Execution results belong to [current verification status](IMPLEMENTATION_STATUS.md), not these manual cells. A local Chromium pass does not establish hosted cookies, Chrome/Edge manual acceptance or Safari on an Apple device.

## Public Interface

| Feature | Local | Hosted | Automated Test |
| --- | :---: | :---: | --- |
| **C01 — Public entry** · Public entry, keyboard navigation, narrow layout, visible errors · Manual: [CE-01](#executed-run-record-2026-10-22) | 🟢 | ⚪ | visual/accessibility acceptance remains manual · Browser: [Public browser flow](../apps/web-console/e2e/public.spec.ts); [Cross-browser smoke](../apps/web-console/e2e/smoke.spec.ts) |
| **C02 — Preferences** · Language, light/dark/system appearance, reload, unavailable storage · Manual: CE-01 | 🟢 | ⚪ | Browser: [Workspace/browser preferences](../apps/web-console/e2e/workspace.spec.ts); [Cross-browser smoke](../apps/web-console/e2e/smoke.spec.ts) |

## Inspection and Blocking

| Feature | Local | Hosted | Automated Test |
| --- | :---: | :---: | --- |
| **C03 — Inspection** · Clean text and supported email/card/IBAN/national-ID masking · Manual: [CE-02](#executed-run-record-2026-10-22) | 🟢 | ⚪ | [Public gRPC rules](../services/engine/tests/rules.rs), [service](../services/engine/tests/service.rs) |
| **C04 — Inspection** · Repeated entities, Unicode preservation, invalid checksum · Manual: CE-02 | 🟢 | ⚪ | [Public gRPC service](../services/engine/tests/service.rs) |
| **C05 — Blocking** · Critical secrets and known override phrases block · Manual: [CE-03](#executed-run-record-2026-10-22) | 🟢 | ⚪ | [Rules](../services/engine/tests/rules.rs), [HTTP fail-fast](../services/gateway/internal/api/server_test.go) |
| **C06 — Fail-closed dispatch** · Blocked/inspection-failed/audit-failed traffic never reaches upstream · Manual: CE-03, [CE-11 technical](VERIFICATION.md#failure-and-model-acceptance) | 🟢 | ⚪ | [HTTP dispatch](../services/gateway/internal/api/server_test.go), [audit failure](../services/gateway/internal/api/lifecycle_test.go) |

## Providers and Protected Chat

| Feature | Local | Hosted | Automated Test |
| --- | :---: | :---: | --- |
| **C07 — Provider models** · Provider discovery, refresh, selection, empty/401/403 outcomes · Manual: [CE-04](#executed-run-record-2026-10-22) | 🟢 | ⚪ | [HTTP discovery](../services/gateway/internal/api/models_test.go); discovery routes are mocked in browser tests · Browser: [Model browser flows](../apps/web-console/e2e/models.spec.ts) |
| **C08 — Provider credentials** · Key reload/forget/provider-switch, memory fallback, pending discovery cancellation · Manual: CE-04 | 🟢 | ⚪ | Browser: [Public session lifecycle](../apps/web-console/e2e/public.spec.ts), [late discovery](../apps/web-console/e2e/models.spec.ts) |
| **C09 — Protected chat** · Public chat, missing-key denial, provider credential isolation · Manual: [CE-05 run](#executed-run-record-2026-10-23) | 🟢 · controlled upstream / in-app browser | ⚪ | [Public HTTP contracts](../services/gateway/internal/api/public_test.go); live provider acceptance remains manual |
| **C10 — Request lifecycle** · Per-request restoration, clear/reload conversation, cancellation recovery · Manual: [CE-05 run](#executed-run-record-2026-10-23), [CE-09 technical](VERIFICATION.md#provider-boundary-acceptance)  · [Technical run](#technical-acceptance-run-2026-10-23) | 🟢 · local controlled fixtures | ⚪ | [Proxy restoration](../services/gateway/internal/api/server_test.go), [cancellation](../services/gateway/internal/api/lifecycle_test.go) · Browser: [public browser](../apps/web-console/e2e/public.spec.ts) |

## Operator Workspace

| Feature | Local | Hosted | Automated Test |
| --- | :---: | :---: | --- |
| **C11 — Operator session** · Operator login/logout, protected reads, Origin and CSRF · Manual: [CE-06](#executed-run-record-2026-10-22) | 🟢 | ⚪ | [HTTP authentication](../services/gateway/internal/api/server_test.go) · Browser: [real local workspace](../apps/web-console/e2e/workspace.spec.ts) |
| **C12 — Overview** · Overview readiness and bounded latest-event counts · Manual: CE-06 | 🟢 | ⚪ | [Gateway handlers](../services/gateway/internal/api/server_test.go); full displayed-data check remains manual |
| **C13 — Policies** · Mask/keyword policy behavior and stale-version conflict · Manual: [CE-07 run](#executed-run-record-2026-10-23) | 🟢 · in-app browser | ⚪ | [HTTP conflict](../services/gateway/internal/api/lifecycle_test.go) · Browser: [Real workspace](../apps/web-console/e2e/workspace.spec.ts) |
| **C14 — Contextual NER** · Contextual NER, independent setting, unavailable required model · Manual: [CE-07 run](#executed-run-record-2026-10-23), [CE-11 technical](VERIFICATION.md#failure-and-model-acceptance)  · [Technical run](#technical-acceptance-run-2026-10-23) | 🟢 · pinned model and failure/recovery | ⚪ | [Explicit NER tests](../services/engine/tests/ner.rs); model-backed ignored tests require explicit execution |
| **C15 — Application keys** · One-time application secret, authenticated inspection, revocation · Manual: [CE-08](#executed-run-record-2026-10-22) | 🟢 | ⚪ | [HTTP key lifecycle](../services/gateway/internal/api/server_test.go) · Browser: [real workspace](../apps/web-console/e2e/workspace.spec.ts) |

## API and Streaming

| Feature | Local | Hosted | Automated Test |
| --- | :---: | :---: | --- |
| **C16 — Provider API** · OpenAI/Anthropic JSON contracts and separate credential headers · Manual: [CE-09 technical](VERIFICATION.md#provider-boundary-acceptance)  · [Technical run](#technical-acceptance-run-2026-10-23) | 🟢 · controlled OpenAI/Anthropic contracts | ⚪ | [SDK boundary](../services/gateway/internal/api/sdk_test.go), [header contracts](../services/gateway/internal/api/lifecycle_test.go); SDK test is opt-in |
| **C17 — Streaming** · Split-placeholder SSE, explicit completion, abrupt failure, cancellation · Manual: [CE-09 technical](VERIFICATION.md#provider-boundary-acceptance)  · [Technical run](#technical-acceptance-run-2026-10-23) | 🟢 · controlled SSE and cancellation | ⚪ | [Streaming HTTP](../services/gateway/internal/api/stream_test.go), [public stream](../services/gateway/internal/api/public_test.go) |
| **C18 — Embeddings** · Embeddings sanitize input and preserve vector response · Manual: [CE-09 technical](VERIFICATION.md#provider-boundary-acceptance)  · [Technical run](#technical-acceptance-run-2026-10-23) | 🟢 · controlled embedding fixture | ⚪ | [SDK embeddings](../services/gateway/internal/api/sdk_test.go), [HTTP proxy](../services/gateway/internal/api/server_test.go) |
| **C19 — Input limits** · Unsupported content, body limits, output-token conflicts · Manual: [CE-09 technical](VERIFICATION.md#provider-boundary-acceptance)  · [Technical run](#technical-acceptance-run-2026-10-23) | 🟢 · real local HTTP boundary | ⚪ | [HTTP proxy](../services/gateway/internal/api/server_test.go), [token limits](../services/gateway/internal/api/token_limits_test.go) |

## Security Events

| Feature | Local | Hosted | Automated Test |
| --- | :---: | :---: | --- |
| **C20 — Events** · Events filter/pagination and metadata-only database/log evidence · Manual: [CE-10](#executed-run-record-2026-10-22) | 🟢 | ⚪ | [Audit storage](../services/gateway/internal/store/postgres_test.go), [HTTP privacy](../services/gateway/internal/api/server_test.go); infrastructure logs/APM remain manual |

## Recovery and Persistence

| Feature | Local | Hosted | Automated Test |
| --- | :---: | :---: | --- |
| **C21 — Recovery** · Engine/storage/cache/provider failure, fail-closed response, real recovery · Manual: [CE-11 technical](VERIFICATION.md#failure-and-model-acceptance)  · [Technical run](#technical-acceptance-run-2026-10-23) | 🟢 · isolated real-service faults | ⚪ | [Failure handlers](../services/gateway/internal/api/lifecycle_test.go), [busy/unavailable mapping](../services/gateway/internal/api/server_test.go) |
| **C22 — Persistence** · Persistent restart, session invalidation, separate ephemeral behavior · Manual: [CE-12 technical](VERIFICATION.md#persistence-and-operations-acceptance)  · [Technical run](#technical-acceptance-run-2026-10-23) | 🟢 · persistent/ephemeral process restart | ⚪ | [Store integration](../services/gateway/internal/store/postgres_test.go); actual process restart remains manual |

## Hosted Access and Admission

| Feature | Local | Hosted | Automated Test |
| --- | :---: | :---: | --- |
| **C23 — Hosted session** · HTTPS origins, secure cookies, preflight, direct-route refresh, streaming ingress · Manual: [CE-13 technical](VERIFICATION.md#hosted-and-performance-acceptance) | ⚫ · local HTTP fixture | ⚪ | [CORS HTTP backstop](../services/gateway/internal/api/public_test.go); real ingress/Vercel acceptance remains manual |
| **C24 — Public admission** · Hosted five-request UTC quota; no downstream work after rejection · Manual: [CE-13 technical](VERIFICATION.md#hosted-and-performance-acceptance) | ⚫ · development exempt | ⚪ | [Daily admission HTTP](../services/gateway/internal/api/public_test.go), [environment boundary](../services/gateway/cmd/gateway/main_test.go) |
| **C25 — Ingress admission** · Ingress rejects excess public traffic/preflight before gateway · Manual: [CE-13 technical](VERIFICATION.md#hosted-and-performance-acceptance) | ⚫ · no hosted ingress | ⚪ | No deployed-ingress evidence supplied by repository tests |

## Operations and Performance

| Feature | Local | Hosted | Automated Test |
| --- | :---: | :---: | --- |
| **C26 — Operations** · Encrypted backup/restore, retention, rollback, infrastructure redaction · Manual: [CE-12 technical](VERIFICATION.md#persistence-and-operations-acceptance), [CE-13 technical](VERIFICATION.md#hosted-and-performance-acceptance)  · [Technical run](#technical-acceptance-run-2026-10-23)  · [Operations run](#c26-local-operations-acceptance-2026-10-23) | 🟢 · local operations; hosted infrastructure absent | ⚪ | [Operator procedure](OPERATIONS.md#upgrade-backup-and-recovery); target operations acceptance remains manual |
| **C27 — Performance** · NER quality, native cancellation/drain, sustained load/resource target · Manual: [CE-13 technical](VERIFICATION.md#hosted-and-performance-acceptance)  · [Technical run](#technical-acceptance-run-2026-10-23)  · [Performance run](#c27-local-model-and-performance-acceptance-2026-10-23) | 🟢 · user accepted; latency optimization target non-blocking | ⚪ | [Explicit performance/model commands](VERIFICATION.md), [pool boundary](../services/engine/tests/pool.rs); browser workflows are insufficient |

## Gemini acceptance — C28

| Feature | Local | Hosted | Automated Test |
| --- | :---: | :---: | --- |
| **C28 — Google Gemini** · AI Studio key/model discovery, protected JSON/SSE chat, restoration, cancellation, provider switch and Forget · Acceptance: [real Google run](evidence/live-google-gemini-20261026.json) | 🟢 · local UI/core and real Google | ⚪ | [HTTP Gemini contracts](../services/gateway/internal/api/gemini_test.go), [model discovery](../services/gateway/internal/api/models_test.go), [SDK boundary](../services/gateway/internal/api/sdk_test.go), [browser flow](../apps/web-console/e2e/models.spec.ts) |

The user delegated the pending UI scenario to Codex. On 2026-10-25, its local branch passed through installed Chrome and Edge channels in isolated headless browser sessions, using the actual frontend, gateway, rules engine and controlled provider without HTTP route mocks. Model discovery required an explicit selection; P11 restored the synthetic email, P07 was blocked before provider dispatch, and P02 was masked. Refresh/reload, provider-switch clearing and Forget/reload passed. The [UI acceptance record](evidence/local-gemini-ui-acceptance-20261025.json) records the exercised boundaries and cleanup. The existing C01-C27 results retain their recorded provider/revision scope; the [earlier automated record](evidence/local-gemini-integration-20261025.json) separately covers JSON/SSE, cancellation, SDK and dependency checks.

On 2026-10-26, the user supplied an AI Studio key and the remaining real Google branch passed through the official endpoint. Discovery returned 60 models; `models/gemini-flash-lite-latest` completed protected JSON and SSE chat with P11 restored, no leaked placeholder and explicit stream completion. P07 returned the safe security block, and Forget/clear/reload removed the provider session. The listed `gemini-2.5-flash-lite` identifier returned 404 both through Prompt Shield and directly from Google; the discovered latest alias succeeded with the same key. The [real-provider record](evidence/live-google-gemini-20261026.json) preserves this model-specific limit. This is local application acceptance against the real provider, not hosted ingress/deployment acceptance. Completed Gemini instructions were removed from the temporary guide.

### Hosted partial observation (2026-11-03)

In a user-delegated Chrome session on Windows, the production HTTPS Community frontend and API were exercised with a user-supplied Gemini key and the rules-only engine. The browser showed masked email and card detections for the synthetic inspection example. A protected chat using the discovered `models/gemini-flash-lite-latest` returned the synthetic email in the response. The UI returned to an empty provider-key entry after Forget. A request using `models/gemini-2.5-flash-lite` showed a provider-rejection message; its upstream status and cause were not captured. The deployed revision and exact browser version were not independently recorded for this observation.

The daily per-IP allowance was exhausted during the run. A subsequent security test displayed a generic error, and an independent preflight returned HTTP 429. Security blocking, cancellation and recovery, key behavior after reload, and the full hosted Gemini acceptance remain pending. This partial observation does not mark any hosted manual acceptance row as passed.

Later on 2026-11-03, the user delegated another hosted check in Chrome on Windows. The public Community page loaded, switching English to Turkish changed the route and visible language, and reloading preserved the Turkish page. The original English selection was restored. From the same network, API readiness returned HTTP 200 and an inspection preflight returned HTTP 429 because the daily allowance was exhausted. The browser version and deployed revisions were not captured; theme, keyboard/narrow layout, unavailable storage, and remaining acceptance branches were not tested in this check.

## Automated execution boundaries

- Run frontend commands from `apps/web-console`: `npm run verify`, `npm run lint`, and `npm run test:e2e`. They are separate checks. `verify` does not run Playwright or lint.
- Default Playwright uses a real local rules engine/gateway for workspace/inspection, with selected discovery/provider routes mocked. It does not accept HTTPS staging, live provider compatibility, production databases, or Safari on an Apple device. Firefox/WebKit execute the public smoke only. External-URL runs have their own environment scope.
- Go storage/cache, real-engine, and SDK integrations require their explicit fixture settings. Missing settings can cause skips. Use the mandatory integration runner in [VERIFICATION.md](VERIFICATION.md#clean-install-integration-and-ci) for release evidence rather than interpreting an ordinary Go test exit code as complete integration coverage.
- Model/pool tests and load benchmarks have explicit acceptance commands and prerequisites in [VERIFICATION.md](VERIFICATION.md). Do not infer a measured capacity or P99 result from passing functional tests.

## Real OpenAI prerequisite check — 2026-10-28

The shared run evidence (`docs/evidence/live-openai-20261028.json` in `prompt-shield-enterprise`) records a user-supplied replacement credential against the official OpenAI endpoints. Direct model discovery and repeated Community public model discovery passed with 128 models; the initial gateway discovery attempt returned a transient 502. Actual text generation returned provider HTTP 429 with `credit_balance_exhausted` / `insufficient_quota`. Community JSON/SSE requests returned the existing safe 502 `upstream_rejected` envelope. Successful live OpenAI generation, restoration and streaming remain unaccepted until API credit/quota is available.

The actual rules-only Rust engine and gateway ran locally with no configured server provider key; the credential was request-scoped and was not saved to code or env files. Existing controlled-provider green cells retain their original scope. No production code, hosted acceptance, NER quality or performance result changed. Temporary processes/credentials were cleaned up; the shared record also covers the Enterprise session/application-key observations.

## Run record

Copy this record into the acceptance issue or a private evidence file. Keep secrets and raw prompt captures out of the repository.

```text
Run ID / tester / date:
Repository revision:
Local or hosted; frontend/API origins:
OS; browser/version; viewport:
Storage mode; rules/NER mode; model identity:
Provider/model or controlled upstream; dependency fixtures:
Matrix row IDs and individual observations:
Status per row; blocked prerequisite or N/A reason:
Expected / actual result; exact reproduction:
Redacted screenshot or metadata evidence reference:
Cleanup and settings restored:
```

### Executed Run Record (2026-10-22)

```text
Run ID / tester / date: RUN-20261022-COMMUNITY-01 / qa / 2026-10-22
Repository revision: main (working tree local dev)
Local or hosted; frontend/API origins: Local (http://127.0.0.1:5173 / http://127.0.0.1:18081; API :8080)
OS; browser/version; viewport: Windows 11; Chrome & Edge; 1440px & 390px
Storage mode; rules/NER mode; model identity: Storage enabled/disabled; rules-only (deterministic); provider-discovery
Provider/model or controlled upstream; dependency fixtures: Synthetic provider / OpenAI & Anthropic discovery; local gateway/engine
Matrix row IDs and individual observations:
  - C01 (CE-01): Public entry, keyboard/Tab navigation, 390px narrow layout, 200% zoom, storage-off fallback observed and passed.
  - C02 (CE-01): Language toggle (TR/EN), light/dark/system themes, reload persistence observed and passed.
  - C03 (CE-02): Clean text (P01) unaltered; P02-P05 email/card/IBAN/national ID masking with entity counts and latency badges observed and passed.
  - C04 (CE-02): Invalid card checksum (P06) rejected as non-card; repeated emails & unicode/emojis (P09) preserved; Clear conversation observed and passed.
  - C05 (CE-03): Critical secrets & override phrases (P07, P08, P10) blocked with explicit fail-closed message observed and passed.
  - C06 (CE-03 UI): Blocked message correctly displayed in UI; CE-11 technical dispatch verification pending.
  - C07 (CE-04): Provider selection, model list refresh, model selection and error/empty states observed and passed.
  - C08 (CE-04): Session credential lifecycle, reload retention, Forget key, and provider switch memory clearing observed and passed.
  - C11 (CE-06 UI): Operator login with password, invalid password error, protected route navigation (Overview, Playground, Policies, API keys, Events, Integration, Preferences), and sign out observed and passed.
  - C12 (CE-06): Overview readiness and bounded latest-events display observed and passed.
  - C15 (CE-08 UI): One-time application secret creation, hidden after dismissal/reload, metadata change on revocation observed and passed.
  - C20 (CE-10 UI): Event filtering (allowed/masked/blocked/failed), pagination, and absence of raw prompt/credentials/mappings observed and passed.
Status per row; blocked prerequisite or N/A reason:
  - C01: 🟢 PASS
  - C02: 🟢 PASS
  - C03: 🟢 PASS
  - C04: 🟢 PASS
  - C05: 🟢 PASS
  - C06: 🟢 PASS
  - C07: 🟢 PASS
  - C08: 🟢 PASS
  - C09-C10: 🟡 IN PROGRESS (Scenario 1 pending)
  - C11: 🟢 PASS (UI login/navigation passed)
  - C12: 🟢 PASS
  - C13-C14: 🟡 IN PROGRESS (Scenario 2 pending)
  - C15: 🟢 PASS
  - C20: 🟢 PASS
Expected / actual result; exact reproduction: Observed UI behaviors matched all CE-01, CE-02, CE-03, CE-04, CE-06, CE-08, CE-10 criteria.
Redacted screenshot or metadata evidence reference: Local development manual session evidence verified.
Cleanup and settings restored: Temporary keys forgotten, operator session signed out, test conversation cleared.
```

### Executed Run Record (2026-10-23)

```text
Run ID / tester / date: RUN-20261023-COMMUNITY-02 / Codex, delegated interactive UI verification / 2026-10-23
Repository revision: 4e6b3643e6f1cac08cad922dff11021f36cc3993 + working-tree NER label fix
Local or hosted; frontend/API origins: Local; same-origin http://127.0.0.1:18081 (rules) and :18082 (hybrid)
OS; browser/version; viewport: Windows; Codex in-app browser; version/viewport not captured
Storage mode; rules/NER mode; model identity: Isolated ephemeral gateways built from this checkout; existing rules engine and separate hybrid engine
NER: Xenova/bert-base-NER, revision 24c7e5aba9ae350923357a6f0b92571be34037ec; one session / four intra-op threads
Verified model SHA-256: caaee70a5518ec7f9e46e5308fcc9263a8c227703a9ce46cf61c69a552349648
Verified tokenizer SHA-256: 343989712a36cd8b253efeaf8baf6a08b9d2583f78e395e83832e8ee9f8d8ee1
Provider/model or controlled upstream; dependency fixtures: Repository local-provider.ts / fixture; synthetic credentials only
Matrix row IDs and individual observations:
  - C09 (CE-05): P11 completed through the real gateway/engine/controlled provider. The assistant displayed the restored synthetic email. A follow-up completed. Forget key disabled chat with a nonempty prompt; a fresh tab had no provider key while the original tab held one.
  - C10 (CE-05 UI): Clear conversation followed by reload left no history. P07 displayed the blocked message. Cancel during a delayed P11 request restored the controls; subsequent P01 completed with only its own user/assistant pair and Received nothing, without the cancelled email reply appearing later.
  - Controlled-provider metadata after these chat probes: requests=5, placeholders=4, exposed_synthetic_email=0. No production vendor was contacted. These counters are privacy/dispatch evidence, not capacity measurements.
  - C13 (CE-07): Disabling Mask personal information made P02 unchanged; restoring it masked the email again. The inspection lab masked a synthetic valid card. Saving synthetic-deny-phrase blocked P13; removing it allowed P13 again.
  - C13 conflict: Two separately loaded editor tabs held the same old policy. The first saved masking off; the second received the policy-conflict message. Reload confirmed masking remained off and the rejected keyword was absent. These tabs shared an operator session; separate Chrome/Edge profiles were not exercised.
  - C14 (CE-07 UI): Rules-only UI disabled Contextual detection. With the pinned real model ready, P12 masked the person, organization and location. With Mask personal information off and contextual detection on, P12 still masked all three. With contextual detection off, P12 was unchanged.
  - C14 UI defect and retest: Missing entity.PERSON, entity.ORGANIZATION and entity.LOCATION English labels initially rendered generic error text. Added the English labels and pending translation keys; rebuilt and repeated P12, which displayed Person / Organization / Location and three detections correctly. Turkish translations remain pending.
Status per row; blocked prerequisite or N/A reason:
  - C09: PASS for the recorded controlled-upstream/in-app-browser scope.
  - C10: UI PASS; complete CE-09 technical acceptance remained pending at this run (superseded by the technical run below).
  - C13: PASS for policy behavior and stale-version conflict in the recorded editor-tab scope.
  - C14: UI/model-toggle PASS; required-model failure/recovery remained pending at this run (superseded below).
  - Chrome and Edge: Not run by this agent; preserve the preceding 7/9 browser acceptance records.
  - Hosted, live vendor compatibility, Safari, model-quality benchmarks and latency/capacity acceptance: Not run.
Expected / actual result: Both pending guide scenarios passed their interactive UI steps after the label correction.
Evidence reference: Interactive browser observations and controlled-provider metadata in this Codex run; no screenshots or private prompts stored in the repository.
Cleanup and settings restored: Baseline mask_pii=true, contextual_pii=false, empty blocked keywords restored; synthetic keys forgotten and conversations cleared; operator sessions signed out; only this run's temporary services removed.
```

### Technical acceptance run (2026-10-23)

The user delegated the remaining local technical checks to Codex. This record supersedes the C10/C14 pending notes in the preceding UI run for the local boundary only. Structured, metadata-only results are retained in [the evidence file](evidence/local-technical-acceptance-20261023.json).

```text
Run ID / tester / date: RUN-20261023-COMMUNITY-TECH-03 / Codex, user-delegated technical acceptance / 2026-10-23
Repository revision: 19c9b9090b9ecd5a06ec5b02fcf3e2cdf7e70036; clean at test start
Environment: Windows; actual gateway/engine/PostgreSQL/Redis container processes on isolated networks; loopback gateway ports 18083-18085 and controlled upstream 18090
Build identity: Gateway/admin compiled from this checkout; engine Dockerfile rebuilt from this checkout; no production source/configuration/default changes
Model: Pinned Xenova/bert-base-NER INT8 + tokenizer; SHA-256 values in the evidence; one session / four intra-op threads; English only
Provider: Controlled local OpenAI/Anthropic-compatible upstream; synthetic credentials/text only; no vendor account contacted
Automated corroboration: Mandatory integration runner passed all four required database/cache/engine/pinned-SDK boundaries, with zero skips; 46 top-level Go tests / 107 passing results including subtests
Explicit NER/pool tests: 6 passed, 0 failed, 0 ignored; model-backed fixtures, whole-word/Unicode/token boundaries, independent detection, queued cancellation/recovery/isolation and configuration budget
Direct process observations:
  - C10: Repeated placeholders restored twice in the response; disconnecting a real streaming client closed the upstream socket; a fresh request restored only its own different email.
  - C14: With contextual policy still enabled, replacing the hybrid engine with a rules-only engine caused 503 inspection_unavailable and zero provider dispatch. Replacing it with the hybrid engine restored success. A separate NER-enabled process with missing model files exited with code 1. Baseline contextual policy was restored afterward.
  - C16: Both JSON provider contracts restored repeated originals. The controlled provider verified its expected Authorization/x-api-key credential independently from the Prompt Shield application key and observed no raw synthetic email.
  - C17: Both provider streams restored a placeholder split inside its identifier and emitted explicit completion. Abrupt closure emitted upstream_stream_failed without false completion or leaking a partial placeholder. Client disconnect and fresh-request recovery passed at the actual socket boundary.
  - C18: Embedding input was sanitized; the returned [0.25, 0.75] vector remained unchanged.
  - C19: Multimodal content, body above 1 MiB, conflicting output-token fields and a critical secret returned HTTP 400 with zero provider dispatch. The mandatory suite separately exercised fail-closed audit/inspection and the wider limit/header contracts.
  - C21: Stopping engine, PostgreSQL and Redis individually produced safe 503 inspection_unavailable, 401 unauthorized and 503 rate_limit_unavailable respectively; all had zero provider dispatch. Restoring each process restored chat. Provider disconnect returned safe 502 and a fresh request then succeeded.
  - C22: Restarting the persistent gateway retained policy, one key and seven metadata events while invalidating the old operator session (401). Actual persistent PostgreSQL/Redis restart retained state. A separate ephemeral gateway lost its key/policy on restart; old session/key both returned 401.
  - C26 local subset: Production prune removed exactly one synthetic event older than 30 days. AES-256-GCM encrypted a disposable pg_dump; decryption and pg_restore into a separate database preserved policy, one key and nine events. The restored gateway accepted the original application key. Gateway logs/audit rows contained none of the tested synthetic originals, provider/application secrets or placeholder mappings.
Acceptance: C10, C14, C16-C19, C21 and C22 local cells PASS for these recorded boundaries. C26 remains partial; matching-version restore is not cross-version rollback or hosted backup-access acceptance. C27 functional model/pool checks passed but corpus quality, sustained load/resource limits and P99/capacity acceptance were not rerun.
Browser scope: Chrome/Edge remaining scenarios were not run in those browsers because only the in-app browser was connected. Their earlier 7/9 records remain. Safari is blocked by missing macOS/Apple hardware.
Hosted scope: No HTTPS/ingress deployment, real vendor model, backup-access policy or proxy/APM infrastructure acceptance. C23-C25 local N/A and all hosted cells remain unchanged.
Cleanup: All temporary acceptance containers/networks/volumes and integration dependencies removed. Original Community services preserved. The encrypted disposable backup has no retained decryption key and is not an operational backup artifact.
```

### C26 local operations acceptance (2026-10-23)

The user confirmed that no deployed backend, proxy/APM or backup infrastructure exists and requested completion of the local scope. Deployment access policies and proxy/APM checks therefore have no target in this run; no hosted cell is accepted. [Structured evidence](evidence/local-operations-acceptance-20261023.json) supplements the preceding retention/restore record.

- Built and ran the actual older gateway revision `4c06aebf2beaf0adcc9ff9a9a2494c8456c0b657` in a disposable PostgreSQL/Redis environment. Created a policy, key and audit event; encrypted the pre-upgrade backup with AES-256-GCM. An incorrect key failed authentication.
- Stopped the old gateway, ran current revision `19c9b9090b9ecd5a06ec5b02fcf3e2cdf7e70036` against that database, verified the retained key/data, and saved a distinct post-upgrade policy.
- Stopped the current gateway, decrypted/restored the pre-upgrade backup into a separate database and ran the older gateway against it. The exact original policy, one key and one event were restored. The original key authenticated; the old process's session returned 401. This exercises cross-version application rollback through a matching backup, without downgrade migrations.
- Captured both stdout and stderr from all three gateway processes (204 bytes) and nonempty audit metadata (588 bytes). No tested synthetic original, provider/application secret or placeholder mapping appeared. The preceding real prune test already verified removal of exactly one expired event.
- Removed only this run's disposable containers/networks/volumes. No primary service, production defaults, branch, commit or push was changed. Backup keys were transient and not retained; the encrypted fixture files are test artifacts rather than usable operational backups.

C26 local status: **PASS** for this tested source pair and the user-selected local scope. Hosted deployment acceptance remains **NOT RUN** until infrastructure exists; a different version pair needs its own rollback evidence.

### C27 local model and performance acceptance (2026-10-23)

Run `RUN-20261023-C27-LOCAL-05` / Codex, user-delegated local verification / source `19c9b9090b9ecd5a06ec5b02fcf3e2cdf7e70036` completed the pinned 2,000-example English corpus comparison, three sustained default-setting baseline repetitions, 128/512-token requests, deliberate cancellation, a 15-minute mixed soak and a separate native cancellation/drain observer. Per-condition measurements and their scope are canonical in [the new local reference](PERFORMANCE_RESULTS.md#2026-10-23-local-default-configuration-reference); [structured evidence](evidence/local-model-performance-acceptance-20261023.json) retains fingerprints, outcomes and resources.

C27 local status: **PASS, accepted by the user on 2026-10-23**. The 2,000-example English quality comparison, 20 native cancellation/recovery rounds, resource checks and 15-minute mixed soak passed without unexpected functional errors. The user set **short-NER P99 <50 ms as an advisory optimization goal**; exceeding it alone must not fail release, acceptance or model/default promotion. The recorded **40.4–49.3 ms P99** meets the current goal for this workload and environment. This decision supersedes the earlier red C27 classification and the former 6 ms target. [The goal decision record](evidence/performance-goal-20261023.json) evaluates existing measurements; historical evidence retains the original target/gate flags. Required quality, safety, recovery and resource checks still apply. Hosted/real-vendor and HTTP/SSE sustained-capacity acceptance were outside this local engine reference. Primary service configuration and production model/session/thread defaults were unchanged.

### Independent UI recheck (2026-10-23)

Run `RUN-20261023-COMMUNITY-RECHECK-06` / Codex, at the user's request, compared acceptance commit `ce41a20f2444fb6cd1b2f6e0ac076969de3a34aa` with this conversation's prior evidence. That commit accepted C01-C08 and C11-C12. Their remaining UI steps were independently exercised on current source `19c9b9090b9ecd5a06ec5b02fcf3e2cdf7e70036`; C15 and C20 from the later user record were also rechecked. [Structured evidence](evidence/local-ui-recheck-20261023.json) distinguishes each exercised boundary from untested branches.

- Environment: Windows, Codex in-app browser at 1280x900 and 390x844; fresh frontend build, actual isolated rules engine/gateway, ephemeral store and synthetic OpenAI/Anthropic discovery provider at loopback `:18085`/`:18090`. No real provider or hosted service was contacted. Browser version was not captured.
- C01/C02: Keyboard traversal and visible focus, English/Turkish, light/dark/system selection, preference reload and narrow layout passed. Long masked output wrapped without horizontal overflow at 390px. Confirmed 200% browser zoom, Windows theme changes and a storage-disabled browser profile were **not repeated** with the available controls.
- C03-C05: P01-P10 inspection examples passed their applicable masking, invalid-checksum, repeated-entity, Unicode and blocking checks. Clearing removed the result; operator P02 masking also passed. C06's visible block passed; its dispatch/audit boundaries remain established by the preceding technical evidence.
- C07/C08: Both provider lists, refresh/selection, distinct 401/403/empty outcomes, same-tab key reload, provider-switch clearing and Forget passed. A loading discovery request followed by Forget did not repopulate models after its delayed response window. Actual browser storage-disabled fallback was **not repeated**.
- C11/C12: Invalid/correct login, all seven workspace screens, protected reload and sign-out/direct-route denial passed. With 113 events, Overview explicitly counted the latest 100 (93 allowed, 3 masked, 3 blocked, 1 failed) and reported the recovered engine connected. This UI run does not newly establish Origin/CSRF or HTTPS-cookie acceptance.
- C15: A disposable secret appeared once, disappeared after dismissal/reload and left metadata only. Integration distinguished application credentials from server provider configuration. Revocation changed Active to Revoked. The UI-created key was not separately used for an API request in this run.
- C20: Real requests generated 112 events, including an actual engine-stop failure/recovery, alongside one operator event. Pages contained 50/50/13 records. Masked/blocked/failed filters returned 4/3/1 rows; allowed returned a full first page. Filter changes reset pagination and removed previous results. No tested raw values appeared in UI/HTTP metadata. Two subsequent public inspections raised the final metadata count to 115; all pages remained free of the tested originals/secrets/mappings.
- Corroboration: `npm run build` passed; the targeted preferences/provider-session suite passed 2 files / 6 tests, including controlled storage fallback. Those tests do not replace the unperformed browser/OS branches.
- Cleanup: Both disposable application keys were revoked, baseline policy verified, provider key/conversation cleared, operator signed out and temporary tabs/viewport/provider/containers/network cleaned. The four original Community containers and production settings were preserved.

No new product failure was observed in the exercised steps and no production source changed. The original user-owned green cells remain backed by that user's run; this partial environment recheck does not expand Chrome/Edge/Safari or hosted acceptance. This UI recheck did not retest C27; its subsequent user acceptance is recorded in the C27 section above.

## Native Chrome and Edge Completion (2026-10-29)

The user delegated the remaining CE-05/CE-07 browser acceptance. The [run record](evidence/community-native-browser-20261029.json) identifies the current working tree, model hashes, tools, observations and cleanup. Chrome 156.0.8078.49 used the connected installed browser and interactive CUA controls. When Edge's native input failed, the user explicitly authorized a separate visible Playwright session; the installed `msedge` channel 156.0.4314.50 ran without route mocks, and its chat/NER screenshots were reviewed. Both used Windows 11 Education 10.0.26300, actual local Community gateways, the actual Rust rules/English INT8 NER engine, and the repository's synthetic provider. No external provider credential or hosted service was used.

- CE-05 / C09-C10: Restored email and follow-up chat, tab-local credential isolation, conversation clear/reload, key reload/Forget denial, security blocking without dispatch, cancellation, successful subsequent chat and absence of a late cancelled email response passed in both browsers.
- CE-07 / C13-C14: Mask off/on behavior, inspection-lab card masking, custom keyword add/remove and stale-editor rejection passed. The pinned model displayed Person, Organization and Location for P12; contextual detection still worked with deterministic masking off, disabling contextual detection left P12 unchanged, and the rules-only installation disabled the contextual control.
- The Edge runner's first attempts exposed a test selector matching both the input and result, then the unchanged public minute limit. The selector was scoped to the rendered result; policy inspections used the authenticated inspection lab and a fresh isolated gateway. The final full run passed all eight grouped checks. Neither issue required a product-source change or disabling admission controls.
- Baseline `mask_pii=true`, `contextual_pii=false` and empty custom keywords were restored. Test conversations/provider sessions were cleared, the hybrid operator signed out, isolated Edge contexts closed, agent-created Chrome tabs closed, and all owned provider/gateway/engine processes stopped. The six test ports were verified closed; no Docker services or customer settings were changed in this run.

Acceptance: Chrome and Edge local compatibility cells are now green: seven previously user-accepted scenarios plus these two newly observed protocols. This does not reaccept all earlier scenarios against the current working tree, establish a performance result, or change Safari, hosted, real-provider or Enterprise acceptance.

## Request queue and configurable IP admission — C29

| Feature | Local | Hosted | Automated Test |
| --- | :---: | :---: | --- |
| **C29 — Protection queue** · Shared FIFO/capacity, real UI progress, cancellation, recovery and independent IP switches · Manual: [new scenario](MANUAL_TEST_GUIDE_TR.md#c29) | ⚪ · new manual browser acceptance pending | ⚪ | [Redis queue](../services/gateway/internal/workqueue/queue_test.go), [two HTTP gateways/progress](../services/gateway/internal/api/progress_test.go), [browser stream](../apps/web-console/e2e/queue.spec.ts), real multi-process/UI pilot in `prompt-shield-enterprise/apps/web-dashboard/e2e/live-local.spec.ts` |

The [2026-10-30 local automated record](evidence/request-queue-20261030.json)
covers real Redis, separate gateway processes, the rules-only Rust engine and
synthetic providers. It does not automatically accept a new manual row or extend
the earlier native Chrome/Edge acceptance to this feature. Hosted ingress, capacity
and cost remain untested; older rows retain their recorded scope.
