#!/usr/bin/env python3
"""Malformed-manifest regression tests for the dependency validator."""

from __future__ import annotations

import copy
import importlib.util
import json
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import dependencies


RELEASE_DIGEST_SPEC = importlib.util.spec_from_file_location(
    "release_input_digest", Path(__file__).with_name("release-input-digest.py")
)
if RELEASE_DIGEST_SPEC is None or RELEASE_DIGEST_SPEC.loader is None:
    raise RuntimeError("RELEASE_INPUT_TEST_MODULE_INVALID")
release_input_digest = importlib.util.module_from_spec(RELEASE_DIGEST_SPEC)
RELEASE_DIGEST_SPEC.loader.exec_module(release_input_digest)


MANIFEST_PATH = (
    Path(__file__).resolve().parents[1]
    / "data/dat/emulatorjs/4.2.3/manifest.json"
)


class DependencyManifestValidationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.baseline = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
        dependencies.validate_small_manifest("4.2.3", cls.baseline)

    def assert_invalid(self, mutate: object, code: str) -> None:
        manifest = copy.deepcopy(self.baseline)
        mutate(manifest)
        with self.assertRaisesRegex(dependencies.CheckError, code):
            dependencies.validate_small_manifest("4.2.3", manifest)

    def test_rejects_duplicate_product_and_runtime_core_ids(self) -> None:
        self.assert_invalid(
            lambda manifest: manifest["emulatorjs"]["selected_core_artifacts"].__setitem__(
                1,
                {
                    **manifest["emulatorjs"]["selected_core_artifacts"][1],
                    "core_id": manifest["emulatorjs"]["selected_core_artifacts"][0]["core_id"],
                },
            ),
            "DEPENDENCY_CORE_INVALID",
        )
        self.assert_invalid(
            lambda manifest: manifest["emulatorjs"]["selected_core_artifacts"][1].__setitem__(
                "runtime_core_id",
                manifest["emulatorjs"]["selected_core_artifacts"][0]["runtime_core_id"],
            ),
            "DEPENDENCY_RUNTIME_CORE_DUPLICATE",
        )

    def test_rejects_basename_traversal_and_thread_mismatch(self) -> None:
        self.assert_invalid(
            lambda manifest: manifest["emulatorjs"]["selected_core_artifacts"][0].__setitem__(
                "requested_artifact_basename", "../core-wasm.data"
            ),
            "DEPENDENCY_ARTIFACT_BASENAME_INVALID",
        )
        self.assert_invalid(
            lambda manifest: manifest["emulatorjs"]["selected_core_artifacts"][0].__setitem__(
                "requires_threads",
                not manifest["emulatorjs"]["selected_core_artifacts"][0]["requires_threads"],
            ),
            "DEPENDENCY_ARTIFACT_THREAD_MISMATCH",
        )

    def test_rejects_auxiliary_file_outside_allowlist_and_unknown_component(self) -> None:
        self.assert_invalid(
            lambda manifest: manifest["emulatorjs"]["auxiliary_files"][0].__setitem__(
                "path_in_release", "data/cores/not-allowlisted.zip"
            ),
            "DEPENDENCY_AUXILIARY_ALLOWLIST_INVALID",
        )
        self.assert_invalid(
            lambda manifest: manifest["emulatorjs"]["selected_core_artifacts"][0].__setitem__(
                "source_component_id", "missing-component"
            ),
            "DEPENDENCY_CORE_COMPONENT_INVALID",
        )

    def test_rejects_empty_license_and_dangerous_option_key(self) -> None:
        self.assert_invalid(
            lambda manifest: manifest["license_materialization"]["components"][0].__setitem__(
                "license_files", []
            ),
            "DEPENDENCY_LICENSE_FILES_EMPTY",
        )
        self.assert_invalid(
            lambda manifest: manifest["emulatorjs"]["selected_core_artifacts"][0][
                "default_options"
            ].__setitem__("__proto__", "unsafe"),
            "DEPENDENCY_CORE_OPTIONS_INVALID",
        )

    def test_rejects_invalid_startup_action_and_none_save_kind(self) -> None:
        def invalid_action(manifest: dict[str, object]) -> None:
            artifact = next(
                item
                for item in manifest["emulatorjs"]["selected_core_artifacts"]
                if item["core_id"] == "ppsspp"
            )
            artifact["startup_actions"][0]["kind"] = "ARBITRARY_SCRIPT"

        self.assert_invalid(invalid_action, "DEPENDENCY_STARTUP_ACTION_INVALID")

        def invalid_none_kind(manifest: dict[str, object]) -> None:
            artifact = next(
                item
                for item in manifest["emulatorjs"]["selected_core_artifacts"]
                if item["persistent_save_mode"] == "NONE"
            )
            artifact["persistent_save_kind"] = "CORE_SAVE"

        self.assert_invalid(invalid_none_kind, "DEPENDENCY_PERSISTENT_SAVE_INVALID")


class ReleaseInputDigestTests(unittest.TestCase):
    def test_unstaged_delete_is_absent_and_untracked_rename_is_present(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "--quiet"], cwd=root, check=True)
            old_path = root / "parallel-n64.txt"
            old_path.write_text("old\n", encoding="utf-8")
            subprocess.run(["git", "add", old_path.name], cwd=root, check=True)
            old_path.unlink()
            new_path = root / "parallel_n64.txt"
            new_path.write_text("new\n", encoding="utf-8")

            with mock.patch.object(release_input_digest, "ROOT", root):
                entries = release_input_digest.source_entries()

            self.assertEqual([entry["path"] for entry in entries], [new_path.name])


class DependencyMaterializationTests(unittest.TestCase):
    def test_identical_notice_preserves_file_identity_and_timestamp(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "THIRD_PARTY_NOTICES"
            self.assertTrue(dependencies.publish_bytes_if_changed(target, b"notice\n"))
            initial = target.stat()

            self.assertFalse(dependencies.publish_bytes_if_changed(target, b"notice\n"))
            unchanged = target.stat()
            self.assertEqual(unchanged.st_ino, initial.st_ino)
            self.assertEqual(unchanged.st_mtime_ns, initial.st_mtime_ns)

            self.assertTrue(dependencies.publish_bytes_if_changed(target, b"updated\n"))
            self.assertEqual(target.read_bytes(), b"updated\n")
            self.assertEqual(target.stat().st_mode & 0o777, 0o600)


if __name__ == "__main__":
    unittest.main()
