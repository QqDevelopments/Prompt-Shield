"""Behavioral checks for evidence integrity and incomplete-run reporting."""

import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from report_load import follow, markdown, summarize


class LoadReportTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)
        self.environment = {
            "profile": "smoke",
            "commit": "fixture",
            "os": "linux",
            "cpu": "fixture",
            "logical_cpus": 2,
            "host_memory": "MemTotal: 1024 kB",
        }
        self.write("environment.json", self.environment)
        self.summary = {
            "state": "in_progress",
            "planned_conditions": 2,
            "measurements": [{"name": "condition", "exit_code": 0}],
        }
        self.write("run-summary.json", self.summary)
        histogram = {"count": 10, "p50_ms": 1, "p95_ms": 2, "p99_ms": 3}
        self.report = {
            "environment_sha256": hashlib.sha256(
                (self.directory / "environment.json").read_bytes()
            ).hexdigest(),
            "resource_report": "condition-resources.json",
            "recovered": True,
            "requests": 10,
            "successful_requests": 7,
            "outcomes": {"ok": 7, "http_429": 2, "canceled": 1},
            "actual_mix": {"clean-64": 10},
            "all_latency": histogram,
            "successful_latency": dict(histogram, count=7),
            "duration_seconds": 2,
            "error_rate": 0.2,
            "unsuccessful_rate": 0.3,
            "attempted_rps": 5,
            "successful_rps": 3.5,
            "transport": "http",
            "scenario": "mixed",
            "concurrency": 4,
            "inputs": [],
            "warmup_seconds": 0,
            "cancellation_latency": dict(histogram, count=1),
            "cancellation_selected": 1,
            "cancellation_signaled": 1,
            "cancel_after_ms": 10,
            "recovery_ms": 1,
            "measurement_clock": "monotonic",
            "clock_frequency_hz": 1000000000,
            "deadline_ms": 1000,
        }
        self.write("condition.json", self.report)
        self.resources = {
            "sampling_errors": [],
            "samples": [
                {
                    "engine": {
                        "rss_bytes": 1024,
                        "process_lifetime_peak_rss_bytes": 4096,
                    }
                },
                {
                    "engine": {
                        "rss_bytes": 2048,
                        "process_lifetime_peak_rss_bytes": 4096,
                    }
                },
            ],
        }
        self.write("condition-resources.json", self.resources)

    def write(self, name, value):
        (self.directory / name).write_text(json.dumps(value), encoding="utf-8")

    def test_incomplete_requires_explicit_snapshot(self):
        with self.assertRaisesRegex(ValueError, "run is incomplete"):
            summarize(self.directory)
        result = summarize(self.directory, allow_incomplete=True)
        self.assertFalse(result["complete"])
        self.assertEqual(result["completed_conditions"], 1)
        self.assertIn("in_progress; 1/2", markdown(result))
        row = result["conditions"][0]
        self.assertEqual(row["outcomes"], {"ok": 7, "http_429": 2, "canceled": 1})
        self.assertEqual(row["memory"]["engine"]["sampled_peak_rss_bytes"], 2048)
        self.assertEqual(
            row["memory"]["engine"]["process_lifetime_peak_rss_bytes"], 4096
        )

    def test_completed_run_cannot_omit_condition(self):
        self.summary["state"] = "completed"
        self.write("run-summary.json", self.summary)
        with self.assertRaisesRegex(ValueError, "missing conditions"):
            summarize(self.directory)

    def test_duplicate_condition_is_rejected(self):
        self.summary["measurements"] *= 2
        self.write("run-summary.json", self.summary)
        with self.assertRaisesRegex(ValueError, "duplicate condition"):
            summarize(self.directory, True)

    def test_environment_tampering_is_rejected(self):
        self.environment["cpu"] = "different"
        self.write("environment.json", self.environment)
        with self.assertRaisesRegex(ValueError, "environment changed"):
            summarize(self.directory, True)

    def test_capacity_errors_cannot_be_reported_as_success(self):
        self.report["successful_requests"] = 9
        self.write("condition.json", self.report)
        with self.assertRaisesRegex(ValueError, "invalid success count"):
            summarize(self.directory, True)

    def test_resource_failure_is_rejected(self):
        self.resources["sampling_errors"] = ["OSError"]
        self.write("condition-resources.json", self.resources)
        with self.assertRaisesRegex(ValueError, "missing resource samples"):
            summarize(self.directory, True)

    def test_follow_exits_after_complete_evidence(self):
        self.summary.update(state="completed", planned_conditions=1)
        self.write("run-summary.json", self.summary)
        self.write(
            "cancellation-dispatch.json",
            {"iterations": 1, "samples": [{"recovery_passed": True}]},
        )
        for case in ("clean-64", "masked-1024", "blocked-16384", "ner-32", "ner-512"):
            self.write("allocation-" + case + ".json", {"case": case})
        (self.directory / "go-benchmarks.txt").write_text("PASS\n", encoding="utf-8")
        result = follow(self.directory, self.directory / "report", timeout=1)
        self.assertTrue(result["complete"])
        saved = json.loads((self.directory / "report.json").read_text(encoding="utf-8"))
        self.assertEqual(saved["state"], "completed")
        self.assertTrue((self.directory / "report.md").is_file())

    def test_follow_does_not_succeed_when_run_stops(self):
        self.summary["state"] = "incomplete"
        self.write("run-summary.json", self.summary)
        with self.assertRaisesRegex(ValueError, "load run stopped incomplete"):
            follow(self.directory, self.directory / "report", timeout=1)
        saved = json.loads((self.directory / "report.json").read_text(encoding="utf-8"))
        self.assertFalse(saved["complete"])

    def test_follow_has_a_finite_timeout(self):
        with self.assertRaises(TimeoutError):
            follow(self.directory, self.directory / "report", timeout=0)


if __name__ == "__main__":
    unittest.main()
