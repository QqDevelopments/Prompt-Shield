# Recommended VS Code extensions

These extensions support the Community Rust engine, Go gateway and React/Vite frontend. They are editor tools, not application runtime dependencies. You can build and test from the command line without installing them.

The workspace recommendation list is maintained in [extensions.json](../.vscode/extensions.json). It does not install extensions automatically. Open the `prompt-shield` folder in VS Code, open Extensions with `Ctrl+Shift+X`, search for `@recommended`, and install the workspace recommendations relevant to your work. If you open the parent directory instead, add `prompt-shield` as a workspace folder to use its folder settings and recommendations.

## Repository recommendations

| Extension ID | Use |
| --- | --- |
| `rust-lang.rust-analyzer` | Rust navigation, diagnostics and formatting integration |
| `golang.go` | Go language tools, navigation, formatting and debugging |
| `bufbuild.vscode-buf` | Protobuf editing and formatting |
| `tamasfe.even-better-toml` | Cargo/TOML editing |
| `ms-azuretools.vscode-docker` | Container tooling for the local services |
| `redhat.vscode-yaml` | Compose, workflow and deployment YAML editing |
| `vadimcn.vscode-lldb` | Optional native Rust debugging |

Rust, Go and Protobuf extensions are especially useful because [settings.json](../.vscode/settings.json) selects their formatters. CodeLLDB is optional unless you need native debugging.

## Installation and verification

You can also install an individual extension from a terminal when the VS Code CLI is on PATH:

```powershell
code --install-extension rust-lang.rust-analyzer
code --list-extensions
```

Compilers, Node.js, Docker, protoc and other command-line dependencies must still be installed separately; see [README.md](../README.md) and [VERIFICATION.md](VERIFICATION.md). The `apps/web-console` frontend is built as a standard `dist` artifact for Vercel or another static host; the backend provider is undecided. An installed extension does not prove CI checks or achievement of the advisory short-NER P99 <50 ms goal.

See the official [VS Code extension management guide](https://code.visualstudio.com/docs/configure/extensions/extension-marketplace#workspace-recommended-extensions) for workspace recommendations and installation commands.
