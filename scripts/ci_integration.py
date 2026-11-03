"""Run mandatory real integrations and fail if a required test was skipped."""

import json
import os
import subprocess
import sys

from verification import ROOT, cargo_binary, command, port, stop, wait_port


def main():
    for variable in ("TEST_DATABASE_URL", "TEST_REDIS_URL"):
        if not os.environ.get(variable):
            raise RuntimeError(variable + " is required")
    output = ROOT / "output/ci-integration"
    output.mkdir(parents=True, exist_ok=True)
    command(
        [
            "node",
            "--no-warnings",
            "--experimental-strip-types",
            "scripts/fetch-model.ts",
        ]
    )
    binary = cargo_binary("prompt-shield-engine", "ner")
    number = port()
    env = {
        **os.environ,
        "ENGINE_LISTEN": f"127.0.0.1:{number}",
        "ENGINE_TEST_ADDR": f"127.0.0.1:{number}",
        "ENGINE_TOKEN": "synthetic-ci-engine-token-not-a-real-secret",
        "NER_ENABLED": "true",
        "ONNX_MODEL_PATH": str(ROOT / "models/general-ner/model.onnx"),
        "TOKENIZER_CONFIG_PATH": str(ROOT / "models/general-ner/tokenizer.json"),
        "SDK_SMOKE_PYTHON": sys.executable,
    }
    required = {
        "TestPostgresBoundary",
        "TestRedisAtomicLimit",
        "TestRealEngineHTTPBoundary",
        "TestPinnedSDKCompatibility",
        "TestRedisQueueSharedAcrossReplicas",
        "TestRedisParallelAdmissionKeepsSharedLimits",
        "TestRedisOwnerExpiryAndRunningDeadline",
        "TestTwoGatewayHTTPQueueSharesRedisAndCancelsWithoutDispatch",
    }
    with (output / "engine.log").open("wb") as log:
        process = subprocess.Popen(
            [binary], cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT
        )
        try:
            wait_port(number, process)
            with (output / "go-tests.jsonl").open("wb") as results:
                result = subprocess.run(
                    ["go", "test", "-json", "-count=1", "-race", "./..."],
                    cwd=ROOT / "services/gateway",
                    env=env,
                    stdout=results,
                    check=False,
                )
            passed, skipped = set(), set()
            for line in (output / "go-tests.jsonl").read_text().splitlines():
                event = json.loads(line)
                if event.get("Action") == "pass":
                    passed.add(event.get("Test"))
                if event.get("Action") == "skip":
                    skipped.add(event.get("Test"))
            missing = required - passed
            (output / "summary.json").write_text(
                json.dumps(
                    {
                        "schema_version": 1,
                        "exit_code": result.returncode,
                        "required": sorted(required),
                        "missing_or_failed": sorted(missing),
                        "skipped": sorted(s for s in skipped if s),
                    },
                    indent=2,
                )
            )
            if result.returncode or missing:
                raise RuntimeError(
                    "mandatory integrations failed: " + ", ".join(sorted(missing))
                )
            print(f"all {len(required)} mandatory integration boundaries passed")
        finally:
            stop(process)


if __name__ == "__main__":
    main()
