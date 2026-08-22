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

    def test_backend_build_generates_go_api_before_compiling(self) -> None:
        output = subprocess.run(
            ["make", "--no-print-directory", "--dry-run", "--always-make", "build"],
            cwd=REPOSITORY_ROOT,
            check=True,
            capture_output=True,
            text=True,
        ).stdout
        generator_position = output.find("oapi-codegen")
        build_position = output.find("go build ./cmd/retrom")
        self.assertTrue(0 <= generator_position < build_position, output)

    def test_generated_go_api_is_ignored_and_untracked(self) -> None:
        generated = "internal/httpapi/generated/api.gen.go"
        ignored = subprocess.run(
            ["git", "check-ignore", "--quiet", generated],
            cwd=REPOSITORY_ROOT,
            check=False,
        )
        tracked = subprocess.run(
            ["git", "ls-files", "--error-unmatch", generated],
            cwd=REPOSITORY_ROOT,
            check=False,
            capture_output=True,
            text=True,
        )
        self.assertEqual(ignored.returncode, 0, "api.gen.go must be ignored")
        self.assertNotEqual(tracked.returncode, 0, "api.gen.go must not be tracked")

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
        output = self.dry_run("dev")
        install_position = output.find("npm ci")
        dev_position = output.find("scripts/dev.sh")
        self.assertTrue(0 <= install_position < dev_position, output)
        self.assertIn(
            f'RETROM_DEV_STATE_DIR="{REPOSITORY_ROOT / ".dev-data/dev-state"}"',
            output,
        )
        self.assertIn(
            f'RETROM_DATA_DIR="{REPOSITORY_ROOT / ".dev-data/data"}"',
            output,
        )
        makefile = (REPOSITORY_ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertIn("RETROM_DEV_CONFIG ?= $(abspath .dev-data/dev.mk)", makefile)
        self.assertIn("-include $(RETROM_DEV_CONFIG)", makefile)

    def test_ci_runs_the_structure_gate_without_warning_only_bypass(self) -> None:
        output = self.dry_run("ci")
        structure_position = output.find("scripts/quality_structure.py")
        api_position = output.find("scripts/api-check.sh")
        self.assertTrue(0 <= structure_position < api_position, output)
        self.assertNotIn("quality_structure.py || true", output)

    def test_backend_and_web_checks_run_the_same_structure_gate(self) -> None:
        for target in ("backend-check", "web-check"):
            output = self.dry_run(target)
            self.assertEqual(output.count("scripts/quality_structure.py"), 1, output)


if __name__ == "__main__":
    unittest.main()
