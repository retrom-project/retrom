#!/usr/bin/env python3
"""Regression tests for clean-runner GitHub Actions orchestration."""

from __future__ import annotations

import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]


class GitHubWorkflowDependencyTests(unittest.TestCase):
    def test_quality_jobs_prepare_dependencies_before_ci(self) -> None:
        for relative_path in (
            ".github/workflows/ci.yml",
            ".github/workflows/docker-image.yml",
        ):
            with self.subTest(workflow=relative_path):
                workflow = (REPOSITORY_ROOT / relative_path).read_text(encoding="utf-8")
                prepare_position = workflow.find("run: make prepare-deps")
                ci_position = workflow.find("run: make ci")
                self.assertNotEqual(prepare_position, -1, workflow)
                self.assertNotEqual(ci_position, -1, workflow)
                self.assertLess(prepare_position, ci_position, workflow)


if __name__ == "__main__":
    unittest.main()
