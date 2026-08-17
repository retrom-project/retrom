#!/usr/bin/env python3
"""Keep tests that consume private local fixtures out of the default CI tag."""

from __future__ import annotations

import subprocess
import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]


class PrivateFixtureBoundaryTests(unittest.TestCase):
    def test_private_resources_have_one_ignored_root(self) -> None:
        tracked = subprocess.run(
            ["git", "ls-files", "--", "data/game"],
            cwd=REPOSITORY_ROOT,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
        self.assertEqual("", tracked, "data/game must not contain tracked private resources")

        ignored = subprocess.run(
            ["git", "check-ignore", "-q", "data/game/.retrom-private-resource-probe"],
            cwd=REPOSITORY_ROOT,
            check=False,
        )
        self.assertEqual(0, ignored.returncode, "data/game must remain Git-ignored")

    def test_removed_private_resource_roots_are_not_referenced(self) -> None:
        for legacy_path in (
            "/".join(("data", "example")),
            "/".join(("data", "core-validation")),
            "/".join(("demo", "netplay-demo")),
        ):
            self.assertFalse(
                (REPOSITORY_ROOT / legacy_path).exists(),
                f"removed private-resource root still exists: {legacy_path}",
            )
            result = subprocess.run(
                ["git", "grep", "-n", "-F", legacy_path, "--", "."],
                cwd=REPOSITORY_ROOT,
                check=False,
                capture_output=True,
                text=True,
            )
            self.assertEqual(
                1,
                result.returncode,
                f"removed private-resource root is still referenced:\n{result.stdout}",
            )

    def test_go_tests_using_local_fixtures_require_explicit_tag(self) -> None:
        matches: list[Path] = []
        for path in (REPOSITORY_ROOT / "internal").rglob("*_test.go"):
            contents = path.read_text(encoding="utf-8")
            if "data/game/" not in contents:
                continue
            matches.append(path)
            build_constraint = contents.splitlines()[0]
            self.assertIn(
                "localfixtures",
                build_constraint,
                f"{path.relative_to(REPOSITORY_ROOT)} must require the localfixtures build tag",
            )
        self.assertTrue(matches, "expected at least one private-fixture Go test")


if __name__ == "__main__":
    unittest.main()
