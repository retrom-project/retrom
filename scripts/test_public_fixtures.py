#!/usr/bin/env python3
"""Verify repository-owned, redistributable emulator fixtures."""

from __future__ import annotations

import binascii
import hashlib
import subprocess
import unittest
import zipfile
from pathlib import Path
from xml.etree import ElementTree


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
FIXTURE_ROOT = REPOSITORY_ROOT / "testdata" / "public-roms" / "gba-smoke"
ROM = FIXTURE_ROOT / "gba-smoke.gba"
EXPECTED_SHA256 = "f86c63b35aea59190f5e1cf99f8f3d576c3646b26da02f3f826fde192a47239b"
ARCADE_FIXTURE_ROOT = REPOSITORY_ROOT / "testdata" / "public-roms" / "arcade-smoke"
ARCADE_OUTPUTS = {
    "pacman.zip": (16782, "9364e180d8ec43d8efd530afea30399f7a974eeec78266dcaecc04f3524c68d7"),
    "puckman.zip": (9578, "a7ca86aecb425661a66a4faee139ff386c906e513c988f583f5b9bae71073b34"),
    "retrombios.zip": (396, "36c08c0777cad0d3bc9a1824072f611b0ec3def41e17e363e68cbf62d41add01"),
    "mame2003-smoke.xml": (3043, "e7838a8e2ab2b8e7e5f451585f6c53f32e1d66f412d425998a6268827327c5be"),
}


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

    def test_arcade_smoke_outputs_match_their_generator(self) -> None:
        subprocess.run(
            ["python3", str(ARCADE_FIXTURE_ROOT / "build.py"), "--check"],
            cwd=REPOSITORY_ROOT,
            check=True,
            capture_output=True,
            text=True,
        )

    def test_arcade_smoke_outputs_have_locked_identities(self) -> None:
        for name, (expected_size, expected_sha256) in ARCADE_OUTPUTS.items():
            with self.subTest(name=name):
                content = (ARCADE_FIXTURE_ROOT / name).read_bytes()
                self.assertEqual(expected_size, len(content))
                self.assertEqual(expected_sha256, hashlib.sha256(content).hexdigest())

    def test_arcade_smoke_dat_locks_child_parent_and_bios_archives(self) -> None:
        archives: dict[str, dict[str, bytes]] = {}
        for machine in ("pacman", "puckman", "retrombios"):
            with zipfile.ZipFile(ARCADE_FIXTURE_ROOT / f"{machine}.zip") as archive:
                archives[machine] = {name: archive.read(name) for name in archive.namelist()}

        root = ElementTree.parse(ARCADE_FIXTURE_ROOT / "mame2003-smoke.xml").getroot()
        machines = {machine.attrib["name"]: machine for machine in root.findall("game")}
        self.assertEqual({"pacman", "puckman", "retrombios"}, set(machines))
        self.assertEqual("puckman", machines["pacman"].attrib["cloneof"])
        self.assertEqual("retrombios", machines["pacman"].attrib["romof"])
        self.assertEqual("retrombios", machines["puckman"].attrib["romof"])
        self.assertEqual("yes", machines["retrombios"].attrib["isbios"])

        for machine_name, archive_entries in archives.items():
            dat_entries = {
                entry.attrib["name"]: entry
                for entry in machines[machine_name].findall("rom")
                if "merge" not in entry.attrib
            }
            self.assertEqual(set(archive_entries), set(dat_entries))
            for name, content in archive_entries.items():
                with self.subTest(machine=machine_name, name=name):
                    self.assertEqual(str(len(content)), dat_entries[name].attrib["size"])
                    self.assertEqual(
                        f"{binascii.crc32(content) & 0xFFFFFFFF:08x}",
                        dat_entries[name].attrib["crc"],
                    )
                    self.assertEqual(
                        hashlib.sha1(content, usedforsecurity=False).hexdigest(),
                        dat_entries[name].attrib["sha1"],
                    )

        self.assertIn(
            b"RETROM PUBLIC ARCADE SMOKE - ORIGINAL MIT-LICENSED Z80 PROGRAM",
            archives["pacman"]["pacman.6j"],
        )
        self.assertTrue(archives["retrombios"]["retrom-test-bios.bin"].startswith(b"RETROM TEST BIOS CONTRACT V1"))

    def test_public_roms_are_excluded_from_backend_image_context(self) -> None:
        dockerignore = (REPOSITORY_ROOT / ".dockerignore").read_text(
            encoding="utf-8"
        ).splitlines()
        self.assertIn("testdata/public-roms", dockerignore)


if __name__ == "__main__":
    unittest.main()
