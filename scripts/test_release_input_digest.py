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
    def test_backend_image_materializes_only_production_provider_locks(self) -> None:
        dockerfile = (SCRIPT.parent.parent / "Dockerfile").read_text(encoding="utf-8")
        ignore = (SCRIPT.parent.parent / ".dockerignore").read_text(encoding="utf-8")
        self.assertIn("COPY data/runtime-providers/*.lock.json locks/", dockerfile)
        self.assertIn("--source production", dockerfile)
        self.assertNotIn("rpg-runtime/registry", dockerfile)
        for mutable in ("active.json", "candidate-active.json", "installed", "cache", "archive"):
            self.assertIn(f"data/runtime-providers/{mutable}", ignore)

    def test_missing_production_locks_fail_closed(self) -> None:
        with tempfile.TemporaryDirectory(dir="/tmp") as temporary, \
                mock.patch.object(release_input, "ROOT", Path(temporary)):
            with self.assertRaisesRegex(ValueError, "RELEASE_INPUT_PROVIDER_LOCKS_MISSING"):
                release_input.provider_lock_entries()

    def test_candidate_or_mutable_provider_state_is_forbidden(self) -> None:
        with tempfile.TemporaryDirectory(dir="/tmp") as temporary:
            root = Path(temporary)
            (root / "data/runtime-providers/installed").mkdir(parents=True)
            with mock.patch.object(release_input, "ROOT", root):
                with self.assertRaisesRegex(ValueError, "RELEASE_INPUT_CANDIDATE"):
                    release_input.provider_lock_entries()

    def test_lock_digest_covers_both_providers_and_one_release_identity(self) -> None:
        with tempfile.TemporaryDirectory(dir="/tmp") as temporary:
            root = Path(temporary)
            locks = root / "data/runtime-providers"
            locks.mkdir(parents=True)
            for provider_id, version in (("emulatorjs", "1.0.0"), ("retrom-runtime", "0.12.0")):
                (locks / f"{provider_id}.lock.json").write_text(json.dumps(
                    lock_fixture(provider_id, version), separators=(",", ":"), sort_keys=True,
                ), encoding="utf-8")
            with mock.patch.object(release_input, "ROOT", root):
                entries = release_input.provider_lock_entries()
                candidate = root / ".pfb/candidates/runtime/providers"
                candidate.mkdir(parents=True)
                (candidate / "provider.json").write_text("private dev state", encoding="utf-8")
                self.assertEqual(entries, release_input.provider_lock_entries())
            self.assertEqual([entry["providerId"] for entry in entries], ["emulatorjs", "retrom-runtime"])
            self.assertTrue(all(len(entry["lockSha256"]) == 64 for entry in entries))


def lock_fixture(provider_id: str, version: str) -> dict[str, object]:
    return {
        "bundleSha256": "a" * 64,
        "bundleSizeBytes": 100,
        "bundleUrl": (
            "https://github.com/retrom-project/retrom-runtime/releases/download/"
            f"v0.12.0/{provider_id}-provider-{version}.tar.gz"
        ),
        "commit": "b" * 40,
        "fileCount": 6,
        "manifestSha256": "c" * 64,
        "providerId": provider_id,
        "providerVersion": version,
        "repository": "https://github.com/retrom-project/retrom-runtime",
        "schemaVersion": 1,
        "tag": "v0.12.0",
        "unpackedSizeBytes": 200,
    }


if __name__ == "__main__":
    unittest.main()
