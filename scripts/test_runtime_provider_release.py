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
    def test_explicit_release_pin_verifies_metadata_tool_and_every_chunk(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            _, archives, manifest, downloads = paired_fixture(root)
            downloads["runtime-inputs.json"] = json.dumps(manifest).encode()
            pin = root / "selected.json"
            with serve_release(archives) as (base, counts):
                fetch = Mock(side_effect=lambda url, maximum: downloads[url.removeprefix(base + "/")])
                self.assertEqual(pin_release("v1.0.0", pin, root / "cache", fetch=fetch, base_url=base), manifest)
                self.assertEqual(fetch.call_count, 2)
                self.assertGreater(counts["chunks"], 0)
            self.assertEqual(json.loads(pin.read_text()), manifest)
            self.assertTrue(any((root / "cache/chunks").iterdir()))

    def test_bad_metadata_or_artifact_preserves_existing_pin(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            _, _, manifest, downloads = paired_fixture(root)
            original = root / "selected.json"
            original.write_text("existing authoritative pin")
            wrong = copy.deepcopy(manifest)
            wrong["release"]["tag"] = "v2.0.0"
            for contents in (b"bad", b"[]", b" " * (MAX_METADATA + 1), json.dumps(wrong).encode()):
                with self.assertRaises(ValueError):
                    pin_release("v1.0.0", original, root / "cache", fetch=lambda *args: contents)
                self.assertEqual(original.read_text(), "existing authoritative pin")
            def fetch(url, maximum):
                return json.dumps(manifest).encode() if url.endswith(".json") else b"corrupt"
            with self.assertRaisesRegex(ValueError, "ARTIFACT_DIGEST_INVALID"):
                pin_release("v1.0.0", original, root / "cache", fetch=fetch)
            self.assertEqual(original.read_text(), "existing authoritative pin")
            self.assertEqual(list((root / "cache").rglob("*.tar.gz")), [])

    def test_candidate_cannot_be_claimed_as_a_formal_release(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            _, _, manifest, _ = paired_fixture(root, formal=False)
            with self.assertRaisesRegex(ValueError, "RELEASE_INVALID"):
                pin_release("v1.0.0", root / "pin", root / "cache", fetch=lambda *args: json.dumps(manifest).encode())


if __name__ == "__main__":
    unittest.main()
