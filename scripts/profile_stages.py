"""Separate Linux stage instrumentation; never used as latency acceptance."""

import argparse
import json
import math
import subprocess
from pathlib import Path

from verification import ROOT


def profile(output):
    result = subprocess.run(
        [
            "docker",
            "run",
            "--rm",
            "--cpuset-cpus",
            "0-7",
            "--cpus",
            "8",
            "--memory",
            "1g",
            "--memory-swap",
            "1g",
            "--mount",
            f"type=bind,src={ROOT / 'models'},dst=/source/models,readonly",
            "--mount",
            f"type=bind,src={ROOT / 'output/performance-cases.json'},dst=/cases.json,readonly",
            "-e",
            "ONNX_MODEL_PATH=/source/models/general-ner/model.onnx",
            "-e",
            "TOKENIZER_CONFIG_PATH=/source/models/general-ner/tokenizer.json",
            "ps-perf-tests:local",
            "cargo",
            "run",
            "--locked",
            "--release",
            "-j",
            "1",
            "--features",
            "evaluation,profiling",
            "--bin",
            "allocation-profile",
            "--",
            "/cases.json",
            "ner-32",
            "100",
        ],
        cwd=ROOT,
        capture_output=True,
        text=True,
        check=True,
    )
    stages = {}
    for line in result.stderr.splitlines():
        if line.startswith("profile stage="):
            _, stage, nanos = line.split()
            stages.setdefault(stage.split("=")[1], []).append(
                int(nanos.split("=")[1]) / 1000
            )
    expected = {
        "prefilter",
        "tokenization",
        "session_wait",
        "tensor_prepare",
        "onnx",
        "span_decode",
        "merge_mask",
        "signing",
    }
    if set(stages) != expected or any(len(values) != 120 for values in stages.values()):
        raise ValueError("incomplete stage profile")
    summary = {}
    for name, values in stages.items():
        values.sort()
        summary[name] = {
            "samples": len(values),
            "mean_us": sum(values) / len(values),
            "p50_us": values[math.ceil(len(values) * 0.5) - 1],
            "p99_us": values[math.ceil(len(values) * 0.99) - 1],
        }
    report = {
        "scope": "Separate Linux instrumented public-service dispatch, serial calls; includes 20 warm-ups and 100 measured calls. Not a gRPC acceptance or contention measurement.",
        "stages": summary,
        "allocation": json.loads(result.stdout.strip().splitlines()[-1]),
    }
    output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(summary), flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    profile(parser.parse_args().output)
