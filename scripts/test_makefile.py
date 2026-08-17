#!/usr/bin/env python3
"""Regression tests for clean-checkout Makefile dependency ordering."""

from __future__ import annotations

import subprocess
import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]


class MakefileDependencyTests(unittest.TestCase):
    def dry_run(self, target: str) -> str:
        return subprocess.run(
            ["make", "--no-print-directory", "--dry-run", target],
            cwd=REPOSITORY_ROOT,
            check=True,
            capture_output=True,
            text=True,
        ).stdout

    def assert_web_install_precedes(self, target: str, command: str) -> None:
        output = self.dry_run(target)
        install_position = output.find("npm ci")
        command_position = output.find(command)
        self.assertNotEqual(install_position, -1, output)
        self.assertNotEqual(command_position, -1, output)
        self.assertLess(install_position, command_position, output)

    def test_api_check_installs_locked_web_dependencies_first(self) -> None:
        self.assert_web_install_precedes("api-check", "scripts/api-check.sh")

    def test_api_generate_installs_locked_web_dependencies_first(self) -> None:
        self.assert_web_install_precedes("api-generate", "npm run api:generate")

    def test_web_e2e_prepares_locked_browser_after_web_dependencies(self) -> None:
        output = self.dry_run("web-e2e")
        install_position = output.find("npm ci")
        browser_position = output.find("scripts/prepare-e2e-browser.sh")
        fixture_position = output.find("gba-smoke/build.py --check")
        e2e_position = output.find("scripts/acceptance/web-e2e.sh")
        self.assertTrue(
            0
            <= install_position
            < browser_position
            < fixture_position
            < e2e_position,
            output,
        )

    def test_install_deps_covers_all_project_dependency_classes(self) -> None:
        output = self.dry_run("install-deps")
        for command in (
            "go install mvdan.cc/gofumpt",
            "go install github.com/golangci/golangci-lint",
            "scripts/dependencies.py prepare",
            "npm ci",
            "scripts/prepare-e2e-browser.sh",
            "gba-smoke/build.py --check",
            "go mod download",
        ):
            self.assertIn(command, output)

    def test_dev_installs_locked_web_dependencies_before_starting(self) -> None:
        self.assert_web_install_precedes("dev", "scripts/dev.sh")


if __name__ == "__main__":
    unittest.main()
