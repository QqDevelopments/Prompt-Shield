"""Behavioral checks for manual acceptance and candidate quality gates."""

import copy
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace

from performance_candidates import compare, digest
from performance_matrix import accepted, evaluation


class AcceptanceTests(unittest.TestCase):
    def test_advisory_latency_goal_preserves_required_reference_checks(self):
        args = SimpleNamespace(scenario="ner-32", cancel_percent=0)
        report = {
            "warmup_seconds": 30,
            "duration_seconds": 120,
            "concurrency": 4,
            "requests": 10000,
            "successful_requests": 10000,
            "all_latency": {"p99_ms": 49.9},
            "recovered": True,
            "inputs": [{"contextual": True, "tokens": 32, "expected": "masked"}],
        }
        samples = [{"cgroup": {"oom": 0, "oom_kill": 0, "memory_peak": 500000000}}]
        self.assertTrue(accepted(report, samples, [], args))
        for p99, goal_met in [(49.9, True), (50, False), (100, False)]:
            with self.subTest(p99_ms=p99):
                result = evaluation(
                    {**report, "all_latency": {"p99_ms": p99}}, samples, [], args
                )
                self.assertTrue(result["acceptance_passed"])
                self.assertEqual(result["p99_target_ms"], 50)
                self.assertFalse(result["p99_target_required"])
                self.assertEqual(result["p99_target_met"], goal_met)
        for key, value in [
            ("successful_requests", 9999),
            ("requests", 9999),
            ("warmup_seconds", 29),
            ("duration_seconds", 119),
            ("concurrency", 1),
            ("recovered", False),
            ("inputs", []),
            ("inputs", [{"contextual": False, "tokens": 32}]),
            ("inputs", [{"contextual": True, "tokens": 33, "expected": "masked"}]),
            ("inputs", [{"contextual": True, "tokens": 32, "expected": "blocked"}]),
        ]:
            self.assertFalse(accepted({**report, key: value}, samples, [], args), key)
        self.assertFalse(accepted(report, [], [], args))
        self.assertFalse(accepted(report, samples, ["failed"], args))
        self.assertIsNone(evaluation(report, [], [], args)["p99_target_met"])
        for key in ["oom", "oom_kill", "memory_peak"]:
            broken = copy.deepcopy(samples)
            broken[0]["cgroup"][key] = 1073741825
            self.assertFalse(accepted(report, broken, [], args), key)


class CandidateTests(unittest.TestCase):
    def test_unicode_exposure_and_excess_masking_are_independent_of_aggregate_scores(
        self,
    ):
        with tempfile.TemporaryDirectory() as temporary:
            corpus = Path(temporary) / "corpus.jsonl"
            corpus.write_text(
                json.dumps(
                    {"id": "one", "text": "Éva X", "entities": [[0, 4, "PERSON"]]}
                )
                + "\n",
                encoding="utf-8",
            )
            base = {
                "corpus_sha256": digest(corpus),
                "rejected_ids": [],
                "evaluated": 1,
                "predictions": [{"id": "one", "spans": [[0, 4, "PERSON"]]}],
                "prediction_sha256": "same",
                "counts": {
                    kind: {"gold": 1, "predicted": 1, "matched": 1}
                    for kind in ("PERSON", "ORGANIZATION", "LOCATION")
                },
            }
            self.assertTrue(compare(corpus, base, base, base, base)["passed"])
            exposed = copy.deepcopy(base)
            exposed["predictions"][0]["spans"] = [[2, 4, "PERSON"]]
            result = compare(corpus, base, base, exposed, exposed)
            self.assertFalse(result["passed"])
            self.assertEqual(result["newly_exposed_sensitive_characters"], 1)
            excess = copy.deepcopy(base)
            excess["predictions"][0]["spans"] = [[0, 6, "PERSON"]]
            self.assertIn(
                "non-sensitive masking increased",
                compare(corpus, base, base, excess, excess)["failures"],
            )
            missing = copy.deepcopy(base)
            missing["rejected_ids"] = ["one"]
            with self.assertRaises(ValueError):
                compare(corpus, base, base, missing, missing)


if __name__ == "__main__":
    unittest.main()
