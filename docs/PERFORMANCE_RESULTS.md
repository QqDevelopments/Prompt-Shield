# Manual performance optimization results

Status: implementation, functional verification, nine historical sustained reference repetitions and the 15-minute mixed soak are complete. The current **short-NER gRPC P99 <50 ms optimization goal is advisory**, not a mandatory release, acceptance or model/default-promotion gate. The latest three local baseline repetitions measured **40.4–49.3 ms P99**, meeting this goal for their recorded workload and environment. No model or session/thread default has been promoted. The measured bottleneck is ONNX computation; neither smaller model meets the protection requirements. [Historical evidence](evidence/performance-optimization.json) includes all 30 conditions, outcomes, cgroup resources, candidate quality results and source/model fingerprints.

This report preserves local reference measurements. On 2026-10-23, the user replaced the former 6 ms target with the advisory 50 ms goal; [the decision record](evidence/performance-goal-20261023.json) evaluates existing measurements without rerunning or rewriting historical evidence. Old JSON target/gate fields describe the policy at execution time. The later sustained Community workflow is recorded in [current CI verification](IMPLEMENTATION_STATUS.md#recorded-ci-verification). Passing CI conditions does not establish latency on another host; detailed later latency/RSS artifacts were unavailable during the 2026-10-07 review. See [rerun guidance](VERIFICATION.md#when-to-rerun-the-workflow).

## 2026-10-23 local default-configuration reference

The user requested completion of C27's local quality and performance checks. Run `RUN-20261023-C27-LOCAL-05` tested source `19c9b9090b9ecd5a06ec5b02fcf3e2cdf7e70036`; all seven conditions retained the same production-source fingerprint. [Structured evidence](evidence/local-model-performance-acceptance-20261023.json) contains artifact hashes, per-condition quantiles/resources, corpus scores and native cancellation samples.

The Linux Docker engine used CPUs 0-7, eight CPUs of quota and a 1 GiB memory limit with no additional swap; the load container used CPUs 8-15. The retained production settings were one session, four intra-op threads and spinning enabled, with four concurrent requests and a 10-second measurement deadline. The three baseline repetitions each used a fresh engine/cgroup, 30 seconds warm-up and 120 seconds measurement. Long-input runs used 10/60 seconds, cancellation 5/30 seconds and the soak 30/900 seconds.

| Condition | Requests | Outcomes | All-request P99 ms | Peak cgroup MiB |
| --- | ---: | --- | ---: | ---: |
| 32-token baseline 1 | 16,248 | 16,248 ok | 48.829 | 218.574 |
| 32-token baseline 2 | 16,600 | 16,600 ok | 40.418 | 216.406 |
| 32-token baseline 3 | 15,043 | 15,043 ok | 49.318 | 217.164 |
| 128 tokens | 2,742 | 2,742 ok | 114.901 | 218.941 |
| 512 tokens | 601 | 601 ok | 458.141 | 268.695 |
| 512 tokens, 50% cancellation | 611 | 305 canceled; 306 ok | 440.264 | 275.016 |
| 15-minute mixed-long soak | 72,988 | 7,298 expected_block; 65,690 ok | 278.567 | 270.473 |

All conditions had zero unexpected errors, zero OOM events/kills, complete resource sampling and successful subsequent recovery. The cancellation row's 305 canceled outcomes were deliberately selected and signaled; the soak's 7,298 policy blocks were expected outcomes. Quantiles are per condition and are never averaged.

**All three baseline P99 values are below the current advisory 50 ms goal**, with more than 10,000 requests each and memory usage within the limit. On 2026-10-23, the user accepted local **C27 as PASS** and replaced the former 6 ms target with this goal. Exceeding 50 ms alone must not fail release, acceptance or model/default promotion. Historical 6 ms gate flags remain as recorded; quality, safety, recovery and resource checks remain required. Hosted deployment acceptance remains separate. This shared workstation also performed model preparation and rollback work during parts of the early baseline; dedicated-host performance remains a separate measurement.

The pinned English Few-NERD sample evaluated all 2,000 examples with zero rejected IDs through the production Rust decoder. Existing INT8 versus FP32 passed the unchanged quantization gates: overall F1 decreased by 0.393 percentage points and recall by 0.304 points, below their one-point limits; each class recall drop remained below two points. Repeated QInt8 conversion produced identical hashes and predictions. Dataset provenance and per-class scores are retained in the evidence; this accepts the stated English cross-dataset comparison, without a Turkish/domain or absolute-accuracy claim. Generated conversion artifacts remained under ignored output; the pinned production model was unchanged.

After ordinary latency measurements finished, the separate Windows native service-dispatch observer completed 20 occupied-NER cancellation rounds in 20 attempts, with zero observation misses. Every round drained and recovered; the maximum observed drain time was 13.416 ms. The worker retained capacity until native exit. These observer samples are distinct from the Linux RPC cancellation latencies.

The local reference processes, containers and networks were cleaned up; primary Community services and their configuration were preserved.

## Implemented behavior

The completed optimization plan is consolidated into this report. Its advisory goal is real gRPC P99 below 50 ms for at most 32 tokenizer tokens (including special tokens) and four concurrent requests. Reference evaluation uses zero unsuccessful requests and three independent repetitions of 30 seconds warm-up plus 120 seconds measurement with at least 10,000 requests each. Missing the latency goal alone does not fail the evaluation. Keep the eight-CPU/1-GiB Linux Docker reference and record source/artifact fingerprints, all-request quantiles, native stage timings, throttling, OOM and recovery. Continue separate 128/512-token, cancellation and mixed-soak measurements; the short-input goal does not apply to these conditions.

Promotion still requires unchanged masking/quantization gates and no regression in per-class precision/recall/F1, no newly exposed protected characters and no increased non-sensitive masking across both disjoint corpora. Reject failed candidates before performance-based selection. Compare peak memory and worst-repetition P99 among configurations that meet required quality, safety, recovery and resource checks; missing 50 ms alone does not disqualify a configuration. The revised goal does not automatically promote a candidate or change defaults. Reproduction commands remain in [VERIFICATION.md](VERIFICATION.md#manual-performance-reference).

The engine creates a bounded session pool at startup. Four prefilter permits and four NER permits are independent. NER waits asynchronously for an exclusive session lease; cancellation removes waiters and signals request-specific ONNX RunOptions for active work. The lease remains owned by native work until it exits. Session-owned input buffers are bounded to 512 tokens and cleared before reuse. Token limits, whole-word masking, confidence thresholds, mapping proofs and protobuf contracts are preserved.

Startup settings are `NER_SESSION_COUNT=1`, `NER_INTRA_THREADS=4`, and `NER_SPINNING=true`. Session count and intra-op threads each accept 1/2/4, with their product at most eight. Latency observations for these defaults apply to the recorded workload and host.

## Measurement environment

AMD Ryzen 7 7800X3D, Windows host, Docker Desktop Linux engine 29.6.2. Engine containers are built with Rust 1.90; the load image uses Go 1.27. The engine receives logical CPUs 0-7, a quota of eight CPUs, a 1 GiB memory limit and no extra swap allowance. A separate Go load-generator container receives CPUs 8-15. CPU affinity separates logical processors; it does not establish exclusive physical cores or an idle host. No shared CI result is used as the local baseline.

Production model SHA-256: `caaee70a5518ec7f9e46e5308fcc9263a8c227703a9ce46cf61c69a552349648`. Tokenizer SHA-256: `343989712a36cd8b253efeaf8baf6a08b9d2583f78e395e83832e8ee9f8d8ee1`. The pre-change engine was built from revision `d4bd65fabf519c51a0620fed6d7a62f673f5dd61` before engine edits, as `ps-perf-baseline:local`. Per-run environment files record immutable image IDs, source fingerprints, tool versions and case hashes. Their working-tree fingerprints describe the measurement workspace; the baseline engine's source is the recorded Git revision, not subsequent workspace edits.

The fixed case set includes eight synthetic NER sentences, padded and verified with the tokenizer to exactly 32, 128, 512 or 513 tokens including special tokens. Measurements use real gRPC, four closed-loop workers, no retries and a 10-second request deadline. The existing load client warms sequentially, so additional pool sessions receive their first request during measurement. Histograms use 1% logarithmic buckets. Resource sampling includes warm-up, measurement and final recovery. Memory figures are cgroup peaks, not Rust heap allocation or process RSS.

## Initial screening

The old engine's 10-second warm-up plus 30-second reference run completed 4,055 requests with zero failures and all-request P99 of 38.456 ms. Sixteen eligible combinations were then screened with five seconds warm-up plus fifteen seconds measurement each. All sixteen were error-free; none met the former 6 ms target used at execution time.

The three lowest screened P99 values were 27.969 ms for four sessions/one thread/spinning off, 28.817 ms for four sessions/one thread/spinning on, and 31.204 ms for two sessions/two threads/spinning on. Their observed peak cgroup memory was approximately 811.293, 811.590 and 422.758 MiB respectively. Screening is insufficient to establish a reliable improvement or select a default. The sequential runs also do not isolate the causal effect of spinning from host variability.

## Sustained reference results

Each repetition starts a fresh engine process and cgroup, with 30 seconds warm-up followed by 120 seconds measurement. All nine repetitions exceeded 10,000 requests and had zero unsuccessful requests. None met the former 6 ms target used at execution time.

| Sessions / threads / spinning | Requests in each repetition | All-request P99 ms, repetitions 1 / 2 / 3 | Maximum peak MiB |
| --- | --- | --- | ---: |
| 4 / 1 / off | 16,619 / 17,065 / 25,015 | 39.229 / 38.841 / 26.878 | 849.371 |
| 4 / 1 / on | 25,726 / 25,762 / 25,933 | 25.573 / 25.573 / 25.069 | 812.176 |
| 2 / 2 / on | 20,861 / 21,166 / 20,561 | 30.287 / 29.690 / 30.589 | 423.477 |

All-request and successful-request percentiles coincide in these error-free conditions. Mean cgroup CPU usage, including warm-up, ranged from 3.38 to 3.66 CPU cores. There were no OOM events, CPU-quota throttling events or resource-sampling failures. The historical selection under the former 6 ms policy promoted no configuration. Four sessions/one thread/spinning on had the lowest worst-repetition P99 among these measured configurations. The current advisory 50 ms goal informs comparison without automatically changing the recorded default decision.

## Long inputs, cancellation and soak

These conditions used four sessions/one thread/spinning off, selected from the initial screen for stress testing. The 128/512-token conditions used ten seconds warm-up and sixty seconds measurement. Cancellation used five seconds warm-up and thirty seconds measurement, with 50% of requests selected for a 1 ms cancellation timer. The mixed soak used thirty seconds warm-up and 900 seconds measurement, with a 40% clean / 30% rule-mask / 10% blocked / 10% 128-token NER / 10% 512-token NER mix.

| Condition | Requests | Outcomes | All-request P99 ms | Peak MiB |
| --- | ---: | --- | ---: | ---: |
| NER 128 tokens | 3,257 | All successful | 105.059 | 823.816 |
| NER 512 tokens | 635 | All successful | 435.905 | 997.664 |
| NER 512 with cancellation | 981 | 318 successful, 389 canceled, 274 capacity rejections | 418.896 | 988.078 |
| 15-minute mixed soak | 80,049 | 72,045 allowed/masked, 8,004 expected blocks; zero failures | 398.565 | 1,005.543 |

Every condition recovered, with no OOM or CPU-quota throttling. The soak collected 1,287 resource samples without sampling errors. Its ending current memory was 1,003.430 MiB, compared with 806.512 MiB before warm-up; the native runtime retains working memory. Peak headroom below the 1,024 MiB limit was only 18.457 MiB, so this is evidence for the measured workload, not a general memory safety margin or proof of absence of every leak.

The cancellation condition's successful-only P99 was 427.316 ms. Its 274 capacity rejections remain failures; cancellations do not make this a zero-error capacity result. Health/rule recovery probes completed in less than 1 ms after each condition, which does not by itself prove native inference preemption. Separate public-service tests verified native-work drainage and subsequent contextual requests across twenty cancellation rounds for each pool size.

## Smaller models

Both pinned DistilBERT candidates were converted twice with the locked ONNX toolchain. Each pair was byte-identical and produced identical predictions. Model input names were verified against the graph, and BIO label order came from the hash-verified configuration. Both candidates failed the existing quantization-loss limits and the stricter production-protection comparison on the 2,000-example corpus and the disjoint 2,000-example confirmation corpus.

| Model | Main corpus F1 | Confirmation F1 | Newly exposed sensitive characters, main / confirmation |
| --- | ---: | ---: | ---: |
| Existing production INT8 | 0.693847 | 0.677605 | Reference |
| dslim DistilBERT INT8 | 0.634051 | 0.621811 | 1,741 / 1,758 |
| ONNX Community DistilBERT INT8 | 0.645925 | 0.634428 | 1,151 / 1,127 |

These were native offline quality evaluations using the production Rust wordpiece decoder, not Docker latency measurements. Both models were disqualified by quality before performance-based selection. Neither can become the default merely by being faster. Detailed per-class precision/recall/F1, masking coverage and conversion hashes are retained by the candidate runner.

## Separate profiling and verification

A separate instrumented Linux public-service profile collected 120 serial calls, including 20 warm-ups. Mean ONNX execution was 7,514 microseconds; tokenization 62.2, prefilter 7.9, tensor preparation 3.5, span decoding 1.9, merging/masking 4.4, and signing 1.4 microseconds. Session waiting averaged 0.45 microseconds in this serial profile; it does not measure contention. Instrumented timing is excluded from gRPC acceptance.

All thirteen Rust tests passed on Windows and in Linux Docker under the 8-CPU/1-GiB limit. Coverage includes Unicode, whole-word masking, repeated entities, exact 512/513-token boundaries, signed restoration, cross-request isolation, independent rule service while NER is full, and twenty cancellation/recovery rounds for each of 1/2/4 sessions. NER-disabled tests passed separately. Clippy passed with warnings denied. Go benchmark tests passed with the race detector; Python report/model tests and Ruff checks passed.

An additional Windows CLI regression test passed for both pinned candidate tokenizers, generating eight cases at each of 32/128/512/513 tokens. The offline workload generator explicitly disables tokenizer-provided truncation and padding and bounds its padding loop, so a saved tokenizer truncation setting cannot hide the rejection boundary. This tooling correction does not change production inference or the completed latency measurements.

Native Criterion estimates were 247.74 ns clean, 1.157 microseconds masked, 232.53 ns blocked and 3.9825 ms short NER. These are detector microbenchmarks, not service P99. The Go handler benchmark measured 54,746 ns/op, 11,100 B/op and 92 allocations/op. Host contention and different measurement scopes prevent treating these as a controlled before/after latency comparison.

## Reproduction

From the repository root:

```powershell
docker build -f deploy/Dockerfile.bench -t ps-perf-bench:local .
docker build -f services/engine/Dockerfile -t ps-perf-final:local .
python scripts/performance_matrix.py --image ps-perf-final:local --cases output/performance-cases.json --output output/new-reference --sessions 4 --threads 1 --spinning false
python scripts/performance_matrix.py --image ps-perf-final:local --cases output/performance-cases.json --output output/new-soak --sessions 4 --threads 1 --spinning false --scenario mixed-long --duration 900 --repeats 1
```

Generate the case file using the commands in [the manual reference guide](VERIFICATION.md#manual-performance-reference). Candidate reproduction uses `scripts/performance_candidates.py fetch`, followed by `workloads` and `evaluate` under `output/model-env/Scripts/python.exe` with the locked `scripts/model-requirements.txt` environment. The primary corpus and separate disjoint confirmation corpus must already be prepared; the ordinary model-evaluation workflow prepares only the primary sample. Output directories must be new so earlier evidence is never overwritten by a measurement run.
