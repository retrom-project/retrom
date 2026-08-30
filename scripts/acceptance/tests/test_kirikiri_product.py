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

    def test_product_case_records_range_loading_and_cross_launch_cache_evidence(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        contract = (ROOT / "scripts/acceptance/kirikiri_product_contract.mjs").read_text(encoding="utf-8")
        self.assertIn('trackRuntimeLoading(originalPage, [], { timeoutMs: 60_000 })', contents)
        self.assertIn('trackRuntimeLoading(restoredPage, [], { timeoutMs: 60_000 })', contents)
        self.assertIn('sameProjectContentIdentity:', contents)
        self.assertIn('value.fullProjectFileResponseCount !== 0', contract)
        self.assertIn('value.rangeProjectFileResponseCount < 1', contract)
        self.assertIn('requireCacheHit && value.runtimeAssetCacheHitCount < 1', contract)
        self.assertIn(
            "trackRuntimeLoading(originalPage, [], { timeoutMs: 60_000 })",
            contents,
        )
        self.assertIn(
            "trackRuntimeLoading(restoredPage, [], { timeoutMs: 60_000 })",
            contents,
        )
        self.assertIn("KIRIKIRI_ACCEPTANCE_LOADING_EVIDENCE_FAILED", contents)

    def test_local_acceptance_routes_rpg_subdomains_through_the_loopback_proxy(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn(
            'import { localRpgAcceptanceProxy } from "./rpgmaker_local_proxy.mjs";',
            contents,
        )
        self.assertIn("const localProxy = await localRpgAcceptanceProxy(baseUrl);", contents)
        self.assertIn("...localProxy.contextOptions", contents)
        self.assertIn("await localProxy.close();", contents)

    def test_encrypted_operator_archive_is_blocked_instead_of_core_failure(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn('class AcceptanceBlocked extends Error', contents)
        self.assertIn('code === "ARCHIVE_ENCRYPTED_UNSUPPORTED"', contents)
        self.assertIn('status: "BLOCKED"', contents)
        self.assertIn('process.exitCode = blocked ? 3 : 1;', contents)

    def test_product_screenshots_wait_for_runtime_ready_state(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        original_ready = "await waitForProductReady(originalPage);"
        original_frame = 'const beforeInput = await screenshotEvidence(originalCanvas, "product-before-input.png");'
        restored_ready = "await waitForProductReady(restoredPage);"
        restored_frame = 'const restoreMatch = await waitForMatchingScreenshot('
        self.assertIn(original_ready, contents)
        self.assertIn(restored_ready, contents)
        self.assertLess(contents.index(original_ready), contents.index(original_frame))
        self.assertLess(contents.index(restored_ready), contents.index(restored_frame))

    def test_restore_capture_waits_for_the_saved_frame_after_runtime_ready(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn(
            'const restoreMatch = await waitForMatchingScreenshot(',
            contents,
        )
        self.assertNotIn(
            'const restoredFrame = await screenshotEvidence(restoredCanvas, "restored.png");',
            contents,
        )
        self.assertIn("KIRIKIRI_ACCEPTANCE_RESTORE_POSITION_TIMEOUT", contents)
        self.assertIn("compareKiriKiriVisualSamples(", contents)

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

    def test_smoke_input_scans_bounded_kag_menu_targets_with_the_gamepad(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn("const kagInputTargets = [", contents)
        self.assertIn("[0.08, 0.355]", contents)
        self.assertIn("for (const [targetX, targetY] of kagInputTargets)", contents)
        self.assertNotIn("await canvas.click({ position:", contents)

    def test_standard_gamepad_drives_visible_pointer_confirm_and_cancel(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        contract = (ROOT / "scripts/acceptance/kirikiri_product_contract.mjs").read_text(encoding="utf-8")
        self.assertIn('Object.defineProperty(navigator, "getGamepads"', contents)
        self.assertIn("globalThis.__retromTestGamepad", contents)
        self.assertIn("element.ownerDocument.defaultView?.__retromTestGamepad", contents)
        self.assertIn('[data-kirikiri-gamepad-cursor]', contents)
        self.assertIn("await setVirtualGamepadButton(canvas, 0, true);", contents)
        self.assertIn("await setVirtualGamepadButton(canvas, 1, true);", contents)
        self.assertIn('"standard-gamepad-control"', contract)

    def test_immersive_player_opens_exit_menu_with_double_select_start_chord(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        contract = (ROOT / "scripts/acceptance/kirikiri_product_contract.mjs").read_text(encoding="utf-8")
        self.assertIn('immersiveUrl.searchParams.set("experience", "immersive");', contents)
        immersive_start = contents.index('immersiveUrl.searchParams.set("experience", "immersive");')
        immersive_end = contents.index("await immersivePage.close();", immersive_start)
        immersive_flow = contents[immersive_start:immersive_end]
        self.assertIn("() => waitForKagStable(immersiveCanvas)", immersive_flow)
        self.assertNotIn("waitForProductReady(immersivePage)", immersive_flow)
        self.assertIn("KIRIKIRI_ACCEPTANCE_IMMERSIVE_RUNTIME_NOT_READY", immersive_flow)
        self.assertIn("KIRIKIRI_ACCEPTANCE_IMMERSIVE_EXIT_MENU_FAILED", immersive_flow)
        self.assertEqual(contents.count("await setVirtualGamepadButton(canvas, 8, true);"), 2)
        self.assertEqual(contents.count("await setVirtualGamepadButton(canvas, 9, true);"), 2)
        self.assertIn('page.getByRole("dialog", { name: "游戏菜单", exact: true })', contents)
        self.assertNotIn('name: /kirikiri|KAG fixture/iu', contents)
        self.assertIn('["取消", "创建存档", "退出游戏"]', contents)
        self.assertIn('"immersive-exit-menu"', contract)
        self.assertIn('"immersiveLaunchId"', contract)

    def test_gamepad_cancel_probe_is_bounded_and_does_not_leave_a_page_promise(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn('element.dataset.retromAcceptanceContextMenu = `${event.button}:${event.buttons}`;', contents)
        self.assertIn('element.ownerDocument.defaultView?.addEventListener("contextmenu"', contents)
        self.assertIn("{ capture: true, once: true }", contents)
        self.assertIn("const deadline = Date.now() + 2_000;", contents)
        self.assertNotIn('new Promise((resolvePromise) => {\n    element.addEventListener("contextmenu"', contents)
        self.assertIn('"KIRIKIRI_ACCEPTANCE_GAMEPAD_CANCEL_FAILED"', contents)
        self.assertIn('"KIRIKIRI_ACCEPTANCE_GAMEPAD_CONFIRM_FAILED"', contents)

    def test_preview_waits_for_stable_runtime_before_cancel_probe(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        preview_start = contents.index("const previewCanvas = await runtimeCanvas(previewPage);")
        preview_end = contents.index("await previewPage.close();", preview_start)
        preview_flow = contents[preview_start:preview_end]
        self.assertLess(
            preview_flow.index("() => waitForKagStable(previewCanvas)"),
            preview_flow.index("() => verifyGamepadCancel(previewCanvas)"),
        )

    def test_input_accepts_a_visible_kag_transition_that_never_reports_unstable(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn("await waitForKagStable(canvas);", contents)
        self.assertIn("const transition = waitForKagTransition(canvas, beforeFrame, 5_000);", contents)
        self.assertIn("await transition;", contents)
        self.assertIn("const beforeFrame = await canvasFrameFingerprint(canvas);", contents)
        self.assertIn("const kagInputTargets = [", contents)
        self.assertIn("[0.11, 0.38]", contents)
        self.assertIn("for (const [targetX, targetY] of kagInputTargets)", contents)
        self.assertIn("await moveVirtualGamepadCursor(canvas, targetX, targetY);", contents)
        self.assertIn("waitForKagTransition(canvas, beforeFrame, 5_000)", contents)
        self.assertIn("await setVirtualGamepadButton(canvas, 0, false);", contents)
        self.assertNotIn("page.keyboard.press", contents)
        self.assertIn("observedUnstable = true;", contents)
        self.assertIn(
            "observedVisualChange = await canvasFrameFingerprint(canvas) !== beforeFrame;",
            contents,
        )
        self.assertIn("if (observedUnstable || observedVisualChange)", contents)
        self.assertIn("_krkr2_host_bookmark_is_ready", contents)

    def test_kag_ready_must_remain_continuous_before_the_driver_sends_input(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn("const readyStableForMs = 500;", contents)
        self.assertIn("let readySince = null;", contents)
        self.assertIn("Date.now() - readySince >= readyStableForMs", contents)
        self.assertIn("readySince = null;", contents)

    def test_started_kag_transition_gets_the_full_runtime_completion_window(self) -> None:
        contents = DRIVER_PATH.read_text(encoding="utf-8")
        self.assertIn(
            "if (observedUnstable || observedVisualChange) {\n"
            "      await waitForKagStable(canvas);\n"
            "      return;\n"
            "    }",
            contents,
        )


if __name__ == "__main__":
    unittest.main()
