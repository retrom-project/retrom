#!/usr/bin/env python3

from __future__ import annotations

import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import dependencies


class VersionTests(unittest.TestCase):
    def test_versions_are_strictly_increasing(self) -> None:
        self.assertEqual(["4.2.3", "4.3.0-pre"], dependencies.parse_versions("4.2.3,4.3.0-pre"))
        for invalid in ("", "4.2.3,4.2.3", "4.3.0,4.2.3", "4.2.03"):
            with self.subTest(invalid=invalid), self.assertRaises(dependencies.CheckError):
                dependencies.parse_versions(invalid)


class DATManifestTests(unittest.TestCase):
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


class NetplayTests(unittest.TestCase):
    def test_profiles_resolve_through_host_bindings(self) -> None:
        dependencies.validate_netplay_manifest()

    def test_unbound_target_is_rejected(self) -> None:
        manifest = dependencies.load_json(dependencies.NETPLAY_MANIFEST_PATH)
        catalog = dependencies.load_json(dependencies.TARGET_CATALOG_PATH)
        manifest["profiles"][0]["targetId"] = "missing-target"

        def load(path: Path) -> dict[str, object]:
            return manifest if path == dependencies.NETPLAY_MANIFEST_PATH else catalog

        with tempfile.TemporaryDirectory(dir="/tmp") as directory:
            schema = Path(directory) / "schema.json"
            schema.write_text("{}", encoding="utf-8")
            with mock.patch.object(dependencies, "load_json", side_effect=load), \
                    mock.patch.object(dependencies, "NETPLAY_SCHEMA_PATH", schema):
                with self.assertRaisesRegex(dependencies.CheckError, "NETPLAY_MANIFEST_INVALID"):
                    dependencies.validate_netplay_manifest()


class MaterializationTests(unittest.TestCase):
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
        self.assertIn("netplay/v2/manifest.json", entries)


if __name__ == "__main__":
    unittest.main()
