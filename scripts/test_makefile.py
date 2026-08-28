#!/usr/bin/env python3
"""Regression tests for clean-checkout Makefile dependency ordering."""

from __future__ import annotations

import json
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
        self.assertIn("rpgmaker-smoke/build.py --check", output)

    def test_web_e2e_startup_budget_covers_dependency_validation(self) -> None:
        script = (REPOSITORY_ROOT / "scripts" / "acceptance" / "web-e2e.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn("server_start_timeout_seconds=300", script)
        self.assertIn("SECONDS + server_start_timeout_seconds", script)
        self.assertNotIn("deadline=$((SECONDS + 90))", script)

    def test_web_e2e_uses_an_isolated_test_runtime_origin(self) -> None:
        script = (REPOSITORY_ROOT / "scripts" / "acceptance" / "web-e2e.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn(
            'runtime_origin_template="http://{launchId}.rpg.localhost:${backend_port}"',
            script,
        )
        self.assertIn('RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN="true"', script)
        self.assertIn(
            'RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE="$runtime_origin_template"',
            script,
        )

    def test_web_e2e_supports_a_focused_playwright_regression_without_changing_the_default(self) -> None:
        script = (REPOSITORY_ROOT / "scripts" / "acceptance" / "web-e2e.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn("playwright_command=(npm run test:e2e)", script)
        self.assertIn('e2e_grep="${RETROM_E2E_GREP:-}"', script)
        self.assertIn("unset RETROM_E2E_GREP", script)
        self.assertIn('playwright_command+=(-- --grep "$e2e_grep")', script)
        self.assertIn('"${playwright_command[@]}"', script)

    def test_runtime_e2e_uses_the_stable_player_frame_and_case_budgets(self) -> None:
        runtime_cases = (
            REPOSITORY_ROOT / "web" / "e2e" / "acceptance-runtime-cases.ts"
        ).read_text(encoding="utf-8")
        expansion_cases = (
            REPOSITORY_ROOT / "web" / "e2e" / "acceptance-core-expansion-cases.ts"
        ).read_text(encoding="utf-8")
        support = (
            REPOSITORY_ROOT / "web" / "e2e" / "acceptance-support.ts"
        ).read_text(encoding="utf-8")
        ui_cases = (
            REPOSITORY_ROOT / "web" / "e2e" / "acceptance.spec.ts"
        ).read_text(encoding="utf-8")
        stale_selector = 'iframe[title="Retrom EmulatorJS Player"]'
        self.assertNotIn(stale_selector, runtime_cases)
        self.assertNotIn(stale_selector, expansion_cases)
        self.assertNotIn(stale_selector, support)
        for relative_path in (
            "emulationstation-import.spec.ts",
            "immersive.spec.ts",
            "immersive-library.spec.ts",
            "mobile.spec.ts",
            "navigation.spec.ts",
            "server-import.spec.ts",
        ):
            immersive_cases = (REPOSITORY_ROOT / "web" / "e2e" / relative_path).read_text(encoding="utf-8")
            self.assertNotIn(stale_selector, immersive_cases)
        self.assertGreaterEqual(runtime_cases.count('test.setTimeout(180_000)'), 3)
        self.assertIn('test.setTimeout(180_000);\n  const routes = [', ui_cases)

    def test_arcade_acceptance_selects_the_current_launchable_artifact(self) -> None:
        script = (REPOSITORY_ROOT / "scripts" / "acceptance" / "arcade-flow.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn(".selectedForNewBindings == true", script)
        self.assertIn(".availableForLaunch == true", script)
        self.assertNotIn(".enabled == true", script)

    def test_arcade_schema_v2_seeder_preserves_runtime_identity(self) -> None:
        script = (
            REPOSITORY_ROOT / "scripts" / "acceptance" / "seed-arcade-schema-v2-launch.py"
        ).read_text(encoding="utf-8")
        self.assertIn("revision.route_key", script)
        self.assertIn("core_artifact_id,route_key,dat_version_id", script)
        self.assertIn('connection.execute("PRAGMA busy_timeout=30000")', script)

    def test_immersive_seeder_preserves_launch_content_and_route_identity(self) -> None:
        script = (
            REPOSITORY_ROOT / "scripts" / "acceptance" / "seed-immersive-library.py"
        ).read_text(encoding="utf-8")
        self.assertIn("revision.route_key", script)
        self.assertIn("game_content_revision_id", script)
        self.assertIn("core_artifact_id,route_key,save_state_id", script)

    def test_public_fixture_targets_cover_rpgmaker_outputs(self) -> None:
        self.assertIn("rpgmaker-smoke/build.py", self.dry_run("public-fixtures-generate"))
        self.assertIn("rpgmaker-smoke/build.py --check", self.dry_run("public-fixtures-check"))

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
        dev_script = (REPOSITORY_ROOT / "scripts" / "dev.sh").read_text(encoding="utf-8")
        self.assertIn('next dev --hostname "$2" --port "$3" --webpack', dev_script)
        package = json.loads((REPOSITORY_ROOT / "web" / "package.json").read_text(encoding="utf-8"))
        self.assertTrue(package["scripts"]["dev"].endswith("--webpack"))

    def test_rpg_runtime_uses_release_assets_without_a_local_build_target(self) -> None:
        for target in ("prepare-deps", "dev"):
            output = self.dry_run(target)
            self.assertNotIn("build.py reproduce", output)
            self.assertNotIn("docker run", output)
        makefile = (REPOSITORY_ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertNotIn("reproduce-rpg-runtime", makefile)

    def test_dev_can_link_an_explicit_local_runtime_after_locked_dependencies(self) -> None:
        local_runtime = REPOSITORY_ROOT.parent / "retrom-runtime"
        output = subprocess.run(
            [
                "make", "--no-print-directory", "--dry-run", "dev",
                f"RETROM_RUNTIME_DEV_ROOT={local_runtime}",
            ],
            cwd=REPOSITORY_ROOT,
            check=True,
            capture_output=True,
            text=True,
        ).stdout
        self.assertLess(output.find("npm ci"), output.find("retrom_runtime_dev.py activate"), output)
        self.assertLess(output.find("retrom_runtime_dev.py activate"), output.find("scripts/dev.sh"), output)
        self.assertIn("npm run build", output)
        self.assertIn("npm run package:check", output)
        self.assertNotIn("npm run release:build", output)
        self.assertNotIn("core:ons:build", output)
        self.assertIn("env -u RETROM_RUNTIME_DEV_ROOT scripts/dev.sh", output)

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
