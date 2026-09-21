#!/usr/bin/env python3
"""Contract tests for the repository source-structure quality gate."""

from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from datetime import date
from pathlib import Path

import quality_structure


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]


class RepositoryLintConfigurationTests(unittest.TestCase):
    def test_gosec_stays_removed_from_lint_config_and_allowlist(self) -> None:
        configuration = (REPOSITORY_ROOT / ".golangci.yml").read_text(
            encoding="utf-8"
        )
        allowlist = json.loads(
            (REPOSITORY_ROOT / "quality" / "go-suppressions.json").read_text(
                encoding="utf-8"
            )
        )

        self.assertNotRegex(configuration, r"(?m)^\s*-\s+gosec\s*$")
        self.assertNotIn(
            "gosec", {entry["linter"] for entry in allowlist["exceptions"]}
        )


class PhysicalLineTests(unittest.TestCase):
    def test_counts_empty_newline_and_missing_final_newline(self) -> None:
        self.assertEqual(quality_structure.count_physical_lines(b""), 0)
        self.assertEqual(quality_structure.count_physical_lines(b"one\n"), 1)
        self.assertEqual(quality_structure.count_physical_lines(b"one"), 1)
        self.assertEqual(quality_structure.count_physical_lines(b"one\ntwo"), 2)

    def test_counts_crlf_and_unicode_comments_by_physical_line(self) -> None:
        contents = "// 注释\r\npackage sample\r\n".encode()
        self.assertEqual(quality_structure.count_physical_lines(contents), 2)


class ClassificationTests(unittest.TestCase):
    def assert_classification(self, path: str, category: str, limit: int) -> None:
        classification = quality_structure.classify_path(path)
        self.assertIsNotNone(classification)
        assert classification is not None
        self.assertEqual(classification.category, category)
        self.assertEqual(classification.line_limit, limit)

    def test_classifies_every_controlled_source_category(self) -> None:
        self.assert_classification("internal/sample/service.go", "go-production", 1000)
        self.assert_classification("internal/sample/service_test.go", "go-test", 1200)
        self.assert_classification("web/features/sample/view.tsx", "web-production", 600)
        self.assert_classification("web/features/sample/view.test.tsx", "web-test", 800)
        self.assert_classification("web/e2e/sample.spec.ts", "web-test", 800)
        self.assert_classification("web/styles/sample.css", "web-css", 800)

    def test_generated_files_require_an_exact_path_and_marker(self) -> None:
        for filename in ("models.gen.go", "server.gen.go", "spec.gen.go"):
            self.assertTrue(
                quality_structure.classify_path(
                    f"internal/httpapi/generated/{filename}"
                ).generated
            )
        self.assertFalse(
            quality_structure.classify_path("internal/sample/generated.go").generated
        )


class RepositoryGateTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary_directory.name)
        subprocess.run(["git", "init", "-q"], cwd=self.root, check=True)
        (self.root / "quality").mkdir()
        self.write_allowlist([])

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def write(self, relative_path: str, contents: str, *, final_newline: bool = True) -> None:
        path = self.root / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        suffix = "\n" if final_newline else ""
        path.write_bytes((contents + suffix).encode())

    def write_lines(self, relative_path: str, count: int, *, final_newline: bool = True) -> None:
        self.write(relative_path, "\n".join("line" for _ in range(count)), final_newline=final_newline)

    def write_allowlist(self, exceptions: list[dict[str, object]]) -> None:
        (self.root / "quality" / "go-suppressions.json").write_text(
            json.dumps({"version": 1, "exceptions": exceptions}),
            encoding="utf-8",
        )

    def discover(self) -> list[str]:
        return quality_structure.discover_repository_files(self.root)

    def validate(self) -> list[quality_structure.Violation]:
        return quality_structure.validate_repository(self.root, today=date(2026, 8, 22))

    def test_discovers_tracked_and_untracked_but_not_ignored_files(self) -> None:
        self.write("tracked.go", "package tracked")
        subprocess.run(["git", "add", "tracked.go"], cwd=self.root, check=True)
        self.write("untracked.go", "package untracked")
        self.write("ignored.go", "package ignored")
        self.write(".gitignore", "ignored.go")

        self.assertEqual(
            self.discover(),
            [".gitignore", "quality/go-suppressions.json", "tracked.go", "untracked.go"],
        )

    def test_renamed_file_is_checked_under_its_new_path(self) -> None:
        self.write("internal/sample/old.go", "package sample")
        subprocess.run(["git", "add", "."], cwd=self.root, check=True)
        subprocess.run(
            ["git", "mv", "internal/sample/old.go", "internal/sample/new.go"],
            cwd=self.root,
            check=True,
        )
        self.assertIn("internal/sample/new.go", self.discover())
        self.assertNotIn("internal/sample/old.go", self.discover())

    def test_worktree_deleted_tracked_file_is_not_scanned(self) -> None:
        self.write("internal/sample/deleted.go", "package sample")
        subprocess.run(["git", "add", "."], cwd=self.root, check=True)
        (self.root / "internal" / "sample" / "deleted.go").unlink()

        self.assertNotIn("internal/sample/deleted.go", self.discover())

    def test_reports_boundary_plus_one_and_accepts_exact_boundary(self) -> None:
        self.write_lines("internal/sample/exact.go", 1000, final_newline=False)
        self.write_lines("internal/sample/large.go", 1001)

        violations = self.validate()

        self.assertFalse(any(item.path == "internal/sample/exact.go" for item in violations))
        large = [item for item in violations if item.path == "internal/sample/large.go"]
        self.assertEqual([(item.rule, item.actual, item.limit) for item in large], [("max-lines", 1001, 1000)])

    def test_rejects_forged_generated_marker_and_missing_marker(self) -> None:
        self.write(
            "internal/sample/forged.go",
            "// Code generated by a person. DO NOT EDIT.\npackage sample",
        )
        self.write("internal/httpapi/generated/models.gen.go", "package generated")

        violations = self.validate()

        self.assertEqual(
            {(item.path, item.rule) for item in violations},
            {
                ("internal/sample/forged.go", "generated-marker"),
                ("internal/httpapi/generated/models.gen.go", "generated-marker"),
            },
        )

    def test_rejects_structural_and_frontend_inline_suppressions(self) -> None:
        self.write(
            "internal/sample/service.go",
            "package sample\n\n//nolint:funlen // keep together\nfunc run() {}",
        )
        self.write(
            "web/features/sample/view.tsx",
            "// eslint-disable-next-line complexity\nexport const View = () => null;\n// @ts-ignore",
        )

        violations = self.validate()

        self.assertEqual(
            {(item.path, item.line, item.rule) for item in violations},
            {
                ("internal/sample/service.go", 3, "forbidden-go-suppression"),
                ("web/features/sample/view.tsx", 1, "forbidden-fe-suppression"),
                ("web/features/sample/view.tsx", 3, "forbidden-fe-suppression"),
            },
        )

    def test_nonstructural_suppression_and_allowlist_are_bidirectional(self) -> None:
        self.write(
            "internal/sample/service.go",
            "package sample\n\nfunc open() {\n\t_ = source() //nolint:errcheck // fixed trusted source\n}",
        )
        self.write_allowlist(
            [
                {
                    "path": "internal/sample/service.go",
                    "line": 4,
                    "symbol": "open",
                    "linter": "errcheck",
                    "reason": "The tool cannot infer that the path is fixed.",
                    "invariant": "No request-controlled value reaches the path.",
                    "reviewAfter": "2027-08-22",
                },
                {
                    "path": "internal/sample/unused.go",
                    "line": 9,
                    "symbol": "unused",
                    "linter": "errcheck",
                    "reason": "Unused fixture.",
                    "invariant": "No source suppression exists.",
                    "reviewAfter": "2027-08-22",
                },
            ]
        )

        violations = self.validate()

        self.assertEqual(
            {(item.path, item.line, item.rule) for item in violations},
            {("internal/sample/unused.go", 9, "unused-allowlist-entry")},
        )

    def test_rejects_unregistered_expired_and_multi_linter_suppressions(self) -> None:
        self.write(
            "internal/sample/service.go",
            "package sample\n\n"
            "func first() { _ = source() } //nolint:errcheck // reviewed exception\n"
            "func second() { _ = source() } //nolint:staticcheck,errcheck // invalid bundle\n",
        )
        self.write_allowlist(
            [
                {
                    "path": "internal/sample/service.go",
                    "line": 4,
                    "symbol": "first",
                    "linter": "staticcheck",
                    "reason": "The source is constant.",
                    "invariant": "No request-controlled value reaches the source.",
                    "reviewAfter": "2026-08-21",
                }
            ]
        )

        violations = self.validate()

        self.assertEqual(
            {(item.line, item.rule) for item in violations},
            {
                (3, "unregistered-go-suppression"),
                (4, "multi-linter-suppression"),
                (4, "expired-allowlist-entry"),
            },
        )


if __name__ == "__main__":
    unittest.main()
