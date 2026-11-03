"""Build a fresh Compose installation from declared source, with no local dependencies."""

import argparse
import hashlib
import json
import os
import secrets
import shutil
import subprocess
import uuid
from pathlib import Path

from verification import ROOT, command, port


def main(output, working_source):
    output.mkdir(parents=True, exist_ok=False)
    source = output / "source"
    source.mkdir()
    args = ["git", "ls-files", "-z", "--cached"]
    if working_source:
        args += ["--others", "--exclude-standard"]
    files = set(
        subprocess.check_output(args, cwd=ROOT).decode().strip("\0").split("\0")
    )
    inventory = {}
    for name in sorted(files):
        relative = Path(name)
        if (
            relative.is_absolute()
            or ".." in relative.parts
            or any(
                p in {".git", "node_modules", "target", "output", "dist", "__pycache__"}
                for p in relative.parts
            )
            or relative.name == ".env"
            or relative.suffix in {".onnx", ".pyc"}
        ):
            raise RuntimeError("unexpected source path: " + name)
        origin = ROOT / relative
        if not origin.exists():
            continue
        if origin.is_symlink():
            raise RuntimeError("symlinks are not accepted in clean source")
        destination = source / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(origin, destination)
        inventory[name] = hashlib.sha256(origin.read_bytes()).hexdigest()
    (output / "source-manifest.json").write_text(
        json.dumps(
            {
                "schema_version": 1,
                "source_mode": "working-source-including-untracked"
                if working_source
                else "tracked-source",
                "commit": subprocess.check_output(
                    ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True
                ).strip(),
                "files": inventory,
            },
            indent=2,
        )
    )
    # Host prerequisites: Git, Python, Node and Docker. App dependencies build only in images.
    command(
        [
            "node",
            "--no-warnings",
            "--experimental-strip-types",
            "scripts/fetch-model.ts",
        ],
        cwd=source,
    )
    gateway_port = port()
    project = "ps-clean-" + uuid.uuid4().hex[:12]
    envfile = output / "installation.env"
    settings = {
        "ENVIRONMENT": "development",
        "PUBLIC_ORIGIN": f"http://127.0.0.1:{gateway_port}",
        "COMBINED_PUBLIC_ORIGIN": f"http://127.0.0.1:{gateway_port}",
        "OPERATOR_PASSWORD": secrets.token_hex(24),
        "ENGINE_TOKEN": secrets.token_hex(32),
        "POSTGRES_PASSWORD": secrets.token_hex(24),
        "NER_ENABLED": "true",
        "OPENAI_API_KEY": "",
        "ANTHROPIC_API_KEY": "",
        "GEMINI_API_KEY": "",
        "METRICS_TOKEN": "",
        "ORGANIZATION_ID": "community",
    }
    envfile.write_text("\n".join(k + "=" + v for k, v in settings.items()) + "\n")
    override = output / "ports.yml"
    override.write_text(
        f'services:\n  gateway:\n    ports: !override ["127.0.0.1:{gateway_port}:8080"]\n'
    )
    env = os.environ.copy()
    env.update(settings)
    compose = [
        "docker",
        "compose",
        "--env-file",
        envfile,
        "-p",
        project,
        "-f",
        source / "deploy/docker-compose.yml",
        "-f",
        source / "deploy/compose.combined.yml",
        "-f",
        override,
    ]
    try:
        with (output / "build.log").open("wb") as log:
            command(
                compose + ["build", "--no-cache"],
                env=env,
                stdout=log,
                stderr=subprocess.STDOUT,
            )
        command(compose + ["up", "-d", "--wait", "--wait-timeout", "180"], env=env)
        env["SMOKE_BASE_URL"] = settings["PUBLIC_ORIGIN"]
        with (output / "smoke.json").open("wb") as stream:
            command(
                [
                    "node",
                    "--no-warnings",
                    "--experimental-strip-types",
                    "scripts/smoke.ts",
                ],
                cwd=source,
                env=env,
                stdout=stream,
            )
        smoke = json.loads((output / "smoke.json").read_text())
        if smoke.get("ner") != "passed":
            raise RuntimeError("clean install did not exercise NER")
        resources = {}
        for service in ("engine", "gateway"):
            identity = subprocess.check_output(
                [str(a) for a in compose + ["ps", "-q", service]], env=env, text=True
            ).strip()
            # Read kernel counters from this invocation's containers, not user installations.
            counters = subprocess.check_output(
                [
                    "docker",
                    "run",
                    "--rm",
                    "--pid=container:" + identity,
                    "--cap-drop=ALL",
                    "--cap-add=SYS_PTRACE",
                    "--security-opt",
                    "no-new-privileges:true",
                    "debian:bookworm-slim",
                    "cat",
                    "/proc/1/root/sys/fs/cgroup/memory.current",
                    "/proc/1/root/sys/fs/cgroup/memory.peak",
                ],
                text=True,
            ).splitlines()
            if len(counters) != 2:
                raise RuntimeError("container memory counters unavailable")
            resources[service] = {
                "container_id": identity,
                "cgroup_memory_current_bytes": int(counters[0]),
                "cgroup_memory_peak_bytes": int(counters[1]),
                "scope": "installation and functional smoke; not sustained load",
            }
        (output / "resources.json").write_text(json.dumps(resources, indent=2))
        (output / "result.json").write_text(
            json.dumps(
                {
                    "schema_version": 1,
                    "passed": True,
                    "source_files": len(inventory),
                    "source_mode": "working-source-including-untracked"
                    if working_source
                    else "tracked-source",
                    "preexisting_app_dependencies": False,
                    "ner": "passed",
                },
                indent=2,
            )
        )
    finally:
        with (output / "compose.log").open("wb") as log:
            command(
                compose + ["logs", "--no-color"],
                env=env,
                stdout=log,
                stderr=subprocess.STDOUT,
            )
        # Named volumes belong exclusively to this random project and contain synthetic test data.
        command(compose + ["down", "--volumes", "--remove-orphans"], env=env)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument(
        "--working-source",
        action="store_true",
        help="include declared nonignored new source before commit",
    )
    args = parser.parse_args()
    main(args.output.resolve(), args.working_source)
