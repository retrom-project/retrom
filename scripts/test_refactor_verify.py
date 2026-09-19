#!/usr/bin/env python3
"""Executable evidence must distinguish real cases from a green package summary."""

from __future__ import annotations

import importlib.util
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

SPEC = importlib.util.spec_from_file_location("refactor_verify", Path(__file__).with_name("refactor_verify.py"))
assert SPEC is not None and SPEC.loader is not None
VERIFY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(VERIFY)


def go_case(symbol: str = "TestPass") -> dict:
    return {
        "id": "RFA-RF01-fixture", "point_id": "RF01", "tier": "tool",
        "target_file": "internal/example/example_test.go", "symbol": symbol,
        "setup_and_action": "Execute a bounded isolated compiler fixture",
        "required_assertions": "The named Go case must actually pass",
        "runner": {
            "type": "go-test", "command": ["go", "test", "-count=1", "-json", "-timeout=10s", "./internal/example", "-run", "^" + symbol + "$"],
            "must_execute": symbol,
        },
        "hard_timeout_seconds": 10, "required": True,
    }


class GoExecutionEvidenceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.directory = tempfile.TemporaryDirectory(prefix="retrom-refactor-runner-")
        cls.root = Path(cls.directory.name)
        (cls.root / "go.mod").write_text("module fixture\n\ngo 1.26.5\n", encoding="utf-8")
        source = cls.root / "internal/example/example_test.go"
        source.parent.mkdir(parents=True)
        source.write_text(
            'package example\nimport "testing"\n'
            'func TestPass(t *testing.T) {}\n'
            'func TestSkip(t *testing.T) { t.Skip("negative runner fixture") }\n'
            'func TestFail(t *testing.T) { t.Fatal("negative runner fixture") }\n',
            encoding="utf-8",
        )

    @classmethod
    def tearDownClass(cls) -> None:
        cls.directory.cleanup()

    def test_real_named_pass_has_execution_events(self) -> None:
        result = VERIFY.run_go_leaf(go_case(), self.root)
        self.assertEqual(result["status"], "PASS")
        self.assertEqual(result["exitCode"], 0)
        self.assertEqual([event["Action"] for event in result["events"] if event["Action"] != "output"], ["run", "pass"])

    def test_zero_matching_tests_is_an_analysis_error(self) -> None:
        with self.assertRaisesRegex(VERIFY.AnalysisError, "did not execute"):
            VERIFY.run_go_leaf(go_case("TestAbsent"), self.root)

    def test_skipped_case_is_not_a_pass(self) -> None:
        result = VERIFY.run_go_leaf(go_case("TestSkip"), self.root)
        self.assertEqual(result["status"], "FAIL")
        self.assertEqual(result["exitCode"], 0)

    def test_failed_case_cannot_be_hidden_by_other_passes(self) -> None:
        result = VERIFY.run_go_leaf(go_case("TestFail"), self.root)
        self.assertEqual(result["status"], "FAIL")
        self.assertNotEqual(result["exitCode"], 0)

    def test_package_pass_cannot_stand_in_for_case(self) -> None:
        output = b'{"Action":"pass","Package":"fixture/internal/example","Elapsed":0}\n'
        with self.assertRaises(VERIFY.AnalysisError):
            VERIFY.parse_go_evidence(output, "TestPass", 10)

    def test_test_output_does_not_enter_evidence(self) -> None:
        output = b'\n'.join([
            b'{"Action":"run","Package":"fixture","Test":"TestPass"}',
            b'{"Action":"output","Test":"TestPass","Output":"secret-cookie=/private/path"}',
            b'{"Action":"pass","Package":"fixture","Test":"TestPass","Elapsed":0.001}',
        ])
        result = VERIFY.parse_go_evidence(output, "TestPass", 10)
        self.assertNotIn("secret-cookie", json.dumps(result))
        self.assertNotIn("/private/path", json.dumps(result))

    def test_impossible_test_results_fail_closed(self) -> None:
        start = {"Action": "run", "Package": "fixture", "Test": "TestPass"}
        end = {**start, "Action": "pass", "Elapsed": 0.001}
        for records in (
            [end, start], [start, start, end],
            [start, {**end, "Package": "different"}],
            *[[start, {**end, "Elapsed": value}] for value in (-1, "0", None, True, float("nan"), float("inf"))],
        ):
            with self.subTest(records=records), self.assertRaises(VERIFY.AnalysisError):
                VERIFY.parse_go_evidence("\n".join(json.dumps(item) for item in records).encode(), "TestPass", 10)

    def test_duplicate_or_invalid_events_fail_closed(self) -> None:
        for output in (
            b"not-json",
            b'{"Package":"fixture"}',
            b'{"Action":"run","Test":"TestPass"}\n'
            b'{"Action":"pass","Test":"TestPass"}\n'
            b'{"Action":"pass","Test":"TestPass"}',
        ):
            with self.subTest(output=output), self.assertRaises(VERIFY.AnalysisError):
                VERIFY.parse_go_evidence(output, "TestPass", 10)


class RegistryAndSourceTests(unittest.TestCase):
    def test_registry_rejects_unknown_fields_and_unbounded_cases(self) -> None:
        for mutation in (
            {"ignoreFailures": True},
            {"hard_timeout_seconds": 0},
            {"hard_timeout_seconds": 3600},
            {"required": False},
            {"target_file": "../outside.go"},
            {"point_id": "RF99"},
            {"point_id": None}, {"id": "RFA-RF01-"}, {"tier": "ignored"},
            {"symbol": []}, {"target_file": None}, {"target_file": "internal/../escape.go"},
            {"required_assertions": ""}, {"hard_timeout_seconds": True},
            {"runner": {"type": "go-test"}}, {"runner": {"type": []}},
        ):
            case = go_case()
            case.update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(VERIFY.AnalysisError):
                VERIFY.validate_case(case)

    def test_registry_matches_every_declared_case(self) -> None:
        cases = VERIFY.load_registry(Path(__file__).resolve().parents[1])
        self.assertEqual(len(cases), 102)
        self.assertEqual({case["point_id"] for case in cases}, {f"RF{number:02d}" for number in range(1, 23)})

    def test_registry_rejects_duplicate_keys(self) -> None:
        with self.assertRaises(VERIFY.AnalysisError):
            json.loads('{"cases":[],"cases":[1]}', object_pairs_hook=VERIFY.reject_duplicate_keys)

    def test_leaf_command_is_direct_and_bounded(self) -> None:
        case = go_case()
        command = VERIFY.go_test_command(case)
        self.assertEqual(command[:4], ["go", "test", "-count=1", "-json"])
        self.assertIn("-timeout=10s", command)
        self.assertNotIn("make", command)
        self.assertNotIn("refactor-final", command)
        case["tier"] = "integration"
        self.assertIn("-tags=integration", VERIFY.go_test_command(case))

    def test_fingerprint_covers_untracked_bytes_and_rejects_symlinks(self) -> None:
        with tempfile.TemporaryDirectory(prefix="retrom-refactor-fingerprint-") as directory:
            root = Path(directory)
            self.git(root, "init", "-q")
            self.git(root, "config", "user.name", "Refactor fixture")
            self.git(root, "config", "user.email", "fixture@example.invalid")
            self.git(root, "config", "commit.gpgsign", "false")
            source = root / "source.go"
            source.write_text("package fixture\n", encoding="utf-8")
            self.git(root, "add", "source.go")
            self.git(root, "commit", "-qm", "fixture")
            before = VERIFY.source_fingerprint(root)
            (root / "untracked.go").write_text("package fixture\n", encoding="utf-8")
            after = VERIFY.source_fingerprint(root)
            self.assertNotEqual(before["sourceSha256"], after["sourceSha256"])
            os.symlink("source.go", root / "link.go")
            with self.assertRaises(VERIFY.AnalysisError):
                VERIFY.source_fingerprint(root)

    @staticmethod
    def git(root: Path, *arguments: str) -> None:
        subprocess.run(["git", *arguments], cwd=root, check=True, capture_output=True)


if __name__ == "__main__":
    unittest.main()
