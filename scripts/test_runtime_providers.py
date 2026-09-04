import hashlib
import io
import json
import subprocess
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path

from runtime_provider_contract import canonical_json_bytes
from runtime_provider_bundle import (
    check_installed_provider,
    install_provider_bundle,
    validate_provider_lock,
)
from runtime_providers import (
    check_active_providers,
    check_active_providers_for_upgrade,
    pin_provider_release,
    prepare_candidate_providers,
    prepare_production_providers,
    verify_provider_upgrade,
)


class RuntimeProviderInstallerTest(unittest.TestCase):
    def test_bundle_module_is_importable_by_the_pfb_package_entrypoint(self):
        root = Path(__file__).resolve().parent.parent
        result = subprocess.run(
            [sys.executable, "-c", "import scripts.runtime_provider_bundle"],
            cwd=root,
            check=False,
            capture_output=True,
            text=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)

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

            malicious = {
                "directory": [tar_member("nested", b"", tarfile.DIRTYPE)],
                "hardlink": [tar_member("linked", b"", tarfile.LNKTYPE, "client.mjs")],
                "symlink": [tar_member("linked", b"", tarfile.SYMTYPE, "client.mjs")],
                "duplicate": [tar_member("same", b"x"), tar_member("same", b"y")],
            }
            for name, members in malicious.items():
                with self.subTest(name=name):
                    candidate = root / f"{name}.tar.gz"
                    with tarfile.open(candidate, "w:gz") as output:
                        for info, payload in members:
                            output.addfile(info, io.BytesIO(payload) if info.isfile() else None)
                    candidate_lock = dict(lock)
                    candidate_lock["bundleSha256"] = digest(candidate.read_bytes())
                    candidate_lock["bundleSizeBytes"] = candidate.stat().st_size
                    with self.assertRaisesRegex(ValueError, "PROVIDER_BUNDLE_UNSAFE"):
                        install_provider_bundle(candidate, candidate_lock, root / "installed-malicious")

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

    def test_installer_rejects_an_unsupported_provider_api(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, lock = fixture_bundle(root, provider_api=2)
            with self.assertRaisesRegex(ValueError, "RUNTIME_PROVIDER_API_UNSUPPORTED"):
                install_provider_bundle(archive, lock, root / "installed")

    def test_revalidates_every_installed_byte_instead_of_trusting_the_proof(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, lock = fixture_bundle(root)
            installed = install_provider_bundle(archive, lock, root / "installed")
            (installed / "client.mjs").write_text("tampered\n", encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "PROVIDER_INTEGRITY_INVALID"):
                check_installed_provider(lock, root / "installed")
            with self.assertRaisesRegex(ValueError, "PROVIDER_INTEGRITY_INVALID"):
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
                "schemaVersion": 1,
                "sourceTreeSha256": "e" * 64,
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
            (candidate / "providers/provider-build.json").write_text(json.dumps(metadata), encoding="utf-8")

            active = prepare_candidate_providers(
                candidate,
                root / "installed",
            )

            self.assertEqual(active["providers"][0]["providerId"], "fixture")
            self.assertEqual(active["providers"][0]["bundleSha256"], lock["bundleSha256"])
            self.assertEqual(active["source"], "candidate")
            self.assertIsNone(active["release"])
            self.assertNotIn("commit", json.dumps(active))
            self.assertNotIn("tag", json.dumps(active))
            self.assertTrue((root / "installed/fixture" / lock["bundleSha256"] / "provider.json").is_file())

    def test_rejects_downgrade_same_version_rebuild_and_unreadable_checkpoint(self):
        current = active_fixture(version="2.0.0", bundle="a", read_formats=["state-v1"])
        with self.assertRaisesRegex(ValueError, "RUNTIME_PROVIDER_DOWNGRADE_FORBIDDEN"):
            verify_provider_upgrade(current, active_fixture(version="1.9.0", bundle="b", read_formats=["state-v1"]), [])
        with self.assertRaisesRegex(ValueError, "RUNTIME_PROVIDER_VERSION_REBUILT"):
            verify_provider_upgrade(current, active_fixture(version="2.0.0", bundle="b", read_formats=["state-v1"]), [])
        with self.assertRaisesRegex(ValueError, "RUNTIME_PROVIDER_CHECKPOINT_FORMAT_UNREADABLE"):
            verify_provider_upgrade(
                current,
                active_fixture(version="2.1.0", bundle="b", read_formats=["state-v2"]),
                [{"format": "state-v1", "providerId": "fixture", "targetId": "fixture"}],
            )
        verify_provider_upgrade(
            current,
            active_fixture(version="2.1.0", bundle="b", read_formats=["state-v1", "state-v2"]),
            [{"format": "state-v1", "providerId": "fixture", "targetId": "fixture"}],
        )

    def test_legacy_active_base_is_fully_verified_only_as_an_upgrade_source(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            active_path, installed_root, legacy = legacy_active_fixture(root)

            with self.assertRaises(ValueError):
                check_active_providers(active_path, installed_root, "candidate")
            current = check_active_providers_for_upgrade(active_path, installed_root, "candidate")
            self.assertEqual(current, legacy)
            verify_provider_upgrade(
                current,
                active_fixture(version="1.1.0", bundle="b", read_formats=["state-v1"]),
                [],
            )

            installed = installed_root / legacy["providers"][0]["installationPath"]
            (installed / "client.mjs").write_text("tampered\n", encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "PROVIDER_INTEGRITY_INVALID"):
                check_active_providers_for_upgrade(active_path, installed_root, "candidate")

    def test_pins_release_then_prepares_from_a_verified_offline_cache(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, lock = fixture_bundle(root)
            release_root = root / "release"
            archive_target = release_root / "providers/fixture" / archive.name
            archive_target.parent.mkdir(parents=True)
            archive_target.write_bytes(archive.read_bytes())
            metadata = formal_release_metadata(lock)
            (release_root / "providers/provider-release.json").write_text(json.dumps(metadata), encoding="utf-8")
            lock_root = root / "locks"
            locks = pin_provider_release(release_root, lock_root)
            self.assertEqual([item["providerId"] for item in locks], ["fixture"])

            fetch_count = 0

            def fetch(_url, _maximum):
                nonlocal fetch_count
                fetch_count += 1
                return archive.read_bytes()

            active_path = root / "active/active.json"
            active = prepare_production_providers(
                lock_root, root / "cache", root / "installed-a", active_path, fetch,
            )
            self.assertEqual(fetch_count, 1)
            self.assertEqual(active["source"], "production")
            self.assertEqual(active["release"], metadata["release"])
            check_active_providers(active_path, root / "installed-a", "production")

            def offline(_url, _maximum):
                raise AssertionError("verified cache hit must not access the network")

            prepare_production_providers(
                lock_root, root / "cache", root / "installed-b", root / "active-b.json", offline,
            )

    def test_production_prepare_rejects_candidate_active_descriptor(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            active_path = root / "active.json"
            active_path.write_text(json.dumps(active_fixture(
                version="1.0.0", bundle="a", read_formats=["state-v1"],
            )), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "RUNTIME_PROVIDER_CANDIDATE_FORBIDDEN"):
                prepare_production_providers(
                    root / "locks", root / "cache", root / "installed", active_path,
                    lambda _url, _maximum: b"",
                )


def fixture_bundle(root: Path, manifest_asset="assets/core.wasm", provider_api=1, *, legacy=False):
    target = {
        **({
            "gameCompatibilityLine": "fixture-v1",
            "netplayCompatibilityLine": None,
        } if legacy else {}),
        "assetPaths": [manifest_asset],
        "capabilities": {
            "checkpoint": False, "frameCounter": False, "frameMode": "NONE",
            "discSwitch": False, "inputFilter": False, "nativeSettings": False,
            "netplayPort": False, "pause": False, "requiresThreads": False,
            "screenshot": False, "standardGamepad": False,
            "validationProbes": [], "videoModes": [], "volume": False,
        },
        "checkpoint": None,
        "displayName": "Fixture",
        "id": "fixture",
        "inputs": [{"cardinality": "ONE", "kind": "ROM_BLOB", "optional": False, "role": "game"}],
        "targetOptionsSchema": {
            "additionalProperties": False, "properties": {}, "required": [], "type": "object",
        },
    }
    manifest = json_bytes({
        "clientModulePath": "client.mjs",
        "providerApiVersion": provider_api,
        "providerId": "fixture",
        "providerVersion": "1.0.0",
        "schemaVersion": 1,
        "targets": [target],
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


def legacy_active_fixture(root: Path):
    archive, lock = fixture_bundle(root, legacy=True)
    installed_root = root / "installed"
    installed = installed_root / "fixture" / lock["bundleSha256"]
    installed.mkdir(parents=True)
    with tarfile.open(archive, "r:gz") as source:
        for member in source:
            payload = source.extractfile(member)
            assert payload is not None
            target = installed / member.name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(payload.read())
    proof = {
        "bundleSha256": lock["bundleSha256"],
        "fileCount": lock["fileCount"],
        "manifestSha256": lock["manifestSha256"],
        "providerId": lock["providerId"],
        "providerVersion": lock["providerVersion"],
        "schemaVersion": 1,
        "unpackedSizeBytes": lock["unpackedSizeBytes"],
    }
    (installed / ".installation.json").write_text(json.dumps(proof), encoding="utf-8")
    manifest = json.loads((installed / "provider.json").read_text(encoding="utf-8"))
    integrity = json.loads((installed / "integrity.json").read_text(encoding="utf-8"))
    entries = {entry["path"]: entry for entry in integrity["files"]}
    target = manifest["targets"][0]
    contract = {
        "assets": [{
            "path": path,
            "sha256": entries[path]["sha256"],
            "sizeBytes": entries[path]["sizeBytes"],
        } for path in target["assetPaths"]],
        "schemaVersion": 1,
        "target": target,
    }
    active = {
        "providers": [{
            "bundleSha256": lock["bundleSha256"],
            "bundleSizeBytes": lock["bundleSizeBytes"],
            "clientModulePath": "client.mjs",
            "fileCount": lock["fileCount"],
            "installationPath": f'fixture/{lock["bundleSha256"]}',
            "manifestSha256": lock["manifestSha256"],
            "moduleSha256": entries["client.mjs"]["sha256"],
            "providerApiVersion": 1,
            "providerId": "fixture",
            "providerVersion": "1.0.0",
            "targets": [{
                "checkpoint": None,
                "gameCompatibilityLine": "fixture-v1",
                "id": "fixture",
                "netplayCompatibilityLine": None,
                "targetContractSha256": digest(canonical_json_bytes(contract)),
            }],
            "unpackedSizeBytes": lock["unpackedSizeBytes"],
        }],
        "release": None,
        "schemaVersion": 1,
        "source": "candidate",
        "sourceTreeSha256": "f" * 64,
    }
    active_path = root / "active.json"
    active_path.write_text(json.dumps(active), encoding="utf-8")
    return active_path, installed_root, active


def active_fixture(*, version, bundle, read_formats):
    checkpoint = {
        "maxBytes": 1024,
        "readFormats": read_formats,
        "writeFormat": read_formats[-1],
    }
    return {
        "providers": [{
            "bundleSha256": bundle * 64,
            "clientModulePath": "client.mjs",
            "installationPath": f"fixture/{bundle * 64}",
            "manifestSha256": "c" * 64,
            "moduleSha256": "d" * 64,
            "providerApiVersion": 1,
            "providerId": "fixture",
            "providerVersion": version,
            "targets": [{
                "checkpoint": checkpoint,
                "id": "fixture",
            }],
        }],
        "release": None,
        "schemaVersion": 1,
        "source": "candidate",
        "sourceTreeSha256": "f" * 64,
    }


def formal_release_metadata(lock):
    return {
        "providers": [{
            "archive": "fixture/fixture-provider-1.0.0.tar.gz",
            "bundleDirectory": "fixture/fixture-1.0.0",
            "bundleSha256": lock["bundleSha256"],
            "bundleSizeBytes": lock["bundleSizeBytes"],
            "fileCount": lock["fileCount"],
            "manifestSha256": lock["manifestSha256"],
            "providerId": "fixture",
            "providerVersion": "1.0.0",
            "unpackedSizeBytes": lock["unpackedSizeBytes"],
        }],
        "release": {
            "commit": "a" * 40,
            "repository": "https://github.com/retrom-project/retrom-runtime",
            "tag": "v0.12.0",
        },
        "schemaVersion": 1,
    }


def tar_member(name, payload, kind=tarfile.REGTYPE, linkname=""):
    info = tarfile.TarInfo(name)
    info.type = kind
    info.linkname = linkname
    info.size = len(payload) if kind == tarfile.REGTYPE else 0
    return info, payload
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
