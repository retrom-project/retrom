#!/usr/bin/env python3
"""Regression tests for clean-checkout Makefile dependency ordering."""

from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]


class MakefileDependencyTests(unittest.TestCase):
    def test_runtime_resource_change_regenerates_openapi_bundle(self) -> None:
        with tempfile.NamedTemporaryFile(suffix=".yaml") as bundle:
            output = subprocess.run(
                ["make", "--no-print-directory", "--dry-run", "--what-if=api/runtime-provider/v1/runtime-resource.schema.json",
                 f"API_BUNDLE={bundle.name}", "api-bundle"],
                cwd=REPOSITORY_ROOT, check=True, text=True, capture_output=True,
            ).stdout
        self.assertIn("go run ./scripts/openapi-bundle", output)

    def test_provider_manifest_change_regenerates_openapi_bundle(self) -> None:
        with tempfile.NamedTemporaryFile(suffix=".yaml") as bundle:
            output = subprocess.run(
                ["make", "--no-print-directory", "--dry-run", "--what-if=api/runtime-provider/v1/provider-manifest.schema.json",
                 f"API_BUNDLE={bundle.name}", "api-bundle"],
                cwd=REPOSITORY_ROOT, check=True, text=True, capture_output=True,
            ).stdout
        self.assertIn("go run ./scripts/openapi-bundle", output)

    def test_data_check_covers_preserving_pfb_identity_when_adding_a_core(self) -> None:
        self.assertIn("python3 -m unittest scripts.test_pfb_init", self.dry_run("data-check"))
        self.assertIn("python3 -m unittest scripts.test_pfb_data_reset", self.dry_run("data-check"))

    def test_data_reset_forwards_optional_verified_provider_base(self) -> None:
        output = subprocess.run(
            ["make", "--dry-run", "pfb-data-reset", "PFB=fixture", "CONFIRM=fixture-000000000000",
             "SOURCE_ROOT=/tmp/verified-provider-base"],
            cwd=REPOSITORY_ROOT, check=True, text=True, capture_output=True,
        ).stdout
        self.assertIn('--source-root "/tmp/verified-provider-base"', output)
        self.assertNotIn("--source-root", self.dry_run("pfb-data-reset"))

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
        bundle_position = output.find("go run ./scripts/openapi-bundle")
        generator_position = output.find("oapi-codegen")
        build_position = output.find("go build ./cmd/retrom")
        self.assertTrue(
            0 <= bundle_position < generator_position < build_position,
            output,
        )
        for config in ("models.yaml", "server.yaml", "spec.yaml"):
            self.assertIn(f"api/codegen/{config}", output)

    def test_generated_go_api_is_ignored_and_untracked(self) -> None:
        for filename in ("models.gen.go", "server.gen.go", "spec.gen.go"):
            generated = f"internal/httpapi/generated/{filename}"
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
            self.assertEqual(ignored.returncode, 0, f"{filename} must be ignored")
            self.assertNotEqual(tracked.returncode, 0, f"{filename} must not be tracked")

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
        self.assertIn("rpgmaker-smoke/build.py --check", output)

    def test_web_e2e_collects_only_playwright_spec_files(self) -> None:
        configuration = (
            REPOSITORY_ROOT / "web" / "playwright.config.ts"
        ).read_text(encoding="utf-8")
        self.assertIn("testMatch: /.*\\.spec\\.ts/", configuration)

    def test_public_fixture_targets_cover_rpgmaker_outputs(self) -> None:
        self.assertIn("rpgmaker-smoke/build.py", self.dry_run("public-fixtures-generate"))
        self.assertIn("rpgmaker-smoke/build.py --check", self.dry_run("public-fixtures-check"))

    def test_install_deps_covers_all_project_dependency_classes(self) -> None:
        output = self.dry_run("install-deps")
        for command in (
            "scripts/prepare-go.sh",
            "go install mvdan.cc/gofumpt",
            "go install github.com/golangci/golangci-lint",
            "scripts/dependencies.py prepare-auth",
            "npm ci",
            "scripts/prepare-e2e-browser.sh",
            "gba-smoke/build.py --check",
            "go mod download",
        ):
            self.assertIn(command, output)

    def test_dev_installs_locked_web_dependencies_before_starting(self) -> None:
        output = self.dry_run("dev")
        user_check_position = output.find("python3 scripts/local_user.py")
        go_prepare_position = output.find("scripts/prepare-go.sh")
        install_position = output.find("npm ci")
        dev_position = output.find("scripts/dev.sh")
        self.assertTrue(
            0 <= user_check_position < go_prepare_position < install_position < dev_position,
            output,
        )
        self.assertIn('RETROM_PUBLIC_ORIGIN="http://localhost:4000"', output)
        self.assertIn('NEXT_DEV_PORT="4000"', output)
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
        dev_script = (REPOSITORY_ROOT / "scripts" / "dev.sh").read_text(encoding="utf-8")
        self.assertIn('next dev --hostname "$2" --port "$3" --webpack', dev_script)
        package = json.loads((REPOSITORY_ROOT / "web" / "package.json").read_text(encoding="utf-8"))
        self.assertTrue(package["scripts"]["dev"].endswith("--webpack"))

    def test_host_does_not_install_provider_checkpoint_compression(self) -> None:
        web = REPOSITORY_ROOT / "web"
        package = json.loads((web / "package.json").read_text(encoding="utf-8"))
        lock = json.loads((web / "package-lock.json").read_text(encoding="utf-8"))
        self.assertNotIn("fflate", package["dependencies"])
        self.assertNotIn("node_modules/fflate", lock["packages"])

    def test_pfb_dev_reuses_the_image_toolchains(self) -> None:
        entrypoint = (REPOSITORY_ROOT / "scripts/pfb/entrypoint.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn("/workspace/retrom/scripts/dev.sh", entrypoint)
        self.assertIn("pfb-provider-watch.mjs", entrypoint)
        self.assertNotIn("make dev", entrypoint)
        for target in (
            "pfb-init", "pfb-validate", "pfb-build", "pfb-up", "pfb-use",
            "pfb-restart", "pfb-down", "pfb-status", "pfb-logs", "pfb-verify",
            "pfb-core-build", "pfb-provider-import", "pfb-migrate-storage", "pfb-data-reset", "pfb-remove",
            "pfb-destroy", "pfb-gateway-up", "pfb-gateway-down",
        ):
            output = self.dry_run(target)
            self.assertLess(
                output.find("python3 scripts/local_user.py"),
                output.find("python3 -m scripts.pfb.cli"),
                output,
            )

    def test_rpg_runtime_uses_release_assets_without_a_local_build_target(self) -> None:
        for target in ("prepare-deps", "dev"):
            output = self.dry_run(target)
            self.assertNotIn("build.py reproduce", output)
            self.assertNotIn("docker run", output)
        makefile = (REPOSITORY_ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertNotIn("reproduce-rpg-runtime", makefile)

    def test_dev_requires_verified_paired_runtime_inputs(self) -> None:
        output = self.dry_run("dev")
        self.assertIn("image_input_digest.py --check", output)
        self.assertIn("prepare_image_inputs.py", output)
        self.assertIn("--run scripts/dev.sh", output)
        self.assertNotIn("RETROM_MULTI_DISC", output)
        self.assertNotIn("runtime_providers.py prepare", output)

    def test_ci_runs_the_structure_gate_without_warning_only_bypass(self) -> None:
        output = self.dry_run("ci")
        structure_position = output.find("scripts/quality_structure.py")
        api_position = output.find("scripts/api-check.sh")
        self.assertTrue(0 <= structure_position < api_position, output)
        self.assertNotIn("quality_structure.py || true", output)

    def test_pr_contracts_target_keeps_every_non_application_gate(self) -> None:
        output = self.dry_run("ci-contracts")
        for command in (
            "python3 workspace/catalog.py",
            "python3 scripts/quality_structure.py",
            "scripts/api-check.sh",
            "python3 scripts/test_workflows.py",
            "python3 scripts/dependencies.py data-check",
        ):
            self.assertIn(command, output)
        self.assertLess(output.index("scripts/quality_structure.py"),
                        output.index("scripts/api-check.sh"))

    def test_backend_and_web_checks_run_the_same_structure_gate(self) -> None:
        for target in ("backend-check", "web-check"):
            output = self.dry_run(target)
            self.assertEqual(output.count("scripts/quality_structure.py"), 1, output)


if __name__ == "__main__":
    unittest.main()
