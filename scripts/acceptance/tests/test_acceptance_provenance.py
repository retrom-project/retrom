import hashlib
import importlib.util
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
RUNNER_PATH = ROOT / "scripts" / "acceptance" / "run.py"
SPEC = importlib.util.spec_from_file_location("acceptance_runner", RUNNER_PATH)
assert SPEC and SPEC.loader
runner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runner)


class AcceptanceProvenanceTests(unittest.TestCase):
    def test_git_provenance_contains_a_relative_dirty_file_summary(self) -> None:
        provenance = runner.git_provenance()
        self.assertRegex(provenance["gitCommit"], r"^(?:[0-9a-f]{40}|UNBORN)$")
        summary = provenance["gitDirtySummary"]
        self.assertEqual(provenance["gitDirty"], summary["fileCount"] > 0)
        self.assertRegex(summary["sha256"], r"^[0-9a-f]{64}$")
        self.assertEqual(summary["fileCount"], len(summary["entries"]))
        for entry in summary["entries"]:
            self.assertEqual({"status", "path"}, set(entry))
            self.assertFalse(Path(entry["path"]).is_absolute())

    def test_archived_failure_is_registered_and_requires_an_explanation(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            run_dir = Path(directory) / "20260827T000000Z-01234567"
            case_dir = run_dir / "cases" / "acc-rpg-009"
            case_dir.mkdir(parents=True)
            (run_dir / "defects.json").write_text("[]\n", encoding="utf-8")
            (case_dir / "stdout.log").write_text("first failure\n", encoding="utf-8")
            (case_dir / "result.json").write_text(json.dumps({
                "caseId": "ACC-RPG-009", "status": "FAIL", "finishedAtMs": 123,
                "assertions": [{"details": "pack relation mismatch"}],
            }), encoding="utf-8")

            runner.archive_previous(case_dir)

            defects = json.loads((run_dir / "defects.json").read_text(encoding="utf-8"))
            self.assertEqual(1, len(defects))
            defect = defects[0]
            self.assertEqual("OPEN", defect["status"])
            self.assertEqual("ACC-RPG-009", defect["caseId"])
            self.assertEqual(
                "cases/acc-rpg-009/attempts/001/result.json", defect["failedResult"],
            )
            self.assertEqual([defect["defectId"]], runner.unresolved_defect_ids(run_dir, "ACC-RPG-009"))

    def test_archived_result_must_match_its_case_directory(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            run_dir = Path(directory) / "20260827T000000Z-01234567"
            case_dir = run_dir / "cases" / "acc-rpg-012"
            attempt = case_dir / "attempts" / "001"
            attempt.mkdir(parents=True)
            (run_dir / "defects.json").write_text("[]\n", encoding="utf-8")
            (attempt / "result.json").write_text(json.dumps({
                "caseId": "ACC-RPG-011", "status": "FAIL",
                "assertions": [{"details": "wrong Case identity"}],
            }), encoding="utf-8")

            with self.assertRaisesRegex(RuntimeError, "ACCEPTANCE_DEFECT_RESULT_CASE_INVALID"):
                runner.synchronize_failure_defects(case_dir)

            self.assertEqual([], json.loads((run_dir / "defects.json").read_text(encoding="utf-8")))

    def test_blocked_preflight_is_not_registered_as_a_product_defect(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            run_dir = Path(directory) / "20260827T000000Z-01234567"
            case_dir = run_dir / "cases" / "acc-rpg-011"
            attempt = case_dir / "attempts" / "001"
            attempt.mkdir(parents=True)
            (run_dir / "defects.json").write_text("[]\n", encoding="utf-8")
            (attempt / "result.json").write_text(json.dumps({
                "caseId": "ACC-RPG-011", "status": "BLOCKED",
                "assertions": [{"details": "Chrome input missing"}],
            }), encoding="utf-8")

            runner.synchronize_failure_defects(case_dir)

            self.assertEqual([], json.loads((run_dir / "defects.json").read_text(encoding="utf-8")))

    def test_resolution_is_fail_closed_and_links_red_and_green_results(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            run_dir = Path(directory) / "20260827T000000Z-01234567"
            case_dir = run_dir / "cases" / "acc-rpg-012"
            red_dir = case_dir / "attempts" / "001"
            red_dir.mkdir(parents=True)
            red_result = "cases/acc-rpg-012/attempts/001/result.json"
            (red_dir / "result.json").write_text(json.dumps({
                "caseId": "ACC-RPG-012", "status": "FAIL",
                "assertions": [{"details": "restore mismatch"}],
            }), encoding="utf-8")
            defect_id = "acc-rpg-012-attempt-001"
            (run_dir / "defects.json").write_text(json.dumps([{
                "schemaVersion": 1, "defectId": defect_id, "caseId": "ACC-RPG-012",
                "status": "OPEN", "failedResult": red_result, "failureReason": "restore mismatch",
            }]), encoding="utf-8")
            regression = ROOT / "scripts" / "acceptance" / "tests" / "test_acceptance_provenance.py"
            commit = subprocess.run(
                ["git", "rev-parse", "HEAD"], cwd=ROOT, check=True, text=True, capture_output=True,
            ).stdout.strip()
            resolution = Path(directory) / "resolution.json"
            resolution.write_text(json.dumps({
                "schemaVersion": 1, "caseId": "ACC-RPG-012",
                "rerunExplanation": "The restore provenance assertion now rejects missing phase output.",
                "defects": [{
                    "defectId": defect_id,
                    "rootCause": "The final bundle did not bind the setup phases.",
                    "regressionTest": f"{regression.relative_to(ROOT)}::AcceptanceProvenanceTests",
                    "redEvidence": red_result,
                    "greenCommand": "printf 'green\\n'",
                    "fixCommit": commit,
                }],
            }), encoding="utf-8")

            normalized = runner.load_defect_resolution(run_dir, "ACC-RPG-012", resolution)
            normalized, succeeded = runner.run_defect_regressions(normalized, case_dir, run_dir)
            self.assertTrue(succeeded)
            green_result = "cases/acc-rpg-012/result.json"
            runner.close_resolved_defects(run_dir, normalized, green_result)

            defect = json.loads((run_dir / "defects.json").read_text(encoding="utf-8"))[0]
            self.assertEqual("FIXED", defect["status"])
            self.assertEqual(red_result, defect["redEvidence"])
            self.assertEqual(green_result, defect["successfulResult"])
            self.assertEqual(0, defect["greenExitCode"])
            self.assertTrue((run_dir / defect["greenEvidence"]).is_file())
            self.assertTrue(defect["rerunExplanation"])

            (case_dir / "result.json").write_text(json.dumps({
                "caseId": "ACC-RPG-012", "status": "PASS",
                "evidence": [defect["greenEvidence"]],
            }), encoding="utf-8")
            (case_dir / "rerun-resolution.json").write_text(json.dumps(normalized), encoding="utf-8")
            runner.archive_previous(case_dir)
            defect = json.loads((run_dir / "defects.json").read_text(encoding="utf-8"))[0]
            self.assertEqual("cases/acc-rpg-012/attempts/002/result.json", defect["successfulResult"])
            self.assertTrue(defect["greenEvidence"].startswith("cases/acc-rpg-012/attempts/002/"))
            self.assertTrue((run_dir / defect["greenEvidence"]).is_file())


class RPGDedicatedProvenanceContractTests(unittest.TestCase):
    def test_pack_provisioning_identity_is_required_by_the_final_inspector(self) -> None:
        provisioner = (ROOT / "scripts/acceptance/rpgmaker_pack_provision.mjs").read_text()
        inspector = (ROOT / "scripts/acceptance/rpgmaker_pack_inspect.py").read_text()
        self.assertIn('"--evidence"', provisioner)
        self.assertIn("writeProvisionEvidence", provisioner)
        self.assertIn("RETROM_ACC_RPG_009_PROVISION_EVIDENCE", inspector)
        self.assertIn('"provisioningEvidence"', inspector)

    def test_compatibility_final_bundle_requires_every_setup_phase_output(self) -> None:
        driver = (ROOT / "scripts/acceptance/rpgmaker_compatibility.mjs").read_text()
        provenance = (ROOT / "scripts/acceptance/rpgmaker_compatibility_provenance.mjs").read_text()
        for name in (
            "RETROM_ACC_RPG_012_PREPARE_EVIDENCE",
            "RETROM_ACC_RPG_012_OLD_PROVISION_EVIDENCE",
            "RETROM_ACC_RPG_012_PROMOTE_EVIDENCE",
            "RETROM_ACC_RPG_012_NEW_PROVISION_EVIDENCE",
            "RETROM_ACC_RPG_012_DRIFT_EVIDENCE",
            "RETROM_ACC_RPG_012_INSPECT_EVIDENCE",
        ):
            self.assertIn(name, provenance)
        self.assertIn("provisioningEvidence", driver)

    def test_compatibility_phase_loader_binds_all_six_documents(self) -> None:
        identifier = lambda number: f"{number:08d}-1111-4111-8111-111111111111"
        dirty_entries = [{"status": " M", "path": "scripts/acceptance/rpgmaker_case.py"}]
        repository = {
            "gitCommit": "1" * 40, "gitDirty": True,
            "gitDirtySummary": {
                "fileCount": len(dirty_entries),
                "sha256": hashlib.sha256(json.dumps(
                    dirty_entries, ensure_ascii=False, separators=(",", ":"),
                ).encode()).hexdigest(),
                "entries": dirty_entries,
            },
        }
        old_artifact = {"id": identifier(1), "routeKey": "OLD"}
        new_artifact = {"id": identifier(2), "routeKey": "NEW"}
        checkpoint = {"gameId": identifier(3), "saveStateId": identifier(4)}
        variant = {"gameId": identifier(5)}
        state = lambda phase, old, new, drifts: {
            "schemaVersion": 1, "caseId": "ACC-RPG-012", "phase": phase,
            "databasePathSha256": "3" * 64, "oldArtifact": old_artifact,
            "newArtifact": new_artifact, "oldCheckpoint": old, "newVariant": new,
            "driftSaveStateIds": drifts, "updatedAtMs": 1,
        }
        prepare = state("OLD_SELECTED", None, None, None)
        promote = state("NEW_SELECTED", checkpoint, None, None)
        final = state("DRIFT_SEEDED", checkpoint, variant, {
            "content": identifier(6), "artifact": identifier(7),
            "pack": identifier(8), "adapterAbi": identifier(9),
        })
        old_product = {
            "schemaVersion": 1, "caseId": "ACC-RPG-012", "phase": "OLD",
            "importItemId": identifier(10), "validationId": identifier(11),
            "routeKey": "OLD", "gameId": checkpoint["gameId"],
            "saveStateId": checkpoint["saveStateId"], "repository": repository,
        }
        new_product = {
            "schemaVersion": 1, "caseId": "ACC-RPG-012", "phase": "NEW",
            "importItemId": identifier(12), "validationId": identifier(13),
            "routeKey": "NEW", "gameId": variant["gameId"], "repository": repository,
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            documents = {
                "PREPARE": prepare, "OLD_PROVISION": old_product, "PROMOTE": promote,
                "NEW_PROVISION": new_product, "DRIFT": final, "INSPECT": final,
            }
            environment = os.environ.copy()
            for label, payload in documents.items():
                path = root / f"{label.lower()}.json"
                path.write_text(json.dumps(payload), encoding="utf-8")
                environment[f"RETROM_ACC_RPG_012_{label}_EVIDENCE"] = str(path)
            module = "./scripts/acceptance/rpgmaker_compatibility_provenance.mjs"
            script = (
                f'import {{loadCompatibilityProvisioning}} from {json.dumps(module)};'
                f'process.stdout.write(JSON.stringify(loadCompatibilityProvisioning({json.dumps(final)})));'
            )
            completed = subprocess.run(
                ["node", "--input-type=module", "-e", script], cwd=ROOT, env=environment,
                check=False, text=True, capture_output=True,
            )
            self.assertEqual(0, completed.returncode, completed.stderr)
            payload = json.loads(completed.stdout)
            self.assertEqual(set(documents), {key.replace("Provision", "_PROVISION").upper()
                for key in payload["phases"]})


if __name__ == "__main__":
    unittest.main()
