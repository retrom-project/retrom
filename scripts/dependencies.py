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
RUNTIME_CORE_ID = re.compile(r"^[a-z0-9_]{1,64}$")
ARTIFACT_BASENAME = re.compile(r"^[A-Za-z0-9_.-]+-wasm\.data$")
DANGEROUS_OPTION_KEYS = {"__proto__", "constructor", "prototype"}
SEMVER = re.compile(
    r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$"
)


class CheckError(RuntimeError):
    """A stable validation failure suitable for command-line output."""


def parse_versions(raw: str) -> list[str]:
    versions = raw.split(",")
    if not raw or any(not value or value.strip() != value for value in versions):
        raise CheckError("DEPENDENCY_VERSION_LIST_INVALID")
    if len(set(versions)) != len(versions):
        raise CheckError("DEPENDENCY_VERSION_LIST_DUPLICATE")
    parsed = [parse_semver(value) for value in versions]
    if any(compare_semver(parsed[index - 1], parsed[index]) >= 0 for index in range(1, len(parsed))):
        raise CheckError("DEPENDENCY_VERSION_LIST_NOT_SORTED")
    return versions


def parse_semver(value: str) -> tuple[tuple[int, int, int], tuple[tuple[int, int | str], ...] | None]:
    matched = SEMVER.fullmatch(value)
    if matched is None:
        raise CheckError("DEPENDENCY_VERSION_INVALID")
    prerelease = matched.group(4)
    identifiers: tuple[tuple[int, int | str], ...] | None = None
    if prerelease is not None:
        parts: list[tuple[int, int | str]] = []
        for identifier in prerelease.split("."):
            if identifier.isdigit():
                if len(identifier) > 1 and identifier.startswith("0"):
                    raise CheckError("DEPENDENCY_VERSION_INVALID")
                parts.append((0, int(identifier)))
            else:
                parts.append((1, identifier))
        identifiers = tuple(parts)
    return (int(matched.group(1)), int(matched.group(2)), int(matched.group(3))), identifiers


def compare_semver(
    left: tuple[tuple[int, int, int], tuple[tuple[int, int | str], ...] | None],
    right: tuple[tuple[int, int, int], tuple[tuple[int, int | str], ...] | None],
) -> int:
    if left[0] != right[0]:
        return -1 if left[0] < right[0] else 1
    if left[1] == right[1]:
        return 0
    if left[1] is None:
        return 1
    if right[1] is None:
        return -1
    return -1 if left[1] < right[1] else 1


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
    if manifest.get("schema_version") != 4:
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
    runtime_ids: set[str] = set()
    selected_paths: set[str] = set()
    for item in selected:
        if not isinstance(item, dict):
            raise CheckError("DEPENDENCY_CORE_INVALID")
        core_id = item.get("core_id")
        runtime_core_id = item.get("runtime_core_id")
        if not isinstance(core_id, str) or core_id in selected_ids:
            raise CheckError("DEPENDENCY_CORE_INVALID")
        if not isinstance(runtime_core_id, str) or RUNTIME_CORE_ID.fullmatch(runtime_core_id) is None:
            raise CheckError("DEPENDENCY_RUNTIME_CORE_INVALID")
        if runtime_core_id in runtime_ids:
            raise CheckError("DEPENDENCY_RUNTIME_CORE_DUPLICATE")
        selected_ids.append(core_id)
        runtime_ids.add(runtime_core_id)
        path_value = item.get("path_in_release") or item.get("local_path")
        safe_relative_path(path_value, "DEPENDENCY_CORE_PATH_INVALID")
        if path_value in selected_paths:
            raise CheckError("DEPENDENCY_CORE_PATH_DUPLICATE")
        selected_paths.add(path_value)
        if not isinstance(item.get("size_bytes"), int):
            raise CheckError("DEPENDENCY_CORE_SIZE_INVALID")
        expect_digest(item.get("sha256"), "DEPENDENCY_CORE_SHA256_INVALID")
        validate_artifact_capability(item, paths)

    auxiliary = emulatorjs.get("auxiliary_files")
    if not isinstance(auxiliary, list):
        raise CheckError("DEPENDENCY_AUXILIARY_INVALID")
    auxiliary_paths: set[str] = set()
    for item in auxiliary:
        if not isinstance(item, dict) or item.get("component_id") not in selected_ids:
            raise CheckError("DEPENDENCY_AUXILIARY_INVALID")
        auxiliary_path = safe_relative_path(
            item.get("path_in_release"), "DEPENDENCY_AUXILIARY_PATH_INVALID"
        )
        if auxiliary_path in auxiliary_paths or auxiliary_path not in paths:
            raise CheckError("DEPENDENCY_AUXILIARY_ALLOWLIST_INVALID")
        auxiliary_paths.add(auxiliary_path)
        if not isinstance(item.get("size_bytes"), int):
            raise CheckError("DEPENDENCY_AUXILIARY_SIZE_INVALID")
        expect_digest(item.get("sha256"), "DEPENDENCY_AUXILIARY_SHA256_INVALID")

    licenses = manifest.get("license_materialization")
    if not isinstance(licenses, dict):
        raise CheckError("DEPENDENCY_LICENSE_MANIFEST_INVALID")
    entries = licenses.get("components")
    order = licenses.get("notice_order")
    if not isinstance(entries, list) or order != [entry.get("component_id") for entry in entries]:
        raise CheckError("DEPENDENCY_LICENSE_ORDER_INVALID")
    if order != ["emulatorjs", *selected_ids] or len(set(order)) != len(order):
        raise CheckError("DEPENDENCY_LICENSE_COMPONENTS_INVALID")
    component_ids = {
        component.get("component_id") for component in entries if isinstance(component, dict)
    }
    if any(item.get("source_component_id") not in component_ids for item in selected):
        raise CheckError("DEPENDENCY_CORE_COMPONENT_INVALID")
    for component in entries:
        if not isinstance(component, dict):
            raise CheckError("DEPENDENCY_LICENSE_ENTRY_INVALID")
        files = component.get("license_files")
        if not isinstance(files, list) or not files:
            raise CheckError("DEPENDENCY_LICENSE_FILES_EMPTY")
        output_paths = [entry.get("output_relative_path") for entry in files if isinstance(entry, dict)]
        if len(output_paths) != len(files) or output_paths != sorted(output_paths, key=lambda value: value.encode("utf-8")):
            raise CheckError("DEPENDENCY_LICENSE_FILE_ORDER_INVALID")
        for entry in files:
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
                if component.get("source_commit") not in url:
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
    if not isinstance(cores, list):
        raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")
    expected_sums: dict[str, str] = {}
    for core in cores:
        if not isinstance(core, dict):
            raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")
        if "dat" not in core:
            continue
        if not isinstance(core.get("dat"), dict):
            raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")
        dat = core["dat"]
        local_path = safe_relative_path(dat.get("local_path"), "DEPENDENCY_DAT_PATH_INVALID")
        expected_sums[local_path] = expect_digest(dat.get("sha256"), "DEPENDENCY_DAT_SHA256_INVALID")
    if sums != expected_sums:
        raise CheckError("DEPENDENCY_SHA256SUMS_DRIFT")
    if len(expected_sums) not in (0, 3):
        raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")


def validate_artifact_capability(item: dict[str, Any], allowlist_paths: set[str]) -> None:
    basename = item.get("requested_artifact_basename")
    if not isinstance(basename, str) or ARTIFACT_BASENAME.fullmatch(basename) is None or ".." in basename:
        raise CheckError("DEPENDENCY_ARTIFACT_BASENAME_INVALID")
    threaded = item.get("requires_threads")
    if not isinstance(threaded, bool) or ("-thread-wasm.data" in basename) != threaded:
        raise CheckError("DEPENDENCY_ARTIFACT_THREAD_MISMATCH")
    policy = item.get("canvas_resize_policy")
    if policy not in ("NONE", "ON_GAME_START_TO_CSS_PIXELS"):
        raise CheckError("DEPENDENCY_CANVAS_POLICY_INVALID")
    options = item.get("default_options")
    if not isinstance(options, dict) or len(options) > 32:
        raise CheckError("DEPENDENCY_CORE_OPTIONS_INVALID")
    for key, value in options.items():
        if key in DANGEROUS_OPTION_KEYS or not isinstance(key, str) or not isinstance(value, str):
            raise CheckError("DEPENDENCY_CORE_OPTIONS_INVALID")
        try:
            encoded_key = key.encode("ascii")
            encoded_value = value.encode("ascii")
        except UnicodeEncodeError as exc:
            raise CheckError("DEPENDENCY_CORE_OPTIONS_INVALID") from exc
        if not 1 <= len(encoded_key) <= 128 or len(encoded_value) > 128 or any(byte < 0x20 or byte > 0x7E for byte in (*encoded_key, *encoded_value)):
            raise CheckError("DEPENDENCY_CORE_OPTIONS_INVALID")
    mode = item.get("persistent_save_mode")
    kind = item.get("persistent_save_kind")
    expected_kind = {"SINGLE_FILE": "CORE_SAVE", "DOS_OVERLAY": "DOS_OVERLAY", "NONE": None}
    if mode not in expected_kind or kind != expected_kind[mode]:
        raise CheckError("DEPENDENCY_PERSISTENT_SAVE_INVALID")
    if item.get("input_mode") not in ("STANDARD", "POINTER"):
        raise CheckError("DEPENDENCY_INPUT_MODE_INVALID")
    report_path = safe_relative_path(item.get("report_path"), "DEPENDENCY_CORE_REPORT_INVALID")
    if report_path not in allowlist_paths:
        raise CheckError("DEPENDENCY_CORE_REPORT_NOT_ALLOWLISTED")
    if not isinstance(item.get("source_component_id"), str):
        raise CheckError("DEPENDENCY_CORE_COMPONENT_INVALID")
    actions = item.get("startup_actions")
    if not isinstance(actions, list) or len(actions) > 4:
        raise CheckError("DEPENDENCY_STARTUP_ACTION_INVALID")
    for action in actions:
        if not isinstance(action, dict) or set(action) != {
            "event", "kind", "delayMs", "player", "control", "durationMs"
        } or action.get("event") != "GAME_START" or action.get("kind") != "PRESS_CONTROL":
            raise CheckError("DEPENDENCY_STARTUP_ACTION_INVALID")
        values = [action.get(name) for name in ("delayMs", "player", "control", "durationMs")]
        if any(not isinstance(value, int) or isinstance(value, bool) for value in values):
            raise CheckError("DEPENDENCY_STARTUP_ACTION_INVALID")
        if not 0 <= values[0] <= 10_000 or not 0 <= values[1] <= 3 or not 0 <= values[2] <= 255 or not 1 <= values[3] <= 1_000:
            raise CheckError("DEPENDENCY_STARTUP_ACTION_INVALID")


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
        if "dat" not in core:
            continue
        dat = core["dat"]
        check_file(dat_root / dat["local_path"], dat["size_bytes"], dat["sha256"])
    for component in manifest["license_materialization"]["components"]:
        for entry in component["license_files"]:
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
        if "dat" not in core:
            continue
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
    entries = [
        entry
        for component in manifest["license_materialization"]["components"]
        for entry in component["license_files"]
    ]
    for entry in entries:
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
    for component in manifest["license_materialization"]["components"]:
        for entry in component["license_files"]:
            header = (
            "===============================================================================\n"
            f"Component: {component['component_id']}\n"
            f"Source: {component['repository']}/tree/{component['source_commit']}\n"
            f"Binary association: {component['binary_association_status']}\n"
            f"Declared license path: {entry['declared_license_path']}\n"
            "-------------------------------------------------------------------------------\n"
            ).encode("ascii")
            license_bytes = runtime_path(version, entry["output_relative_path"]).read_bytes()
            chunks.append(header + license_bytes.rstrip(b"\n") + b"\n")
    return b"\n".join(chunks)


def publish_bytes_if_changed(target: Path, contents: bytes) -> bool:
    try:
        if target.read_bytes() == contents:
            if target.stat().st_mode & 0o777 != 0o600:
                os.chmod(target, 0o600)
            return False
    except FileNotFoundError:
        pass
    target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    descriptor, candidate_name = tempfile.mkstemp(prefix=f".{target.name}.", dir=target.parent)
    candidate = Path(candidate_name)
    try:
        with os.fdopen(descriptor, "wb") as output:
            output.write(contents)
            output.flush()
            os.fsync(output.fileno())
        os.chmod(candidate, 0o600)
        os.replace(candidate, target)
    finally:
        candidate.unlink(missing_ok=True)
    return True


def prepare(version: str, manifest: dict[str, Any]) -> None:
    ensure_release(version, manifest)
    ensure_override(version, manifest)
    ensure_dat(version, manifest)
    ensure_licenses(version, manifest)
    notice_path = runtime_path(
        version, manifest["license_materialization"]["third_party_notices_relative_path"]
    )
    notice = render_notice(manifest)
    publish_bytes_if_changed(notice_path, notice)


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
