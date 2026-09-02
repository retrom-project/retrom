import hashlib
import io
import json
import tarfile
import tempfile
import unittest
from pathlib import Path

from runtime_provider_bundle import install_provider_bundle, validate_provider_lock
from runtime_providers import prepare_candidate_providers


class RuntimeProviderInstallerTest(unittest.TestCase):
    def test_installs_a_closed_verified_bundle_by_content_digest(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, lock = fixture_bundle(root)
            installed = install_provider_bundle(archive, lock, root / "installed")
            self.assertEqual(
                installed,
                root / "installed" / "fixture" / lock["bundleSha256"],
            )
            self.assertEqual((installed / "client.mjs").read_bytes(), b"export const providerId='fixture';\n")
            proof = json.loads((installed / ".installation.json").read_text(encoding="utf-8"))
            self.assertEqual(proof["bundleSha256"], lock["bundleSha256"])
            self.assertEqual(proof["manifestSha256"], lock["manifestSha256"])

    def test_rejects_unknown_lock_fields_and_archive_traversal(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, lock = fixture_bundle(root)
            with self.assertRaisesRegex(ValueError, "PROVIDER_LOCK_INVALID"):
                validate_provider_lock({**lock, "adapterId": "leaked"})

            traversal = root / "traversal.tar.gz"
            with tarfile.open(traversal, "w:gz") as output:
                info = tarfile.TarInfo("../outside")
                info.size = 1
                output.addfile(info, io.BytesIO(b"x"))
            changed = dict(lock)
            changed["bundleSha256"] = digest(traversal.read_bytes())
            changed["bundleSizeBytes"] = traversal.stat().st_size
            with self.assertRaisesRegex(ValueError, "PROVIDER_BUNDLE_UNSAFE"):
                install_provider_bundle(traversal, changed, root / "installed")

    def test_accepts_provider_version_independent_from_release_tag(self):
        with tempfile.TemporaryDirectory() as temporary:
            _, lock = fixture_bundle(Path(temporary))
            lock["tag"] = "v0.12.0"
            self.assertEqual(validate_provider_lock(lock), lock)

    def test_rejects_a_manifest_whose_asset_closure_differs_from_the_bundle(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, lock = fixture_bundle(root, manifest_asset="assets/missing.wasm")
            with self.assertRaisesRegex(ValueError, "PROVIDER_MANIFEST_ASSET_CLOSURE_INVALID"):
                install_provider_bundle(archive, lock, root / "installed")

    def test_prepares_candidate_release_metadata_without_network_access(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, lock = fixture_bundle(root)
            candidate = root / "candidate"
            provider_root = candidate / "providers/fixture"
            provider_root.mkdir(parents=True)
            archive_target = provider_root / archive.name
            archive_target.write_bytes(archive.read_bytes())
            metadata = {
                "release": {
                    "commit": "a" * 40,
                    "repository": "https://github.com/retrom-project/retrom-runtime",
                    "tag": "v0.12.0",
                },
                "schemaVersion": 1,
                "providers": [{
                    "archive": f"fixture/{archive.name}",
                    "bundleDirectory": "fixture/fixture-1.0.0",
                    "bundleSha256": lock["bundleSha256"],
                    "bundleSizeBytes": lock["bundleSizeBytes"],
                    "fileCount": lock["fileCount"],
                    "manifestSha256": lock["manifestSha256"],
                    "providerId": "fixture",
                    "providerVersion": "1.0.0",
                    "unpackedSizeBytes": lock["unpackedSizeBytes"],
                }],
            }
            (candidate / "providers/provider-release.json").write_text(json.dumps(metadata), encoding="utf-8")

            active = prepare_candidate_providers(
                candidate,
                root / "installed",
            )

            self.assertEqual(active["providers"][0]["providerId"], "fixture")
            self.assertEqual(active["providers"][0]["bundleSha256"], lock["bundleSha256"])
            self.assertTrue((root / "installed/fixture" / lock["bundleSha256"] / "provider.json").is_file())


def fixture_bundle(root: Path, manifest_asset="assets/core.wasm"):
    manifest = json_bytes({
        "clientModulePath": "client.mjs",
        "providerApiVersion": 1,
        "providerId": "fixture",
        "providerVersion": "1.0.0",
        "schemaVersion": 1,
        "targets": [{
            "assetPaths": [manifest_asset],
            "capabilities": {
                "checkpoint": False, "frameCounter": False, "frameMode": "NONE",
                "discSwitch": False, "inputFilter": False, "nativeSettings": False,
                "netplayPort": False,
                "pause": False, "requiresThreads": False, "screenshot": False,
                "standardGamepad": False, "validationProbes": [], "videoModes": [],
                "volume": False,
            },
            "checkpoint": None,
            "displayName": "Fixture",
            "gameCompatibilityLine": "fixture-v1",
            "id": "fixture",
            "inputs": [{"cardinality": "ONE", "kind": "ROM_BLOB_V1", "optional": False, "role": "game"}],
            "netplayCompatibilityLine": None,
            "optionsKind": "NONE_V1",
        }],
    })
    files = {
        "assets/core.wasm": b"\x00asm\x01\x00\x00\x00",
        "client.mjs": b"export const providerId='fixture';\n",
        "provider.json": manifest,
        "provenance.json": json_bytes({"schemaVersion": 1}),
        "licenses/fixture/LICENSE": b"fixture license\n",
    }
    integrity = json_bytes({
        "files": [{
            "mediaType": media_type(path), "path": path, "sha256": digest(contents),
            "sizeBytes": len(contents),
        } for path, contents in sorted(files.items())],
        "schemaVersion": 1,
    })
    files["integrity.json"] = integrity
    archive = root / "fixture-provider-1.0.0.tar.gz"
    with tarfile.open(archive, "w:gz", format=tarfile.USTAR_FORMAT) as output:
        for path, contents in sorted(files.items()):
            info = tarfile.TarInfo(path)
            info.mode = 0o644
            info.size = len(contents)
            output.addfile(info, io.BytesIO(contents))
    lock = {
        "bundleSha256": digest(archive.read_bytes()),
        "bundleSizeBytes": archive.stat().st_size,
        "bundleUrl": "https://example.invalid/fixture-provider-1.0.0.tar.gz",
        "commit": "a" * 40,
        "fileCount": len(files),
        "manifestSha256": digest(manifest),
        "providerId": "fixture",
        "providerVersion": "1.0.0",
        "repository": "https://example.invalid/fixture",
        "schemaVersion": 1,
        "tag": "v1.0.0",
        "unpackedSizeBytes": sum(len(value) for value in files.values()),
    }
    return archive, lock


def json_bytes(value):
    return (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode()


def digest(value):
    return hashlib.sha256(value).hexdigest()


def media_type(path):
    if path.endswith(".wasm"):
        return "application/wasm"
    if path.endswith(".mjs"):
        return "text/javascript; charset=utf-8"
    if path.endswith(".json"):
        return "application/json; charset=utf-8"
    return "text/plain; charset=utf-8"


if __name__ == "__main__":
    unittest.main()
