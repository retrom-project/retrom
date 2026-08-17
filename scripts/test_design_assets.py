#!/usr/bin/env python3
"""Enforce the repository boundary for locally generated design captures."""

from __future__ import annotations

import re
import subprocess
import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
DESIGN_ROOT = REPOSITORY_ROOT / "docs" / "design"
IMAGE_SUFFIXES = (
    ".png",
    ".jpg",
    ".jpeg",
    ".webp",
    ".gif",
    ".avif",
    ".bmp",
    ".tif",
    ".tiff",
    ".ico",
    ".svg",
)
DESIGN_IMAGE_REFERENCE = re.compile(
    r"(?:docs/)?design/[^\s)\]>'\"]+\.(?:png|jpe?g|webp|gif|avif|bmp|tiff?|ico|svg)"
    r"|retrom-ui-[A-Za-z0-9._-]+\.(?:png|jpe?g|webp|gif|avif|bmp|tiff?|ico|svg)",
    re.IGNORECASE,
)


class DesignAssetBoundaryTests(unittest.TestCase):
    def test_design_images_are_not_present_as_tracked_files(self) -> None:
        tracked = subprocess.run(
            ["git", "ls-files", "docs/design"],
            cwd=REPOSITORY_ROOT,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.splitlines()
        present_images = [
            path
            for path in tracked
            if Path(path).suffix.lower() in IMAGE_SUFFIXES
            and (REPOSITORY_ROOT / path).is_file()
        ]
        self.assertEqual([], present_images)

    def test_design_gitignore_covers_common_image_formats(self) -> None:
        for suffix in IMAGE_SUFFIXES:
            for variant in (suffix, suffix.upper()):
                candidate = DESIGN_ROOT / f"ignore-check{variant}"
                result = subprocess.run(
                    ["git", "check-ignore", "--no-index", "--quiet", str(candidate)],
                    cwd=REPOSITORY_ROOT,
                    check=False,
                )
                self.assertEqual(0, result.returncode, candidate)

    def test_documents_do_not_reference_local_design_images(self) -> None:
        references: list[str] = []
        documents = [REPOSITORY_ROOT / "AGENTS.md"]
        documents.extend((REPOSITORY_ROOT / "docs").rglob("*.md"))
        documents.extend((REPOSITORY_ROOT / "docs").rglob("*.html"))
        for document in documents:
            for line_number, line in enumerate(
                document.read_text(encoding="utf-8").splitlines(), start=1
            ):
                if DESIGN_IMAGE_REFERENCE.search(line):
                    references.append(
                        f"{document.relative_to(REPOSITORY_ROOT)}:{line_number}"
                    )
        self.assertEqual([], references)


if __name__ == "__main__":
    unittest.main()
