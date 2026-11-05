"""Paired preparation verifies bytes, provenance and publication as one input set."""
import contextlib
import copy
import functools
import hashlib
import http.server
import json
import tempfile
import threading
import unittest
from pathlib import Path
from runtime_input_manifest import REPOSITORY, check_module_pin, module_version, validate_manifest
from runtime_inputs import prepare, verify_prepared
from runtime_input_transport import transport_base
from test_runtime_providers import chunk_release, fixture_bundle


def paired_fixture(root, *, formal=True, source="c" * 64):
    """A runtime release directory (Provider chunks) and its pin."""
    archives = root / "inputs"
    providers = chunk_release(archives, [fixture_bundle(provider_id=provider_id, runtime_source=source)
                                         for provider_id in ("emulatorjs", "retrom-runtime")])
    manifest = {"schemaVersion": 2, "repository": REPOSITORY, "sourceTreeSha256": source,
                "release": {"repository": REPOSITORY, "tag": "v1.0.0", "commit": "a" * 40} if formal else None,
                "providers": providers}
    pin = root / "runtime-inputs.json"
    pin.write_text(json.dumps(manifest))
    return pin, archives, manifest


@contextlib.contextmanager
def serve_release(directory):
    """A loopback mirror of a release directory that counts chunk requests."""
    counts = {"chunks": 0}

    class Handler(http.server.SimpleHTTPRequestHandler):
        def do_GET(self):  # noqa: N802 - http.server API
            if "-chunk-" in self.path:
                counts["chunks"] += 1
            super().do_GET()

        def log_message(self, *args):
            pass

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), functools.partial(Handler, directory=str(directory)))
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield f"http://127.0.0.1:{server.server_address[1]}", counts
    finally:
        server.shutdown()
        server.server_close()


class RuntimeInputsTests(unittest.TestCase):
    def test_formal_cold_then_offline_preparation_and_candidate_local_use_one_shape(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pin, archives, manifest = paired_fixture(root)
            with serve_release(archives) as (base, counts):
                prepared = prepare(pin, root / "output", root / "cache", base_url=base)
                cold = counts["chunks"]
                self.assertGreater(cold, 0)
                self.assertEqual(sorted(path.name for path in prepared.iterdir()), ["providers", "runtime-inputs.json"])
                self.assertEqual(json.loads((prepared / "providers/active.json").read_text())["release"],
                                 manifest["release"])
                self.assertEqual(prepare(pin, root / "output", root / "cache", base_url=base), prepared)
                second = prepare(pin, root / "second", root / "cache", base_url=base)
                verify_prepared(second, manifest)
                # The second consumer installs every Provider file from the shared chunk cache.
                self.assertEqual(counts["chunks"], cold)
            manifest["release"] = None
            pin.write_text(json.dumps(manifest))
            candidate = prepare(pin, root / "candidate", root / "local-cache", archive_root=archives)
            self.assertEqual(json.loads((candidate / "providers/active.json").read_text())["source"], "candidate")

    def test_invalid_manifest_fields_and_cross_source_artifacts_never_publish(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pin, archives, manifest = paired_fixture(root, formal=False)
            for field, value in (("sourceTreeSha256", "bad"), ("schemaVersion", True), ("schemaVersion", 1),
                                 ("release", {"tag": "v1.0.0"}),
                                 ("tool", {"archive": "retrom-runtime-host-tool-1.0.0.tar.gz", "sizeBytes": 1,
                                           "sha256": "a" * 64})):
                changed = copy.deepcopy(manifest)
                changed[field] = value
                with self.assertRaises(ValueError):
                    validate_manifest(changed)
            malformed = []
            for field, value in (("chunkManifest", {"asset": "../escape.json", "sha256": "a" * 64, "sizeBytes": 1}),
                                 ("providerVersion", "2.0.0"), ("bundleSha256", "bad"), ("fileCount", 100001),
                                 ("moduleSha256", "bad"), ("archive", "retired.tar.gz")):
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

    def test_formal_pin_and_host_module_share_one_release_tag(self):
        go_mod = "module retrom\n\nrequire (\n\tgithub.com/retrom-project/retrom-runtime v1.0.0\n)\n"
        self.assertEqual(module_version(go_mod), "v1.0.0")
        formal = {"release": {"repository": REPOSITORY, "tag": "v1.0.0", "commit": "a" * 40}}
        check_module_pin(formal, go_mod)
        check_module_pin({"release": None}, go_mod.replace("v1.0.0", "v1.0.1-0.20260101000000-abcdefabcdef"))
        with self.assertRaisesRegex(ValueError, "MODULE_MISMATCH"):
            check_module_pin(formal, go_mod.replace("v1.0.0", "v1.0.1"))
        with self.assertRaisesRegex(ValueError, "MODULE_REQUIRED"):
            check_module_pin(formal, "module retrom\n")

    def test_corrupt_chunk_never_publishes_a_prepared_set(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pin, archives, manifest = paired_fixture(root, formal=False)
            # Corrupt a chunk only the first Provider has: an identical chunk of another Provider
            # shares its content identity and may already be verified in the cache.
            manifests = [json.loads((archives / provider["chunkManifest"]["asset"]).read_text())
                         for provider in manifest["providers"]]
            shared = {chunk["chunkId"] for chunk in manifests[1]["chunks"]}
            unique = next(chunk for chunk in manifests[0]["chunks"] if chunk["chunkId"] not in shared)
            chunk = archives / unique["archive"]
            chunk.write_bytes(chunk.read_bytes() + b"\0")
            with self.assertRaisesRegex(ValueError, "PROVIDER_CHUNK_INVALID"):
                prepare(pin, root / "output", root / "cache", archive_root=archives)
            self.assertEqual(list((root / "output").glob("[0-9a-f]*")), [])

    def test_unpublished_missing_transport_has_no_old_release_fallback(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pin, _, _ = paired_fixture(root, formal=False)
            with self.assertRaisesRegex(ValueError, "PROVIDER_SOURCE_REQUIRED"):
                prepare(pin, root / "output", root / "cache")
            self.assertEqual(list((root / "output").glob("[0-9a-f]*")), [])

    def test_only_loopback_mirrors_may_use_plain_http(self):
        manifest = {"repository": REPOSITORY, "release": None}
        self.assertEqual(transport_base(manifest, "http://127.0.0.1:8080/"), "http://127.0.0.1:8080")
        self.assertEqual(transport_base(manifest, "https://mirror.example/r"), "https://mirror.example/r")
        for value in ("http://mirror.example/r", "ftp://127.0.0.1/r", "https://user:pw@mirror.example/r"):
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, "TRANSPORT_INVALID"):
                transport_base(manifest, value)

    def test_edited_integrity_file_is_not_the_pinned_bundle(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pin, archives, manifest = paired_fixture(root, formal=False)
            prepared = prepare(pin, root / "output", root / "cache", archive_root=archives)
            provider = manifest["providers"][0]
            installed = prepared / "providers/installed" / provider["providerId"] / provider["bundleSha256"]
            changed = b"\x00asm\x02\x00\x00\x00"
            # Break the shared hard link before editing, as a careless local edit would.
            (installed / "assets/core.wasm").unlink()
            (installed / "assets/core.wasm").write_bytes(changed)
            integrity_path = installed / "integrity.json"
            integrity = json.loads(integrity_path.read_text())
            entry = next(file for file in integrity["files"] if file["path"] == "assets/core.wasm")
            entry["sizeBytes"], entry["sha256"] = len(changed), hashlib.sha256(changed).hexdigest()
            integrity_path.unlink()
            integrity_path.write_text(json.dumps(integrity, ensure_ascii=False, indent=2) + "\n")
            # A consistent local rehash still cannot match the pinned bundle identity.
            with self.assertRaisesRegex(ValueError, "PROVIDER_BUNDLE_DIGEST_INVALID"):
                verify_prepared(prepared, manifest)


if __name__ == "__main__":
    unittest.main()
