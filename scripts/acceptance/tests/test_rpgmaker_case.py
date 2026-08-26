from __future__ import annotations

import hashlib
import importlib.util
import io
import json
import os
import struct
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
COMPATIBILITY_BROWSER_PATH = Path(__file__).resolve().parents[1] / "rpgmaker_compatibility.mjs"
COMPATIBILITY_PROVISION_PATH = (
    Path(__file__).resolve().parents[1] / "rpgmaker_compatibility_provision.mjs"
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

    def test_compatibility_projects_have_distinct_locked_content_identity(self) -> None:
        fixture_root = Path(__file__).resolve().parents[3] / "testdata/public-roms/rpgmaker-smoke"
        old_digest, old_count, old_bytes = rpgmaker.project_digest(fixture_root / "rpg2000")
        new_digest, new_count, new_bytes = rpgmaker.project_digest(fixture_root / "rpg2000-compat")
        self.assertEqual(old_count, new_count)
        self.assertGreater(old_bytes, 0)
        self.assertGreater(new_bytes, 0)
        self.assertNotEqual(old_digest, new_digest)


class EvidenceContractTests(unittest.TestCase):
    def test_isolation_driver_accepts_csp_block_before_request_dispatch(self) -> None:
        source = SECURITY_BROWSER_PATH.read_text()
        self.assertIn('"popup", "externalFetch", "serviceWorker"', source)
        self.assertIn('exact(probes?.[name], "blocked"', source)
        self.assertIn('request.urlKind === "external" && request.status !== 0', source)
        self.assertNotIn('request.urlKind === "external" && request.status === 0', source)

    def test_isolation_driver_focuses_runtime_canvas_and_surfaces_action_errors(self) -> None:
        source = SECURITY_BROWSER_PATH.read_text()
        self.assertIn("element.tabIndex = 0;", source)
        self.assertIn("element.focus();", source)
        self.assertIn("await page.waitForTimeout(800);", source)
        self.assertIn("RPG_ACCEPTANCE_RUNTIME_ACTION_", source)

    def test_product_drivers_reveal_toolbar_through_the_visible_hud_handle(self) -> None:
        for path in (BROWSER_PATH, COMPATIBILITY_BROWSER_PATH):
            source = path.read_text()
            self.assertIn('page.locator(".player-hud-handle").click()', source)
            self.assertIn('page.locator(".player-toolbar.is-visible").waitFor', source)
            self.assertNotIn("page.mouse.move(400, 1)", source)

    def test_compatibility_driver_uses_the_product_launch_url_contract(self) -> None:
        source = COMPATIBILITY_BROWSER_PATH.read_text()
        self.assertEqual(2, source.count("${launch.playUrl}"))
        self.assertNotIn("${launch.playerUrl}", source)

    def test_compatibility_driver_opens_the_product_overflow_before_debug(self) -> None:
        source = COMPATIBILITY_BROWSER_PATH.read_text()
        overflow = source.index('page.getByRole("button", { name: "更多操作" })')
        debug = source.index('page.locator(".player-debug-control")')
        self.assertLess(overflow, debug)
        self.assertIn("await moreActions.click();", source)
        self.assertNotIn('.click({ force: true })', source)

    def test_compatibility_driver_keeps_internal_rpg_binding_out_of_player_ui(self) -> None:
        source = COMPATIBILITY_BROWSER_PATH.read_text()
        self.assertIn(
            "[config.routeKey, config.artifactId, config.adapter?.adapterId]", source
        )
        self.assertIn("RPG_ACCEPTANCE_PLAYER_DIAGNOSTIC_IMPLEMENTATION_LEAK", source)
        self.assertNotIn("!text.includes(config.artifactId)", source)

    def test_compatibility_driver_reads_the_standard_error_envelope(self) -> None:
        source = COMPATIBILITY_BROWSER_PATH.read_text()
        self.assertIn("const code = response.error?.code;", source)
        self.assertNotIn('response.code !== "LAUNCH_BLOCKED"', source)

    def test_pack_provision_uses_review_detail_approval_projection(self) -> None:
        source = (
            Path(__file__).resolve().parents[1] / "rpgmaker_pack_provision.mjs"
        ).read_text()
        self.assertIn("if (!current.canApprove", source)
        self.assertIn("if (review.canApprove ||", source)
        self.assertNotIn('state !== "REVIEW_PENDING"', source)

    def test_pack_provision_preserves_a_title_wrapper_without_mutating_binding(self) -> None:
        source = PACK_PROVISION_PRODUCT_PATH.read_text()
        self.assertIn("directoryFiles(sourcePath, `${sourceName}/`)", source)
        self.assertIn("review.metadata?.title !== sourceName", source)
        self.assertNotIn("ensureReviewTitle", source)

    def test_pack_driver_initializes_role_contract_before_product_execution(self) -> None:
        source = PACK_BROWSER_PATH.read_text()
        self.assertLess(source.index("const reviewRoles ="), source.index("const browser ="))
        self.assertLess(source.index("const reviewRoles ="), source.index("validateReviewRole(role"))

    def test_compatibility_provision_uses_two_product_imports_and_real_save(self) -> None:
        source = COMPATIBILITY_PROVISION_PATH.read_text()
        self.assertIn('phase === "old" ? "rpg2000" : "rpg2000-compat"', source)
        self.assertIn("directoryFiles(`${fixtureRoot}/${fixture}`, `${fixture}/`)", source)
        self.assertIn('restorePage, "恢复后输入已经生效"', source)
        self.assertIn("createOldProductSave(context, client, published.gameId)", source)
        self.assertIn('expected: 201', source)
        self.assertNotIn("sqlite", source.lower())

    def test_all_formal_rpg_cases_have_a_driver_registration(self) -> None:
        self.assertEqual(
            {f"ACC-RPG-{number:03d}" for number in range(2, 9)},
            set(rpgmaker.GENERATION_CASES),
        )
        self.assertEqual(
            {"ACC-RPG-010", "ACC-RPG-011"},
            set(rpgmaker.SECURITY_CASES),
        )
        self.assertEqual({}, rpgmaker.DEFERRED_CASES)
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
        payload["validation"]["checkpointRoundTrip"]["restoredPosition"]["playerX"] = 99
        with self.assertRaisesRegex(rpgmaker.ContractError, "POSITION_ROUND_TRIP_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, digest)

    def test_complete_generation_evidence_is_valid_for_all_seven_cores(self) -> None:
        for spec in rpgmaker.GENERATION_CASES.values():
            with self.subTest(generation=spec.generation):
                rpgmaker.validate_generation_evidence(product_payload(spec, "a" * 64), spec, "a" * 64)

    def test_generation_evidence_binds_machine_position_gates_to_round_trip(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-006"]
        payload = product_payload(spec, "a" * 64)
        gate = next(
            item for item in payload["validation"]["machineGates"]
            if item["gate"] == "SAVE_POINT_RECORDED"
        )
        gate["evidence"]["playerX"] += 1
        with self.assertRaisesRegex(rpgmaker.ContractError, "GATE_POSITION_EVIDENCE_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_generation_evidence_binds_local_fixture_digest(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-007"]
        with self.assertRaisesRegex(rpgmaker.ContractError, "PROJECTFINGERPRINT_MISMATCH"):
            rpgmaker.validate_generation_evidence(product_payload(spec, "b" * 64), spec, "a" * 64)

    def test_generation_evidence_requires_fixture_variable_zero_one_two(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-005"]
        payload = product_payload(spec, "a" * 64)
        round_trip = payload["validation"]["checkpointRoundTrip"]
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

    def test_generation_evidence_requires_sanitized_upload_import_transcript(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-006"]
        payload = product_payload(spec, "a" * 64)
        payload["inputTranscript"]["upload"]["sourcePath"] = "/private/rpgvxace"
        with self.assertRaisesRegex(rpgmaker.ContractError, "INPUT_TRANSCRIPT_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_xp_generation_evidence_rejects_a_malformed_optional_270_mib_trace(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-004"]
        payload = product_payload(spec, "a" * 64)
        payload["xpRuntimeTrace"]["checkpointUpload"]["requestContentLengthBytes"] = 75 << 20
        with self.assertRaisesRegex(rpgmaker.ContractError, "XP_RUNTIME_TRACE_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_xp_minimal_product_round_trip_does_not_require_negative_trace_input(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-004"]
        payload = product_payload(spec, "a" * 64)
        payload.pop("xpRuntimeTrace")
        rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_mv_generation_evidence_requires_two_origin_inventory(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-007"]
        payload = product_payload(spec, "a" * 64)
        payload["originInventory"]["appOrigin"]["projectResourceResponses"] = 1
        with self.assertRaisesRegex(rpgmaker.ContractError, "ORIGIN_INVENTORY_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_mz_generation_evidence_requires_legal_lineage_engine_chrome_and_durations(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-008"]
        payload = product_payload(spec, "a" * 64)
        payload["inputProvenance"].pop("licenseUrl")
        payload["runtimeEnvironment"]["chromeVersion"] = ""
        payload["runtimeEnvironment"]["gateDurationsMs"] = []
        with self.assertRaisesRegex(rpgmaker.ContractError, "MZ_INPUT_PROVENANCE_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_mz_generation_evidence_rejects_missing_chrome_and_gate_durations(self) -> None:
        spec = rpgmaker.GENERATION_CASES["ACC-RPG-008"]
        payload = product_payload(spec, "a" * 64)
        payload["runtimeEnvironment"]["chromeVersion"] = ""
        payload["runtimeEnvironment"]["gateDurationsMs"] = []
        with self.assertRaisesRegex(rpgmaker.ContractError, "RUNTIME_ENVIRONMENT_INVALID"):
            rpgmaker.validate_generation_evidence(payload, spec, "a" * 64)

    def test_generation_browser_derives_transcript_and_origin_inventory_without_paths(self) -> None:
        source = BROWSER_PATH.read_text()
        self.assertIn('`/api/v1/admin/imports/${importJobId}`', source)
        self.assertIn('`/api/v1/admin/uploads/${imported.uploadId}`', source)
        self.assertIn("collectOriginInventory(page, config.adapter.uniqueOrigin", source)
        transcript_source = source[source.index("async function readInputTranscript"):]
        self.assertNotIn("relativePath:", transcript_source)
        self.assertNotIn("bootstrapTicket:", transcript_source)

    def test_png_visual_evidence_is_derived_from_bytes_and_rejects_black_frame(self) -> None:
        marker = "RETROM RPGVX"
        rgb = [168, 85, 247]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            visible = root / "visible.png"
            visible.write_bytes(test_png(320, 180, rgb))
            evidence = rpgmaker.png_visual_evidence(
                visible, "screenshots/visible.png", marker, rgb,
            )
            rpgmaker.validate_restore_visual(evidence, marker, rgb)
            self.assertEqual(hashlib.sha256(visible.read_bytes()).hexdigest(), evidence["sha256"])
            black = root / "black.png"
            black.write_bytes(test_png(320, 180, [0, 0, 0], solid=True))
            black_evidence = rpgmaker.png_visual_evidence(
                black, "screenshots/black.png", marker, rgb,
            )
            with self.assertRaisesRegex(rpgmaker.ContractError, "RESTORE_VISUAL_INVALID"):
                rpgmaker.validate_restore_visual(black_evidence, marker, rgb)

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
        xp = rpgmaker.required_environment("ACC-RPG-004")
        mz = rpgmaker.required_environment("ACC-RPG-008")
        self.assertIn("RETROM_CHROME_EXECUTABLE", xp)
        self.assertNotIn("RETROM_ACC_RPG_004_TRACE", xp)
        self.assertIn("RPG_MZ_SMOKE_PROVENANCE", mz)

    def test_missing_live_ids_are_blocked_and_machine_readable(self) -> None:
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(os.environ, {}, clear=True):
            case_dir = Path(directory)
            with redirect_stdout(io.StringIO()):
                self.assertEqual(3, rpgmaker.run("ACC-RPG-002", case_dir))
            result = json.loads((case_dir / "rpgmaker-product.json").read_text())
            self.assertEqual("BLOCKED", result["status"])
            self.assertIn("RETROM_ACC_RPG_002_IMPORT_ITEM_ID", result["missingInputs"])

    def test_compatibility_case_requires_fresh_database_state_instead_of_static_defer(self) -> None:
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(os.environ, {}, clear=True):
            case_dir = Path(directory)
            with redirect_stdout(io.StringIO()):
                self.assertEqual(3, rpgmaker.run("ACC-RPG-012", case_dir))
            result = json.loads((case_dir / "rpgmaker-product.json").read_text())
            self.assertEqual("BLOCKED", result["status"])
            self.assertEqual(
                ["RETROM_ACCEPTANCE_BASE_URL", "RETROM_ACCEPTANCE_USERNAME", "RETROM_ACCEPTANCE_PASSWORD",
                 "RETROM_ACC_RPG_012_DATABASE", "RETROM_ACC_RPG_012_STATE"],
                result["missingInputs"],
            )

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
                        "kind": identity[0], "generation": identity[1], "declaredName": identity[2],
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

    def test_pack_evidence_rejects_unreleased_consumption_and_fake_selection(self) -> None:
        payload = pack_evidence_payload()
        payload["databaseEvidence"]["uploads"]["zeroReference"]["consumptionReleasedAtMs"] = None
        with self.assertRaisesRegex(rpgmaker.ContractError, "CONSUMPTION_EVIDENCE_INVALID"):
            rpgmaker.validate_pack_evidence(payload)
        payload = pack_evidence_payload()
        payload["databaseEvidence"]["selectedReviews"][0]["installationId"] = pack_uuid(900)
        with self.assertRaisesRegex(rpgmaker.ContractError, "SELECTION_EVIDENCE_INVALID"):
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

    def test_compatibility_state_is_fresh_db_bound_and_inspected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            database = root / "retrom.db"
            state_path = root / "state.json"
            database.write_bytes(b"sqlite")
            state = compatibility_state_payload()
            state_path.write_text(json.dumps(state))
            with mock.patch.object(rpgmaker.subprocess, "run", return_value=mock.Mock(returncode=0)) as run:
                self.assertEqual(state, rpgmaker.compatibility_state(state_path, database))
            command = run.call_args.args[0]
            self.assertEqual("inspect", command[3])
            self.assertIn(str(database), command)
            state["oldArtifact"]["selectedForNewBindings"] = True
            state_path.write_text(json.dumps(state))
            with self.assertRaisesRegex(rpgmaker.ContractError, "ARTIFACT_STATE_INVALID"):
                rpgmaker.compatibility_state(state_path, database)

    def test_compatibility_state_rejects_a_repeated_old_fixture_identity(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            database = root / "retrom.db"
            state_path = root / "state.json"
            database.write_bytes(b"sqlite")
            state = compatibility_state_payload()
            state["newVariant"]["projectFingerprint"] = state["oldCheckpoint"]["projectFingerprint"]
            state_path.write_text(json.dumps(state))
            with self.assertRaisesRegex(rpgmaker.ContractError, "FIXTURE_IDENTITY_INVALID"):
                rpgmaker.compatibility_state(state_path, database)

    def test_compatibility_evidence_requires_exact_restore_and_four_rejections(self) -> None:
        state = compatibility_state_payload()
        payload = compatibility_evidence_payload(state)
        rpgmaker.validate_compatibility_evidence(payload, state)
        payload["oldRestore"]["screenshotRoundTripExact"] = False
        with self.assertRaisesRegex(rpgmaker.ContractError, "OLD_RESTORE_INVALID"):
            rpgmaker.validate_compatibility_evidence(payload, state)

    def test_content_security_evidence_requires_42_combinations_and_family_only_route(self) -> None:
        wrong = [
            {
                "sourceGeneration": "RPG2000" if index == 0 else "RPGMV",
                "selectedCoreId": "rpgmaker_2003" if index == 0 else "rpgmaker_mz",
                "accepted": index == 0, "status": 202 if index == 0 else 422,
                "code": None if index == 0 else "RPG_SELECTED_CORE_MISMATCH",
            }
            for index in range(42)
        ]
        payload = {
            "schemaVersion": 1, "caseId": "ACC-RPG-010", "status": "PASS",
            "wrongCore": wrong, "unsafe": [{} for _ in range(13)],
            "nestedArchives": [
                {"sha256": "c" * 64, "filesDigest": "d" * 64, "nestedEntryCount": 0}
                for _ in range(70)
            ],
            "familyOnly": {
                "selectedCoreId": "rpgmaker_2003", "evidenceConfidence": "FAMILY_ONLY",
                "config": {"routeKey": "RPG2003_EASYRPG_0811_V4"},
            },
            "opaqueNative": {
                "filesDigest": "a" * 64,
                "sourceFiles": [
                    {"name": name, "sha256": "b" * 64, "sizeBytes": 1}
                    for name in ("Game.exe", "nw.dll", "plugin.node", "launcher.bat")
                ],
                "runtimeProjection": [
                    {"name": name, "status": 404}
                    for name in ("Game.exe", "nw.dll", "plugin.node", "launcher.bat")
                ],
            },
        }
        rpgmaker.validate_security_evidence(payload, "ACC-RPG-010")
        payload["wrongCore"].pop()
        with self.assertRaisesRegex(rpgmaker.ContractError, "WRONG_CORE_MATRIX_INCOMPLETE"):
            rpgmaker.validate_security_evidence(payload, "ACC-RPG-010")

    def test_isolation_evidence_requires_bootstrap_and_cross_launch_checkpoint(self) -> None:
        harnesses = []
        for index, generation in enumerate(("RPGMV", "RPGMZ"), start=1):
            original = f"{index}1111111-1111-4111-8111-111111111111"
            restore = f"{index}2222222-2222-4222-8222-222222222222"
            harnesses.append({
                "generation": generation, "originalLaunchId": original, "restoreLaunchId": restore,
                "bootstrap": {
                    "authenticatedReloadStatus": 303, "replayStatus": 410,
                    "appHostEntryStatus": 404, "runtimeApiStatus": 404,
                    "confusedHostStatus": 404, "inactiveBootstrapStatus": 410,
                },
                "machineGates": [gate_evidence(gate, generation) for gate in rpgmaker.GATES],
                "checkpointRoundTrip": checkpoint_payload(original, restore),
            })
        payload = {"schemaVersion": 1, "caseId": "ACC-RPG-011", "status": "PASS", "harnesses": harnesses}
        rpgmaker.validate_security_evidence(payload, "ACC-RPG-011")
        payload["harnesses"][0]["bootstrap"]["replayStatus"] = 204
        with self.assertRaisesRegex(rpgmaker.ContractError, "ISOLATION_BOOTSTRAP_INVALID"):
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
            "jobId": validation["jobId"], "kind": rpgmaker.PACK_UPLOAD_ROLES[role][0],
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
            "validationId": pack_uuid(140 + index), "generation": "RPGXP", "status": 201,
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
            "publish": {"status": 409, "code": "REVIEW_VALIDATION_STALE"},
        }
        outcomes.append(item)
        selected.append({key: item[key] for key in ("role", "itemId", "installationId")})
    for index, (role, upload_role) in enumerate((
        ("rpgxpStandardAmbiguous", "rgss1StandardV1"),
        ("rpgvxStandardAmbiguous", "rgss2StandardV1"),
        ("rpgvxaceStandardAmbiguous", "rgss3StandardV1"),
    )):
        item = {
            "role": role, "itemId": pack_uuid(160 + index), "matcher": "AMBIGUOUS", "patchStatus": 200,
            "installationId": installations[upload_role]["installationId"],
            "rejectionStatus": 422, "rejectionCode": "REVIEW_DRAFT_INVALID",
            "publish": {"status": 409, "code": "REVIEW_VALIDATION_STALE"},
        }
        outcomes.append(item)
        selected.append({key: item[key] for key in ("role", "itemId", "installationId")})
    protected_references = {
        "publishedVariant": {"installationId": pack_uuid(180), "gameId": pack_uuid(181)},
        "restorableCheckpoint": {
            "installationId": pack_uuid(182), "gameId": pack_uuid(183), "saveStateId": pack_uuid(184),
        },
    }
    protected_database = {
        "publishedVariant": {
            **protected_references["publishedVariant"], "definitionId": "rgss1_standard",
            "availableForLaunch": True,
        },
        "restorableCheckpoint": {
            **protected_references["restorableCheckpoint"], "definitionId": "rgss2_rpgvx",
            "availableForLaunch": True,
        },
    }
    zero_upload = database_uploads["zeroReference"]
    release_job = pack_job(190, "PAYLOAD_RELEASE", True)
    return {
        "schemaVersion": 1, "caseId": "ACC-RPG-009", "status": "PASS",
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
    artifact = "44444444-4444-4444-8444-444444444444"
    position_a = {"mapId": 1, "playerX": 1, "playerY": 1, "fixtureState": 0}
    position_b = {"mapId": 1, "playerX": 2, "playerY": 1, "fixtureState": 1}
    position_c = {"mapId": 1, "playerX": 3, "playerY": 1, "fixtureState": 2}
    position_after = {"mapId": 1, "playerX": 4, "playerY": 1, "fixtureState": 1}
    machine_gates = [
        gate_evidence(gate, spec.generation, index) for index, gate in enumerate(rpgmaker.GATES)
    ]
    position_gates = {
        "INITIAL_POSITION_RECORDED": position_a,
        "SAVE_POINT_RECORDED": position_b,
        "POST_SAVE_STATE_DIVERGED": position_c,
        "RESTORE_POSITION_VERIFIED": position_b,
        "RESTORE_INPUT": position_after,
    }
    for gate in machine_gates:
        if gate["gate"] in position_gates:
            gate["evidence"] = dict(position_gates[gate["gate"]])
    marker = {
        "RPG2000": ("RETROM RPG2000", [45, 180, 138]),
        "RPG2003": ("RETROM RPG2003", [245, 158, 11]),
        "RPGXP": ("RETROM RPGXP", [59, 130, 246]),
        "RPGVX": ("RETROM RPGVX", [168, 85, 247]),
        "RPGVXACE": ("RETROM RPGVXACE", [244, 63, 94]),
        "RPGMV": ("RETROM RPGMV", [64, 208, 255]),
        "RPGMZ": ("RETROM RPGMZ", [34, 197, 94]),
    }[spec.generation]
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
                "runtimeValidationCurrent": True,
            },
        },
        "validation": {
            "importItemId": "55555555-5555-4555-8555-555555555555",
            "launchId": original, "restoreLaunchId": restore, "state": "PASSED",
            "decision": {"decision": "PASS"},
            "routeEvidence": {
                "coreId": spec.core_id, "generation": spec.generation,
                "evidenceGeneration": spec.evidence_generation, "evidenceConfidence": spec.confidence,
                "routeKey": spec.route_key, "projectFingerprint": digest, "artifactId": artifact,
                "artifactSetSha256": "c" * 64, "adapterId": "adapter",
                "adapterAbi": "abi", "dependencySnapshotSha256": "d" * 64,
            },
            "machineGates": machine_gates,
            "checkpointRoundTrip": {
                "created": True, "originalLaunchEnded": True, "restoreStarted": True,
                "positionVerified": True, "restoreInputVerified": True,
                "originalLaunchId": original, "restoreLaunchId": restore,
                "initialPosition": position_a, "savedPosition": position_b,
                "divergedPosition": position_c, "restoredPosition": dict(position_b),
                "restoreInputPosition": position_after, "sha256": "e" * 64,
                "screenshotUrl": "/api/v1/admin/review-assets/screenshot",
                "payloadKind": "RUNTIME_STATE" if spec.generation not in {"RPGMV", "RPGMZ"}
                else "NATIVE_SAVE_BUNDLE_V1",
                "sizeBytes": 268_435_456 if spec.generation == "RPGXP" else 4_096,
            },
        },
        "productLaunch": {
            "launchId": product, "playerRunning": True,
            "config": {
                "runtimeFamily": "RPGMAKER", "purpose": "PRODUCT", "coreId": spec.core_id,
                "generation": spec.generation, "routeKey": spec.route_key, "artifactId": artifact,
                "adapterId": "adapter",
            },
        },
        "inputTranscript": {
            "transportScheme": "HTTPS",
            "upload": {
                "uploadId": upload_id, "state": "COMPLETE", "purpose": "RPG_MAKER_PROJECT",
                "sourceType": "DIRECTORY", "fileCount": 10, "totalBytes": 1024,
                "receivedBytes": 1024, "finalizationNo": 1,
            },
            "import": {
                "importJobId": import_id, "uploadId": upload_id, "state": "COMPLETED",
                "payloadState": "RELEASED", "platformId": "rpgmaker",
                "defaultCoreId": spec.core_id, "coreArtifactId": artifact,
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
            "gateDurationsMs": [
                {"gate": gate, "durationMs": 10} for gate in rpgmaker.GATES
            ],
        },
        "screenshots": [restore_screenshot, f"screenshots/{spec.core_id}-product-player.png"],
    }
    if spec.generation in {"RPGMV", "RPGMZ"}:
        payload["productLaunch"]["config"].update({
            "bridgeProfile": rpgmaker.ENGINE_PROFILES[spec.generation],
        })
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
        })
    if spec.generation == "RPGXP":
        payload["productLaunch"]["config"].update({
            "adapterKind": "MKXP_LIBRETRO_WEB", "stateBufferBytes": 268_435_456,
        })
        payload["xpRuntimeTrace"] = xp_runtime_trace("e" * 64)
    return payload


def compatibility_state_payload() -> dict:
    identifiers = [f"{number:08d}-1111-4111-8111-111111111111" for number in range(1, 10)]
    artifact = lambda identifier, route, digest, selected: {
        "id": identifier, "coreId": "rpgmaker_2000", "generation": "RPG2000", "routeKey": route,
        "artifactSetSha256": digest * 64, "adapterId": "easyrpg-web-v1",
        "adapterAbi": "easyrpg-save-v1", "manifestSha256": "f" * 64,
        "selectedForNewBindings": selected, "availableForLaunch": True,
    }
    fixture_root = Path(__file__).resolve().parents[3] / "testdata/public-roms/rpgmaker-smoke"
    old_fingerprint = rpgmaker.project_digest(fixture_root / "rpg2000")[0]
    new_fingerprint = rpgmaker.project_digest(fixture_root / "rpg2000-compat")[0]
    return {
        "schemaVersion": 1, "caseId": "ACC-RPG-012", "phase": "DRIFT_SEEDED",
        "databasePathSha256": "d" * 64,
        "oldArtifact": artifact(identifiers[0], "RPG2000_EASYRPG_0811_V4", "a", False),
        "newArtifact": artifact(identifiers[1], "RPG2000_EASYRPG_0811_V5", "b", True),
        "oldCheckpoint": {
            "gameId": identifiers[2], "saveStateId": identifiers[3], "contentRevisionId": identifiers[4],
            "variantRevisionId": identifiers[5], "artifactId": identifiers[0],
            "routeKey": "RPG2000_EASYRPG_0811_V4", "adapterAbi": "easyrpg-save-v1",
            "projectFingerprint": old_fingerprint,
            "dependencySnapshotSha256": "c" * 64, "runtimePacks": [],
        },
        "newVariant": {
            "gameId": identifiers[6], "contentRevisionId": identifiers[7], "variantRevisionId": identifiers[8],
            "artifactId": identifiers[1], "routeKey": "RPG2000_EASYRPG_0811_V5",
            "adapterAbi": "easyrpg-save-v1", "dependencySnapshotSha256": "e" * 64,
            "projectFingerprint": new_fingerprint,
            "runtimePacks": [],
        },
        "driftSaveStateIds": {
            "content": "10000000-1111-4111-8111-111111111111",
            "artifact": "11000000-1111-4111-8111-111111111111",
            "pack": "12000000-1111-4111-8111-111111111111",
            "adapterAbi": "13000000-1111-4111-8111-111111111111",
        },
        "updatedAtMs": 1,
    }


def compatibility_evidence_payload(state: dict) -> dict:
    safe_artifact = lambda value: {
        "id": value["id"], "routeKey": value["routeKey"],
        "selectedForNewBindings": value["selectedForNewBindings"], "availableForLaunch": True,
    }
    return {
        "schemaVersion": 1, "caseId": "ACC-RPG-012", "status": "PASS",
        "artifacts": {
            "old": safe_artifact(state["oldArtifact"]), "new": safe_artifact(state["newArtifact"]),
        },
        "oldRestore": {
            "artifactId": state["oldArtifact"]["id"], "routeKey": state["oldArtifact"]["routeKey"],
            "launchId": "14000000-1111-4111-8111-111111111111",
            "replaySaveStateId": "15000000-1111-4111-8111-111111111111",
            "playerRunning": True, "screenshotRoundTripExact": True,
            "originalScreenshotSha256": "9" * 64, "replayScreenshotSha256": "9" * 64,
        },
        "newLaunch": {
            "artifactId": state["newArtifact"]["id"], "routeKey": state["newArtifact"]["routeKey"],
            "launchId": "16000000-1111-4111-8111-111111111111", "playerRunning": True,
        },
        "driftRejections": [
            {"kind": kind, "saveStateId": state["driftSaveStateIds"][kind], "status": 422,
             "code": "LAUNCH_BLOCKED", "launchCreated": False}
            for kind in ("content", "artifact", "pack", "adapterAbi")
        ],
        "bindings": {"oldCheckpoint": state["oldCheckpoint"], "newVariant": state["newVariant"]},
        "screenshots": [
            "screenshots/old-save.png", "screenshots/restored-save.png",
            "screenshots/old-player.png", "screenshots/new-player.png",
        ],
    }


def checkpoint_payload(original: str, restore: str) -> dict:
    position_a = {"mapId": 1, "playerX": 1, "playerY": 1, "fixtureState": 0}
    position_b = {"mapId": 1, "playerX": 2, "playerY": 1, "fixtureState": 1}
    return {
        "created": True, "originalLaunchEnded": True, "restoreStarted": True,
        "positionVerified": True, "restoreInputVerified": True,
        "originalLaunchId": original, "restoreLaunchId": restore,
        "initialPosition": position_a, "savedPosition": position_b,
        "divergedPosition": {"mapId": 1, "playerX": 3, "playerY": 1, "fixtureState": 2},
        "restoredPosition": dict(position_b),
        "restoreInputPosition": {"mapId": 1, "playerX": 4, "playerY": 1, "fixtureState": 1},
        "sha256": "e" * 64, "screenshotUrl": "/api/v1/admin/review-assets/screenshot",
    }
def gate_evidence(gate: str, generation: str, index: int = 0) -> dict:
    evidence = {}
    if gate == "ENGINE_PROFILE":
        evidence = {"generation": generation, "adapterId": "adapter", "engineProfile": rpgmaker.ENGINE_PROFILES[generation]}
    elif gate == "FRAMES_300":
        evidence = {"continuousFrames": 300}
    begun = 1_000 + index * 20
    return {
        "gate": gate, "status": "PASSED", "begunAtMs": begun,
        "completedAtMs": begun + 10, "evidence": evidence, "failureCode": None,
    }


def xp_runtime_trace(checkpoint_sha256: str) -> dict:
    capabilities = {
        "secureContext": False, "crossOriginIsolated": False, "sharedArrayBuffer": False,
    }
    return {
        "schemaVersion": 1,
        "checkpointUpload": {
            "requestPayloadBytes": 268_435_456,
            "requestContentLengthBytes": 268_436_128,
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
                "phase": "VALIDATION", "capabilities": capabilities,
                "responseStatus": 409, "errorCode": "RPG_RUNTIME_ROUTE_UNAVAILABLE",
                "launchCredentialIssued": False, "projectPayloadRequestCount": 0,
            },
            {
                "attemptId": "99999999-9999-4999-8999-999999999999",
                "phase": "RESTORE", "capabilities": capabilities,
                "responseStatus": 409, "errorCode": "RPG_RUNTIME_ROUTE_UNAVAILABLE",
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


def png_chunk(kind: bytes, contents: bytes) -> bytes:
    return struct.pack(">I", len(contents)) + kind + contents + \
        struct.pack(">I", zlib.crc32(kind + contents) & 0xFFFFFFFF)


if __name__ == "__main__":
    unittest.main()
