#!/usr/bin/env python3
"""Link an ignored retrom-runtime checkout into the Retrom dev instance."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import tempfile
from pathlib import Path
from typing import Any


MARKER_FILENAME = ".retrom-runtime-dev.json"
OBSERVED_FILENAME = ".release-observed.json"


class LinkError(RuntimeError):
    """Stable local runtime link failure."""


def load_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise LinkError(f"RETROM_RUNTIME_DEV_DOCUMENT_INVALID:{path.name}") from exc
    if not isinstance(value, dict):
        raise LinkError(f"RETROM_RUNTIME_DEV_DOCUMENT_INVALID:{path.name}")
    return value


def digest(path: Path) -> tuple[int, str]:
    contents = path.read_bytes()
    return len(contents), hashlib.sha256(contents).hexdigest()


def checked_root(path: Path, required_file: str) -> Path:
    if not path.is_absolute():
        raise LinkError("RETROM_RUNTIME_DEV_ROOT_MUST_BE_ABSOLUTE")
    resolved = path.resolve(strict=True)
    if not resolved.is_dir() or not (resolved / required_file).is_file():
        raise LinkError("RETROM_RUNTIME_DEV_ROOT_INVALID")
    return resolved


def validate_local_runtime(source: Path) -> tuple[dict[str, Any], str]:
    package = load_json(source / "package.json")
    manifest = load_json(source / "runtime-manifest.json")
    if (
        package.get("name") != "@xxxsen/retrom-runtime"
        or package.get("version") != manifest.get("packageVersion")
        or manifest.get("packageName") != package.get("name")
        or manifest.get("schemaVersion") != 1
        or manifest.get("publicApiVersion") != 1
        or not (source / "dist/index.js").is_file()
        or not (source / "dist/index.d.ts").is_file()
    ):
        raise LinkError("RETROM_RUNTIME_DEV_PACKAGE_INVALID")
    result = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=source, capture_output=True, text=True, check=False,
    )
    commit = result.stdout.strip()
    if result.returncode != 0 or len(commit) != 40 or any(char not in "0123456789abcdef" for char in commit):
        raise LinkError("RETROM_RUNTIME_DEV_COMMIT_INVALID")
    return manifest, commit


def local_assets(source: Path, manifest: dict[str, Any], include_runtime_assets: bool) -> dict[str, Path]:
    assets: dict[str, Path] = {}
    local = manifest.get("localAssets")
    builds = manifest.get("sourceBuilds")
    if not isinstance(local, list) or not isinstance(builds, list):
        raise LinkError("RETROM_RUNTIME_DEV_MANIFEST_INVALID")
    if not include_runtime_assets:
        return assets
    for item in local:
        add_asset(source, item, assets, required=True)
    for build in builds:
        if not isinstance(build, dict) or not isinstance(build.get("assets"), list) or not build["assets"]:
            raise LinkError("RETROM_RUNTIME_DEV_MANIFEST_INVALID")
        declared = build["assets"]
        present = [isinstance(item, dict) and (source / str(item.get("source", ""))).is_file() for item in declared]
        if any(present) and not all(present):
            raise LinkError(f"RETROM_RUNTIME_DEV_BUILD_PARTIAL:{build.get('id', 'unknown')}")
        if all(present):
            for item in declared:
                add_asset(source, item, assets, required=True)
    return assets


def add_asset(source: Path, item: object, assets: dict[str, Path], required: bool) -> None:
    if not isinstance(item, dict) or not isinstance(item.get("source"), str) or not isinstance(item.get("output"), str):
        raise LinkError("RETROM_RUNTIME_DEV_MANIFEST_INVALID")
    relative_source = Path(item["source"])
    output = item["output"]
    if relative_source.is_absolute() or ".." in relative_source.parts or not output or output.startswith("/"):
        raise LinkError("RETROM_RUNTIME_DEV_MANIFEST_INVALID")
    target = source / relative_source
    if required and not target.is_file():
        raise LinkError(f"RETROM_RUNTIME_DEV_ASSET_MISSING:{relative_source.name}")
    if output in assets:
        raise LinkError("RETROM_RUNTIME_DEV_MANIFEST_INVALID")
    assets[output] = target


def activate(
    source_arg: Path,
    runtime_arg: Path,
    web_package_arg: Path,
    manifest_path: Path,
    include_runtime_assets: bool,
) -> None:
    source = checked_root(source_arg, "runtime-manifest.json")
    runtime_root = checked_root(runtime_arg, OBSERVED_FILENAME)
    formal = load_json(manifest_path)
    local, commit = validate_local_runtime(source)
    assets = local_assets(source, local, include_runtime_assets)
    declarations = {
        item["bundle_path"]: item
        for item in formal.get("runtime_files", [])
        if isinstance(item, dict) and isinstance(item.get("bundle_path"), str)
    }
    if any(output not in declarations for output in assets):
        raise LinkError("RETROM_RUNTIME_DEV_ASSET_UNDECLARED")
    for output, local_path in assets.items():
        destination = runtime_root / declarations[output]["path_in_release"]
        replace_file(local_path, destination)
    observed = observed_document(formal, runtime_root)
    replace_json(runtime_root / OBSERVED_FILENAME, observed)
    replace_json(runtime_root / MARKER_FILENAME, {
        "schema_version": 1,
        "source_root": str(source),
        "source_commit": commit,
        "package_version": local["packageVersion"],
        "overlaid_assets": sorted(assets),
    })
    replace_package_link(source, web_package_arg)
    print(f"retrom-runtime-dev: linked {source} at {commit[:12]} ({len(assets)} runtime assets)")


def observed_document(formal: dict[str, Any], runtime_root: Path) -> dict[str, Any]:
    release = formal["release"]
    records: dict[str, dict[str, object]] = {}
    for item in formal["runtime_files"]:
        target = runtime_root / item["path_in_release"]
        size, sha256 = digest(target)
        if size < 1 or size > item["max_size_bytes"]:
            raise LinkError(f"RETROM_RUNTIME_DEV_ASSET_INVALID:{target.name}")
        records[item["path_in_release"]] = {
            "observed_size_bytes": size,
            "observed_sha256": sha256,
        }
    return {
        "schema_version": 1,
        "repository": release["repository"],
        "tag": release["tag"],
        "tag_commit": release["tag_commit"],
        "bundle_filename": release["bundle_asset"]["filename"],
        "files": records,
    }


def replace_file(source: Path, target: Path) -> None:
    target.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=f".{target.name}.", dir=target.parent)
    os.close(descriptor)
    temporary_path = Path(temporary)
    try:
        shutil.copyfile(source, temporary_path)
        os.chmod(temporary_path, 0o600)
        os.replace(temporary_path, target)
    finally:
        temporary_path.unlink(missing_ok=True)


def replace_json(target: Path, value: dict[str, Any]) -> None:
    target.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=f".{target.name}.", dir=target.parent, text=True)
    os.close(descriptor)
    temporary_path = Path(temporary)
    try:
        temporary_path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        os.chmod(temporary_path, 0o600)
        os.replace(temporary_path, target)
    finally:
        temporary_path.unlink(missing_ok=True)


def replace_package_link(source: Path, target_arg: Path) -> None:
    if not target_arg.is_absolute():
        raise LinkError("RETROM_RUNTIME_DEV_WEB_PACKAGE_MUST_BE_ABSOLUTE")
    target = target_arg.absolute()
    target.parent.mkdir(parents=True, exist_ok=True)
    if target.is_symlink() or target.is_file():
        target.unlink()
    elif target.exists():
        shutil.rmtree(target)
    target.symlink_to(source, target_is_directory=True)


def deactivate(runtime_arg: Path, web_package_arg: Path) -> None:
    runtime_root = runtime_arg.absolute()
    marker = runtime_root / MARKER_FILENAME
    if marker.is_file():
        shutil.rmtree(runtime_root)
    package = web_package_arg.absolute()
    if package.is_symlink():
        package.unlink()
    print("retrom-runtime-dev: local override removed")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("activate", "deactivate"))
    parser.add_argument("--source", type=Path)
    parser.add_argument("--runtime-root", type=Path, required=True)
    parser.add_argument("--web-package", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--include-runtime-assets", action="store_true")
    args = parser.parse_args()
    if args.action == "activate":
        if args.source is None:
            raise LinkError("RETROM_RUNTIME_DEV_ROOT_REQUIRED")
        activate(
            args.source,
            args.runtime_root,
            args.web_package,
            args.manifest,
            args.include_runtime_assets,
        )
    else:
        deactivate(args.runtime_root, args.web_package)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (LinkError, OSError, KeyError, TypeError, ValueError) as error:
        print(str(error), file=os.sys.stderr)
        raise SystemExit(1) from error
