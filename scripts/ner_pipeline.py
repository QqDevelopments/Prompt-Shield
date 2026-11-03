"""Offline conversion/evaluation. Model binaries and public corpus stay in ignored output."""

import argparse
import hashlib
import importlib.metadata
import json
import os
import platform
import shutil
import subprocess
import sys
import tempfile
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MANIFEST = ROOT / "models/evaluation.json"
CLASSES = ("PERSON", "ORGANIZATION", "LOCATION")


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def read(path):
    return json.loads(Path(path).read_text(encoding="utf-8"))


def write(path, value):
    Path(path).write_text(
        json.dumps(value, indent=2, ensure_ascii=True) + "\n", encoding="utf-8"
    )


def environment():
    return {
        "python": sys.version,
        "os": platform.platform(),
        "cpu": platform.processor(),
        "logical_cpus": os.cpu_count(),
        "commit": subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True
        ).strip(),
        "tracked_diff_sha256": hashlib.sha256(
            subprocess.check_output(["git", "diff", "HEAD"], cwd=ROOT)
        ).hexdigest(),
        "packages": {
            p: importlib.metadata.version(p)
            for p in ("onnx", "onnxruntime", "pyarrow", "numpy")
        },
    }


def fetch(directory):
    spec = read(MANIFEST)
    directory.mkdir(parents=True, exist_ok=True)
    for item in spec["files"]:
        destination = directory / item["name"]
        if (
            destination.exists()
            and destination.stat().st_size == item["size"]
            and digest(destination) == item["sha256"]
        ):
            continue
        temporary = destination.with_suffix(destination.suffix + ".part")
        with (
            urllib.request.urlopen(item["url"], timeout=120) as response,
            temporary.open("wb") as out,
        ):
            shutil.copyfileobj(response, out)
        if (
            temporary.stat().st_size != item["size"]
            or digest(temporary) != item["sha256"]
        ):
            raise ValueError("artifact integrity mismatch: " + item["name"])
        temporary.replace(destination)
        print("verified " + item["name"], flush=True)
    prepare_corpus(directory)
    for name in ("fp32", "existing"):
        artifact(directory, name)


def artifact(directory, name):
    write(
        directory / (name + ".json"),
        {
            "model": name + ".onnx",
            "model_sha256": digest(directory / (name + ".onnx")),
            "tokenizer": "tokenizer.json",
            "tokenizer_sha256": digest(directory / "tokenizer.json"),
        },
    )


def example(row, label_map):
    tokens, labels, fine = row["tokens"], row["ner_tags"], row["fine_ner_tags"]
    if not (len(tokens) == len(labels) == len(fine)):
        raise ValueError("inconsistent annotations")
    offsets, cursor = [], 0
    for token in tokens:
        offsets.append((cursor, cursor + len(token.encode("utf-8"))))
        cursor = offsets[-1][1] + 1
    entities, index = [], 0
    while index < len(tokens):
        end = index + 1
        while (
            end < len(tokens)
            and labels[end] == labels[index]
            and fine[end] == fine[index]
        ):
            end += 1
        kind = label_map.get(str(labels[index]))
        if kind:
            entities.append([offsets[index][0], offsets[end - 1][1], kind])
        index = end
    return {"id": str(row["id"]), "text": " ".join(tokens), "entities": entities}


def prepare_corpus(directory):
    import pyarrow.parquet as pq

    spec = read(MANIFEST)["dataset"]
    rows = pq.read_table(directory / "few-nerd-test.parquet").to_pylist()
    rows.sort(
        key=lambda row: (
            hashlib.sha256(str(row["id"]).encode()).hexdigest(),
            str(row["id"]),
        )
    )
    selected = [
        example(row, spec["coarse_labels"]) for row in rows[: spec["sample_count"]]
    ]
    if len(selected) != spec["sample_count"] or len({r["id"] for r in selected}) != len(
        selected
    ):
        raise ValueError("insufficient or duplicate examples")
    with (directory / "corpus.jsonl").open(
        "w", encoding="utf-8", newline="\n"
    ) as stream:
        for row in selected:
            stream.write(json.dumps(row, ensure_ascii=True) + "\n")
    write(
        directory / "corpus-manifest.json",
        {
            **spec,
            "sha256": digest(directory / "corpus.jsonl"),
            "ids": [r["id"] for r in selected],
            "source_rows": len(rows),
            "reconstruction": "single ASCII spaces; offsets are UTF-8 bytes",
        },
    )


def convert(directory):
    import onnx
    from onnxruntime.quantization import QuantType, quantize_dynamic

    spec = read(MANIFEST)
    if digest(directory / "fp32.onnx") != spec["files"][0]["sha256"]:
        raise ValueError("FP32 integrity mismatch")
    options = spec["conversion"]
    hashes = []
    for name in ("candidate", "candidate-repeat"):
        # No model editing through strings; ONNX APIs operate on binary protobufs.
        with tempfile.TemporaryDirectory(prefix="ps-quantize-") as scratch:
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
        artifact(directory, name)
    write(
        directory / "conversion.json",
        {
            "schema_version": 1,
            "environment": environment(),
            "options": options,
            "source_sha256": digest(directory / "fp32.onnx"),
            "output_sha256": hashes,
            "reproducible": len(set(hashes)) == 1,
        },
    )
    if len(set(hashes)) != 1:
        raise ValueError("conversion was not bitwise reproducible")


def evaluate(directory):
    command = [
        "cargo",
        "build",
        "--locked",
        "--release",
        "--features",
        "evaluation",
        "--bin",
        "ner-evaluate",
        "--manifest-path",
        str(ROOT / "services/engine/Cargo.toml"),
        "--message-format=json",
    ]
    build = subprocess.run(
        command, cwd=ROOT, capture_output=True, text=True, check=False
    )
    if build.returncode:
        sys.stderr.write(build.stderr)
        raise RuntimeError("evaluation build failed")
    # Cargo supplies the executable path; build internals are not inspected.
    executable = None
    for line in build.stdout.splitlines():
        message = json.loads(line)
        if (
            message.get("executable")
            and message.get("target", {}).get("name") == "ner-evaluate"
        ):
            executable = message["executable"]
    if executable is None:
        raise RuntimeError("cargo did not report evaluation executable")
    # Execute the Cargo-reported artifact directly; never enumerate build directories.
    for name in ("fp32", "existing", "candidate", "candidate-repeat"):
        with (directory / (name + "-result.json")).open("wb") as stream:
            subprocess.run(
                [
                    executable,
                    str(directory / (name + ".json")),
                    str(directory / "corpus.jsonl"),
                ],
                stdout=stream,
                check=True,
                cwd=ROOT,
            )
        print("evaluated " + name, flush=True)
    compare(directory)


def metrics(counts):
    def score(c):
        precision = c["matched"] / c["predicted"] if c["predicted"] else 0.0
        recall = c["matched"] / c["gold"] if c["gold"] else 0.0
        return {
            "precision": precision,
            "recall": recall,
            "f1": 2 * precision * recall / (precision + recall)
            if precision + recall
            else 0.0,
            **c,
        }

    return {
        **{k: score(counts[k]) for k in CLASSES},
        "overall": score(
            {
                key: sum(counts[c][key] for c in CLASSES)
                for key in ("gold", "predicted", "matched")
            }
        ),
    }


def comparison(reports, expected_ids, limits):
    base = reports["fp32"]
    evaluated_ids = [p["id"] for p in base["predictions"]]
    all_ids = evaluated_ids + base["rejected_ids"]
    if (
        len(all_ids) != len(expected_ids)
        or len(set(all_ids)) != len(all_ids)
        or len(set(expected_ids)) != len(expected_ids)
        or set(all_ids) != set(expected_ids)
    ):
        raise ValueError("incomplete baseline evaluation")
    scores, failures = {}, []
    for name, report in reports.items():
        if (
            report["corpus_sha256"] != base["corpus_sha256"]
            or report["tokenizer_sha256"] != base["tokenizer_sha256"]
            or report.get("span_aggregation") != base.get("span_aggregation")
            or [p["id"] for p in report["predictions"]] != evaluated_ids
            or report["rejected_ids"] != base["rejected_ids"]
            or report["evaluated"] != len(evaluated_ids)
        ):
            raise ValueError("incomparable evaluation: " + name)
        if any(report["counts"][c]["gold"] == 0 for c in CLASSES):
            raise ValueError("missing class support")
        for kind in CLASSES:
            count = report["counts"][kind]
            if count["gold"] != base["counts"][kind]["gold"] or not 0 <= count[
                "matched"
            ] <= min(count["gold"], count["predicted"]):
                raise ValueError("inconsistent evaluation counts")
        scores[name] = metrics(report["counts"])
    for name in ("existing", "candidate"):
        for metric in ("f1", "recall"):
            if (
                scores["fp32"]["overall"][metric] - scores[name]["overall"][metric]
                > limits["overall_" + metric + "_drop"] + 1e-12
            ):
                failures.append(name + ": overall " + metric)
        for kind in CLASSES:
            if (
                scores["fp32"][kind]["recall"] - scores[name][kind]["recall"]
                > limits["class_recall_drop"] + 1e-12
            ):
                failures.append(name + ": " + kind + " recall")
    if (
        reports["candidate"]["prediction_sha256"]
        != reports["candidate-repeat"]["prediction_sha256"]
    ):
        failures.append("candidate predictions are not reproducible")
    return {
        "schema_version": 1,
        "passed": not failures,
        "failures": failures,
        "scores": scores,
        "evaluated": len(evaluated_ids),
        "rejected_ids": base["rejected_ids"],
    }


def compare(directory):
    reports = {
        name: read(directory / (name + "-result.json"))
        for name in ("fp32", "existing", "candidate", "candidate-repeat")
    }
    corpus = read(directory / "corpus-manifest.json")
    if any(r["corpus_sha256"] != corpus["sha256"] for r in reports.values()):
        raise ValueError("corpus digest mismatch")
    for name, result in reports.items():
        expected = read(directory / (name + ".json"))
        if (
            result["artifact_sha256"] != expected["model_sha256"]
            or result["tokenizer_sha256"] != expected["tokenizer_sha256"]
        ):
            raise ValueError("evaluation artifact mismatch: " + name)
    conversion = read(directory / "conversion.json")
    if not conversion["reproducible"] or conversion["output_sha256"] != [
        reports[n]["artifact_sha256"] for n in ("candidate", "candidate-repeat")
    ]:
        raise ValueError("conversion evidence mismatch")
    report = comparison(reports, corpus["ids"], read(MANIFEST)["acceptance"])
    report.update(
        {
            "environment": environment(),
            "corpus": corpus,
            "artifacts": {name: r["artifact_sha256"] for name, r in reports.items()},
        }
    )
    write(directory / "comparison.json", report)
    print(
        json.dumps(
            {k: report[k] for k in ("passed", "failures", "evaluated", "rejected_ids")}
        )
    )
    if not report["passed"]:
        raise ValueError(
            "quantization accuracy gate failed; default artifact unchanged"
        )


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=("fetch", "convert", "evaluate", "compare"))
    parser.add_argument(
        "--directory", type=Path, default=ROOT / "output/model-evaluation"
    )
    args = parser.parse_args()
    globals()[args.command](args.directory.resolve())
