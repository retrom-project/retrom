#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock
from test_runtime_inputs import paired_fixture


SCRIPT = Path(__file__).with_name("release-input-digest.py")
SPEC = importlib.util.spec_from_file_location("release_input_digest", SCRIPT)
assert SPEC and SPEC.loader
release_input = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release_input)


class ProviderInputTests(unittest.TestCase):
    def test_backend_image_requires_explicit_paired_inputs(self) -> None:
        dockerfile = (SCRIPT.parent.parent / "Dockerfile").read_text(encoding="utf-8")
        ignore = (SCRIPT.parent.parent / ".dockerignore").read_text(encoding="utf-8")
        self.assertIn("COPY --from=providers /active.json", dockerfile)
        self.assertIn("COPY --from=runtime-tool", dockerfile)
        self.assertNotIn("rpg-runtime/registry", dockerfile)
        for mutable in ("active.json", "candidate-active.json", "installed", "cache", "archive"):
            self.assertIn(f"data/runtime-providers/{mutable}", ignore)

    def test_missing_production_release_fails_closed(self) -> None:
        with tempfile.TemporaryDirectory(dir="/tmp") as temporary, \
                mock.patch.object(release_input, "ROOT", Path(temporary)):
            with self.assertRaisesRegex(ValueError, "RUNTIME_INPUT_MANIFEST_REQUIRED"):
                release_input.runtime_input()

    def test_candidate_or_mutable_provider_state_is_forbidden(self) -> None:
        with tempfile.TemporaryDirectory(dir="/tmp") as temporary:
            root = Path(temporary)
            (root / "data/runtime-providers/installed").mkdir(parents=True)
            with mock.patch.object(release_input, "ROOT", root):
                with self.assertRaisesRegex(ValueError, "RELEASE_INPUT_CANDIDATE"):
                    release_input.runtime_input()

    def test_release_digest_tracks_the_complete_pin_without_reading_pfb_state(self) -> None:
        with tempfile.TemporaryDirectory(dir="/tmp") as temporary:
            root = Path(temporary)
            pin, _, manifest, _ = paired_fixture(root, formal=False)
            config = root / "data/runtime-inputs.json"
            config.parent.mkdir(parents=True)
            config.write_bytes(pin.read_bytes())
            with mock.patch.object(release_input, "ROOT", root):
                entry = release_input.runtime_input()
                self.assertEqual(entry, manifest)
                candidate = root / ".pfb/candidates/runtime/providers"
                candidate.mkdir(parents=True)
                (candidate / "provider.json").write_text("private dev state")
                self.assertEqual(entry, release_input.runtime_input())
                manifest["sourceTreeSha256"] = "d" * 64
                config.write_text(json.dumps(manifest))
                self.assertNotEqual(entry, release_input.runtime_input())
                manifest["tag"] = "v1.0.0"
                config.write_text(json.dumps(manifest))
                with self.assertRaisesRegex(ValueError, "MANIFEST_INVALID"):
                    release_input.runtime_input()

    def test_symlink_to_mutable_release_config_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "data/runtime-inputs.json"
            config.parent.mkdir(parents=True)
            mutable = root / "candidate.json"
            mutable.write_text('{"tag":"v0.47.0"}')
            config.symlink_to(mutable)
            with mock.patch.object(release_input, "ROOT", root):
                with self.assertRaisesRegex(ValueError, "RUNTIME_INPUT_MANIFEST_REQUIRED"):
                    release_input.runtime_input()


if __name__ == "__main__":
    unittest.main()
