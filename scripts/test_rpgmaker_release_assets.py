#!/usr/bin/env python3
"""Unit tests for the aggregate retrom-runtime Release consumer."""

from __future__ import annotations

import copy
import importlib.util
import io
import json
import os
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "data/dat/rpgmaker/v1/build.py"
SPEC = importlib.util.spec_from_file_location("retrom_rpg_runtime_build", MODULE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("RPG_RUNTIME_BUILD_IMPORT_FAILED")
BUILD = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = BUILD
SPEC.loader.exec_module(BUILD)


def metadata(manifest: dict[str, object]) -> bytes:
    release = manifest["release"]
    files = [
        {
            "path": item["bundle_path"],
            "filename": Path(item["bundle_path"]).name,
            "sizeBytes": len(f"payload:{item['bundle_path']}".encode()),
            "sha256": "metadata-digest-is-not-an-admission-coordinate",
        }
        for item in manifest["runtime_files"]
    ]
    return json.dumps({
        "schemaVersion": 1,
        "repository": release["repository"],
        "tag": release["tag"],
        "commit": release["tag_commit"],
        "version": release["tag"][1:],
        "publicApiVersion": BUILD.PUBLIC_API_VERSION,
        "files": files,
    }).encode()


def bundle(manifest: dict[str, object]) -> bytes:
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode="w:gz") as archive:
        for item in manifest["runtime_files"]:
            contents = f"payload:{item['bundle_path']}".encode()
            record = tarfile.TarInfo(f"./{item['bundle_path']}")
            record.size = len(contents)
            archive.addfile(record, io.BytesIO(contents))
    return output.getvalue()


class RPGMakerReleaseAssetTests(unittest.TestCase):
    def setUp(self) -> None:
        self.manifest = BUILD.load_manifest()

    def test_manifest_contains_only_one_release_and_eleven_current_routes(self) -> None:
        BUILD.validate_manifest(self.manifest)
        self.assertEqual(11, len(self.manifest["artifacts"]))
        self.assertEqual(
            set(BUILD.EXPECTED_ROUTES),
            {item["route_key"] for item in self.manifest["artifacts"]},
        )
        serialized = json.dumps(self.manifest)
        for stale in ("runtime_releases", "source_archives", "retrom-web-", "_V3", "_V4", "_V5", "_V6", "_V7"):
            self.assertNotIn(stale, serialized)

    def test_prepare_materializes_one_tag_directory_and_records_observed_integrity(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "runtime"
            with patch.object(BUILD, "download_bytes", side_effect=[metadata(self.manifest), bundle(self.manifest)]):
                BUILD.prepare(self.manifest, root, offline=False)
            BUILD.verify_runtime(self.manifest, root)
            self.assertEqual(
                [self.manifest["release"]["tag"]],
                sorted(path.name for path in root.iterdir() if path.is_dir()),
            )
            self.assertEqual(
                f"payload:{self.manifest['runtime_files'][0]['bundle_path']}".encode(),
                (root / self.manifest["runtime_files"][0]["path_in_release"]).read_bytes(),
            )
            observed = json.loads((root / BUILD.OBSERVED_FILENAME).read_text())
            self.assertEqual(self.manifest["release"]["tag"], observed["tag"])
            self.assertEqual(24, len(observed["files"]))

    def test_release_metadata_sha_is_not_a_remote_admission_coordinate(self) -> None:
        records = BUILD.validate_release_metadata(self.manifest, metadata(self.manifest))
        self.assertEqual(
            "metadata-digest-is-not-an-admission-coordinate",
            records[self.manifest["runtime_files"][0]["bundle_path"]]["sha256"],
        )

    def test_release_metadata_requires_contract_v2(self) -> None:
        previous = json.loads(metadata(self.manifest))
        previous["publicApiVersion"] = 1
        with self.assertRaisesRegex(BUILD.BuildError, "RPG_RUNTIME_RELEASE_METADATA_INVALID"):
            BUILD.validate_release_metadata(self.manifest, json.dumps(previous).encode())

    def test_web_package_uses_the_same_aggregate_release(self) -> None:
        version = self.manifest["release"]["tag"].removeprefix("v")
        asset_url = (
            "https://github.com/xxxsen/retrom-runtime/releases/download/"
            f"v{version}/xxxsen-retrom-runtime-{version}.tgz"
        )
        package = json.loads((ROOT / "web/package.json").read_text(encoding="utf-8"))
        package_lock = json.loads((ROOT / "web/package-lock.json").read_text(encoding="utf-8"))
        locked = package_lock["packages"]["node_modules/@xxxsen/retrom-runtime"]
        self.assertEqual(asset_url, package["dependencies"]["@xxxsen/retrom-runtime"])
        self.assertEqual(asset_url, package_lock["packages"][""]["dependencies"]["@xxxsen/retrom-runtime"])
        self.assertEqual(version, locked["version"])
        self.assertEqual(asset_url, locked["resolved"])

    def test_local_tamper_and_offline_missing_release_fail_closed(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "runtime"
            with self.assertRaisesRegex(BUILD.BuildError, "RPG_RUNTIME_RELEASE_REQUIRED"):
                BUILD.prepare(self.manifest, root, offline=True)
            with patch.object(BUILD, "download_bytes", side_effect=[metadata(self.manifest), bundle(self.manifest)]):
                BUILD.prepare(self.manifest, root, offline=False)
            target = root / self.manifest["runtime_files"][0]["path_in_release"]
            target.write_bytes(b"tampered")
            with self.assertRaisesRegex(BUILD.BuildError, "RPG_RUNTIME_FILE_MISMATCH"):
                BUILD.verify_runtime(self.manifest, root)

    def test_local_dev_override_requires_the_matching_explicit_checkout(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary) / "retrom-runtime"
            source.mkdir()
            root = Path(temporary) / "runtime"
            with patch.object(BUILD, "download_bytes", side_effect=[metadata(self.manifest), bundle(self.manifest)]):
                BUILD.prepare(self.manifest, root, offline=False)
            marker = {
                "schema_version": 1,
                "source_root": str(source.resolve()),
                "source_commit": "a" * 40,
                "package_version": "0.4.1",
                "overlaid_assets": ["runtime/ons/onsyuri.js"],
            }
            (root / BUILD.DEV_MARKER_FILENAME).write_text(json.dumps(marker), encoding="utf-8")
            with patch.dict(os.environ, {}, clear=True):
                with self.assertRaisesRegex(BUILD.BuildError, "RPG_RUNTIME_DEV_OVERRIDE_ACTIVE"):
                    BUILD.verify_runtime(self.manifest, root)
            with patch.dict(os.environ, {"RETROM_RUNTIME_DEV_ROOT": str(source)}):
                BUILD.verify_runtime(self.manifest, root)

    def test_rejects_migration_route_and_archive_link(self) -> None:
        drifted = copy.deepcopy(self.manifest)
        drifted["artifacts"][0]["route_key"] = "RPG2000_UNDECLARED"
        with self.assertRaisesRegex(BUILD.BuildError, "RPG_RUNTIME_ARTIFACT_ROUTE_INVALID"):
            BUILD.validate_manifest(drifted)

        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode="w:gz") as archive:
            record = tarfile.TarInfo("runtime/easyrpg/easyrpg-player.js")
            record.type = tarfile.SYMTYPE
            record.linkname = "../../outside"
            archive.addfile(record)
        with self.assertRaisesRegex(BUILD.BuildError, "RPG_RUNTIME_RELEASE_ARCHIVE_INVALID"):
            BUILD.extract_runtime_files(self.manifest, output.getvalue())


if __name__ == "__main__":
    unittest.main()
