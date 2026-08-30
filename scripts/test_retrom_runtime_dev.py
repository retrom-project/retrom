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
    def test_activate_links_library_and_preserves_formal_assets_by_default(self) -> None:
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
                    source.resolve(), runtime.resolve(), web_package.resolve(), formal_path, False,
                )

            self.assertTrue(web_package.is_symlink())
            self.assertEqual(source.resolve(), web_package.resolve())
            native = runtime / formal["runtime_files"][0]["path_in_release"]
            ons = runtime / formal["runtime_files"][1]["path_in_release"]
            self.assertEqual(b"released bridge", native.read_bytes())
            self.assertEqual(b"released ons", ons.read_bytes())
            marker = json.loads((runtime / DEV.MARKER_FILENAME).read_text(encoding="utf-8"))
            self.assertEqual([], marker["overlaid_assets"])
            observed = json.loads((runtime / DEV.OBSERVED_FILENAME).read_text(encoding="utf-8"))
            self.assertEqual(len(b"released bridge"), observed["files"][formal["runtime_files"][0]["path_in_release"]]["observed_size_bytes"])

            DEV.deactivate(runtime.resolve(), web_package.absolute())
            self.assertFalse(runtime.exists())
            self.assertFalse(web_package.exists())

    def test_activate_with_assets_stages_the_complete_candidate_release(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache/tmp") as temporary:
            root = Path(temporary)
            source = root / "retrom-runtime"
            runtime = root / "runtime"
            web_package = root / "web/node_modules/@xxxsen/retrom-runtime"
            formal_path = root / "manifest.json"
            self.write_local_runtime(source)
            formal = self.write_formal_manifest(formal_path)
            stage = source / "release/stage"
            for item, contents in zip(
                formal["runtime_files"],
                (b"candidate bridge", b"candidate ons"),
                strict=True,
            ):
                target = stage / item["bundle_path"]
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(contents)

            git = Mock(returncode=0, stdout="a" * 40 + "\n")
            with patch.object(DEV.subprocess, "run", return_value=git):
                DEV.activate(
                    source.resolve(), runtime.resolve(), web_package.resolve(), formal_path, True,
                )

            self.assertEqual(
                b"candidate bridge",
                (runtime / formal["runtime_files"][0]["path_in_release"]).read_bytes(),
            )
            self.assertEqual(
                b"candidate ons",
                (runtime / formal["runtime_files"][1]["path_in_release"]).read_bytes(),
            )
            marker = json.loads((runtime / DEV.MARKER_FILENAME).read_text(encoding="utf-8"))
            self.assertEqual(
                [item["bundle_path"] for item in formal["runtime_files"]],
                marker["overlaid_assets"],
            )

    def test_candidate_assets_do_not_require_the_unreleased_package_version_to_match(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT / ".cache/tmp") as temporary:
            root = Path(temporary)
            source = root / "retrom-runtime"
            runtime = root / "runtime"
            web_package = root / "web/node_modules/@xxxsen/retrom-runtime"
            formal_path = root / "manifest.json"
            self.write_local_runtime(source)
            formal = self.write_formal_manifest(formal_path, version="0.6.1")
            for item, contents in zip(
                formal["runtime_files"], (b"candidate bridge", b"candidate ons"), strict=True,
            ):
                target = source / "release/stage" / item["bundle_path"]
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(contents)

            git = Mock(returncode=0, stdout="a" * 40 + "\n")
            with patch.object(DEV.subprocess, "run", return_value=git):
                DEV.activate(
                    source.resolve(), runtime.resolve(), web_package.resolve(), formal_path, True,
                )

            marker = json.loads((runtime / DEV.MARKER_FILENAME).read_text(encoding="utf-8"))
            self.assertEqual("0.7.0", marker["package_version"])
            self.assertTrue((runtime / "v0.6.1/onsyuri.js").is_file())

    @staticmethod
    def write_local_runtime(source: Path) -> None:
        (source / "dist").mkdir(parents=True)
        (source / "assets/runtime/native").mkdir(parents=True)
        (source / "dist/index.js").write_text("export {};\n", encoding="utf-8")
        (source / "dist/index.d.ts").write_text("export {};\n", encoding="utf-8")
        (source / "assets/runtime/native/bridge.js").write_bytes(b"local bridge")
        (source / "package.json").write_text(json.dumps({
            "name": "@xxxsen/retrom-runtime", "version": "0.7.0",
        }), encoding="utf-8")
        (source / "runtime-manifest.json").write_text(json.dumps({
            "schemaVersion": 4,
            "packageName": "@xxxsen/retrom-runtime",
            "packageVersion": "0.7.0",
            "publicApiVersion": 2,
            "localAssets": [{
                "source": "assets/runtime/native/bridge.js", "output": "runtime/native/bridge.js",
            }],
            "upstreamReleases": [],
        }), encoding="utf-8")

    @staticmethod
    def write_formal_runtime(runtime: Path, manifest_path: Path) -> dict[str, object]:
        formal = RetromRuntimeDevTests.write_formal_manifest(manifest_path)
        runtime.mkdir(parents=True)
        for item, contents in zip(
            formal["runtime_files"], (b"released bridge", b"released ons"), strict=True,
        ):
            target = runtime / item["path_in_release"]
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(contents)
        observed = DEV.observed_document(formal, runtime)
        (runtime / DEV.OBSERVED_FILENAME).write_text(json.dumps(observed), encoding="utf-8")
        return formal

    @staticmethod
    def write_formal_manifest(manifest_path: Path, version: str = "0.7.0") -> dict[str, object]:
        release = {
            "repository": "https://github.com/xxxsen/retrom-runtime",
            "tag": f"v{version}",
            "tag_commit": "b" * 40,
            "bundle_asset": {"filename": f"retrom-runtime-{version}.tar.gz"},
        }
        files = [
            {
                "bundle_path": "runtime/native/bridge.js",
                "path_in_release": f"v{version}/native-bridge.js",
                "max_size_bytes": 1024,
            },
            {
                "bundle_path": "runtime/ons/onsyuri.js",
                "path_in_release": f"v{version}/onsyuri.js",
                "max_size_bytes": 1024,
            },
        ]
        formal = {"release": release, "runtime_files": files}
        manifest_path.write_text(json.dumps(formal), encoding="utf-8")
        return formal


if __name__ == "__main__":
    unittest.main()
