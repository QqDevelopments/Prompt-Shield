# Recommended coding-agent skills

These are local instructions for coding agents, not VS Code Marketplace extensions. The VS Code installation guide is [RECOMMENDED_EXTENSIONS.md](RECOMMENDED_EXTENSIONS.md). They apply to source work; Vercel is only the planned frontend host and the backend provider remains undecided.

## Community skills

In the two-repository workspace, the skill files live under the parent's `.agents/skills/` directory:

| Skill | Location | Purpose |
| --- | --- | --- |
| `rust-ai-sanitizer` | [SKILL.md](../../.agents/skills/rust-ai-sanitizer/SKILL.md) | Rust inspection, ONNX inference, tokenization, allocation control and public-service boundaries |
| `go-cloud-native-gateway` | [SKILL.md](../../.agents/skills/go-cloud-native-gateway/SKILL.md) | Go proxy handling, cancellation, timeouts, buffer reuse and persistent gRPC connections |

The workspace also contains an Enterprise compliance skill for the separate Enterprise repository: [enterprise-compliance-security](../../.agents/skills/enterprise-compliance-security/SKILL.md). Community work does not require its Enterprise features.

## Availability and use

These two Community skill directories already exist in the shared workspace. No VS Code extension installation is needed for the Markdown files themselves. Whether an agent discovers them automatically depends on that agent's configured skill support; otherwise point it to the relevant `SKILL.md`.

A standalone clone of `prompt-shield` does not include the parent workspace's skill directories. Follow the repository's [AGENTS.md](../AGENTS.md), and provide the shared skill files separately if your agent needs them.

Skills guide implementation; they do not guarantee correctness, performance or automatic verification. The short-NER gRPC P99 <50 ms goal is advisory, not a mandatory release or acceptance gate. Consult [IMPLEMENTATION_STATUS.md](IMPLEMENTATION_STATUS.md) for current scope and [PERFORMANCE_RESULTS.md](PERFORMANCE_RESULTS.md) for measured outcomes.
