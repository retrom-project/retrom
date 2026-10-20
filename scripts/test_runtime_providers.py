import hashlib
import io
import json
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path

from runtime_provider_bundle import (
    MEDIA_TYPES,
    _media_type,
    _installed_media_type_matches,
    check_installed_provider,
    describe_installed_provider,
    install_record,
)
from runtime_providers import (
    check_active_providers,
    prepare_candidate_providers,
    verify_provider_upgrade,
)


class RuntimeProviderInstallerTest(unittest.TestCase):
    def test_only_jsbeeb_provider_page_has_html_media_type(self):
        self.assertIn("text/html; charset=utf-8", MEDIA_TYPES)
        self.assertEqual(_media_type("assets/jsbeeb/site/index.html"), "text/html; charset=utf-8")
        self.assertEqual(_media_type("assets/other/index.html"), "application/octet-stream")
        self.assertTrue(_installed_media_type_matches("assets/jsbeeb/site/index.html", "application/octet-stream"))
        self.assertTrue(_installed_media_type_matches("assets/jsbeeb/site/index.html", "text/html; charset=utf-8"))
        self.assertFalse(_installed_media_type_matches("assets/other/index.html", "text/html; charset=utf-8"))

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

    def test_identifies_an_installed_bundle_by_its_integrity_digest(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            files, record = fixture_bundle()
            installed = install_fixture(root / "installed", files, record)
            self.assertEqual(record["bundleSha256"], digest(files["integrity.json"]))
            self.assertEqual(check_installed_provider(record, root / "installed"), installed)
            entry = describe_installed_provider(record, root / "installed")
            self.assertNotIn("bundleSizeBytes", entry)
            self.assertEqual(entry["installationPath"], f'fixture/{record["bundleSha256"]}')

    def test_rejects_invalid_records_and_a_bundle_that_is_not_the_pinned_one(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            files, record = fixture_bundle()
            for field, value in (("bundleSha256", "bad"), ("fileCount", 1), ("providerVersion", "v1")):
                with self.subTest(field=field), self.assertRaisesRegex(ValueError, "PROVIDER_RECORD_INVALID"):
                    install_record({**record, field: value})
            other, _ = fixture_bundle(provider_id="fixture", core=b"\x00asm\x01\x00\x00\x01")
            install_fixture(root / "installed", other, record)
            with self.assertRaisesRegex(ValueError, "PROVIDER_BUNDLE_DIGEST_INVALID"):
                check_installed_provider(record, root / "installed")

    def test_rejects_a_manifest_whose_asset_closure_differs_from_the_bundle(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            files, record = fixture_bundle(manifest_asset="assets/missing.wasm")
            install_fixture(root / "installed", files, record)
            with self.assertRaisesRegex(ValueError, "PROVIDER_MANIFEST_ASSET_CLOSURE_INVALID"):
                check_installed_provider(record, root / "installed")

    def test_rejects_an_unsupported_provider_api(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            files, record = fixture_bundle(provider_api=3)
            install_fixture(root / "installed", files, record)
            with self.assertRaisesRegex(ValueError, "RUNTIME_PROVIDER_API_UNSUPPORTED"):
                check_installed_provider(record, root / "installed")

    def test_revalidates_every_installed_byte(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            files, record = fixture_bundle()
            installed = install_fixture(root / "installed", files, record)
            original = (installed / "client.mjs").read_bytes()
            (installed / "client.mjs").write_bytes(original.upper())
            with self.assertRaisesRegex(ValueError, "PROVIDER_INTEGRITY_INVALID"):
                check_installed_provider(record, root / "installed")

    def test_installs_a_local_chunked_candidate_and_keeps_the_current_one_on_refusal(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            providers = root / "state/runtime-providers"
            newer = root / "newer"
            write_candidate(newer, "2.0.0")
            active = prepare_candidate_providers(newer, providers)
            self.assertEqual((active["source"], active["release"]), ("candidate", None))
            self.assertEqual(active["providers"][0]["providerVersion"], "2.0.0")
            self.assertNotIn("bundleSizeBytes", json.dumps(active))
            self.assertTrue((providers / "installed/fixture" / active["providers"][0]["bundleSha256"] / "provider.json").is_file())
            again = prepare_candidate_providers(newer, providers)
            self.assertEqual(again, active)
            older = root / "older"
            write_candidate(older, "1.0.0")
            before = (providers / "active.json").read_bytes()
            with self.assertRaisesRegex(ValueError, "RUNTIME_PROVIDER_DOWNGRADE_FORBIDDEN"):
                prepare_candidate_providers(older, providers)
            self.assertEqual((providers / "active.json").read_bytes(), before)
            self.assertEqual(sorted(path.name for path in providers.parent.iterdir()), ["runtime-providers"])

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

    def test_active_entries_must_match_the_installed_bundle_exactly(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            files, record = fixture_bundle()
            install_fixture(root / "installed", files, record)
            entry = describe_installed_provider(record, root / "installed")
            active = {"providers": [entry], "release": None, "schemaVersion": 1, "source": "candidate",
                      "sourceTreeSha256": "f" * 64}
            active_path = root / "active.json"
            active_path.write_text(json.dumps(active), encoding="utf-8")
            self.assertEqual(check_active_providers(active_path, root / "installed", "candidate"), active)
            active["providers"][0]["targets"][0]["gameCompatibilityLine"] = "retired"
            active_path.write_text(json.dumps(active), encoding="utf-8")
            with self.assertRaises(ValueError):
                check_active_providers(active_path, root / "installed", "candidate")


def fixture_bundle(manifest_asset="assets/core.wasm", provider_api=1, *, provider_id="fixture", runtime_source=None,
                   version="1.0.0", core=b"\x00asm\x01\x00\x00\x00"):
    """A contract-valid Provider bundle as files, plus its install record."""
    target = {
        "assetPaths": [manifest_asset],
        "capabilities": {
            "checkpoint": False, "frameCounter": False, "frameMode": "NONE",
            "inputFilter": False, "nativeSettings": False,
            "pause": False, "requiresThreads": False,
            "screenshot": False, "standardGamepad": False,
            "videoModes": [], "volume": False,
        },
        "checkpoint": None,
        "displayName": "Fixture",
        "id": "fixture",
        "inputs": [{"cardinality": "ONE", "kind": "ROM_BLOB", "optional": False, "role": "game", "maxFileBytes": None}],
        "targetOptionsSchema": {
            "additionalProperties": False, "properties": {}, "required": [], "type": "object",
        },
    }
    manifest = json_bytes({
        "clientModulePath": "client.mjs",
        "providerApiVersion": provider_api,
        "providerId": provider_id,
        "providerVersion": version,
        "schemaVersion": 1,
        "targets": [target],
    })
    files = {
        "assets/core.wasm": core,
        "client.mjs": f"export const providerId='{provider_id}';\n".encode(),
        "provider.json": manifest,
        "provenance.json": json_bytes({"schemaVersion": 1, **({"runtimeSourceTreeSha256": runtime_source} if runtime_source else {})}),
        "licenses/fixture/LICENSE": b"fixture license\n",
    }
    if runtime_source:
        files["runtime-fingerprints.json"] = json_bytes({"schemaVersion": 1, "providerId": provider_id,
                                                       "targets": {"fixture": {"fingerprint": "f" * 64, "inputs": []}}})
    files["integrity.json"] = json_bytes({
        "files": [{
            "mediaType": media_type(path), "path": path, "sha256": digest(contents),
            "sizeBytes": len(contents),
        } for path, contents in sorted(files.items())],
        "schemaVersion": 1,
    })
    record = {
        "bundleSha256": digest(files["integrity.json"]),
        "fileCount": len(files),
        "manifestSha256": digest(manifest),
        "providerId": provider_id,
        "providerVersion": version,
        "unpackedSizeBytes": sum(len(value) for value in files.values()),
    }
    return files, record


def install_fixture(installed_root: Path, files, record) -> Path:
    destination = installed_root / record["providerId"] / record["bundleSha256"]
    for path, contents in files.items():
        (destination / path).parent.mkdir(parents=True, exist_ok=True)
        (destination / path).write_bytes(contents)
    return destination


def chunk_release(release: Path, bundles) -> list:
    """Publish (files, record) bundles as chunk archives and manifests, exactly like the runtime."""
    release.mkdir(parents=True, exist_ok=True)
    records = []
    for files, record in bundles:
        provider_id, version = record["providerId"], record["providerVersion"]
        groups: dict[str, list[str]] = {}
        for path in files:
            groups.setdefault("/".join(path.split("/")[:2]) if path.startswith("assets/") else "", []).append(path)
        chunks = []
        for members in groups.values():
            members.sort(key=lambda path: path.encode())
            identity = [[path, len(files[path]), digest(files[path])] for path in members]
            chunk_id = digest(json.dumps(identity, separators=(",", ":"), ensure_ascii=False).encode())
            stream = io.BytesIO()
            with tarfile.open(fileobj=stream, mode="w:gz", format=tarfile.USTAR_FORMAT) as output:
                for path in members:
                    info = tarfile.TarInfo(path)
                    info.size, info.mode = len(files[path]), 0o644
                    output.addfile(info, io.BytesIO(files[path]))
            archive = stream.getvalue()
            name = f"{provider_id}-chunk-{chunk_id}.tar.gz"
            (release / name).write_bytes(archive)
            chunks.append({"archive": name, "archiveSha256": digest(archive), "archiveSizeBytes": len(archive),
                           "chunkId": chunk_id, "unpackedSizeBytes": sum(len(files[path]) for path in members),
                           "files": [{"path": path, "sha256": digest(files[path]), "sizeBytes": len(files[path])}
                                     for path in members]})
        chunks.sort(key=lambda chunk: chunk["chunkId"])
        manifest = json.dumps({"schemaVersion": 1, "providerId": provider_id, "providerVersion": version,
                               "bundleSha256": record["bundleSha256"], "chunks": chunks}).encode()
        manifest_name = f"{provider_id}-chunks-{version}.json"
        (release / manifest_name).write_bytes(manifest)
        records.append({**record, "chunkManifest": {"asset": manifest_name, "sha256": digest(manifest),
                                                    "sizeBytes": len(manifest)},
                        "moduleSha256": digest(files["client.mjs"])})
    return records


def write_candidate(release: Path, version: str) -> None:
    """A local runtime candidate release/ directory with one fixture Provider."""
    records = chunk_release(release, [fixture_bundle(version=version)])
    (release / "runtime-inputs.json").write_text(json.dumps({
        "schemaVersion": 1, "repository": "https://github.com/retrom-project/retrom-runtime",
        "sourceTreeSha256": "e" * 64, "release": None, "tool": {}, "providers": records}), encoding="utf-8")


def active_fixture(*, version, bundle, read_formats):
    checkpoint = {
        "maxBytes": 1024,
        "semantics": "INSTANT",
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
