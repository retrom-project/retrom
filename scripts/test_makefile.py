#!/usr/bin/env python3
"""Regression tests for clean-checkout Makefile dependency ordering."""

from __future__ import annotations

import subprocess
import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]


class MakefileDependencyTests(unittest.TestCase):
    def assert_web_install_precedes(self, target: str, command: str) -> None:
        result = subprocess.run(
            ["make", "--no-print-directory", "--dry-run", target],
            cwd=REPOSITORY_ROOT,
            check=True,
            capture_output=True,
            text=True,
        )
        install_position = result.stdout.find("npm ci")
        command_position = result.stdout.find(command)
        self.assertNotEqual(install_position, -1, result.stdout)
        self.assertNotEqual(command_position, -1, result.stdout)
        self.assertLess(install_position, command_position, result.stdout)

    def test_api_check_installs_locked_web_dependencies_first(self) -> None:
        self.assert_web_install_precedes("api-check", "scripts/api-check.sh")

    def test_api_generate_installs_locked_web_dependencies_first(self) -> None:
        self.assert_web_install_precedes("api-generate", "npm run api:generate")


if __name__ == "__main__":
    unittest.main()
