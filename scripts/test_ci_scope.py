#!/usr/bin/env python3
"""Only pure documentation changes may skip the product browser gate."""
from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from ci_scope import documentation_only  # noqa: E402

SCRIPT = Path(__file__).resolve().parent / "ci_scope.py"


class ChangeScopeTests(unittest.TestCase):
    def test_markdown_and_docs_are_documentation(self) -> None:
        self.assertTrue(documentation_only(["CLAUDE.md", "web/AGENTS.md", "docs/ui/review.html"]))

    def test_any_product_path_requires_the_browser(self) -> None:
        for path in ("data/runtime-inputs.json", "web/app/page.tsx", ".github/workflows/ci.yml",
                     "scripts/acceptance/browser.py", "docsite/index.md.tsx", "README.md.orig"):
            with self.subTest(path=path):
                self.assertFalse(documentation_only(["docs/guide.md", path]))

    def test_empty_change_set_is_not_documentation_only(self) -> None:
        self.assertFalse(documentation_only([]))

    def test_cli_reads_the_git_diff_between_commits(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            def git(*arguments: str) -> str:
                return subprocess.run(["git", *arguments], cwd=directory, check=True,
                                      capture_output=True, text=True).stdout.strip()
            git("init", "-q")
            git("config", "user.email", "ci@example.invalid")
            git("config", "user.name", "ci")
            Path(directory, "app.go").write_text("package app\n")
            git("add", "."); git("commit", "-qm", "base")
            base = git("rev-parse", "HEAD")
            Path(directory, "README.md").write_text("docs\n")
            git("add", "."); git("commit", "-qm", "docs")
            docs = git("rev-parse", "HEAD")
            Path(directory, "app.go").write_text("package app // changed\n")
            git("add", "."); git("commit", "-qm", "code")
            code = git("rev-parse", "HEAD")
            run = lambda start, end: subprocess.run([sys.executable, str(SCRIPT), start, end], cwd=directory,
                                                    check=True, capture_output=True, text=True).stdout
            self.assertEqual("product=false\n", run(base, docs))
            self.assertEqual("product=true\n", run(base, code))


if __name__ == "__main__":
    unittest.main()
