# Implementation status and verification

Updated: 2026-10-08. Scope: Community and Enterprise.

This file is the current capability and release-gate summary. Deployment configuration belongs in [HOSTING.md](HOSTING.md), reproducible commands in [VERIFICATION.md](VERIFICATION.md), and measured latency/resource evidence in [PERFORMANCE_RESULTS.md](PERFORMANCE_RESULTS.md).

## Implemented product boundaries

### Community

- Independent React frontend, Go gateway, Rust inspection engine, PostgreSQL audit metadata and Redis admission controls.
- Account-free inspection and BYOK protected chat, a separate operator area, and authenticated integration endpoints.
- Exact Origin/CORS/CSRF boundaries, transient provider credentials, provider-response restoration, metadata-only audit and bounded streaming.
- Public daily admission defaults to five requests/IP/UTC day outside development and can be disabled with `PUBLIC_DAILY_IP_LIMIT_ENABLED`. Community minute/per-IP concurrency controls have an independent switch; global capacity and token quotas remain. The [shared request queue](REQUEST_QUEUE.md) bounds protection work across core replicas and reports real progress over the original POST.
- Contextual NER uses the pinned production model and bounded session pool. Inputs above 512 tokenizer tokens are rejected rather than silently truncated.

### Enterprise

- OIDC authorization code flow with PKCE/state/nonce, verified-email sessions, role and subscription enforcement, invitations, application/SCIM keys and tenant-core routing.
- The public evaluation workflow verifies work-email possession, DNS TXT or fixed-path HTTPS domain control, and a required LinkedIn OIDC account connection before manual review. Applicant PII is encrypted before PostgreSQL storage; email, IP, actor and LinkedIn identifiers use keyed hashes. Submission, secure-link request and token consumption share the default five-actions/IP/UTC-day admission outside development, controlled by `PUBLIC_DAILY_IP_LIMIT_ENABLED`.
- Platform administrators review versioned verification evidence, request information, decide applications and provision organizations separately. Application submission or approval never activates tenant access.
- Tenant roles are enforced on the server. Auditors are restricted to read-only security metadata and signed reports; browser and application-key inspection/chat paths reject them.
- SCIM Users support bounded pagination and equality filtering on stable subject identifiers. Groups support full replacement and bounded incremental membership PATCH.
- Metadata-only events, 7/30-day usage and conservative quota views, cursor-paginated activity, revisioned policy changes with scoped history, signed JSON summaries and browser-printable report views are implemented. Bounded Splunk/Datadog delivery exists; live connector and destination acceptance remains external.

Detailed Enterprise API and onboarding boundaries are maintained in the companion `prompt-shield-enterprise` repository (see `README.md` and `docs/ENTERPRISE_VERIFICATION_AND_ONBOARDING.md`).

## Recorded CI verification

Reviewed through authenticated `gh` on 2026-10-07. These runs verify their own commits; the current working changes were tested locally and have not been pushed or remotely dispatched.

| Repository | Run / revision | Observed result and limits |
| --- | --- | --- |
| Community | [35630478306](https://github.com/QqDevelopments/Prompt-Shield/actions/runs/35630478306), `c213827b9b3d2929a0adced1ab07a8e206abe66d`, 2026-09-21 UTC | 8/8 jobs successful; no skipped jobs. Mandatory integration and explicit NER/pool gates passed. The load job ran about 161 minutes and completed 60/60 full-profile conditions. Artifacts were no longer available through the API, so numerical latency/RSS/quality values were not reconstructed. |
| Enterprise | [35535257784](https://github.com/QqDevelopments/Prompt-Shield-Enterprise/actions/runs/35535257784), `a636e3dd49434616086f7868ace7548dcdad0199`, 2026-09-20 UTC | 2/2 jobs successful. Later intake, identity, policy and reporting revisions need their own release verification. |

At review time, Community HEAD `5379fa19ea0a929460bea163a35d42b76938350d` was 10 commits beyond its run, and Enterprise HEAD `ddb27a11e858493cb03627cbbf7c9bb54adb35b4` was 18 commits beyond its run. That historical review used the former 6 ms P99 target. The current advisory short-NER goal is <50 ms and does not block release or acceptance; see [local measured results](PERFORMANCE_RESULTS.md). The older retained model score file is historical evidence, not a substitute for missing later artifacts.

## Recorded local verification

The 2026-10-07/08 (Istanbul) checks below cover these base HEADs plus the uncommitted Security Green UI, guide, fixture and dependency changes. They do not establish hosted acceptance or user-owned manual PASS.

| Boundary | Result and scope |
| --- | --- |
| Both frontend verification chains | Types, TypeScript automation/test checking, functional tests, build/package checks and dependency scanners passed. Community: 5 script + 7 component tests; Enterprise: 3 script + 5 component tests. |
| Community browser | 13 passed: 11 Chromium behavioral journeys and one smoke each in Firefox/WebKit; public rules inspection/operator use real native services, selected provider discovery is mocked. |
| Enterprise browser | 23 passed; the ordinary run skipped 3 opt-in live cases. Those 3 ran separately and passed against real Keycloak, PostgreSQL, Redis, two Community cores and a synthetic provider. |
| Browser smoke scope | Public entry, 1440/390 px layout, saved appearance and system theme changes. Windows WebKit tabs through form controls; skip-link activation is checked through explicit focus. First-link tab order is exercised in Chromium/Firefox. Safari on an Apple device is still untested. |
| Community integration | Mandatory runner passed all four required PostgreSQL, Redis, real-engine HTTP and pinned OpenAI/Anthropic SDK boundaries; no required boundary was skipped. |
| Community gateway | `go test -race -count=1 ./...` and `go vet ./...` passed with disposable PostgreSQL/Redis; real-engine and SDK requirements were accepted through the mandatory runner. |
| Enterprise gateway | `go test -race -count=1 ./...` and `go vet ./...` passed with disposable PostgreSQL/Redis. |
| Engine | Default rules/public-service tests passed. Release `--features evaluation --test ner --test pool -- --include-ignored --test-threads=1` ran 4 NER and 2 pool tests, all passed. Model/session defaults did not change. |
| Security fix | Both gateways now use `golang.org/x/text v0.41.0` (and required `x/sync v0.22.0`); reachable `GO-2026-6629` no longer appears. Go scanners report zero reachable vulnerabilities. npm reports zero vulnerabilities; Rust reports the existing allowed unmaintained `paste` notice. Non-reachable module advisories remain visible in scanner output. |
| Localization | Community 188 English keys; Enterprise 348. Six new Enterprise keys remain in the pending queue; other locales and the bulk translation command were untouched. |
| UI and print review | 84 real-loopback desktop/mobile light/dark captures covered public inspection, all seven operator views, six tenant views, separate role landings and platform views. Mocked applicant/review views were captured separately. The 75-row browser PDF rendered in four pages with repeated headers; the empty PDF rendered in one page. Sidebar offset and skip-link print leakage were fixed. |
| Hosted frontend packaging | Both `build:hosted` commands passed with isolated staging HTTPS origins. Public configuration contained the expected product/API origin; local builds were restored afterward. No deployment or target-host acceptance occurred. |
| UI fixes | Stale public inspection results clear on a new draft/failing request; aborted responses cannot commit. Public skip links precede branding. Platform management now exposes the existing CSRF-protected logout boundary and clears any transient provider credential. Review evidence and decision controls use responsive columns. |
| Manual guides | Turkish UI scenarios and English technical acceptance are separated. Feature/Shared tests/Browser E2E columns preserve C01–C27 and E01–E39 IDs and all manual states. |
| Local fixture preparation | Loopback Windows coordinator started and restarted successfully; shutdown freed all seven native ports while preserving private identities/signer and data volumes. Each core accepted 55 real synthetic inspections for pagination. The three live Enterprise cases passed again after restart. F07 is separate from F05; live invitation cleanup leaves F07 inactive and restores B expiry. |

### Historical local snapshot (2026-09-26)

The following results are the recorded 2026-09-26 snapshot. For later revisions, rerun the applicable gates and record the tested revision; manual acceptance remains in each project's matrix.

| Boundary | Result |
| --- | --- |
| Community gateway | `go test -race ./...` and `go vet ./...` passed |
| Community daily admission | Functional tests confirmed five accepted requests, a sixth `429 daily_limit_exceeded`, no sixth engine call, and development exemption |
| Enterprise gateway | `go test -race ./...` and `go vet ./...` passed against disposable PostgreSQL and Redis |
| Enterprise application workflow | Real HTTP/storage tests covered encrypted intake, shared public admission, single-use applicant access, domain-workflow state, private/reserved address classification, LinkedIn OIDC evidence, versioned review gates and provisioning prerequisites |
| Enterprise SCIM | Real PostgreSQL tests covered user filtering/pagination and group add/remove/full replacement |
| Enterprise frontend | `npm run verify` passed with 342 English keys and 46 pending translation keys; the ordinary Playwright run passed 19 scenarios with 3 opt-in live tests skipped |
| Enterprise local browser | The isolated Keycloak profile passed 3 tests: login/logout, invitations, subscription expiry/renewal, role views, organization switching, two tenant-core routes, masking/restoration, blocking, streaming and signed reports |
| PDF view | Browser PDF output rendered a four-page 75-row report with repeated table headers and Turkish text; the empty report rendered on one page without card overlap |
| SIEM and retention | Local HTTP receiver tests covered delivery, retry, tenant routing, outage counters and bounded queue drops; production cleanup retained recent events/policy history and removed expired rows in disposable PostgreSQL |
| Local backup restore | A synthetic PostgreSQL backup restored into a separate disposable database; organization/event/history counts matched at restore time (`2/8/0`) |
| Repository hygiene | `git diff --check` passed in both repositories |

Current workflow identities are maintained above; [rerun guidance](VERIFICATION.md#when-to-rerun-the-workflow) remains revision-specific.

## Deployment state

- Frontend/backend packaging and local development flows are implemented.
- No production frontend, API, DNS, backend host or identity service was deployed or purchased in this work.
- The backend provider remains undecided. The evaluated reference candidate is recorded only in [HOSTING.md](HOSTING.md).
- No commit, staging, push, branch or tag operation was performed.

## Remaining release gates

Status: prepared for local acceptance; controlled hosted pilot still requires the following gates. No backend host or capacity has been selected. Manual cells remain tester-owned; six Enterprise translations remain in the user's pending queue.

Follow this sequence; the existing guides are ready to execute and do not need to be rewritten.

1. **Complete local UI acceptance first.** Prepare/start the reusable local kit (`prompt-shield-enterprise/docs/LOCAL_PILOT.md`), then follow the nine [Community scenarios](MANUAL_TEST_GUIDE_TR.md) before the thirteen Enterprise scenarios (`prompt-shield-enterprise/docs/MANUAL_TEST_GUIDE_TR.md`). Use Windows Chrome and Edge, synthetic examples and the prepared roles. Record browser, revision, observed results and reproductions in the [Community matrix](MANUAL_TEST_MATRIX.md) and Enterprise matrix (`prompt-shield-enterprise/docs/MANUAL_TEST_MATRIX.md`). Missing external inputs block only their corresponding branches. The local kit uses rules-only engines; its UI results do not accept contextual NER or real providers. A partially observed row is not a complete PASS.
2. **Resolve failures and freeze the launch scope.** Fix reproduced defects and rerun the affected scenarios. Complete the pending translations before claiming complete Turkish UI acceptance. If Turkish contextual PERSON/ORGANIZATION/LOCATION detection is required at launch, evaluate the two-model [shortlist](MODEL.md#alternative-models-and-turkish-support) through the real inspection/restoration boundary before selecting a replacement; testing every researched model is unnecessary. The current pinned model has no Turkish accuracy acceptance. Freeze the chosen model/configuration before release verification.
3. **Verify the actual release revisions.** After user-owned commits/pushes, manually dispatch both workflows and confirm each full release SHA, following the [rerun rules](VERIFICATION.md#when-to-rerun-the-workflow). Community `full_load=false` is sufficient for the current UI/documentation/dependency changes; repeating the approximately 160-minute run is unnecessary for those changes. Reassess full load and the separate model/pool evaluation if engine/model, workload or sustained-capacity claims change. The historical successful run does not verify a new release revision.
4. **Accept the chosen hosted staging path.** Select backend host/region/capacity and prepare isolated HTTPS frontend/API/identity origins using [HOSTING.md](HOSTING.md). Accept cookies, Origin/CORS/CSRF, direct-route refresh, private tenant-core routing, streaming and cancellation through the deployed path. Prove the hosted five-actions-per-IP-per-UTC-day allowance at ingress and zero gateway interaction for excess traffic/preflight; application Redis tests alone cannot accept this boundary. Record hosted evidence separately from local results.
5. **Accept real integrations in the launch scope.** Validate real-provider models/streaming without retaining provider credentials or raw prompts; current local end-to-end traffic uses a synthetic provider. For the Enterprise application workflow, accept production SMTP, owned public DNS/TLS domain proof, real LinkedIn account consent/connection, versioned approval and separate provisioning using the external gates (`prompt-shield-enterprise/docs/ENTERPRISE_VERIFICATION_AND_ONBOARDING.md#external-acceptance-gates`). Validate customer OIDC/SAML/SCIM and SIEM destinations when included in the pilot's feature scope; local fixtures do not accept vendors.
6. **Complete operational acceptance before opening production access.** Accept encrypted backup/restore, retention, rollback and ingress/APM redaction on the chosen infrastructure using the [Community technical acceptance](VERIFICATION.md#technical-manual-acceptance) and Enterprise technical acceptance (`prompt-shield-enterprise/docs/ENTERPRISE_VERIFICATION_AND_ONBOARDING.md#technical-manual-acceptance`). Complete hosted-service ownership, licensing, contribution and jurisdiction-specific legal review. Begin a controlled hosted pilot only after the required gates for its declared scope pass. Sector models, automatic billing/activation and automatic tenant infrastructure remain outside delivered scope.

The Community short-NER P99 <50 ms goal is advisory, not a mandatory release, acceptance or model/default-promotion gate. Record actual latency/capacity on the selected host; the latest local 40.4–49.3 ms baseline does not establish performance in a different environment. Model/session default changes still require the separate quality, safety and resource evaluation in [PERFORMANCE_RESULTS.md](PERFORMANCE_RESULTS.md).

## Canonical documentation

| Topic | Canonical file |
| --- | --- |
| Product overview and quick start | Community [README](../README.md), Enterprise (`prompt-shield-enterprise/README.md`) |
| Cross-product deployment and domains | [HOSTING.md](HOSTING.md) |
| Community API contract | [API.md](API.md) |
| Community operations | [OPERATIONS.md](OPERATIONS.md) |
| Security boundaries | [SECURITY.md](SECURITY.md) |
| Model provenance and accuracy | [MODEL.md](MODEL.md) |
| Verification commands and CI rerun rules | [VERIFICATION.md](VERIFICATION.md) |
| Performance evidence | [PERFORMANCE_RESULTS.md](PERFORMANCE_RESULTS.md) |
| Enterprise runtime and identity | Enterprise (`prompt-shield-enterprise/docs/HOSTING.md`) |
| Enterprise intake and activation | Enterprise (`prompt-shield-enterprise/docs/ENTERPRISE_VERIFICATION_AND_ONBOARDING.md`) |
| Persistent manual acceptance | Community [MANUAL_TEST_MATRIX.md](MANUAL_TEST_MATRIX.md), Enterprise (`prompt-shield-enterprise/docs/MANUAL_TEST_MATRIX.md`) |
| Windows developer preparation | Enterprise (`prompt-shield-enterprise/docs/LOCAL_PILOT.md`) |
| Reusable manual test data | Community [MANUAL_TEST_EXAMPLES.md](MANUAL_TEST_EXAMPLES.md), Enterprise (`prompt-shield-enterprise/docs/MANUAL_TEST_EXAMPLES.md`) |
