"""Compact retained evidence for the manual performance plan."""

import argparse
import hashlib
import json
import subprocess
from pathlib import Path

from verification import ROOT


def read(path):
    return json.loads(path.read_text(encoding="utf-8"))


def summarize(path):
    report = read(path)
    environment = read(path.parent / "environment.json")
    resource = read(path.parent / "resources-0.json")
    samples = resource["samples"]
    # Early reference measurements used the raw cgroup representation.
    for sample in samples:
        if isinstance(sample["cgroup"], str):
            values, memory = {}, []
            for line in sample["cgroup"].splitlines():
                parts = line.split()
                if len(parts) == 1:
                    memory.append(int(parts[0]))
                else:
                    values[parts[0]] = int(parts[1])
            values["memory_current"], values["memory_peak"] = memory
            sample["cgroup"] = values
    first, last = samples[0], samples[-1]
    elapsed = last["elapsed"] - first["elapsed"]
    cpu = last["cgroup"]["usage_usec"] - first["cgroup"]["usage_usec"]
    state = read(path.parent / "container-state.json")
    return {
        "path": path.parent.relative_to(ROOT).as_posix(),
        "report_sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
        "environment_sha256": hashlib.sha256(
            (path.parent / "environment.json").read_bytes()
        ).hexdigest(),
        "engine_image": environment["engine_image"],
        "bench_image": environment["bench_image"],
        "model_sha256": environment["model_sha256"],
        "tokenizer_sha256": environment["tokenizer_sha256"],
        "cases_sha256": environment["cases_sha256"],
        "sessions": environment["sessions"],
        "threads": environment["threads"],
        "spinning": environment["spinning"],
        "scenario": report["scenario"],
        "concurrency": report["concurrency"],
        "requests": report["requests"],
        "successful_requests": report["successful_requests"],
        "outcomes": report["outcomes"],
        "all_latency": report["all_latency"],
        "successful_latency": report["successful_latency"],
        "successful_rps": report["successful_rps"],
        "duration_seconds": report["duration_seconds"],
        "warmup_seconds": report["warmup_seconds"],
        "acceptance_passed": report["acceptance_passed"],
        "recovered": report["recovered"],
        "recovery_ms": report["recovery_ms"],
        "resource_errors": resource["errors"],
        "resource_samples": len(samples),
        "resource_scope": "cgroup samples include warm-up and measurement; CPU cores is usage divided by elapsed wall time",
        "mean_cpu_cores": cpu / 1000000 / elapsed if elapsed else None,
        "throttled_usec": last["cgroup"]["throttled_usec"]
        - first["cgroup"]["throttled_usec"],
        "nr_throttled": last["cgroup"]["nr_throttled"]
        - first["cgroup"]["nr_throttled"],
        "memory_peak_bytes": max(s["cgroup"]["memory_peak"] for s in samples),
        "first_memory_bytes": first["cgroup"]["memory_current"],
        "last_memory_bytes": last["cgroup"]["memory_current"],
        "oom": last["cgroup"]["oom"],
        "oom_kill": last["cgroup"]["oom_kill"],
        "container_oom_killed": state["OOMKilled"],
    }


def collect():
    output = ROOT / "output"
    reports = sorted(output.glob("performance-*/repeat-0.json"))
    reports += sorted(output.glob("performance-final-*/trial-*/repeat-0.json"))
    conditions = [
        summarize(path)
        for path in reports
        if (path.parent / "container-state.json").exists()
    ]
    source_snapshots = {}
    for path in reports:
        environment = read(path.parent / "environment.json")
        source_snapshots.setdefault(
            environment["engine_image"],
            {
                "revision": environment["commit"],
                "workspace_source_sha256": {
                    name: digest
                    for name, digest in environment["source_sha256"].items()
                    if name.startswith(("services/engine/", "proto/"))
                },
                "note": "Workspace fingerprints at measurement time. For the preserved pre-change baseline image, use the recorded source revision; its image was built before engine edits.",
            },
        )
        if path.parent.name == "performance-baseline":
            revision = environment["commit"]
            names = subprocess.check_output(
                [
                    "git",
                    "ls-tree",
                    "-r",
                    "--name-only",
                    revision,
                    "services/engine",
                    "proto",
                ],
                cwd=ROOT,
                text=True,
            ).splitlines()
            source_snapshots[environment["engine_image"]][
                "preserved_revision_source_sha256"
            ] = {
                name: hashlib.sha256(
                    subprocess.check_output(
                        ["git", "show", f"{revision}:{name}"], cwd=ROOT
                    )
                ).hexdigest()
                for name in names
                if not any(
                    part in {"target", "node_modules", ".git"}
                    for part in Path(name).parts
                )
            }
    candidates = {}
    for name in ("dslim-distilbert", "community-distilbert"):
        base = output / "performance-candidates" / name
        conversion = read(base / "conversion.json")
        candidates[name] = {
            "conversion": {
                key: conversion[key]
                for key in ("source_sha256", "output_sha256", "reproducible")
            },
            "corpora": [read(base / f"comparison-{i}.json") for i in (0, 1)],
        }
    full = [row for row in conditions if "performance-final-" in row["path"]]
    groups = {}
    for row in full:
        groups.setdefault(
            (row["sessions"], row["threads"], row["spinning"]), []
        ).append(row)
    eligible = [
        rows
        for rows in groups.values()
        if len(rows) == 3 and all(row["acceptance_passed"] for row in rows)
    ]
    eligible.sort(
        key=lambda rows: (
            max(row["memory_peak_bytes"] for row in rows),
            max(row["all_latency"]["p99_ms"] for row in rows),
        )
    )
    return {
        "schema_version": 1,
        "conditions": conditions,
        "source_snapshots": source_snapshots,
        "candidates": candidates,
        "candidate_workloads": read(
            output / "performance-candidates/workload-validation.json"
        ),
        "stage_profile": read(output / "performance-stage-profile.json"),
        "complete_final_repetitions": len(groups) == 3
        and all(len(rows) == 3 for rows in groups.values()),
        "latency_accepted_configuration": [
            {
                "sessions": rows[0]["sessions"],
                "threads": rows[0]["threads"],
                "spinning": rows[0]["spinning"],
            }
            for rows in eligible[:1]
        ],
        "default_promotion_requires_all_gates": True,
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    report = collect()
    args.output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(
        f"retained {len(report['conditions'])} conditions; nine final repetitions complete={report['complete_final_repetitions']}"
    )
