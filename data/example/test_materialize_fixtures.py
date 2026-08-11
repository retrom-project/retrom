#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import importlib.util
import tempfile
import unittest
import zipfile
from pathlib import Path


SCRIPT = Path(__file__).with_name("materialize-fixtures.py")
SPEC = importlib.util.spec_from_file_location("retrom_fixtures", SCRIPT)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("unable to load fixture materializer")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class FixtureMaterializationTests(unittest.TestCase):
    def test_materializes_a_single_member_archive_with_fixed_hashes(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source_root = root / "source"
            repo_root = root / "repo"
            source_root.mkdir()
            payload = b"fixture-rom"
            archive_path = source_root / "fixture.zip"
            with zipfile.ZipFile(archive_path, "w") as archive:
                archive.writestr("game.rom", payload)
            fixture = {
                "core": "fixture",
                "game": {
                    "localPath": "data/example/local-fixtures/fixture/game.rom",
                    "sourceRelativePath": "fixture.zip",
                    "sourceArchiveLocalPath": "data/example/local-fixtures/fixture/source.zip",
                    "sourceArchiveSize": archive_path.stat().st_size,
                    "sourceArchiveSha256": hashlib.sha256(archive_path.read_bytes()).hexdigest(),
                    "singleMemberArchive": True,
                    "size": len(payload),
                    "sha256": hashlib.sha256(payload).hexdigest(),
                },
            }

            written = MODULE.materialize_game(fixture, source_root, repo_root)

            self.assertEqual(len(written), 2)
            self.assertEqual(
                (repo_root / "data/example/local-fixtures/fixture/game.rom").read_bytes(),
                payload,
            )

    def test_rejects_source_and_target_traversal(self) -> None:
        with self.assertRaisesRegex(ValueError, "safe relative"):
            MODULE.safe_relative("../outside", "sourceRelativePath")
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(ValueError, "must remain"):
                MODULE.target_path(Path(temporary), "data/game/outside.rom")

    def test_rejects_a_multi_member_archive(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source_root = root / "source"
            source_root.mkdir()
            archive_path = source_root / "fixture.zip"
            with zipfile.ZipFile(archive_path, "w") as archive:
                archive.writestr("one.rom", b"one")
                archive.writestr("two.rom", b"two")
            fixture = {
                "core": "fixture",
                "game": {
                    "localPath": "data/example/local-fixtures/fixture/game.rom",
                    "sourceRelativePath": "fixture.zip",
                    "sourceArchiveLocalPath": "data/example/local-fixtures/fixture/source.zip",
                    "sourceArchiveSize": archive_path.stat().st_size,
                    "sourceArchiveSha256": hashlib.sha256(archive_path.read_bytes()).hexdigest(),
                    "singleMemberArchive": True,
                    "size": 3,
                    "sha256": hashlib.sha256(b"one").hexdigest(),
                },
            }

            with self.assertRaisesRegex(ValueError, "expected one archive member"):
                MODULE.materialize_game(fixture, source_root, root / "repo")

    def test_materializes_an_optional_parent_archive(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source_root = root / "source"
            source_root.mkdir()
            payload = b"parent-romset"
            (source_root / "parent.zip").write_bytes(payload)
            fixture = {
                "parent": {
                    "localPath": "data/example/local-fixtures/fixture/parent.zip",
                    "sourceRelativePath": "parent.zip",
                    "size": len(payload),
                    "sha256": hashlib.sha256(payload).hexdigest(),
                }
            }

            written = MODULE.materialize_parent(fixture, source_root, root / "repo")

            self.assertEqual(len(written), 1)
            self.assertEqual(written[0].read_bytes(), payload)


if __name__ == "__main__":
    unittest.main()
