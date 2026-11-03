"""Validate and compact load artifacts without losing failures or measurement scope."""

import argparse
import hashlib
import json
import math
import time
from pathlib import Path


def read(path):
    return json.loads(path.read_text(encoding="utf-8"))


def require(condition, message):
    if not condition:
        raise ValueError(message)


def summarize(directory, allow_incomplete=False):
    summary = read(directory / "run-summary.json")
    environment_path = directory / "environment.json"
    environment = read(environment_path)
    environment_hash = hashlib.sha256(environment_path.read_bytes()).hexdigest()
    measurements = summary["measurements"]
    names = [item["name"] for item in measurements]
    require(len(set(names)) == len(names), "duplicate condition")
    require(len(names) <= summary["planned_conditions"], "too many conditions")
    complete = summary["state"] == "completed"
    if complete:
        require(len(names) == summary["planned_conditions"], "missing conditions")
    require(
        complete or allow_incomplete,
        "run is incomplete; use --allow-incomplete for a snapshot",
    )
    rows = []
    for item in measurements:
        name = item["name"]
        require(
            Path(name).name == name and "/" not in name and "\\" not in name,
            "invalid condition name",
        )
        report = read(directory / (name + ".json"))
        require(
            report["environment_sha256"] == environment_hash,
            name + ": environment changed",
        )
        require(
            report["resource_report"] == name + "-resources.json",
            name + ": unexpected resource path",
        )
        resources = read(directory / report["resource_report"])
        require(item["exit_code"] == 0, name + ": failed benchmark")
        require(report["recovered"], name + ": failed recovery")
        require(
            resources["samples"] and not resources["sampling_errors"],
            name + ": missing resource samples",
        )
        total, good = report["requests"], report["successful_requests"]
        outcomes = report["outcomes"]
        require(
            total > 0 and sum(outcomes.values()) == total,
            name + ": invalid outcome counts",
        )
        require(
            sum(report["actual_mix"].values()) == total,
            name + ": invalid policy counts",
        )
        expected_good = sum(
            outcomes.get(k, 0) for k in ("ok", "expected_block", "expected_rejection")
        )
        require(good == expected_good, name + ": invalid success count")
        require(
            report["all_latency"]["count"] == total,
            name + ": invalid all-request histogram",
        )
        require(
            report["successful_latency"]["count"] == good,
            name + ": invalid success histogram",
        )
        require(report["duration_seconds"] > 0, name + ": invalid duration")
        for field, expected in (
            ("error_rate", (total - good - outcomes.get("canceled", 0)) / total),
            ("unsuccessful_rate", (total - good) / total),
            ("attempted_rps", total / report["duration_seconds"]),
            ("successful_rps", good / report["duration_seconds"]),
        ):
            require(
                math.isclose(report[field], expected, rel_tol=1e-9, abs_tol=1e-12),
                name + ": inconsistent " + field,
            )
        row = {
            key: report[key]
            for key in (
                "transport",
                "scenario",
                "concurrency",
                "inputs",
                "actual_mix",
                "warmup_seconds",
                "duration_seconds",
                "requests",
                "successful_requests",
                "outcomes",
                "attempted_rps",
                "successful_rps",
                "error_rate",
                "unsuccessful_rate",
                "all_latency",
                "successful_latency",
                "cancellation_latency",
                "cancellation_selected",
                "cancellation_signaled",
                "cancel_after_ms",
                "recovered",
                "recovery_ms",
                "measurement_clock",
                "clock_frequency_hz",
                "deadline_ms",
            )
        }
        row.update(name=name, resource_samples=len(resources["samples"]), memory={})
        for process in ("engine", "gateway_fixture"):
            samples = [s[process] for s in resources["samples"] if process in s]
            if samples:
                row["memory"][process] = {
                    "first_rss_bytes": samples[0]["rss_bytes"],
                    "last_rss_bytes": samples[-1]["rss_bytes"],
                    "sampled_peak_rss_bytes": max(s["rss_bytes"] for s in samples),
                    "process_lifetime_peak_rss_bytes": max(
                        s["process_lifetime_peak_rss_bytes"] for s in samples
                    ),
                }
        if "gateway_after" in resources:
            after = resources["gateway_after"]
            require(
                after["active_upstream"] == 0 and after["privacy_failures"] == 0,
                name + ": upstream verification failed",
            )
            row["gateway_fixture"] = {
                key: resources[key]
                for key in ("go_allocated_bytes", "go_allocations", "go_gc_cycles")
            }
            row["gateway_fixture"]["after"] = {
                key: value for key, value in after.items() if key != "memory"
            }
        rows.append(row)
    result = {
        "schema_version": 1,
        "state": summary["state"],
        "complete": complete,
        "profile": environment["profile"],
        "planned_conditions": summary["planned_conditions"],
        "completed_conditions": len(rows),
        "environment_sha256": environment_hash,
        "environment": environment,
        "conditions": rows,
        "notes": [
            "Percentiles are per condition; never average percentiles across rows.",
            "Expected policy blocks/rejections count as successful decisions; HTTP/SSE deadlines, 429 and resource exhaustion do not.",
            "RSS samples include warm-up. Process lifetime peaks may precede the current condition.",
            "Go allocation/GC deltas include warm-up, polling, gateway and synthetic upstream.",
            "Rust allocation profiles exclude native ONNX allocations and run after latency measurement.",
            "Closed-loop load with synthetic padded inputs does not establish production workload capacity.",
        ],
    }
    if complete:
        cancellation = read(directory / "cancellation-dispatch.json")
        require(
            len(cancellation["samples"]) == cancellation["iterations"],
            "missing dispatch observations",
        )
        require(
            all(s["recovery_passed"] for s in cancellation["samples"]),
            "dispatch recovery failed",
        )
        result["cancellation_dispatch"] = cancellation
        result["rust_allocations"] = [
            read(directory / ("allocation-" + case + ".json"))
            for case in (
                "clean-64",
                "masked-1024",
                "blocked-16384",
                "ner-32",
                "ner-512",
            )
        ]
        benchmarks = directory / "go-benchmarks.txt"
        require(benchmarks.is_file(), "Go benchmark evidence missing")
        result["go_benchmarks"] = benchmarks.read_text(encoding="utf-8")
    return result


def markdown(report):
    env = report["environment"]
    ram = (
        f"{env['host_memory_bytes']} bytes"
        if "host_memory_bytes" in env
        else env.get("host_memory", "unavailable")
    )
    lines = [
        "# Load measurement report",
        "",
        f"State: {report['state']}; {report['completed_conditions']}/{report['planned_conditions']} conditions; profile: {report['profile']}.",
        "",
        f"Commit: {env['commit']}. Modified source fingerprints and exact input/policy counts are retained in the companion JSON.",
        "",
        f"OS: {env['os']}. CPU: {env['cpu']}; {env['logical_cpus']} logical processors. Host RAM: {ram}.",
        "",
        "All-request percentiles include capacity failures and cancellations. Successful throughput includes expected policy blocks and rejections. Rates are per second; latency is milliseconds; sampled engine RSS is MiB.",
        "",
        "| Condition | Seconds | Attempts/s | Successful/s | Error % | Unsuccessful % | All P50 | All P95 | All P99 | Success P99 | Engine peak RSS |",
        "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |",
    ]

    def number(value):
        return "n/a" if value is None else f"{value:.3f}"

    for row in report["conditions"]:
        latency = row["all_latency"]
        values = [
            row["duration_seconds"],
            row["attempted_rps"],
            row["successful_rps"],
            100 * row["error_rate"],
            100 * row["unsuccessful_rate"],
            latency["p50_ms"],
            latency["p95_ms"],
            latency["p99_ms"],
            row["successful_latency"]["p99_ms"],
            row["memory"]["engine"]["sampled_peak_rss_bytes"] / 1024**2,
        ]
        lines.append(
            "| " + row["name"] + " | " + " | ".join(number(v) for v in values) + " |"
        )
    lines += ["", "## Interpretation", ""] + ["- " + note for note in report["notes"]]
    return "\n".join(lines) + "\n"


def publish(result, output):
    output.parent.mkdir(parents=True, exist_ok=True)
    output.with_suffix(".json").write_text(
        json.dumps(result, indent=2) + "\n", encoding="utf-8"
    )
    output.with_suffix(".md").write_text(markdown(result), encoding="utf-8")
    print(
        f"{result['state']}: {result['completed_conditions']}/{result['planned_conditions']} conditions; {output}",
        flush=True,
    )


def follow(directory, output, timeout=14400):
    """Follow one existing finite run; never start or restart a load test."""
    deadline, previous = time.monotonic() + timeout, None
    while time.monotonic() < deadline:
        try:
            summary = read(directory / "run-summary.json")
        except json.JSONDecodeError:
            # The producer replaces the JSON contents between conditions.
            time.sleep(1)
            continue
        revision = (summary["state"], len(summary["measurements"]))
        if revision != previous:
            try:
                result = summarize(directory, allow_incomplete=True)
            except json.JSONDecodeError:
                time.sleep(1)
                continue
            publish(result, output)
            previous = revision
            if result["complete"]:
                return result
            require(result["state"] == "in_progress", "load run stopped incomplete")
        time.sleep(5)
    raise TimeoutError(
        "load report follow timed out; published snapshot remains incomplete"
    )


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("directory", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--allow-incomplete", action="store_true")
    parser.add_argument(
        "--follow",
        action="store_true",
        help="follow one active run for at most four hours",
    )
    args = parser.parse_args()
    if args.follow:
        follow(args.directory, args.output)
    else:
        publish(summarize(args.directory, args.allow_incomplete), args.output)
