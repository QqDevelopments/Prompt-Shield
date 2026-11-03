"""Pinned small-model evaluation using the production Rust wordpiece decoder."""

import argparse
import importlib.metadata
import json
import shutil
import subprocess
import tempfile
import urllib.request
from pathlib import Path

from ner_pipeline import CLASSES, ROOT, digest, environment, metrics, read, write
from verification import cargo_binary

SPEC = ROOT / "models/performance-candidates.json"


def fetch(directory):
    for candidate in read(SPEC)["candidates"]:
        target = directory / candidate["name"]
        target.mkdir(parents=True, exist_ok=True)
        for item in candidate["files"]:
            path = target / item["name"]
            if not path.exists() or digest(path) != item["sha256"]:
                url = f"https://huggingface.co/{candidate['repository']}/resolve/{candidate['revision']}/{item['path']}"
                temporary = path.with_suffix(path.suffix + ".part")
                with (
                    urllib.request.urlopen(url, timeout=120) as response,
                    temporary.open("wb") as out,
                ):
                    shutil.copyfileobj(response, out)
                if digest(temporary) != item["sha256"]:
                    raise ValueError("candidate integrity mismatch")
                temporary.replace(path)
            print(f"verified {candidate['name']} {item['name']}", flush=True)


def convert(directory, candidate):
    import onnx
    from onnxruntime.quantization import QuantType, quantize_dynamic

    for line in (ROOT / "scripts/model-requirements.txt").read_text().splitlines():
        if "==" in line:
            package, version = line.split("==")
            if importlib.metadata.version(package) != version:
                raise ValueError("locked model toolchain mismatch: " + package)
    for item in candidate["files"]:
        if digest(directory / item["name"]) != item["sha256"]:
            raise ValueError("candidate input changed")
    graph = onnx.load(str(directory / "fp32.onnx"))
    if [item.name for item in graph.graph.input] != candidate["inputs"]:
        raise ValueError("candidate input schema mismatch")
    del graph
    options = read(ROOT / "models/evaluation.json")["conversion"]
    hashes = []
    for name in ("candidate", "candidate-repeat"):
        with tempfile.TemporaryDirectory(prefix="ps-candidate-") as scratch:
            out = Path(scratch) / "model.onnx"
            quantize_dynamic(
                str(directory / "fp32.onnx"),
                str(out),
                weight_type=QuantType.QInt8,
                per_channel=options["per_channel"],
                reduce_range=options["reduce_range"],
                op_types_to_quantize=options["operators"],
                extra_options=options["extra_options"],
            )
            onnx.checker.check_model(str(out))
            shutil.copyfile(out, directory / (name + ".onnx"))
        hashes.append(digest(directory / (name + ".onnx")))
    write(
        directory / "conversion.json",
        {
            "environment": environment(),
            "options": options,
            "source_sha256": digest(directory / "fp32.onnx"),
            "output_sha256": hashes,
            "reproducible": hashes[0] == hashes[1],
        },
    )
    if hashes[0] != hashes[1]:
        raise ValueError("candidate conversion is not reproducible")
    for name in ("fp32", "candidate", "candidate-repeat"):
        write(
            directory / (name + ".json"),
            {
                "model": name + ".onnx",
                "model_sha256": digest(directory / (name + ".onnx")),
                "tokenizer": "tokenizer.json",
                "tokenizer_sha256": digest(directory / "tokenizer.json"),
                "config": "config.json",
                "config_sha256": digest(directory / "config.json"),
                "inputs": candidate["inputs"],
            },
        )


def covered(text, spans):
    positions, offset = set(), 0
    for char in text:
        if any(start <= offset < end for start, end, _ in spans):
            positions.add(offset)
        offset += len(char.encode("utf-8"))
    return positions


def compare(corpus, reference, fp32, candidate, repeated):
    rows = [
        json.loads(line) for line in corpus.read_text(encoding="utf-8").splitlines()
    ]
    reports = [reference, fp32, candidate, repeated]
    failures = []
    for report in reports:
        if (
            report["corpus_sha256"] != digest(corpus)
            or report["rejected_ids"]
            or report["evaluated"] != len(rows)
        ):
            raise ValueError("incomplete or incomparable corpus evaluation")
        if [r["id"] for r in report["predictions"]] != [r["id"] for r in rows]:
            raise ValueError("prediction order mismatch")
    baseline, floating, quantized = (metrics(r["counts"]) for r in reports[:3])
    limits = read(ROOT / "models/evaluation.json")["acceptance"]
    for metric in ("f1", "recall"):
        if (
            floating["overall"][metric] - quantized["overall"][metric]
            > limits["overall_" + metric + "_drop"] + 1e-12
        ):
            failures.append("quantization overall " + metric)
    for kind in CLASSES:
        if (
            floating[kind]["recall"] - quantized[kind]["recall"]
            > limits["class_recall_drop"] + 1e-12
        ):
            failures.append("quantization " + kind + " recall")
    for kind in (*CLASSES, "overall"):
        for metric in ("precision", "recall", "f1"):
            if quantized[kind][metric] + 1e-12 < baseline[kind][metric]:
                failures.append("production regression " + kind + " " + metric)
    if candidate["prediction_sha256"] != repeated["prediction_sha256"]:
        failures.append("nonreproducible predictions")
    exposed, old_extra, new_extra = 0, 0, 0
    for row, before, after in zip(
        rows, reference["predictions"], candidate["predictions"], strict=True
    ):
        gold = covered(row["text"], row["entities"])
        old = covered(row["text"], before["spans"])
        new = covered(row["text"], after["spans"])
        exposed += len((old & gold) - new)
        old_extra += len(old - gold)
        new_extra += len(new - gold)
    if exposed:
        failures.append("previously protected sensitive characters exposed")
    if new_extra > old_extra:
        failures.append("non-sensitive masking increased")
    return {
        "passed": not failures,
        "failures": failures,
        "examples": len(rows),
        "scores": {"production": baseline, "fp32": floating, "candidate": quantized},
        "newly_exposed_sensitive_characters": exposed,
        "production_non_sensitive_masked_characters": old_extra,
        "candidate_non_sensitive_masked_characters": new_extra,
        "corpus_sha256": digest(corpus),
    }


def evaluate(directory, corpora):
    sets = [read(p.parent / "corpus-manifest.json") for p in corpora]
    if any(
        len(s["ids"]) != 2000 or digest(p) != s["sha256"]
        for s, p in zip(sets, corpora, strict=True)
    ):
        raise ValueError("expected two pinned 2000-example corpora")
    if set(sets[0]["ids"]) & set(sets[1]["ids"]):
        raise ValueError("confirmation set overlaps evaluation set")
    executable = cargo_binary("ner-evaluate")
    reference = ROOT / "output/model-evaluation/existing.json"
    references = []
    for index, corpus in enumerate(corpora):
        path = directory / f"production-{index}.json"
        with path.open("wb") as stream:
            subprocess.run(
                [executable, reference, corpus], cwd=ROOT, stdout=stream, check=True
            )
        references.append(read(path))
    for candidate in read(SPEC)["candidates"]:
        target = directory / candidate["name"]
        convert(target, candidate)
        for index, corpus in enumerate(corpora):
            reports = []
            for name in ("fp32", "candidate", "candidate-repeat"):
                path = target / f"{name}-{index}-result.json"
                with path.open("wb") as stream:
                    subprocess.run(
                        [executable, target / (name + ".json"), corpus],
                        cwd=ROOT,
                        stdout=stream,
                        check=True,
                    )
                reports.append(read(path))
                print(
                    f"evaluated {candidate['name']} corpus={index} {name}", flush=True
                )
            report = compare(corpus, references[index], *reports)
            write(target / f"comparison-{index}.json", report)
            print(
                f"{candidate['name']} corpus={index} quality_passed={report['passed']}",
                flush=True,
            )


def validate_workloads(directory):
    executable = cargo_binary("ner-evaluate")
    reference = read(ROOT / "output/performance-cases.json")
    short = [row["text"] for row in reference if row["name"] == "ner-32"]
    evidence = []
    for spec in read(SPEC)["candidates"]:
        tokenizer = directory / spec["name"] / "tokenizer.json"
        output = subprocess.check_output(
            [executable, "workloads", tokenizer], cwd=ROOT, timeout=30
        )
        rows = [row for row in json.loads(output) if row["name"] == "ner-32"]
        if [row["text"] for row in rows] != short or any(
            row["tokens"] != 32 for row in rows
        ):
            raise ValueError("short workloads differ between tokenizers")
        evidence.append(
            {
                "candidate": spec["name"],
                "tokenizer_sha256": digest(tokenizer),
                "short_cases": len(rows),
                "tokens_including_special": 32,
            }
        )
    write(directory / "workload-validation.json", evidence)
    print(
        "verified identical 32-token workloads for both candidate tokenizers",
        flush=True,
    )


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("step", choices=("fetch", "evaluate", "workloads"))
    parser.add_argument(
        "--directory", type=Path, default=ROOT / "output/performance-candidates"
    )
    parser.add_argument(
        "--corpora",
        type=Path,
        nargs=2,
        default=[
            ROOT / "output/model-evaluation/corpus.jsonl",
            ROOT / "output/ner-wordpiece-holdout/corpus.jsonl",
        ],
    )
    args = parser.parse_args()
    if args.step == "fetch":
        fetch(args.directory)
    elif args.step == "workloads":
        validate_workloads(args.directory)
    else:
        evaluate(args.directory, args.corpora)
