from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]


class RuntimeTrialRetirementTests(unittest.TestCase):
    def test_formal_contracts_do_not_reintroduce_retired_capture_and_proof_fields(self):
        docs = ROOT.parents[1] / "docs"
        for name in ("http-api-contract.md", "import-and-review.md", "project-acceptance.md"):
            with self.subTest(document=name):
                source = (docs / name).read_text()
                for retired in ("captureAllowed", "captureAfterMs", "capturedAfterMs", "restore_launch_id",
                                "第 5 秒", "继续运行 5 秒", "Arcade schema v2", "review_version_at_create"):
                    self.assertFalse(retired in source, f"{name}: {retired}")

    def test_all_project_consumers_capture_review_screenshots_through_the_ordinary_player(self):
        for name in ("ons", "kirikiri", "butterscotch", "tyranoscript"):
            with self.subTest(core=name):
                source = (ROOT / f"{name}_product.mjs").read_text()
                self.assertIn("captureOptionalReviewScreenshot", source)
                self.assertNotIn("第 5 秒运行截图", source)
                self.assertNotIn("fixed five-second capture", source)

    def test_generation_runner_accepts_trial_artifact_instead_of_retired_id(self):
        source = (ROOT / "rpgmaker_case.py").read_text()
        self.assertNotIn('"VALIDATION_ID"', source)
        self.assertIn('"TRIAL_EVIDENCE"', source)

    def test_resource_pack_browser_requires_real_readiness_after_selection(self):
        source = (ROOT / "rpgmaker_pack.mjs").read_text()
        for retired in ("runtimeValidation", "validationId:", "selectBindingAndRejectStaleApproval"):
            self.assertNotIn(retired, source)
        for retained in ("publishReadiness", "REVIEW_VALIDATION_STALE", "REVIEW_DRAFT_INVALID",
                         "RPG_RUNTIME_PACK_IN_USE", "RPG_ACCEPTANCE_PACK_PATCH_RESULT_INVALID"):
            self.assertIn(retained, source)

    def test_security_trials_use_ordinary_preview_and_preserve_content_and_browser_boundaries(self):
        source = (ROOT / "rpgmaker_security.mjs").read_text()
        self.assertIn("/previews", source)
        for retired in ("runtime-validations", "machineGates", "waitForValidation", "validationId"):
            self.assertNotIn(retired, source)
        for retained in (
            "capturePreviewCheckpoint", "observeFixturePosition", "bootstrapChecks",
            "restoreFromPreviewId", "inactiveBootstrapStatus", "storedZIPMember",
            "inspectNativeProjection", "finishPreview",
        ):
            self.assertIn(retained, source)

    def test_generation_provision_uses_ordinary_previews_and_local_observations(self):
        source = (ROOT / "rpgmaker_generation_provision.mjs").read_text()
        self.assertIn("/previews", source)
        for retired in ("runtime-validations", "machineGates", "waitForValidation"):
            self.assertNotIn(retired, source)
        for retained in (
            "capturePreviewCheckpoint", "observeFixturePosition", "observePreviewFrames",
            "readAudioObservation", "assertPositionSequence", "restoreFromPreviewId",
            "RPG_PROVISION_RESTORE_FROZEN_PAYLOAD_MISMATCH", "rejectDeclaredOversize",
        ):
            self.assertIn(retained, source)


if __name__ == "__main__":
    unittest.main()
