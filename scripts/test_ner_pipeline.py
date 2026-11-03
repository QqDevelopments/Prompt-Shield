import copy
import unittest

from ner_pipeline import comparison, example


class EvaluationTests(unittest.TestCase):
    def test_utf8_and_fine_label_boundaries(self):
        row = {
            "id": "1",
            "tokens": ["Éva", "Li", "Paris"],
            "ner_tags": [7, 7, 4],
            "fine_ner_tags": [50, 54, 21],
        }
        actual = example(row, {"7": "PERSON", "4": "LOCATION"})
        self.assertEqual(actual["text"], "Éva Li Paris")
        self.assertEqual(
            actual["entities"],
            [[0, 4, "PERSON"], [5, 7, "PERSON"], [8, 13, "LOCATION"]],
        )

    def reports(self):
        counts = {
            k: {"gold": 100, "predicted": 100, "matched": 100}
            for k in ("PERSON", "ORGANIZATION", "LOCATION")
        }
        base = {
            "counts": counts,
            "predictions": [{"id": "a"}],
            "evaluated": 1,
            "rejected_ids": [],
            "corpus_sha256": "corpus",
            "tokenizer_sha256": "tokenizer",
            "prediction_sha256": "same",
        }
        return {
            k: copy.deepcopy(base)
            for k in ("fp32", "existing", "candidate", "candidate-repeat")
        }

    def test_accuracy_regression_fails_without_changing_baseline(self):
        reports = self.reports()
        reports["candidate"]["counts"]["PERSON"]["matched"] = 90
        result = comparison(
            reports,
            ["a"],
            {
                "overall_f1_drop": 0.01,
                "overall_recall_drop": 0.01,
                "class_recall_drop": 0.02,
            },
        )
        self.assertFalse(result["passed"])
        self.assertIn("candidate: PERSON recall", result["failures"])
        self.assertEqual(reports["fp32"]["counts"]["PERSON"]["matched"], 100)

    def test_incomplete_or_incomparable_results_fail(self):
        limits = {
            "overall_f1_drop": 0.01,
            "overall_recall_drop": 0.01,
            "class_recall_drop": 0.02,
        }
        with self.assertRaises(ValueError):
            comparison(self.reports(), ["a", "b"], limits)
        reports = self.reports()
        reports["candidate"]["rejected_ids"] = ["a"]
        with self.assertRaises(ValueError):
            comparison(reports, ["a"], limits)

    def test_repeated_conversion_predictions_must_match(self):
        reports = self.reports()
        reports["candidate-repeat"]["prediction_sha256"] = "different"
        result = comparison(
            reports,
            ["a"],
            {
                "overall_f1_drop": 0.01,
                "overall_recall_drop": 0.01,
                "class_recall_drop": 0.02,
            },
        )
        self.assertFalse(result["passed"])

    def test_different_span_aggregation_cannot_be_compared(self):
        reports = self.reports()
        reports["candidate"]["span_aggregation"] = "first_confident_entity_wordpiece_v1"
        with self.assertRaisesRegex(ValueError, "incomparable evaluation"):
            comparison(
                reports,
                ["a"],
                {
                    "overall_f1_drop": 0.01,
                    "overall_recall_drop": 0.01,
                    "class_recall_drop": 0.02,
                },
            )

    def test_duplicate_samples_and_changed_support_fail(self):
        limits = {
            "overall_f1_drop": 0.01,
            "overall_recall_drop": 0.01,
            "class_recall_drop": 0.02,
        }
        reports = self.reports()
        reports["fp32"]["predictions"].append({"id": "a"})
        reports["fp32"]["evaluated"] = 2
        with self.assertRaises(ValueError):
            comparison(reports, ["a"], limits)
        reports = self.reports()
        reports["candidate"]["counts"]["PERSON"]["gold"] = 101
        with self.assertRaises(ValueError):
            comparison(reports, ["a"], limits)


if __name__ == "__main__":
    unittest.main()
