#!/usr/bin/env python3
"""Validate and materialize Retrom's version-pinned third-party dependencies."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request
from pathlib import Path, PurePosixPath
from typing import Any


REPOSITORY_ROOT = Path(__file__).resolve().parent.parent
DATA_ROOT = REPOSITORY_ROOT / "data"
HEX_64 = re.compile(r"^[0-9a-f]{64}$")
PINNED_RAW = re.compile(
    r"^https://raw\.githubusercontent\.com/[^/]+/[^/]+/[0-9a-f]{40}/.+$"
)
CORE_IDS = (
    "fceumm",
    "snes9x",
    "gambatte",
    "mgba",
    "fbneo",
    "mame2003",
    "mame2003_plus",
    "dosbox_pure",
)


class CheckError(RuntimeError):
    """A stable validation failure suitable for command-line output."""


def parse_versions(raw: str) -> list[str]:
    versions = raw.split(",")
    if not raw or any(not value or value.strip() != value for value in versions):
        raise CheckError("DEPENDENCY_VERSION_LIST_INVALID")
    if len(set(versions)) != len(versions):
        raise CheckError("DEPENDENCY_VERSION_LIST_DUPLICATE")
    key = lambda value: tuple(int(part) for part in value.split("."))
    try:
        if any(len(value.split(".")) != 3 for value in versions):
            raise ValueError
        if versions != sorted(versions, key=key):
            raise CheckError("DEPENDENCY_VERSION_LIST_NOT_SORTED")
    except ValueError as exc:
        raise CheckError("DEPENDENCY_VERSION_INVALID") from exc
    return versions


def load_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise CheckError(f"DEPENDENCY_MANIFEST_INVALID:{path.name}") from exc
    if not isinstance(value, dict):
        raise CheckError(f"DEPENDENCY_MANIFEST_INVALID:{path.name}")
    return value


def safe_relative_path(value: object, code: str) -> str:
    if not isinstance(value, str) or not value or "\\" in value or "\x00" in value:
        raise CheckError(code)
    path = PurePosixPath(value)
    if path.is_absolute() or any(part in ("", ".", "..") for part in path.parts):
        raise CheckError(code)
    return value


def expect_digest(value: object, code: str) -> str:
    if not isinstance(value, str) or HEX_64.fullmatch(value) is None:
        raise CheckError(code)
    return value


def file_digest(path: Path) -> tuple[int, str]:
    digest = hashlib.sha256()
    size = 0
    try:
        with path.open("rb") as source:
            while chunk := source.read(1024 * 1024):
                size += len(chunk)
                digest.update(chunk)
    except OSError as exc:
        raise CheckError(f"DEPENDENCY_FILE_MISSING:{path.name}") from exc
    return size, digest.hexdigest()


def check_file(path: Path, size: object, sha256: object) -> None:
    if not isinstance(size, int) or isinstance(size, bool) or size < 0:
        raise CheckError(f"DEPENDENCY_SIZE_INVALID:{path.name}")
    expected = expect_digest(sha256, f"DEPENDENCY_SHA256_INVALID:{path.name}")
    actual_size, actual_digest = file_digest(path)
    if actual_size != size or actual_digest != expected:
        raise CheckError(f"DEPENDENCY_FILE_MISMATCH:{path.name}")


def manifest_path(version: str) -> Path:
    return DATA_ROOT / "dat" / "emulatorjs" / version / "manifest.json"


def load_manifest(version: str) -> dict[str, Any]:
    manifest = load_json(manifest_path(version))
    if manifest.get("schema_version") != 3:
        raise CheckError(f"DEPENDENCY_SCHEMA_UNSUPPORTED:{version}")
    emulatorjs = manifest.get("emulatorjs")
    if not isinstance(emulatorjs, dict) or emulatorjs.get("version") != version:
        raise CheckError(f"DEPENDENCY_VERSION_MISMATCH:{version}")
    return manifest


def validate_registry(manifests: list[dict[str, Any]]) -> None:
    registry_path = REPOSITORY_ROOT / "web/features/player/adapters/registry.json"
    registry = load_json(registry_path)
    entries = registry.get("adapters")
    if registry.get("schemaVersion") != 1 or not isinstance(entries, list):
        raise CheckError("PLAYER_ADAPTER_REGISTRY_INVALID")
    registered: dict[str, str] = {}
    for raw in entries:
        if not isinstance(raw, dict) or set(raw) != {"id", "version", "module"}:
            raise CheckError("PLAYER_ADAPTER_REGISTRY_INVALID")
        adapter_id = raw.get("id")
        version = raw.get("version")
        module = safe_relative_path(raw.get("module"), "PLAYER_ADAPTER_MODULE_INVALID")
        if not isinstance(adapter_id, str) or not isinstance(version, str):
            raise CheckError("PLAYER_ADAPTER_REGISTRY_INVALID")
        if adapter_id in registered:
            raise CheckError("PLAYER_ADAPTER_REGISTRY_DUPLICATE")
        module_path = REPOSITORY_ROOT / "web/features/player/adapters" / module
        if not module_path.is_file():
            raise CheckError(f"PLAYER_ADAPTER_IMPLEMENTATION_MISSING:{adapter_id}")
        registered[adapter_id] = version

    expected: dict[str, str] = {}
    for manifest in manifests:
        emulatorjs = manifest["emulatorjs"]
        adapter = emulatorjs.get("player_adapter")
        if not isinstance(adapter, dict):
            raise CheckError("PLAYER_ADAPTER_MANIFEST_INVALID")
        adapter_id = adapter.get("id")
        version = emulatorjs.get("version")
        if not isinstance(adapter_id, str) or not isinstance(version, str):
            raise CheckError("PLAYER_ADAPTER_MANIFEST_INVALID")
        if adapter_id in expected:
            raise CheckError("PLAYER_ADAPTER_MANIFEST_DUPLICATE")
        runtime_base = safe_relative_path(
            adapter.get("runtime_base_path_in_release"), "PLAYER_ADAPTER_RUNTIME_PATH_INVALID"
        )
        loader = safe_relative_path(
            adapter.get("loader_path_in_release"), "PLAYER_ADAPTER_LOADER_PATH_INVALID"
        )
        if not runtime_base.endswith("/") or not loader.startswith(runtime_base):
            raise CheckError("PLAYER_ADAPTER_LOADER_PATH_INVALID")
        allowlist = {
            item.get("path_in_release")
            for item in emulatorjs.get("runtime_allowlist", [])
            if isinstance(item, dict)
        }
        if loader not in allowlist:
            raise CheckError("PLAYER_ADAPTER_LOADER_NOT_ALLOWLISTED")
        expected[adapter_id] = version
    if registered != expected:
        raise CheckError("PLAYER_ADAPTER_REGISTRY_DRIFT")


def validate_small_manifest(version: str, manifest: dict[str, Any]) -> None:
    emulatorjs = manifest["emulatorjs"]
    allowlist = emulatorjs.get("runtime_allowlist")
    selected = emulatorjs.get("selected_core_artifacts")
    if not isinstance(allowlist, list) or not isinstance(selected, list):
        raise CheckError(f"DEPENDENCY_MANIFEST_INVALID:{version}")
    paths: set[str] = set()
    for item in allowlist:
        if not isinstance(item, dict):
            raise CheckError("DEPENDENCY_ALLOWLIST_INVALID")
        path = safe_relative_path(item.get("path_in_release"), "DEPENDENCY_ALLOWLIST_PATH_INVALID")
        if path in paths:
            raise CheckError("DEPENDENCY_ALLOWLIST_DUPLICATE")
        paths.add(path)
        if not isinstance(item.get("size_bytes"), int):
            raise CheckError("DEPENDENCY_ALLOWLIST_SIZE_INVALID")
        expect_digest(item.get("sha256"), "DEPENDENCY_ALLOWLIST_SHA256_INVALID")
    selected_ids: list[str] = []
    for item in selected:
        if not isinstance(item, dict):
            raise CheckError("DEPENDENCY_CORE_INVALID")
        core_id = item.get("core_id")
        if not isinstance(core_id, str):
            raise CheckError("DEPENDENCY_CORE_INVALID")
        selected_ids.append(core_id)
        path_value = item.get("path_in_release") or item.get("local_path")
        safe_relative_path(path_value, "DEPENDENCY_CORE_PATH_INVALID")
        if not isinstance(item.get("size_bytes"), int):
            raise CheckError("DEPENDENCY_CORE_SIZE_INVALID")
        expect_digest(item.get("sha256"), "DEPENDENCY_CORE_SHA256_INVALID")
    if tuple(selected_ids) != CORE_IDS:
        raise CheckError("DEPENDENCY_CORE_SELECTION_INVALID")

    licenses = manifest.get("license_materialization")
    if not isinstance(licenses, dict):
        raise CheckError("DEPENDENCY_LICENSE_MANIFEST_INVALID")
    entries = licenses.get("entries")
    order = licenses.get("notice_order")
    if not isinstance(entries, list) or order != [entry.get("component_id") for entry in entries]:
        raise CheckError("DEPENDENCY_LICENSE_ORDER_INVALID")
    if tuple(order) != ("emulatorjs",) + CORE_IDS:
        raise CheckError("DEPENDENCY_LICENSE_COMPONENTS_INVALID")
    for entry in entries:
        if not isinstance(entry, dict):
            raise CheckError("DEPENDENCY_LICENSE_ENTRY_INVALID")
        safe_relative_path(entry.get("output_relative_path"), "DEPENDENCY_LICENSE_PATH_INVALID")
        expect_digest(entry.get("sha256"), "DEPENDENCY_LICENSE_SHA256_INVALID")
        if not isinstance(entry.get("size_bytes"), int):
            raise CheckError("DEPENDENCY_LICENSE_SIZE_INVALID")
        materialization = entry.get("materialization")
        if not isinstance(materialization, dict):
            raise CheckError("DEPENDENCY_LICENSE_SOURCE_INVALID")
        source_type = materialization.get("type")
        if source_type == "PINNED_RAW_FILE":
            url = materialization.get("url")
            if not isinstance(url, str) or PINNED_RAW.fullmatch(url) is None:
                raise CheckError("DEPENDENCY_LICENSE_SOURCE_INVALID")
            if entry.get("source_commit") not in url:
                raise CheckError("DEPENDENCY_LICENSE_COMMIT_MISMATCH")
        elif source_type == "RELEASE_ENTRY":
            safe_relative_path(materialization.get("path_in_release"), "DEPENDENCY_LICENSE_SOURCE_INVALID")
        else:
            raise CheckError("DEPENDENCY_LICENSE_SOURCE_INVALID")

    dat_lines = (DATA_ROOT / "dat/emulatorjs" / version / "SHA256SUMS").read_text(
        encoding="utf-8"
    ).splitlines()
    sums: dict[str, str] = {}
    for line in dat_lines:
        if not line:
            continue
        digest, separator, path = line.partition("  ")
        if not separator or not HEX_64.fullmatch(digest):
            raise CheckError("DEPENDENCY_SHA256SUMS_INVALID")
        safe_relative_path(path, "DEPENDENCY_SHA256SUMS_INVALID")
        sums[path] = digest
    cores = manifest.get("cores")
    if not isinstance(cores, list) or len(cores) != 3:
        raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")
    expected_sums: dict[str, str] = {}
    for core in cores:
        if not isinstance(core, dict) or not isinstance(core.get("dat"), dict):
            raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")
        dat = core["dat"]
        local_path = safe_relative_path(dat.get("local_path"), "DEPENDENCY_DAT_PATH_INVALID")
        expected_sums[local_path] = expect_digest(dat.get("sha256"), "DEPENDENCY_DAT_SHA256_INVALID")
    if sums != expected_sums:
        raise CheckError("DEPENDENCY_SHA256SUMS_DRIFT")


def runtime_path(version: str, relative: str) -> Path:
    return DATA_ROOT / "runtime/emulatorjs" / version / relative


def check_payload(version: str, manifest: dict[str, Any]) -> None:
    emulatorjs = manifest["emulatorjs"]
    for item in emulatorjs["runtime_allowlist"]:
        check_file(
            runtime_path(version, item["path_in_release"]),
            item["size_bytes"],
            item["sha256"],
        )
    for item in emulatorjs["selected_core_artifacts"]:
        relative = item.get("path_in_release") or item.get("local_path")
        check_file(runtime_path(version, relative), item["size_bytes"], item["sha256"])
    dat_root = DATA_ROOT / "dat/emulatorjs" / version
    for core in manifest["cores"]:
        dat = core["dat"]
        check_file(dat_root / dat["local_path"], dat["size_bytes"], dat["sha256"])
    for entry in manifest["license_materialization"]["entries"]:
        check_file(
            runtime_path(version, entry["output_relative_path"]),
            entry["size_bytes"],
            entry["sha256"],
        )
    notice = render_notice(manifest)
    notice_path = runtime_path(
        version, manifest["license_materialization"]["third_party_notices_relative_path"]
    )
    try:
        actual_notice = notice_path.read_bytes()
    except OSError as exc:
        raise CheckError("DEPENDENCY_NOTICE_MISSING") from exc
    if actual_notice != notice:
        raise CheckError("DEPENDENCY_NOTICE_MISMATCH")


def download(url: str, target: Path, size: int, sha256: str) -> None:
    target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    request = urllib.request.Request(url, headers={"User-Agent": "retrom-dependency-preparer/1"})
    temp_fd, temp_name = tempfile.mkstemp(prefix=".retrom-download-", dir=target.parent)
    os.close(temp_fd)
    temp_path = Path(temp_name)
    try:
        digest = hashlib.sha256()
        written = 0
        with urllib.request.urlopen(request, timeout=30) as response, temp_path.open("wb") as output:
            final_url = response.geturl()
            if not final_url.startswith("https://"):
                raise CheckError("DEPENDENCY_REDIRECT_SCHEME_INVALID")
            while chunk := response.read(1024 * 1024):
                written += len(chunk)
                if written > size:
                    raise CheckError("DEPENDENCY_DOWNLOAD_TOO_LARGE")
                digest.update(chunk)
                output.write(chunk)
            output.flush()
            os.fsync(output.fileno())
        if written != size or digest.hexdigest() != sha256:
            raise CheckError("DEPENDENCY_DOWNLOAD_MISMATCH")
        os.chmod(temp_path, 0o600)
        os.replace(temp_path, target)
        directory_fd = os.open(target.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
    finally:
        temp_path.unlink(missing_ok=True)


def ensure_release(version: str, manifest: dict[str, Any]) -> None:
    release = manifest["emulatorjs"]["release_asset"]
    archive = runtime_path(version, release["name"])
    try:
        check_file(archive, release["size_bytes"], release["sha256"])
    except CheckError:
        download(release["url"], archive, release["size_bytes"], release["sha256"])
    required = [item["path_in_release"] for item in manifest["emulatorjs"]["runtime_allowlist"]]
    required.extend(
        item["path_in_release"]
        for item in manifest["emulatorjs"]["selected_core_artifacts"]
        if item.get("path_in_release") is not None
    )
    if all(runtime_path(version, relative).is_file() for relative in required):
        return
    seven_zip = shutil.which("7z") or shutil.which("7zz")
    if seven_zip is None:
        raise CheckError("DEPENDENCY_7Z_TOOL_MISSING")
    subprocess.run(
        [seven_zip, "x", "-y", f"-o{runtime_path(version, '.')}" , str(archive)],
        check=True,
        stdout=subprocess.DEVNULL,
    )


def ensure_dat(version: str, manifest: dict[str, Any]) -> None:
    dat_root = DATA_ROOT / "dat/emulatorjs" / version
    for core in manifest["cores"]:
        dat = core["dat"]
        target = dat_root / dat["local_path"]
        try:
            check_file(target, dat["size_bytes"], dat["sha256"])
            continue
        except CheckError:
            pass
        materialization = dat["materialization"]
        if materialization["strategy"] == "download_exact_file":
            download(materialization["url"], target, dat["size_bytes"], dat["sha256"])
            continue
        if materialization["strategy"] != "download_pinned_snapshot_then_replace_exact_bytes":
            raise CheckError("DEPENDENCY_DAT_STRATEGY_UNSUPPORTED")
        base_size = materialization["base_size_bytes"]
        base_digest = materialization["base_sha256"]
        temp_dir = Path(tempfile.mkdtemp(prefix="retrom-dat-", dir=dat_root))
        try:
            base = temp_dir / "base.dat"
            download(materialization["base_url"], base, base_size, base_digest)
            contents = base.read_bytes()
            for replacement in materialization["byte_replacements"]:
                old = replacement["from_utf8"].encode()
                new = replacement["to_utf8"].encode()
                if contents.count(old) != replacement["expected_count"]:
                    raise CheckError("DEPENDENCY_DAT_REPLACEMENT_COUNT_MISMATCH")
                contents = contents.replace(old, new)
            target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            candidate = target.with_name(f".{target.name}.candidate")
            candidate.write_bytes(contents)
            os.chmod(candidate, 0o600)
            check_file(candidate, dat["size_bytes"], dat["sha256"])
            os.replace(candidate, target)
        finally:
            shutil.rmtree(temp_dir, ignore_errors=True)


def ensure_override(version: str, manifest: dict[str, Any]) -> None:
    for core in manifest["cores"]:
        override = core.get("tested_runtime_override")
        if not isinstance(override, dict):
            continue
        target = runtime_path(version, Path(override["local_path_from_repository_root"]).relative_to(
            f"data/runtime/emulatorjs/{version}"
        ).as_posix())
        try:
            check_file(target, override["size_bytes"], override["sha256"])
        except CheckError:
            download(override["official_url"], target, override["size_bytes"], override["sha256"])


def ensure_licenses(version: str, manifest: dict[str, Any]) -> None:
    archive_root = runtime_path(version, ".")
    for entry in manifest["license_materialization"]["entries"]:
        target = runtime_path(version, entry["output_relative_path"])
        try:
            check_file(target, entry["size_bytes"], entry["sha256"])
            continue
        except CheckError:
            pass
        source = entry["materialization"]
        if source["type"] == "PINNED_RAW_FILE":
            download(source["url"], target, entry["size_bytes"], entry["sha256"])
        else:
            release_path = archive_root / source["path_in_release"]
            target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            shutil.copyfile(release_path, target)
            os.chmod(target, 0o600)
            check_file(target, entry["size_bytes"], entry["sha256"])


def render_notice(manifest: dict[str, Any]) -> bytes:
    chunks: list[bytes] = []
    version = manifest["emulatorjs"]["version"]
    for entry in manifest["license_materialization"]["entries"]:
        header = (
            "===============================================================================\n"
            f"Component: {entry['component_id']}\n"
            f"Source: {entry['repository']}/tree/{entry['source_commit']}\n"
            f"Binary association: {entry['binary_association_status']}\n"
            f"Declared license path: {entry['declared_license_path']}\n"
            "-------------------------------------------------------------------------------\n"
        ).encode("ascii")
        license_bytes = runtime_path(version, entry["output_relative_path"]).read_bytes()
        chunks.append(header + license_bytes.rstrip(b"\n") + b"\n")
    return b"\n".join(chunks)


def prepare(version: str, manifest: dict[str, Any]) -> None:
    ensure_release(version, manifest)
    ensure_override(version, manifest)
    ensure_dat(version, manifest)
    ensure_licenses(version, manifest)
    notice_path = runtime_path(
        version, manifest["license_materialization"]["third_party_notices_relative_path"]
    )
    notice = render_notice(manifest)
    notice_path.write_bytes(notice)
    os.chmod(notice_path, 0o600)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("data-check", "prepare", "deps-check"))
    parser.add_argument("--versions", required=True)
    args = parser.parse_args()
    try:
        versions = parse_versions(args.versions)
        manifests = [load_manifest(version) for version in versions]
        for version, manifest in zip(versions, manifests, strict=True):
            validate_small_manifest(version, manifest)
        validate_registry(manifests)
        if args.action == "prepare":
            for version, manifest in zip(versions, manifests, strict=True):
                prepare(version, manifest)
        if args.action in ("prepare", "deps-check"):
            for version, manifest in zip(versions, manifests, strict=True):
                check_payload(version, manifest)
        print(f"{args.action}: ok ({','.join(versions)})")
        return 0
    except (CheckError, OSError, KeyError, TypeError, ValueError, urllib.error.URLError) as exc:
        print(str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
