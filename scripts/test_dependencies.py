#!/usr/bin/env python3

from __future__ import annotations

import contextlib
import io
import json
import os
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest import mock

import dependencies


class VersionTests(unittest.TestCase):
    def test_cli_reports_dependency_errors_without_a_traceback(self) -> None:
        output = io.StringIO()
        with mock.patch("sys.argv", ["dependencies.py", "data-check", "--versions", ""]), \
                contextlib.redirect_stderr(output):
            self.assertEqual(1, dependencies.main())
        self.assertEqual("DEPENDENCY_VERSION_LIST_INVALID\n", output.getvalue())

    def test_versions_are_strictly_increasing(self) -> None:
        self.assertEqual(["4.2.3", "4.3.0-pre"], dependencies.parse_versions("4.2.3,4.3.0-pre"))
        for invalid in ("", "4.2.3,4.2.3", "4.3.0,4.2.3", "4.2.03"):
            with self.subTest(invalid=invalid), self.assertRaises(dependencies.CheckError):
                dependencies.parse_versions(invalid)


class DATManifestTests(unittest.TestCase):
    def test_mame_current_dat_is_bound_to_published_core_and_provider(self) -> None:
        manifest = dependencies.load_mame_manifest()
        self.assertEqual("v0.58.0", manifest["provider_release"]["tag"])
        self.assertEqual("retrom-core-gf65d5ba9bc42-r2", manifest["core_release"]["tag"])
        self.assertEqual("mame_arcade", manifest["cores"][0]["core_id"])
        self.assertEqual(10049, manifest["cores"][0]["parse_stats"]["machine_count"])
        entries = dependencies.image_export_entries([], [], dependencies.load_auth_manifest(), manifest)
        self.assertIn("dat/mame-current/v0.58.0/mame-arcade.xml", entries)
        self.assertIn("runtime-providers/release.json", entries)

    def test_repository_manifests_are_provider_neutral(self) -> None:
        for version in ("4.2.3", "4.3.0-pre"):
            manifest = dependencies.load_manifest(version)
            encoded = json.dumps(manifest, sort_keys=True)
            for forbidden in (
                "runtime_allowlist", "selected_core_artifacts", "player_adapter",
                "runtime_family", "route_key", "adapter_abi",
            ):
                self.assertNotIn(forbidden, encoded)

    def test_unknown_runtime_mapping_is_rejected(self) -> None:
        manifest = dependencies.load_json(dependencies.manifest_path("4.2.3"))
        manifest["runtimeRegistry"] = []
        with mock.patch.object(dependencies, "load_json", return_value=manifest):
            with self.assertRaisesRegex(dependencies.CheckError, "DEPENDENCY_SCHEMA_UNSUPPORTED"):
                dependencies.load_manifest("4.2.3")

    def test_sha256s_match_declared_dat_set(self) -> None:
        manifest = dependencies.load_manifest("4.2.3")
        declared = {core["dat"]["local_path"] for core in manifest["cores"]}
        sums = dependencies.load_sha256s(
            dependencies.DATA_ROOT / "dat/emulatorjs/4.2.3/SHA256SUMS"
        )
        self.assertEqual(declared, set(sums))


class MaterializationTests(unittest.TestCase):
    def test_mame_dat_extracts_only_verified_member(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            payload = b"<mame/>"
            archive = root / "source.zip"
            with zipfile.ZipFile(archive, "w") as bundle:
                bundle.writestr("mame-arcade.xml", payload)
                bundle.writestr("other.txt", b"ignored")
            manifest = {"core_release": {"archive": {"filename": "mame-current-assets.zip",
                "url": "https://invalid.test/archive", "size_bytes": archive.stat().st_size,
                "sha256": dependencies.hashlib.sha256(archive.read_bytes()).hexdigest()}},
                "cores": [{"dat": {"local_path": "mame-arcade.xml", "archive_member": "mame-arcade.xml",
                    "size_bytes": len(payload), "sha256": dependencies.hashlib.sha256(payload).hexdigest()}}]}
            def copy_archive(url: str, target: Path, size: int, digest: str) -> None:
                self.assertEqual(archive.stat().st_size, size)
                self.assertEqual(dependencies.hashlib.sha256(archive.read_bytes()).hexdigest(), digest)
                target.write_bytes(archive.read_bytes())
            with mock.patch.object(dependencies, "MAME_DAT_ROOT", root), mock.patch.object(
                dependencies, "download", side_effect=copy_archive,
            ):
                dependencies.prepare_mame_dat(manifest)
                self.assertEqual(payload, (root / "mame-arcade.xml").read_bytes())
                (root / "mame-arcade.xml").unlink()
                manifest["cores"][0]["dat"]["sha256"] = "0" * 64
                with self.assertRaisesRegex(dependencies.CheckError, "MAME_DAT_ARCHIVE_MEMBER_INVALID"):
                    dependencies.prepare_mame_dat(manifest)

    def test_existing_auth_payload_is_normalized_to_private_mode(self) -> None:
        contents = b"fixture\n"
        digest = dependencies.hashlib.sha256(contents).hexdigest()
        manifest = {
            "passwords": {
                "output_relative_path": "payload/passwords.txt",
                "size_bytes": len(contents), "sha256": digest, "url": "https://invalid.test/passwords",
            },
            "license": {
                "output_relative_path": "payload/LICENSE",
                "size_bytes": len(contents), "sha256": digest, "url": "https://invalid.test/license",
            },
        }
        with tempfile.TemporaryDirectory(dir="/tmp") as directory:
            root = Path(directory)
            for entry in manifest.values():
                target = root / entry["output_relative_path"]
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(contents)
                os.chmod(target, 0o777)
            with mock.patch.object(dependencies, "AUTH_ROOT", root):
                dependencies.prepare_auth(manifest)
            for entry in manifest.values():
                self.assertEqual(0o600, (root / entry["output_relative_path"]).stat().st_mode & 0o777)

    def test_image_export_contains_no_runtime_implementation(self) -> None:
        versions = ["4.2.3", "4.3.0-pre"]
        manifests = [dependencies.load_manifest(version) for version in versions]
        entries = dependencies.image_export_entries(
            versions, manifests, dependencies.load_auth_manifest()
        )
        self.assertFalse(any(path.startswith("runtime/") for path in entries))
        self.assertIn("runtime-target-bindings/v1/catalog.json", entries)


if __name__ == "__main__":
    unittest.main()
