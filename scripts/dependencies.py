#!/usr/bin/env python3
"""Validate and materialize Retrom's version-pinned third-party dependencies."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request
from pathlib import Path, PurePosixPath
from typing import Any
from xml.etree import ElementTree

import fbalpha2012_dat


REPOSITORY_ROOT = Path(__file__).resolve().parent.parent
DATA_ROOT = REPOSITORY_ROOT / "data"
AUTH_MANIFEST_PATH = DATA_ROOT / "auth/password-blocklists/v1/manifest.json"
NETPLAY_MANIFEST_PATH = DATA_ROOT / "netplay/v2/manifest.json"
NETPLAY_SCHEMA_PATH = DATA_ROOT / "netplay/v2/schema.json"
RPG_MAKER_DAT_ROOT = DATA_ROOT / "dat/rpgmaker/v1"
RPG_MAKER_MANIFEST_PATH = RPG_MAKER_DAT_ROOT / "manifest.json"
RPG_MAKER_RUNTIME_ROOT = DATA_ROOT / "runtime/rpgmaker/v1"
RPG_MAKER_BUILD_PATH = RPG_MAKER_DAT_ROOT / "build.py"
RPG_MAKER_REPRODUCTION_PATH = RPG_MAKER_DAT_ROOT / "REPRODUCING.md"
RPG_MAKER_PLAYER_REGISTRY_PATH = (
    REPOSITORY_ROOT / "web/features/player/rpg-runtime/registry.json"
)
CPS_FIXTURE_LAYOUT_PATH = (
    REPOSITORY_ROOT / "testdata/public-roms/arcade-smoke/driver-layouts.json"
)
HEX_64 = re.compile(r"^[0-9a-f]{64}$")
PINNED_RAW = re.compile(
    r"^https://raw\.githubusercontent\.com/[^/]+/[^/]+/[0-9a-f]{40}/.+$"
)
RUNTIME_CORE_ID = re.compile(r"^[a-z0-9_]{1,64}$")
ARTIFACT_BASENAME = re.compile(r"^[A-Za-z0-9_.-]+-wasm\.data$")
DANGEROUS_OPTION_KEYS = {"__proto__", "constructor", "prototype"}
EXPECTED_DAT_CORE_IDS = {
    "4.2.3": {
        "fbneo",
        "fbalpha2012_cps1",
        "fbalpha2012_cps2",
        "mame2003",
        "mame2003_plus",
    },
    "4.3.0-pre": set(),
}
FBA2012_GENERATION_GATES = {
    "fbalpha2012_cps1": (227, []),
    "fbalpha2012_cps2": (284, ["mmancp2u->megaman"]),
}
PARSE_STAT_KEYS = {
    "machine_count",
    "rom_entry_count",
    "rom_entry_with_merge_count",
    "rom_entry_with_bios_count",
    "rom_nodump_count",
    "rom_baddump_count",
    "rom_missing_crc32_count",
    "rom_missing_sha1_count",
    "rom_missing_all_hash_count",
    "non_nodump_rom_missing_all_hash_count",
    "bios_set_count",
    "default_bios_set_count",
    "disk_entry_count",
    "disk_missing_sha1_count",
    "sample_entry_count",
    "cloneof_relation_count",
    "romof_relation_count",
    "explicit_bios_machine_count",
    "base_dependency_target_count",
    "unresolved_cloneof_target_count",
    "unresolved_romof_target_count",
}
PARSE_STAT_DETAIL_KEYS = {"unresolved_cloneof_targets", "unresolved_romof_targets"}
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
    if manifest.get("schema_version") != 7:
        raise CheckError(f"DEPENDENCY_SCHEMA_UNSUPPORTED:{version}")
    emulatorjs = manifest.get("emulatorjs")
    if not isinstance(emulatorjs, dict) or emulatorjs.get("version") != version:
        raise CheckError(f"DEPENDENCY_VERSION_MISMATCH:{version}")
    return manifest


def load_auth_manifest() -> dict[str, Any]:
    manifest = load_json(AUTH_MANIFEST_PATH)
    if manifest.get("schema_version") != 1 or manifest.get("id") != "PASSWORD_BLOCKLIST_V1":
        raise CheckError("AUTH_BLOCKLIST_MANIFEST_INVALID")
    source = manifest.get("source")
    passwords = manifest.get("passwords")
    license_entry = manifest.get("license")
    if not isinstance(source, dict) or not isinstance(passwords, dict) or not isinstance(license_entry, dict):
        raise CheckError("AUTH_BLOCKLIST_MANIFEST_INVALID")
    commit = source.get("commit")
    if not isinstance(commit, str) or re.fullmatch(r"[0-9a-f]{40}", commit) is None:
        raise CheckError("AUTH_BLOCKLIST_SOURCE_INVALID")
    for entry in (passwords, license_entry):
        url = entry.get("url")
        if not isinstance(url, str) or PINNED_RAW.fullmatch(url) is None or commit not in url:
            raise CheckError("AUTH_BLOCKLIST_SOURCE_INVALID")
        safe_relative_path(entry.get("output_relative_path"), "AUTH_BLOCKLIST_PATH_INVALID")
        if not isinstance(entry.get("size_bytes"), int) or entry["size_bytes"] < 1:
            raise CheckError("AUTH_BLOCKLIST_SIZE_INVALID")
        expect_digest(entry.get("sha256"), "AUTH_BLOCKLIST_SHA256_INVALID")
    if passwords.get("line_count") != 10_000 or license_entry.get("spdx") != "MIT":
        raise CheckError("AUTH_BLOCKLIST_MANIFEST_INVALID")
    return manifest


def run_rpg_maker_dependency_action(action: str) -> None:
    result = subprocess.run(
        [sys.executable, str(RPG_MAKER_BUILD_PATH), action],
        cwd=REPOSITORY_ROOT,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        detail = result.stderr.strip().splitlines()
        raise CheckError(detail[-1] if detail else "RPG_RUNTIME_DEPENDENCY_ACTION_FAILED")


def load_rpg_maker_manifest() -> dict[str, Any]:
    return load_json(RPG_MAKER_MANIFEST_PATH)


def validate_rpg_maker_registry(
    manifest: dict[str, Any], registry: dict[str, Any]
) -> None:
    routes = registry.get("routes")
    if (
        registry.get("schemaVersion") != 1
        or set(registry) != {"schemaVersion", "routes"}
        or not isinstance(routes, list)
    ):
        raise CheckError("RPG_PLAYER_REGISTRY_INVALID")

    registered: set[tuple[str, str, str, str, str]] = set()
    implementations: dict[tuple[str, str], str] = {}
    for raw in routes:
        if not isinstance(raw, dict) or set(raw) != {
            "coreId",
            "generation",
            "routeKey",
            "adapterKind",
            "adapterId",
            "implementation",
        }:
            raise CheckError("RPG_PLAYER_REGISTRY_INVALID")
        values = tuple(
            raw.get(key)
            for key in ("coreId", "generation", "routeKey", "adapterKind", "adapterId")
        )
        if any(not isinstance(value, str) or not value for value in values):
            raise CheckError("RPG_PLAYER_REGISTRY_INVALID")
        if values in registered:
            raise CheckError("RPG_PLAYER_REGISTRY_DUPLICATE")
        registered.add(values)

        implementation = safe_relative_path(
            raw.get("implementation"), "RPG_PLAYER_IMPLEMENTATION_INVALID"
        )
        implementation_path = (
            RPG_MAKER_PLAYER_REGISTRY_PATH.parent / implementation
        )
        if not implementation_path.is_file():
            raise CheckError(f"RPG_PLAYER_IMPLEMENTATION_MISSING:{values[0]}")
        adapter = (values[3], values[4])
        previous = implementations.setdefault(adapter, implementation)
        if previous != implementation:
            raise CheckError("RPG_PLAYER_IMPLEMENTATION_DRIFT")

    artifacts = manifest.get("artifacts")
    if not isinstance(artifacts, list):
        raise CheckError("RPG_PLAYER_MANIFEST_INVALID")
    expected: set[tuple[str, str, str, str, str]] = set()
    for artifact in artifacts:
        if not isinstance(artifact, dict):
            raise CheckError("RPG_PLAYER_MANIFEST_INVALID")
        values = tuple(
            artifact.get(key)
            for key in (
                "core_id",
                "generation",
                "route_key",
                "runtime_adapter_kind",
                "adapter_id",
            )
        )
        if any(not isinstance(value, str) or not value for value in values):
            raise CheckError("RPG_PLAYER_MANIFEST_INVALID")
        if values in expected:
            raise CheckError("RPG_PLAYER_MANIFEST_DUPLICATE")
        expected.add(values)
    if registered != expected:
        raise CheckError("RPG_PLAYER_REGISTRY_DRIFT")


def auth_payload_path(relative: str) -> Path:
    return AUTH_MANIFEST_PATH.parent / relative


def prepare_auth(manifest: dict[str, Any]) -> None:
    for key in ("passwords", "license"):
        entry = manifest[key]
        target = auth_payload_path(entry["output_relative_path"])
        try:
            check_file(target, entry["size_bytes"], entry["sha256"])
        except CheckError:
            download(entry["url"], target, entry["size_bytes"], entry["sha256"])


def check_auth_payload(manifest: dict[str, Any]) -> None:
    for key in ("passwords", "license"):
        entry = manifest[key]
        check_file(auth_payload_path(entry["output_relative_path"]), entry["size_bytes"], entry["sha256"])
    contents = auth_payload_path(manifest["passwords"]["output_relative_path"]).read_bytes()
    if len(contents.splitlines()) != manifest["passwords"]["line_count"]:
        raise CheckError("AUTH_BLOCKLIST_LINE_COUNT_MISMATCH")


def validate_registry(manifests: list[dict[str, Any]]) -> None:
    registry_path = REPOSITORY_ROOT / "web/features/player/adapters/registry.json"
    registry = load_json(registry_path)
    entries = registry.get("adapters")
    if registry.get("schemaVersion") != 2 or set(registry) != {
        "schemaVersion", "adapters", "netplayAdapters"
    } or not isinstance(entries, list):
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
    netplay_entries = registry.get("netplayAdapters")
    if not isinstance(netplay_entries, list):
        raise CheckError("NETPLAY_ADAPTER_REGISTRY_INVALID")
    netplay_registered: dict[str, str] = {}
    for raw in netplay_entries:
        if not isinstance(raw, dict) or set(raw) != {"id", "version", "module"}:
            raise CheckError("NETPLAY_ADAPTER_REGISTRY_INVALID")
        adapter_id = raw.get("id")
        version = raw.get("version")
        module = safe_relative_path(raw.get("module"), "NETPLAY_ADAPTER_MODULE_INVALID")
        if not isinstance(adapter_id, str) or not isinstance(version, str) or adapter_id in netplay_registered:
            raise CheckError("NETPLAY_ADAPTER_REGISTRY_INVALID")
        module_path = REPOSITORY_ROOT / "web/features/player/netplay" / module
        if not module_path.is_file():
            raise CheckError(f"NETPLAY_ADAPTER_IMPLEMENTATION_MISSING:{adapter_id}")
        netplay_registered[adapter_id] = version
    validate_netplay_manifest(manifests, registered, netplay_registered)
    protocol = load_json(NETPLAY_MANIFEST_PATH)["protocol"]
    expected[protocol["playerAdapterId"]] = "4.2.3"
    if registered != expected:
        raise CheckError("PLAYER_ADAPTER_REGISTRY_DRIFT")


def validate_cps_fixture_layouts(manifests: list[dict[str, Any]]) -> None:
    layout = load_json(CPS_FIXTURE_LAYOUT_PATH)
    drivers = layout.get("drivers")
    if layout.get("schemaVersion") != 1 or not isinstance(drivers, dict):
        raise CheckError("CPS_FIXTURE_LAYOUT_INVALID")
    runtime_manifest = next(
        (item for item in manifests if item.get("emulatorjs", {}).get("version") == "4.2.3"),
        None,
    )
    if runtime_manifest is None:
        raise CheckError("CPS_FIXTURE_LAYOUT_INVALID")
    cores = {
        item.get("core_id"): item
        for item in runtime_manifest.get("cores", [])
        if isinstance(item, dict)
    }
    expected = {
        "1941": "fbalpha2012_cps1",
        "spf2xjd": "fbalpha2012_cps2",
    }
    if set(drivers) != set(expected):
        raise CheckError("CPS_FIXTURE_LAYOUT_INVALID")
    for driver_name, core_id in expected.items():
        driver = drivers[driver_name]
        core = cores.get(core_id)
        if not isinstance(driver, dict) or not isinstance(core, dict):
            raise CheckError("CPS_FIXTURE_LAYOUT_INVALID")
        dat = core.get("dat")
        stats = core.get("parse_stats")
        local_path = dat.get("local_path") if isinstance(dat, dict) else None
        if (
            driver.get("coreId") != core_id
            or driver.get("sourceCommit") != core.get("core_source", {}).get("commit")
            or driver.get("productionDatSha256") != (dat or {}).get("sha256")
            or driver.get("productionMachineCount") != (stats or {}).get("machine_count")
            or not isinstance(local_path, str)
        ):
            raise CheckError("CPS_FIXTURE_LAYOUT_INVALID")
        production_path = DATA_ROOT / "dat/emulatorjs/4.2.3" / local_path
        _, digest = file_digest(production_path)
        if digest != driver["productionDatSha256"]:
            raise CheckError("CPS_FIXTURE_LAYOUT_INVALID")
        root = ElementTree.parse(production_path).getroot()
        machine = next(
            (item for item in root.findall("machine") if item.attrib.get("name") == driver_name),
            None,
        )
        entries = driver.get("entries")
        if machine is None or not isinstance(entries, list):
            raise CheckError("CPS_FIXTURE_LAYOUT_INVALID")
        declared = [
            (entry.get("name"), str(entry.get("size")), entry.get("crc32"))
            for entry in entries
            if isinstance(entry, dict)
        ]
        actual = [
            (entry.attrib.get("name"), entry.attrib.get("size"), entry.attrib.get("crc"))
            for entry in machine.findall("rom")
        ]
        if len(declared) != len(entries) or declared != actual:
            raise CheckError("CPS_FIXTURE_LAYOUT_INVALID")


def validate_netplay_manifest(
    manifests: list[dict[str, Any]], player_adapters: dict[str, str], netplay_adapters: dict[str, str]
) -> None:
    schema = load_json(NETPLAY_SCHEMA_PATH)
    if schema.get("$schema") != "https://json-schema.org/draft/2020-12/schema":
        raise CheckError("NETPLAY_SCHEMA_INVALID")
    manifest = load_json(NETPLAY_MANIFEST_PATH)
    if set(manifest) != {"schemaVersion", "protocol", "profiles"} or manifest.get("schemaVersion") != 4:
        raise CheckError("NETPLAY_MANIFEST_INVALID")
    protocol = manifest.get("protocol")
    expected_protocol = {
        "version": "retrom-netplay-v2",
        "playerAdapterId": "ejs-4.2.3-v2",
        "netplayAdapterId": "ejs-netplay-4.2.3-v1",
        "controlCount": 24,
        "checkpointEveryFrames": 120,
        "maxPredictionFrames": 8,
        "maxRollbackFrames": 120,
        "canonicalHistoryFrames": 600,
        "maxStateBytes": 1_048_576,
        "allowedContentKinds": ["SINGLE_FILE"],
    }
    if protocol != expected_protocol:
        raise CheckError("NETPLAY_PROTOCOL_MANIFEST_INVALID")
    if netplay_adapters != {protocol["netplayAdapterId"]: "4.2.3"}:
        raise CheckError("NETPLAY_ADAPTER_REGISTRY_DRIFT")

    versions = {manifest["emulatorjs"]["version"]: manifest for manifest in manifests}
    runtime_manifest = versions.get("4.2.3")
    if (
        runtime_manifest is None
        or player_adapters.get(protocol["playerAdapterId"]) != "4.2.3"
        or runtime_manifest["emulatorjs"]["player_adapter"]["id"]
        == protocol["playerAdapterId"]
    ):
        raise CheckError("NETPLAY_PLAYER_ADAPTER_DRIFT")
    artifacts = {
        item.get("core_id"): item
        for item in runtime_manifest["emulatorjs"].get("selected_core_artifacts", [])
        if isinstance(item, dict)
    }
    expected_profiles = {
        "fceumm-423-v1": (
            "fceumm", ["nes"], "8c449fd5c36646fb0769423ed6ffa9efbdfc21fbfdc9bac7952b559d34d5b493", 8,
        ),
        "fbneo-423-v1": (
            "fbneo", ["arcade"], "315a25e0bcd61d58ee0d9e8b1dbf3740b9e0ca4b7d0726f848ce1068de73437c", 0,
        ),
        "snes9x-423-v1": (
            "snes9x", ["snes"], "eaa0bcfce67673809886e50387a80a616b719502175db64c090d04c9d75958ee", 0,
        ),
        "mame2003-423-override-v1": (
            "mame2003", ["arcade"], "1d8283ce042f71607b9b55656cd4068f703c52faa7a3d0940855c9dd21d542df", 0,
        ),
        "mame2003-plus-423-v1": (
            "mame2003_plus", ["arcade"], "cb6d9c80a88b65d1579d16d02128a678f8d1cd3f51de1479e647cea27b13247b", 0,
        ),
        "fbalpha2012-cps1-423-v1": (
            "fbalpha2012_cps1", ["arcade"], "15b47667eb3c3746649c79e997b9f8c463f83bed9f61f51322cbe4db3d6e078e", 0,
        ),
        "fbalpha2012-cps2-423-v1": (
            "fbalpha2012_cps2", ["arcade"], "432c2dd513603b04ccbf4e81f282f012763d2435311805443e2bd0cc9021d8d1", 0,
        ),
        "nestopia-423-v1": (
            "nestopia", ["nes"], "051de1b67a5b582b8a1bac6b99471d4f9f883ce3b3603d00330c1a066e546375", 0,
        ),
    }
    profiles = manifest.get("profiles")
    if not isinstance(profiles, list) or len(profiles) != len(expected_profiles):
        raise CheckError("NETPLAY_PROFILE_MANIFEST_INVALID")
    seen: set[str] = set()
    required_keys = {
        "id", "emulatorjsVersion", "coreId", "platformIds", "coreArtifactSha256", "maxPlayers",
        "maxPredictionFrames",
    }
    for profile in profiles:
        if not isinstance(profile, dict) or set(profile) != required_keys:
            raise CheckError("NETPLAY_PROFILE_MANIFEST_INVALID")
        profile_id = profile.get("id")
        expected = expected_profiles.get(profile_id)
        if expected is None or profile_id in seen:
            raise CheckError("NETPLAY_PROFILE_MANIFEST_INVALID")
        seen.add(profile_id)
        actual = (
            profile.get("coreId"), profile.get("platformIds"),
            profile.get("coreArtifactSha256"), profile.get("maxPredictionFrames"),
        )
        artifact = artifacts.get(profile.get("coreId"))
        if actual != expected or profile.get("emulatorjsVersion") != "4.2.3" or \
                profile.get("maxPlayers") != 2 or \
                artifact is None or artifact.get("sha256") != profile.get("coreArtifactSha256") or \
                "SINGLE_FILE" not in artifact.get("supported_content_kinds", []):
            raise CheckError("NETPLAY_PROFILE_MANIFEST_INVALID")


def artifact_set_sha256(runtime_allowlist: list[dict[str, Any]], artifact: dict[str, Any]) -> str:
    entries = {
        entry["path_in_release"]: {
            "path": entry["path_in_release"],
            "sha256": entry["sha256"],
            "sizeBytes": entry["size_bytes"],
        }
        for entry in runtime_allowlist
    }
    entry_path = artifact.get("path_in_release") or artifact.get("local_path")
    entries[entry_path] = {
        "path": entry_path,
        "sha256": artifact["sha256"],
        "sizeBytes": artifact["size_bytes"],
    }
    canonical = json.dumps(
        [entries[path] for path in sorted(entries, key=lambda value: value.encode("utf-8"))],
        ensure_ascii=False,
        separators=(",", ":"),
        sort_keys=True,
    ).encode("utf-8")
    return hashlib.sha256(canonical).hexdigest()


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
        if item.get("artifact_set_sha256") != artifact_set_sha256(allowlist, item):
            raise CheckError("DEPENDENCY_ARTIFACT_SET_SHA256_INVALID")
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
    dat_core_ids: set[str] = set()
    for core in cores:
        if not isinstance(core, dict):
            raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")
        if "dat" not in core:
            continue
        if not isinstance(core.get("dat"), dict):
            raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")
        dat = core["dat"]
        core_id = core.get("core_id")
        if not isinstance(core_id, str) or core_id in dat_core_ids:
            raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")
        dat_core_ids.add(core_id)
        local_path = safe_relative_path(dat.get("local_path"), "DEPENDENCY_DAT_PATH_INVALID")
        expected_sums[local_path] = expect_digest(dat.get("sha256"), "DEPENDENCY_DAT_SHA256_INVALID")
        validate_dat_definition(core_id, core, dat)
    if sums != expected_sums:
        raise CheckError("DEPENDENCY_SHA256SUMS_DRIFT")
    if dat_core_ids != EXPECTED_DAT_CORE_IDS.get(version):
        raise CheckError("DEPENDENCY_DAT_MANIFEST_INVALID")


def validate_dat_definition(core_id: str, core: dict[str, Any], dat: dict[str, Any]) -> None:
    stats = core.get("parse_stats")
    if (
        not isinstance(stats, dict)
        or not PARSE_STAT_KEYS.issubset(stats)
        or not set(stats).issubset(PARSE_STAT_KEYS | PARSE_STAT_DETAIL_KEYS)
        or any(
            not isinstance(stats[key], int) or isinstance(stats[key], bool) or stats[key] < 0
            for key in PARSE_STAT_KEYS
        )
        or any(
            not isinstance(stats[key], list) or any(not isinstance(item, str) for item in stats[key])
            for key in set(stats) & PARSE_STAT_DETAIL_KEYS
        )
    ):
        raise CheckError("DEPENDENCY_DAT_STATS_INVALID")
    materialization = dat.get("materialization")
    if not isinstance(materialization, dict):
        raise CheckError("DEPENDENCY_DAT_MATERIALIZATION_INVALID")
    if core_id not in FBA2012_GENERATION_GATES:
        return
    expected_machine_count, expected_parents = FBA2012_GENERATION_GATES[core_id]
    expected_keys = {
        "strategy",
        "source_archive_url",
        "source_archive_root",
        "source_archive_size_bytes",
        "source_archive_sha256",
        "expected_normalized_external_parent_count",
        "expected_normalized_external_parents",
    }
    source_url = materialization.get("source_archive_url")
    source_root = materialization.get("source_archive_root")
    commit = core.get("core_source", {}).get("commit")
    if (
        set(materialization) != expected_keys
        or materialization.get("strategy") != "build_fbalpha2012_native_dat"
        or not isinstance(source_url, str)
        or source_url != f"https://codeload.github.com/EmulatorJS/{core_id}/tar.gz/{commit}"
        or source_root != f"{core_id}-{commit}"
        or not isinstance(materialization.get("source_archive_size_bytes"), int)
        or materialization["source_archive_size_bytes"] <= 0
        or not HEX_64.fullmatch(str(materialization.get("source_archive_sha256", "")))
        or stats["machine_count"] != expected_machine_count
        or materialization.get("expected_normalized_external_parent_count") != len(expected_parents)
        or materialization.get("expected_normalized_external_parents") != expected_parents
        or stats["explicit_bios_machine_count"] != 0
        or stats["base_dependency_target_count"] != 0
        or stats["unresolved_cloneof_target_count"] != 0
        or stats["unresolved_romof_target_count"] != 0
    ):
        raise CheckError("DEPENDENCY_FBA2012_DAT_GATE_INVALID")


def validate_artifact_capability(item: dict[str, Any], allowlist_paths: set[str]) -> None:
    required_fields = {
        "core_id", "source_component_id", "runtime_core_id", "path_in_release", "size_bytes", "sha256",
        "artifact_set_sha256", "adapter_abi", "requires_threads", "bundle_version", "artifact_flavor",
        "requested_artifact_basename",
        "canvas_resize_policy", "default_options", "input_mode", "startup_actions", "report_path",
        "supported_content_kinds",
    }
    optional_fields = {"local_path", "override_ref", "multi_disc"}
    if not required_fields.issubset(item) or not set(item).issubset(required_fields | optional_fields):
        raise CheckError("DEPENDENCY_ARTIFACT_CAPABILITY_INVALID")
    if item.get("adapter_abi") != "emulatorjs-state-v1":
        raise CheckError("DEPENDENCY_ARTIFACT_ADAPTER_ABI_INVALID")
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
        if not 0 <= values[0] <= 30_000 or not 0 <= values[1] <= 3 or not 0 <= values[2] <= 255 or not 1 <= values[3] <= 1_000:
            raise CheckError("DEPENDENCY_STARTUP_ACTION_INVALID")
    content_kinds = item.get("supported_content_kinds")
    multi_disc = item.get("multi_disc")
    expected_primary = "DOS_BUNDLE" if item.get("core_id") == "dosbox_pure" else "SINGLE_FILE"
    if content_kinds not in ([expected_primary], [expected_primary, "MULTI_DISC_M3U_V1"]):
        raise CheckError("DEPENDENCY_CONTENT_CAPABILITY_INVALID")
    if multi_disc is None:
        if len(content_kinds) != 1:
            raise CheckError("DEPENDENCY_MULTI_DISC_CAPABILITY_INVALID")
        return
    if (
        item.get("core_id") != "yabause"
        or content_kinds != ["SINGLE_FILE", "MULTI_DISC_M3U_V1"]
        or not isinstance(multi_disc, dict)
        or set(multi_disc) != {"max_discs", "max_total_bytes", "delivery"}
        or multi_disc.get("max_discs") != 8
        or multi_disc.get("max_total_bytes") != 1_073_741_824
        or multi_disc.get("delivery") != "EAGER_EXTERNAL_FILES"
    ):
        raise CheckError("DEPENDENCY_MULTI_DISC_CAPABILITY_INVALID")


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


def image_export_entries(
    versions: list[str], manifests: list[dict[str, Any]], auth_manifest: dict[str, Any],
    rpg_maker_manifest: dict[str, Any],
) -> dict[str, Path]:
    entries: dict[str, Path] = {}

    def add(source: Path, destination: str) -> None:
        relative = safe_relative_path(destination, "DEPENDENCY_IMAGE_EXPORT_PATH_INVALID")
        previous = entries.get(relative)
        if previous is not None and previous != source:
            raise CheckError("DEPENDENCY_IMAGE_EXPORT_PATH_COLLISION")
        entries[relative] = source

    for version, manifest in zip(versions, manifests, strict=True):
        dat_root = DATA_ROOT / "dat/emulatorjs" / version
        add(dat_root / "manifest.json", f"dat/emulatorjs/{version}/manifest.json")
        add(dat_root / "SHA256SUMS", f"dat/emulatorjs/{version}/SHA256SUMS")
        for core in manifest["cores"]:
            dat = core.get("dat")
            if isinstance(dat, dict):
                relative = safe_relative_path(
                    dat.get("local_path"), "DEPENDENCY_IMAGE_EXPORT_PATH_INVALID"
                )
                add(dat_root / relative, f"dat/emulatorjs/{version}/{relative}")

        runtime_relatives = {
            safe_relative_path(
                item.get("path_in_release"), "DEPENDENCY_IMAGE_EXPORT_PATH_INVALID"
            )
            for item in manifest["emulatorjs"]["runtime_allowlist"]
        }
        runtime_relatives.update(
            safe_relative_path(
                item.get("path_in_release") or item.get("local_path"),
                "DEPENDENCY_IMAGE_EXPORT_PATH_INVALID",
            )
            for item in manifest["emulatorjs"]["selected_core_artifacts"]
        )
        runtime_relatives.update(
            safe_relative_path(
                entry.get("output_relative_path"),
                "DEPENDENCY_IMAGE_EXPORT_PATH_INVALID",
            )
            for component in manifest["license_materialization"]["components"]
            for entry in component["license_files"]
        )
        runtime_relatives.add(
            safe_relative_path(
                manifest["license_materialization"].get(
                    "third_party_notices_relative_path"
                ),
                "DEPENDENCY_IMAGE_EXPORT_PATH_INVALID",
            )
        )
        for relative in sorted(runtime_relatives):
            add(
                runtime_path(version, relative),
                f"runtime/emulatorjs/{version}/{relative}",
            )

    add(AUTH_MANIFEST_PATH, "auth/password-blocklists/v1/manifest.json")
    for key in ("passwords", "license"):
        relative = safe_relative_path(
            auth_manifest[key].get("output_relative_path"),
            "DEPENDENCY_IMAGE_EXPORT_PATH_INVALID",
        )
        add(auth_payload_path(relative), f"auth/password-blocklists/v1/{relative}")
    add(NETPLAY_MANIFEST_PATH, "netplay/v2/manifest.json")
    add(NETPLAY_SCHEMA_PATH, "netplay/v2/schema.json")
    add(RPG_MAKER_MANIFEST_PATH, "dat/rpgmaker/v1/manifest.json")
    add(RPG_MAKER_REPRODUCTION_PATH, "dat/rpgmaker/v1/REPRODUCING.md")
    build = rpg_maker_manifest["build"]
    for key in (
        "recipe_path", "easyrpg_patch_path", "mkxp_bridge_path", "native_bridge_v3_path",
    ):
        relative = safe_relative_path(build[key], "RPG_RUNTIME_IMAGE_EXPORT_PATH_INVALID")
        add(RPG_MAKER_DAT_ROOT / relative, f"dat/rpgmaker/v1/{relative}")
    for item in rpg_maker_manifest["runtime_files"]:
        relative = safe_relative_path(item["path_in_release"], "RPG_RUNTIME_IMAGE_EXPORT_PATH_INVALID")
        add(RPG_MAKER_RUNTIME_ROOT / relative, f"runtime/rpgmaker/v1/{relative}")
    add(
        RPG_MAKER_RUNTIME_ROOT / ".release-assets-observed.json",
        "runtime/rpgmaker/v1/.release-assets-observed.json",
    )
    for directory_name in ("corresponding-source", "licenses"):
        directory = RPG_MAKER_RUNTIME_ROOT / directory_name
        if directory.is_symlink() or not directory.is_dir():
            raise CheckError(f"RPG_RUNTIME_IMAGE_EXPORT_SOURCE_INVALID:{directory_name}")
        for source in sorted(directory.rglob("*")):
            if source.is_symlink():
                raise CheckError(f"RPG_RUNTIME_IMAGE_EXPORT_SOURCE_INVALID:{source.name}")
            if source.is_dir():
                continue
            if not source.is_file():
                raise CheckError(f"RPG_RUNTIME_IMAGE_EXPORT_SOURCE_INVALID:{source.name}")
            relative = safe_relative_path(
                source.relative_to(directory).as_posix(),
                "RPG_RUNTIME_IMAGE_EXPORT_PATH_INVALID",
            )
            add(
                source,
                f"runtime/rpgmaker/v1/{directory_name}/{relative}",
            )
    add(
        RPG_MAKER_RUNTIME_ROOT / "THIRD_PARTY_NOTICES",
        "runtime/rpgmaker/v1/THIRD_PARTY_NOTICES",
    )
    return entries


def export_image_dependencies(
    output_root: Path,
    versions: list[str],
    manifests: list[dict[str, Any]],
    auth_manifest: dict[str, Any],
    rpg_maker_manifest: dict[str, Any],
) -> None:
    if not output_root.is_absolute():
        raise CheckError("DEPENDENCY_IMAGE_EXPORT_TARGET_INVALID")
    if output_root.exists() or output_root.is_symlink():
        raise CheckError("DEPENDENCY_IMAGE_EXPORT_TARGET_EXISTS")
    output_root.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
    staging = Path(
        tempfile.mkdtemp(prefix=".retrom-image-dependencies-", dir=output_root.parent)
    )
    try:
        entries = image_export_entries(versions, manifests, auth_manifest, rpg_maker_manifest)
        for relative, source in sorted(entries.items()):
            try:
                source_stat = source.lstat()
            except OSError as exc:
                raise CheckError(f"DEPENDENCY_IMAGE_EXPORT_SOURCE_INVALID:{source.name}") from exc
            if not stat.S_ISREG(source_stat.st_mode):
                raise CheckError(f"DEPENDENCY_IMAGE_EXPORT_SOURCE_INVALID:{source.name}")
            destination = staging / relative
            destination.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
            with source.open("rb") as input_file, destination.open("xb") as output_file:
                shutil.copyfileobj(input_file, output_file, length=1024 * 1024)
            os.chmod(destination, 0o444)
        directories = sorted(
            (path for path in staging.rglob("*") if path.is_dir()),
            key=lambda path: len(path.parts),
            reverse=True,
        )
        for directory in directories:
            os.chmod(directory, 0o555)
        os.chmod(staging, 0o555)
        os.replace(staging, output_root)
    finally:
        if staging.exists():
            shutil.rmtree(staging, ignore_errors=True)


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
        if materialization["strategy"] == "build_fbalpha2012_native_dat":
            temp_dir = Path(tempfile.mkdtemp(prefix="retrom-fbalpha2012-dat-", dir=dat_root))
            try:
                source_archive = temp_dir / "source.tar.gz"
                download(
                    materialization["source_archive_url"],
                    source_archive,
                    materialization["source_archive_size_bytes"],
                    materialization["source_archive_sha256"],
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
                    report = fbalpha2012_dat.materialize(source_archive, core["core_id"], target, config)
                except fbalpha2012_dat.GenerationError as error:
                    raise CheckError(str(error)) from error
                if report["sizeBytes"] != dat["size_bytes"] or report["sha256"] != dat["sha256"]:
                    raise CheckError("DEPENDENCY_FBA2012_DAT_OUTPUT_MISMATCH")
                check_file(target, dat["size_bytes"], dat["sha256"])
            finally:
                shutil.rmtree(temp_dir, ignore_errors=True)
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
    parser.add_argument(
        "action", choices=("data-check", "prepare", "deps-check", "image-export")
    )
    parser.add_argument("--versions", required=True)
    parser.add_argument("--output")
    args = parser.parse_args()
    try:
        if (args.action == "image-export") != (args.output is not None):
            raise CheckError("DEPENDENCY_IMAGE_EXPORT_OUTPUT_INVALID")
        versions = parse_versions(args.versions)
        manifests = [load_manifest(version) for version in versions]
        auth_manifest = load_auth_manifest()
        rpg_maker_manifest = load_rpg_maker_manifest()
        for version, manifest in zip(versions, manifests, strict=True):
            validate_small_manifest(version, manifest)
        validate_registry(manifests)
        validate_rpg_maker_registry(
            rpg_maker_manifest, load_json(RPG_MAKER_PLAYER_REGISTRY_PATH)
        )
        run_rpg_maker_dependency_action("data-check")
        if args.action == "data-check":
            validate_cps_fixture_layouts(manifests)
        if args.action == "prepare":
            for version, manifest in zip(versions, manifests, strict=True):
                prepare(version, manifest)
            prepare_auth(auth_manifest)
            run_rpg_maker_dependency_action("prepare")
        if args.action in ("prepare", "deps-check", "image-export"):
            for version, manifest in zip(versions, manifests, strict=True):
                check_payload(version, manifest)
            check_auth_payload(auth_manifest)
            run_rpg_maker_dependency_action("deps-check")
        if args.action == "image-export":
            export_image_dependencies(
                Path(args.output), versions, manifests, auth_manifest, rpg_maker_manifest
            )
        print(f"{args.action}: ok ({','.join(versions)})")
        return 0
    except (CheckError, OSError, KeyError, TypeError, ValueError, urllib.error.URLError) as exc:
        print(str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
