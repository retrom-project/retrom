#!/usr/bin/env python3
"""Malformed-manifest regression tests for the dependency validator."""

from __future__ import annotations

import copy
import importlib.util
import json
import subprocess
import sys
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

RPG_BUILD_SPEC = importlib.util.spec_from_file_location(
    "rpg_runtime_build",
    Path(__file__).resolve().parents[1] / "data/dat/rpgmaker/v1/build.py",
)
if RPG_BUILD_SPEC is None or RPG_BUILD_SPEC.loader is None:
    raise RuntimeError("RPG_RUNTIME_BUILD_TEST_MODULE_INVALID")
rpg_runtime_build = importlib.util.module_from_spec(RPG_BUILD_SPEC)
RPG_BUILD_DIRECTORY = str(Path(RPG_BUILD_SPEC.origin).resolve().parent)
sys.path.insert(0, RPG_BUILD_DIRECTORY)
try:
    RPG_BUILD_SPEC.loader.exec_module(rpg_runtime_build)
finally:
    sys.path.remove(RPG_BUILD_DIRECTORY)


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

    def test_rejects_artifact_set_digest_and_adapter_abi_drift(self) -> None:
        self.assert_invalid(
            lambda manifest: manifest["emulatorjs"]["selected_core_artifacts"][0].__setitem__(
                "artifact_set_sha256", "0" * 64
            ),
            "DEPENDENCY_ARTIFACT_SET_SHA256_INVALID",
        )
        self.assert_invalid(
            lambda manifest: manifest["emulatorjs"]["selected_core_artifacts"][0].__setitem__(
                "adapter_abi", "latest"
            ),
            "DEPENDENCY_ARTIFACT_ADAPTER_ABI_INVALID",
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

    def test_rejects_invalid_startup_action(self) -> None:
        def invalid_action(manifest: dict[str, object]) -> None:
            artifact = next(
                item
                for item in manifest["emulatorjs"]["selected_core_artifacts"]
                if item["core_id"] == "ppsspp"
            )
            artifact["startup_actions"][0]["kind"] = "ARBITRARY_SCRIPT"

        self.assert_invalid(invalid_action, "DEPENDENCY_STARTUP_ACTION_INVALID")

    def test_startup_action_delay_accepts_30_seconds_and_rejects_one_more_ms(self) -> None:
        def set_delay(manifest: dict[str, object], delay_ms: int) -> None:
            artifact = next(
                item
                for item in manifest["emulatorjs"]["selected_core_artifacts"]
                if item["core_id"] == "ppsspp"
            )
            artifact["startup_actions"][0]["delayMs"] = delay_ms

        accepted = copy.deepcopy(self.baseline)
        set_delay(accepted, 30_000)
        dependencies.validate_small_manifest("4.2.3", accepted)
        self.assert_invalid(
            lambda manifest: set_delay(manifest, 30_001),
            "DEPENDENCY_STARTUP_ACTION_INVALID",
        )

    def test_rejects_fbalpha_generation_gate_drift(self) -> None:
        def invalid_machine_count(manifest: dict[str, object]) -> None:
            core = next(item for item in manifest["cores"] if item["core_id"] == "fbalpha2012_cps1")
            core["parse_stats"]["machine_count"] = 226

        self.assert_invalid(invalid_machine_count, "DEPENDENCY_FBA2012_DAT_GATE_INVALID")

        def invalid_parent_mapping(manifest: dict[str, object]) -> None:
            core = next(item for item in manifest["cores"] if item["core_id"] == "fbalpha2012_cps2")
            core["dat"]["materialization"]["expected_normalized_external_parents"] = [
                "other->megaman"
            ]

        self.assert_invalid(invalid_parent_mapping, "DEPENDENCY_FBA2012_DAT_GATE_INVALID")

    def test_rejects_implicit_or_unverified_multi_disc_capabilities(self) -> None:
        def missing_content_kinds(manifest: dict[str, object]) -> None:
            manifest["emulatorjs"]["selected_core_artifacts"][0].pop("supported_content_kinds")

        self.assert_invalid(missing_content_kinds, "DEPENDENCY_ARTIFACT_CAPABILITY_INVALID")

        def unsupported_core(manifest: dict[str, object]) -> None:
            artifact = manifest["emulatorjs"]["selected_core_artifacts"][0]
            artifact["supported_content_kinds"].append("MULTI_DISC_M3U_V1")
            artifact["multi_disc"] = {
                "max_discs": 8,
                "max_total_bytes": 1_073_741_824,
                "delivery": "EAGER_EXTERNAL_FILES",
            }

        self.assert_invalid(unsupported_core, "DEPENDENCY_MULTI_DISC_CAPABILITY_INVALID")

        def invalid_limit(manifest: dict[str, object]) -> None:
            artifact = next(
                item
                for item in manifest["emulatorjs"]["selected_core_artifacts"]
                if item["core_id"] == "yabause"
            )
            artifact["multi_disc"]["max_discs"] = 9

        self.assert_invalid(invalid_limit, "DEPENDENCY_MULTI_DISC_CAPABILITY_INVALID")


class DependencyVersionValidationTests(unittest.TestCase):
    def test_accepts_ordered_prerelease_runtime_overlay(self) -> None:
        self.assertEqual(
            dependencies.parse_versions("4.2.3,4.3.0-pre"),
            ["4.2.3", "4.3.0-pre"],
        )

    def test_rejects_prerelease_after_its_release_and_numeric_leading_zero(self) -> None:
        with self.assertRaisesRegex(dependencies.CheckError, "DEPENDENCY_VERSION_LIST_NOT_SORTED"):
            dependencies.parse_versions("4.3.0,4.3.0-pre")
        with self.assertRaisesRegex(dependencies.CheckError, "DEPENDENCY_VERSION_INVALID"):
            dependencies.parse_versions("4.3.0-pre.01")


class RPGMakerDependencyTests(unittest.TestCase):
    def setUp(self) -> None:
        self.manifest = rpg_runtime_build.load_manifest()
        rpg_runtime_build.validate_manifest(self.manifest)

    def test_manifest_has_one_release_and_no_local_build_inputs(self) -> None:
        self.assertEqual(
            {"schema_version", "runtime_id", "release", "runtime_files", "artifacts"},
            set(self.manifest),
        )
        self.assertEqual(10, len(self.manifest["artifacts"]))
        self.assertEqual(
            ["KIRIKIRI2_KAG"],
            [
                item["route_key"]
                for item in self.manifest["artifacts"]
                if item["runtime_family"] == "KIRIKIRI"
            ],
        )
        serialized = json.dumps(self.manifest)
        for stale in ("source_archives", "runtime_releases", "recipe_path", "retrom-web-"):
            self.assertNotIn(stale, serialized)

    def test_rejects_old_route_and_parallel_version_directory(self) -> None:
        old_route = copy.deepcopy(self.manifest)
        old_route["artifacts"][0]["route_key"] = "RPG2000_UNDECLARED"
        with self.assertRaisesRegex(
            rpg_runtime_build.BuildError, "RPG_RUNTIME_ARTIFACT_ROUTE_INVALID"
        ):
            rpg_runtime_build.validate_manifest(old_route)

        old_directory = copy.deepcopy(self.manifest)
        old_directory["runtime_files"][0]["path_in_release"] = "v7/easyrpg-player.js"
        with self.assertRaisesRegex(
            rpg_runtime_build.BuildError, "RPG_RUNTIME_FILE_DECLARATION_INVALID"
        ):
            rpg_runtime_build.validate_manifest(old_directory)

    def test_player_registry_matches_every_runtime_route(self) -> None:
        registry = dependencies.load_json(
            dependencies.RPG_MAKER_PLAYER_REGISTRY_PATH
        )
        dependencies.validate_rpg_maker_registry(self.manifest, registry)

        drifted = copy.deepcopy(registry)
        drifted["routes"][0]["routeKey"] = "RPG2000_LATEST"
        with self.assertRaisesRegex(
            dependencies.CheckError, "RPG_PLAYER_REGISTRY_DRIFT"
        ):
            dependencies.validate_rpg_maker_registry(self.manifest, drifted)

    def test_player_registry_rejects_unknown_fields_and_split_implementation(self) -> None:
        registry = dependencies.load_json(
            dependencies.RPG_MAKER_PLAYER_REGISTRY_PATH
        )
        unknown = copy.deepcopy(registry)
        unknown["routes"][0]["fallback"] = True
        with self.assertRaisesRegex(
            dependencies.CheckError, "RPG_PLAYER_REGISTRY_INVALID"
        ):
            dependencies.validate_rpg_maker_registry(self.manifest, unknown)

        split = copy.deepcopy(registry)
        split["routes"][1]["implementation"] = "contract.ts"
        with self.assertRaisesRegex(
            dependencies.CheckError, "RPG_PLAYER_IMPLEMENTATION_DRIFT"
        ):
            dependencies.validate_rpg_maker_registry(self.manifest, split)


class NetplayManifestValidationTests(unittest.TestCase):
    def test_release_netplay_manifest_matches_adapters_and_core_artifacts(self) -> None:
        manifests = [
            dependencies.load_manifest("4.2.3"),
            dependencies.load_manifest("4.3.0-pre"),
        ]
        dependencies.validate_netplay_manifest(
            manifests,
            {
                "ejs-4.2.3-v2": "4.2.3",
                "ejs-4.2.3-v3": "4.2.3",
                "ejs-4.3.0-pre-v2": "4.3.0-pre",
            },
            {"ejs-netplay-4.2.3-v1": "4.2.3"},
        )

    def test_rejects_unregistered_netplay_adapter(self) -> None:
        manifests = [
            dependencies.load_manifest("4.2.3"),
            dependencies.load_manifest("4.3.0-pre"),
        ]
        with self.assertRaisesRegex(
            dependencies.CheckError, "NETPLAY_ADAPTER_REGISTRY_DRIFT"
        ):
            dependencies.validate_netplay_manifest(
                manifests,
                {
                    "ejs-4.2.3-v2": "4.2.3",
                    "ejs-4.2.3-v3": "4.2.3",
                    "ejs-4.3.0-pre-v2": "4.3.0-pre",
                },
                {},
            )


class CPSFixtureLayoutValidationTests(unittest.TestCase):
    def test_production_registry_validation_does_not_depend_on_test_fixtures(self) -> None:
        manifests = [
            dependencies.load_manifest("4.2.3"),
            dependencies.load_manifest("4.3.0-pre"),
        ]
        with mock.patch.object(
            dependencies,
            "CPS_FIXTURE_LAYOUT_PATH",
            Path("/fixture-source-must-not-be-required-by-production-prepare"),
        ):
            dependencies.validate_registry(manifests)

    def test_layout_matches_locked_source_without_production_dat(self) -> None:
        manifests = [
            dependencies.load_manifest("4.2.3"),
            dependencies.load_manifest("4.3.0-pre"),
        ]
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(
            dependencies, "DATA_ROOT", Path(directory)
        ):
            dependencies.validate_cps_fixture_layouts(
                manifests, require_production_dat=False
            )

    def test_data_check_dispatch_does_not_require_production_dat(self) -> None:
        arguments = [
            "dependencies.py",
            "data-check",
            "--versions",
            "4.2.3,4.3.0-pre",
        ]
        with mock.patch.object(
            dependencies,
            "file_digest",
            side_effect=AssertionError("data-check must not read payload files"),
        ), mock.patch.object(
            dependencies,
            "validate_cps_fixture_layouts",
            wraps=dependencies.validate_cps_fixture_layouts,
        ) as validate_layouts, mock.patch.object(sys, "argv", arguments), mock.patch(
            "builtins.print"
        ):
            self.assertEqual(dependencies.main(), 0)
        validate_layouts.assert_called_once()
        self.assertFalse(validate_layouts.call_args.kwargs["require_production_dat"])

    def test_payload_layout_validation_still_requires_production_dat(self) -> None:
        manifests = [
            dependencies.load_manifest("4.2.3"),
            dependencies.load_manifest("4.3.0-pre"),
        ]
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(
            dependencies, "DATA_ROOT", Path(directory)
        ), self.assertRaisesRegex(
            dependencies.CheckError,
            "DEPENDENCY_FILE_MISSING:fbalpha2012-cps1.dat",
        ):
            dependencies.validate_cps_fixture_layouts(
                manifests, require_production_dat=True
            )

    def test_rejects_source_commit_drift(self) -> None:
        manifests = [dependencies.load_manifest("4.2.3")]
        core = next(
            item
            for item in manifests[0]["cores"]
            if item["core_id"] == "fbalpha2012_cps1"
        )
        core["core_source"]["commit"] = "0" * 40
        with self.assertRaisesRegex(
            dependencies.CheckError,
            "CPS_FIXTURE_LAYOUT_INVALID",
        ):
            dependencies.validate_cps_fixture_layouts(
                manifests, require_production_dat=False
            )


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

    def test_rpg_runtime_manifest_is_a_named_release_input(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "--quiet"], cwd=root, check=True)
            files = {
                "data/dat/emulatorjs/1.0.0/manifest.json": {
                    "emulatorjs": {"player_adapter": {"id": "fixture-adapter"}}
                },
                "data/auth/password-blocklists/v1/manifest.json": {},
                "data/netplay/v2/manifest.json": {},
                "data/dat/rpgmaker/v1/manifest.json": {
                    "schema_version": 3,
                    "runtime_id": "retrom-runtime",
                    "release": {"tag": "v0.4.2"},
                    "runtime_files": [{} for _ in range(8)],
                    "artifacts": [{} for _ in range(7)],
                },
            }
            for relative, value in files.items():
                target = root / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(json.dumps(value), encoding="utf-8")
            with mock.patch.object(release_input_digest, "ROOT", root):
                value = release_input_digest.release_input_value(["1.0.0"], "1.0.0")
            expected = release_input_digest.sha256(
                (root / "data/dat/rpgmaker/v1/manifest.json").read_bytes()
            )
            self.assertEqual(value["rpgMakerRuntimeManifestSha256"], expected)

    def test_rpg_runtime_manifest_shape_is_fail_closed_without_fixed_counts(self) -> None:
        manifest = {
            "schema_version": 3,
            "runtime_id": "retrom-runtime",
            "release": {"tag": "v0.4.2"},
            "runtime_files": [{}],
            "artifacts": [{}],
        }
        release_input_digest.validate_rpg_runtime_manifest(manifest)
        for invalid in (
            {key: value for key, value in manifest.items() if key != "artifacts"},
            {**manifest, "artifacts": "not-a-list"},
            {**manifest, "runtime_files": []},
        ):
            with self.assertRaisesRegex(ValueError, "RELEASE_INPUT_RPG_RUNTIME_MANIFEST_INVALID"):
                release_input_digest.validate_rpg_runtime_manifest(invalid)

    def test_release_digest_rejects_unordered_dependency_versions(self) -> None:
        with self.assertRaisesRegex(
            dependencies.CheckError,
            "DEPENDENCY_VERSION_LIST_NOT_SORTED",
        ):
            release_input_digest.release_input_value(
                ["4.3.0-pre", "4.2.3"],
                "4.2.3",
            )


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

    def test_image_export_contains_only_declared_read_only_payload(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_root = root / "data"
            auth_manifest_path = data_root / "auth/password-blocklists/v1/manifest.json"
            netplay_manifest_path = data_root / "netplay/v2/manifest.json"
            netplay_schema_path = data_root / "netplay/v2/schema.json"
            rpg_dat_root = data_root / "dat/rpgmaker/v1"
            rpg_manifest_path = rpg_dat_root / "manifest.json"
            rpg_runtime_root = data_root / "runtime/rpgmaker/v1"
            manifest = {
                "cores": [{"dat": {"local_path": "arcade/catalog.dat"}}],
                "emulatorjs": {
                    "runtime_allowlist": [
                        {"path_in_release": "data/loader.js"},
                        {"path_in_release": "data/cores/reports/core.json"},
                    ],
                    "selected_core_artifacts": [
                        {"path_in_release": "data/cores/core-wasm.data"},
                        {"path_in_release": None, "local_path": "overrides/core.data"},
                    ],
                },
                "license_materialization": {
                    "components": [
                        {
                            "license_files": [
                                {"output_relative_path": "licenses/core/LICENSE"}
                            ]
                        }
                    ],
                    "third_party_notices_relative_path": "THIRD_PARTY_NOTICES",
                },
            }
            auth_manifest = {
                "passwords": {"output_relative_path": "payload/passwords.txt"},
                "license": {"output_relative_path": "payload/LICENSE"},
            }
            rpg_manifest = {"runtime_files": [
                {"path_in_release": "v0.2.0/runtime.js"},
                {"path_in_release": "v0.2.0/LICENSE"},
            ]}
            expected = {
                "dat/emulatorjs/1.0.0/manifest.json",
                "dat/emulatorjs/1.0.0/SHA256SUMS",
                "dat/emulatorjs/1.0.0/arcade/catalog.dat",
                "runtime/emulatorjs/1.0.0/data/loader.js",
                "runtime/emulatorjs/1.0.0/data/cores/reports/core.json",
                "runtime/emulatorjs/1.0.0/data/cores/core-wasm.data",
                "runtime/emulatorjs/1.0.0/overrides/core.data",
                "runtime/emulatorjs/1.0.0/licenses/core/LICENSE",
                "runtime/emulatorjs/1.0.0/THIRD_PARTY_NOTICES",
                "auth/password-blocklists/v1/manifest.json",
                "auth/password-blocklists/v1/payload/passwords.txt",
                "auth/password-blocklists/v1/payload/LICENSE",
                "netplay/v2/manifest.json",
                "netplay/v2/schema.json",
                "dat/rpgmaker/v1/manifest.json",
                "runtime/rpgmaker/v1/.release-observed.json",
                "runtime/rpgmaker/v1/v0.2.0/runtime.js",
                "runtime/rpgmaker/v1/v0.2.0/LICENSE",
            }
            source_paths = set(expected)
            source_paths.remove("dat/emulatorjs/1.0.0/manifest.json")
            source_paths.remove("auth/password-blocklists/v1/manifest.json")
            source_paths.remove("dat/rpgmaker/v1/manifest.json")
            for relative in source_paths:
                target = data_root / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(relative.encode("utf-8"))
                target.chmod(0o600)
            for target in (auth_manifest_path, netplay_manifest_path, netplay_schema_path, rpg_manifest_path):
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text("{}\n", encoding="utf-8")
                target.chmod(0o600)
            runtime_manifest_path = data_root / "dat/emulatorjs/1.0.0/manifest.json"
            runtime_manifest_path.parent.mkdir(parents=True, exist_ok=True)
            runtime_manifest_path.write_text("{}\n", encoding="utf-8")
            unused = data_root / "runtime/emulatorjs/1.0.0/data/cores/unused.data"
            unused.parent.mkdir(parents=True, exist_ok=True)
            unused.write_bytes(b"must not be exported")

            output = root / "image-dependencies"
            with mock.patch.multiple(
                dependencies,
                DATA_ROOT=data_root,
                AUTH_MANIFEST_PATH=auth_manifest_path,
                NETPLAY_MANIFEST_PATH=netplay_manifest_path,
                NETPLAY_SCHEMA_PATH=netplay_schema_path,
                RPG_MAKER_DAT_ROOT=rpg_dat_root,
                RPG_MAKER_MANIFEST_PATH=rpg_manifest_path,
                RPG_MAKER_RUNTIME_ROOT=rpg_runtime_root,
            ):
                dependencies.export_image_dependencies(
                    output, ["1.0.0"], [manifest], auth_manifest, rpg_manifest
                )

            actual = {
                path.relative_to(output).as_posix()
                for path in output.rglob("*")
                if path.is_file()
            }
            self.assertEqual(actual, expected)
            self.assertNotIn("runtime/emulatorjs/1.0.0/data/cores/unused.data", actual)
            for path in output.rglob("*"):
                self.assertEqual(path.stat().st_mode & 0o777, 0o555 if path.is_dir() else 0o444)
            self.assertEqual(output.stat().st_mode & 0o777, 0o555)

    def test_image_export_rejects_symlink_source(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_root = root / "data"
            source = data_root / "runtime/emulatorjs/1.0.0/data/loader.js"
            source.parent.mkdir(parents=True)
            real_source = root / "real-loader.js"
            real_source.write_bytes(b"loader")
            source.symlink_to(real_source)
            manifest = {
                "cores": [],
                "emulatorjs": {
                    "runtime_allowlist": [{"path_in_release": "data/loader.js"}],
                    "selected_core_artifacts": [],
                },
                "license_materialization": {
                    "components": [],
                    "third_party_notices_relative_path": "THIRD_PARTY_NOTICES",
                },
            }
            auth_manifest = {
                "passwords": {"output_relative_path": "payload/passwords.txt"},
                "license": {"output_relative_path": "payload/LICENSE"},
            }
            rpg_dat_root = data_root / "dat/rpgmaker/v1"
            rpg_manifest_path = rpg_dat_root / "manifest.json"
            rpg_runtime_root = data_root / "runtime/rpgmaker/v1"
            rpg_manifest = {"runtime_files": []}
            required = (
                data_root / "dat/emulatorjs/1.0.0/manifest.json",
                data_root / "dat/emulatorjs/1.0.0/SHA256SUMS",
                data_root / "runtime/emulatorjs/1.0.0/THIRD_PARTY_NOTICES",
                data_root / "auth/password-blocklists/v1/manifest.json",
                data_root / "auth/password-blocklists/v1/payload/passwords.txt",
                data_root / "auth/password-blocklists/v1/payload/LICENSE",
                data_root / "netplay/v2/manifest.json",
                data_root / "netplay/v2/schema.json",
                rpg_manifest_path,
                rpg_runtime_root / ".release-observed.json",
            )
            for target in required:
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(b"fixture")
            with mock.patch.multiple(
                dependencies,
                DATA_ROOT=data_root,
                AUTH_MANIFEST_PATH=required[3],
                NETPLAY_MANIFEST_PATH=required[6],
                NETPLAY_SCHEMA_PATH=required[7],
                RPG_MAKER_DAT_ROOT=rpg_dat_root,
                RPG_MAKER_MANIFEST_PATH=rpg_manifest_path,
                RPG_MAKER_RUNTIME_ROOT=rpg_runtime_root,
            ), self.assertRaisesRegex(
                dependencies.CheckError, "DEPENDENCY_IMAGE_EXPORT_SOURCE_INVALID"
            ):
                dependencies.export_image_dependencies(
                    root / "image-dependencies", ["1.0.0"], [manifest], auth_manifest, rpg_manifest
                )


class ImagePackagingTests(unittest.TestCase):
    def test_backend_image_uses_curated_export_without_fixed_runtime_user(self) -> None:
        repository_root = MANIFEST_PATH.parents[4]
        dockerfile = (repository_root / "Dockerfile").read_text(encoding="utf-8")
        self.assertIn("dependencies.py image-export", dockerfile)
        self.assertIn("/work/image-dependencies", dockerfile)
        self.assertIn(
            "COPY web/features/player/rpg-runtime/registry.json "
            "web/features/player/rpg-runtime/registry.json",
            dockerfile,
        )
        fixture = "testdata/public-roms/arcade-smoke/driver-layouts.json"
        self.assertIn(f"COPY {fixture} {fixture}", dockerfile)
        dockerignore = (repository_root / ".dockerignore").read_text(encoding="utf-8")
        self.assertIn("!web/features/player/rpg-runtime/registry.json", dockerignore)
        self.assertIn(f"!{fixture}", dockerignore)
        implementation = "web/features/player/rpg-runtime/index.ts"
        self.assertIn(f"COPY {implementation} {implementation}", dockerfile)
        self.assertIn(f"!{implementation}", dockerignore)
        self.assertIn(
            "RUN --mount=type=cache,target=/work/.cache/dependencies,sharing=locked",
            dockerfile,
        )
        self.assertNotIn("/work/data/runtime/emulatorjs /opt/retrom/dependencies", dockerfile)
        self.assertNotIn("chmod -R", dockerfile)
        self.assertNotRegex(dockerfile, r"(?m)^USER\s+")
        self.assertNotIn("adduser", dockerfile)

    def test_web_image_does_not_declare_a_fixed_runtime_user(self) -> None:
        dockerfile = (MANIFEST_PATH.parents[4] / "web/Dockerfile").read_text(
            encoding="utf-8"
        )
        self.assertNotRegex(dockerfile, r"(?m)^USER\s+")
        self.assertNotIn("adduser", dockerfile)
        self.assertNotIn("--chown=retrom:retrom", dockerfile)


if __name__ == "__main__":
    unittest.main()
