# Security boundaries

## Threat model

Clients may submit hostile text, forged organization identifiers, oversized payloads, altered placeholders, invalid JSON, or unsupported provider features. Upstreams may time out, return malformed data, redirect, or interrupt streams. The operator and configured provider endpoints are trusted. A compromised host, engine token, provider, database administrator, browser extension, or operator session is outside the protection offered by text masking.

Client plaintext enters the gateway, travels over private authenticated gRPC to the engine, and returns as sanitized text plus request-local mappings. Only sanitized supported text fields go to the configured provider. Mappings restore permitted response text before returning it to the authorized caller. The caller and browser can therefore see original text. Embeddings use sanitized text and are not de-anonymized.

The console inspection API returns sanitized text and metadata without original mapping values. Raw input/output is transient UI state, cleared on reset, unmount, logout or session invalidation. Language/theme storage contains only preferences. Clipboard copying is an explicit action.

## Enforcement

- Random API keys are stored as SHA-256 hashes, scoped to their organization and revocable. Lists contain prefixes only. The configured operator password is hashed with bcrypt at startup.
- In-memory console sessions expire after eight hours and use HttpOnly/SameSite=Strict cookies. Production adds Secure. Mutations require exact Origin and session CSRF token.
- Recognized critical secrets always block, regardless of the legacy gRPC `block_secrets` field. Keywords and limited injection phrases block before NER/provider dispatch.
- Each engine input is capped at 128 KiB. Policies allow 64 keywords of 128 bytes. Proxy JSON is capped at 1 MiB, ordinary upstream responses at 2 MiB, SSE events at 1 MiB and total SSE data at 8 MiB. Unsupported JSON fields fail closed.
- Gateway engine calls have a 500 ms deadline; provider calls have 60 s. The engine bounds prefilter work to four jobs and independently limits running plus waiting NER requests to four. A startup-configured session pool is leased asynchronously; the prefilter permit is released before waiting, keeping rules available during NER saturation. Excess work fails with resource exhaustion.
- The engine token has at least 32 ASCII characters. gRPC is plaintext on the private Compose network. Normal Compose does not publish engine/database/cache ports.
- Random-nonce placeholders carry HMAC-SHA256 signed mappings, including request/organization identity, value, type and offsets. Alteration/cross-scope reuse fails. Engine restart invalidates in-flight signatures. No persistent mapping database exists.
- Egress destinations and credentials come from operator configuration. HTTPS is required outside loopback development; redirects and automatic upstream retries are disabled. Provider error bodies are replaced by safe codes.
- Audit writes precede proxy dispatch; failure prevents dispatch. Records contain identifiers, decisions, counts, latency, time, delivery outcome and reported token usage. They contain no original prompt/entity, mapping or credential. Keywords and key labels are operator configuration: do not put secrets in either.

## Limits

Patterns cover selected formats, not all credentials, phone numbers, identifiers or obfuscated values. Compact uppercase IBAN validation covers TR, DE, GB, FR, NL, ES and IT. NER supports English at a 0.5 confidence threshold. False positives/negatives are possible. Four injection phrases are a limited heuristic; PII NER does not establish general prompt-injection resistance.

The mask_pii switch controls deterministic PII rules; disabling it permits their ordinary PII to leave through the proxy. Contextual NER masking has its own contextual_pii switch and remains active independently. Recognized critical secrets always block. Sanitized text may disclose semantics or sensitive values the detectors miss.

Go buffers and session-owned token input buffers are cleared before pool reuse; mapping references are released at completion. Cancellation removes queued NER waiters and signals request-specific ONNX RunOptions for active native work. The native worker retains its session lease and NER capacity permit until computation actually exits, preventing early reuse. Termination is cooperative and has no guaranteed immediate deadline. Strings, serializer/TLS copies, allocators, swap, crash dumps and browser memory do not guarantee physical erasure. Do not claim secure zeroization.

Audit records are not cryptographically tamper-evident. Post-response accounting failures increment an operational counter; provider token usage may be absent. Quotas reserve a conservative estimate without refunds. Redis restart may reset counters in the supplied nonpersistent setup.

Operators own backups, surrounding request logging, host access, TLS, dump policies, and provider data handling. Do not enable body logging in surrounding infrastructure. No legal compliance certification is claimed.
