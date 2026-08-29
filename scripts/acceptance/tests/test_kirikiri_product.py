from __future__ import annotations

import importlib.util
import unittest
from pathlib import Path
import sys


ROOT = Path(__file__).resolve().parents[3]
DRIVER_PATH = ROOT / "scripts/acceptance/kirikiri_product.mjs"
RUNNER_PATH = ROOT / "scripts/acceptance/run.py"


class KiriKiriProductAcceptanceTests(unittest.TestCase):
    def test_formal_case_is_registered(self) -> None:
        spec = importlib.util.spec_from_file_location("acceptance_run_kirikiri", RUNNER_PATH)
        assert spec and spec.loader
        runner = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = runner
        spec.loader.exec_module(runner)
        self.assertEqual({"ACC-KIRIKIRI-001"}, runner.KIRIKIRI_CASES)
        self.assertIn("ACC-KIRIKIRI-001", runner.CASE_COMMANDS)
        self.assertIn("ACC-KIRIKIRI-001", runner.all_cases())

    def test_canvas_is_focused_before_layout_validation(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        focus_call = "await focusRuntimeCanvas(canvas);"
        layout_call = "const layout = await canvasLayoutEvidence(canvas)"
        self.assertIn(focus_call, contents)
        self.assertLess(contents.index(focus_call), contents.index(layout_call))
        self.assertIn("element.tabIndex = 0;", contents)
        self.assertIn("element.focus();", contents)

    def test_checkpoint_waits_for_player_save_availability(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn("await waitForEnabled(saveButton, 120_000);", contents)
        self.assertNotIn(
            'if (!await saveButton.isEnabled()) {throw new Error("KIRIKIRI_ACCEPTANCE_SAVE_UNAVAILABLE");}',
            contents,
        )

    def test_import_wait_accepts_the_terminal_completed_state(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn('["REVIEW_PENDING", "COMPLETED"].includes(job.state)', contents)
        self.assertNotIn('["REVIEW_PENDING", "COMPLETE"].includes(job.state)', contents)

    def test_product_screenshots_wait_for_runtime_ready_state(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        original_ready = "await waitForProductReady(originalPage);"
        original_frame = 'const beforeInput = await screenshotEvidence(originalCanvas, "product-before-input.png");'
        restored_ready = "await waitForProductReady(restoredPage);"
        restored_frame = 'const restoredFrame = await waitForMatchingScreenshot('
        self.assertIn(original_ready, contents)
        self.assertIn(restored_ready, contents)
        self.assertLess(contents.index(original_ready), contents.index(original_frame))
        self.assertLess(contents.index(restored_ready), contents.index(restored_frame))

    def test_restore_capture_waits_for_the_saved_frame_after_runtime_ready(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn(
            'const restoredFrame = await waitForMatchingScreenshot(',
            contents,
        )
        self.assertNotIn(
            'const restoredFrame = await screenshotEvidence(restoredCanvas, "restored.png");',
            contents,
        )
        self.assertIn("KIRIKIRI_ACCEPTANCE_RESTORE_POSITION_TIMEOUT", contents)

    def test_screenshot_evidence_reads_the_runtime_canvas_not_host_compositor(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn("element.toDataURL(\"image/png\")", contents)
        self.assertNotIn("await canvas.screenshot(", contents)

    def test_input_waits_for_kag_transition_and_resumes_after_checkpoint(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn("await canvas.page().waitForTimeout(2_000);", contents)
        checkpoint = "const saved = await createCheckpoint(originalPage, original.launchId);"
        resume = "await resumePlayerAfterCheckpoint(originalPage);"
        continue_input = "await advanceKag(originalCanvas);"
        self.assertIn(resume, contents)
        self.assertLess(contents.index(checkpoint), contents.index(resume))
        self.assertLess(contents.index(resume), contents.index(continue_input, contents.index(resume)))
        self.assertIn('getByRole("button", { name: "已暂停，点击游戏画面继续"', contents)
        self.assertIn('locator(".player-stage").dispatchEvent("click")', contents)

    def test_input_waits_for_kag_to_leave_and_reenter_a_stable_save_point(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn("await waitForKagStable(canvas);", contents)
        self.assertIn("const transition = waitForKagTransition(canvas);", contents)
        self.assertIn("await transition;", contents)
        self.assertIn("const bounds = await canvas.boundingBox();", contents)
        self.assertIn("await canvas.click({ position:", contents)
        self.assertNotIn("page.keyboard.press", contents)
        self.assertIn("observedUnstable = true;", contents)
        self.assertIn("if (observedUnstable && ready)", contents)
        self.assertIn("_krkr2_host_bookmark_is_ready", contents)


if __name__ == "__main__":
    unittest.main()
