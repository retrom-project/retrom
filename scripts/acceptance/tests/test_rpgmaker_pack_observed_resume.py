"""An inspection failure may reuse only a complete, immutable browser attempt."""
import copy
import unittest

from scripts.acceptance import rpgmaker_pack_observed_resume as resume


class ObservedResumeTests(unittest.TestCase):
    def fixture(self):
        observed = {"schemaVersion": 1, "caseId": "ACC-RPG-009", "status": "OBSERVED",
                    "screenshots": ["screenshots/rpgmaker-pack-catalog.png"]}
        result = {"caseId": "ACC-RPG-009", "status": "FAIL", "timedOut": False,
                  "startedAtMs": 100, "finishedAtMs": 200, "durationMs": 100,
                  "productEvidence": copy.deepcopy(observed)}
        request = {"schemaVersion": 1, "attempt": "001", "resultSha256": "a" * 64,
                   "observationSha256": "b" * 64, "provisionSha256": "c" * 64,
                   "screenshots": {observed["screenshots"][0]: "d" * 64}}
        return request, result, observed

    def test_inspection_resume_requires_completed_original_browser_observation(self):
        resume.validate_source(*self.fixture())

    def test_partial_timed_out_changed_or_path_escaping_attempt_is_rejected(self):
        for mutate in [
            lambda q, r, o: r.update(status="PASS"),
            lambda q, r, o: r.update(timedOut=True),
            lambda q, r, o: r.update(durationMs=300_001),
            lambda q, r, o: r["productEvidence"].update(status="PASS"),
            lambda q, r, o: o.update(status="PASS"),
            lambda q, r, o: q.update(attempt="../001"),
            lambda q, r, o: q.update(observationSha256="bad"),
            lambda q, r, o: q.update(screenshots={"../outside.png": "d" * 64}),
        ]:
            values = self.fixture()
            mutate(*values)
            with self.assertRaisesRegex(ValueError, "OBSERVED_RESUME_INVALID"):
                resume.validate_source(*values)

    def test_inspection_cannot_exceed_combined_original_case_budget(self):
        metadata = {"sourceDurationMs": 299_999, "inspectionStartedAtMs": 1}
        with self.assertRaisesRegex(ValueError, "OBSERVED_RESUME_TIMEOUT"):
            resume.finish_inspection(metadata, now_ms=3)
