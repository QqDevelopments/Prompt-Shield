# Agent Instructions: Prompt Shield (Community Edition)

These instructions apply to AI coding agents working directly inside the `prompt-shield` repository.


---

## 1. Subproject Overview

`prompt-shield` is the open-source, ultra-low-latency core engine:
- **`apps/web-console`**: Independent Vercel frontend at prompt-shield.qqdevelopments.com, with account-free use and a separate operator area. Backend hosting remains provider-neutral; see [docs/HOSTING.md](docs/HOSTING.md).
- **`proto/shield.proto`**: Service-to-service Protobuf contract (`InspectAndSanitize`, `DeAnonymize`).
- **`services/gateway`**: Go module language 1.25 (CI currently uses Go 1.27), Community API Gateway (HTTP `:8080`), restricted OpenAI/Anthropic/Google Gemini reverse proxy, Redis rate limiter, PostgreSQL audit logger.
- **`services/engine`**: Rust 1.90+ high-performance inference core (gRPC `:50051`), Aho-Corasick & Regex pre-filter, ONNX Runtime v2.0 INT8 NER models.

---

## Manual Test Documentation Lifecycle

- Write manual test guides in Turkish, using numbered, goal-oriented scenarios with an objective, relevant example inputs, ordered UI steps, and an observable completion result. Prepare and reuse the same test data across scenarios instead of repeating setup for each feature. Keep exact UI labels and protocol identifiers unchanged, and map scenarios to the matrix's feature names or row IDs.
- Keep developer/API/infrastructure checks in a clearly marked section, and leave only the checks with missing prerequisites untested rather than blocking unrelated scenarios.
- Keep `docs/MANUAL_TEST_MATRIX.md` as the persistent source of manual acceptance status and automated coverage references.
- `docs/MANUAL_TEST_GUIDE*.md` files are temporary instructions for pending manual verification. The user may delete a guide after completing its checks. Completion and removal are independent for each project and platform; a removed guide in another project or platform does not justify deleting this project's remaining guides.
- The user owns manual completion and guide deletion. Do not delete guides autonomously, treat an absent guide as a documentation defect, or recreate a removed guide unless the user explicitly requests a new one. Neither guide absence nor automated test success establishes a manual pass; update matrix status only from the user's actual manual evidence.
- Supporting documents such as `docs/MANUAL_TEST_MEDIA_EXAMPLES*.md` and `docs/MANUAL_TEST_EXAMPLES.md` may remain permanently and independently of guides. Preserve them when a guide is removed.
- When maintaining documentation after guide removal, remove or repoint stale guide links to the remaining matrix or examples. Do not restore the guide or copy its completed checklist into another permanent document to compensate for its absence.

## 2. Core Development Rules

1. **Protobuf Single Source of Truth:** `proto/shield.proto` is the authoritative contract. Any interface change must update the proto file first and regenerate stubs (`make proto`).
2. **Rust Engine (Critical Latency Path):**
   - **Advisory optimization goal:** Short-NER gRPC P99 latency <50 ms under the [documented reference workload](docs/PERFORMANCE_RESULTS.md). Missing this goal alone must not fail release or acceptance.
   - Zero-copy: Use `Cow<'a, str>`. Calling `String::clone()` or `to_string()` inside detection loops is strictly prohibited.
   - Safety: `unwrap()` and `expect()` are forbidden; use `thiserror` for domain errors and `anyhow` for top-level entrypoints.
   - Session reuse: Build a bounded pool at startup, lease each mutable session exclusively, and share one `Arc<Tokenizer>`. Validate the session/thread configuration against the eight-thread budget; retain the legacy one-session/four-thread fallback.
   - Antigravity Skill: `rust-ai-sanitizer`.
3. **Go Gateway:**
   - Always enforce `context.WithTimeout` on both external LLM calls and internal gRPC requests.
   - Minimize allocations using `sync.Pool` for byte buffers.
   - When the Rust response indicates `is_blocked = true`, immediately return `400 Bad Request` without calling upstream LLMs.
   - **Server Rate Limit & Cost Control:** Development is exempt from daily IP admission. Outside development, `PUBLIC_DAILY_IP_LIMIT_ENABLED=true` defaults to 5 actions/IP/UTC day; `false` disables it. `PUBLIC_IP_RATE_LIMIT_ENABLED` independently controls public minute/per-IP concurrency checks, preserving global capacity and quotas. Protection work shares the Redis core queue; progress never opens polling requests. Mirror enabled limits at ingress; see `docs/REQUEST_QUEUE.md`.
   - Antigravity Skill: `go-cloud-native-gateway`.
4. **Data Privacy (Zero Plaintext Logging):**
   - Raw user prompts, credentials, or detected PII must never be stored in plain text in audit logs or PostgreSQL tables.
5. **Git & Language Mandates:**
   - Never perform autonomous `git commit` or `git push`.
   - Markdown files, code comments, and documentation must be written in English, except `docs/MANUAL_TEST_GUIDE*.md`, which must use Turkish and the scenario format defined above. Matrices and example documents remain in English.
6. **Test Suite Policy & Zero JS Mandate:**
   - Do not add or retain tests that read implementation source files and assert through string matching, regular expressions, AST/whitespace checks, or variable names.
   - Tests must invoke production functions or exercise a real public/native boundary. They must validate actual contracts, normalized settings, state transitions, or functional behavior without depending on exact code formatting or implementation details.
   - **Zero JS:** No JavaScript test or script files (`*.js`, `*.mjs`, `*.cjs`) are permitted. All tests and scripts must be TypeScript (`.ts`), Python (`.py`), Go (`*_test.go`), or Rust (`tests/*.rs`). Validate TypeScript scripts and tests with `npm run typecheck:test`.
7. **Localization Change Policy:**
   - When adding a new localization key, update only `en.json` and `scripts/pending-localization-keys.json`. Do not modify any other localization files; translations will be handled separately.
   - Do not run `npm run translate:pending-keys` during feature implementation, audits, or verification. The user runs that bulk translation command explicitly; its resulting non-English locale changes are user-owned and must be preserved.

