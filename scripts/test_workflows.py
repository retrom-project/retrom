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
            "browser_ui": "make acceptance-case CASE=ACC-UI-011",
        }
        for job_id, command in commands.items():
            with self.subTest(job=job_id):
                job = ci_job(job_id)
                self.assertIn(command, job)
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
            ("browser_ui", "make acceptance-case CASE=ACC-UI-011"),
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
        self.assertIn(".artifacts/acceptance/*/cases/acc-ui-011/", browser)

        quality = ci_job("quality")
        self.assertIn("needs: [contracts, backend, integration, web, browser_ui]", quality)
        self.assertIn("if: ${{ always() }}", quality)
        for job_id in ("contracts", "backend", "integration", "web", "browser_ui"):
            self.assertIn(f"needs.{job_id}.result", quality)

        script = textwrap.dedent(quality.split("        run: |\n", 1)[1])
        results = (
            "CONTRACTS_RESULT", "BACKEND_RESULT", "INTEGRATION_RESULT",
            "WEB_RESULT", "BROWSER_UI_RESULT",
        )
        environment = {**os.environ, **dict.fromkeys(results, "success")}
        self.assertEqual(subprocess.run(["bash", "-e"], input=script, text=True,
                                        env=environment, capture_output=True).returncode, 0)
        for name in results:
            with self.subTest(failed_job=name):
                failed_environment = {**environment, name: "failure"}
                self.assertNotEqual(
                    subprocess.run(["bash", "-e"], input=script, text=True,
                                   env=failed_environment, capture_output=True).returncode,
                    0,
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


if __name__ == "__main__":
    unittest.main()
