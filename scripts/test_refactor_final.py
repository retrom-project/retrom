#!/usr/bin/env python3
"""Final coordination must not manufacture execution or reuse old evidence."""

from __future__ import annotations

import fcntl
import hashlib
import json
import os
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import patch

import refactor_final as final
import refactor_verify as verify


class FinalCoordinatorTests(unittest.TestCase):
    def setUp(self) -> None:
        self.directory = tempfile.TemporaryDirectory(prefix="retrom-refactor-final-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        (self.root / ".gitignore").write_text(".artifacts/\n", encoding="utf-8")
        (self.root / "source.go").write_text("package fixture\n", encoding="utf-8")
        docs = self.root / "docs"
        docs.mkdir()
        (docs / "project-acceptance.md").write_text(
            "### ACC-QA-001：quality\n### ACC-NET-002：conditional\n### ACC-MZ-001：legal input\n",
            encoding="utf-8",
        )
        self.cases = [self.case(point) for point in final.POINTS]
        registry = self.root / "quality/architecture/test-cases.json"
        registry.parent.mkdir(parents=True)
        registry.write_text(json.dumps({"schemaVersion": 1, "baseline": verify.BASELINE, "cases": self.cases}), encoding="utf-8")
        for arguments in (
            ["init", "-q"], ["config", "user.name", "Refactor fixture"],
            ["config", "user.email", "fixture@example.invalid"], ["config", "commit.gpgsign", "false"],
            ["add", "."], ["commit", "-qm", "fixture"],
        ):
            subprocess.run(["git", *arguments], cwd=self.root, check=True, capture_output=True)
        self.snapshot = verify.source_fingerprint(self.root)

    @staticmethod
    def case(point: str) -> dict:
        case = {
            "id": "RFA-" + point + "-fixture", "point_id": point, "tier": "tool",
            "target_file": "internal/example/example_test.go", "symbol": "TestPass",
            "setup_and_action": "Run isolated fixture", "required_assertions": "Named execution",
            "hard_timeout_seconds": 10, "required": True,
        }
        case["runner"] = {"type": "go-test", "command": verify.go_test_command(case), "must_execute": "TestPass"}
        return case

    def child(self, identifier: str, code: str, kind: str = "command") -> dict:
        return final.step(identifier, [sys.executable, "-c", code], 5, kind)

    def emit(self, identifier: str, kind: str, relative: str, payload: dict, exit_code: int = 0) -> dict:
        code = (
            "import pathlib,json; p=pathlib.Path(" + repr(relative) + ");"
            "p.parent.mkdir(parents=True,exist_ok=True);p.write_text(" + repr(json.dumps(payload)) + ");"
            "print('private-cookie=/private/sample');raise SystemExit(" + str(exit_code) + ")"
        )
        return self.child(identifier, code, kind)

    def execute(self, item: dict, context: dict | None = None) -> dict:
        return final.execute_step(self.root, item, self.snapshot, self.cases, context if context is not None else {})

    def point_payload(self) -> dict:
        return {
            **self.snapshot, "point": "RF01", "status": "PASS", "pending": [],
            "cases": [{
                "caseId": "RFA-RF01-fixture", "status": "PASS", "exitCode": 0, "timedOut": False,
                "events": [
                    {"Action": "run", "Package": "fixture", "Test": "TestPass"},
                    {"Action": "pass", "Package": "fixture", "Test": "TestPass", "Elapsed": 0.01},
                ],
            }],
        }

    def test_fixed_graph_has_all_points_before_original_acceptance(self) -> None:
        plan = final.final_plan(self.root)
        ids = [item["id"] for item in plan]
        self.assertEqual(ids[:7], ["preparation", "checker-selftest", "go-architecture", "web-architecture", "contracts", "ci", "race"])
        self.assertEqual(ids[7:29], list(final.POINTS))
        self.assertLess(ids.index("web-e2e"), ids.index("ACC-QA-001"))
        self.assertEqual(ids[-1], "acceptance-report")
        self.assertEqual(len(ids), len(set(ids)))
        self.assertTrue(all("refactor-final" not in item["command"] for item in plan))
        self.assertEqual({item["command"][1] for item in plan if item["kind"] == "point"}, {"refactor-verify"})

    def test_missing_work_item_and_duplicate_product_case_reject_plan(self) -> None:
        path = self.root / "quality/architecture/test-cases.json"
        payload = json.loads(path.read_text())
        payload["cases"].pop()
        path.write_text(json.dumps(payload))
        with self.assertRaisesRegex(verify.AnalysisError, "every work item"):
            final.final_plan(self.root)
        with (self.root / "docs/project-acceptance.md").open("a") as output:
            output.write("### ACC-QA-001: duplicate\n")
        with self.assertRaisesRegex(verify.AnalysisError, "duplicate"):
            final.acceptance_catalog(self.root)

    def test_rfa_aliases_never_reenter_original_product_loop(self) -> None:
        with (self.root / "docs/project-acceptance.md").open("a") as output:
            output.writelines(f"### ACC-RFA-{number:03d}: point\n" for number in range(1, 23))
        self.assertEqual(final.acceptance_catalog(self.root), ["ACC-QA-001", "ACC-NET-002", "ACC-MZ-001"])

    def test_dirty_candidate_rejected_before_preparation(self) -> None:
        with patch.object(verify, "BASELINE", self.snapshot["commit"]):
            final.require_clean(self.root)
            (self.root / "untracked.txt").write_text("uncommitted")
            with self.assertRaisesRegex(verify.AnalysisError, "clean"):
                final.require_clean(self.root)

    def test_real_child_exit_controls_result_and_output_is_private(self) -> None:
        for code, expected in [(0, "PASS"), (1, "FAIL")]:
            script = self.root / ".artifacts/private-output-fixture.py"
            script.parent.mkdir(parents=True, exist_ok=True)
            script.write_text(f"print('private-cookie=/private/sample');raise SystemExit({code})")
            item = final.step("fixture", [sys.executable, ".artifacts/private-output-fixture.py"], 5)
            result = self.execute(item)
            self.assertEqual(result["status"], expected)
            self.assertEqual(result["exitCode"], code)
            self.assertNotIn("private-cookie", json.dumps(result))
            self.assertNotIn("/private/sample", json.dumps(result))
            self.assertRegex(result["outputSha256"], r"^[0-9a-f]{64}$")

    def test_timeout_is_failure_even_without_output(self) -> None:
        item = self.child("fixture", "import time;time.sleep(10)")
        item["timeoutSeconds"] = 0.05
        result = self.execute(item)
        self.assertEqual(result["status"], "FAIL")
        self.assertTrue(result["timedOut"])

    def test_missing_or_stale_artifact_cannot_use_zero_exit(self) -> None:
        item = self.child("contracts", "pass", "contracts")
        self.assertEqual(self.execute(item)["status"], "ANALYSIS_ERROR")
        path = self.root / ".artifacts/refactor/contract.json"
        path.parent.mkdir(parents=True)
        path.write_text("{}")
        os.utime(path, ns=(1, 1))
        self.assertEqual(self.execute(item)["status"], "ANALYSIS_ERROR")

    def test_static_gate_rejects_wrong_head_or_pending_green_report(self) -> None:
        for mutation in (
            {"baseline": {"commit": "0" * 40, "tree": self.snapshot["tree"]}},
            {"pendingChecks": ["missing check"]}, {"violations": [{"rule": "AR04"}]},
        ):
            payload = {
                "status": "VERIFIED", "baseline": self.snapshot,
                "sourceSha256": self.snapshot["sourceSha256"], "pendingChecks": [], "violations": [], **mutation,
            }
            result = self.execute(self.emit("contracts", "contracts", ".artifacts/refactor/contract.json", payload))
            self.assertEqual(result["status"], "ANALYSIS_ERROR", mutation)

    def test_point_must_contain_actual_complete_named_execution(self) -> None:
        path = ".artifacts/refactor/points/RF01/result.json"
        result = self.execute(self.emit("RF01", "point", path, self.point_payload()))
        self.assertEqual(result["status"], "PASS")
        self.assertEqual(result["artifacts"][0]["sha256"], hashlib.sha256((self.root / path).read_bytes()).hexdigest())
        for change in ("missing", "duplicate", "zero-match", "skip", "pending", "wrong-source"):
            payload = self.point_payload()
            if change == "missing":
                payload["cases"] = []
            elif change == "duplicate":
                payload["cases"] *= 2
            elif change == "zero-match":
                payload["cases"][0]["events"] = [{"Action": "pass", "Package": "fixture", "Elapsed": 0.01}]
            elif change == "skip":
                payload["cases"][0]["events"][-1]["Action"] = "skip"
            elif change == "pending":
                payload["pending"] = ["operation matrix"]
            else:
                payload["sourceSha256"] = "0" * 64
            self.assertEqual(self.execute(self.emit("RF01", "point", path, payload))["status"], "ANALYSIS_ERROR", change)

    def test_failed_gate_records_remaining_steps_not_run(self) -> None:
        plan = [self.child("first", "raise SystemExit(1)"), self.child("second", "raise RuntimeError('must not execute')")]
        results = final.run_plan(self.root, plan, self.snapshot, self.cases)
        self.assertEqual([result["status"] for result in results], ["FAIL", "NOT_RUN"])
        self.assertEqual(final.final_status(results), "FAIL")

    def test_child_source_mutation_invalidates_success(self) -> None:
        plan = [
            self.child("mutate", "from pathlib import Path;Path('source.go').write_text('changed')"),
            self.child("later", "pass"),
        ]
        results = final.run_plan(self.root, plan, self.snapshot, self.cases)
        self.assertEqual([result["status"] for result in results], ["ANALYSIS_ERROR", "NOT_RUN"])

    def prepare_acceptance_pointer(self) -> tuple[str, str]:
        run_id = "20260919T120000Z-12345678"
        pointer = self.root / ".artifacts/acceptance/current-run"
        pointer.parent.mkdir(parents=True, exist_ok=True)
        pointer.write_text(run_id)
        return run_id, ".artifacts/acceptance/" + run_id

    def test_blocked_input_does_not_suppress_next_original_case(self) -> None:
        run_id, base = self.prepare_acceptance_pointer()
        context = {"acceptanceRun": run_id}
        payload = {"caseId": "ACC-MZ-001", "status": "BLOCKED", "gitCommit": self.snapshot["commit"], "gitDirty": False}
        item = self.emit("ACC-MZ-001", "acceptance", base + "/cases/acc-mz-001/result.json", payload, 1)
        result = self.execute(item, context)
        self.assertEqual(result["status"], "BLOCKED")
        self.assertEqual(final.final_status([result, {"status": "PASS"}]), "BLOCKED")
        # Exercise the real serial loop, including its preparation-created run identity.
        prepared = {"status": "PREPARED", "gitCommit": self.snapshot["commit"], "gitDirty": False}
        prepare = self.emit("acceptance-prepare", "acceptance-prepare", base + "/run.json", prepared)
        following = self.child("following", "pass")
        results = final.run_plan(self.root, [prepare, item, following], self.snapshot, self.cases)
        self.assertEqual([entry["status"] for entry in results], ["PASS", "BLOCKED", "PASS"])

    def test_required_case_cannot_become_not_applicable(self) -> None:
        run_id, base = self.prepare_acceptance_pointer()
        for case, expected in [("ACC-NET-002", "NOT_APPLICABLE"), ("ACC-MZ-001", "FAIL")]:
            payload = {"caseId": case, "status": "NOT_APPLICABLE", "gitCommit": self.snapshot["commit"], "gitDirty": False}
            item = self.emit(case, "acceptance", base + "/cases/" + case.lower() + "/result.json", payload)
            self.assertEqual(self.execute(item, {"acceptanceRun": run_id})["status"], expected)

    def test_report_and_lock_reject_symlinks_and_overlapping_final(self) -> None:
        directory = self.root / ".artifacts/refactor/final"
        directory.mkdir(parents=True)
        with (directory / "coordinator.lock").open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(verify.AnalysisError, "already running"):
                final.run_final(self.root)
        (directory / "result.json").symlink_to(self.root / "source.go")
        with self.assertRaisesRegex(verify.AnalysisError, "symlinked"):
            final.write_report(self.root, {"status": "FAIL"})

    def test_empty_or_unfinished_execution_is_never_pass(self) -> None:
        self.assertEqual(final.final_status([]), "ANALYSIS_ERROR")
        for status in ("FAIL", "BLOCKED", "NOT_READY", "NOT_RUN", "ANALYSIS_ERROR", "SKIP"):
            self.assertNotEqual(final.final_status([{"status": "PASS"}, {"status": status}]), "PASS")


if __name__ == "__main__":
    unittest.main()
