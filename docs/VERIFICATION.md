# Community verification commands and acceptance

This guide covers dependency security gates, model evaluation, load/installation verification and the manual session-pool performance reference. It does not declare a production release. Run commands from the Community repository root unless stated otherwise. Source state, environment, model identities and exclusions are part of the evidence.

For user-facing browser/API workflows, use the separate [Turkish manual test guide](MANUAL_TEST_GUIDE_TR.md), [acceptance matrix](MANUAL_TEST_MATRIX.md), and [synthetic examples](MANUAL_TEST_EXAMPLES.md). Automated and manual evidence remain separate.

## Automated dependency security gates

`npm run verify` from `apps/web-console` runs the existing frontend checks and full `npm audit`, followed by the Rust and Go scans. Install Node.js 24, Go 1.25+ (the CI reference is 1.27), Rust 1.90+ and the pinned Rust scanner:

```powershell
cargo install cargo-audit --version 0.22.2 --locked
cd apps/web-console
npm run verify
```

The scans can also run separately from that directory:

```powershell
npm run verify:frontend
npm run audit:backend
npm run audit:rust
npm run audit:go
npm run audit:npm
```

`verify:frontend` covers the independently deployed Vite application and its npm dependencies. `audit:rust` scans the entire engine `Cargo.lock`, including optional NER dependencies, with `cargo audit --deny unsound --deny yanked`; vulnerabilities, unsound advisories and yanked crates fail the gate. Unmaintained-crate notices remain visible warnings. No engine advisory is ignored. `audit:go` runs the pinned `golang.org/x/vuln/cmd/govulncheck@v1.7.0` through `go run`, so a separate global installation is unnecessary. This scanner version supports the gateway's Go 1.25 minimum while fetching current vulnerability data. It checks `./...` using the selected Go toolchain and build target, and fails when vulnerable symbols are reachable from the application. Findings in dependencies that are not called remain informational in the scanner output.

Missing tools, scanner failures and advisory database access failures must fail verification rather than skip the scan. These commands need network access to retrieve advisory data and, on the first Go scan, the pinned tool. The Community workflow quality job installs cargo-audit and invokes the complete `npm run verify` chain; Linux CI covers the deployed Go target while a Windows run covers Windows. Gateway and engine functional tests remain the separate existing backend gates; dependency scanning does not replace them or model/manual acceptance.

Primary references: [RustSec cargo-audit](https://github.com/rustsec/rustsec/tree/main/cargo-audit) and [Go vulnerability management](https://go.dev/doc/security/vuln/).

## Model conversion and accuracy

Host prerequisites: Python 3.14, Rust/Cargo and Git. Model dependencies are fully version-pinned in scripts/model-requirements.txt. Windows commands:

```powershell
python -m venv output/model-env
& output/model-env/Scripts/python.exe -m pip install -r scripts/model-requirements.txt
& output/model-env/Scripts/python.exe -m pip check
& output/model-env/Scripts/python.exe scripts/ner_pipeline.py fetch
& output/model-env/Scripts/python.exe scripts/ner_pipeline.py convert
& output/model-env/Scripts/python.exe scripts/ner_pipeline.py evaluate
```

On Linux, use output/model-env/bin/python. Downloaded binary weights, the public dataset and generated reports remain under ignored output/model-evaluation. No production model is overwritten. The evaluation Cargo feature adds an offline, checksum-verified model loader; standard service builds retain the original pinned checksum.

Conversion uses fixed dynamic QInt8 options on the pinned FP32 ONNX artifact twice. Both binary hashes and Rust predictions must match across repeated conversions. Evaluation uses the production tokenizer, confidence threshold and BIO/UTF-8 span assembly, not a separately implemented Python inference pipeline.

Few-NERD supervised/test is pinned in models/evaluation.json. Exactly 2,000 unique IDs are selected by SHA-256 order. Input tokens are joined by single ASCII spaces and offsets are UTF-8 bytes. Entity boundaries follow contiguous fine labels; coarse PERSON, ORGANIZATION and LOCATION are scored, while the other categories are treated as outside the supported classes. The CC-BY-SA-4.0 dataset and Ding et al. attribution are recorded in the manifest. This is an English cross-dataset evaluation, not a Turkish/domain accuracy claim or a claim of comparability with CoNLL scores.

The FP32 and both INT8 artifacts must use the same corpus, accepted/rejected examples, tokenizer, span-aggregation version and class support. Inputs above 512 tokens are rejected, not truncated. Reports include rejected IDs. An INT8 artifact fails when overall F1 or recall drops by more than 1 percentage point, or any class recall drops by more than 2 percentage points relative to FP32. A failed comparison exits nonzero after writing comparison.json; do not remove the failure or adjust its thresholds to obtain a green CI run.

Observed on 2026-09-05 with the old subtoken decoder: all 2,000 examples were evaluated and both INT8 artifacts failed accuracy acceptance. After correcting whole-word span assembly, both pass the unchanged gates on the original sample and a disjoint 2,000-example confirmation sample. See [the model correction and its coverage tradeoff](MODEL.md#wordpiece-masking-correction). The production weights and conversion recipe remain unchanged. Rerun every artifact after a decoder change; do not combine old and new prediction reports.

## Load, allocations and cancellation

Host prerequisites: Python 3.14, Node 24, Go, Rust and Docker Desktop with Linux containers (or a Linux Docker engine). Each invocation creates a unique disposable PostgreSQL/Redis Compose project and dedicated native service processes; existing installations are not reused.

```powershell
python scripts/run_load.py --profile smoke --output output/load-smoke
python scripts/run_load.py --profile full --output output/load-full
python scripts/profile_native.py --workloads output/load-smoke/workloads.json --output output/native-profile
python scripts/report_load.py output/load-full --output output/load-full/report
```

Use a new output directory for each run. The full profile takes approximately 160 minutes plus build/profiling time: 30-second warm-up and 120-second samples across rules/NER sizes and 1/4/16 workers, HTTP/SSE mixed cases, a 15-minute mixed soak, cancellation and rejection probes. The smoke profile exercises the same boundaries with short durations; it is not sustained-load evidence.

The report command validates condition counts, environment hashes, outcome totals, rates, recovery and resource samples before writing compact JSON and a Markdown table. Incomplete runs require an explicit --allow-incomplete snapshot and remain labeled incomplete. Per-condition percentiles are never averaged together. The companion JSON preserves all-request and successful percentiles, exact policy counts, process-lifetime versus sampled memory peaks, allocations and cancellation observations.

Use --follow instead of --allow-incomplete to update the same report as one already-running matrix completes conditions. This follows that invocation for at most four hours; it never starts or restarts load. It succeeds only on a validated complete run and exits with failure when the run stops incomplete or the follow timeout expires.

Create an empty STOP file in the run directory to stop safely after the current condition. The runner then retains completed reports, marks the run incomplete and cleans up only its own processes/dependency project.

Load is closed-loop and synthetic. gRPC performance is separate from HTTP/SSE performance. The HTTP fixture runs actual production handlers with real PostgreSQL/Redis and an echo upstream delayed by 20 ms. It retains the production limit of 120 requests/minute per organization and key; 429 responses can dominate saturation measurements. They are reported as failures, not successful capacity. The fixture process includes the synthetic upstream, so its process allocation numbers must not be described as isolated gateway allocations.

Workloads record byte/token lengths and actual policy mix. Latency uses bounded logarithmic histograms with 1% bucket width, with success-only and all-request percentiles reported separately. Empty histograms contain null quantiles. Expected blocks and boundary rejections are successful policy outcomes; deadline exhaustion, resource exhaustion and 429 are capacity failures. Cancellation selection, timer signals, canceled outcomes and other failures are distinct.

The load matrix explicitly uses a 10-second per-request deadline for direct gRPC probes and a 1-second deadline for HTTP/SSE probes, recorded as deadline_ms in each report. At the retained one-session default, four concurrent maximum-length NER requests still wait for that session, but queued requests no longer occupy blocking threads or prefilter permits. Native cancellation is cooperative and retains NER capacity until work exits. This measurement budget does not change production gateway timeouts, cancellation timers, latency percentiles or accuracy thresholds. HTTP/SSE deadline exhaustion remains an unsuccessful capacity result in the evidence but does not abort the remaining matrix when recovery and fixture-health checks pass. A direct gRPC deadline or an unknown transport/protocol failure still fails the condition.

Windows request timing uses QueryPerformanceCounter with its recorded frequency; other platforms use Go's monotonic clock. Nonpositive request intervals fail measurement. Earlier phase-two smoke runs 01/02 and the aborted full run 01 used a clock that sometimes returned zero for short Windows RPC intervals; their timing is invalidated. Functional outcomes remain diagnostic evidence. The corrected smoke run 03 records the clock and frequency. [Windows interval counter documentation](https://learn.microsoft.com/en-us/windows/win32/api/profileapi/nf-profileapi-queryperformancecounter).

Resource sidecars sample native service RSS every 500 ms and capture process-lifetime high-water marks. HTTP fixture MemStats deltas include warm-up, resource polling, gateway and synthetic upstream. Rust allocation-profile separately counts global allocator activity across request construction and real public-service dispatch; it excludes native ONNX allocation. Heaptrack runs in a separate Linux container and captures native malloc/free including ONNX, startup and warm-up. Its RSS includes profiling overhead and is not a latency-run RSS figure. Residual allocations at process exit are not automatically classified as request leaks.

Cancellation is also observed through an offline evaluation-only service dispatch profiler: it records occupied prefilter plus NER permits before/after abort, time to drain and subsequent public-service success. Only rounds that observe an occupied NER permit count toward the twenty samples; scheduler-missed probes are counted separately and retried within a bounded attempt limit. Current dispatch cancellation removes session waiters and signals request-specific ONNX RunOptions. The worker retains its lease until native exit; the signal does not establish an instantaneous preemption guarantee. Public pool tests additionally check rule availability, subsequent contextual restoration and request isolation across twenty rounds for each session count.

The gRPC cancellation timer is armed immediately before the client Inspect call; the HTTP/SSE timer is armed at the client's WroteRequest callback. Neither client signal proves that every selected request has started inference or reached the upstream. Reported cancellation latency spans the client invocation to its return, including the configured delay; the separate occupied-permit observer supplies evidence of cancellation after engine dispatch.

HTTP fixture connection files contain only temporary synthetic credentials but are excluded from CI artifacts. Reports never contain original user prompts or mappings. Environment metadata and resource sidecars must accompany latency reports.

## Manual performance reference

This is a separate runner from `run_load.py` and the workflow's `full_load` input. It confines the engine to logical CPUs 0-7, eight CPUs of quota and 1 GiB memory with no additional swap, and the separate load container to CPUs 8-15. Docker must expose all sixteen logical CPU IDs and cgroup v2 memory counters. Measurements on other allocations are a different reference. Host tools include Python, Go, Rust, Node and Docker because source/environment fingerprints are collected.

Generate diverse fixed cases with the pinned production tokenizer; Python preserves the evaluator's JSON bytes without PowerShell output-encoding differences:

```powershell
node --no-warnings --experimental-strip-types scripts/fetch-model.ts
@'
import subprocess, sys
from pathlib import Path
sys.path.insert(0, 'scripts')
from verification import ROOT, cargo_binary
destination = ROOT / 'output/performance-cases.json'
destination.parent.mkdir(parents=True, exist_ok=True)
if destination.exists():
    raise SystemExit('Keep existing evidence; choose a new case-file path.')
destination.write_bytes(subprocess.check_output([
    cargo_binary('ner-evaluate'), 'workloads',
    ROOT / 'models/general-ner/tokenizer.json'], cwd=ROOT))
'@ | python -
docker build -f deploy/Dockerfile.bench -t ps-perf-bench:local .
docker build -f services/engine/Dockerfile -t ps-perf-final:local .
python scripts/performance_matrix.py --image ps-perf-final:local --cases output/performance-cases.json --output output/new-reference --sessions 4 --threads 1 --spinning false
```

The reference command defaults to four workers, a 10-second deadline, 30 seconds warm-up and 120 seconds measurement, repeated three times in fresh engine processes/cgroups. Use a new output directory for every invocation. The example configuration is a measurement input, not an accepted production recommendation. Screen all sixteen eligible combinations first using `--warmup 5 --duration 15 --repeats 1`, then run the best three with the full defaults. Preserve a baseline image before editing production code; rebuilding the current source cannot reproduce the old baseline.

The short-NER gRPC P99 <50 ms optimization goal is advisory. The runner reports `p99_target_ms=50`, `p99_target_required=false` and a separate `p99_target_met` result. `acceptance_passed` evaluates complete reference data, error-free outcomes, recovery and resource limits independently of latency; missing the goal alone does not fail acceptance or the command. Goal attainment is `null` outside a valid short-input reference. Long-input/cancellation/soak conditions have their own reported outcomes. Historical evidence retains its original 6 ms target/gate fields; see the [current goal decision](PERFORMANCE_RESULTS.md).

Independent long-input, cancellation and soak examples:

```powershell
python scripts/performance_matrix.py --image ps-perf-final:local --cases output/performance-cases.json --output output/new-128 --sessions 4 --threads 1 --spinning false --scenario ner-128 --warmup 10 --duration 60 --repeats 1
python scripts/performance_matrix.py --image ps-perf-final:local --cases output/performance-cases.json --output output/new-512 --sessions 4 --threads 1 --spinning false --scenario ner-512 --warmup 10 --duration 60 --repeats 1
python scripts/performance_matrix.py --image ps-perf-final:local --cases output/performance-cases.json --output output/new-cancellation --sessions 4 --threads 1 --spinning false --scenario ner-512 --warmup 5 --duration 30 --cancel-percent 50 --repeats 1
python scripts/performance_matrix.py --image ps-perf-final:local --cases output/performance-cases.json --output output/new-soak --sessions 4 --threads 1 --spinning false --scenario mixed-long --duration 900 --repeats 1
docker build -f deploy/Dockerfile.performance-tests -t ps-perf-tests:local .
python scripts/profile_stages.py --output output/new-stage-profile.json
```

`profile_stages.py` uses `output/performance-cases.json` and a separate instrumented image; never enable `profiling` in a latency acceptance image. The 120 serial profile samples include twenty warm-ups and do not measure session contention. `report_performance.py` consolidates the original experiment's fixed `output/performance-*` directories into [retained evidence](evidence/performance-optimization.json); it does not automatically discover arbitrary `output/new-*` runs. Each new run's own JSON/resource/environment files remain its evidence.

### Candidate reproduction and explicit tests

Prepare the primary corpus with `ner_pipeline.py fetch` as above. The candidate runner also requires `output/ner-wordpiece-holdout/corpus.jsonl` and its `corpus-manifest.json`: use the same checksum-verified Few-NERD parquet, sort by `(SHA256(str(id)), str(id))`, select positions 2001-4000 and reconstruct examples with `ner_pipeline.example`. Retain dataset provenance, selection, UTF-8 corpus SHA-256 and IDs in the manifest. Fetching the ordinary corpus into a second directory selects the same first 2,000 IDs and is not a disjoint confirmation set; the candidate runner rejects overlap. `--corpora` can supply two alternative prepared paths.

With both corpora and `output/model-evaluation/existing.json` prepared, use the locked model environment:

```powershell
& output/model-env/Scripts/python.exe scripts/performance_candidates.py fetch
& output/model-env/Scripts/python.exe scripts/performance_candidates.py workloads
& output/model-env/Scripts/python.exe scripts/performance_candidates.py evaluate
cargo test --manifest-path services/engine/Cargo.toml --locked --release -j 1 --features evaluation --test ner --test pool -- --include-ignored --test-threads=1
cargo test --manifest-path services/engine/Cargo.toml --locked --release -j 1 --features evaluation --test workloads -- --ignored
```

The `workloads` step requires the case file above and verifies identical 32-token inputs across candidate tokenizers. The additional ignored CLI test checks eight inputs at each 32/128/512/513-token boundary. Workflow integration explicitly includes the model and pool tests; candidate downloads, this candidate CLI test, smaller-model quality evaluation and the manual reference matrix are local/manual checks. Candidate evaluation writes `comparison-0.json` and `comparison-1.json` with `passed` and `failures`; a completed command does not itself mean a candidate passed quality. Both recorded smaller models failed and remain disqualified. See [the complete results](PERFORMANCE_RESULTS.md).

## Clean install, integration and CI

```powershell
python scripts/verify_proto.py
python scripts/clean_install.py --output output/clean-install
```

The default clean install uses tracked source. Before a commit, explicitly add --working-source to include nonignored newly authored files without staging them; this mode is recorded in the source manifest and is not called a committed-checkout test. A fresh source directory, fresh model download, no-cache image build, new secrets and a unique database volume are used. Host application dependencies and existing .env are not copied. The disposable project's own volumes are removed after verification.

The clean-install smoke checks readiness, authentication, rules, blocking, NER, key visibility/revocation and policy restoration. Its cgroup peak memory is installation/smoke evidence, separate from sustained native load.

For mandatory integrations, provide TEST_DATABASE_URL and TEST_REDIS_URL for a disposable fixture, install openai==2.45.0 and anthropic==1.4.0 into a separate Python environment, and run scripts/ci_integration.py with that environment's Python. It starts its own real engine and requires all eight database/cache/engine/SDK and shared-queue boundaries to pass, including concurrent Redis admission, owner expiry and two-gateway HTTP cancellation/recovery. Missing configuration or skipped required tests cannot produce success.

The workflow has independent quality, integration, model-evaluation, clean-install and load jobs. A manually dispatched run executes the verification gates, and its `full_load` input selects the sustained load profile. Formatting, type checks, real browser journeys, explicit model tests, protobuf regeneration and benchmarks are included. Absolute performance targets are reported rather than enforced on variable shared-runner hardware. INT8 accuracy thresholds remain enforced.

Manual workflow runs provide a `linux_runner` choice. Select `Self-Hosted PC (WSL2/Docker - 0 mins)` to run the Linux verification jobs on a registered runner with the labels `self-hosted`, `Linux`, and `X64`; select the GitHub Cloud option to use `ubuntu-24.04`. The self-hosted runner must be registered and have a Linux/WSL2 user with passwordless sudo; the workflow bootstraps the missing host tools. The portability job still uses its Ubuntu and Windows matrix so platform coverage is not silently replaced by one machine. “0 mins” means no GitHub-hosted runner minutes are consumed; the full load still takes about 160 minutes of wall-clock time on the selected PC. Verify has no push or pull-request trigger, so it starts only when an operator selects Run workflow.

At the beginning of every Linux verification job, `scripts/bootstrap-linux-runner.sh` checks Git, curl, Docker, Docker Compose v2 and protoc, installs missing apt packages, and installs or selects Rust 1.90 with rustup. A sudo password prompt or unavailable Docker daemon fails with an actionable message. The workflow setup actions install and expose Go 1.27.0, Node.js 24.12.0 and Python 3.14. The quality job installs Chromium, Firefox, WebKit and their system dependencies immediately after `npm ci`, before frontend checks. Docker Desktop WSL2 integration must be enabled for the selected distribution.

The current Community and Enterprise workflow identities, outcomes and scopes are maintained in [recorded CI verification](IMPLEMENTATION_STATUS.md#recorded-ci-verification). The retained [older numerical model snapshot](evidence/ci-34062487943.json) describes its original revision only.

## Technical manual acceptance

The [Turkish guide](MANUAL_TEST_GUIDE_TR.md) covers actions visible in the product. The following technical scenarios retain the API, dependency and operations coverage previously mixed into CE-09 and CE-11 through CE-13. A developer prepares the isolated fixtures and records evidence in [MANUAL_TEST_MATRIX.md](MANUAL_TEST_MATRIX.md); no browser screenshot alone accepts these boundaries. Use synthetic [examples](MANUAL_TEST_EXAMPLES.md), a controlled upstream and disposable storage. Keep requests, provider keys and restoration mappings out of retained logs.

### Provider boundary acceptance

Scope: former CE-09; C06, C10 and C16–C19.

1. Execute the mandatory integration runner described above. Its real-engine and pinned OpenAI/Anthropic SDK checks must run rather than skip.
2. With the chosen controlled or separately authorized real upstream, send JSON chat requests for both supported provider contracts, including repeated email placeholders. Verify that the provider receives sanitized text and the client receives restoration belonging only to that request. Keep application and provider credentials in their separate documented headers.
3. Exercise split-placeholder SSE, an explicit completion, abrupt upstream closure and client cancellation. Compare sanitized upstream observations with restored client output and verify that a later request succeeds without receiving the earlier request's state.
4. Exercise embeddings, unsupported content, body limits and conflicting output-token fields using [API.md](API.md). Vectors and safe protocol errors must follow the existing contract. Blocked, inspection-failed and audit-failed traffic must produce zero upstream dispatch.

Record provider/model, request IDs and metadata counters. A controlled fixture pass does not accept current vendor models or vendor streaming.

### Failure and model acceptance

Scope: former CE-11; C06, C14 and C21.

1. In an isolated fixture, make engine, PostgreSQL, Redis and provider dependencies unavailable one at a time. Observe safe busy/unavailable/authorization errors and the documented retry behavior; compare the controlled provider's dispatch counter.
2. Restore each dependency and issue a fresh request. It must succeed after recovery, without stale inspection results or cross-request mappings. Restore all fault settings.
3. Enable contextual NER only with the pinned model/tokenizer. Run the explicit NER/pool commands above, check English contextual PII independently from deterministic masking, and verify failure when a required model cannot load. Rules-only results do not accept contextual NER quality.

### Persistence and operations acceptance

Scope: former CE-12; C22 and C26.

1. Use persistent disposable PostgreSQL/Redis, create a test policy/key and metadata events, then stop and restart the actual gateway and dependencies. Verify retained policy/key metadata and the intended session invalidation. Repeat ephemeral development separately: its state is not expected to survive process restart.
2. Follow [the recovery procedure](OPERATIONS.md#upgrade-backup-and-recovery) into a separate restore target. Compare organization, policy/key and event counts plus an authenticated request before accepting restore.
3. On the selected infrastructure, verify encrypted backup access, retention, rollback and proxy/APM redaction. Local unencrypted backups or database assertions alone leave this hosted gate open.

### Hosted and performance acceptance

Scope: former CE-13; C23–C27.

1. Prepare the approved separate HTTPS frontend/API origins from [HOSTING.md](HOSTING.md). Verify exact Origin/CORS/CSRF, secure session cookies, direct-route refresh, ingress streaming and cancellation through the deployed network path.
2. With `PUBLIC_DAILY_IP_LIMIT_ENABLED=true` and a fresh dedicated test-IP identity, perform five admitted actions, including model discovery if used, then the sixth. Observe `429 daily_limit_exceeded` and `Retry-After`; confirm no sixth policy, engine, audit or provider operation. Separately verify the disabled setting and development exemption without disabling authentication, token quotas or queue capacity.
3. Record both IP switches and inspect ingress counters to prove configured excess requests and preflight finish before the gateway. Redis application admission is a backend backstop and cannot establish zero backend interaction by itself. Check Community minute/per-IP controls enabled and disabled; the public global and core queue limits must remain enforced. Follow the queue drain/restart procedure when changing settings.
4. Run only the load/model profile required by the changed boundary. Capture model/build identity, runner/cgroup limits, latency, resource peaks, native cancellation/drain and quality. Compare to [measured reference results](PERFORMANCE_RESULTS.md); passing functional tests or CI conditions does not establish the advisory short-NER P99 <50 ms goal on the chosen host. Missing that goal alone does not fail release or acceptance.

## When to rerun the workflow

The successful sustained Community baseline (run 35630478306) and Enterprise baseline (run 35535257784) precede the current frontend/application revisions. Before release, the user must commit/push the intended revisions and manually dispatch each repository's verification workflow. Confirm each run's full `headSha`; rerunning an old run tests its original commit. Neither workflow starts on push or pull request.

| Verification | Current guidance | Scope |
| --- | --- | --- |
| Verify Community, `full_load=false` | Required before releasing the current Community revision | Quality, portability, real dependencies/SDKs, explicit model/pool tests, BERT accuracy gates, fresh Compose installation and smoke load |
| Verify Community, `full_load=true` | Optional unless workload, engine/model or sustained-capacity claims change | Approximately 160-minute native engine and HTTP/SSE matrix; not the dedicated 8-CPU/1-GiB pool acceptance runner |
| Repeat manual pool matrix and 15-minute soak | Not required for documentation or Go admission-only changes | Rerun when measured engine/model, workload, build or reference environment changes, or before proposing a new performance default |
| Verify Enterprise | Required before releasing the current Enterprise revision | Go race/integration, frontend build/tests, Playwright and package checks against the pushed revision |

Keep tested revision and artifact links with every result. The historical successful workflow does not establish the current advisory short-NER P99 <50 ms goal or qualify either rejected smaller model. The latency goal is not a mandatory release, acceptance or model/default-promotion gate; required quality, safety, recovery and resource checks remain separate. No remote workflow was dispatched for the current working tree during this implementation.
