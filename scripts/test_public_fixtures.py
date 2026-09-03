#!/usr/bin/env python3
"""Verify repository-owned, redistributable emulator fixtures."""

from __future__ import annotations

import binascii
import hashlib
import json
import subprocess
import tarfile
import unittest
import zipfile
import zlib
from pathlib import Path
from xml.etree import ElementTree


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
FIXTURE_ROOT = REPOSITORY_ROOT / "testdata" / "public-roms" / "gba-smoke"
GBA_ROMS = {
    "gba-smoke.gba": (b"RETROM SMOKE", b"RTSM", "f86c63b35aea59190f5e1cf99f8f3d576c3646b26da02f3f826fde192a47239b"),
    "pegasus-smoke.gba": (b"RETROM PEGAS", b"RTPG", "6550cc49ddd91337c7c44bc827e2e9305b91c811ef6b032e1ee35fa5884a2e3a"),
    "emulationstation-smoke.gba": (b"RETROM ESTAT", b"RTES", "b2e50f15541e172933fd1f0d02355105233f5e36b55d121c07f39079f21347c5"),
}
EMULATIONSTATION_GAMELIST_SHA256 = "f58df21608d161b9d3d53bba57fa2744658d66ae603026265b01686a685db50c"
GBA_MEDIA = {
    "gba-smoke-cover.png": (
        20_746,
        "030146f84ab6b02269811286f2907b1cad59a67c281614ed0d864f94827865fb",
    ),
    "gba-smoke-video.webm": (
        770,
        "3b176271d963c9aacf5729d913e3b5d3ba13b87c57eaa936be8528f65f7cb939",
    ),
    "emulationstation-smoke-cover.png": (
        20_746,
        "0d72b89ed87fcf349a3422d7f3888183ce57a3fa757bc6baab0365a70f7ccc02",
    ),
    "emulationstation-smoke-video.webm": (
        767,
        "39a3044ce78c029049bda10b617724203bb91f4e2cb32ec5f15e3bdd45f6d10d",
    ),
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
RPGMAKER_FIXTURE_ROOT = REPOSITORY_ROOT / "testdata" / "public-roms" / "rpgmaker-smoke"
RPGMAKER_GENERATIONS = {
    "rpg2000": "RPG2000",
    "rpg2003": "RPG2003",
    "rpgxp": "RPGXP",
    "rpgvx": "RPGVX",
    "rpgvxace": "RPGVXACE",
    "rpgmv": "RPGMV",
    "malicious-rpgmv": "RPGMV",
    "malicious-rpgmz": "RPGMZ",
    "negative-matrix": "SECURITY_MATRIX",
}
MV_CORE_SHA256 = {
    "rpg_core.js": "cce476804212b28049fef8b5c117577d0e356eac4abe33804628931e1826dd41",
    "rpg_managers.js": "aef81821786b12a3c27f8db510089663f6c2aabb1b1708dd065f765f3d1f9c18",
    "rpg_objects.js": "d4278c2161193d9105787ee6b1ff5167c07ed23af4deef190b7705a326f1926e",
    "rpg_scenes.js": "b10b50dcb62793e42fd807fa3034fb82a89c6cb7ab32afafbb366d6d0a7f248a",
    "rpg_sprites.js": "3b434c9b81c4081d00dd9eca3900fe7564545c6b5a70e3b00cd263de4170a4c8",
    "rpg_windows.js": "81343a9b3fb1c957f1c6098776f8ca263aac8431f3996b647caab36d6a03c5f3",
}
MV_LIBRARY_SHA256 = {
    "fpsmeter.js": "fec43a13a522dafe9c28c3d30635a275af350edf3423de0349fb6fb9c01e9450",
    "iphone-inline-video.browser.js": "688ce9e9460d08399b898519b6d6811f8bd6722369e266b1f2761002be608f72",
    "lz-string.js": "7acc5ae524455fb67dee09375b4246386241f7dc4708dcdf8af0e78ca8267de7",
    "pixi.js": "47097d24b261679366419f9e36196a3303c35fa3d06d0518edb7f1ab5417def0",
    "pixi-picture.js": "f0e2af6190f2c53361047379ff0ae041568097f1b5beadcad28012f0aa5a99bb",
    "pixi-tilemap.js": "7401aeac40f9af7f7e777ce7a03a99c39571fa744fdb97add34732d7f8984e06",
}


def read_marshal_integer(payload: bytes, offset: int) -> tuple[int, int]:
    prefix = int.from_bytes(payload[offset : offset + 1], "little", signed=True)
    offset += 1
    if prefix == 0:
        return 0, offset
    if 5 < prefix < 128:
        return prefix - 5, offset
    if -129 < prefix < -5:
        return prefix + 5, offset
    size = abs(prefix)
    value = int.from_bytes(payload[offset : offset + size], "little", signed=prefix < 0)
    return value, offset + size


def decode_single_rgss_script(payload: bytes) -> tuple[bytes, bytes]:
    if not payload.startswith(b'\x04\x08['):
        raise AssertionError("RGSS fixture is not a Ruby Marshal 4.8 array")
    count, offset = read_marshal_integer(payload, 3)
    if count != 1 or payload[offset : offset + 1] != b"[":
        raise AssertionError("RGSS fixture must contain one script tuple")
    tuple_size, offset = read_marshal_integer(payload, offset + 1)
    if tuple_size != 3 or payload[offset : offset + 1] != b"i":
        raise AssertionError("RGSS fixture script tuple is invalid")
    script_id, offset = read_marshal_integer(payload, offset + 1)
    if script_id != 1 or payload[offset : offset + 1] != b'"':
        raise AssertionError("RGSS fixture script id/name is invalid")
    name_size, offset = read_marshal_integer(payload, offset + 1)
    name = payload[offset : offset + name_size]
    offset += name_size
    if payload[offset : offset + 1] != b'"':
        raise AssertionError("RGSS fixture compressed source is invalid")
    source_size, offset = read_marshal_integer(payload, offset + 1)
    compressed = payload[offset : offset + source_size]
    if offset + source_size != len(payload):
        raise AssertionError("RGSS fixture contains trailing Marshal data")
    return name, zlib.decompress(compressed)
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
    def test_rpgmaker_smoke_outputs_match_their_generator_and_manifest(self) -> None:
        subprocess.run(
            ["python3", str(RPGMAKER_FIXTURE_ROOT / "build.py"), "--check"],
            cwd=REPOSITORY_ROOT,
            check=True,
            capture_output=True,
            text=True,
        )
        manifest = json.loads(
            (RPGMAKER_FIXTURE_ROOT / "fixture-manifest.json").read_text(encoding="utf-8")
        )
        self.assertEqual(1, manifest["schemaVersion"])
        self.assertEqual("generator/*.go", manifest["generator"])
        declared: set[str] = set()
        observed_generations: set[str] = set()
        for entry in manifest["files"]:
            relative = entry["path"]
            self.assertNotIn(relative, declared)
            declared.add(relative)
            contents = (RPGMAKER_FIXTURE_ROOT / relative).read_bytes()
            self.assertEqual(len(contents), entry["sizeBytes"])
            self.assertEqual(hashlib.sha256(contents).hexdigest(), entry["sha256"])
            directory = relative.split("/", 1)[0]
            self.assertEqual(RPGMAKER_GENERATIONS[directory], entry["generation"])
            observed_generations.add(entry["generation"])
        actual = {
            path.relative_to(RPGMAKER_FIXTURE_ROOT).as_posix()
            for directory in RPGMAKER_GENERATIONS
            for path in (RPGMAKER_FIXTURE_ROOT / directory).rglob("*")
            if path.is_file()
        }
        self.assertEqual(actual, declared)
        self.assertEqual(set(RPGMAKER_GENERATIONS.values()), observed_generations)

    def test_rpgmaker_smoke_contains_no_vendor_runtime_or_executable(self) -> None:
        forbidden_suffixes = {
            ".exe", ".dll", ".so", ".dylib", ".node", ".bat", ".cmd", ".ps1",
        }
        allowed_opaque_native = {
            "malicious-rpgmz/Game.exe", "malicious-rpgmz/nw.dll",
            "malicious-rpgmz/plugin.node", "malicious-rpgmz/launcher.bat",
            "negative-matrix/referenced-native/plugin.node",
        }
        observed_opaque_native: set[str] = set()
        for directory in RPGMAKER_GENERATIONS:
            for path in (RPGMAKER_FIXTURE_ROOT / directory).rglob("*"):
                with self.subTest(path=path):
                    self.assertFalse(path.is_symlink())
                    if path.is_file() and path.suffix.lower() in forbidden_suffixes:
                        relative = path.relative_to(RPGMAKER_FIXTURE_ROOT).as_posix()
                        self.assertIn(relative, allowed_opaque_native)
                        self.assertTrue(path.read_bytes().startswith(b"RETROM OWNED"))
                        observed_opaque_native.add(relative)
        self.assertEqual(allowed_opaque_native, observed_opaque_native)
        archives = list((RPGMAKER_FIXTURE_ROOT / "vendor").glob("*.tar.gz"))
        self.assertEqual(1, len(archives), "only the locked MIT CoreScript source archive is allowed")

    def test_lcf_smoke_projects_have_generation_markers_and_owned_assets(self) -> None:
        for directory, marker, ldb_id_encoding in (
            ("rpg2000", b"RETROM RPG2000", b"\x0a\x01\x00"),
            ("rpg2003", b"RETROM RPG2003", b"\x0a\x02\x8f\x53"),
        ):
            root = RPGMAKER_FIXTURE_ROOT / directory
            with self.subTest(directory=directory):
                database = (root / "RPG_RT.ldb").read_bytes()
                map_tree = (root / "RPG_RT.lmt").read_bytes()
                game_map = (root / "Map0001.lmu").read_bytes()
                self.assertTrue(database.startswith(b"\x0bLcfDataBase"))
                self.assertTrue(map_tree.startswith(b"\x0aLcfMapTree"))
                self.assertTrue(game_map.startswith(b"\x0aLcfMapUnit"))
                self.assertIn(marker, map_tree)
                self.assertIn(marker, game_map)
                self.assertEqual(2, game_map.count(b"RETROM STATE CHANGED"))
                self.assertIn(b"retrom-tone", game_map)
                self.assertIn(b"retrom-marker", game_map)
                self.assertIn(ldb_id_encoding, database)
                for image_name in (
                    "ChipSet/retrom-chipset.png",
                    "CharSet/retrom-hero.png",
                    "Picture/retrom-marker.png",
                    "System/retrom-system.png",
                ):
                    image = (root / image_name).read_bytes()
                    self.assertEqual(b"\x89PNG\r\n\x1a\n", image[:8])
                    self.assertEqual(8, image[24], "LCF assets require 8-bit PNG samples")
                    self.assertEqual(3, image[25], "LCF assets must be indexed-color PNGs")
                self.assertEqual(b"RIFF", (root / "Sound/retrom-tone.wav").read_bytes()[:4])
                self.assertIn(b"FullPackageFlag=1", (root / "RPG_RT.ini").read_bytes())

    def test_rgss_smoke_archives_are_minimal_marshal_zlib_programs(self) -> None:
        archives = (
            ("rpgxp", "Data/Scripts.rxdata", b"RETROM RPGXP"),
            ("rpgvx", "Data/Scripts.rvdata", b"RETROM RPGVX"),
            ("rpgvxace", "Data/Scripts.rvdata2", b"RETROM RPGVXACE"),
        )
        for directory, relative, marker in archives:
            with self.subTest(directory=directory):
                payload = (RPGMAKER_FIXTURE_ROOT / directory / relative).read_bytes()
                name, source = decode_single_rgss_script(payload)
                self.assertEqual(b"Retrom Smoke", name)
                self.assertIn(marker, source)
                self.assertIn(b"$game_variables", source)
                self.assertIn(b"Input.repeat?", source)
                self.assertIn(b"Input.trigger?(Input::C)", source)
                self.assertNotIn(b"Scene_Title", source)

    def test_rgss_smoke_projects_have_an_unread_large_archive_member(self) -> None:
        relative = Path("Graphics/Unused/retrom-lazy-padding.bin")
        expected_size = 5 * 1024 * 1024
        for directory in ("rpgxp", "rpgvx", "rpgvxace"):
            with self.subTest(directory=directory):
                payload = (RPGMAKER_FIXTURE_ROOT / directory / relative).read_bytes()
                self.assertEqual(expected_size, len(payload))
                self.assertEqual({0}, set(payload))

    def test_mv_source_and_outputs_have_locked_identities_and_licenses(self) -> None:
        vendor_manifest = json.loads(
            (RPGMAKER_FIXTURE_ROOT / "vendor/manifest.json").read_text(encoding="utf-8")
        )
        archive = RPGMAKER_FIXTURE_ROOT / "vendor" / vendor_manifest["archive"]["path"]
        archive_bytes = archive.read_bytes()
        self.assertEqual(vendor_manifest["archive"]["size"], len(archive_bytes))
        self.assertEqual(vendor_manifest["archive"]["sha256"], hashlib.sha256(archive_bytes).hexdigest())
        with tarfile.open(archive, "r:gz") as source:
            prefix = f"corescript-{vendor_manifest['commit']}/"
            archive_license = source.extractfile(prefix + vendor_manifest["license"]["archivePath"])
            self.assertIsNotNone(archive_license)
            license_bytes = archive_license.read()
        self.assertEqual(vendor_manifest["license"]["archiveSize"], len(license_bytes))
        self.assertEqual(vendor_manifest["license"]["archiveSha256"], hashlib.sha256(license_bytes).hexdigest())
        self.assertEqual(license_bytes, (RPGMAKER_FIXTURE_ROOT / "LICENSES/CoreScript-MIT.txt").read_bytes())
        self.assertEqual(
            {"CoreScript", "FPSMeter 0.3.1", "iphone-inline-video", "LZ-String", "PixiJS 4.5.4", "pixi-picture", "pixi-tilemap"},
            {component["name"] for component in vendor_manifest["components"]},
        )
        for name, expected in {**MV_CORE_SHA256, **MV_LIBRARY_SHA256}.items():
            subdirectory = "libs" if name in MV_LIBRARY_SHA256 else ""
            contents = (RPGMAKER_FIXTURE_ROOT / "rpgmv/js" / subdirectory / name).read_bytes()
            self.assertEqual(expected, hashlib.sha256(contents).hexdigest())

    def test_mv_smoke_uses_standard_data_manager_map_and_fixture_state(self) -> None:
        root = RPGMAKER_FIXTURE_ROOT / "rpgmv"
        system = json.loads((root / "data/System.json").read_text(encoding="utf-8"))
        game_map = json.loads((root / "data/Map001.json").read_text(encoding="utf-8"))
        self.assertEqual("RETROM RPGMV", system["gameTitle"])
        self.assertEqual((1, 10, 8), (system["startMapId"], system["startX"], system["startY"]))
        self.assertEqual("Retrom fixture state", system["variables"][1])
        self.assertEqual((20, 15, 20 * 15 * 6), (game_map["width"], game_map["height"], len(game_map["data"])))
        self.assertEqual([None], game_map["events"])
        plugin = (root / "js/plugins/RetromSmoke.js").read_text(encoding="utf-8")
        self.assertIn("DataManager.setupNewGame()", plugin)
        self.assertIn('$gameVariables.setValue(1,', plugin)
        self.assertIn("new WebAudio(toneUrl)", plugin)
        index = (root / "index.html").read_text(encoding="utf-8")
        for name in (*MV_LIBRARY_SHA256, *MV_CORE_SHA256, "plugins.js", "main.js"):
            self.assertIn(name, index)

    def test_rpgmaker_security_matrix_is_complete_and_byte_locked(self) -> None:
        matrix = json.loads(
            (RPGMAKER_FIXTURE_ROOT / "negative-matrix/matrix.json").read_text(encoding="utf-8")
        )
        self.assertEqual(1, matrix["schemaVersion"])
        cores = {
            "rpgmaker_2000", "rpgmaker_2003", "rpgmaker_xp", "rpgmaker_vx",
            "rpgmaker_vx_ace", "rpgmaker_mv", "rpgmaker_mz",
        }
        wrong_pairs = {
            (source["coreId"], target["coreId"])
            for source in matrix["wrongCore"]
            for target in source["targets"]
        }
        self.assertEqual(42, len(wrong_pairs))
        self.assertEqual({(source, target) for source in cores for target in cores if source != target}, wrong_pairs)
        accepted_wrong_core = [
            (source["generation"], target)
            for source in matrix["wrongCore"]
            for target in source["targets"]
            if target["accepted"]
        ]
        self.assertEqual(
            [],
            accepted_wrong_core,
        )
        unsafe_names = {case["name"] for case in matrix["unsafe"]}
        self.assertEqual(
            {
                "dual-root", "multi-generation", "rgss-conflict", "lcf-truncated",
                "case-collision", "nfkc-collision", "gencache-collision", "traversal", "symlink", "bomb",
                "external", "referenced-native", "opaque-native",
            },
            unsafe_names,
        )
        self.assertEqual(
            {"RPG2000", "RPG2003", "RPGXP", "RPGVX", "RPGVXACE", "RPGMV", "RPGMZ"},
            {case["generation"] for case in matrix["nestedArchives"]},
        )
        self.assertEqual(70, len(matrix["nestedArchives"]))
        self.assertEqual({"ZIP", "7Z", "RAR", "TAR", "GZIP"}, {case["format"] for case in matrix["nestedArchives"]})
        self.assertEqual({"extension", "magic"}, {case["detection"] for case in matrix["nestedArchives"]})
        archive_root = RPGMAKER_FIXTURE_ROOT / "negative-matrix/archives"
        with zipfile.ZipFile(archive_root / "traversal.zip") as archive:
            self.assertEqual(["../index.html"], archive.namelist())
        with zipfile.ZipFile(archive_root / "symlink.zip") as archive:
            mode = archive.infolist()[0].external_attr >> 16
            self.assertEqual(0o120000, mode & 0o170000)
        with zipfile.ZipFile(archive_root / "bomb.zip") as archive:
            entry = archive.infolist()[0]
            self.assertEqual(17 << 20, entry.file_size)
            self.assertGreater(entry.file_size, entry.compress_size * 100)

    def test_malicious_mv_mz_harnesses_are_project_owned_runtime_shapes(self) -> None:
        for directory, engine, core in (
            ("malicious-rpgmv", "MV", "rpg_core.js"),
            ("malicious-rpgmz", "MZ", "rmmz_core.js"),
        ):
            root = RPGMAKER_FIXTURE_ROOT / directory
            with self.subTest(directory=directory):
                core_source = (root / "js" / core).read_text(encoding="utf-8")
                runtime = (root / "js/main.js").read_text(encoding="utf-8")
                self.assertIn(f'RPGMAKER_NAME: "{engine}"', core_source)
                self.assertIn("__RETROM_MALICIOUS_RESULTS__", runtime)
                for probe in (
                    "parentDom", "appCookie", "topNavigation", "popup", "form",
                    "externalFetch", "serviceWorker", "nonAllowlistApi",
                ):
                    self.assertIn(f'\"{probe}\"', runtime)
                self.assertIn("DataManager", runtime)
                self.assertIn("saveGame", runtime)
                self.assertIn("loadGame", runtime)
                self.assertIn("requestAnimationFrame", runtime)
                if engine == "MZ":
                    self.assertIn("global.JsonEx", runtime)
                    self.assertIn("global.Graphics", runtime)
                    self.assertIn("global.ColorManager", runtime)
                    self.assertIn("stringify: function", runtime)
                    self.assertIn("parse: function", runtime)

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

    def test_emulationstation_gamelist_locks_its_rom_and_media(self) -> None:
        gamelist = (FIXTURE_ROOT / "gamelist.xml").read_bytes()
        self.assertEqual(478, len(gamelist))
        self.assertEqual(
            EMULATIONSTATION_GAMELIST_SHA256,
            hashlib.sha256(gamelist).hexdigest(),
        )
        root = ElementTree.fromstring(gamelist)
        self.assertEqual("gameList", root.tag)
        games = root.findall("game")
        self.assertEqual(1, len(games))
        declared_path = games[0].findtext("path")
        self.assertEqual("./emulationstation-smoke.gba", declared_path)
        self.assertEqual(
            "EmulationStation GBA Smoke",
            games[0].findtext("name"),
        )
        self.assertEqual(
            "./emulationstation-smoke-cover.png",
            games[0].findtext("image"),
        )
        self.assertEqual(
            "./emulationstation-smoke-video.webm",
            games[0].findtext("video"),
        )
        referenced_rom = FIXTURE_ROOT / declared_path.removeprefix("./")
        self.assertTrue(referenced_rom.is_file())
        self.assertEqual(
            GBA_ROMS["emulationstation-smoke.gba"][2],
            hashlib.sha256(referenced_rom.read_bytes()).hexdigest(),
        )

    def test_gba_media_have_locked_project_owned_bytes(self) -> None:
        for name, (expected_size, expected_sha256) in GBA_MEDIA.items():
            with self.subTest(name=name):
                payload = (FIXTURE_ROOT / name).read_bytes()
                self.assertEqual(expected_size, len(payload))
                self.assertEqual(expected_sha256, hashlib.sha256(payload).hexdigest())
        for name in ("gba-smoke-cover.png", "emulationstation-smoke-cover.png"):
            with self.subTest(name=name):
                cover = (FIXTURE_ROOT / name).read_bytes()
                self.assertEqual(b"\x89PNG\r\n\x1a\n", cover[:8])
                self.assertEqual(
                    (70, 98),
                    (
                        int.from_bytes(cover[16:20], "big"),
                        int.from_bytes(cover[20:24], "big"),
                    ),
                )
        for name in ("gba-smoke-video.webm", "emulationstation-smoke-video.webm"):
            with self.subTest(name=name):
                video = (FIXTURE_ROOT / name).read_bytes()
                self.assertEqual(b"\x1a\x45\xdf\xa3", video[:4])
                self.assertIn(b"webm", video[:128])
        self.assertNotEqual(
            (FIXTURE_ROOT / "gba-smoke-cover.png").read_bytes(),
            (FIXTURE_ROOT / "emulationstation-smoke-cover.png").read_bytes(),
        )
        self.assertNotEqual(
            (FIXTURE_ROOT / "gba-smoke-video.webm").read_bytes(),
            (FIXTURE_ROOT / "emulationstation-smoke-video.webm").read_bytes(),
        )

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

    def test_public_rom_payloads_are_excluded_from_backend_image_context(self) -> None:
        dockerignore = (REPOSITORY_ROOT / ".dockerignore").read_text(
            encoding="utf-8"
        ).splitlines()
        self.assertIn("testdata", dockerignore)
        self.assertFalse(any(line.startswith("!testdata") for line in dockerignore))


if __name__ == "__main__":
    unittest.main()
