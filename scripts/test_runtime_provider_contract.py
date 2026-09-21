#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import subprocess
import sys
import tempfile
import unittest
from copy import deepcopy
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))

from runtime_provider_contract import (  # noqa: E402
    ContractError,
    canonical_json_bytes,
    check_contract_snapshot,
    parse_launch_envelope,
    sync_contract_snapshot,
    validate_provider_manifest,
)


def valid_manifest() -> dict[str, object]:
    return {
        "schemaVersion": 1,
        "providerId": "retrom-runtime",
        "providerVersion": "0.12.0",
        "providerApiVersion": 1,
        "clientModulePath": "client.mjs",
        "targets": [
            {
                "id": "wasm4",
                "displayName": "WASM-4",
                "targetOptionsSchema": {
                    "type": "object",
                    "additionalProperties": False,
                    "properties": {},
                    "required": [],
                },
                "capabilities": {
                    "pause": True,
                    "screenshot": True,
                    "checkpoint": True,
                    "standardGamepad": True,
                    "frameCounter": True,
                    "volume": False,
                    "discSwitch": False,
                    "nativeSettings": False,
                    "inputFilter": True,
                    "netplayPort": False,
                    "videoModes": ["original", "pixel"],
                    "requiresThreads": False,
                    "frameMode": "NONE",
                },
                "inputs": [
                    {
                        "role": "game",
                        "kind": "WASM4_CART",
                        "cardinality": "ONE",
                        "optional": False,
                    }
                ],
                "checkpoint": {
                    "writeFormat": "wasm4-state-v1",
                    "readFormats": ["wasm4-state-v1"],
                    "maxBytes": 132144,
                },
                "assetPaths": ["assets/wasm4/wasm4-retrom.mjs"],
            }
        ],
    }


class RuntimeProviderAuthorityTests(unittest.TestCase):
    def test_authority_contains_only_the_frozen_v1_files(self) -> None:
        contract_root = ROOT / "api/runtime-provider/v1"
        self.assertEqual(
            sorted(path.name for path in contract_root.iterdir()),
            [
                "common.schema.json",
                "fixtures",
                "launch-envelope.schema.json",
                "provider-integrity.schema.json",
                "provider-lock.schema.json",
                "provider-manifest.schema.json",
                "provider-module-v1.d.ts",
                "runtime-resource.schema.json",
            ],
        )
        for schema_path in contract_root.glob("*.schema.json"):
            schema = json.loads(schema_path.read_text(encoding="utf-8"))
            self.assertEqual(schema["$schema"], "https://json-schema.org/draft/2020-12/schema")
            self.assertFalse(schema.get("additionalProperties", True), schema_path.name)

    def test_accepts_latest_main_wasm4_target(self) -> None:
        validate_provider_manifest(valid_manifest())

    def test_target_options_are_discriminator_free_and_provider_schema_owned(self) -> None:
        manifest = valid_manifest()
        target = manifest["targets"][0]  # type: ignore[index]
        self.assertNotIn("optionsKind", target)
        schema = target["targetOptionsSchema"]
        self.assertEqual(schema, {
            "type": "object", "additionalProperties": False, "properties": {}, "required": [],
        })
        validate_provider_manifest(manifest)

        invalid = deepcopy(manifest)
        invalid["targets"][0]["optionsKind"] = "NONE"  # type: ignore[index]
        with self.assertRaises(ContractError):
            validate_provider_manifest(invalid)

    def test_manifest_structure_version_is_not_the_provider_api_support_policy(self) -> None:
        future_api = deepcopy(valid_manifest())
        future_api["providerApiVersion"] = 2
        validate_provider_manifest(future_api)

    def test_semantic_contract_values_do_not_embed_versions(self) -> None:
        suffix = re.compile(r"_V[0-9]+$")
        semantic_fields = {
            "kind", "contentKind", "acceptedContentKinds", "supportedContentKinds",
            "detectorProfile", "deliveryProfile", "launchPolicy", "reviewPolicy", "optionsKind",
        }

        def strings(value: object) -> list[str]:
            if isinstance(value, str):
                return [value]
            if isinstance(value, list):
                return [item for child in value for item in strings(child)]
            if isinstance(value, dict):
                return [item for child in value.values() for item in strings(child)]
            return []

        def semantic_values(value: object) -> list[str]:
            if isinstance(value, list):
                return [item for child in value for item in semantic_values(child)]
            if not isinstance(value, dict):
                return []
            result: list[str] = []
            for key, child in value.items():
                if key in semantic_fields:
                    result.extend(strings(child))
                result.extend(semantic_values(child))
            return result

        documents = [
            json.loads((ROOT / "api/runtime-provider/v1/provider-manifest.schema.json").read_text()),
            json.loads((ROOT / "api/runtime-provider/v1/runtime-resource.schema.json").read_text()),
            json.loads((ROOT / "data/runtime-target-bindings/v1/catalog.json").read_text()),
            json.loads((ROOT / "data/runtime-target-bindings/v1/schema.json").read_text()),
        ]
        offenders = sorted({item for document in documents for item in semantic_values(document) if suffix.search(item)})
        self.assertEqual(offenders, [])

        serialized_formats = {
            "ARGON2ID_V1", "RETROM_DOS_DIRECT_ZIP_V1", "RETROM_FILESET_V1",
            "RETROM_LAUNCH_BUNDLE_V1", "RETROM_MULTIDISC_M3U_V1",
            "RETROM_RUNTIME_EXTERNAL_V1", "RETROM_RUNTIME_GAME_V2", "RETROM_RUNTIME_GAME_V3",
            "RETROM_RUNTIME_PROJECT_V1",
            "RETROM_SINGLE_FILE_V1", "RETROM_VARIANT_VALIDATION_INPUT_V3", "SOURCE_V1",
        }
        paths = [
            ROOT / "api/domains/catalog.yaml", ROOT / "api/domains/imports.yaml",
            ROOT / "api/domains/runtime.yaml", ROOT / "data/runtime-target-bindings/v1/catalog.json",
            ROOT / "internal/contentcapability", ROOT / "internal/contentmanifest", ROOT / "internal/contentprofile",
            ROOT / "internal/launch", ROOT / "internal/runtimecatalog", ROOT / "migrations",
            ROOT / "web/features/imports", ROOT / "web/features/player", ROOT / "web/features/reviews",
        ]
        quoted = re.compile(r'(?<![A-Z0-9_])([A-Z][A-Z0-9_]*_V[0-9]+)(?![A-Z0-9_])')
        found: set[str] = set()
        for path in paths:
            files = [path] if path.is_file() else [item for item in path.rglob("*") if item.is_file()]
            for file in files:
                try:
                    found.update(quoted.findall(file.read_text(encoding="utf-8")))
                except UnicodeDecodeError:
                    continue
        self.assertEqual(found - serialized_formats, set())

    def test_shared_launch_envelope_fixtures_have_the_declared_result(self) -> None:
        fixture_root = ROOT / "api/runtime-provider/v1/fixtures"
        for path in sorted((fixture_root / "valid").glob("*.json")):
            with self.subTest(path=path):
                parse_launch_envelope(path.read_bytes())
        for path in sorted((fixture_root / "invalid").glob("*.json")):
            with self.subTest(path=path), self.assertRaises(ContractError):
                parse_launch_envelope(path.read_bytes())

    def test_provider_inputs_exclude_host_owned_netplay_channel(self) -> None:
        candidate = deepcopy(valid_manifest())
        candidate["targets"][0]["inputs"].append({  # type: ignore[index]
            "role": "netplay",
            "kind": "NETPLAY_CHANNEL",
            "cardinality": "ONE",
            "optional": True,
        })
        with self.assertRaises(ContractError):
            validate_provider_manifest(candidate)

    def test_module_abi_covers_every_host_action_without_provider_private_types(self) -> None:
        source = (ROOT / "api/runtime-provider/v1/provider-module-v1.d.ts").read_text(encoding="utf-8")
        for operation in (
            "setVideoMode(", "openNativeSettings(", "closeNativeSettings(", "getDiscState(",
            "switchDisc(", "setInputFilter(", "getNetplayPort(",
        ):
            self.assertIn(operation, source)
        for operation in (
            "pauseAtBoundary(", "captureState(", "loadStateAndWait(", "runFrame(",
            "sampleLocalControls(", "resetLocalControls(", "close(",
        ):
            self.assertIn(operation, source)
        self.assertNotIn("NETPLAY_CHANNEL", source)
        self.assertNotIn("EmulatorJS", source)

    def test_rejects_unknown_fields_at_every_contract_boundary(self) -> None:
        cases = []
        for path, key in [
            ((), "unexpected"),
            (("targets", 0), "adapterId"),
            (("targets", 0, "capabilities"), "extraCapability"),
            (("targets", 0, "inputs", 0), "mountPath"),
            (("targets", 0, "checkpoint"), "codec"),
        ]:
            candidate = deepcopy(valid_manifest())
            current: object = candidate
            for part in path:
                current = current[part]  # type: ignore[index]
            current[key] = "forbidden"  # type: ignore[index]
            cases.append(candidate)
        for candidate in cases:
            with self.subTest(candidate=candidate), self.assertRaises(ContractError):
                validate_provider_manifest(candidate)

    def test_rejects_noncanonical_identity_version_and_paths(self) -> None:
        mutations = [
            ("providerId", "Retrom_Runtime"),
            ("providerVersion", "v0.12.0"),
            ("providerVersion", "0.12.0+rebuilt"),
            ("clientModulePath", "../client.mjs"),
        ]
        for key, value in mutations:
            candidate = deepcopy(valid_manifest())
            candidate[key] = value
            with self.subTest(key=key, value=value), self.assertRaises(ContractError):
                validate_provider_manifest(candidate)

        candidate = deepcopy(valid_manifest())
        candidate["targets"][0]["assetPaths"] = ["assets/ok", "assets/../escape"]  # type: ignore[index]
        with self.assertRaises(ContractError):
            validate_provider_manifest(candidate)

    def test_requires_sorted_unique_targets_and_manifest_sets(self) -> None:
        duplicate = deepcopy(valid_manifest())
        duplicate["targets"].append(deepcopy(duplicate["targets"][0]))  # type: ignore[union-attr,index]
        with self.assertRaises(ContractError):
            validate_provider_manifest(duplicate)

        unsorted = deepcopy(valid_manifest())
        unsorted["targets"][0]["assetPaths"] = ["assets/z", "assets/a"]  # type: ignore[index]
        with self.assertRaises(ContractError):
            validate_provider_manifest(unsorted)

    def test_checkpoint_capability_and_formats_are_atomic(self) -> None:
        no_checkpoint = deepcopy(valid_manifest())
        no_checkpoint["targets"][0]["capabilities"]["checkpoint"] = False  # type: ignore[index]
        no_checkpoint["targets"][0]["checkpoint"] = None  # type: ignore[index]
        validate_provider_manifest(no_checkpoint)

        missing_reader = deepcopy(valid_manifest())
        missing_reader["targets"][0]["checkpoint"]["readFormats"] = ["legacy-v1"]  # type: ignore[index]
        with self.assertRaises(ContractError):
            validate_provider_manifest(missing_reader)

        contradictory = deepcopy(no_checkpoint)
        contradictory["targets"][0]["checkpoint"] = valid_manifest()["targets"][0]["checkpoint"]  # type: ignore[index]
        with self.assertRaises(ContractError):
            validate_provider_manifest(contradictory)

    def test_canonical_bytes_are_stable_for_schema_safe_values(self) -> None:
        self.assertEqual(
            canonical_json_bytes({"z": [3, 2, 1], "name": "运行时", "a": True}),
            b'{"a":true,"name":"\xe8\xbf\x90\xe8\xa1\x8c\xe6\x97\xb6","z":[3,2,1]}',
        )
        with self.assertRaises(ContractError):
            canonical_json_bytes({"unsafe": 1.5})
        self.assertEqual(
            canonical_json_bytes({"\ue000": 1, "\U00010000": 2}),
            '{"𐀀":2,"\ue000":1}'.encode(),
        )

    def test_runtime_snapshot_is_exact_and_detects_tampering(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            runtime_root = Path(temporary) / "retrom-runtime"
            runtime_root.mkdir()
            sync_contract_snapshot(runtime_root)
            check_contract_snapshot(runtime_root)

            snapshot_root = runtime_root / "contracts/retrom-provider/v1"
            source = json.loads((snapshot_root / "SOURCE.json").read_text(encoding="utf-8"))
            self.assertEqual(source["contractVersion"], "runtime-provider-v1")
            self.assertNotIn(str(ROOT), json.dumps(source))
            self.assertEqual(
                sorted(path.relative_to(snapshot_root).as_posix() for path in snapshot_root.rglob("*") if path.is_file()),
                [
                    "SOURCE.json",
                    "common.schema.json",
                    "fixtures/invalid/checkpoint-missing-read-formats.json",
                    "fixtures/invalid/duplicate-field.json",
                    "fixtures/invalid/exponent-json-input.json",
                    "fixtures/invalid/float-json-input.json",
                    "fixtures/invalid/invalid-unicode.json",
                    "fixtures/invalid/missing-capability.json",
                    "fixtures/invalid/netplay-mode-mismatch.json",
                    "fixtures/invalid/netplay-resource.json",
                    "fixtures/invalid/unknown-top-level.json",
                    "fixtures/invalid/unsafe-integer-json-input.json",
                    "fixtures/target-options/schema-validation.json",
                    "fixtures/valid/checkpoint-restore.json",
                    "fixtures/valid/netplay.json",
                    "fixtures/valid/single-minimal.json",
                    "launch-envelope.schema.json",
                    "provider-integrity.schema.json",
                    "provider-lock.schema.json",
                    "provider-manifest.schema.json",
                    "provider-module-v1.d.ts",
                    "runtime-resource.schema.json",
                ],
            )

            manifest = snapshot_root / "provider-manifest.schema.json"
            manifest.write_bytes(manifest.read_bytes() + b"\n")
            with self.assertRaises(ContractError):
                check_contract_snapshot(runtime_root)

    def test_cross_repository_cli_syncs_and_checks_an_explicit_runtime_root(self) -> None:
        command = [sys.executable, str(ROOT / "scripts/runtime-provider-contract.py")]
        with tempfile.TemporaryDirectory() as temporary:
            runtime_root = Path(temporary) / "retrom-runtime"
            runtime_root.mkdir()
            for action in ("sync", "check"):
                result = subprocess.run(
                    [*command, action, "--runtime-root", str(runtime_root)],
                    check=False,
                    capture_output=True,
                    text=True,
                )
                self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
