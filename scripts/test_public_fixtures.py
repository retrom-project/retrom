#!/usr/bin/env python3
"""Verify repository-owned, redistributable emulator fixtures."""

from __future__ import annotations

import hashlib
import subprocess
import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
FIXTURE_ROOT = REPOSITORY_ROOT / "testdata" / "public-roms" / "gba-smoke"
ROM = FIXTURE_ROOT / "gba-smoke.gba"
EXPECTED_SHA256 = "f86c63b35aea59190f5e1cf99f8f3d576c3646b26da02f3f826fde192a47239b"


class PublicFixtureTests(unittest.TestCase):
    def test_gba_smoke_rom_matches_its_generator(self) -> None:
        subprocess.run(
            ["python3", str(FIXTURE_ROOT / "build.py"), "--check"],
            cwd=REPOSITORY_ROOT,
            check=True,
            capture_output=True,
            text=True,
        )

    def test_gba_smoke_rom_has_locked_identity_and_valid_header(self) -> None:
        image = ROM.read_bytes()
        self.assertEqual(1024, len(image))
        self.assertEqual(EXPECTED_SHA256, hashlib.sha256(image).hexdigest())
        self.assertEqual(b"RETROM SMOKE", image[0xA0:0xAC])
        self.assertEqual(b"RTSM", image[0xAC:0xB0])
        self.assertEqual(0x96, image[0xB2])
        self.assertEqual(
            0,
            (0x19 + sum(image[0xA0:0xBE])) & 0xFF,
            "GBA header complement checksum drifted",
        )
        self.assertEqual(
            bytes(156),
            image[0x04:0xA0],
            "the public ROM must not embed a third-party vendor logo",
        )

    def test_public_roms_are_excluded_from_backend_image_context(self) -> None:
        dockerignore = (REPOSITORY_ROOT / ".dockerignore").read_text(
            encoding="utf-8"
        ).splitlines()
        self.assertIn("testdata/public-roms", dockerignore)


if __name__ == "__main__":
    unittest.main()
