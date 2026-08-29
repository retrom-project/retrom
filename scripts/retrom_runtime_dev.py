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
        or manifest.get("schemaVersion") != 3
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


def activate(
    source_arg: Path,
    runtime_arg: Path,
    web_package_arg: Path,
    manifest_path: Path,
    include_runtime_assets: bool,
) -> None:
    source = checked_root(source_arg, "runtime-manifest.json")
    formal = load_json(manifest_path)
    local, commit = validate_local_runtime(source)
    if include_runtime_assets:
        if formal.get("release", {}).get("tag") != f"v{local['packageVersion']}":
            raise LinkError("RETROM_RUNTIME_DEV_VERSION_MISMATCH")
        assets = staged_release_assets(source, formal)
        runtime_root = runtime_arg.absolute()
        publish_candidate_runtime(formal, assets, runtime_root)
    else:
        assets = {}
        runtime_root = checked_root(runtime_arg, OBSERVED_FILENAME)
        replace_json(runtime_root / OBSERVED_FILENAME, observed_document(formal, runtime_root))
    replace_json(runtime_root / MARKER_FILENAME, {
        "schema_version": 1,
        "source_root": str(source),
        "source_commit": commit,
        "package_version": local["packageVersion"],
        "overlaid_assets": sorted(assets),
    })
    replace_package_link(source, web_package_arg)
    print(f"retrom-runtime-dev: linked {source} at {commit[:12]} ({len(assets)} runtime assets)")


def staged_release_assets(source: Path, formal: dict[str, Any]) -> dict[str, Path]:
    stage = source / "release/stage"
    declarations = formal.get("runtime_files")
    if not stage.is_dir() or not isinstance(declarations, list) or not declarations:
        raise LinkError("RETROM_RUNTIME_DEV_STAGE_INVALID")
    assets: dict[str, Path] = {}
    for item in declarations:
        if not isinstance(item, dict):
            raise LinkError("RETROM_RUNTIME_DEV_STAGE_INVALID")
        bundle_path = item.get("bundle_path")
        release_path = item.get("path_in_release")
        if not isinstance(bundle_path, str) or not isinstance(release_path, str):
            raise LinkError("RETROM_RUNTIME_DEV_STAGE_INVALID")
        relative = Path(bundle_path)
        destination = Path(release_path)
        if (
            relative.is_absolute()
            or destination.is_absolute()
            or ".." in relative.parts
            or ".." in destination.parts
            or bundle_path in assets
        ):
            raise LinkError("RETROM_RUNTIME_DEV_STAGE_INVALID")
        candidate = stage / relative
        if not candidate.is_file():
            raise LinkError(f"RETROM_RUNTIME_DEV_ASSET_MISSING:{relative.name}")
        assets[bundle_path] = candidate
    return assets


def publish_candidate_runtime(
    formal: dict[str, Any], assets: dict[str, Path], runtime_root: Path,
) -> None:
    runtime_root.parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=f".{runtime_root.name}.staging-", dir=runtime_root.parent))
    backup = Path(tempfile.mkdtemp(prefix=f".{runtime_root.name}.backup-", dir=runtime_root.parent))
    backup.rmdir()
    declarations = {item["bundle_path"]: item for item in formal["runtime_files"]}
    try:
        for bundle_path, source in assets.items():
            replace_file(source, staging / declarations[bundle_path]["path_in_release"])
        replace_json(staging / OBSERVED_FILENAME, observed_document(formal, staging))
        if runtime_root.exists():
            os.replace(runtime_root, backup)
        try:
            os.replace(staging, runtime_root)
        except BaseException:
            if backup.exists() and not runtime_root.exists():
                os.replace(backup, runtime_root)
            raise
        if backup.exists():
            shutil.rmtree(backup)
    finally:
        if staging.exists():
            shutil.rmtree(staging)
        if backup.exists():
            shutil.rmtree(backup)


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
