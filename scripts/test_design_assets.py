#!/usr/bin/env python3
"""Enforce the repository boundary for locally generated design captures."""

from __future__ import annotations

import re
import subprocess
import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
DESIGN_ROOT = REPOSITORY_ROOT / "docs" / "design"
README_ASSET_ROOT = REPOSITORY_ROOT / "docs" / "readme-assets"
README_IMAGE_NAMES = (
    "home-4k-150.png",
    "player-4k-150.png",
)
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

    def test_readme_images_are_the_declared_physical_4k_captures(self) -> None:
        readme = (REPOSITORY_ROOT / "README.md").read_text(encoding="utf-8")
        present_images = sorted(
            path.name
            for path in README_ASSET_ROOT.iterdir()
            if path.is_file() and path.suffix.lower() in IMAGE_SUFFIXES
        )
        self.assertEqual(sorted(README_IMAGE_NAMES), present_images)

        for name in README_IMAGE_NAMES:
            path = README_ASSET_ROOT / name
            self.assertIn(f"docs/readme-assets/{name}", readme)
            with path.open("rb") as image:
                header = image.read(24)
            self.assertEqual(b"\x89PNG\r\n\x1a\n", header[:8], path)
            self.assertEqual((3840).to_bytes(4, "big"), header[16:20], path)
            self.assertEqual((2160).to_bytes(4, "big"), header[20:24], path)


if __name__ == "__main__":
    unittest.main()
