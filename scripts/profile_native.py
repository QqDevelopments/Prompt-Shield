"""Native malloc/free attribution in a separate Linux profiling container."""

import argparse
import json
import subprocess
import uuid
from pathlib import Path

from verification import ROOT, command


def summarize(output):
    for case in ("clean-64", "ner-32", "ner-512"):
        raw = output / ("run-" + case + ".txt")
        result_path = output / ("rust-" + case + ".json")
        if not raw.exists():
            # Preserve mixed stdout from older heaptrack invocations before extracting JSON.
            raw.write_bytes(result_path.read_bytes())
        records = []
        for line in raw.read_text().splitlines():
            if line.startswith("{"):
                record = json.loads(line)
                if record.get("schema_version") == 1 and record.get("case") == case:
                    records.append(record)
        if len(records) != 1:
            raise RuntimeError("missing or ambiguous Rust allocation result: " + case)
        result_path.write_text(json.dumps(records[0], indent=2))


def main(workloads, output):
    output.mkdir(parents=True, exist_ok=False)
    image = "ps-allocation-" + uuid.uuid4().hex[:12]
    with (output / "build.txt").open("wb") as log:
        command(
            ["docker", "build", "-f", "deploy/Dockerfile.profile", "-t", image, "."],
            stdout=log,
            stderr=subprocess.STDOUT,
        )
    model = ROOT / "models/general-ner"
    for case in ("clean-64", "ner-32", "ner-512"):
        args = [
            "docker",
            "run",
            "--rm",
            "--mount",
            f"type=bind,source={model},target=/models,readonly",
            "--mount",
            f"type=bind,source={workloads},target=/workloads.json,readonly",
            "--mount",
            f"type=bind,source={output},target=/reports",
            "-e",
            "ONNX_MODEL_PATH=/models/model.onnx",
            "-e",
            "TOKENIZER_CONFIG_PATH=/models/tokenizer.json",
            image,
            "/bin/sh",
            "-c",
            "heaptrack -o /reports/heaptrack-"
            + case
            + " /allocation-profile /workloads.json "
            + case
            + " 100 > /reports/run-"
            + case
            + ".txt && heaptrack_print /reports/heaptrack-"
            + case
            + ".gz > /reports/native-"
            + case
            + ".txt",
        ]
        command(args)
    summarize(output)
    # heaptrack captures process malloc/free, including ONNX, not only Rust's allocator.
    # Reports include startup and 20 warm-up + 100 measured requests; timing is not a latency gate.
    report = {
        "schema_version": 1,
        "passed": True,
        "profiler": "heaptrack",
        "scope": "whole Linux process native allocator including ONNX; startup and warm-up included",
        "cases": ["clean-64", "ner-32", "ner-512"],
        "measured_requests_per_case": 100,
        "warmup_requests_per_case": 20,
        "production_instrumentation": False,
        "image_id": subprocess.check_output(
            ["docker", "image", "inspect", image, "--format", "{{.Id}}"], text=True
        ).strip(),
    }
    for path in output.glob("native-*.txt"):
        if not path.stat().st_size:
            raise RuntimeError("empty native allocation report")
    (output / "result.json").write_text(json.dumps(report, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--workloads", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    main(args.workloads.resolve(), args.output.resolve())
