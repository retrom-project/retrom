#!/usr/bin/env python3
"""Verify repository-owned, redistributable emulator fixtures."""

from __future__ import annotations

import binascii
import hashlib
import json
import subprocess
import unittest
import zipfile
from pathlib import Path
from xml.etree import ElementTree


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
FIXTURE_ROOT = REPOSITORY_ROOT / "testdata" / "public-roms" / "gba-smoke"
GBA_ROMS = {
    "gba-smoke.gba": (b"RETROM SMOKE", b"RTSM", "f86c63b35aea59190f5e1cf99f8f3d576c3646b26da02f3f826fde192a47239b"),
    "pegasus-smoke.gba": (b"RETROM PEGAS", b"RTPG", "6550cc49ddd91337c7c44bc827e2e9305b91c811ef6b032e1ee35fa5884a2e3a"),
}
ARCADE_FIXTURE_ROOT = REPOSITORY_ROOT / "testdata" / "public-roms" / "arcade-smoke"
NES_FIXTURE_ROOT = REPOSITORY_ROOT / "testdata" / "public-roms" / "nes-smoke"
NES_ROMS = {
    "nes-smoke.nes": (
        b"RETROM PUBLIC NES NETPLAY SMOKE - MIT",
        "6b5224f3227879472e19e4d419008d77e69296140205771fd2df8370f18a01f8",
    ),
    "nestopia-smoke.nes": (
        b"RETROM PUBLIC NESTOPIA NETPLAY SMOKE - MIT",
        "ab4adf02261946fbb80bb8a2141908589fd6cd7a32408875d7541eb94efc61ff",
    ),
}
SNES_FIXTURE_ROOT = REPOSITORY_ROOT / "testdata" / "public-roms" / "snes-smoke"
SNES_SHA256 = "408574e6a6b7db1273e21142789bc50e5a1acb529bcf61c059cced5cfe1082db"
ARCADE_OUTPUTS = {
    "pacman.zip": (16782, "eb7575bf12b9616874aa8672f4bc8fa9f90142f94ba8683d8d18dce928989611"),
    "puckman.zip": (9578, "a7ca86aecb425661a66a4faee139ff386c906e513c988f583f5b9bae71073b34"),
    "retrombios.zip": (396, "36c08c0777cad0d3bc9a1824072f611b0ec3def41e17e363e68cbf62d41add01"),
    "mame2003-smoke.xml": (3043, "746f0828479bd8749596c0f57af43e5f46afca215f0bb3e005d53a2adb2994c8"),
    "mame2003_plus/pacman.zip": (16813, "b90e480240cd3803e504d5e42147f16d1afda60f63ffe814bbb28824f56cdb3c"),
    "mame2003_plus/puckman.zip": (9609, "64f1d998794885513b604e7229389838e958e055904c44989386dbbc2edeeeb9"),
    "mame2003_plus/retrombios.zip": (427, "95f45fb7d73fca423094d80c861387e735f968198d77ca081d397406a25810ce"),
    "mame2003_plus/mame2003-plus-smoke.xml": (3043, "746f0828479bd8749596c0f57af43e5f46afca215f0bb3e005d53a2adb2994c8"),
    "fbneo/pacman.zip": (25162, "4af28131b7621391e3de9c009e075d960c4f7126c8d93f016206c1c0237dd271"),
    "fbneo/puckman.zip": (1190, "7dd4c47b9c0832c8f43d72817c189a5b5081ec7dee50fe4423f3c281c8e33f6e"),
    "fbneo/retrombios.zip": (396, "36c08c0777cad0d3bc9a1824072f611b0ec3def41e17e363e68cbf62d41add01"),
    "fbneo/fbneo-smoke.dat": (2403, "f460da0fd6d2f2613df3838dad956df05f453d023db04b112eba44ff4121341a"),
    "fbalpha2012_cps1/1941.zip": (3477121, "5f60848ea1ac623907c226f8f81a45f573384d4463012dd15d50894e51a2af23"),
    "fbalpha2012_cps1/fbalpha2012-cps1-smoke.dat": (3959, "9d1dfba059d6e9f5429dbe982c6a13ae5ba4b7ddbea053279392fd3da637d205"),
    "fbalpha2012_cps2/spf2xjd.zip": (9700248, "889639182cb4d102cedaabf79ae8435e75e8f62f377fd05c7ad90237a24ea396"),
    "fbalpha2012_cps2/spf2t.zip": (209, "c1bd398cd8ca628c3cb7394a8b7e1bb06422ff509d4ee67c162119ace51a4fec"),
    "fbalpha2012_cps2/fbalpha2012-cps2-smoke.dat": (2582, "121e3e16c7a604448392b5086f2c28293b98c830b6face5a00d778d311b439c4"),
}
FBNEO_DRIVER_CRC32 = {
    "pacman.6e": "c1e6ab10",
    "pacman.6f": "1a6fb2d4",
    "pacman.6h": "bcdd1beb",
    "pacman.6j": "817d94e3",
    "pacman.5e": "0c944964",
    "pacman.5f": "958fedf9",
    "pm1-1.7f": "2fc650bd",
    "pm1-4.4a": "3eb3a8e4",
    "pm1-3.1m": "a9cc86bf",
    "pm1-2.3m": "77245b66",
}
FBNEO_MERGES = {
    "82s123.7f": "pm1-1.7f",
    "82s126.4a": "pm1-4.4a",
    "82s126.1m": "pm1-3.1m",
    "82s126.3m": "pm1-2.3m",
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

    def test_gba_smoke_roms_have_locked_identities_and_valid_headers(self) -> None:
        identities: set[str] = set()
        for name, (title, product_code, expected_sha256) in GBA_ROMS.items():
            with self.subTest(name=name):
                image = (FIXTURE_ROOT / name).read_bytes()
                digest = hashlib.sha256(image).hexdigest()
                self.assertEqual(1024, len(image))
                self.assertEqual(expected_sha256, digest)
                self.assertEqual(title, image[0xA0:0xAC])
                self.assertEqual(product_code, image[0xAC:0xB0])
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
                identities.add(digest)
        self.assertEqual(len(GBA_ROMS), len(identities))

    def test_arcade_smoke_outputs_match_their_generator(self) -> None:
        subprocess.run(
            ["python3", str(ARCADE_FIXTURE_ROOT / "build.py"), "--check"],
            cwd=REPOSITORY_ROOT,
            check=True,
            capture_output=True,
            text=True,
        )

    def test_nes_netplay_smoke_is_a_locked_mapper_zero_rom(self) -> None:
        subprocess.run(
            ["python3", str(NES_FIXTURE_ROOT / "build.py"), "--check"],
            cwd=REPOSITORY_ROOT,
            check=True,
            capture_output=True,
            text=True,
        )
        identities: set[str] = set()
        for name, (marker, expected_sha256) in NES_ROMS.items():
            with self.subTest(name=name):
                image = (NES_FIXTURE_ROOT / name).read_bytes()
                digest = hashlib.sha256(image).hexdigest()
                self.assertEqual(24_592, len(image))
                self.assertEqual(expected_sha256, digest)
                self.assertEqual(b"NES\x1a\x01\x01\x00\x00" + bytes(8), image[:16])
                self.assertIn(marker, image)
                vectors = image[16 + 0x3FFA : 16 + 0x4000]
                self.assertEqual(0x8000, int.from_bytes(vectors[2:4], "little"))
                self.assertTrue(0x8000 <= int.from_bytes(vectors[0:2], "little") < 0xC000)
                self.assertIn(bytes((0xAD, 0x16, 0x40)), image[16 : 16 + 0x4000])
                self.assertIn(bytes((0xAD, 0x17, 0x40)), image[16 : 16 + 0x4000])
                identities.add(digest)
        self.assertEqual(len(NES_ROMS), len(identities))

    def test_snes_netplay_smoke_is_a_locked_lorom(self) -> None:
        subprocess.run(
            ["python3", str(SNES_FIXTURE_ROOT / "build.py"), "--check"],
            cwd=REPOSITORY_ROOT,
            check=True,
            capture_output=True,
            text=True,
        )
        image = (SNES_FIXTURE_ROOT / "snes-smoke.sfc").read_bytes()
        self.assertEqual(32_768, len(image))
        self.assertEqual(SNES_SHA256, hashlib.sha256(image).hexdigest())
        self.assertEqual(b"RETROM SNES SMOKE".ljust(21), image[0x7FC0:0x7FD5])
        complement = int.from_bytes(image[0x7FDC:0x7FDE], "little")
        checksum = int.from_bytes(image[0x7FDE:0x7FE0], "little")
        self.assertEqual(0xFFFF, checksum ^ complement)
        self.assertEqual(checksum, sum(image) & 0xFFFF)
        self.assertEqual(0x8000, int.from_bytes(image[0x7FFC:0x7FFE], "little"))
        self.assertIn(b"RETROM PUBLIC SNES NETPLAY SMOKE", image)

    def test_arcade_smoke_outputs_have_locked_identities(self) -> None:
        for name, (expected_size, expected_sha256) in ARCADE_OUTPUTS.items():
            with self.subTest(name=name):
                content = (ARCADE_FIXTURE_ROOT / name).read_bytes()
                self.assertEqual(expected_size, len(content))
                self.assertEqual(expected_sha256, hashlib.sha256(content).hexdigest())

    def test_mame2003_plus_archives_have_distinct_containers_and_identical_members(self) -> None:
        for archive_name in ("pacman.zip", "puckman.zip", "retrombios.zip"):
            with self.subTest(archive=archive_name):
                base_path = ARCADE_FIXTURE_ROOT / archive_name
                plus_path = ARCADE_FIXTURE_ROOT / "mame2003_plus" / archive_name
                self.assertNotEqual(base_path.read_bytes(), plus_path.read_bytes())
                with zipfile.ZipFile(base_path) as base_archive, zipfile.ZipFile(plus_path) as plus_archive:
                    self.assertEqual(base_archive.namelist(), plus_archive.namelist())
                    self.assertEqual(
                        {name: base_archive.read(name) for name in base_archive.namelist()},
                        {name: plus_archive.read(name) for name in plus_archive.namelist()},
                    )

    def test_cps_archives_exactly_match_layout_and_test_dat(self) -> None:
        layouts = json.loads(
            (ARCADE_FIXTURE_ROOT / "driver-layouts.json").read_text(encoding="utf-8")
        )["drivers"]
        paths = {
            "1941": ("fbalpha2012_cps1/1941.zip", "fbalpha2012_cps1/fbalpha2012-cps1-smoke.dat"),
            "spf2xjd": ("fbalpha2012_cps2/spf2xjd.zip", "fbalpha2012_cps2/fbalpha2012-cps2-smoke.dat"),
        }
        for driver_name, (archive_name, dat_name) in paths.items():
            with self.subTest(driver=driver_name):
                with zipfile.ZipFile(ARCADE_FIXTURE_ROOT / archive_name) as archive:
                    contents = {name: archive.read(name) for name in archive.namelist()}
                specifications = layouts[driver_name]["entries"]
                self.assertEqual([item["name"] for item in specifications], list(contents))
                root = ElementTree.parse(ARCADE_FIXTURE_ROOT / dat_name).getroot()
                machines = root.findall("game")
                indexed_machines = {machine.attrib["name"]: machine for machine in machines}
                if driver_name == "spf2xjd":
                    self.assertEqual({"spf2t", "spf2xjd"}, set(indexed_machines))
                    child = indexed_machines[driver_name]
                    self.assertEqual("spf2t", child.attrib["cloneof"])
                    self.assertEqual("spf2t", child.attrib["romof"])
                    with zipfile.ZipFile(
                        ARCADE_FIXTURE_ROOT / "fbalpha2012_cps2/spf2t.zip"
                    ) as parent_archive:
                        self.assertEqual(["retrom-parent.marker"], parent_archive.namelist())
                        parent_entry = indexed_machines["spf2t"].find("rom")
                        self.assertIsNotNone(parent_entry)
                        self.assertEqual("retrom-parent.marker", parent_entry.attrib["name"])
                else:
                    self.assertEqual({driver_name}, set(indexed_machines))
                    child = indexed_machines[driver_name]
                    self.assertNotIn("cloneof", child.attrib)
                    self.assertNotIn("romof", child.attrib)
                dat_entries = {entry.attrib["name"]: entry for entry in child.findall("rom")}
                for specification in specifications:
                    content = contents[specification["name"]]
                    dat_entry = dat_entries[specification["name"]]
                    self.assertEqual(specification["size"], len(content))
                    self.assertEqual(
                        specification["crc32"],
                        f"{binascii.crc32(content) & 0xFFFFFFFF:08x}",
                    )
                    self.assertEqual(
                        hashlib.sha1(content, usedforsecurity=False).hexdigest(),
                        dat_entry.attrib["sha1"],
                    )
                    self.assertEqual(hashlib.sha256(content).hexdigest(), dat_entry.attrib["sha256"])

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
        program = b"".join(
            archives["pacman"][name]
            for name in ("pacman.6e", "pacman.6f", "pacman.6h", "pacman.6j")
        )
        self.assertIn(bytes((0x3A, 0x00, 0x50)), program)
        self.assertIn(bytes((0x3A, 0x40, 0x50)), program)
        self.assertTrue(archives["retrombios"]["retrom-test-bios.bin"].startswith(b"RETROM TEST BIOS CONTRACT V1"))

    def test_fbneo_smoke_dat_locks_driver_crc_child_parent_and_bios(self) -> None:
        fixture_root = ARCADE_FIXTURE_ROOT / "fbneo"
        archives: dict[str, dict[str, bytes]] = {}
        for machine in ("pacman", "puckman", "retrombios"):
            with zipfile.ZipFile(fixture_root / f"{machine}.zip") as archive:
                archives[machine] = {
                    name: archive.read(name) for name in archive.namelist()
                }

        root = ElementTree.parse(fixture_root / "fbneo-smoke.dat").getroot()
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

        actual_driver_crc32 = {
            name: f"{binascii.crc32(content) & 0xFFFFFFFF:08x}"
            for archive_name in ("pacman", "puckman")
            for name, content in archives[archive_name].items()
        }
        self.assertEqual(FBNEO_DRIVER_CRC32, actual_driver_crc32)
        merge_entries = {
            entry.attrib["name"]: entry
            for entry in machines["pacman"].findall("rom")
            if "merge" in entry.attrib
        }
        self.assertEqual(FBNEO_MERGES, {
            name: entry.attrib["merge"] for name, entry in merge_entries.items()
        })
        for child_name, parent_name in FBNEO_MERGES.items():
            with self.subTest(merge=child_name):
                content = archives["puckman"][parent_name]
                self.assertEqual(str(len(content)), merge_entries[child_name].attrib["size"])
                self.assertEqual(
                    f"{binascii.crc32(content) & 0xFFFFFFFF:08x}",
                    merge_entries[child_name].attrib["crc"],
                )
                self.assertEqual(
                    hashlib.sha1(content, usedforsecurity=False).hexdigest(),
                    merge_entries[child_name].attrib["sha1"],
                )

        self.assertIn(
            b"RETROM PUBLIC ARCADE SMOKE - ORIGINAL MIT-LICENSED Z80 PROGRAM",
            archives["pacman"]["pacman.6j"],
        )
        self.assertTrue(
            archives["retrombios"]["retrom-test-bios.bin"].startswith(
                b"RETROM TEST BIOS CONTRACT V1"
            )
        )

    def test_public_roms_are_excluded_from_backend_image_context(self) -> None:
        dockerignore = (REPOSITORY_ROOT / ".dockerignore").read_text(
            encoding="utf-8"
        ).splitlines()
        self.assertIn("testdata/public-roms", dockerignore)


if __name__ == "__main__":
    unittest.main()
