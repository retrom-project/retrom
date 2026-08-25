#!/usr/bin/env python3
"""Regression tests for clean-runner GitHub Actions orchestration."""

from __future__ import annotations

import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]


class GitHubWorkflowDependencyTests(unittest.TestCase):
    def test_pull_request_quality_prepares_dependencies_before_ci(self) -> None:
        workflow = (REPOSITORY_ROOT / ".github/workflows/ci.yml").read_text(
            encoding="utf-8"
        )
        prepare_position = workflow.find("run: make prepare-deps")
        ci_position = workflow.find("run: make ci")
        self.assertNotEqual(prepare_position, -1, workflow)
        self.assertNotEqual(ci_position, -1, workflow)
        self.assertLess(prepare_position, ci_position, workflow)

    def test_pull_request_runs_structure_gate_before_runtime_preparation(self) -> None:
        workflow = (REPOSITORY_ROOT / ".github/workflows/ci.yml").read_text(
            encoding="utf-8"
        )
        structure_position = workflow.find("run: make quality-structure-check")
        prepare_position = workflow.find("run: make prepare-deps")
        self.assertTrue(0 <= structure_position < prepare_position, workflow)
        self.assertNotIn("make quality-structure-check || true", workflow)

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


if __name__ == "__main__":
    unittest.main()
