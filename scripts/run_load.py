"""Isolated native service load matrix with real disposable PostgreSQL/Redis."""

import argparse
import hashlib
import json
import os
import subprocess
import threading
import time
import uuid
from pathlib import Path

from verification import (
    ROOT,
    cargo_binary,
    command,
    fixture_stats,
    memory,
    metadata,
    port,
    stop,
    wait_port,
)


def run(profile, destination):
    destination.mkdir(parents=True, exist_ok=False)
    env = os.environ.copy()
    engine_port, database_port, redis_port = port(), port(), port()
    project = "ps-load-" + uuid.uuid4().hex[:12]
    override = destination / "dependencies.json"
    # JSON is accepted by Compose; unique ports and project-owned tmpfs data isolate the run.
    override.write_text(
        json.dumps(
            {
                "services": {
                    "postgres": {
                        "image": "postgres:17-alpine",
                        "environment": {
                            "POSTGRES_DB": "promptshield",
                            "POSTGRES_USER": "promptshield",
                            "POSTGRES_PASSWORD": "synthetic-load-password",
                        },
                        "ports": [f"127.0.0.1:{database_port}:5432"],
                        "tmpfs": ["/var/lib/postgresql/data"],
                        "healthcheck": {
                            "test": [
                                "CMD-SHELL",
                                "pg_isready -U promptshield -d promptshield",
                            ],
                            "interval": "2s",
                            "retries": 30,
                        },
                    },
                    "redis": {
                        "image": "redis:7-alpine",
                        "ports": [f"127.0.0.1:{redis_port}:6379"],
                        "command": ["redis-server", "--save", "", "--appendonly", "no"],
                        "healthcheck": {
                            "test": ["CMD", "redis-cli", "ping"],
                            "interval": "2s",
                            "retries": 30,
                        },
                    },
                }
            }
        )
    )
    compose = ["docker", "compose", "-p", project, "-f", override]
    env.update(
        {
            "TEST_DATABASE_URL": f"postgres://promptshield:synthetic-load-password@127.0.0.1:{database_port}/promptshield?sslmode=disable",
            "TEST_REDIS_URL": f"redis://127.0.0.1:{redis_port}/0",
            "ENGINE_LISTEN": f"127.0.0.1:{engine_port}",
            "RUST_ENGINE_ADDR": f"127.0.0.1:{engine_port}",
            "ENGINE_TOKEN": "synthetic-load-engine-token-not-a-real-secret",
            "NER_ENABLED": "true",
            "ONNX_MODEL_PATH": str(ROOT / "models/general-ner/model.onnx"),
            "TOKENIZER_CONFIG_PATH": str(ROOT / "models/general-ner/tokenizer.json"),
        }
    )
    command(
        [
            "node",
            "--no-warnings",
            "--experimental-strip-types",
            "scripts/fetch-model.ts",
        ]
    )
    engine = cargo_binary("prompt-shield-engine", "ner")
    evaluator = cargo_binary("ner-evaluate")
    allocator = cargo_binary("allocation-profile")
    cancellation_observer = cargo_binary("cancellation-profile")
    suffix = ".exe" if os.name == "nt" else ""
    bench, fixture = (
        destination / ("bench" + suffix),
        destination / ("fixture" + suffix),
    )
    for path, name in ((bench, "bench"), (fixture, "bench-fixture")):
        command(
            ["go", "build", "-o", path, "./cmd/" + name], cwd=ROOT / "services/gateway"
        )
    cases = destination / "workloads.json"
    with cases.open("wb") as stream:
        command(
            [evaluator, "workloads", ROOT / "models/general-ner/tokenizer.json"],
            stdout=stream,
        )
    description = metadata()
    description.update(
        {
            "profile": profile,
            "service_cpu_limit": "unrestricted native processes",
            "service_ram_limit": "host limit",
            "model_intra_threads": 4,
            "engine_inspection_permits": 4,
            "gateway_policy": "unchanged 120 requests/minute per organization and key",
            "upstream": "synthetic echo, 20ms delay after first SSE event or before ordinary response",
        }
    )
    (destination / "environment.json").write_text(json.dumps(description, indent=2))
    environment_hash = hashlib.sha256(
        (destination / "environment.json").read_bytes()
    ).hexdigest()
    processes, files, measurements, matrix = [], [], [], []
    completed = False
    try:
        command(compose + ["up", "-d", "--wait"])
        log = (destination / "engine.log").open("wb")
        files.append(log)
        engine_process = subprocess.Popen(
            [engine], cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT
        )
        processes.append(engine_process)
        wait_port(engine_port, engine_process)
        # Each HTTP case gets new organizations/keys; production limiter/quota logic remains active.
        if profile == "full":
            matrix = [
                (transport, scenario, concurrency, 120, 30, 0)
                for transport, scenarios in [
                    (
                        "grpc",
                        [
                            f"{kind}-{size}"
                            for kind in ("clean", "masked", "blocked")
                            for size in (64, 1024, 16384, 131072)
                        ]
                        + ["ner-32", "ner-128", "ner-512", "mixed"],
                    ),
                    ("http", ["mixed"]),
                    ("sse", ["mixed"]),
                ]
                for scenario in scenarios
                for concurrency in (1, 4, 16)
            ]
            matrix += [("grpc", "mixed", 4, 900, 30, 0)]
        else:
            matrix = [
                ("grpc", s, 4, 3, 1, 0)
                for s in (
                    "clean-64",
                    "masked-1024",
                    "blocked-16384",
                    "ner-32",
                    "ner-512",
                    "mixed",
                )
            ]
            matrix += [(t, "mixed", 4, 3, 0, 0) for t in ("http", "sse")]
        matrix += [
            (
                t,
                "ner-128" if t == "grpc" else "mixed",
                4,
                120 if profile == "full" else 3,
                0,
                10,
            )
            for t in ("grpc", "http", "sse")
        ]
        matrix += [("grpc", s, 1, 1, 0, 0) for s in ("rules-over-limit", "ner-513")]
        for number, (
            transport,
            scenario,
            concurrency,
            duration,
            warmup,
            cancel,
        ) in enumerate(matrix):
            if (destination / "STOP").exists():
                raise InterruptedError("stop requested; existing measurements retained")
            name = f"{number:03d}-{transport}-{scenario}-c{concurrency}-cancel{cancel}"
            fixture_process, connection, before = None, None, None
            if transport != "grpc":
                connection_file = destination / (name + "-connection.json")
                log = (destination / (name + "-fixture.log")).open("wb")
                files.append(log)
                fixture_process = subprocess.Popen(
                    [fixture, "-output", connection_file, "-upstream-delay", "20ms"],
                    cwd=ROOT,
                    env=env,
                    stdout=log,
                    stderr=subprocess.STDOUT,
                )
                processes.append(fixture_process)
                deadline = time.monotonic() + 60
                while not connection_file.exists():
                    if (
                        fixture_process.poll() is not None
                        or time.monotonic() > deadline
                    ):
                        raise RuntimeError("HTTP fixture failed startup")
                    time.sleep(0.1)
                connection = json.loads(connection_file.read_text())
                before = fixture_stats(connection)
            samples, sampling_errors, halt = [], [], threading.Event()

            def sample(halt, samples, sampling_errors, fixture_process):
                while not halt.is_set():
                    try:
                        entry = {
                            "time_monotonic": time.monotonic(),
                            "engine": memory(engine_process.pid),
                        }
                        if fixture_process:
                            entry["gateway_fixture"] = memory(fixture_process.pid)
                        samples.append(entry)
                    except (OSError, RuntimeError) as error:
                        sampling_errors.append(type(error).__name__)
                    halt.wait(0.5)

            sampler = threading.Thread(
                target=sample, args=(halt, samples, sampling_errors, fixture_process)
            )
            sampler.start()
            report_path = destination / (name + ".json")
            args = [
                bench,
                "-cases",
                cases,
                "-scenario",
                scenario,
                "-transport",
                transport,
                "-concurrency",
                concurrency,
                "-duration",
                f"{duration}s",
                # Long contextual inputs can queue within the bounded NER pool.
                # Allow slower runners without changing gateway timeouts.
                "-deadline",
                "10s" if transport == "grpc" else "1s",
                "-warmup",
                f"{warmup}s",
                "-cancel-percent",
                cancel,
                "-cancel-after",
                "10ms" if transport != "grpc" else "1ms",
                "-output",
                report_path,
            ]
            if connection:
                args += ["-fixture", connection_file]
            try:
                result = subprocess.run(
                    [str(a) for a in args], cwd=ROOT, env=env, check=False
                )
            finally:
                halt.set()
                sampler.join()
            if report_path.exists():
                report = json.loads(report_path.read_text())
                report["environment_file"] = "environment.json"
                report["environment_sha256"] = environment_hash
                report["resource_report"] = name + "-resources.json"
                report_path.write_text(json.dumps(report, indent=2))
            resources = {
                "schema_version": 1,
                "samples": samples,
                "sampling_errors": sampling_errors,
                "sampling_interval_ms": 500,
                "scope": "service processes; load generator excluded",
            }
            if connection:
                after = fixture_stats(connection)
                deadline = time.monotonic() + 10
                while after["active_upstream"] and time.monotonic() < deadline:
                    time.sleep(0.05)
                    after = fixture_stats(connection)
                resources["gateway_before"] = before
                resources["gateway_after"] = after
                resources["go_allocated_bytes"] = (
                    after["memory"]["TotalAlloc"] - before["memory"]["TotalAlloc"]
                )
                resources["go_allocations"] = (
                    after["memory"]["Mallocs"] - before["memory"]["Mallocs"]
                )
                resources["go_gc_cycles"] = (
                    after["memory"]["NumGC"] - before["memory"]["NumGC"]
                )
                if after["active_upstream"] or after["privacy_failures"]:
                    raise RuntimeError(
                        "upstream did not drain or privacy boundary failed"
                    )
                stop(fixture_process)
            (destination / (name + "-resources.json")).write_text(
                json.dumps(resources, indent=2)
            )
            measurements.append({"name": name, "exit_code": result.returncode})
            (destination / "run-summary.json").write_text(
                json.dumps(
                    {
                        "schema_version": 1,
                        "state": "in_progress",
                        "planned_conditions": len(matrix),
                        "measurements": measurements,
                    },
                    indent=2,
                )
            )
            print(
                f"{name}: exit={result.returncode}, resource_samples={len(samples)}",
                flush=True,
            )
            if result.returncode or sampling_errors or not samples:
                raise RuntimeError("load or resource measurement failed: " + name)
        # Instrumentation runs separately after latency measurements.
        with (destination / "cancellation-dispatch.json").open("wb") as stream:
            command([cancellation_observer], env=env, stdout=stream)
        for case in ("clean-64", "masked-1024", "blocked-16384", "ner-32", "ner-512"):
            with (destination / ("allocation-" + case + ".json")).open("wb") as stream:
                command([allocator, cases, case, "100"], env=env, stdout=stream)
        with (destination / "go-benchmarks.txt").open("wb") as stream:
            command(
                ["go", "test", "-run=^$", "-bench=.", "-benchmem", "./internal/api"],
                cwd=ROOT / "services/gateway",
                stdout=stream,
            )
        completed = True
    finally:
        for process in reversed(processes):
            stop(process)
        for stream in files:
            stream.close()
        # Only the unique project created by this invocation is stopped; tmpfs data is disposable.
        command(compose + ["down", "--remove-orphans"])
        (destination / "run-summary.json").write_text(
            json.dumps(
                {
                    "schema_version": 1,
                    "state": "completed" if completed else "incomplete",
                    "planned_conditions": len(matrix),
                    "measurements": measurements,
                },
                indent=2,
            )
        )


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--profile", choices=("smoke", "full"), default="smoke")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    run(args.profile, args.output.resolve())
