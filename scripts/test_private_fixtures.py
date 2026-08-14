#!/usr/bin/env python3
"""Keep tests that consume private local fixtures out of the default CI tag."""

from __future__ import annotations

import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]


class PrivateFixtureBoundaryTests(unittest.TestCase):
    def test_go_tests_using_local_fixtures_require_explicit_tag(self) -> None:
        matches: list[Path] = []
        for path in (REPOSITORY_ROOT / "internal").rglob("*_test.go"):
            contents = path.read_text(encoding="utf-8")
            if "data/example/local-fixtures/" not in contents:
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
