"""A formal pin changes only after metadata and the complete byte closure verify."""
import copy
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock

from runtime_input_manifest import MAX_METADATA, REPOSITORY
from runtime_provider_release import pin_release
from test_runtime_inputs import paired_fixture, serve_release


class RuntimeReleaseTests(unittest.TestCase):
    def test_explicit_release_pin_verifies_metadata_every_chunk_and_pins_the_host_module(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            _, archives, manifest = paired_fixture(root)
            downloads = {"runtime-inputs.json": json.dumps(manifest).encode()}
            pin = root / "selected.json"
            module = Mock()
            with serve_release(archives) as (base, counts):
                fetch = Mock(side_effect=lambda url, maximum: downloads[url.removeprefix(base + "/")])
                self.assertEqual(pin_release("v1.0.0", pin, root / "cache", fetch=fetch, base_url=base,
                                             pin_go_module=module), manifest)
                self.assertEqual(fetch.call_count, 1)
                self.assertGreater(counts["chunks"], 0)
            module.assert_called_once_with("v1.0.0")
            self.assertEqual(json.loads(pin.read_text()), manifest)
            self.assertTrue(any((root / "cache/chunks").iterdir()))

    def test_bad_metadata_or_artifact_preserves_existing_pin(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            _, archives, manifest = paired_fixture(root)
            original = root / "selected.json"
            module = Mock()
            original.write_text("existing authoritative pin")
            wrong = copy.deepcopy(manifest)
            wrong["release"]["tag"] = "v2.0.0"
            for contents in (b"bad", b"[]", b" " * (MAX_METADATA + 1), json.dumps(wrong).encode()):
                with self.assertRaises(ValueError):
                    pin_release("v1.0.0", original, root / "cache", fetch=lambda *args: contents, pin_go_module=module)
                self.assertEqual(original.read_text(), "existing authoritative pin")
            manifests = [json.loads((archives / provider["chunkManifest"]["asset"]).read_text())
                         for provider in manifest["providers"]]
            chunk = archives / manifests[0]["chunks"][0]["archive"]
            chunk.write_bytes(chunk.read_bytes() + b"\0")
            with serve_release(archives) as (base, _):
                with self.assertRaisesRegex(ValueError, "PROVIDER_SYNC_FAILED"):
                    pin_release("v1.0.0", original, root / "cache", base_url=base, pin_go_module=module,
                                fetch=lambda *args: json.dumps(manifest).encode())
            self.assertEqual(original.read_text(), "existing authoritative pin")
            module.assert_not_called()

    def test_candidate_cannot_be_claimed_as_a_formal_release(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            _, _, manifest = paired_fixture(root, formal=False)
            with self.assertRaisesRegex(ValueError, "RELEASE_INVALID"):
                pin_release("v1.0.0", root / "pin", root / "cache", fetch=lambda *args: json.dumps(manifest).encode())


if __name__ == "__main__":
    unittest.main()
