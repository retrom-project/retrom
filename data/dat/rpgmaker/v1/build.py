#!/usr/bin/env python3
"""Materialize the single pinned retrom-runtime Release used by Retrom."""

from __future__ import annotations

import argparse
import hashlib
import io
import json
import os
import re
import shutil
import stat
import sys
import tarfile
import tempfile
import urllib.error
import urllib.request
from pathlib import Path, PurePosixPath
from typing import Any


DAT_ROOT = Path(__file__).resolve().parent
DEFAULT_RUNTIME_ROOT = DAT_ROOT.parents[2] / "runtime/rpgmaker/v1"
OBSERVED_FILENAME = ".release-observed.json"
DEV_MARKER_FILENAME = ".retrom-runtime-dev.json"
PUBLIC_API_VERSION = 2
HEX_40 = re.compile(r"^[0-9a-f]{40}$")
SEMVER_TAG = re.compile(r"^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?$")
RUNTIME_COMPATIBILITY_IDENTITY = re.compile(r"^[a-z0-9][a-z0-9-]{0,118}-v[1-9][0-9]*$")
EXPECTED_FILES = {
    "LICENSE": ("LICENSE", "license", 1 << 20),
    "THIRD_PARTY_NOTICES.md": ("THIRD_PARTY_NOTICES.md", "license", 1 << 20),
    "runtime/easyrpg/easyrpg-player.js": ("easyrpg-player.js", "runtime_js", 1 << 20),
    "runtime/easyrpg/easyrpg-player.wasm": ("easyrpg-player.wasm", "runtime_wasm", 16 << 20),
    "runtime/mkxp/mkxp-z_libretro.js": ("mkxp-z_libretro.js", "runtime_js", 1 << 20),
    "runtime/mkxp/mkxp-z_libretro.wasm": ("mkxp-z_libretro.wasm", "runtime_wasm", 64 << 20),
    "runtime/mkxp/position_bridge.rb": ("position_bridge.rb", "adapter_bridge", 16 << 10),
    "runtime/native/bridge.js": ("native-bridge.js", "adapter_bridge", 128 << 10),
    "runtime/ons/onsyuri.js": ("onsyuri.js", "runtime_js", 1 << 20),
    "runtime/ons/onsyuri.wasm": ("onsyuri.wasm", "runtime_wasm", 16 << 20),
    "licenses/onsyuri/COPYING": ("onsyuri-COPYING", "license", 64 << 10),
    "runtime/kirikiri/index.js": ("index.js", "runtime_js", 1 << 20),
    "runtime/kirikiri/index.wasm": ("index.wasm", "runtime_wasm", 64 << 20),
    "runtime/kirikiri/vlfs.js": ("vlfs.js", "runtime_js", 256 << 10),
    "runtime/kirikiri/assets.zip": ("assets.zip", "runtime_asset", 16 << 20),
    "licenses/kirikiri2/LICENSE": ("kirikiri2-LICENSE", "license", 64 << 10),
}
EXPECTED_ROUTES = {
    "RPG2000_EASYRPG": ("rpgmaker_2000", "RPGMAKER", "RPG2000", "EASYRPG_WEB", "easyrpg-web", "easyrpg-save"),
    "RPG2003_EASYRPG": ("rpgmaker_2003", "RPGMAKER", "RPG2003", "EASYRPG_WEB", "easyrpg-web", "easyrpg-save"),
    "RPGXP_MKXP": ("rpgmaker_xp", "RPGMAKER", "RPGXP", "MKXP_LIBRETRO_WEB", "mkxp-libretro-web", "mkxp-state-compact"),
    "RPGVX_MKXP": ("rpgmaker_vx", "RPGMAKER", "RPGVX", "MKXP_LIBRETRO_WEB", "mkxp-libretro-web", "mkxp-state-compact"),
    "RPGVXACE_MKXP": ("rpgmaker_vx_ace", "RPGMAKER", "RPGVXACE", "MKXP_LIBRETRO_WEB", "mkxp-libretro-web", "mkxp-state-compact"),
    "RPGMV_NATIVE": ("rpgmaker_mv", "RPGMAKER", "RPGMV", "NATIVE_WEB", "native-web", "native-save"),
    "RPGMZ_NATIVE": ("rpgmaker_mz", "RPGMAKER", "RPGMZ", "NATIVE_WEB", "native-web", "native-save"),
    "ONS_YURI": ("onscripter_yuri", "ONS", "ONS", "ONS_YURI_WEB", "ons-yuri-web", "ons-save"),
    "KIRIKIRI2_KAG": (
        "kirikiri2", "KIRIKIRI", "KIRIKIRI2", "KIRIKIRI2_WEB", "kirikiri2-web", "kirikiri-kag-bookmark",
    ),
}


class BuildError(RuntimeError):
    """Stable dependency preparation failure."""


def digest(contents: bytes) -> str:
    return hashlib.sha256(contents).hexdigest()


def load_manifest() -> dict[str, Any]:
    try:
        manifest = json.loads((DAT_ROOT / "manifest.json").read_bytes())
    except (OSError, json.JSONDecodeError) as exc:
        raise BuildError("RPG_RUNTIME_MANIFEST_INVALID") from exc
    validate_manifest(manifest)
    return manifest


def safe_path(value: object) -> PurePosixPath:
    if not isinstance(value, str) or "\\" in value or "\x00" in value:
        raise BuildError("RPG_RUNTIME_PATH_INVALID")
    result = PurePosixPath(value)
    if result.is_absolute() or str(result) != value or any(part in ("", ".", "..") for part in result.parts):
        raise BuildError("RPG_RUNTIME_PATH_INVALID")
    return result


def validate_manifest(manifest: object) -> None:
    if not isinstance(manifest, dict) or set(manifest) != {
        "schema_version", "runtime_id", "release", "runtime_files", "artifacts",
    } or manifest.get("schema_version") != 3 or manifest.get("runtime_id") != "retrom-runtime":
        raise BuildError("RPG_RUNTIME_MANIFEST_INVALID")
    release = manifest.get("release")
    if not isinstance(release, dict) or set(release) != {
        "repository", "tag", "tag_commit", "bundle_asset", "metadata_asset",
    }:
        raise BuildError("RPG_RUNTIME_RELEASE_INVALID")
    repository = release.get("repository")
    tag = release.get("tag")
    if repository != "https://github.com/xxxsen/retrom-runtime" or not isinstance(tag, str) or SEMVER_TAG.fullmatch(tag) is None or not isinstance(release.get("tag_commit"), str) or HEX_40.fullmatch(release["tag_commit"]) is None:
        raise BuildError("RPG_RUNTIME_RELEASE_INVALID")
    validate_release_asset(release, release.get("bundle_asset"), f"retrom-runtime-{tag[1:]}.tar.gz", 256 << 20)
    validate_release_asset(release, release.get("metadata_asset"), "retrom-runtime-release.json", 1 << 20)
    validate_runtime_files(manifest.get("runtime_files"), tag)
    validate_artifacts(manifest.get("artifacts"), tag)


def validate_release_asset(release: dict[str, Any], value: object, filename: str, maximum: int) -> None:
    if not isinstance(value, dict) or set(value) != {"filename", "url", "max_size_bytes"}:
        raise BuildError("RPG_RUNTIME_RELEASE_INVALID")
    expected_url = f"{release['repository']}/releases/download/{release['tag']}/{filename}"
    if value.get("filename") != filename or value.get("url") != expected_url or value.get("max_size_bytes") != maximum:
        raise BuildError("RPG_RUNTIME_RELEASE_INVALID")


def validate_runtime_files(value: object, tag: str) -> None:
    if not isinstance(value, list) or len(value) != len(EXPECTED_FILES):
        raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")
    seen: set[str] = set()
    for item in value:
        if not isinstance(item, dict) or set(item) != {
            "bundle_path", "path_in_release", "role", "max_size_bytes",
        }:
            raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")
        bundle_path = str(safe_path(item.get("bundle_path")))
        expected = EXPECTED_FILES.get(bundle_path)
        if expected is None or bundle_path in seen:
            raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")
        filename, role, maximum = expected
        if item.get("path_in_release") != f"{tag}/{filename}" or item.get("role") != role or item.get("max_size_bytes") != maximum:
            raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")
        seen.add(bundle_path)
    if seen != set(EXPECTED_FILES):
        raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")


def validate_artifacts(value: object, tag: str) -> None:
    if not isinstance(value, list) or len(value) != len(EXPECTED_ROUTES):
        raise BuildError("RPG_RUNTIME_ARTIFACT_DECLARATION_INVALID")
    seen: set[str] = set()
    selected: set[str] = set()
    for artifact in value:
        required = {
            "core_id", "runtime_family", "generation", "route_key", "runtime_adapter_kind", "runtime_version",
            "adapter_id", "adapter_abi", "entry_path", "file_paths", "requires_threads",
            "save_payload_kind", "save_max_bytes", "selected_for_new_bindings",
            "available_for_launch", "compatibility",
        }
        if not isinstance(artifact, dict) or set(artifact) != required:
            raise BuildError("RPG_RUNTIME_ARTIFACT_DECLARATION_INVALID")
        route = EXPECTED_ROUTES.get(artifact.get("route_key"))
        actual = tuple(artifact.get(key) for key in (
            "core_id", "runtime_family", "generation", "runtime_adapter_kind", "adapter_id", "adapter_abi",
        ))
        if route is None or actual != route or artifact["route_key"] in seen or artifact.get("runtime_version") != tag:
            raise BuildError("RPG_RUNTIME_ARTIFACT_ROUTE_INVALID")
        if artifact.get("selected_for_new_bindings") is not True or artifact.get("available_for_launch") is not True:
            raise BuildError("RPG_RUNTIME_ARTIFACT_ROUTE_INVALID")
        validate_runtime_compatibility(artifact.get("compatibility"), artifact.get("adapter_abi"))
        paths = artifact.get("file_paths")
        if not isinstance(paths, list) or not paths or len(paths) != len(set(paths)) or any(
            not isinstance(path, str) or not path.startswith(f"{tag}/") for path in paths
        ):
            raise BuildError("RPG_RUNTIME_ARTIFACT_FILES_INVALID")
        seen.add(artifact["route_key"])
        selected.add(artifact["core_id"])
    if seen != set(EXPECTED_ROUTES) or len(selected) != 9:
        raise BuildError("RPG_RUNTIME_ARTIFACT_ROUTE_INVALID")


def validate_runtime_compatibility(value: object, adapter_abi: object) -> None:
    if not isinstance(value, dict) or value.get("adapterAbi") != adapter_abi:
        raise BuildError("RPG_RUNTIME_COMPATIBILITY_INVALID")
    game_line = value.get("gameCompatibilityLine")
    save_abi = value.get("saveAbi")
    readable = value.get("readableSaveAbis")
    if (
        not isinstance(game_line, str)
        or RUNTIME_COMPATIBILITY_IDENTITY.fullmatch(game_line) is None
        or not isinstance(save_abi, str)
        or RUNTIME_COMPATIBILITY_IDENTITY.fullmatch(save_abi) is None
        or not isinstance(readable, list)
        or not 1 <= len(readable) <= 16
        or any(not isinstance(item, str) or RUNTIME_COMPATIBILITY_IDENTITY.fullmatch(item) is None for item in readable)
        or len(set(readable)) != len(readable)
        or save_abi not in readable
    ):
        raise BuildError("RPG_RUNTIME_COMPATIBILITY_INVALID")


def download_bytes(url: str, maximum: int) -> bytes:
    request = urllib.request.Request(url, headers={"User-Agent": "retrom-dependency-preparer/1"})
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            if not response.geturl().startswith("https://"):
                raise BuildError("RPG_RUNTIME_RELEASE_REDIRECT_INVALID")
            contents = response.read(maximum + 1)
    except urllib.error.URLError as exc:
        raise BuildError("RPG_RUNTIME_RELEASE_DOWNLOAD_FAILED") from exc
    if not contents or len(contents) > maximum:
        raise BuildError("RPG_RUNTIME_RELEASE_SIZE_INVALID")
    return contents


def validate_release_metadata(manifest: dict[str, Any], contents: bytes) -> dict[str, dict[str, Any]]:
    try:
        metadata = json.loads(contents)
    except (UnicodeError, json.JSONDecodeError) as exc:
        raise BuildError("RPG_RUNTIME_RELEASE_METADATA_INVALID") from exc
    release = manifest["release"]
    required = {"schemaVersion", "repository", "tag", "commit", "version", "publicApiVersion", "files"}
    if not isinstance(metadata, dict) or set(metadata) != required or metadata.get("schemaVersion") != 1 or metadata.get("repository") != release["repository"] or metadata.get("tag") != release["tag"] or metadata.get("commit") != release["tag_commit"] or metadata.get("version") != release["tag"][1:] or metadata.get("publicApiVersion") != PUBLIC_API_VERSION or not isinstance(metadata.get("files"), list):
        raise BuildError("RPG_RUNTIME_RELEASE_METADATA_INVALID")
    records: dict[str, dict[str, Any]] = {}
    for record in metadata["files"]:
        if not isinstance(record, dict) or set(record) != {"path", "filename", "sizeBytes", "sha256"}:
            raise BuildError("RPG_RUNTIME_RELEASE_METADATA_INVALID")
        path = str(safe_path(record.get("path")))
        if path in records or not isinstance(record.get("sizeBytes"), int) or record["sizeBytes"] < 1:
            raise BuildError("RPG_RUNTIME_RELEASE_METADATA_INVALID")
        records[path] = record
    for item in manifest["runtime_files"]:
        record = records.get(item["bundle_path"])
        if record is None or record.get("filename") != PurePosixPath(item["bundle_path"]).name or record["sizeBytes"] > item["max_size_bytes"]:
            raise BuildError("RPG_RUNTIME_RELEASE_METADATA_INVALID")
    return records


def extract_runtime_files(manifest: dict[str, Any], archive: bytes) -> dict[str, bytes]:
    declarations = {item["bundle_path"]: item for item in manifest["runtime_files"]}
    extracted: dict[str, bytes] = {}
    try:
        with tarfile.open(fileobj=io.BytesIO(archive), mode="r:gz") as source:
            for member in source.getmembers():
                name = member.name.removeprefix("./")
                if not name or name == ".":
                    continue
                safe_path(name.rstrip("/"))
                if member.issym() or member.islnk() or not (member.isdir() or member.isfile()):
                    raise BuildError("RPG_RUNTIME_RELEASE_ARCHIVE_INVALID")
                declaration = declarations.get(name)
                if declaration is None or member.isdir():
                    continue
                if name in extracted or member.size < 1 or member.size > declaration["max_size_bytes"]:
                    raise BuildError("RPG_RUNTIME_RELEASE_ARCHIVE_INVALID")
                stream = source.extractfile(member)
                if stream is None:
                    raise BuildError("RPG_RUNTIME_RELEASE_ARCHIVE_INVALID")
                contents = stream.read(declaration["max_size_bytes"] + 1)
                if len(contents) != member.size:
                    raise BuildError("RPG_RUNTIME_RELEASE_ARCHIVE_INVALID")
                extracted[name] = contents
    except (tarfile.TarError, OSError) as exc:
        raise BuildError("RPG_RUNTIME_RELEASE_ARCHIVE_INVALID") from exc
    if set(extracted) != set(declarations):
        raise BuildError("RPG_RUNTIME_RELEASE_ARCHIVE_INCOMPLETE")
    return extracted


def observed_document(manifest: dict[str, Any], files: dict[str, bytes]) -> dict[str, Any]:
    release = manifest["release"]
    return {
        "schema_version": 1,
        "repository": release["repository"],
        "tag": release["tag"],
        "tag_commit": release["tag_commit"],
        "bundle_filename": release["bundle_asset"]["filename"],
        "files": {
            item["path_in_release"]: {
                "observed_size_bytes": len(files[item["bundle_path"]]),
                "observed_sha256": digest(files[item["bundle_path"]]),
            }
            for item in manifest["runtime_files"]
        },
    }


def publish_runtime(manifest: dict[str, Any], files: dict[str, bytes], runtime_root: Path) -> None:
    runtime_root.parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=".rpg-runtime-", dir=runtime_root.parent))
    backup = runtime_root.with_name(f".{runtime_root.name}.previous")
    try:
        for item in manifest["runtime_files"]:
            target = staging / safe_path(item["path_in_release"])
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(files[item["bundle_path"]])
            os.chmod(target, 0o600)
        observed = observed_document(manifest, files)
        (staging / OBSERVED_FILENAME).write_text(json.dumps(observed, indent=2) + "\n", encoding="utf-8")
        os.chmod(staging / OBSERVED_FILENAME, 0o600)
        if backup.exists():
            shutil.rmtree(backup)
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


def verify_runtime(manifest: dict[str, Any], runtime_root: Path) -> None:
    verify_dev_override(runtime_root)
    try:
        observed = json.loads((runtime_root / OBSERVED_FILENAME).read_bytes())
    except (OSError, json.JSONDecodeError) as exc:
        raise BuildError("RPG_RUNTIME_RELEASE_OBSERVED_INVALID") from exc
    release = manifest["release"]
    if not isinstance(observed, dict) or set(observed) != {"schema_version", "repository", "tag", "tag_commit", "bundle_filename", "files"} or observed.get("schema_version") != 1 or tuple(observed.get(key) for key in ("repository", "tag", "tag_commit", "bundle_filename")) != (release["repository"], release["tag"], release["tag_commit"], release["bundle_asset"]["filename"]) or not isinstance(observed.get("files"), dict):
        raise BuildError("RPG_RUNTIME_RELEASE_OBSERVED_INVALID")
    expected_paths = {item["path_in_release"] for item in manifest["runtime_files"]}
    if set(observed["files"]) != expected_paths:
        raise BuildError("RPG_RUNTIME_RELEASE_OBSERVED_INVALID")
    for item in manifest["runtime_files"]:
        record = observed["files"][item["path_in_release"]]
        if not isinstance(record, dict) or set(record) != {"observed_size_bytes", "observed_sha256"} or not isinstance(record.get("observed_size_bytes"), int) or record["observed_size_bytes"] < 1 or record["observed_size_bytes"] > item["max_size_bytes"] or not isinstance(record.get("observed_sha256"), str) or len(record["observed_sha256"]) != 64:
            raise BuildError("RPG_RUNTIME_RELEASE_OBSERVED_INVALID")
        target = runtime_root / safe_path(item["path_in_release"])
        try:
            info = target.lstat()
            contents = target.read_bytes()
        except OSError as exc:
            raise BuildError(f"RPG_RUNTIME_FILE_MISSING:{target.name}") from exc
        if not stat.S_ISREG(info.st_mode) or len(contents) != record["observed_size_bytes"] or digest(contents) != record["observed_sha256"]:
            raise BuildError(f"RPG_RUNTIME_FILE_MISMATCH:{target.name}")


def verify_dev_override(runtime_root: Path) -> None:
    marker_path = runtime_root / DEV_MARKER_FILENAME
    if not marker_path.exists():
        return
    try:
        marker = json.loads(marker_path.read_bytes())
        configured = Path(os.environ["RETROM_RUNTIME_DEV_ROOT"]).resolve(strict=True)
    except (KeyError, OSError, json.JSONDecodeError) as exc:
        raise BuildError("RPG_RUNTIME_DEV_OVERRIDE_ACTIVE") from exc
    if (
        not isinstance(marker, dict)
        or set(marker) != {
            "schema_version", "source_root", "source_commit", "package_version", "overlaid_assets",
        }
        or marker.get("schema_version") != 1
        or marker.get("source_root") != str(configured)
        or not isinstance(marker.get("source_commit"), str)
        or HEX_40.fullmatch(marker["source_commit"]) is None
        or not isinstance(marker.get("package_version"), str)
        or not isinstance(marker.get("overlaid_assets"), list)
    ):
        raise BuildError("RPG_RUNTIME_DEV_OVERRIDE_INVALID")


def prepare(manifest: dict[str, Any], runtime_root: Path, offline: bool) -> None:
    try:
        verify_runtime(manifest, runtime_root)
        return
    except BuildError:
        if offline:
            raise BuildError("RPG_RUNTIME_RELEASE_REQUIRED")
    release = manifest["release"]
    metadata = download_bytes(release["metadata_asset"]["url"], release["metadata_asset"]["max_size_bytes"])
    validate_release_metadata(manifest, metadata)
    bundle = download_bytes(release["bundle_asset"]["url"], release["bundle_asset"]["max_size_bytes"])
    files = extract_runtime_files(manifest, bundle)
    publish_runtime(manifest, files, runtime_root)
    verify_runtime(manifest, runtime_root)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("data-check", "prepare", "deps-check"))
    parser.add_argument("--runtime-root", type=Path, default=DEFAULT_RUNTIME_ROOT)
    parser.add_argument("--offline", action="store_true")
    args = parser.parse_args()
    manifest = load_manifest()
    if args.action == "prepare":
        prepare(manifest, args.runtime_root, args.offline)
    elif args.action == "deps-check":
        verify_runtime(manifest, args.runtime_root)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (BuildError, OSError, KeyError, TypeError, ValueError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1) from error
