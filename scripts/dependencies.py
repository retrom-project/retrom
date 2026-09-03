#!/usr/bin/env python3
"""Validate and materialize Retrom's Provider-neutral data dependencies."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import tempfile
import urllib.error
import urllib.request
from pathlib import Path, PurePosixPath
from typing import Any

import fbalpha2012_dat


REPOSITORY_ROOT = Path(__file__).resolve().parent.parent
DATA_ROOT = Path(os.environ.get("RETROM_DEPENDENCY_ROOT", REPOSITORY_ROOT / "data")).resolve()
AUTH_ROOT = DATA_ROOT / "auth/password-blocklists/v1"
AUTH_MANIFEST_PATH = AUTH_ROOT / "manifest.json"
NETPLAY_MANIFEST_PATH = DATA_ROOT / "netplay/v2/manifest.json"
NETPLAY_SCHEMA_PATH = DATA_ROOT / "netplay/v2/schema.json"
TARGET_CATALOG_ROOT = DATA_ROOT / "runtime-target-bindings/v1"
TARGET_CATALOG_PATH = TARGET_CATALOG_ROOT / "catalog.json"
TARGET_CATALOG_SCHEMA_PATH = TARGET_CATALOG_ROOT / "schema.json"
HEX_64 = re.compile(r"^[0-9a-f]{64}$")
COMMIT = re.compile(r"^[0-9a-f]{40}$")
PINNED_RAW = re.compile(
    r"^https://raw\.githubusercontent\.com/[^/]+/[^/]+/[0-9a-f]{40}/.+$"
)
SEMVER = re.compile(
    r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$"
)
EXPECTED_DAT_CORE_IDS = {
    "4.2.3": {
        "fbneo", "fbalpha2012_cps1", "fbalpha2012_cps2", "mame2003", "mame2003_plus",
    },
    "4.3.0-pre": set(),
}
PARSE_STAT_KEYS = {
    "machine_count", "rom_entry_count", "rom_entry_with_merge_count",
    "rom_entry_with_bios_count", "rom_nodump_count", "rom_baddump_count",
    "rom_missing_crc32_count", "rom_missing_sha1_count", "rom_missing_all_hash_count",
    "non_nodump_rom_missing_all_hash_count", "bios_set_count", "default_bios_set_count",
    "disk_entry_count", "disk_missing_sha1_count", "sample_entry_count",
    "cloneof_relation_count", "romof_relation_count", "explicit_bios_machine_count",
    "base_dependency_target_count", "unresolved_cloneof_target_count",
    "unresolved_romof_target_count",
}
PARSE_STAT_DETAIL_KEYS = {"unresolved_cloneof_targets", "unresolved_romof_targets"}


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
    expected_keys = {
        "schema_version", "purpose", "created_at", "reviewed_at", "repository_policy",
        "emulatorjs", "cores",
    }
    emulatorjs = manifest.get("emulatorjs")
    if set(manifest) != expected_keys or manifest.get("schema_version") != 8 or not isinstance(emulatorjs, dict):
        raise CheckError(f"DEPENDENCY_SCHEMA_UNSUPPORTED:{version}")
    if set(emulatorjs) != {"version", "tag", "tag_commit"} or emulatorjs.get("version") != version:
        raise CheckError(f"DEPENDENCY_VERSION_MISMATCH:{version}")
    if emulatorjs.get("tag") != f"v{version}" or not isinstance(emulatorjs.get("tag_commit"), str) or \
            COMMIT.fullmatch(emulatorjs["tag_commit"]) is None:
        raise CheckError(f"DEPENDENCY_RELEASE_IDENTITY_INVALID:{version}")
    validate_dat_manifest(version, manifest)
    return manifest


def validate_dat_manifest(version: str, manifest: dict[str, Any]) -> None:
    cores = manifest.get("cores")
    if not isinstance(cores, list):
        raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")
    sums = load_sha256s(DATA_ROOT / "dat" / "emulatorjs" / version / "SHA256SUMS")
    expected_sums: dict[str, str] = {}
    seen: set[str] = set()
    for core in cores:
        if not isinstance(core, dict) or set(core) != {"core_id", "core_source", "dat", "parse_stats"}:
            raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")
        core_id = core.get("core_id")
        if not isinstance(core_id, str) or not core_id or core_id in seen:
            raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")
        seen.add(core_id)
        source = core.get("core_source")
        if not isinstance(source, dict) or not isinstance(source.get("commit"), str) or \
                COMMIT.fullmatch(source["commit"]) is None:
            raise CheckError("DEPENDENCY_DAT_SOURCE_INVALID")
        dat = core.get("dat")
        if not isinstance(dat, dict) or not isinstance(dat.get("materialization"), dict):
            raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")
        relative = safe_relative_path(dat.get("local_path"), "DEPENDENCY_DAT_PATH_INVALID")
        if not isinstance(dat.get("size_bytes"), int) or dat["size_bytes"] < 1:
            raise CheckError("DEPENDENCY_DAT_SIZE_INVALID")
        expected_sums[relative] = expect_digest(dat.get("sha256"), "DEPENDENCY_DAT_SHA256_INVALID")
        validate_parse_stats(core.get("parse_stats"))
    if seen != EXPECTED_DAT_CORE_IDS.get(version) or sums != expected_sums:
        raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")


def validate_parse_stats(stats: object) -> None:
    if not isinstance(stats, dict) or not PARSE_STAT_KEYS.issubset(stats) or \
            not set(stats).issubset(PARSE_STAT_KEYS | PARSE_STAT_DETAIL_KEYS):
        raise CheckError("DEPENDENCY_DAT_STATS_INVALID")
    if any(not isinstance(stats[key], int) or isinstance(stats[key], bool) or stats[key] < 0 for key in PARSE_STAT_KEYS):
        raise CheckError("DEPENDENCY_DAT_STATS_INVALID")
    for key in set(stats) & PARSE_STAT_DETAIL_KEYS:
        if not isinstance(stats[key], list) or any(not isinstance(item, str) for item in stats[key]):
            raise CheckError("DEPENDENCY_DAT_STATS_INVALID")


def load_sha256s(path: Path) -> dict[str, str]:
    result: dict[str, str] = {}
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        raise CheckError("DEPENDENCY_SHA256SUMS_INVALID") from exc
    for line in lines:
        if not line:
            continue
        digest, separator, relative = line.partition("  ")
        if not separator or HEX_64.fullmatch(digest) is None:
            raise CheckError("DEPENDENCY_SHA256SUMS_INVALID")
        safe_relative_path(relative, "DEPENDENCY_SHA256SUMS_INVALID")
        if relative in result:
            raise CheckError("DEPENDENCY_SHA256SUMS_INVALID")
        result[relative] = digest
    return result


def load_auth_manifest() -> dict[str, Any]:
    manifest = load_json(AUTH_MANIFEST_PATH)
    if manifest.get("schema_version") != 1 or manifest.get("id") != "PASSWORD_BLOCKLIST_V1":
        raise CheckError("AUTH_BLOCKLIST_MANIFEST_INVALID")
    source = manifest.get("source")
    if not isinstance(source, dict) or not isinstance(source.get("commit"), str) or \
            COMMIT.fullmatch(source["commit"]) is None:
        raise CheckError("AUTH_BLOCKLIST_SOURCE_INVALID")
    for key in ("passwords", "license"):
        entry = manifest.get(key)
        if not isinstance(entry, dict) or not isinstance(entry.get("url"), str) or \
                PINNED_RAW.fullmatch(entry["url"]) is None or source["commit"] not in entry["url"]:
            raise CheckError("AUTH_BLOCKLIST_SOURCE_INVALID")
        safe_relative_path(entry.get("output_relative_path"), "AUTH_BLOCKLIST_PATH_INVALID")
        if not isinstance(entry.get("size_bytes"), int) or entry["size_bytes"] < 1:
            raise CheckError("AUTH_BLOCKLIST_SIZE_INVALID")
        expect_digest(entry.get("sha256"), "AUTH_BLOCKLIST_SHA256_INVALID")
    if manifest["passwords"].get("line_count") != 10_000 or manifest["license"].get("spdx") != "MIT":
        raise CheckError("AUTH_BLOCKLIST_MANIFEST_INVALID")
    return manifest


def validate_netplay_manifest() -> None:
    manifest = load_json(NETPLAY_MANIFEST_PATH)
    catalog = load_json(TARGET_CATALOG_PATH)
    if manifest.get("schemaVersion") != 5 or not NETPLAY_SCHEMA_PATH.is_file():
        raise CheckError("NETPLAY_MANIFEST_INVALID")
    expected_protocol = {
        "version": "retrom-netplay-v2", "controlCount": 24, "checkpointEveryFrames": 120,
        "maxPredictionFrames": 8, "maxRollbackFrames": 120,
        "canonicalHistoryFrames": 600, "maxStateBytes": 1_048_576,
        "allowedContentKinds": ["SINGLE_FILE"],
    }
    if manifest.get("protocol") != expected_protocol or catalog.get("schemaVersion") != 1:
        raise CheckError("NETPLAY_MANIFEST_INVALID")
    bindings = catalog.get("bindings")
    profiles = manifest.get("profiles")
    if not isinstance(bindings, list) or not isinstance(profiles, list) or len(profiles) != 8:
        raise CheckError("NETPLAY_MANIFEST_INVALID")
    seen: set[str] = set()
    for profile in profiles:
        if not valid_netplay_profile(profile, bindings) or profile["id"] in seen:
            raise CheckError("NETPLAY_MANIFEST_INVALID")
        seen.add(profile["id"])


def valid_netplay_profile(profile: object, bindings: list[object]) -> bool:
    fields = {
        "id", "providerId", "targetId", "coreId", "platformIds",
        "netplayCompatibilityLine", "maxPlayers", "maxPredictionFrames",
    }
    if not isinstance(profile, dict) or set(profile) != fields or profile.get("providerId") != "emulatorjs" or \
            profile.get("netplayCompatibilityLine") != "emulatorjs-netplay-v2" or \
            not isinstance(profile.get("platformIds"), list) or not 2 <= profile.get("maxPlayers", 0) <= 4 or \
            not 0 <= profile.get("maxPredictionFrames", -1) <= 8:
        return False
    return any(
        isinstance(binding, dict) and binding.get("providerId") == profile["providerId"] and
        binding.get("targetId") == profile["targetId"] and binding.get("coreId") == profile["coreId"] and
        "SINGLE_FILE" in binding.get("acceptedContentKinds", []) and
        all(platform in binding.get("platformIds", []) for platform in profile["platformIds"])
        for binding in bindings
    )


def check_dat_payload(version: str, manifest: dict[str, Any]) -> None:
    root = DATA_ROOT / "dat" / "emulatorjs" / version
    for core in manifest["cores"]:
        dat = core["dat"]
        check_file(root / dat["local_path"], dat["size_bytes"], dat["sha256"])


def check_auth_payload(manifest: dict[str, Any]) -> None:
    for key in ("passwords", "license"):
        entry = manifest[key]
        check_file(AUTH_ROOT / entry["output_relative_path"], entry["size_bytes"], entry["sha256"])


def download(url: str, target: Path, size: int, sha256: str) -> None:
    target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    request = urllib.request.Request(url, headers={"User-Agent": "retrom-dependency-preparer/2"})
    descriptor, temporary_name = tempfile.mkstemp(prefix=".retrom-download-", dir=target.parent)
    temporary = Path(temporary_name)
    try:
        digest = hashlib.sha256()
        written = 0
        with os.fdopen(descriptor, "wb") as output, urllib.request.urlopen(request, timeout=30) as response:
            if not response.geturl().startswith("https://"):
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
        os.chmod(temporary, 0o600)
        os.replace(temporary, target)
    finally:
        temporary.unlink(missing_ok=True)


def prepare_auth(manifest: dict[str, Any]) -> None:
    for key in ("passwords", "license"):
        entry = manifest[key]
        target = AUTH_ROOT / entry["output_relative_path"]
        try:
            check_file(target, entry["size_bytes"], entry["sha256"])
        except CheckError:
            download(entry["url"], target, entry["size_bytes"], entry["sha256"])
        os.chmod(target, 0o600)


def prepare_dat(version: str, manifest: dict[str, Any]) -> None:
    root = DATA_ROOT / "dat" / "emulatorjs" / version
    for core in manifest["cores"]:
        dat = core["dat"]
        target = root / dat["local_path"]
        try:
            check_file(target, dat["size_bytes"], dat["sha256"])
            os.chmod(target, 0o600)
            continue
        except CheckError:
            pass
        materialize_dat(root, core, target)
        check_file(target, dat["size_bytes"], dat["sha256"])
        os.chmod(target, 0o600)


def materialize_dat(root: Path, core: dict[str, Any], target: Path) -> None:
    dat = core["dat"]
    materialization = dat["materialization"]
    strategy = materialization.get("strategy")
    if strategy == "download_exact_file":
        download(materialization["url"], target, dat["size_bytes"], dat["sha256"])
        return
    temporary_root = Path(tempfile.mkdtemp(prefix="retrom-dat-", dir=root))
    try:
        if strategy == "build_fbalpha2012_native_dat":
            archive = temporary_root / "source.tar.gz"
            download(
                materialization["source_archive_url"], archive,
                materialization["source_archive_size_bytes"], materialization["source_archive_sha256"],
            )
            config = {
                "archive_root": materialization["source_archive_root"],
                "archive_size_bytes": materialization["source_archive_size_bytes"],
                "archive_sha256": materialization["source_archive_sha256"],
                "expected_machine_count": core["parse_stats"]["machine_count"],
                "expected_normalized_external_parents": materialization[
                    "expected_normalized_external_parents"
                ],
            }
            try:
                fbalpha2012_dat.materialize(archive, core["core_id"], target, config)
            except fbalpha2012_dat.GenerationError as exc:
                raise CheckError(str(exc)) from exc
            return
        if strategy != "download_pinned_snapshot_then_replace_exact_bytes":
            raise CheckError("DEPENDENCY_DAT_STRATEGY_UNSUPPORTED")
        base = temporary_root / "base.dat"
        download(materialization["base_url"], base, materialization["base_size_bytes"], materialization["base_sha256"])
        contents = base.read_bytes()
        for replacement in materialization["byte_replacements"]:
            old = replacement["from_utf8"].encode()
            new = replacement["to_utf8"].encode()
            if contents.count(old) != replacement["expected_count"]:
                raise CheckError("DEPENDENCY_DAT_REPLACEMENT_COUNT_MISMATCH")
            contents = contents.replace(old, new)
        target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
        target.write_bytes(contents)
    finally:
        shutil.rmtree(temporary_root, ignore_errors=True)


def image_export_entries(
    versions: list[str], manifests: list[dict[str, Any]], auth_manifest: dict[str, Any],
) -> dict[str, Path]:
    result: dict[str, Path] = {}

    def add(source: Path, relative: str) -> None:
        destination = safe_relative_path(relative, "DEPENDENCY_IMAGE_EXPORT_PATH_INVALID")
        if destination in result and result[destination] != source:
            raise CheckError("DEPENDENCY_IMAGE_EXPORT_PATH_COLLISION")
        result[destination] = source

    for version, manifest in zip(versions, manifests, strict=True):
        root = DATA_ROOT / "dat" / "emulatorjs" / version
        add(root / "manifest.json", f"dat/emulatorjs/{version}/manifest.json")
        add(root / "SHA256SUMS", f"dat/emulatorjs/{version}/SHA256SUMS")
        for core in manifest["cores"]:
            relative = safe_relative_path(core["dat"]["local_path"], "DEPENDENCY_DAT_PATH_INVALID")
            add(root / relative, f"dat/emulatorjs/{version}/{relative}")
    add(AUTH_MANIFEST_PATH, "auth/password-blocklists/v1/manifest.json")
    for key in ("passwords", "license"):
        relative = safe_relative_path(auth_manifest[key]["output_relative_path"], "AUTH_BLOCKLIST_PATH_INVALID")
        add(AUTH_ROOT / relative, f"auth/password-blocklists/v1/{relative}")
    add(NETPLAY_MANIFEST_PATH, "netplay/v2/manifest.json")
    add(NETPLAY_SCHEMA_PATH, "netplay/v2/schema.json")
    add(TARGET_CATALOG_PATH, "runtime-target-bindings/v1/catalog.json")
    add(TARGET_CATALOG_SCHEMA_PATH, "runtime-target-bindings/v1/schema.json")
    return result


def export_image_dependencies(
    output_root: Path,
    versions: list[str],
    manifests: list[dict[str, Any]],
    auth_manifest: dict[str, Any],
) -> None:
    if not output_root.is_absolute() or output_root.exists() or output_root.is_symlink():
        raise CheckError("DEPENDENCY_IMAGE_EXPORT_TARGET_INVALID")
    output_root.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=".retrom-image-dependencies-", dir=output_root.parent))
    try:
        for relative, source in sorted(image_export_entries(versions, manifests, auth_manifest).items()):
            info = source.lstat()
            if not stat.S_ISREG(info.st_mode):
                raise CheckError(f"DEPENDENCY_IMAGE_EXPORT_SOURCE_INVALID:{source.name}")
            destination = staging / relative
            destination.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
            shutil.copyfile(source, destination)
            os.chmod(destination, 0o444)
        for directory in sorted(
            (path for path in staging.rglob("*") if path.is_dir()),
            key=lambda path: len(path.parts), reverse=True,
        ):
            os.chmod(directory, 0o555)
        os.chmod(staging, 0o555)
        os.replace(staging, output_root)
    finally:
        if staging.exists():
            shutil.rmtree(staging, ignore_errors=True)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("data-check", "prepare", "deps-check", "image-export"))
    parser.add_argument("--versions", required=True)
    parser.add_argument("--output")
    args = parser.parse_args()
    try:
        if (args.action == "image-export") != (args.output is not None):
            raise CheckError("DEPENDENCY_IMAGE_EXPORT_OUTPUT_INVALID")
        versions = parse_versions(args.versions)
        manifests = [load_manifest(version) for version in versions]
        auth_manifest = load_auth_manifest()
        validate_netplay_manifest()
        if args.action == "prepare":
            for version, manifest in zip(versions, manifests, strict=True):
                prepare_dat(version, manifest)
            prepare_auth(auth_manifest)
        if args.action in {"prepare", "deps-check", "image-export"}:
            for version, manifest in zip(versions, manifests, strict=True):
                check_dat_payload(version, manifest)
            check_auth_payload(auth_manifest)
        if args.action == "image-export":
            export_image_dependencies(Path(args.output), versions, manifests, auth_manifest)
        print(f"{args.action}: ok ({','.join(versions)})")
        return 0
    except (
        CheckError, OSError, KeyError, TypeError, ValueError,
        urllib.error.URLError, subprocess.CalledProcessError,
    ) as exc:
        print(str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
