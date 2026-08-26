#!/usr/bin/env python3
"""Unit tests for tag-pinned RPG runtime release assets."""

from __future__ import annotations

import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "data/dat/rpgmaker/v1/release_assets.py"
SPEC = importlib.util.spec_from_file_location("retrom_rpg_release_assets", MODULE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("RPG_RELEASE_ASSET_IMPORT_FAILED")
RELEASES = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = RELEASES
SPEC.loader.exec_module(RELEASES)


def release(
    release_id: str,
    repository: str,
    tag: str,
    commit: str,
    abi: str,
    version: str,
    basename: str,
) -> dict[str, object]:
    base = f"{repository}/releases/download/{tag}"
    assets = [
        {
            "filename": f"{basename}.js",
            "url": f"{base}/{basename}.js",
            "path_in_release": f"{version}/{basename}.js",
            "role": "runtime_js",
            "max_size_bytes": 1024 * 1024,
        },
        {
            "filename": f"{basename}.wasm",
            "url": f"{base}/{basename}.wasm",
            "path_in_release": f"{version}/{basename}.wasm",
            "role": "runtime_wasm",
            "max_size_bytes": 64 * 1024 * 1024,
        },
    ]
    return {
        "id": release_id,
        "repository": repository,
        "tag": tag,
        "tag_commit": commit,
        "adapter_abi": abi,
        "binary_association": "TAGGED_RELEASE_COMPATIBLE",
        "metadata_asset": {
            "filename": "retrom-runtime-release.json",
            "url": f"{base}/retrom-runtime-release.json",
            "max_size_bytes": 65536,
        },
        "assets": assets,
    }


def declarations() -> list[dict[str, object]]:
    return [
        release(
            "easyrpg",
            "https://github.com/xxxsen/Player",
            "retrom-web-0.8.1.1-r2",
            "6" * 40,
            "easyrpg-save-v1",
            "0.8.1.1-v4",
            "easyrpg-player",
        ),
        release(
            "easyrpg-r3",
            "https://github.com/xxxsen/Player",
            "retrom-web-0.8.1.1-r3",
            "7" * 40,
            "easyrpg-save-v1",
            "0.8.1.1-v5",
            "easyrpg-player",
        ),
        release(
            "mkxp",
            "https://github.com/xxxsen/mkxp-z-libretro-emscripten",
            "retrom-web-f2efc98-r1",
            "8" * 40,
            "mkxp-state-v1",
            "f2efc98-v3",
            "mkxp-z_libretro",
        ),
    ]


def metadata(item: dict[str, object]) -> bytes:
    assets = [
        {
            "filename": asset["filename"],
            "observedSha256": "0" * 64,
            "sizeBytes": 1,
        }
        for asset in item["assets"]
    ]
    value = {
        "adapterAbi": item["adapter_abi"],
        "assets": assets,
        "commit": item["tag_commit"],
        "digestPolicy": "OBSERVED_CACHE_INTEGRITY_ONLY",
        "repository": item["repository"],
        "schemaVersion": 1,
        "tag": item["tag"],
    }
    if item["id"] == "mkxp":
        value["sourceCommits"] = RELEASES.MKXP_SOURCE_COMMITS
    return json.dumps(value).encode()


class RPGMakerReleaseAssetTests(unittest.TestCase):
    def test_manifest_shape_rejects_expected_digest_and_floating_identity(self) -> None:
        valid = declarations()
        self.assertEqual({"easyrpg", "easyrpg-r3", "mkxp"}, set(RELEASES.validate(valid)))
        for mutator in (
            lambda values: values[0]["assets"][0].update({"sha256": "0" * 64}),
            lambda values: values[0].update({"tag": "latest"}),
            lambda values: values[0].update({"repository": "https://github.com/other/Player"}),
            lambda values: values[1]["assets"][0].update({"url": "https://example.com/core.js"}),
        ):
            values = declarations()
            mutator(values)
            with self.assertRaises(RELEASES.ReleaseAssetError):
                RELEASES.validate(values)

    def test_materialize_records_observed_digest_and_detects_local_tamper(self) -> None:
        values = declarations()
        queues: list[bytes] = []
        expected: dict[str, bytes] = {}
        for item in values:
            queues.append(metadata(item))
            for asset in item["assets"]:
                contents = f"payload:{asset['filename']}".encode()
                queues.append(contents)
                expected[asset["path_in_release"]] = contents
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with patch.object(RELEASES, "download_bytes", side_effect=queues):
                RELEASES.materialize(values, root, offline=False)
            observed = RELEASES.verify(values, root)
            self.assertEqual(set(expected), set(observed))
            for path, contents in expected.items():
                self.assertEqual(contents, (root / path).read_bytes())
            target = root / values[0]["assets"][0]["path_in_release"]
            target.write_bytes(b"tampered")
            with self.assertRaisesRegex(
                RELEASES.ReleaseAssetError, "RPG_RUNTIME_RELEASE_ASSET_MISMATCH"
            ):
                RELEASES.verify(values, root)

    def test_offline_missing_asset_fails_with_stable_path(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(
                RELEASES.ReleaseAssetError,
                "RPG_RUNTIME_RELEASE_ASSET_REQUIRED:0.8.1.1-v4/easyrpg-player.js",
            ):
                RELEASES.materialize(declarations(), Path(temporary), offline=True)

    def test_release_metadata_requires_exact_tag_commit_and_abi(self) -> None:
        item = declarations()[0]
        RELEASES.validate_release_metadata(item, metadata(item))
        value = json.loads(metadata(item))
        value["commit"] = "0" * 40
        with self.assertRaisesRegex(
            RELEASES.ReleaseAssetError, "RPG_RUNTIME_RELEASE_METADATA_INVALID"
        ):
            RELEASES.validate_release_metadata(item, json.dumps(value).encode())

    def test_mkxp_metadata_requires_exact_source_commits_and_asset_shape(self) -> None:
        item = declarations()[2]
        RELEASES.validate_release_metadata(item, metadata(item))
        for mutate in (
            lambda value: value["sourceCommits"].update({"mkxp-z": "0" * 40}),
            lambda value: value["assets"][0].update({"unexpected": True}),
            lambda value: value["assets"][0].update({"observedSha256": "invalid"}),
        ):
            value = json.loads(metadata(item))
            mutate(value)
            with self.assertRaisesRegex(
                RELEASES.ReleaseAssetError, "RPG_RUNTIME_RELEASE_METADATA_INVALID"
            ):
                RELEASES.validate_release_metadata(item, json.dumps(value).encode())


if __name__ == "__main__":
    unittest.main()
