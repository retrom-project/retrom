from __future__ import annotations

import hashlib
import importlib.util
import io
import json
import os
import struct
import subprocess
import sys
import tempfile
import unittest
import zlib
from contextlib import redirect_stdout
from pathlib import Path
from unittest import mock


MODULE_PATH = Path(__file__).resolve().parents[1] / "rpgmaker_case.py"
RUNNER_PATH = Path(__file__).resolve().parents[1] / "run.py"
BROWSER_PATH = Path(__file__).resolve().parents[1] / "rpgmaker_browser.mjs"
GENERATION_PROVISION_PATH = (
    Path(__file__).resolve().parents[1] / "rpgmaker_generation_provision.mjs"
)
PACK_PROVISION_PRODUCT_PATH = (
    Path(__file__).resolve().parents[1] / "rpgmaker_pack_provision_product.mjs"
)
PACK_BROWSER_PATH = Path(__file__).resolve().parents[1] / "rpgmaker_pack.mjs"
SECURITY_BROWSER_PATH = Path(__file__).resolve().parents[1] / "rpgmaker_security.mjs"
SPEC = importlib.util.spec_from_file_location("rpgmaker_case", MODULE_PATH)
assert SPEC and SPEC.loader
rpgmaker = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = rpgmaker
SPEC.loader.exec_module(rpgmaker)


class ProjectDigestTests(unittest.TestCase):
    def test_digest_uses_retrom_fileset_identity(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "Data").mkdir()
            (root / "Game.ini").write_bytes(b"game")
            (root / "Data" / "Scripts.rxdata").write_bytes(b"scripts")
            actual, count, total = rpgmaker.project_digest(root)
            hasher = hashlib.sha256(b"RETROM_FILESET_V1\0")
            for name, contents in (("Data/Scripts.rxdata", b"scripts"), ("Game.ini", b"game")):
                hasher.update(rpgmaker.length_prefixed("PROJECT_FILE"))
                hasher.update(rpgmaker.length_prefixed(name))
                hasher.update(hashlib.sha256(contents).digest())
                hasher.update(len(contents).to_bytes(8, "big"))
                hasher.update(b"\0")
            self.assertEqual((hasher.hexdigest(), 2, 11), (actual, count, total))

    def test_digest_rejects_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "Game.ini").write_bytes(b"game")
            (root / "linked.ini").symlink_to(root / "Game.ini")
            with self.assertRaisesRegex(rpgmaker.ContractError, "SYMLINK_FORBIDDEN"):
                rpgmaker.project_digest(root)

class EvidenceContractTests(unittest.TestCase):
    def test_generation_cases_use_one_virtual_user_core(self) -> None:
        self.assertEqual("rpgmaker", rpgmaker.USER_CORE_ID)
        self.assertEqual("MATCHED", rpgmaker.GENERATION_CASES["ACC-RPG-002"].confidence)
        self.assertEqual(
            "RPG2000",
            rpgmaker.GENERATION_CASES["ACC-RPG-002"].evidence_generation,
        )

    def test_local_rpg_hostnames_are_loopback_acceptance_origins(self) -> None:
        self.assertTrue(rpgmaker.is_local_acceptance_hostname("localhost"))
        self.assertTrue(rpgmaker.is_local_acceptance_hostname("127.0.0.1"))
        self.assertTrue(rpgmaker.is_local_acceptance_hostname("app.rpg.localhost"))
        self.assertFalse(rpgmaker.is_local_acceptance_hostname("localhost.example"))
        self.assertFalse(rpgmaker.is_local_acceptance_hostname("example.com"))

    def test_lcf_marker_contract_uses_the_rendered_indexed_palette_color(self) -> None:
        fixture_root = Path(__file__).resolve().parents[3] / "testdata/public-roms/rpgmaker-smoke"
        cases = (("ACC-RPG-002", "RPG2000", "rpg2000.json"),
                 ("ACC-RPG-003", "RPG2003", "rpg2003.json"))
        for case_id, generation, filename in cases:
            spec = json.loads((fixture_root / "fixture-spec" / filename).read_text())
            self.assertEqual(tuple(spec["accentRgb"]), rpgmaker.LCF_SOURCE_ACCENTS[generation])
            picture_palette = tuple(channel // 2 for channel in spec["accentRgb"])
            rendered = tuple(channel * 71 // 255 for channel in picture_palette)
            self.assertEqual(rendered, rpgmaker.MARKERS[generation][1])
            marker, marker_rgb, _ = rpgmaker.public_fixture_marker(
                rpgmaker.GENERATION_CASES[case_id],
            )
            self.assertEqual(spec["marker"], marker)
            self.assertEqual(list(rendered), marker_rgb)

    def test_isolation_driver_accepts_csp_block_before_request_dispatch(self) -> None:
        source = SECURITY_BROWSER_PATH.read_text()
        self.assertIn('"popup", "externalFetch", "serviceWorker"', source)
        self.assertIn('exact(probes?.[name], "blocked"', source)
        self.assertIn('request.urlKind === "external" && request.status !== 0', source)
        self.assertNotIn('request.urlKind === "external" && request.status === 0', source)

    def test_isolation_driver_focuses_runtime_canvas_and_surfaces_action_errors(self) -> None:
        source = (MODULE_PATH.parent / "rpgmaker_preview_actions.mjs").read_text()
        self.assertIn("element.tabIndex = 0;", source)
        self.assertIn("element.focus();", source)
        self.assertIn("await canvas.press(key, {delay: 80});", source)
        self.assertIn("for (const key of keys)", source)
        self.assertIn("await page.waitForTimeout(800);", source)
        self.assertNotIn("await page.keyboard.press(key)", source)
        self.assertIn("RPG_PROVISION_RUNTIME_ACTION_UNAVAILABLE_", source)

    def test_security_driver_resolves_reserved_local_hosts_without_system_dns(self) -> None:
        source = SECURITY_BROWSER_PATH.read_text()
        self.assertIn('import { localRpgAcceptanceProxy } from "./rpgmaker_local_proxy.mjs";', source)
        self.assertIn("const localProxy = await localRpgAcceptanceProxy(baseUrl);", source)
        self.assertIn("...localProxy.contextOptions", source)
        self.assertIn("await localProxy.close();", source)

    def test_security_driver_provisions_and_rechecks_the_virtual_rpgmaker_directory(self) -> None:
        source = SECURITY_BROWSER_PATH.read_text()
        apply_call = '"POST", "/api/v1/admin/platform-instances/recommendations/apply"'
        self.assertIn(apply_call, source)
        self.assertIn('item.defaultCoreId === "rpgmaker"', source)
        self.assertIn("if (platforms.length !== 1)", source)
        self.assertIn("new Map(coreIds.map((coreId) => [coreId, platforms[0].id]))", source)
        self.assertIn("RPG_ACCEPTANCE_SECURITY_PLATFORM_INSTANCES_MISSING", source)
        self.assertLess(source.index(apply_call), source.index("return new Map(coreIds.map"))

    def test_content_security_keeps_internal_core_mismatches_at_the_detector_boundary(self) -> None:
        browser_source = SECURITY_BROWSER_PATH.read_text()
        runner_source = MODULE_PATH.read_text()
        self.assertNotIn("for (const { source, target } of rejectedTargets)", browser_source)
        self.assertNotIn('familyOnly:', browser_source)
        self.assertIn("TestPublicWrongCoreMatrixHasFortyTwoMismatches", runner_source)
        self.assertIn('payload["detectorMatrix"] = detector_matrix', runner_source)

    def test_isolation_driver_keeps_each_restore_screenshot(self) -> None:
        source = SECURITY_BROWSER_PATH.read_text()
        self.assertIn(
            "const restoreScreenshot = `screenshots/acc-rpg-011-${input.generation.toLowerCase()}-restore.png`",
            source,
        )
        self.assertNotIn('"acc-rpg-011-restore.png"', source)

    def test_isolation_probes_wait_for_actual_frame_progress(self) -> None:
        source = SECURITY_BROWSER_PATH.read_text()
        wait = "await observePreviewFrames(launched.page);"
        probe = "launched.bootstrap = await bootstrapChecks("
        resume = "await launched.page.bringToFront();"
        validation = "await capturePreviewCheckpoint(launched.page, launched.launchId);"
        self.assertIn(wait, source)
        self.assertIn(probe, source)
        self.assertIn(resume, source)
        self.assertLess(source.index(wait), source.index(probe))
        self.assertLess(source.index(probe), source.index(resume))
        self.assertLess(source.index(resume), source.index(validation))
        self.assertNotIn("const bootstrap = inspectIsolation ? await bootstrapChecks", source)

    def test_content_security_driver_uses_product_content_and_safely_finishes_nested_launches(self) -> None:
        source = SECURITY_BROWSER_PATH.read_text()
        for required in (
            "await inspectNestedProject(context, client, review, sidecar)",
            "storedZIPMember(archive, sidecar.name)", '`/runtime/launches/${launchId}/finish`',
        ):
            self.assertIn(required, source)
        self.assertIn(
            'createPreviewLaunch(context, client, opaqueReview, "acc-rpg-010-opaque-native.png", false)',
            source,
        )
        self.assertIn("await cleanupNativeProjection(opaqueLaunch.frame)", source)
        self.assertIn("await runtimeProjectStatus(opaqueLaunch.frame, name)", source)
        self.assertNotIn("`${opaqueLaunch.runtimeOrigin}/__retrom/project/", source)
        self.assertIn("await finishPreview(opaqueLaunch.page, opaqueLaunch.launchId)", source)
        self.assertNotIn("await finishInspectionLaunch(client, opaqueLaunch.launchId)", source)
        self.assertIn('headers: { Origin: baseUrl, "Content-Type": "application/json" }', source)

    def test_content_security_evidence_requires_opaque_launch_cleanup(self) -> None:
        payload = content_security_evidence_payload()
        payload["opaqueNative"].pop("launchFinished", None)
        with self.assertRaisesRegex(rpgmaker.ContractError, "OPAQUE_NATIVE_EVIDENCE_INVALID"):
            rpgmaker.validate_security_evidence(payload, "ACC-RPG-010")

    def test_security_duplicate_import_is_blocked_before_review_cardinality(self) -> None:
        driver = SECURITY_BROWSER_PATH.read_text()
        upload = (Path(__file__).resolve().parents[1] / "rpgmaker_security_upload.mjs").read_text()
        self.assertIn("error instanceof SecurityInputBlocked", driver)
        self.assertIn('status: "BLOCKED"', driver)
        self.assertIn("process.exitCode = 3", driver)
        self.assertIn('`/api/v1/admin/imports/${importJobId}`', upload)
        self.assertIn("RPG_ACCEPTANCE_SECURITY_FRESH_DATABASE_REQUIRED", upload)

    def test_security_driver_awaits_only_the_successful_config_response(self) -> None:
        source = SECURITY_BROWSER_PATH.read_text()
        self.assertIn("const configResponse = page.waitForResponse", source)
        self.assertIn("response.status() === 200", source)
        self.assertIn("config = await (await configResponse).json()", source)
        self.assertNotIn("page.on(\"response\", async (response)", source)

    def test_security_driver_uses_provider_resources_not_host_adapter_projection(self) -> None:
        source = SECURITY_BROWSER_PATH.read_text()
        self.assertNotIn("config.adapter", source)
        self.assertNotIn("adapterKind", source)
        self.assertIn("providerResource(config,", source)
        self.assertIn("config.runtime.targetId", source)
        self.assertIn("bundleSha256", source)
        self.assertNotIn("targetContractSha256", source)

    def test_product_drivers_reveal_toolbar_through_the_visible_hud_handle(self) -> None:
        source = BROWSER_PATH.read_text()
        self.assertIn('page.locator(".player-hud-handle").click()', source)
        self.assertIn('page.locator(".player-toolbar.is-visible").waitFor', source)
        self.assertNotIn("page.mouse.move(400, 1)", source)

    def test_generation_driver_preserves_bounded_product_start_diagnostics(self) -> None:
        source = BROWSER_PATH.read_text()
        start = source.index("async function waitForProductSaveAvailability")
        end = source.index("\nasync function", start + 1)
        helper = source[start:end]
        self.assertIn("waitForProductSaveAvailability(", source)
        self.assertIn("const productReadyTimeoutMs = 180_000;", source)
        self.assertIn("timeout: productReadyTimeoutMs", helper)
        self.assertIn("RPG_ACCEPTANCE_PRODUCT_SAVE_UNAVAILABLE", helper)
        self.assertIn('page.locator(".player-loading").allTextContents()', helper)
        self.assertIn('page.getByRole("status").allTextContents()', helper)
        self.assertIn("dialogs.slice(0, 5)", helper)
        self.assertNotIn("document.body.innerText", helper)

    def test_pack_provision_uses_review_detail_approval_projection(self) -> None:
        source = (
            Path(__file__).resolve().parents[1] / "rpgmaker_pack_provision.mjs"
        ).read_text()
        self.assertIn("if (!current.canApprove", source)
        self.assertIn("review.canApprove !== initiallyReady", source)
        self.assertNotIn('state !== "REVIEW_PENDING"', source)

    def test_pack_provision_preserves_a_title_wrapper_without_mutating_binding(self) -> None:
        source = PACK_PROVISION_PRODUCT_PATH.read_text()
        self.assertIn("directoryFiles(sourcePath, `${sourceName}/`)", source)
        self.assertIn("review.metadata?.title !== sourceName", source)
        self.assertNotIn("ensureReviewTitle", source)

    def test_pack_provision_uses_one_virtual_rpgmaker_platform_instance(self) -> None:
        source = PACK_PROVISION_PRODUCT_PATH.read_text()
        self.assertIn('item.defaultCoreId === "rpgmaker"', source)
        self.assertIn("new Map(expectedTargetIds.map((targetId) => [targetId, platform.id]))", source)
        self.assertNotIn("expectedCoreIds.includes(item.defaultCoreId)", source)

    def test_pack_provision_reveals_save_by_current_player_pointer_contract(self) -> None:
        source = PACK_PROVISION_PRODUCT_PATH.read_text()
        self.assertIn("await revealPreviewToolbar(page)", source)
        self.assertIn('getByRole("button", {name: "创建存档", exact: true})', source)
        self.assertNotIn('page.locator(".player-toolbar")', source)

    def test_pack_driver_initializes_role_contract_before_product_execution(self) -> None:
        source = PACK_BROWSER_PATH.read_text()
        self.assertLess(source.index("const reviewRoles ="), source.index("const browser ="))
        self.assertLess(source.index("const reviewRoles ="), source.index("validateReviewRole(role"))

    def test_all_formal_rpg_cases_have_a_driver_registration(self) -> None:
        self.assertEqual(
            {f"ACC-RPG-{number:03d}" for number in range(2, 9)},
            set(rpgmaker.GENERATION_CASES),
        )
        self.assertEqual(
            {"ACC-RPG-010", "ACC-RPG-011"},
            set(rpgmaker.SECURITY_CASES),
        )
        self.assertEqual(
            {"ACC-RPG-012": "RPG_SECOND_RUNTIME_RELEASE_REQUIRED"},
            rpgmaker.DEFERRED_CASES,
        )
        self.assertEqual(
            {"ACC-RPG-009", "ACC-RPG-010", "ACC-RPG-011"},
            rpgmaker.MINIMAL_CLOSURE_CASES,
        )
        self.assertEqual("ACC-RPG-009", rpgmaker.PACK_CASE)
        self.assertEqual("ACC-RPG-012", rpgmaker.COMPATIBILITY_CASE)
        runner_spec = importlib.util.spec_from_file_location("acceptance_run", RUNNER_PATH)
        assert runner_spec and runner_spec.loader
        runner = importlib.util.module_from_spec(runner_spec)
        sys.modules[runner_spec.name] = runner
        runner_spec.loader.exec_module(runner)
        self.assertEqual(
            {f"ACC-RPG-{number:03d}" for number in range(1, 13)},
            set(runner.CASE_COMMANDS).intersection(runner.RPG_CASES),
        )
        self.assertTrue(runner.RPG_CASES.issubset(set(runner.all_cases())))
        with tempfile.TemporaryDirectory() as directory:
            case_dir = Path(directory)
            (case_dir / "result.json").write_text("{}")
            (case_dir / "rpgmaker-product.json").write_text("{}")
            runner.archive_previous(case_dir)
            self.assertTrue((case_dir / "attempts" / "001" / "rpgmaker-product.json").is_file())

    def test_generation_evidence_requires_cross_launch_position_restore(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-003"]
        digest = "a" * 64
        payload = product_payload(spec, digest)
        rpgmaker.validate_generation_evidence(payload, spec, digest)
        payload["runtimeTrial"]["checkpointRoundTrip"]["restoredPosition"]["playerX"] = 99
        with self.assertRaisesRegex(rpgmaker.ContractError, "POSITION_ROUND_TRIP_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, digest)

    def test_complete_generation_evidence_is_valid_for_all_seven_cores(self) -> None:
        for spec in rpgmaker.GENERATION_CASES.values():
            with self.subTest(generation=spec.generation):
                rpgmaker.validate_generation_evidence(product_payload(spec, "a" * 64), spec, "a" * 64)

    def test_generation_evidence_binds_frozen_restore_bytes_to_the_saved_checkpoint(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-006"]
        payload = product_payload(spec, "a" * 64)
        payload["runtimeTrial"]["checkpointRoundTrip"]["frozenRestoreSha256"] = "0" * 64
        with self.assertRaisesRegex(rpgmaker.ContractError, "CHECKPOINT_FROZEN_PAYLOAD_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_generation_evidence_binds_local_fixture_digest(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-007"]
        with self.assertRaisesRegex(rpgmaker.ContractError, "PROJECTFINGERPRINT_MISMATCH"):
            rpgmaker.validate_generation_evidence(product_payload(spec, "b" * 64), spec, "a" * 64)

    def test_generation_evidence_requires_fixture_variable_zero_one_two(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-005"]
        payload = product_payload(spec, "a" * 64)
        round_trip = payload["runtimeTrial"]["checkpointRoundTrip"]
        for key in ("initialPosition", "savedPosition", "divergedPosition"):
            round_trip[key]["fixtureState"] = 0
        round_trip["restoredPosition"]["fixtureState"] = 0
        with self.assertRaisesRegex(rpgmaker.ContractError, "FIXTURE_STATE_SEQUENCE_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_generation_evidence_requires_a_non_black_visible_marker(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-003"]
        payload = product_payload(spec, "a" * 64)
        payload["restoreVisualEvidence"].update(
            {"nonBlackPixels": 0, "distinctColorBuckets": 1, "markerPixelCount": 0},
        )
        with self.assertRaisesRegex(rpgmaker.ContractError, "RESTORE_VISUAL_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_mz_generation_rejects_marker_overlay_without_a_visible_game_scene(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-008"]
        payload = product_payload(spec, "a" * 64)
        with tempfile.TemporaryDirectory() as directory:
            screenshot = Path(directory) / "overlay-only.png"
            marker_rgb = payload["inputProvenance"]["markerRgb"]
            screenshot.write_bytes(test_mz_overlay_only_png(640, 480, marker_rgb))
            logical_screenshot = payload["restoreVisualEvidence"]["screenshot"]
            payload["restoreVisualEvidence"] = rpgmaker.image_visual_evidence(
                screenshot, logical_screenshot, "RETROM RPGMZ", marker_rgb,
                rpgmaker.MZ_SCENE_EXCLUSION,
            )
            with self.assertRaisesRegex(rpgmaker.ContractError, "RESTORE_VISUAL_INVALID"):
                rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_generation_evidence_requires_sanitized_upload_import_transcript(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-006"]
        payload = product_payload(spec, "a" * 64)
        payload["inputTranscript"]["upload"]["sourcePath"] = "/private/rpgvxace"
        with self.assertRaisesRegex(rpgmaker.ContractError, "INPUT_TRANSCRIPT_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_xp_generation_evidence_rejects_a_malformed_optional_270_mib_trace(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-004"]
        payload = product_payload(spec, "a" * 64)
        payload["xpRuntimeTrace"]["checkpointUpload"]["requestContentLengthBytes"] = 1_048_575
        with self.assertRaisesRegex(rpgmaker.ContractError, "XP_RUNTIME_TRACE_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_xp_generation_evidence_accepts_minimal_round_trip_without_extended_trace(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-004"]
        payload = product_payload(spec, "a" * 64)
        payload.pop("xpRuntimeTrace")
        rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_mkxp_generation_evidence_rejects_an_uncompressed_raw_checkpoint(self) -> None:
        for case_id in ("ACC-RPG-004", "ACC-RPG-005", "ACC-RPG-006"):
            spec = rpgmaker.GENERATION_CASES[case_id]
            payload = product_payload(spec, "a" * 64)
            payload["runtimeTrial"]["checkpointRoundTrip"]["sizeBytes"] = 268_435_456
            with self.subTest(generation=spec.generation), self.assertRaisesRegex(
                rpgmaker.ContractError, "MKXP_RUNTIME_EVIDENCE_INVALID",
            ):
                rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_generation_provision_collects_xp_trace_from_product_boundaries(self) -> None:
        source = GENERATION_PROVISION_PATH.read_text()
        for required in (
            'review.version, "PREVIEW"', 'review.version, "RESTORE"',
            'declaredContentLengthBytes: 283_115_521',
            'capturePreviewCheckpoint(original, created.previewId)',
            'writeFileSync(tracePath,', 'flag: "wx"', 'mode: 0o600',
            'launchCredentialIssued: false, projectPayloadRequestCount: 0',
            'while (remaining > 0 && !responseHasStarted())',
            'await writeChunk(request, chunk)',
        ):
            self.assertIn(required, source)
        self.assertNotIn("sourcePath", source)

    def test_generation_provision_preserves_runtime_failure_diagnostics(self) -> None:
        source = GENERATION_PROVISION_PATH.read_text() + (MODULE_PATH.parent / "rpgmaker_preview_actions.mjs").read_text()
        self.assertIn("RPG_PROVISION_RUNTIME_ACTION_UNAVAILABLE_", source)
        self.assertIn("page.__retromPageErrors", source)
        self.assertIn('page.addInitScript(() => {', source)
        self.assertIn('window.addEventListener("retrom:runtime-diagnostic"', source)
        self.assertIn("window.__retromRuntimeDiagnostics.length > 100", source)
        self.assertIn('page.exposeBinding("__retromCaptureRuntimeDiagnostic"', source)
        self.assertIn("page.__retromRuntimeDiagnostics = runtimeDiagnostics", source)
        self.assertIn('typeof detail.code !== "string"', source)
        self.assertIn("code: trimDiagnostic(value.code)", source)
        self.assertIn("runtimeDiagnostics: runtimeDiagnostics.map", source)
        self.assertIn('page.on("console", (message)', source)
        self.assertIn("page.__retromConsoleDiagnostics = consoleDiagnostics", source)
        self.assertIn("consoleDiagnostics: (page.__retromConsoleDiagnostics ?? []).slice(-30)", source)
        self.assertIn('request.url().includes("/runtime/content/project/")', source)
        self.assertIn("page.__retromProjectRequests = projectRequests", source)
        self.assertIn("projectRequests: (page.__retromProjectRequests ?? []).slice(-30)", source)
        self.assertIn("await context.newCDPSession(page)", source)
        self.assertIn('cdp.on("Runtime.exceptionThrown"', source)
        self.assertIn("exceptionDiagnostics: (page.__retromExceptionDiagnostics ?? []).slice(-20)", source)
        self.assertIn("details.exception?.preview?.properties", source)
        self.assertIn('cdp.send("Runtime.getProperties"', source)
        self.assertIn("page.__retromExceptionTasks", source)
        self.assertIn("await Promise.allSettled(page.__retromExceptionTasks", source)
        self.assertIn("page.__retromFatalError", source)
        self.assertIn("fatalError.then", source)
        self.assertIn("page.__retromNetworkRequests", source)
        self.assertIn("networkRequests: (page.__retromNetworkRequests ?? []).slice(-100)", source)
        self.assertIn('page.locator(".player-loading")', source)
        self.assertIn('page.getByRole("status")', source)
        self.assertIn("await Promise.race([", source)
        self.assertIn("runtimeFailure.waitFor", source)

    def test_generation_provision_fails_at_the_launch_credential_boundary(self) -> None:
        source = GENERATION_PROVISION_PATH.read_text()
        self.assertEqual(2, source.count("await assertLaunchCookie(context,"))
        self.assertIn('cookie.name === `retrom_launch_${launchId}`', source)
        self.assertIn('cookie.path === expectedPath', source)
        self.assertIn('cookie.httpOnly && cookie.sameSite === "Strict"', source)
        self.assertIn("const configResponse = page.waitForResponse", source)
        self.assertIn("if (config.status() !== 200)", source)
        self.assertIn("RPG_PROVISION_LAUNCH_CONFIG_", source)

    def test_generation_provision_does_not_bind_an_unrequested_xp_trace(self) -> None:
        source = GENERATION_PROVISION_PATH.read_text()
        self.assertIn(
            "checkpointUpload: tracePath ? bindCheckpointUpload(checkpointB, checkpointB) : null",
            source,
        )

    def test_generation_provision_rejects_stale_mz_provenance_before_browser_side_effects(self) -> None:
        source = GENERATION_PROVISION_PATH.read_text()
        validation = 'validateMZProvenance(sourceFiles, required("RPG_MZ_SMOKE_PROVENANCE"));'
        self.assertIn(validation, source)
        self.assertLess(source.index(validation), source.index("await chromium.launch"))

    def test_generation_drivers_route_rpg_localhost_through_the_loopback_proxy(self) -> None:
        for path in (GENERATION_PROVISION_PATH, BROWSER_PATH):
            with self.subTest(path=path.name):
                source = path.read_text()
                self.assertIn('from "./rpgmaker_local_proxy.mjs"', source)
                self.assertIn("await localRpgAcceptanceProxy(baseUrl)", source)
                self.assertIn("...localProxy.contextOptions", source)
                self.assertIn("await localProxy.close()", source)

    def test_native_generation_loading_evidence_does_not_sample_game_frame_timings(self) -> None:
        source = BROWSER_PATH.read_text()
        self.assertIn('collectRuntimeTimings: !["rpgmaker-mv", "rpgmaker-mz"].includes(config.runtime.targetId)', source)
        self.assertEqual(2, source.count("trackRuntimeLoading("))
        self.assertEqual(3, source.count("loadingProbeOptions"))

    def test_generation_browser_rejects_runtime_errors_before_tearing_down_each_page(self) -> None:
        source = BROWSER_PATH.read_text()
        first_stop = source.index("loadingProbe.stop();")
        first_close = source.index("await page.close();", first_stop)
        cache_stop = source.index("cacheLoadingProbe.stop();")
        cache_close = source.index("await cachePage.close();", cache_stop)
        self.assertLess(first_stop, source.index("assertNoPlayerErrors(", first_stop, first_close))
        self.assertLess(cache_stop, source.index("assertNoPlayerErrors(", cache_stop, cache_close))

    def test_generation_browser_preserves_runtime_exception_properties_and_network_context(self) -> None:
        source = BROWSER_PATH.read_text()
        self.assertIn('cdp.send("Runtime.getProperties"', source)
        self.assertIn('cdp.send("Runtime.callFunctionOn"', source)
        self.assertIn("stack: String(runtimeStack", source)
        self.assertIn("JSON.stringify(value).slice(0, 6_000)", source)
        self.assertIn("const runtimeExceptionTasks = [];", source)
        self.assertIn("await Promise.allSettled(runtimeExceptionTasks);", source)
        self.assertIn("properties: properties.slice(0, 16).map", source)
        self.assertIn("networkResponses.slice(-100).map", source)
        self.assertIn("status: response.status()", source)
        self.assertNotIn('["error", "warning"].includes(message.type())', source)

    def test_generation_browser_proves_product_runtime_progress_on_both_launches(self) -> None:
        source = BROWSER_PATH.read_text()
        debug_close = source.index('getByRole("button", { name: "关闭调试信息面板" }).click()')
        resume = source.index('getByRole("button", { name: "继续游戏" }).click()', debug_close)
        progress = source.index("const firstRuntimeProgress = await assertRuntimeProgress(page);", resume)
        self.assertLess(debug_close, resume)
        self.assertLess(resume, progress)
        self.assertIn("const firstRuntimeProgress = await assertRuntimeProgress(page);", source)
        self.assertIn("const cacheRuntimeProgress = await assertRuntimeProgress(cachePage);", source)
        self.assertIn(
            "runtimeProgress: { firstLaunch: firstRuntimeProgress, cacheLaunch: cacheRuntimeProgress },",
            source,
        )
        self.assertIn("RPG_ACCEPTANCE_PRODUCT_RUNTIME_STALLED", source)
        self.assertNotIn("rpg_product_frame_probe", source)

    def test_generation_evidence_requires_progress_on_both_product_launches(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-002"]
        payload = product_payload(spec, "a" * 64)
        del payload["productLaunch"]["runtimeProgress"]["cacheLaunch"]
        with self.assertRaisesRegex(rpgmaker.ContractError, "PRODUCT_RUNTIME_PROGRESS_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_generation_provision_covers_all_seven_current_targets_and_state_inputs(self) -> None:
        source = GENERATION_PROVISION_PATH.read_text()
        expected = {
            "ACC-RPG-002": ("rpgmaker-2000", 'restoreKeys: ["ArrowRight", "ArrowRight", "ArrowRight"'),
            "ACC-RPG-003": ("rpgmaker-2003", 'restoreKeys: ["ArrowRight", "ArrowRight", "ArrowRight"'),
            "ACC-RPG-004": ("rpgmaker-xp", 'saveKeys: ["ArrowRight", "KeyX"]'),
            "ACC-RPG-005": ("rpgmaker-vx", 'saveKeys: ["ArrowRight", "KeyX"]'),
            "ACC-RPG-006": ("rpgmaker-vx-ace", 'saveKeys: ["ArrowRight", "KeyX"]'),
            "ACC-RPG-007": ("rpgmaker-mv", 'saveKeys: ["ArrowRight", "Enter"]'),
            "ACC-RPG-008": ("rpgmaker-mz", 'saveKeys: ["ArrowRight", "Enter"]'),
        }
        for case_id, (target, input_sequence) in expected.items():
            self.assertEqual(1, source.count(f'"{case_id}": {{'))
            self.assertEqual(1, source.count(f'targetId: "{target}"'))
            self.assertIn(input_sequence, source)
        self.assertIn("ACC-RPG-002..ACC-RPG-008", source)

    def test_generation_provision_uses_the_single_virtual_platform_instance(self) -> None:
        source = GENERATION_PROVISION_PATH.read_text()
        self.assertIn('item.defaultCoreId === "rpgmaker"', source)
        self.assertNotIn("item.defaultCoreId === config.coreId", source)

    def test_generation_provision_can_resume_an_exact_review_without_proof_state(self) -> None:
        source = GENERATION_PROVISION_PATH.read_text()
        self.assertIn('process.env.RETROM_RPG_PROVISION_RESUME_ITEM_ID', source)
        self.assertNotIn("validationCanBeReplaced", source)
        self.assertNotIn("runtimeValidationCurrent", source)
        self.assertIn('review.sourceManifest?.filesDigest !== expected.filesDigest', source)
        self.assertIn('.normalize("NFC")', source)
        self.assertIn('Buffer.compare(Buffer.from(left.logicalName), Buffer.from(right.logicalName))', source)
        self.assertIn('"RPG_PROVISION_RESUME_REVIEW_INVALID"', source)

    def test_mv_generation_evidence_requires_two_origin_inventory(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-007"]
        payload = product_payload(spec, "a" * 64)
        payload["originInventory"]["appOrigin"]["projectResourceResponses"] = 1
        with self.assertRaisesRegex(rpgmaker.ContractError, "ORIGIN_INVENTORY_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_generation_evidence_rejects_eager_or_missing_runtime_loading(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-004"]
        payload = product_payload(spec, "a" * 64)
        payload["loading"]["firstVisible"]["fullProjectFileResponseCount"] = 1
        with self.assertRaisesRegex(rpgmaker.ContractError, "RUNTIME_LOADING_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

        native_spec = rpgmaker.GENERATION_CASES["ACC-RPG-007"]
        native_payload = product_payload(native_spec, "a" * 64)
        native_payload["loading"]["firstVisible"]["nativeProjectResponseCount"] = \
            native_payload["inputProvenance"]["fileCount"]
        with self.assertRaisesRegex(rpgmaker.ContractError, "RUNTIME_LOADING_INVALID"):
            rpgmaker.validate_generation_evidence(native_payload, native_spec, "a" * 64)

    def test_mz_generation_evidence_requires_legal_lineage_engine_chrome_and_durations(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-008"]
        payload = product_payload(spec, "a" * 64)
        payload["inputProvenance"].pop("licenseUrl")
        payload["runtimeEnvironment"]["chromeVersion"] = ""
        payload["runtimeEnvironment"]["trialDurationMs"] = None
        with self.assertRaisesRegex(rpgmaker.ContractError, "MZ_INPUT_PROVENANCE_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_mz_generation_evidence_rejects_missing_chrome_and_trial_duration(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-008"]
        payload = product_payload(spec, "a" * 64)
        payload["runtimeEnvironment"]["chromeVersion"] = ""
        payload["runtimeEnvironment"]["trialDurationMs"] = None
        with self.assertRaisesRegex(rpgmaker.ContractError, "RUNTIME_ENVIRONMENT_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_mz_generation_evidence_requires_a_bound_transformation_recipe(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-008"]
        payload = product_payload(spec, "a" * 64)
        del payload["inputProvenance"]["transformation"]
        with self.assertRaisesRegex(rpgmaker.ContractError, "MZ_INPUT_PROVENANCE_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_mz_generation_evidence_accepts_only_the_visible_map_v3_recipe(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-008"]
        payload = product_payload(spec, "a" * 64)
        payload["inputProvenance"]["transformation"]["recipe"] = "RETROM_MZ_MINIMAL_V3"
        rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)
        payload["inputProvenance"]["transformation"]["recipe"] = "RETROM_MZ_MINIMAL_V2"
        with self.assertRaisesRegex(rpgmaker.ContractError, "MZ_TRANSFORMATION_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_runtime_environment_accepts_playwright_numeric_chrome_version(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-002"]
        payload = product_payload(spec, "a" * 64)
        payload["runtimeEnvironment"]["chromeVersion"] = "149.0.7827.55"
        rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

        payload["runtimeEnvironment"]["chromeVersion"] = "Chrome 149"
        with self.assertRaisesRegex(rpgmaker.ContractError, "RUNTIME_ENVIRONMENT_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_generation_browser_derives_transcript_and_origin_inventory_without_paths(self) -> None:
        source = BROWSER_PATH.read_text()
        self.assertIn('`/api/v1/admin/imports/${importJobId}`', source)
        self.assertIn('`/api/v1/admin/uploads/${imported.uploadId}`', source)
        self.assertIn("new URL(runtimeFrameURL, baseUrl).origin", source)
        transcript_source = source[source.index("async function readInputTranscript"):]
        self.assertNotIn("relativePath:", transcript_source)
        self.assertNotIn("bootstrapTicket:", transcript_source)

    def test_generation_browser_records_first_visible_and_second_launch_loading(self) -> None:
        source = BROWSER_PATH.read_text()
        generation_source = source[
            source.index("async function generationCase"):source.index("async function approvedReview")
        ]
        self.assertIn(
            "const projectDeclarations = projectLoadingDeclarations(config)",
            generation_source,
        )
        self.assertIn(
            "const loadingProbe = trackRuntimeLoading(page, projectDeclarations, loadingProbeOptions)",
            generation_source,
        )
        self.assertLess(
            generation_source.index(
                "const loadingProbe = trackRuntimeLoading(page, projectDeclarations, loadingProbeOptions)",
            ),
            generation_source.index("await page.goto"),
        )
        self.assertIn("await loadingProbe.snapshot()", generation_source)
        self.assertEqual(
            generation_source.count(
                "= applyEasyProjectDeclaration("
            ),
            2,
        )
        self.assertIn("trackRuntimeLoading(cachePage, projectDeclarations, loadingProbeOptions)", source)
        self.assertIn("cacheLaunchId: cacheLaunch.launchId", source)
        self.assertIn("sameProjectContentIdentity,", source)
        loading_source = (MODULE_PATH.parent / "runtime_loading_evidence.mjs").read_text()
        self.assertNotIn("projectPath:", loading_source)

    def test_catalog_evidence_reports_applied_recommendation_states(self) -> None:
        source = BROWSER_PATH.read_text()
        self.assertIn("recommendationStates: covered.map", source)
        self.assertNotIn("recommendationStates: recommendations.map", source)

    def test_png_visual_evidence_is_derived_from_bytes_and_rejects_black_frame(self) -> None:
        marker = "RETROM RPGVX"
        rgb = [168, 85, 247]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            visible = root / "visible.png"
            visible.write_bytes(test_png(320, 180, rgb))
            evidence = rpgmaker.image_visual_evidence(
                visible, "screenshots/visible.png", marker, rgb,
            )
            rpgmaker.validate_restore_visual(evidence, marker, rgb)
            self.assertEqual(hashlib.sha256(visible.read_bytes()).hexdigest(), evidence["sha256"])
            undersized = root / "undersized.png"
            undersized.write_bytes(test_png(300, 150, rgb))
            with self.assertRaisesRegex(rpgmaker.ContractError, "RESTORE_SCREENSHOT_PNG_INVALID"):
                rpgmaker.image_visual_evidence(
                    undersized, "screenshots/undersized.png", marker, rgb,
                )
            black = root / "black.png"
            black.write_bytes(test_png(320, 180, [0, 0, 0], solid=True))
            black_evidence = rpgmaker.image_visual_evidence(
                black, "screenshots/black.png", marker, rgb,
            )
            with self.assertRaisesRegex(rpgmaker.ContractError, "RESTORE_VISUAL_INVALID"):
                rpgmaker.validate_restore_visual(black_evidence, marker, rgb)

    def test_visual_evidence_decodes_the_actual_jpeg_upload_and_keeps_its_original_digest(self) -> None:
        # Encode only our deterministic, in-memory test image with Next's locked image library.
        encoder = "import {createRequire} from 'node:module';" \
            "const sharp=createRequire(new URL('./web/package.json',import.meta.url))('sharp');" \
            "const chunks=[];for await(const chunk of process.stdin)chunks.push(chunk);" \
            "process.stdout.write(await sharp(Buffer.concat(chunks)).jpeg().toBuffer());"
        encoded = subprocess.run(
            [str(rpgmaker.ROOT / ".cache/tools/node-v24.18.0-linux-x64/bin/node"),
             "--input-type=module", "-e", encoder],
            cwd=rpgmaker.ROOT, input=test_png(320, 180, [168, 85, 247]),
            capture_output=True, check=True, timeout=20,
        ).stdout
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "restored.jpg"
            path.write_bytes(encoded)
            evidence = rpgmaker.image_visual_evidence(
                path, "screenshots/restored.jpg", "RETROM RPGVX", [168, 85, 247],
            )
            self.assertEqual(evidence["sha256"], hashlib.sha256(encoded).hexdigest())
            self.assertEqual((evidence["width"], evidence["height"]), (320, 180))
            rpgmaker.validate_restore_visual(evidence, "RETROM RPGVX", [168, 85, 247])

    def test_external_web_marker_must_exist_in_the_supplied_project(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "data").mkdir()
            marker = root / "data" / "Map001.json"
            marker.write_text('{"text":"RETROM RPGMZ"}')
            rpgmaker.require_web_marker(root, "RETROM RPGMZ")
            marker.write_text('{"text":"unrelated"}')
            with self.assertRaisesRegex(rpgmaker.ContractError, "WEB_MARKER_MISSING"):
                rpgmaker.require_web_marker(root, "RETROM RPGMZ")

    def test_generation_required_inputs_include_chrome_and_mz_legal_lineage(self) -> None:
        catalog = rpgmaker.required_environment("ACC-RPG-001")
        xp = rpgmaker.required_environment("ACC-RPG-004")
        mz = rpgmaker.required_environment("ACC-RPG-008")
        self.assertIn("RETROM_CHROME_EXECUTABLE", catalog)
        self.assertIn("RETROM_CHROME_EXECUTABLE", xp)
        self.assertNotIn("RETROM_ACC_RPG_004_TRACE", xp)
        self.assertIn("RPG_MZ_SMOKE_PROVENANCE", mz)

    def test_extended_product_cases_require_chrome(self) -> None:
        for case_id in ("ACC-RPG-009", "ACC-RPG-010", "ACC-RPG-011"):
            self.assertIn("RETROM_CHROME_EXECUTABLE", rpgmaker.required_environment(case_id))
        self.assertNotIn(
            "RETROM_CHROME_EXECUTABLE",
            rpgmaker.required_environment("ACC-RPG-012"),
        )
        self.assertIn(
            "RETROM_ACC_RPG_009_PROVISION_EVIDENCE",
            rpgmaker.required_environment("ACC-RPG-009"),
        )

    def test_isolation_is_deferred_before_product_driver(self) -> None:
        environment = {
            "RETROM_ACCEPTANCE_BASE_URL": "https://retrom.example.test",
            "RETROM_ACCEPTANCE_USERNAME": "reviewer",
            "RETROM_ACCEPTANCE_PASSWORD": "secret",
        }
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.dict(os.environ, environment, clear=True), \
                mock.patch.object(rpgmaker.subprocess, "run") as run:
            case_dir = Path(directory)
            with redirect_stdout(io.StringIO()):
                self.assertEqual(3, rpgmaker.run("ACC-RPG-011", case_dir))
            run.assert_not_called()
            result = json.loads((case_dir / "rpgmaker-product.json").read_text())
            self.assertEqual([], result["missingInputs"])
            self.assertEqual("RPG_SEVEN_CORE_MINIMAL_CLOSURE_REQUIRED", result["reason"])

    def test_extended_cases_unlock_after_clean_seven_core_results(self) -> None:
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(os.environ, {}, clear=True):
            cases = Path(directory) / "cases"
            for case_id in rpgmaker.GENERATION_CASES:
                target = cases / case_id.lower()
                target.mkdir(parents=True)
                (target / "result.json").write_text(json.dumps({
                    "caseId": case_id,
                    "status": "PASS",
                    "gitDirty": False,
                    "productEvidence": {"caseId": case_id, "status": "PASS"},
                }))
            case_dir = cases / "acc-rpg-011"
            with redirect_stdout(io.StringIO()):
                self.assertEqual(3, rpgmaker.run("ACC-RPG-011", case_dir))
            result = json.loads((case_dir / "rpgmaker-product.json").read_text())
            self.assertEqual("缺少实际 Retrom 产品验收输入", result["reason"])
            self.assertIn("RETROM_CHROME_EXECUTABLE", result["missingInputs"])

    def test_missing_live_ids_are_blocked_and_machine_readable(self) -> None:
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(os.environ, {}, clear=True):
            case_dir = Path(directory)
            with redirect_stdout(io.StringIO()):
                self.assertEqual(3, rpgmaker.run("ACC-RPG-002", case_dir))
            result = json.loads((case_dir / "rpgmaker-product.json").read_text())
            self.assertEqual("BLOCKED", result["status"])
            self.assertIn("RETROM_ACC_RPG_002_IMPORT_ITEM_ID", result["missingInputs"])

    def test_pack_plan_v2_requires_named_input_review_and_reference_roles(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source_directory = root / "directory"
            source_directory.mkdir()
            (source_directory / "asset.bin").write_bytes(b"asset")
            sources = {"rpg2000Rtp": source_directory}
            for index, role in enumerate(sorted(set(rpgmaker.PACK_UPLOAD_ROLES) - {"rpg2000Rtp"})):
                suffix = ".zip" if index % 2 == 0 else ".7z"
                path = root / f"pack-{index}{suffix}"
                path.write_bytes(b"pack")
                sources[role] = path
            identifiers = [f"{number:08d}-1111-4111-8111-111111111111" for number in range(1, 30)]
            plan = {
                "schemaVersion": 2,
                "uploads": {
                    role: {
                        "sourcePath": str(sources[role]),
                        "sourceType": "DIRECTORY" if sources[role].is_dir() else "FILES",
                        "definitionId": identity[0], "generation": identity[1], "declaredName": identity[2],
                        "sourceNote": rpgmaker.PACK_SOURCE_NOTE,
                        "sourceFileCount": rpgmaker.pack_source_identity(
                            sources[role], "DIRECTORY" if sources[role].is_dir() else "FILES",
                        )[0],
                        "sourceSizeBytes": rpgmaker.pack_source_identity(
                            sources[role], "DIRECTORY" if sources[role].is_dir() else "FILES",
                        )[1],
                        "sourceSha256": rpgmaker.pack_source_identity(
                            sources[role], "DIRECTORY" if sources[role].is_dir() else "FILES",
                        )[2],
                    }
                    for role, identity in rpgmaker.PACK_UPLOAD_ROLES.items()
                },
                "reviewIds": dict(zip(sorted(rpgmaker.PACK_REVIEW_ROLES), identifiers[:13], strict=True)),
                "protectedReferences": {
                    "publishedVariant": {"installationId": identifiers[13], "gameId": identifiers[14]},
                    "restorableCheckpoint": {
                        "installationId": identifiers[15], "gameId": identifiers[16],
                        "saveStateId": identifiers[17],
                    },
                },
            }
            plan_path = root / "plan.json"
            plan_path.write_text(json.dumps(plan))
            self.assertEqual(plan, rpgmaker.pack_plan(plan_path))
            plan["uploads"].pop("rgss1Custom")
            plan_path.write_text(json.dumps(plan))
            with self.assertRaisesRegex(rpgmaker.ContractError, "UPLOAD_MATRIX_INCOMPLETE"):
                rpgmaker.pack_plan(plan_path)

    def test_pack_case_requires_a_fresh_database_path(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            database = Path(directory) / "retrom.db"
            database.write_bytes(b"sqlite")
            self.assertEqual(database, rpgmaker.pack_database(database))
            with self.assertRaisesRegex(rpgmaker.ContractError, "DATABASE_INVALID"):
                rpgmaker.pack_database(Path("relative.db"))

    def test_pack_evidence_requires_real_http_outcomes_and_no_paths(self) -> None:
        payload = pack_evidence_payload()
        rpgmaker.validate_pack_evidence(payload)
        payload["uploads"]["rpg2000Rtp"]["sourcePath"] = "/private/runtime-pack"
        with self.assertRaisesRegex(rpgmaker.ContractError, "SECRET_OR_PATH"):
            rpgmaker.validate_pack_evidence(payload)

    def test_pack_evidence_requires_actual_readiness_not_trial_proof(self) -> None:
        payload = pack_evidence_payload()
        rpgmaker.validate_pack_evidence(payload)
        selected = next(item for item in payload["reviews"]["matcherRejections"] if item["matcher"] == "SELECTED")
        selected["publishReadiness"]["current"] = False
        with self.assertRaisesRegex(rpgmaker.ContractError, "PUBLISH_READINESS"):
            rpgmaker.validate_pack_evidence(payload)
        payload = pack_evidence_payload()
        payload["databaseEvidence"]["selectedReviews"][0]["validationStatus"] = "BLOCKED"
        with self.assertRaisesRegex(rpgmaker.ContractError, "DATABASE_SELECTION"):
            rpgmaker.validate_pack_evidence(payload)

    def test_pack_evidence_rejects_unreleased_consumption_and_fake_selection(self) -> None:
        payload = pack_evidence_payload()
        payload["databaseEvidence"]["uploads"]["zeroReference"]["consumptionReleasedAtMs"] = None
        with self.assertRaisesRegex(rpgmaker.ContractError, "CONSUMPTION_EVIDENCE_INVALID"):
            rpgmaker.validate_pack_evidence(payload)
        payload = pack_evidence_payload()
        payload["databaseEvidence"]["selectedReviews"][0]["installationId"] = pack_uuid(900)
        with self.assertRaisesRegex(rpgmaker.ContractError, "SELECTION_EVIDENCE_INVALID"):
            rpgmaker.validate_pack_evidence(payload)

    def test_pack_evidence_requires_provider_target_identity_for_protected_references(self) -> None:
        payload = pack_evidence_payload()
        payload["databaseEvidence"]["protectedReferences"]["publishedVariant"].pop(
            "bundleSha256",
        )
        with self.assertRaisesRegex(rpgmaker.ContractError, "PROTECTED_REFERENCE_EVIDENCE_INVALID"):
            rpgmaker.validate_pack_evidence(payload)

    def test_pack_browser_capture_does_not_wait_for_polling_network_idle(self) -> None:
        source = PACK_BROWSER_PATH.read_text()
        self.assertNotIn('waitUntil: "networkidle"', source)
        self.assertIn('waitUntil: "domcontentloaded", timeout: 120_000', source)
        self.assertIn('locator("main.content h1").first().waitFor', source)

    def test_pack_http_failure_reports_only_the_stable_error_code(self) -> None:
        source = PACK_BROWSER_PATH.read_text()
        self.assertIn("await responseErrorCode(response)", source)
        self.assertIn("body?.error?.code", source)
        self.assertNotIn("body?.error?.message", source)

    def test_pack_install_retries_only_the_typed_resource_failure(self) -> None:
        source = PACK_BROWSER_PATH.read_text()
        retry = source.split("async function installWithResourceRetry", 1)[1].split(
            "async function uploadFile", 1,
        )[0]
        self.assertIn('response.status() !== 503', retry)
        self.assertIn('code !== "RPG_RUNTIME_PACK_UNAVAILABLE"', retry)
        self.assertIn("attempt === 2", retry)
        self.assertNotIn("RPG_RUNTIME_PACK_INVALID", retry)

    def test_content_security_evidence_requires_exact_product_observations(self) -> None:
        payload = content_security_evidence_payload()
        rpgmaker.validate_security_evidence(payload, "ACC-RPG-010")

    def test_content_security_evidence_requires_the_detector_matrix_boundary(self) -> None:
        payload = content_security_evidence_payload()
        payload["detectorMatrix"]["combinationCount"] = 41
        with self.assertRaisesRegex(rpgmaker.ContractError, "DETECTOR_MATRIX_INVALID"):
            rpgmaker.validate_security_evidence(payload, "ACC-RPG-010")

    def test_content_security_evidence_rejects_a_different_detector_matrix(self) -> None:
        payload = content_security_evidence_payload()
        payload["detectorMatrix"]["matrixSha256"] = "f" * 64
        with self.assertRaisesRegex(rpgmaker.ContractError, "DETECTOR_MATRIX_INVALID"):
            rpgmaker.validate_security_evidence(payload, "ACC-RPG-010")

    def test_content_security_evidence_requires_exact_nested_content_projection(self) -> None:
        for field, value in (
            ("postInspectionFilesDigest", "f" * 64),
            ("launchFinished", False),
        ):
            payload = content_security_evidence_payload()
            payload["nestedArchives"][0][field] = value
            with self.assertRaisesRegex(rpgmaker.ContractError, "NESTED_ARCHIVE_EVIDENCE_INVALID"):
                rpgmaker.validate_security_evidence(payload, "ACC-RPG-010")
        payload = content_security_evidence_payload()
        payload["nestedArchives"][20]["projection"]["exactMember"] = False
        with self.assertRaisesRegex(rpgmaker.ContractError, "NESTED_ARCHIVE_EVIDENCE_INVALID"):
            rpgmaker.validate_security_evidence(payload, "ACC-RPG-010")

    def test_content_security_evidence_requires_exact_unsafe_matrix(self) -> None:
        payload = content_security_evidence_payload()
        payload["unsafe"][0]["status"] = 422
        with self.assertRaisesRegex(rpgmaker.ContractError, "UNSAFE_MATRIX_INVALID"):
            rpgmaker.validate_security_evidence(payload, "ACC-RPG-010")

    def test_isolation_evidence_requires_bootstrap_and_cross_launch_checkpoint(self) -> None:
        payload = isolation_evidence_payload()
        rpgmaker.validate_security_evidence(payload, "ACC-RPG-011")
        payload["harnesses"][0]["bootstrap"]["replayStatus"] = 204
        with self.assertRaisesRegex(rpgmaker.ContractError, "ISOLATION_BOOTSTRAP_INVALID"):
            rpgmaker.validate_security_evidence(payload, "ACC-RPG-011")

    def test_isolation_evidence_requires_csp_probe_origin_and_route(self) -> None:
        payload = isolation_evidence_payload()
        rpgmaker.validate_security_evidence(payload, "ACC-RPG-011")
        for field in ("csp", "probes", "config", "runtimeOrigin"):
            changed = json.loads(json.dumps(payload))
            del changed["harnesses"][0][field]
            with self.assertRaisesRegex(rpgmaker.ContractError, "ISOLATION_.*_INVALID"):
                rpgmaker.validate_security_evidence(changed, "ACC-RPG-011")

    def test_isolation_evidence_requires_exactly_one_harness_per_generation(self) -> None:
        payload = isolation_evidence_payload()
        payload["harnesses"].append(json.loads(json.dumps(payload["harnesses"][1])))
        with self.assertRaisesRegex(rpgmaker.ContractError, "ISOLATION_HARNESS_INCOMPLETE"):
            rpgmaker.validate_security_evidence(payload, "ACC-RPG-011")

    def test_isolation_evidence_requires_launch_ids_unique_across_generations(self) -> None:
        payload = isolation_evidence_payload()
        original = payload["harnesses"][0]["originalLaunchId"]
        duplicate = payload["harnesses"][1]
        duplicate["originalLaunchId"] = original
        duplicate["runtimeOrigin"] = f"https://{original}.rpg-runtime.example.test"
        duplicate["checkpointRoundTrip"]["originalLaunchId"] = original
        with self.assertRaisesRegex(rpgmaker.ContractError, "ISOLATION_HARNESS_INCOMPLETE"):
            rpgmaker.validate_security_evidence(payload, "ACC-RPG-011")


def pack_uuid(number: int) -> str:
    return f"{number:08d}-1111-4111-8111-111111111111"


def pack_job(number: int, kind: str, input_digest: bool = False) -> dict:
    result = {
        "jobId": pack_uuid(number), "kind": kind, "state": "SUCCEEDED",
        "events": ["QUEUED", "STARTED", "SUCCEEDED"],
    }
    if input_digest:
        result["inputDigest"] = "d" * 64
    return result


def pack_evidence_payload() -> dict:
    roles = sorted(rpgmaker.PACK_UPLOAD_ROLES)
    uploads, installations, database_uploads = {}, {}, {}
    for index, role in enumerate(roles, start=1):
        upload_id, installation_id = pack_uuid(index), pack_uuid(index + 20)
        finalize = pack_job(index + 40, "UPLOAD_FINALIZE")
        validation = pack_job(index + 60, "RUNTIME_ASSET_PACK_VALIDATE", True)
        uploads[role] = {
            "role": role, "uploadId": upload_id, "installationId": installation_id,
            "jobId": validation["jobId"], "definitionId": rpgmaker.PACK_UPLOAD_ROLES[role][0],
            "finalizeJob": finalize, "validationJob": validation,
        }
        installations[role] = {
            "installationId": installation_id, "definitionId": "definition",
            "status": "READY", "filesDigest": f"{index:064x}", "bundleSha256": f"{index + 20:064x}",
        }
        database_uploads[role] = {
            "uploadId": upload_id, "installationId": installation_id, "consumptionId": pack_uuid(index + 80),
            "sessionState": "COMPLETE", "consumptionReleasedAtMs": 10 if role == "zeroReference" else None,
            "consumptionReleaseReason": "UPLOAD_CONSUMED" if role == "zeroReference" else None,
            "finalizeJob": finalize, "validationJob": validation,
        }
    published_roles = [
        "rpg2000SelfContained", "rpg2003SelfContained", "rpgxpNoRtp", "rpgvxNoRtp", "rpgvxaceNoRtp",
    ]
    published = [
        {
            "role": role, "itemId": pack_uuid(120 + index), "gameId": pack_uuid(130 + index),
            "generation": "RPGXP", "status": 201,
        }
        for index, role in enumerate(published_roles)
    ]
    missing_roles = ["rpg2000Missing", "rpg2003Missing", "rpgxpCustom", "rpgvxCustom", "rpgvxaceCustom"]
    outcomes = [
        {
            "role": role, "matcher": "MISSING", "patchStatus": 422, "patchCode": "REVIEW_DRAFT_INVALID",
            "publish": {"status": 409, "code": "REVIEW_VALIDATION_STALE"},
        }
        for role in missing_roles
    ]
    selected = []
    for index, role in enumerate(missing_roles):
        item = {
            "role": role, "itemId": pack_uuid(150 + index), "matcher": "SELECTED", "patchStatus": 200,
            "installationId": installations[[
                "rpg2000Rtp", "rpg2003Rtp", "rgss1Custom", "rgss2Custom", "rgss3Custom",
            ][index]]["installationId"],
            "publishReadiness": {"canApprove": True, "current": True, "status": "READY"},
        }
        outcomes.append(item)
        selected.append({**{key: item[key] for key in ("role", "itemId", "installationId")},
                         "validationStatus": "READY", "dependencyInstallationId": item["installationId"]})
    for index, (role, upload_role) in enumerate((
        ("rpgxpStandardAmbiguous", "rgss1StandardV1"),
        ("rpgvxStandardAmbiguous", "rgss2StandardV1"),
        ("rpgvxaceStandardAmbiguous", "rgss3StandardV1"),
    )):
        item = {
            "role": role, "itemId": pack_uuid(160 + index), "matcher": "AMBIGUOUS", "patchStatus": 200,
            "installationId": installations[upload_role]["installationId"],
            "rejectionStatus": 422, "rejectionCode": "REVIEW_DRAFT_INVALID",
            "publishReadiness": {"canApprove": True, "current": True, "status": "READY"},
        }
        outcomes.append(item)
        selected.append({**{key: item[key] for key in ("role", "itemId", "installationId")},
                         "validationStatus": "READY", "dependencyInstallationId": item["installationId"]})
    protected_references = {
        "publishedVariant": {"installationId": pack_uuid(180), "gameId": pack_uuid(181)},
        "restorableCheckpoint": {
            "installationId": pack_uuid(182), "gameId": pack_uuid(183), "saveStateId": pack_uuid(184),
        },
    }
    protected_database = {
        "publishedVariant": {
            **protected_references["publishedVariant"], "definitionId": "rgss1_standard",
            "availableForLaunch": True, "providerId": "retrom-runtime", "targetId": "rpgmaker-xp",
            "bundleSha256": "c" * 64,
        },
        "restorableCheckpoint": {
            **protected_references["restorableCheckpoint"], "definitionId": "rgss2_rpgvx",
            "availableForLaunch": True, "providerId": "retrom-runtime", "targetId": "rpgmaker-vx",
            "bundleSha256": "d" * 64,
        },
    }
    zero_upload = database_uploads["zeroReference"]
    release_job = pack_job(190, "PAYLOAD_RELEASE", True)
    population = {"before": {"games": [], "saves": [], "reviews": []},
                  "after": {"games": [], "saves": [], "reviews": []}}
    return {
        "schemaVersion": 1, "caseId": "ACC-RPG-009", "status": "PASS",
        "populationPreservation": population,
        "uploads": uploads, "installations": installations,
        "reviews": {"published": published, "matcherRejections": outcomes},
        "protectedReferences": protected_references,
        "protectedDeletes": [
            {"role": role, "status": 409, "code": "RPG_RUNTIME_PACK_IN_USE"}
            for role in ("publishedVariant", "restorableCheckpoint")
        ],
        "zeroReferenceDelete": {
            "staleStatus": 412, "currentStatus": 204, "finalStatus": "DELETED", "deletedAtMs": 1,
        },
        "screenshots": [
            "screenshots/rpgmaker-pack-catalog.png", "screenshots/rpgmaker-pack-review-binding.png",
        ],
        "databaseEvidence": {
            "schemaVersion": 1, "uploads": database_uploads,
            "provisioningEvidence": {"payload": {"populationPreservation": population}},
            "publishedReviews": [
                {key: item[key] for key in ("role", "itemId", "gameId")} for item in published
            ],
            "selectedReviews": selected, "protectedReferences": protected_database,
            "zeroReferenceRelease": {
                "consumptionId": zero_upload["consumptionId"], "releaseReason": "UPLOAD_CONSUMED",
                "uploadFileCount": 1, "purgedFileCount": 1, "completionAuditCount": 1,
                "gcFirstUnreferencedAtMs": 10, "gcScheduledAtMs": 20,
                "bundleSha256": installations["zeroReference"]["bundleSha256"], "job": release_job,
            },
        },
    }


def product_payload(spec, digest: str) -> dict:
    original = "11111111-1111-4111-8111-111111111111"
    restore = "22222222-2222-4222-8222-222222222222"
    product = "33333333-3333-4333-8333-333333333333"
    target_contract = "c" * 64
    position_a = {"mapId": 1, "playerX": 1, "playerY": 1, "fixtureState": 0}
    position_b = {"mapId": 1, "playerX": 2, "playerY": 1, "fixtureState": 1}
    position_c = {"mapId": 1, "playerX": 3, "playerY": 1, "fixtureState": 2}
    position_after = {"mapId": 1, "playerX": 4, "playerY": 1, "fixtureState": 2}
    expected_marker, expected_rgb = rpgmaker.MARKERS[spec.generation]
    marker = (expected_marker, list(expected_rgb) if expected_rgb is not None else [34, 197, 94])
    import_id = "66666666-6666-4666-8666-666666666666"
    upload_id = "77777777-7777-4777-8777-777777777777"
    restore_screenshot = f"screenshots/{spec.core_id}-restored-marker.png"
    payload = {
        "schemaVersion": 1, "status": "PASS",
        "review": {
            "itemId": "55555555-5555-4555-8555-555555555555",
            "rpgMaker": {
                "selectedCoreId": spec.core_id, "generation": spec.generation,
                "evidenceGeneration": spec.evidence_generation, "evidenceConfidence": spec.confidence,

            },
        },
        "runtimeTrial": {
            "schemaVersion": 1, "kind": "DEVELOPMENT_RUNTIME_TRIAL",
            "importItemId": "55555555-5555-4555-8555-555555555555",
            "launchId": original, "restoreLaunchId": restore,
            "startedAtMs": 1000, "finishedAtMs": 2000,
            "frameProgress": {"original": {"beforeFrame": 60, "afterFrame": 361},
                              "restored": {"beforeFrame": 60, "afterFrame": 361}},
            "audio": {"contexts": 1, "observedSamples": 4096, "peakAbsoluteSample": 0.25},
            "restoredScreenshot": {"fileName": "trial-restored.png", "sha256": "9" * 64, "sizeBytes": 1024},
            "routeEvidence": {
                "effectiveSourceSnapshotId": "44444444-4444-4444-8444-444444444444",
                "providerId": "retrom-runtime", "targetId": spec.target_id, "generation": spec.generation,
                "evidenceGeneration": spec.evidence_generation, "evidenceConfidence": spec.confidence,
                "projectFingerprint": digest, "dependencySnapshotSha256": "d" * 64,
            },
            "checkpointRoundTrip": {
                "originalLaunchEnded": True, "frozenRestoreSha256": "e" * 64,
                "originalLaunchId": original, "restoreLaunchId": restore,
                "initialPosition": position_a, "savedPosition": position_b,
                "divergedPosition": position_c, "restoredPosition": dict(position_b),
                "restoreInputPosition": position_after, "sha256": "e" * 64,
                "format": "mkxp-state-compact-v1" if spec.generation in {"RPGXP", "RPGVX", "RPGVXACE"}
                else "provider-checkpoint-v1",
                "sizeBytes": 1_048_576 if spec.generation in {"RPGXP", "RPGVX", "RPGVXACE"} else 4_096,
            },
        },
        "productLaunch": {
            "launchId": product, "playerRunning": True,
            "runtimeProgress": {
                "firstLaunch": {"beforeFrame": 62, "afterFrame": 64},
                "cacheLaunch": {"beforeFrame": 60, "afterFrame": 63},
            },
            "config": {
                "purpose": "PRODUCT", "providerId": "retrom-runtime", "providerVersion": "0.12.0",
                "targetId": spec.target_id, "bundleSha256": target_contract,
                "checkpointFormat": "provider-checkpoint-v1", "checkpointMaxBytes": 64 * 1024 * 1024,
            },
        },
        "inputTranscript": {
            "transportScheme": "HTTPS",
            "upload": {
                "uploadId": upload_id, "state": "COMPLETE", "purpose": "PROJECT",
                "sourceType": "DIRECTORY", "fileCount": 10, "totalBytes": 1024,
                "receivedBytes": 1024, "finalizationNo": 1,
            },
            "import": {
                "importJobId": import_id, "uploadId": upload_id, "state": "COMPLETED",
                "payloadState": "RELEASED", "platformId": "rpgmaker",
                "defaultCoreId": rpgmaker.USER_CORE_ID, "providerId": "retrom-runtime",
                "targetId": spec.target_id,
                "counts": {
                    "total": 1, "queued": 0, "running": 0, "reviewPending": 0,
                    "published": 1, "discarded": 0, "failed": 0, "cancelled": 0,
                    "unresolvedRejectedFiles": 0,
                },
                "createdAtMs": 1_000, "updatedAtMs": 2_000,
            },
        },
        "inputProvenance": {
            "schemaVersion": 1, "kind": "RETROM_OWNED_PUBLIC_FIXTURE",
            "projectFingerprint": digest, "fileCount": 10, "totalBytes": 1024,
            "marker": marker[0], "markerRgb": marker[1], "engineVersion": None,
            "licenseBasis": "RETROM_MIT", "licenseUrl": None,
            "sourceUrl": None, "sourceVersion": "fixture-manifest-v1",
            "sourceSha256": "f" * 64,
        },
        "restoreVisualEvidence": {
            "screenshot": restore_screenshot,
            "sha256": "9" * 64, "width": 640, "height": 480,
            "opaquePixels": 640 * 480, "nonBlackPixels": 640 * 480,
            "distinctColorBuckets": 4, "marker": marker[0], "markerRgb": marker[1],
            "markerPixelCount": 128,
        },
        "runtimeEnvironment": {
            "chromeVersion": "Chrome/128.0.6613.84",
            "engineVersion": None,
            "engineProfile": rpgmaker.ENGINE_PROFILES[spec.generation],
            "trialDurationMs": 1000,
        },
        "loading": runtime_loading_evidence(spec.generation),
        "screenshots": [restore_screenshot, f"screenshots/{spec.core_id}-product-player.png"],
    }
    if spec.generation in {"RPGMV", "RPGMZ"}:
        payload["runtimeEnvironment"]["engineVersion"] = "1.6.1" if spec.generation == "RPGMV" else "1.8.0"
        runtime_origin = f"https://{product}.rpg-runtime.example.test"
        payload["originInventory"] = {
            "appOrigin": {
                "origin": "https://retrom.example.test", "documentResponses": 1,
                "scriptResponses": 12, "projectResourceResponses": 0,
                "domProjectResourceReferences": 0, "cacheProjectResourceEntries": 0,
            },
            "runtimeOrigin": {
                "origin": runtime_origin, "documentResponses": 1, "scriptResponses": 8,
                "projectResourceResponses": 24,
            },
            "unexpectedOrigins": [],
        }
    if spec.generation == "RPGMZ":
        payload["inputProvenance"].update({
            "kind": "LICENSED_EXTERNAL_WEB_DEPLOYMENT", "engineVersion": "1.8.0",
            "licenseBasis": "OPEN_SOURCE_LICENSE",
            "licenseUrl": "https://example.test/LICENSE",
            "sourceUrl": "https://example.test/mz-smoke",
            "sourceVersion": "v1.0.0", "sourceSha256": "8" * 64,
            "transformation": mz_transformation(digest, 10, 1024),
        })
        payload["restoreVisualEvidence"].update({
            "sceneExclusion": dict(rpgmaker.MZ_SCENE_EXCLUSION),
            "sceneNonBlackPixels": 640 * 480,
            "sceneDistinctColorBuckets": 32,
        })
    if spec.generation in {"RPGXP", "RPGVX", "RPGVXACE"}:
        payload["productLaunch"]["config"].update({
            "checkpointFormat": "mkxp-state-compact-v1", "checkpointMaxBytes": 268_435_456,
        })
    if spec.generation == "RPGXP":
        payload["xpRuntimeTrace"] = xp_runtime_trace("e" * 64)
    return payload


def runtime_loading_evidence(generation: str) -> dict:
    native = generation in {"RPGMV", "RPGMZ"}
    mkxp = generation in {"RPGXP", "RPGVX", "RPGVXACE"}
    cache_launch_id = "88888888-8888-4888-8888-888888888888"

    def snapshot(cache_hits: int) -> dict:
        return {
            "declaredLargeFileCount": 1 if mkxp else 0,
            "declaredProjectBytes": 32 * 1024 * 1024 if mkxp else (0 if native else 1024),
            "declaredProjectFileCount": 1 if mkxp else (0 if native else 10),
            "fullProjectFileResponseCount": 0 if mkxp or native else 3,
            "nativeProjectResponseCount": 4 if native else 0,
            "projectContentIdentityCount": 0 if native else 1,
            "rangeProjectFileResponseCount": 4 if mkxp else 0,
            "requestedLargeFileCount": 1 if mkxp else 0,
            "requestedProjectBytes": 512 * 1024 if mkxp else (0 if native else 256),
            "requestedProjectFileCount": 1 if mkxp else (0 if native else 3),
            "runtimeAssetCacheHitCount": cache_hits,
            "runtimeAssetRequestCount": 0 if native else 2,
            "runtimeAssetTransferredBytes": 0 if cache_hits else 1_000_000,
        }

    return {
        "schemaVersion": 1,
        "cacheLaunchId": cache_launch_id,
        "sameProjectContentIdentity": None if native else True,
        "firstVisible": snapshot(0),
        "cacheLaunchVisible": snapshot(0 if native else 2),
    }


def mz_transformation(digest: str, file_count: int, total_bytes: int) -> dict:
    removed_names = [
        "instructions.pdf", "save/config.rmmzsave", "save/global.rmmzsave",
        *(f"save/file{index}.rmmzsave" for index in range(7)),
    ]
    return {
        "schemaVersion": 1, "recipe": "RETROM_MZ_MINIMAL_V3",
        "tool": "scripts/acceptance/rpgmaker_mz_prepare.py",
        "sourceSizeBytes": rpgmaker.MZ_SOURCE_SIZE_BYTES,
        "removedEntries": [
            {
                "logicalName": name,
                "reason": "ROOT_DOCUMENTATION_EXCLUDED" if name.endswith(".pdf") else "PACKAGED_SAVE_EXCLUDED",
                "sizeBytes": 1, "sha256": "7" * 64,
            }
            for name in removed_names
        ],
        "injectedFiles": [
            {"logicalName": name, "sizeBytes": 1, "sha256": "6" * 64}
            for name in ("js/plugins.js", "js/plugins/RetromMinimalAcceptance.js")
        ],
        "outputProjectFingerprint": digest,
        "outputFileCount": file_count, "outputTotalBytes": total_bytes,
    }



def content_security_evidence_payload() -> dict:
    targets = {
        "RPG2000": "rpgmaker-2000", "RPG2003": "rpgmaker-2003", "RPGXP": "rpgmaker-xp",
        "RPGVX": "rpgmaker-vx", "RPGVXACE": "rpgmaker-vx-ace", "RPGMV": "rpgmaker-mv",
        "RPGMZ": "rpgmaker-mz",
    }
    unsafe_specs = (
        ("dual-root", False, 409, "RPG_PROJECT_ROOT_AMBIGUOUS"),
        ("multi-generation", False, 409, "RPG_GENERATION_AMBIGUOUS"),
        ("rgss-conflict", False, 422, "RPG_RGSS_GENERATION_CONFLICT"),
        ("lcf-truncated", False, 422, "RPG_LCF_INVALID"),
        ("case-collision", False, 422, "RPG_PATH_COLLISION"),
        ("nfkc-collision", False, 422, "RPG_PATH_COLLISION"),
        ("gencache-collision", False, 409, "IMPORT_INPUT_INVALID"),
        ("traversal", False, 409, "IMPORT_INPUT_INVALID"),
        ("symlink", False, 409, "IMPORT_INPUT_INVALID"),
        ("bomb", False, 413, "ARCHIVE_LIMIT_EXCEEDED"),
        ("external", False, 422, "RPG_NATIVE_DEPENDENCY_UNSUPPORTED"),
        ("referenced-native", False, 422, "RPG_NATIVE_DEPENDENCY_UNSUPPORTED"),
        ("opaque-native", True, 202, None),
    )
    nested = []
    for generation_index, generation in enumerate(targets):
        if generation in {"RPG2000", "RPG2003"}:
            projection_kind = "EASYRPG_PROJECT_FILE"
        elif generation in {"RPGXP", "RPGVX", "RPGVXACE"}:
            projection_kind = "MKXP_ARCHIVE_MEMBER"
        else:
            projection_kind = "NATIVE_WEB_DENIED"
        for format_index, format_name in enumerate(("7Z", "GZIP", "RAR", "TAR", "ZIP")):
            for detection_index, detection in enumerate(("extension", "magic")):
                index = generation_index * 10 + format_index * 2 + detection_index
                logical_name = f"RetromNested/nested-{format_name.lower()}-{detection}"
                digest = f"{index + 10:064x}"
                projected = generation not in {"RPGMV", "RPGMZ"}
                nested.append({
                    "generation": generation, "format": format_name, "detection": detection,
                    "sidecar": logical_name, "sha256": digest, "sizeBytes": index + 1,
                    "filesDigest": f"{index + 100:064x}",
                    "postInspectionFilesDigest": f"{index + 100:064x}", "nestedEntryCount": 0,
                    "importJobId": pack_uuid(200 + index), "importItemId": pack_uuid(300 + index),
                    "contentIdentityDigest": f"{index + 200:064x}",
                    "launchId": pack_uuid(500 + index),
                    "providerId": "retrom-runtime", "targetId": targets[generation],
                    "bundleSha256": f"{generation_index + 600:064x}",
                    "projection": {
                        "kind": projection_kind, "status": 200 if projected else 404,
                        "logicalName": logical_name, "sha256": digest if projected else None,
                        "sizeBytes": index + 1 if projected else None,
                        "containerSha256": f"{index + 300:064x}" if projected else None,
                        "exactMember": projected,
                    },
                    "launchFinished": True,
                })
    opaque_names = ("Game.exe", "nw.dll", "plugin.node", "launcher.bat")
    return {
        "schemaVersion": 1, "caseId": "ACC-RPG-010", "status": "PASS",
        "detectorMatrix": {
            "boundary": "GO_DETECTOR_UNIT",
            "testName": "TestPublicWrongCoreMatrixHasFortyTwoMismatches",
            "combinationCount": 42,
            "expectedCode": "RPG_SELECTED_CORE_MISMATCH",
            "matrixSha256": hashlib.sha256(rpgmaker.SECURITY_MATRIX_PATH.read_bytes()).hexdigest(),
            "log": "detector-matrix.log",
        },
        "unsafe": [
            {"name": name, "accepted": accepted, "status": status, "code": code}
            for name, accepted, status, code in unsafe_specs
        ],
        "nestedArchives": nested,
        "opaqueNative": {
            "importItemId": pack_uuid(802), "generation": "RPGMZ", "filesDigest": "a" * 64,
            "sourceFiles": [
                {"name": name, "sha256": "b" * 64, "sizeBytes": 1} for name in opaque_names
            ],
            "runtimeProjection": [{"name": name, "status": 404} for name in opaque_names],
            "launchId": pack_uuid(803), "runtimeOrigin": f"https://{pack_uuid(803)}.example.test",
            "launchFinished": True,
        },
        "screenshots": ["screenshots/acc-rpg-010-opaque-native.png"],
    }


def isolation_evidence_payload() -> dict:
    harnesses = []
    for index, generation in enumerate(("RPGMV", "RPGMZ"), start=1):
        original = f"{index}1111111-1111-4111-8111-111111111111"
        restore = f"{index}2222222-2222-4222-8222-222222222222"
        checkpoint = checkpoint_payload(original, restore)
        harnesses.append({
            "generation": generation,
            "importItemId": f"{index}3333333-3333-4333-8333-333333333333",
            "originalLaunchId": original, "restoreLaunchId": restore,
            "runtimeOrigin": f"https://{original}.rpg-runtime.example.test",
            "config": {
                "providerId": "retrom-runtime",
                "targetId": "rpgmaker-mv" if generation == "RPGMV" else "rpgmaker-mz",
                "bundleSha256": f"{index:064x}",
            },
            "originalScreenshot": f"screenshots/acc-rpg-011-{generation.lower()}.png",
            "csp": "base-uri 'self'; worker-src 'self' blob:; connect-src 'self'",
            "probes": {
                "parentDom": "blocked", "appCookie": "none", "topNavigation": "blocked",
                "popup": "blocked", "form": "attempted", "externalFetch": "blocked",
                "nonAllowlistApi": "404", "serviceWorker": "blocked", "complete": "true",
            },
            "securityRequests": [{"urlKind": "nonAllowlistApi", "status": 404}],
            "restoreScreenshot": f"screenshots/acc-rpg-011-{generation.lower()}-restore.png",
            "bootstrap": {
                "authenticatedReloadStatus": 303, "replayStatus": 410,
                "appHostEntryStatus": 404, "runtimeApiStatus": 404,
                "confusedHostStatus": 404, "inactiveBootstrapStatus": 410,
            },
            "checkpointRoundTrip": checkpoint,
            "frameProgress": {"original": {"beforeFrame": 60, "afterFrame": 361},
                              "restored": {"beforeFrame": 60, "afterFrame": 361}},
            "audio": {"contexts": 1, "observedSamples": 4096, "peakAbsoluteSample": 0.25},
            "startedAtMs": 1000, "finishedAtMs": 2000,
        })
    return {
        "schemaVersion": 1, "caseId": "ACC-RPG-011", "status": "PASS", "harnesses": harnesses,
        "screenshots": [
            "screenshots/acc-rpg-011-rpgmv.png", "screenshots/acc-rpg-011-rpgmv-restore.png",
            "screenshots/acc-rpg-011-rpgmz.png", "screenshots/acc-rpg-011-rpgmz-restore.png",
        ],
    }


def checkpoint_payload(original: str, restore: str) -> dict:
    position_a = {"mapId": 1, "playerX": 1, "playerY": 1, "fixtureState": 0}
    position_b = {"mapId": 1, "playerX": 2, "playerY": 1, "fixtureState": 1}
    return {
        "originalLaunchEnded": True, "frozenRestoreSha256": "e" * 64,
        "originalLaunchId": original, "restoreLaunchId": restore,
        "initialPosition": position_a, "savedPosition": position_b,
        "divergedPosition": {"mapId": 1, "playerX": 3, "playerY": 1, "fixtureState": 2},
        "restoredPosition": dict(position_b),
        "restoreInputPosition": {"mapId": 1, "playerX": 4, "playerY": 1, "fixtureState": 2},
        "sha256": "e" * 64, "format": "rpgmv-save-v1", "sizeBytes": 4096,
    }

def xp_runtime_trace(checkpoint_sha256: str) -> dict:
    capabilities = {
        "secureContext": False, "crossOriginIsolated": False, "sharedArrayBuffer": False,
    }
    return {
        "schemaVersion": 1,
        "checkpointUpload": {
            "requestPayloadBytes": 1_048_576,
            "requestContentLengthBytes": 1_049_248,
            "responseStatus": 201, "sha256": checkpoint_sha256,
            "startedAtMs": 10_000, "finishedAtMs": 20_000,
        },
        "oversizeRejection": {
            "declaredContentLengthBytes": 283_115_521,
            "responseStatus": 413, "errorCode": "REQUEST_TOO_LARGE",
            "startedAtMs": 21_000, "finishedAtMs": 21_100,
        },
        "threadCapabilityRejections": [
            {
                "attemptId": "88888888-8888-4888-8888-888888888888",
                "phase": "PREVIEW", "capabilities": capabilities,
                "responseStatus": 422, "errorCode": "REVIEW_PREVIEW_CLIENT_UNSUPPORTED",
                "launchCredentialIssued": False, "projectPayloadRequestCount": 0,
            },
            {
                "attemptId": "99999999-9999-4999-8999-999999999999",
                "phase": "RESTORE", "capabilities": capabilities,
                "responseStatus": 422, "errorCode": "REVIEW_PREVIEW_CLIENT_UNSUPPORTED",
                "launchCredentialIssued": False, "projectPayloadRequestCount": 0,
            },
        ],
    }


def test_png(width: int, height: int, marker_rgb: list[int], solid: bool = False) -> bytes:
    rows = bytearray()
    for y in range(height):
        rows.append(0)
        for x in range(width):
            if solid:
                color = marker_rgb
            elif 24 <= x < 72 and 24 <= y < 56:
                color = marker_rgb
            elif x < width // 2:
                color = [15, 23, 42]
            else:
                color = [240, 240, 240]
            rows.extend(color)
    signature = b"\x89PNG\r\n\x1a\n"
    return signature + png_chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)) + \
        png_chunk(b"IDAT", zlib.compress(bytes(rows), level=9)) + png_chunk(b"IEND", b"")


def test_mz_overlay_only_png(width: int, height: int, marker_rgb: list[int]) -> bytes:
    rows = bytearray()
    for y in range(height):
        rows.append(0)
        for x in range(width):
            if 24 <= x < 384 and 24 <= y < 32:
                color = marker_rgb
            elif 24 <= x < 384 and 32 <= y < 96:
                color = [16, 24, 39]
            elif 44 <= x < 300 and 48 <= y < 76:
                color = [240, 240, 240]
            elif width - 64 <= x < width - 32 and 40 <= y < 72:
                color = [96, 96, 96]
            else:
                color = [0, 0, 0]
            rows.extend(color)
    signature = b"\x89PNG\r\n\x1a\n"
    return signature + png_chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)) + \
        png_chunk(b"IDAT", zlib.compress(bytes(rows), level=9)) + png_chunk(b"IEND", b"")


def png_chunk(kind: bytes, contents: bytes) -> bytes:
    return struct.pack(">I", len(contents)) + kind + contents + \
        struct.pack(">I", zlib.crc32(kind + contents) & 0xFFFFFFFF)


if __name__ == "__main__":
    unittest.main()
