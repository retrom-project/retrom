#!/usr/bin/env python3
"""Regression tests for clean-runner GitHub Actions orchestration."""

from __future__ import annotations

import os
import re
import subprocess
import textwrap
import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
CI_WORKFLOW = REPOSITORY_ROOT / ".github/workflows/ci.yml"


def ci_job(job_id: str) -> str:
    workflow = CI_WORKFLOW.read_text(encoding="utf-8")
    match = re.search(
        rf"(?ms)^  {re.escape(job_id)}:\n(.*?)(?=^  [a-z_]+:\n|\Z)",
        workflow,
    )
    if match is None:
        raise AssertionError(f"missing CI job: {job_id}")
    return match.group(1)


class GitHubWorkflowDependencyTests(unittest.TestCase):
    def test_pull_request_runs_all_quality_suites_independently(self) -> None:
        workflow = CI_WORKFLOW.read_text(encoding="utf-8")
        commands = {
            "contracts": "make ci-contracts",
            "backend": "make backend-check",
            "integration": "make integration-test",
            "web": "make web-check",
            "browser_ui": "make acceptance-case CASE=ACC-RF-BROWSER",
        }
        for job_id, command in commands.items():
            with self.subTest(job=job_id):
                job = ci_job(job_id)
                self.assertIn(command, job)
                if job_id == "browser_ui":
                    # The browser waits only for the change classification.
                    self.assertIn("    needs: changes\n", job)
                    job = job.replace("    needs: changes\n", "")
                self.assertNotIn("    needs:", job)
                self.assertIn("actions/checkout@v6", job)
        for job_id in ("contracts", "web", "browser_ui"):
            with self.subTest(node_job=job_id):
                job = ci_job(job_id)
                self.assertIn("actions/setup-node@v6", job)
                self.assertIn("retrom-node-", job)
        self.assertIn("actions/setup-go@v7", ci_job("contracts"))
        self.assertNotIn("run: make ci\n", workflow)
        self.assertNotIn("|| true", workflow)

    def test_runtime_consumers_prepare_their_own_dependencies(self) -> None:
        for job_id, command in (
            ("backend", "run: make backend-check"),
            ("integration", "run: make integration-test"),
            ("browser_ui", "make acceptance-case CASE=ACC-RF-BROWSER"),
        ):
            with self.subTest(job=job_id):
                job = ci_job(job_id)
                self.assertIn("actions/setup-go@v7", job)
                self.assertIn("actions/setup-python@v6", job)
                self.assertIn("retrom-dependencies-", job)
                self.assertTrue(
                    0 <= job.find("run: make prepare-deps") < job.find(command),
                    job,
                )

    def test_browser_evidence_and_required_quality_result_are_preserved(self) -> None:
        browser = ci_job("browser_ui")
        self.assertIn("actions/setup-node@v6", browser)
        self.assertIn("scripts/prepare-e2e-fonts.sh", browser)
        self.assertIn("make prepare-e2e-browser", browser)
        self.assertIn("make acceptance-prepare", browser)
        self.assertIn("if: always()", browser)
        self.assertIn(".artifacts/acceptance/*/cases/acc-rf-browser/", browser)
        self.assertIn("name: ui-consistency-evidence-${{ matrix.project }}", browser)

        quality = ci_job("quality")
        self.assertIn("needs: [changes, contracts, backend, integration, web, browser_ui]", quality)
        self.assertIn("if: ${{ always() }}", quality)
        for job_id in ("changes", "contracts", "backend", "integration", "web", "browser_ui"):
            self.assertIn(f"needs.{job_id}.result", quality)

        script = textwrap.dedent(quality.split("        run: |\n", 1)[1])
        results = (
            "CHANGES_RESULT", "CONTRACTS_RESULT", "BACKEND_RESULT", "INTEGRATION_RESULT",
            "WEB_RESULT", "BROWSER_UI_RESULT",
        )
        environment = {**os.environ, **dict.fromkeys(results, "success"), "PRODUCT_CHANGED": "true"}
        run = lambda env: subprocess.run(["bash", "-e"], input=script, text=True,
                                         env=env, capture_output=True).returncode
        self.assertEqual(run(environment), 0)
        for name in results:
            for outcome in ("failure", "cancelled", "skipped"):
                with self.subTest(job=name, outcome=outcome):
                    self.assertNotEqual(run({**environment, name: outcome}), 0)
        # Only a documentation-only change may skip the browser, never fail it.
        documentation = {**environment, "PRODUCT_CHANGED": "false"}
        self.assertEqual(run({**documentation, "BROWSER_UI_RESULT": "skipped"}), 0)
        self.assertNotEqual(run({**documentation, "BROWSER_UI_RESULT": "failure"}), 0)
        self.assertNotEqual(run({**documentation, "CONTRACTS_RESULT": "skipped"}), 0)

    def test_browser_shards_cover_exactly_the_configured_viewports(self) -> None:
        browser = ci_job("browser_ui")
        config = (REPOSITORY_ROOT / "web/playwright.config.ts").read_text(encoding="utf-8")
        configured = re.findall(r'^\s*name: "([^"]+)",$', config, re.MULTILINE)
        matrix = re.search(r"^        project: \[(.*)\]$", browser, re.MULTILINE)
        self.assertIsNotNone(matrix, browser)
        self.assertEqual(sorted(configured), sorted(item.strip() for item in matrix.group(1).split(",")))
        self.assertEqual(len(configured), 4)
        self.assertIn("fail-fast: false", browser)
        self.assertIn("RETROM_E2E_PROJECT: ${{ matrix.project }}", browser)
        self.assertIn("if: ${{ needs.changes.outputs.product == 'true' }}", browser)

    def test_master_warms_every_cache_pull_requests_restore(self) -> None:
        workflows = REPOSITORY_ROOT / ".github/workflows"
        ci = CI_WORKFLOW.read_text(encoding="utf-8")
        warm = (workflows / "cache-warm.yml").read_text(encoding="utf-8")
        # Pull requests restore only caches saved on the default branch.
        self.assertIn("on:\n  push:\n    branches: [master]\n  workflow_dispatch:", warm)
        self.assertNotIn("pull_request", warm)
        entry = re.compile(r"uses: actions/cache(?:/restore)?@v5\n        with:\n"
                           r"          path: (.*?)\n          key: (.*?)\n", re.DOTALL)
        produced = {key: path for path, key in entry.findall(warm)}
        consumed = {key: path for path, key in entry.findall(ci)}
        self.assertTrue(consumed)
        for key, path in consumed.items():
            with self.subTest(key=key[:40]):
                self.assertEqual(produced.get(key), path)
        for setup in ("cache: true\n          cache-dependency-path: go.sum",
                      "cache: npm\n          cache-dependency-path: web/package-lock.json"):
            self.assertIn(setup, ci)
            self.assertIn(setup, warm)
        self.assertIn("scripts/prepare-postgres.sh", warm)
        self.assertIn("make prepare-deps", warm)
        self.assertIn("make build web-install", warm)
        self.assertIn("go test -run '^$' ./...", warm)

    def test_browser_installs_web_dependencies_once(self) -> None:
        browser = ci_job("browser_ui")
        self.assertNotIn("make build web-install", browser)
        self.assertEqual(browser.count("npm ci"), 0)
        self.assertLess(browser.index("run: make build\n"), browser.index("make prepare-e2e-browser"))

    def test_change_scope_compares_the_merge_commit_with_its_base(self) -> None:
        changes = ci_job("changes")
        self.assertIn("fetch-depth: 2", changes)
        self.assertIn('run: python3 scripts/ci_scope.py HEAD^1 HEAD >> "$GITHUB_OUTPUT"', changes)
        self.assertIn("product: ${{ steps.scope.outputs.product }}", changes)

    def test_integration_has_the_pinned_runtime_required_by_parent_uploads(self) -> None:
        job = ci_job("integration")
        self.assertIn("actions/setup-node@v6", job)
        self.assertIn("node-version-file: .node-version", job)
        self.assertTrue(
            0 <= job.find("run: python3 scripts/prepare_image_inputs.py")
            < job.find("run: make integration-test"),
            job,
        )

    def test_tag_release_builds_without_repeating_quality_job(self) -> None:
        workflow = (REPOSITORY_ROOT / ".github/workflows/docker-image.yml").read_text(
            encoding="utf-8"
        )
        self.assertIn('tags: ["*"]', workflow)
        self.assertIn("  build-and-push:", workflow)
        self.assertIn(
            'run: make build-images BACKEND_IMAGE="$BACKEND_IMAGE" '
            'WEB_IMAGE="$WEB_IMAGE" IMAGE_TAG="$IMAGE_TAG"',
            workflow,
        )
        self.assertNotIn("  quality:", workflow)
        self.assertNotIn("needs: quality", workflow)
        self.assertNotIn("run: make ci", workflow)
        self.assertNotIn("run: make prepare-deps", workflow)

    def test_pull_request_images_are_verified_and_separate_from_production(self) -> None:
        workflow = (REPOSITORY_ROOT / ".github/workflows/branch-image.yml").read_text(
            encoding="utf-8"
        )
        self.assertIn("  pull_request:\n    branches: [master]", workflow)
        self.assertIn("BUILD_SHA: ${{ github.sha }}", workflow)
        self.assertIn("persist-credentials: false", workflow)
        self.assertIn("pr-${PR_NUMBER}-${BUILD_SHA:0:12}", workflow)
        self.assertIn("run: make build-images BACKEND_IMAGE=", workflow)
        self.assertLess(workflow.index("- name: Build and verify both branch images"),
                        workflow.index("- name: Publish branch images to GHCR"))
        self.assertIn("ghcr.io/${{ github.repository_owner }}/retrom-branch", workflow)
        self.assertIn("ghcr.io/${{ github.repository_owner }}/retrom-web-branch", workflow)
        self.assertIn(
            "if: ${{ github.event.pull_request.head.repo.full_name == github.repository }}",
            workflow,
        )
        self.assertNotIn("DOCKER_PASSWORD", workflow)
        self.assertNotIn("xxxsen/retrom", workflow)

    def test_browser_and_images_require_explicit_same_generation_inputs(self) -> None:
        for job in (ci_job("browser_ui"), *( (REPOSITORY_ROOT / ".github/workflows" / name).read_text() for name in ("branch-image.yml", "docker-image.yml") )):
            self.assertIn("scripts/prepare_image_inputs.py", job)
            self.assertIn("RETROM_RUNTIME_INPUT_BASE_URL", job)
            for variable in ("RETROM_RUNTIME_TOOL_ARCHIVE_URL", "RETROM_RUNTIME_TOOL_ARCHIVE_SHA256",
                             "RETROM_PROVIDER_ARCHIVE_URL", "RETROM_PROVIDER_ARCHIVE_SHA256"):
                self.assertNotIn(variable, job)
            self.assertNotIn("runtime-provider-prepare-auto", job)
            self.assertNotIn("--manifest", job)

    def test_both_image_pipelines_require_non_root_backend_startup_before_publish(self) -> None:
        for filename, publish in (("branch-image.yml", "Publish branch images to GHCR"),
                                  ("docker-image.yml", "Log in to Docker Hub")):
            with self.subTest(workflow=filename):
                workflow = (REPOSITORY_ROOT / ".github/workflows" / filename).read_text()
                command = 'bash scripts/verify-backend-image.sh "$BACKEND_IMAGE:$IMAGE_TAG" "$WEB_IMAGE:$IMAGE_TAG"'
                self.assertIn(command, workflow)
                self.assertLess(workflow.index(command), workflow.index(publish))


if __name__ == "__main__":
    unittest.main()
