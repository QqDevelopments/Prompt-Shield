"""Manual Docker reference measurement; never equates shared CI with acceptance."""

import argparse
import hashlib
import json
import subprocess
import threading
import time
import uuid
from pathlib import Path

from verification import ROOT, metadata


P99_TARGET_MS = 50


def run(*args):
    return subprocess.check_output(
        [str(a) for a in args], cwd=ROOT, text=True, timeout=1200
    ).strip()


def write(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def cgroup(identity):
    lines = run(
        "docker",
        "exec",
        identity,
        "sh",
        "-c",
        "cat /sys/fs/cgroup/cpu.stat /sys/fs/cgroup/memory.current /sys/fs/cgroup/memory.peak /sys/fs/cgroup/memory.events",
    ).splitlines()
    values, memory = {}, []
    for line in lines:
        parts = line.split()
        if len(parts) == 1:
            memory.append(int(parts[0]))
        else:
            values[parts[0]] = int(parts[1])
    values["memory_current"], values["memory_peak"] = memory
    return values


def accepted(report, samples, errors, args):
    """Accept complete, error-free reference data independently of latency goals."""
    return (
        args.scenario == "ner-32"
        and args.cancel_percent == 0
        and report["warmup_seconds"] >= 30
        and report["duration_seconds"] >= 120
        and report["concurrency"] == 4
        and report["requests"] >= 10000
        and report["requests"] == report["successful_requests"]
        and report["recovered"]
        and bool(report["inputs"])
        and all(
            row["contextual"]
            and row["expected"] == "masked"
            and 0 < row["tokens"] <= 32
            for row in report["inputs"]
        )
        and not errors
        and bool(samples)
        and all(
            s["cgroup"]["oom"] == 0
            and s["cgroup"]["oom_kill"] == 0
            and s["cgroup"]["memory_peak"] <= 1073741824
            for s in samples
        )
    )


def evaluation(report, samples, errors, args):
    reference_accepted = accepted(report, samples, errors, args)
    return {
        "acceptance_passed": reference_accepted,
        "p99_target_ms": P99_TARGET_MS,
        "p99_target_required": False,
        "p99_target_met": (
            report["all_latency"]["p99_ms"] < P99_TARGET_MS
            if reference_accepted
            else None
        ),
    }


def measure(args):
    destination = args.output.resolve()
    destination.mkdir(parents=True, exist_ok=False)
    identity = "ps-perf-" + uuid.uuid4().hex[:12]
    token = uuid.uuid4().hex + uuid.uuid4().hex
    model = ROOT / "models/general-ner"
    cases = args.cases.resolve()
    evidence = metadata()
    evidence.update(
        {
            "engine_image": run(
                "docker", "image", "inspect", args.image, "--format", "{{.Id}}"
            ),
            "bench_image": run(
                "docker", "image", "inspect", args.bench_image, "--format", "{{.Id}}"
            ),
            "docker_info": json.loads(run("docker", "info", "--format", "{{json .}}")),
            "model_sha256": hashlib.file_digest(
                (model / "model.onnx").open("rb"), "sha256"
            ).hexdigest(),
            "tokenizer_sha256": hashlib.file_digest(
                (model / "tokenizer.json").open("rb"), "sha256"
            ).hexdigest(),
            "cases_sha256": hashlib.sha256(cases.read_bytes()).hexdigest(),
            "sessions": args.sessions,
            "threads": args.threads,
            "spinning": args.spinning,
            "engine_cpus": "0-7",
            "load_cpus": "8-15",
            "memory_limit": 1073741824,
            "profiling": False,
            "p99_target_ms": P99_TARGET_MS,
            "p99_target_required": False,
        }
    )
    write(destination / "environment.json", evidence)
    network = False
    started = False
    try:
        run("docker", "network", "create", identity)
        network = True
        run(
            "docker",
            "run",
            "-d",
            "--name",
            identity,
            "--network",
            identity,
            "--network-alias",
            "engine",
            "--cpuset-cpus",
            "0-7",
            "--cpus",
            "8",
            "--memory",
            "1g",
            "--memory-swap",
            "1g",
            "--mount",
            f"type=bind,src={model},dst=/models,readonly",
            "-e",
            f"ENGINE_TOKEN={token}",
            "-e",
            "NER_ENABLED=true",
            "-e",
            "ONNX_MODEL_PATH=/models/model.onnx",
            "-e",
            "TOKENIZER_CONFIG_PATH=/models/tokenizer.json",
            "-e",
            f"NER_SESSION_COUNT={args.sessions}",
            "-e",
            f"NER_INTRA_THREADS={args.threads}",
            "-e",
            f"NER_SPINNING={args.spinning}",
            args.image,
        )
        started = True
        deadline = time.monotonic() + 90
        while "listening" not in run("docker", "logs", identity):
            if (
                time.monotonic() > deadline
                or run("docker", "inspect", identity, "--format", "{{.State.Running}}")
                != "true"
            ):
                raise RuntimeError("engine failed startup")
            time.sleep(0.25)
        for repeat in range(args.repeats):
            halt = threading.Event()
            samples, errors = [], []

            def sample(halt, samples, errors):
                while not halt.is_set():
                    try:
                        samples.append(
                            {"elapsed": time.monotonic(), "cgroup": cgroup(identity)}
                        )
                    except (
                        subprocess.CalledProcessError,
                        subprocess.TimeoutExpired,
                        ValueError,
                        OSError,
                    ):
                        errors.append("resource_sample_failed")
                    halt.wait(0.5)

            sampler = threading.Thread(target=sample, args=(halt, samples, errors))
            sampler.start()
            try:
                report = json.loads(
                    run(
                        "docker",
                        "run",
                        "--rm",
                        "--network",
                        identity,
                        "--cpuset-cpus",
                        "8-15",
                        "--mount",
                        f"type=bind,src={cases},dst=/cases.json,readonly",
                        "-e",
                        "RUST_ENGINE_ADDR=engine:50051",
                        "-e",
                        f"ENGINE_TOKEN={token}",
                        args.bench_image,
                        "-cases",
                        "/cases.json",
                        "-scenario",
                        args.scenario,
                        "-concurrency",
                        "4",
                        "-warmup",
                        f"{args.warmup}s",
                        "-duration",
                        f"{args.duration}s",
                        "-deadline",
                        "10s",
                        "-cancel-percent",
                        args.cancel_percent,
                        "-cancel-after",
                        "1ms",
                    )
                )
            except subprocess.CalledProcessError as error:
                # The benchmark can return a complete failure report on nonzero
                # exit. Preserve it, including OOM/capacity failures, before cleanup.
                if error.output:
                    try:
                        failure = json.loads(error.output)
                        failure["acceptance_passed"] = False
                        failure["p99_target_ms"] = P99_TARGET_MS
                        failure["p99_target_required"] = False
                        failure["p99_target_met"] = None
                        write(destination / f"repeat-{repeat}.json", failure)
                    except json.JSONDecodeError:
                        pass
                raise
            finally:
                halt.set()
                sampler.join()
                write(
                    destination / f"resources-{repeat}.json",
                    {"samples": samples, "errors": errors},
                )
            report.update(evaluation(report, samples, errors, args))
            write(destination / f"repeat-{repeat}.json", report)
            write(
                destination / f"resources-{repeat}.json",
                {"samples": samples, "errors": errors},
            )
            print(
                f"repeat={repeat} requests={report['requests']} failures={report['requests'] - report['successful_requests']} p99={report['all_latency']['p99_ms']:.3f}ms",
                flush=True,
            )
    finally:
        if started:
            write(
                destination / "container-state.json",
                json.loads(
                    run("docker", "inspect", identity, "--format", "{{json .State}}")
                ),
            )
            run("docker", "rm", "-f", identity)
        if network:
            run("docker", "network", "rm", identity)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--image", required=True)
    parser.add_argument("--bench-image", default="ps-perf-bench:local")
    parser.add_argument("--cases", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--sessions", type=int, default=1)
    parser.add_argument("--threads", type=int, default=4)
    parser.add_argument("--spinning", choices=("true", "false"), default="true")
    parser.add_argument("--scenario", default="ner-32")
    parser.add_argument("--warmup", type=int, default=30)
    parser.add_argument("--duration", type=int, default=120)
    parser.add_argument("--repeats", type=int, default=3)
    parser.add_argument("--cancel-percent", type=int, default=0)
    args = parser.parse_args()
    if args.repeats < 1 or args.warmup < 0 or args.duration < 1:
        parser.error("positive repeats/duration and nonnegative warmup required")
    if args.repeats == 1:
        measure(args)
    else:
        # Each independent repeat starts a fresh process, pool, and cgroup.
        for index in range(args.repeats):
            trial = argparse.Namespace(**vars(args))
            trial.repeats = 1
            trial.output = args.output / f"trial-{index}"
            measure(trial)
