#!/usr/bin/env python3
"""Tests for the explicit local retrom-runtime development override."""

from __future__ import annotations

import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch


ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "scripts/retrom_runtime_dev.py"
SPEC = importlib.util.spec_from_file_location("retrom_runtime_dev", MODULE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("RETROM_RUNTIME_DEV_TEST_IMPORT_FAILED")
DEV = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = DEV
SPEC.loader.exec_module(DEV)


class RetromRuntimeDevTests(unittest.TestCase):
    def test_activate_links_library_and_overlays_only_available_local_assets(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache/tmp") as temporary:
            root = Path(temporary)
            source = root / "retrom-runtime"
            runtime = root / "runtime"
            web_package = root / "web/node_modules/@xxxsen/retrom-runtime"
            formal_path = root / "manifest.json"
            self.write_local_runtime(source)
            formal = self.write_formal_runtime(runtime, formal_path)
            web_package.mkdir(parents=True)
            (web_package / "released.js").write_text("released", encoding="utf-8")

            git = Mock(returncode=0, stdout="a" * 40 + "\n")
            with patch.object(DEV.subprocess, "run", return_value=git):
                DEV.activate(
                    source.resolve(), runtime.resolve(), web_package.resolve(), formal_path, True,
                )

            self.assertTrue(web_package.is_symlink())
            self.assertEqual(source.resolve(), web_package.resolve())
            native = runtime / formal["runtime_files"][0]["path_in_release"]
            ons = runtime / formal["runtime_files"][1]["path_in_release"]
            self.assertEqual(b"local bridge", native.read_bytes())
            self.assertEqual(b"released ons", ons.read_bytes())
            marker = json.loads((runtime / DEV.MARKER_FILENAME).read_text(encoding="utf-8"))
            self.assertEqual(["runtime/native/bridge.js"], marker["overlaid_assets"])
            observed = json.loads((runtime / DEV.OBSERVED_FILENAME).read_text(encoding="utf-8"))
            self.assertEqual(len(b"local bridge"), observed["files"][formal["runtime_files"][0]["path_in_release"]]["observed_size_bytes"])

            DEV.deactivate(runtime.resolve(), web_package.absolute())
            self.assertFalse(runtime.exists())
            self.assertFalse(web_package.exists())

    def test_rejects_partial_source_build_output(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache/tmp") as temporary:
            source = Path(temporary) / "retrom-runtime"
            self.write_local_runtime(source)
            built = source / "build/ons/onsyuri.js"
            built.parent.mkdir(parents=True)
            built.write_bytes(b"partial")
            manifest = DEV.load_json(source / "runtime-manifest.json")
            with self.assertRaisesRegex(DEV.LinkError, "RETROM_RUNTIME_DEV_BUILD_PARTIAL:onsyuri"):
                DEV.local_assets(source, manifest, True)

    def test_default_asset_selection_preserves_the_formal_core_payload(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache/tmp") as temporary:
            source = Path(temporary) / "retrom-runtime"
            self.write_local_runtime(source)
            manifest = DEV.load_json(source / "runtime-manifest.json")
            self.assertEqual({}, DEV.local_assets(source, manifest, False))

    @staticmethod
    def write_local_runtime(source: Path) -> None:
        (source / "dist").mkdir(parents=True)
        (source / "assets/runtime/native").mkdir(parents=True)
        (source / "dist/index.js").write_text("export {};\n", encoding="utf-8")
        (source / "dist/index.d.ts").write_text("export {};\n", encoding="utf-8")
        (source / "assets/runtime/native/bridge.js").write_bytes(b"local bridge")
        (source / "package.json").write_text(json.dumps({
            "name": "@xxxsen/retrom-runtime", "version": "0.4.0",
        }), encoding="utf-8")
        (source / "runtime-manifest.json").write_text(json.dumps({
            "schemaVersion": 1,
            "packageName": "@xxxsen/retrom-runtime",
            "packageVersion": "0.4.0",
            "publicApiVersion": 1,
            "localAssets": [{
                "source": "assets/runtime/native/bridge.js", "output": "runtime/native/bridge.js",
            }],
            "sourceBuilds": [{
                "id": "onsyuri",
                "assets": [
                    {"source": "build/ons/onsyuri.js", "output": "runtime/ons/onsyuri.js"},
                    {"source": "build/ons/onsyuri.wasm", "output": "runtime/ons/onsyuri.wasm"},
                    {"source": "build/ons/COPYING", "output": "licenses/onsyuri/COPYING"},
                ],
            }],
        }), encoding="utf-8")

    @staticmethod
    def write_formal_runtime(runtime: Path, manifest_path: Path) -> dict[str, object]:
        runtime.mkdir(parents=True)
        release = {
            "repository": "https://github.com/xxxsen/retrom-runtime",
            "tag": "v0.3.5",
            "tag_commit": "b" * 40,
            "bundle_asset": {"filename": "retrom-runtime-0.3.5.tar.gz"},
        }
        files = [
            {
                "bundle_path": "runtime/native/bridge.js",
                "path_in_release": "v0.3.5/native-bridge.js",
                "max_size_bytes": 1024,
            },
            {
                "bundle_path": "runtime/ons/onsyuri.js",
                "path_in_release": "v0.3.5/onsyuri.js",
                "max_size_bytes": 1024,
            },
        ]
        for item, contents in zip(files, (b"released bridge", b"released ons"), strict=True):
            target = runtime / item["path_in_release"]
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(contents)
        formal = {"release": release, "runtime_files": files}
        manifest_path.write_text(json.dumps(formal), encoding="utf-8")
        observed = DEV.observed_document(formal, runtime)
        (runtime / DEV.OBSERVED_FILENAME).write_text(json.dumps(observed), encoding="utf-8")
        return formal


if __name__ == "__main__":
    unittest.main()
