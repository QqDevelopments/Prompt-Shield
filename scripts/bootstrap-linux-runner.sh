#!/usr/bin/env bash
set -Eeuo pipefail

# Install host prerequisites and the Rust toolchain that the GitHub runner
# cannot reliably provide on a self-hosted WSL2 machine. Go, Node and Python
# versions are installed by the setup-* actions in each job.
if [[ "${RUNNER_OS:-Linux}" != "Linux" ]]; then
  printf 'This bootstrap is for a Linux/WSL2 runner.\n' >&2
  exit 1
fi

root=()
if (( EUID != 0 )); then
  command -v sudo >/dev/null 2>&1 || {
    printf 'sudo is required to install missing Linux runner packages.\n' >&2
    exit 1
  }
  sudo -n true || {
    printf 'sudo must be usable without an interactive password in Actions.\n' >&2
    exit 1
  }
  root=(sudo)
fi

packages=()
command -v git >/dev/null 2>&1 || packages+=(git)
command -v curl >/dev/null 2>&1 || packages+=(curl)
command -v protoc >/dev/null 2>&1 || packages+=(protobuf-compiler)
command -v docker >/dev/null 2>&1 || packages+=(docker.io)
if ((${#packages[@]})); then
  command -v apt-get >/dev/null 2>&1 || {
    printf 'apt-get is required to install missing runner prerequisites.\n' >&2
    exit 1
  }
  "${root[@]}" apt-get update
  "${root[@]}" env DEBIAN_FRONTEND=noninteractive apt-get install -y \
    ca-certificates "${packages[@]}"
fi

if ! docker compose version >/dev/null 2>&1; then
  command -v apt-get >/dev/null 2>&1 || {
    printf 'Docker Compose v2 is missing and apt-get is unavailable.\n' >&2
    exit 1
  }
  "${root[@]}" apt-get update
  if ! "${root[@]}" env DEBIAN_FRONTEND=noninteractive apt-get install -y docker-compose-plugin; then
    "${root[@]}" env DEBIAN_FRONTEND=noninteractive apt-get install -y docker-compose-v2
  fi
fi

# Docker Desktop must expose its daemon to this WSL2 distribution. A native
# daemon is started when systemd/service is available; otherwise fail clearly.
if ! docker info >/dev/null 2>&1; then
  if command -v systemctl >/dev/null 2>&1; then
    "${root[@]}" systemctl start docker >/dev/null 2>&1 || true
  fi
  if command -v service >/dev/null 2>&1; then
    "${root[@]}" service docker start >/dev/null 2>&1 || true
  fi
fi

for command_name in git curl protoc docker; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'Required command is unavailable after bootstrap: %s\n' "$command_name" >&2
    exit 1
  }
done
docker compose version >/dev/null 2>&1 || {
  printf 'Docker Compose v2 is unavailable after bootstrap.\n' >&2
  exit 1
}
docker info >/dev/null 2>&1 || {
  printf 'Docker daemon is unavailable. Enable Docker Desktop WSL2 integration or start the Linux daemon.\n' >&2
  exit 1
}

if ! command -v rustup >/dev/null 2>&1; then
  rustup_installer="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/rustup-init.sh"
  curl --fail --location --silent --show-error https://sh.rustup.rs --output "$rustup_installer"
  sh "$rustup_installer" -y --profile minimal
fi
export PATH="$HOME/.cargo/bin:$PATH"
if [[ -n "${GITHUB_PATH:-}" ]]; then
  printf '%s\n' "$HOME/.cargo/bin" >> "$GITHUB_PATH"
fi
rustup toolchain install 1.90.0 --profile minimal --component clippy,rustfmt
rustup default 1.90.0

printf 'Runner prerequisites ready: git=%s; docker=%s; compose=%s; protoc=%s; rust=%s\n' \
  "$(git --version)" "$(docker --version)" "$(docker compose version)" "$(protoc --version)" "$(rustc --version)"
