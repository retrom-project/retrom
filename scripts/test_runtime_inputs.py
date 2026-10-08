"""Paired preparation verifies bytes, provenance and publication as one input set."""
import copy
import hashlib
import io
import json
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock

from runtime_input_manifest import REPOSITORY, artifact_records, load_manifest, validate_manifest
from runtime_inputs import prepare, verify_prepared
from test_runtime_providers import fixture_bundle
from runtime_provider_bundle import describe_installed_provider
from runtime_input_manifest import build_record


def paired_fixture(root, *, formal=True, source="c" * 64):
    archives = root / "inputs"
    archives.mkdir()
    providers = []
    for provider_id in ("emulatorjs", "retrom-runtime"):
        archive, lock = fixture_bundle(archives, provider_id=provider_id, runtime_source=source)
        providers.append({**{key: lock[key] for key in ("providerId", "providerVersion", "bundleSha256", "bundleSizeBytes", "manifestSha256", "fileCount", "unpackedSizeBytes")},
                          "archive": f"{provider_id}/{archive.name}", "bundleDirectory": f"{provider_id}/{provider_id}-1.0.0",
                          "moduleSha256": hashlib.sha256(f"export const providerId='{provider_id}';\n".encode()).hexdigest()})
    files = {"host-tool.json": json.dumps({"schemaVersion": 1, "version": "1.0.0", "sourceTreeSha256": source}).encode(),
             "package.json": b'{"dependencies":{"fixture":"1.0.0"}}', "node_modules/fixture/index.js": b"export {}",
             "dist/runtime/index.js": b"export {}", "scripts/runtime-cli.mjs": b"console.log('{}');"}
    tool = archives / "retrom-runtime-host-tool-1.0.0.tar.gz"
    with tarfile.open(tool, "w:gz") as stream:
        for name, data in files.items():
            entry = tarfile.TarInfo("package/" + name)
            entry.size, entry.mode = len(data), 0o644
            stream.addfile(entry, io.BytesIO(data))
    manifest = {"schemaVersion": 1, "repository": REPOSITORY, "sourceTreeSha256": source,
                "release": {"repository": REPOSITORY, "tag": "v1.0.0", "commit": "a" * 40} if formal else None,
                "providers": providers, "tool": {"archive": tool.name, "sizeBytes": tool.stat().st_size,
                                                   "sha256": hashlib.sha256(tool.read_bytes()).hexdigest()}}
    pin = root / "runtime-inputs.json"
    pin.write_text(json.dumps(manifest))
    downloads = {f"{REPOSITORY}/releases/download/v1.0.0/{record['archive']}": (archives / record['archive']).read_bytes()
                 for record in artifact_records(manifest)}
    return pin, archives, manifest, downloads


class RuntimeInputsTests(unittest.TestCase):
    def test_formal_cold_then_offline_preparation_and_candidate_local_use_one_shape(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pin, archives, manifest, downloads = paired_fixture(root)
            fetch = Mock(side_effect=lambda url, maximum: downloads[url])
            prepared = prepare(pin, root / "output", root / "cache", fetch=fetch)
            self.assertEqual(fetch.call_count, 3)
            self.assertEqual(json.loads((prepared / "providers/active.json").read_text())["release"], manifest["release"])
            offline = Mock(side_effect=AssertionError("must reuse pinned cache"))
            self.assertEqual(prepare(pin, root / "output", root / "cache", fetch=offline), prepared)
            second = prepare(pin, root / "second", root / "cache", fetch=offline)
            verify_prepared(second, manifest)
            offline.assert_not_called()
            manifest["release"] = None
            pin.write_text(json.dumps(manifest))
            candidate = prepare(pin, root / "candidate", root / "local-cache", archive_root=archives)
            self.assertEqual(json.loads((candidate / "providers/active.json").read_text())["source"], "candidate")

    def test_invalid_manifest_fields_and_cross_source_artifacts_never_publish(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pin, archives, manifest, _ = paired_fixture(root, formal=False)
            for field, value in (("sourceTreeSha256", "bad"), ("schemaVersion", True), ("release", {"tag": "v1.0.0"})):
                changed = copy.deepcopy(manifest)
                changed[field] = value
                with self.assertRaises(ValueError):
                    validate_manifest(changed)
            malformed = []
            for field, value in (("archive", "../escape.tar.gz"), ("providerVersion", "2.0.0"),
                                 ("bundleSizeBytes", True), ("fileCount", 100001), ("moduleSha256", "bad")):
                changed = copy.deepcopy(manifest)
                changed["providers"][0][field] = value
                malformed.append(changed)
            malformed.extend([{**manifest, "providers": manifest["providers"][:1]},
                              {**manifest, "providers": manifest["providers"] * 2}, {**manifest, "tag": "v1.0.0"}])
            for changed in malformed:
                with self.subTest(changed=changed), self.assertRaises(ValueError):
                    validate_manifest(changed)
            changed = copy.deepcopy(manifest)
            changed["sourceTreeSha256"] = "d" * 64
            pin.write_text(json.dumps(changed))
            with self.assertRaisesRegex(ValueError, "SOURCE_MISMATCH"):
                prepare(pin, root / "output", root / "cache", archive_root=archives)
            self.assertEqual(list((root / "output").glob("[0-9a-f]*")), [])
            self.assertEqual(list((root / "output").glob(".prepare-*")), [])

    def test_corrupt_download_and_repeated_output_validation_fail_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pin, archives, manifest, downloads = paired_fixture(root)
            with self.assertRaisesRegex(ValueError, "ARTIFACT_DIGEST_INVALID"):
                prepare(pin, root / "bad", root / "bad-cache", fetch=lambda *args: b"bad")
            self.assertEqual(list((root / "bad-cache").rglob("*.tar.gz")), [])
            tool_size = manifest["tool"]["sizeBytes"]
            with self.assertRaisesRegex(ValueError, "ARTIFACT_DIGEST_INVALID"):
                prepare(pin, root / "wrong-hash", root / "wrong-hash-cache", fetch=lambda *args: b"x" * tool_size)
            self.assertEqual(list((root / "wrong-hash-cache").rglob("*.tar.gz")), [])
            prepared = prepare(pin, root / "output", root / "cache", archive_root=archives)
            (prepared / "tool/dist/runtime/index.js").write_text("export const stillRunnable = true;")
            # A locally regenerated checksum file is not an authority for the archive's bytes.
            (prepared / "tool-integrity.json").write_text(json.dumps({"dist/runtime/index.js": "forged"}))
            with self.assertRaisesRegex(ValueError, "TOOL_ARCHIVE_MISMATCH"):
                prepare(pin, root / "output", root / "cache", fetch=Mock(side_effect=AssertionError("offline")))
            self.assertEqual(load_manifest(pin), manifest)

    def test_unpublished_missing_transport_has_no_old_release_fallback(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pin, _, _, _ = paired_fixture(root, formal=False)
            fetch = Mock(side_effect=AssertionError("must not request a historical release"))
            with self.assertRaisesRegex(ValueError, "TRANSPORT_REQUIRED"):
                prepare(pin, root / "output", root / "cache", fetch=fetch)
            fetch.assert_not_called()

    def test_rehashed_local_provider_asset_cannot_replace_pinned_archive_facts(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pin, archives, manifest, _ = paired_fixture(root, formal=False)
            prepared = prepare(pin, root / "output", root / "cache", archive_root=archives)
            provider = manifest["providers"][0]
            installed = prepared / "providers/installed" / provider["providerId"] / provider["bundleSha256"]
            changed = b"\x00asm\x02\x00\x00\x00"
            (installed / "assets/core.wasm").write_bytes(changed)
            integrity_path = installed / "integrity.json"
            integrity = json.loads(integrity_path.read_text())
            entry = next(file for file in integrity["files"] if file["path"] == "assets/core.wasm")
            entry["sizeBytes"], entry["sha256"] = len(changed), hashlib.sha256(changed).hexdigest()
            integrity_path.write_text(json.dumps(integrity, ensure_ascii=False, indent=2) + "\n")
            # Existing local-integrity validation alone accepts this same-length replacement.
            describe_installed_provider(build_record(provider), prepared / "providers/installed")
            with self.assertRaisesRegex(ValueError, "PROVIDER_ARCHIVE_MISMATCH"):
                verify_prepared(prepared, manifest)


if __name__ == "__main__":
    unittest.main()
