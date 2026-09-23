#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).with_name("release-input-digest.py")
SPEC = importlib.util.spec_from_file_location("release_input_digest", SCRIPT)
assert SPEC and SPEC.loader
release_input = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release_input)


class ProviderInputTests(unittest.TestCase):
    def test_backend_image_materializes_only_production_release_tag(self) -> None:
        dockerfile = (SCRIPT.parent.parent / "Dockerfile").read_text(encoding="utf-8")
        ignore = (SCRIPT.parent.parent / ".dockerignore").read_text(encoding="utf-8")
        self.assertIn("COPY data/runtime-providers/release.json runtime-release.json", dockerfile)
        self.assertIn("--source production", dockerfile)
        self.assertNotIn("rpg-runtime/registry", dockerfile)
        for mutable in ("active.json", "candidate-active.json", "installed", "cache", "archive"):
            self.assertIn(f"data/runtime-providers/{mutable}", ignore)

    def test_missing_production_release_fails_closed(self) -> None:
        with tempfile.TemporaryDirectory(dir="/tmp") as temporary, \
                mock.patch.object(release_input, "ROOT", Path(temporary)):
            with self.assertRaisesRegex(ValueError, "RELEASE_INPUT_PROVIDER_RELEASE_MISSING"):
                release_input.provider_release_input()

    def test_candidate_or_mutable_provider_state_is_forbidden(self) -> None:
        with tempfile.TemporaryDirectory(dir="/tmp") as temporary:
            root = Path(temporary)
            (root / "data/runtime-providers/installed").mkdir(parents=True)
            with mock.patch.object(release_input, "ROOT", root):
                with self.assertRaisesRegex(ValueError, "RELEASE_INPUT_CANDIDATE"):
                    release_input.provider_release_input()

    def test_release_digest_tracks_the_tag_without_reading_pfb_state(self) -> None:
        with tempfile.TemporaryDirectory(dir="/tmp") as temporary:
            root = Path(temporary)
            config = root / "data/runtime-providers/release.json"
            config.parent.mkdir(parents=True)
            config.write_text('{"tag":"v0.46.0"}')
            with mock.patch.object(release_input, "ROOT", root):
                entry = release_input.provider_release_input()
                self.assertEqual(entry, {"repository": release_input.REPOSITORY, "tag": "v0.46.0"})
                candidate = root / ".pfb/candidates/runtime/providers"
                candidate.mkdir(parents=True)
                (candidate / "provider.json").write_text("private dev state", encoding="utf-8")
                cache = root / ".cache/runtime-providers/releases/v0.46.0"
                cache.mkdir(parents=True)
                (cache / "provider-release.json").write_text("mutable local cache")
                self.assertEqual(entry, release_input.provider_release_input())
                config.write_text('{"tag":"v0.47.0"}')
                self.assertNotEqual(entry, release_input.provider_release_input())
                config.write_text('{"tag":"v0.47.0", "bundleSha256":"old-lock"}')
                with self.assertRaisesRegex(ValueError, "PROVIDER_RELEASE_CONFIG_INVALID"):
                    release_input.provider_release_input()

    def test_symlink_to_mutable_release_config_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "data/runtime-providers/release.json"
            config.parent.mkdir(parents=True)
            mutable = root / "candidate.json"
            mutable.write_text('{"tag":"v0.47.0"}')
            config.symlink_to(mutable)
            with mock.patch.object(release_input, "ROOT", root):
                with self.assertRaisesRegex(ValueError, "RELEASE_INPUT_PROVIDER_RELEASE_MISSING"):
                    release_input.provider_release_input()


if __name__ == "__main__":
    unittest.main()
