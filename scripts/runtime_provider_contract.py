#!/usr/bin/env python3
from __future__ import annotations

import json
import hashlib
import os
import re
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import NoReturn


class ContractError(ValueError):
    pass


_IDENTITY = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$")
_TOKEN = re.compile(r"^[a-z0-9](?:[a-z0-9.-]{0,62}[a-z0-9])?$")
_SEMVER = re.compile(
    r"^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
    r"(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
    r"(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?$"
)
_SAFE_INTEGER = 9_007_199_254_740_991

_MANIFEST_KEYS = {
    "schemaVersion", "providerId", "providerVersion", "providerApiVersion",
    "clientModulePath", "targets",
}
_TARGET_KEYS = {
    "id", "displayName", "gameCompatibilityLine", "netplayCompatibilityLine",
    "optionsKind", "capabilities", "inputs", "checkpoint", "assetPaths",
}
_CAPABILITY_KEYS = {
    "pause", "screenshot", "checkpoint", "standardGamepad", "frameCounter",
    "volume", "discSwitch", "nativeSettings", "inputFilter", "netplayPort",
    "videoModes", "requiresThreads", "frameMode", "validationProbes",
}
_INPUT_KEYS = {"role", "kind", "cardinality", "optional"}
_CHECKPOINT_KEYS = {"writeFormat", "readFormats", "maxBytes"}
_FRAME_MODES = {
    "NONE", "SAME_ORIGIN_BLANK", "SAME_ORIGIN_RESOURCE", "ISOLATED_ORIGIN_RESOURCE",
}
_RESOURCE_KINDS = {
    "ROM_BLOB_V1", "FILE_TREE_V1", "SEEKABLE_BLOB_V1", "NATIVE_WEB_V1",
    "ISOLATED_WEB_V1", "BIOS_BUNDLE_V1", "PARENT_ARCHIVE_V1", "MULTI_DISC_V1",
    "EXTERNAL_FILE_SET_V1", "WASM4_CART_V1",
}
_VIDEO_MODES = {"original", "pixel", "smooth", "sharp-bilinear", "adaptive-sharpen"}
_OPTIONS_KINDS = {
    "NONE_V1", "EMULATORJS_V1", "RPGMAKER_V1", "ONS_PROJECT_V1", "KIRIKIRI_PROJECT_V1",
}
_AUTHORITY_FILES = (
    "common.schema.json",
    "launch-envelope.schema.json",
    "provider-integrity.schema.json",
    "provider-lock.schema.json",
    "provider-manifest.schema.json",
    "provider-module-v1.d.ts",
    "runtime-resource.schema.json",
    "fixtures/invalid/checkpoint-missing-read-formats.json",
    "fixtures/invalid/duplicate-field.json",
    "fixtures/invalid/exponent-json-input.json",
    "fixtures/invalid/float-json-input.json",
    "fixtures/invalid/invalid-unicode.json",
    "fixtures/invalid/missing-capability.json",
    "fixtures/invalid/netplay-mode-mismatch.json",
    "fixtures/invalid/netplay-resource.json",
    "fixtures/invalid/unknown-top-level.json",
    "fixtures/invalid/unsafe-integer-json-input.json",
    "fixtures/valid/checkpoint-validation.json",
    "fixtures/valid/netplay.json",
    "fixtures/valid/single-minimal.json",
)
_AUTHORITY_REPOSITORY = "https://github.com/retrom-project/retrom"
_AUTHORITY_PATH = "api/runtime-provider/v1"
_GENERATED_TYPE = "provider-module-v1.d.ts"


def validate_provider_manifest(value: object) -> None:
    manifest = _record(value, "manifest")
    _exact_keys(manifest, _MANIFEST_KEYS, "manifest")
    _equal(manifest["schemaVersion"], 1, "manifest.schemaVersion")
    _identity(manifest["providerId"], "manifest.providerId")
    _semver(manifest["providerVersion"], "manifest.providerVersion")
    _positive_integer(manifest["providerApiVersion"], "manifest.providerApiVersion")
    module_path = _safe_path(manifest["clientModulePath"], "manifest.clientModulePath")
    if module_path != "client.mjs":
        _fail("manifest.clientModulePath must be client.mjs")

    targets = _array(manifest["targets"], "manifest.targets")
    if not targets:
        _fail("manifest.targets must not be empty")
    target_ids: list[str] = []
    for index, value_target in enumerate(targets):
        target = _record(value_target, f"manifest.targets[{index}]")
        target_ids.append(_validate_target(target, index))
    _sorted_unique(target_ids, "manifest.targets")


def canonical_json_bytes(value: object) -> bytes:
    try:
        return _canonical(value).encode("utf-8")
    except UnicodeError as error:
        raise ContractError("canonical JSON contains invalid Unicode") from error


def parse_launch_envelope(contents: bytes) -> dict[str, object]:
    def reject_number(value: str) -> NoReturn:
        _fail(f"launch envelope contains a non-integer number: {value}")

    def parse_integer(value: str) -> int:
        parsed = int(value)
        if abs(parsed) > _SAFE_INTEGER:
            _fail("launch envelope integer exceeds the safe range")
        return parsed

    def closed_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
        result: dict[str, object] = {}
        for key, value in pairs:
            if key in result:
                _fail(f"launch envelope contains duplicate field {key}")
            result[key] = value
        return result

    try:
        value = json.loads(
            contents.decode("utf-8"),
            object_pairs_hook=closed_object,
            parse_constant=reject_number,
            parse_float=reject_number,
            parse_int=parse_integer,
        )
    except (UnicodeError, json.JSONDecodeError) as error:
        raise ContractError("launch envelope is not strict JSON") from error
    validate_launch_envelope(value)
    return value


def validate_launch_envelope(value: object) -> None:
    envelope = _launch_record(value, "envelope")
    _launch_keys(envelope, {
        "netplay", "resources", "restore", "runtime", "schemaVersion", "session", "targetOptions", "validation",
    }, "envelope")
    if envelope["schemaVersion"] != 1:
        _fail("envelope.schemaVersion must be 1")
    session = _validate_launch_session(envelope["session"])
    runtime = _validate_launch_runtime(envelope["runtime"])
    _validate_launch_resources(envelope["resources"])
    _validate_launch_options(envelope["targetOptions"])
    _validate_launch_restore(envelope["restore"], runtime["checkpoint"])
    _validate_launch_validation(envelope["validation"], runtime["capabilities"])
    _validate_launch_netplay(envelope["netplay"], runtime["capabilities"], session)


def _validate_launch_session(value: object) -> Mapping[str, object]:
    session = _launch_record(value, "session")
    _launch_keys(session, {"id", "mode", "platformName", "purpose", "returnTo", "title", "warnings"}, "session")
    if not isinstance(session["id"], str) or not re.fullmatch(
        r"[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}", session["id"]
    ) or session["purpose"] not in {"PRODUCT", "REVIEW_PREVIEW", "RUNTIME_VALIDATION"} or \
            session["mode"] not in {"SINGLE", "NETPLAY"}:
        _fail("session identity, purpose, or mode is invalid")
    _launch_text(session["title"], 1, 500, "session.title")
    _launch_text(session["platformName"], 1, 200, "session.platformName")
    _launch_relative_url(session["returnTo"], "session.returnTo")
    warnings = _launch_array(session["warnings"], "session.warnings")
    if len(warnings) > 16:
        _fail("session.warnings is too long")
    for warning in warnings:
        _launch_text(warning, 1, 200, "session.warning")
    return session


def _validate_launch_runtime(value: object) -> Mapping[str, object]:
    runtime = _launch_record(value, "runtime")
    _launch_keys(runtime, {
        "bundleSha256", "capabilities", "checkpoint", "gameCompatibilityLine", "moduleSha256", "moduleUrl",
        "providerApiVersion", "providerId", "providerVersion", "runtimeBaseUrl", "targetContractSha256", "targetId",
    }, "runtime")
    if runtime["providerApiVersion"] != 1:
        _fail("runtime.providerApiVersion must be 1")
    for key in ("providerId", "targetId"):
        if not isinstance(runtime[key], str) or not _IDENTITY.fullmatch(runtime[key]):
            _fail(f"runtime.{key} is invalid")
    if not isinstance(runtime["providerVersion"], str) or not _SEMVER.fullmatch(runtime["providerVersion"]):
        _fail("runtime.providerVersion is invalid")
    if not isinstance(runtime["gameCompatibilityLine"], str) or not _TOKEN.fullmatch(runtime["gameCompatibilityLine"]):
        _fail("runtime.gameCompatibilityLine is invalid")
    for key in ("bundleSha256", "moduleSha256", "targetContractSha256"):
        if not isinstance(runtime[key], str) or not re.fullmatch(r"[0-9a-f]{64}", runtime[key]):
            _fail(f"runtime.{key} is invalid")
    capabilities = _validate_launch_capabilities(runtime["capabilities"])
    checkpoint = runtime["checkpoint"]
    if capabilities["checkpoint"] is True:
        _validate_launch_checkpoint(checkpoint)
    elif checkpoint is not None:
        _fail("runtime.checkpoint contradicts its capability")
    base = f'/runtime/providers/{runtime["providerId"]}/{runtime["bundleSha256"]}/'
    if runtime["runtimeBaseUrl"] != base or runtime["moduleUrl"] != f"{base}client.mjs":
        _fail("runtime Provider URLs are not canonical")
    return runtime


def _validate_launch_capabilities(value: object) -> Mapping[str, object]:
    capabilities = _launch_record(value, "runtime.capabilities")
    keys = {
        "checkpoint", "discSwitch", "frameCounter", "frameMode", "inputFilter", "nativeSettings", "netplayPort",
        "pause", "requiresThreads", "screenshot", "standardGamepad", "validationProbes", "videoModes", "volume",
    }
    _launch_keys(capabilities, keys, "runtime.capabilities")
    for key in keys - {"frameMode", "validationProbes", "videoModes"}:
        if not isinstance(capabilities[key], bool):
            _fail(f"runtime.capabilities.{key} must be boolean")
    if capabilities["frameMode"] not in {
        "NONE", "SAME_ORIGIN_BLANK", "SAME_ORIGIN_RESOURCE", "ISOLATED_ORIGIN_RESOURCE",
    }:
        _fail("runtime.capabilities.frameMode is invalid")
    probes = _launch_string_set(capabilities["validationProbes"], "runtime.capabilities.validationProbes")
    if any(not _TOKEN.fullmatch(probe) for probe in probes):
        _fail("runtime validation probe is invalid")
    modes = _launch_string_set(capabilities["videoModes"], "runtime.capabilities.videoModes")
    if any(mode not in _VIDEO_MODES for mode in modes):
        _fail("runtime video mode is invalid")
    return capabilities


def _validate_launch_checkpoint(value: object) -> None:
    checkpoint = _launch_record(value, "runtime.checkpoint")
    _launch_keys(checkpoint, {"maxBytes", "readFormats", "writeFormat"}, "runtime.checkpoint")
    _launch_positive_integer(checkpoint["maxBytes"], "runtime.checkpoint.maxBytes")
    if not isinstance(checkpoint["writeFormat"], str) or not _TOKEN.fullmatch(checkpoint["writeFormat"]):
        _fail("runtime.checkpoint.writeFormat is invalid")
    formats = _launch_string_set(checkpoint["readFormats"], "runtime.checkpoint.readFormats", allow_empty=False)
    if any(not _TOKEN.fullmatch(item) for item in formats) or checkpoint["writeFormat"] not in formats:
        _fail("runtime.checkpoint.readFormats is invalid")


def _validate_launch_resources(value: object) -> None:
    resources = _launch_array(value, "resources")
    if len(resources) > 128:
        _fail("resources has too many items")
    roles: dict[str, list[int]] = {}
    for resource_value in resources:
        resource = _launch_record(resource_value, "resource")
        role = resource.get("role")
        ordinal = resource.get("ordinal")
        if not isinstance(role, str) or not _IDENTITY.fullmatch(role) or not _launch_non_negative(ordinal):
            _fail("resource identity is invalid")
        roles.setdefault(role, []).append(ordinal)
        kind = resource.get("kind")
        if kind in {"ROM_BLOB_V1", "SEEKABLE_BLOB_V1", "PARENT_ARCHIVE_V1", "WASM4_CART_V1"}:
            _launch_keys(resource, {"kind", "ordinal", "rangeRequired", "role", "sha256", "sizeBytes", "url"}, "resource")
            _launch_digest(resource["sha256"], "resource.sha256")
            _launch_positive_integer(resource["sizeBytes"], "resource.sizeBytes")
            _launch_relative_url(resource["url"], "resource.url")
            expected_range = kind in {"SEEKABLE_BLOB_V1", "PARENT_ARCHIVE_V1"}
            if resource["rangeRequired"] is not expected_range:
                _fail("resource.rangeRequired is invalid")
        elif kind == "FILE_TREE_V1":
            _launch_keys(resource, {"contentDigest", "indexUrl", "kind", "ordinal", "role"}, "resource")
            _launch_digest(resource["contentDigest"], "resource.contentDigest")
            _launch_relative_url(resource["indexUrl"], "resource.indexUrl")
        elif kind in {"NATIVE_WEB_V1", "ISOLATED_WEB_V1"}:
            _launch_keys(resource, {
                "bootstrapTicket", "cleanupUrl", "contentDigest", "entryUrl", "kind", "ordinal", "origin", "role",
            }, "resource")
            _launch_digest(resource["contentDigest"], "resource.contentDigest")
            if not isinstance(resource["origin"], str) or not re.fullmatch(r"https?://[^/#]+(?::[0-9]+)?", resource["origin"]):
                _fail("resource.origin is invalid")
            for key in ("entryUrl", "cleanupUrl"):
                if resource[key] is not None and (not isinstance(resource[key], str) or
                    not resource[key].startswith(f'{resource["origin"]}/') or "#" in resource[key]):
                    _fail(f"resource.{key} is invalid")
            if not isinstance(resource["bootstrapTicket"], str) or not re.fullmatch(r"[A-Za-z0-9_-]{43,128}", resource["bootstrapTicket"]):
                _fail("resource.bootstrapTicket is invalid")
        elif kind in {"BIOS_BUNDLE_V1", "EXTERNAL_FILE_SET_V1"}:
            _launch_keys(resource, {"files", "kind", "ordinal", "role"}, "resource")
            files = _launch_array(resource["files"], "resource.files")
            paths = []
            if not files:
                _fail("resource.files is empty")
            for file_value in files:
                file = _launch_record(file_value, "resource.file")
                _launch_keys(file, {"logicalName", "sha256", "sizeBytes", "url", "virtualPath"}, "resource.file")
                _launch_text(file["logicalName"], 1, 240, "resource.file.logicalName")
                paths.append(_launch_safe_path(file["virtualPath"], "resource.file.virtualPath"))
                _launch_relative_url(file["url"], "resource.file.url")
                _launch_digest(file["sha256"], "resource.file.sha256")
                _launch_positive_integer(file["sizeBytes"], "resource.file.sizeBytes")
            _launch_sorted_unique(paths, "resource.files")
        elif kind == "MULTI_DISC_V1":
            _launch_keys(resource, {"entries", "initialDiscIndex", "kind", "ordinal", "role"}, "resource")
            entries = _launch_array(resource["entries"], "resource.entries")
            if not entries or not _launch_non_negative(resource["initialDiscIndex"]) or resource["initialDiscIndex"] >= len(entries):
                _fail("multi-disc bounds are invalid")
            for index, entry_value in enumerate(entries):
                entry = _launch_record(entry_value, "resource.disc")
                _launch_keys(entry, {"index", "label", "sha256", "sizeBytes", "url"}, "resource.disc")
                if entry["index"] != index:
                    _fail("resource.disc.index is invalid")
                _launch_text(entry["label"], 1, 240, "resource.disc.label")
                _launch_relative_url(entry["url"], "resource.disc.url")
                _launch_digest(entry["sha256"], "resource.disc.sha256")
                _launch_positive_integer(entry["sizeBytes"], "resource.disc.sizeBytes")
        else:
            _fail("resource.kind is invalid")
    if any(ordinals != list(range(len(ordinals))) for ordinals in roles.values()):
        _fail("resource ordinals are not contiguous")


def _validate_launch_options(value: object) -> None:
    options = _launch_record(value, "targetOptions")
    kind = options.get("kind")
    if kind == "NONE_V1":
        _launch_keys(options, {"kind"}, "targetOptions")
    elif kind == "EMULATORJS_V1":
        _launch_keys(options, {"dosEntryPath", "initialDiscIndex", "kind"}, "targetOptions")
        if options["dosEntryPath"] is not None:
            _launch_safe_path(options["dosEntryPath"], "targetOptions.dosEntryPath")
        if options["initialDiscIndex"] is not None and not _launch_non_negative(options["initialDiscIndex"]):
            _fail("targetOptions.initialDiscIndex is invalid")
    elif kind == "RPGMAKER_V1":
        _launch_keys(options, {"expectedRestorePosition", "kind"}, "targetOptions")
        position = options["expectedRestorePosition"]
        if position is not None:
            position_record = _launch_record(position, "targetOptions.expectedRestorePosition")
            _launch_keys(position_record, {"fixtureState", "mapId", "playerX", "playerY"}, "targetOptions.expectedRestorePosition")
            if any(not _launch_non_negative(item) for item in position_record.values()):
                _fail("targetOptions restore position is invalid")
    elif kind == "ONS_PROJECT_V1":
        _launch_keys(options, {"kind", "scriptEncoding"}, "targetOptions")
        if options["scriptEncoding"] not in {"gbk", "sjis", "utf8"}:
            _fail("targetOptions.scriptEncoding is invalid")
    elif kind == "KIRIKIRI_PROJECT_V1":
        _launch_keys(options, {"kind", "startupXp3Path"}, "targetOptions")
        if options["startupXp3Path"] is not None:
            _launch_safe_path(options["startupXp3Path"], "targetOptions.startupXp3Path")
    else:
        _fail("targetOptions.kind is invalid")


def _validate_launch_restore(value: object, checkpoint_value: object) -> None:
    if value is None:
        return
    restore = _launch_record(value, "restore")
    checkpoint = _launch_record(checkpoint_value, "runtime.checkpoint")
    _launch_keys(restore, {"format", "sha256", "sizeBytes", "url"}, "restore")
    if restore["format"] not in checkpoint["readFormats"]:
        _fail("restore.format is unsupported")
    _launch_digest(restore["sha256"], "restore.sha256")
    _launch_positive_integer(restore["sizeBytes"], "restore.sizeBytes")
    if restore["sizeBytes"] > checkpoint["maxBytes"]:
        _fail("restore exceeds checkpoint maxBytes")
    _launch_relative_url(restore["url"], "restore.url")


def _validate_launch_validation(value: object, capabilities_value: object) -> None:
    if value is None:
        return
    validation = _launch_record(value, "validation")
    capabilities = _launch_record(capabilities_value, "runtime.capabilities")
    _launch_keys(validation, {"input", "probeId"}, "validation")
    if validation["probeId"] not in capabilities["validationProbes"]:
        _fail("validation.probeId is unsupported")
    _validate_launch_json(validation["input"], 0)
    if not isinstance(validation["input"], Mapping):
        _fail("validation.input must be an object")


def _validate_launch_netplay(value: object, capabilities_value: object, session: Mapping[str, object]) -> None:
    capabilities = _launch_record(capabilities_value, "runtime.capabilities")
    if value is None:
        if session["mode"] == "NETPLAY":
            _fail("NETPLAY session requires netplay configuration")
        return
    netplay = _launch_record(value, "netplay")
    _launch_keys(netplay, {"playerNo", "profile", "roomId", "sessionId", "socketUrl"}, "netplay")
    if session["mode"] != "NETPLAY" or capabilities["netplayPort"] is not True:
        _fail("netplay configuration is unsupported")
    _launch_text(netplay["roomId"], 1, 128, "netplay.roomId")
    if not isinstance(netplay["sessionId"], str) or not re.fullmatch(
        r"[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}", netplay["sessionId"]
    ) or not isinstance(netplay["playerNo"], int) or isinstance(netplay["playerNo"], bool) or not 1 <= netplay["playerNo"] <= 16:
        _fail("netplay identity is invalid")
    if not isinstance(netplay["socketUrl"], str) or not re.fullmatch(r"wss?://[^#]+", netplay["socketUrl"]):
        _fail("netplay.socketUrl is invalid")
    _validate_launch_json(netplay["profile"], 0)
    if not isinstance(netplay["profile"], Mapping):
        _fail("netplay.profile must be an object")


def _validate_launch_json(value: object, depth: int) -> None:
    if depth > 8:
        _fail("JSON value exceeds maximum depth")
    if value is None or isinstance(value, bool):
        return
    if isinstance(value, str):
        if not _launch_unicode(value):
            _fail("JSON string contains invalid Unicode")
        return
    if isinstance(value, int) and not isinstance(value, bool) and abs(value) <= _SAFE_INTEGER:
        return
    if isinstance(value, list):
        if len(value) > 256:
            _fail("JSON array is too long")
        for item in value:
            _validate_launch_json(item, depth + 1)
        return
    if isinstance(value, Mapping) and len(value) <= 64:
        for key, item in value.items():
            if not _launch_unicode(key):
                _fail("JSON object key contains invalid Unicode")
            _validate_launch_json(item, depth + 1)
        return
    _fail("JSON value is outside the canonical subset")


def _launch_record(value: object, label: str) -> Mapping[str, object]:
    if not isinstance(value, Mapping) or any(not isinstance(key, str) for key in value):
        _fail(f"{label} must be an object")
    return value


def _launch_array(value: object, label: str) -> list[object]:
    if not isinstance(value, list):
        _fail(f"{label} must be an array")
    return value


def _launch_keys(value: Mapping[str, object], keys: set[str], label: str) -> None:
    if set(value) != keys:
        _fail(f"{label} fields are not closed")


def _launch_unicode(value: str) -> bool:
    return not any(0xD800 <= ord(character) <= 0xDFFF for character in value)


def _launch_text(value: object, minimum: int, maximum: int, label: str) -> str:
    if not isinstance(value, str) or not minimum <= len(value) <= maximum or not _launch_unicode(value):
        _fail(f"{label} is invalid")
    return value


def _launch_relative_url(value: object, label: str) -> str:
    if not isinstance(value, str) or not 2 <= len(value) <= 2048 or not value.startswith("/") or \
            value.startswith("//") or "\\" in value or "#" in value or not all(" " <= item <= "~" for item in value):
        _fail(f"{label} is invalid")
    return value


def _launch_safe_path(value: object, label: str) -> str:
    if not isinstance(value, str) or not 1 <= len(value) <= 240 or value.startswith("/") or \
            any(item in value for item in ("\\", "?", "#", "\x00")) or any(
                part in {"", ".", ".."} for part in value.split("/")
            ):
        _fail(f"{label} is invalid")
    return value


def _launch_digest(value: object, label: str) -> str:
    if not isinstance(value, str) or not re.fullmatch(r"[0-9a-f]{64}", value):
        _fail(f"{label} is invalid")
    return value


def _launch_positive_integer(value: object, label: str) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or not 1 <= value <= _SAFE_INTEGER:
        _fail(f"{label} is invalid")
    return value


def _launch_non_negative(value: object) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and 0 <= value <= _SAFE_INTEGER


def _launch_string_set(value: object, label: str, *, allow_empty: bool = True) -> list[str]:
    values = _launch_array(value, label)
    if any(not isinstance(item, str) for item in values) or not allow_empty and not values:
        _fail(f"{label} is invalid")
    result = list(values)
    _launch_sorted_unique(result, label)
    return result


def _launch_sorted_unique(values: list[str], label: str) -> None:
    if any(values[index - 1] >= value for index, value in enumerate(values) if index > 0):
        _fail(f"{label} must be sorted and unique")


def sync_contract_snapshot(runtime_root: Path) -> None:
    authority_root, snapshot_root = _snapshot_paths(runtime_root)
    snapshot_root.mkdir(parents=True, exist_ok=True)
    files = []
    for filename in _AUTHORITY_FILES:
        source_path = authority_root / filename
        payload = source_path.read_bytes()
        _atomic_write(snapshot_root / filename, payload)
        files.append({
            "path": f"api/runtime-provider/v1/{filename}",
            "sha256": hashlib.sha256(payload).hexdigest(),
        })
    source = {
        "authorityPath": _AUTHORITY_PATH,
        "authorityRepository": _AUTHORITY_REPOSITORY,
        "contractSha256": _contract_digest(authority_root),
        "contractVersion": "runtime-provider-v1",
        "files": files,
        "schemaVersion": 1,
    }
    _atomic_write(snapshot_root / "SOURCE.json", _pretty_json(source))
    expected = {*_AUTHORITY_FILES, "SOURCE.json"}
    for path in sorted(snapshot_root.rglob("*"), reverse=True):
        relative = path.relative_to(snapshot_root).as_posix()
        if path.is_symlink():
            _fail(f"runtime contract snapshot contains unsafe entry {relative}")
        if path.is_file() and relative not in expected:
            path.unlink()
        elif path.is_dir() and not any(path.iterdir()):
            path.rmdir()
    _sync_generated_types(authority_root, runtime_root)


def check_contract_snapshot(runtime_root: Path) -> None:
    authority_root, snapshot_root = _snapshot_paths(runtime_root)
    expected_names = sorted((*_AUTHORITY_FILES, "SOURCE.json"))
    actual_names = sorted(
        path.relative_to(snapshot_root).as_posix()
        for path in snapshot_root.rglob("*")
        if path.is_file() and not path.is_symlink()
    ) if snapshot_root.is_dir() and not snapshot_root.is_symlink() else []
    if actual_names != expected_names:
        _fail("runtime contract snapshot file set differs from authority")
    expected_files = []
    for filename in _AUTHORITY_FILES:
        authority_bytes = (authority_root / filename).read_bytes()
        snapshot_bytes = (snapshot_root / filename).read_bytes()
        if authority_bytes != snapshot_bytes:
            _fail(f"runtime contract snapshot differs for {filename}")
        expected_files.append({
            "path": f"api/runtime-provider/v1/{filename}",
            "sha256": hashlib.sha256(authority_bytes).hexdigest(),
        })
    expected_source = {
        "authorityPath": _AUTHORITY_PATH,
        "authorityRepository": _AUTHORITY_REPOSITORY,
        "contractSha256": _contract_digest(authority_root),
        "contractVersion": "runtime-provider-v1",
        "files": expected_files,
        "schemaVersion": 1,
    }
    if (snapshot_root / "SOURCE.json").read_bytes() != _pretty_json(expected_source):
        _fail("runtime contract SOURCE.json differs from authority")
    _check_generated_types(authority_root, runtime_root)


def _contract_digest(authority_root: Path) -> str:
    digest = hashlib.sha256()
    for filename in _AUTHORITY_FILES:
        digest.update(filename.encode("utf-8"))
        digest.update(b"\0")
        digest.update((authority_root / filename).read_bytes())
    return digest.hexdigest()


def _generated_type_paths(runtime_root: Path) -> tuple[Path, Path]:
    repository_root = Path(__file__).resolve().parents[1]
    return (
        repository_root / "web/features/player/runtime/generated/provider-module-v1.ts",
        runtime_root / "src/provider/generated/provider-module-v1.ts",
    )


def _sync_generated_types(authority_root: Path, runtime_root: Path) -> None:
    payload = (authority_root / _GENERATED_TYPE).read_bytes()
    for path in _generated_type_paths(runtime_root):
        path.parent.mkdir(parents=True, exist_ok=True)
        _atomic_write(path, payload)


def _check_generated_types(authority_root: Path, runtime_root: Path) -> None:
    payload = (authority_root / _GENERATED_TYPE).read_bytes()
    for path in _generated_type_paths(runtime_root):
        if not path.is_file() or path.read_bytes() != payload:
            _fail(f"generated Provider Module type differs from authority: {path}")


def _snapshot_paths(runtime_root: Path) -> tuple[Path, Path]:
    if not runtime_root.is_absolute() or not runtime_root.is_dir() or runtime_root.is_symlink():
        _fail("runtime root must be an existing absolute directory")
    authority_root = Path(__file__).resolve().parents[1] / "api/runtime-provider/v1"
    return authority_root, runtime_root / "contracts/retrom-provider/v1"


def _atomic_write(path: Path, payload: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.{os.getpid()}.tmp")
    temporary.write_bytes(payload)
    os.replace(temporary, path)


def _pretty_json(value: object) -> bytes:
    return (json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8")


def _validate_target(target: Mapping[str, object], index: int) -> str:
    label = f"manifest.targets[{index}]"
    _exact_keys(target, _TARGET_KEYS, label)
    target_id = _identity(target["id"], f"{label}.id")
    _bounded_text(target["displayName"], f"{label}.displayName", 1, 120)
    _token(target["gameCompatibilityLine"], f"{label}.gameCompatibilityLine")
    netplay = target["netplayCompatibilityLine"]
    if netplay is not None:
        _token(netplay, f"{label}.netplayCompatibilityLine")
    if target["optionsKind"] not in _OPTIONS_KINDS:
        _fail(f"{label}.optionsKind is unsupported")

    capabilities = _record(target["capabilities"], f"{label}.capabilities")
    _validate_capabilities(capabilities, label)
    inputs = _array(target["inputs"], f"{label}.inputs")
    _validate_inputs(inputs, label)
    checkpoint = target["checkpoint"]
    if capabilities["checkpoint"] is True:
        _validate_checkpoint(_record(checkpoint, f"{label}.checkpoint"), label)
    elif checkpoint is not None:
        _fail(f"{label}.checkpoint must be null when checkpoint capability is false")

    paths = _string_array(target["assetPaths"], f"{label}.assetPaths", allow_empty=False)
    _sorted_unique(paths, f"{label}.assetPaths")
    for path_index, path in enumerate(paths):
        _safe_path(path, f"{label}.assetPaths[{path_index}]")
    return target_id


def _validate_capabilities(capabilities: Mapping[str, object], target_label: str) -> None:
    label = f"{target_label}.capabilities"
    _exact_keys(capabilities, _CAPABILITY_KEYS, label)
    for key in _CAPABILITY_KEYS - {"frameMode", "validationProbes", "videoModes"}:
        if not isinstance(capabilities[key], bool):
            _fail(f"{label}.{key} must be a boolean")
    if capabilities["frameMode"] not in _FRAME_MODES:
        _fail(f"{label}.frameMode is unsupported")
    probes = _string_array(capabilities["validationProbes"], f"{label}.validationProbes")
    _sorted_unique(probes, f"{label}.validationProbes")
    for index, probe in enumerate(probes):
        _token(probe, f"{label}.validationProbes[{index}]")
    video_modes = _string_array(capabilities["videoModes"], f"{label}.videoModes")
    _sorted_unique(video_modes, f"{label}.videoModes")
    if any(mode not in _VIDEO_MODES for mode in video_modes):
        _fail(f"{label}.videoModes contains an unsupported mode")


def _validate_inputs(inputs: Sequence[object], target_label: str) -> None:
    if not inputs:
        _fail(f"{target_label}.inputs must not be empty")
    roles: set[str] = set()
    for index, input_value in enumerate(inputs):
        label = f"{target_label}.inputs[{index}]"
        input_item = _record(input_value, label)
        _exact_keys(input_item, _INPUT_KEYS, label)
        role = _identity(input_item["role"], f"{label}.role")
        if role in roles:
            _fail(f"{target_label}.inputs contains duplicate role {role}")
        roles.add(role)
        if input_item["kind"] not in _RESOURCE_KINDS:
            _fail(f"{label}.kind is unsupported")
        if input_item["cardinality"] not in {"ONE", "MANY"}:
            _fail(f"{label}.cardinality is unsupported")
        if not isinstance(input_item["optional"], bool):
            _fail(f"{label}.optional must be a boolean")


def _validate_checkpoint(checkpoint: Mapping[str, object], target_label: str) -> None:
    label = f"{target_label}.checkpoint"
    _exact_keys(checkpoint, _CHECKPOINT_KEYS, label)
    write_format = _token(checkpoint["writeFormat"], f"{label}.writeFormat")
    read_formats = _string_array(checkpoint["readFormats"], f"{label}.readFormats", allow_empty=False)
    _sorted_unique(read_formats, f"{label}.readFormats")
    for index, read_format in enumerate(read_formats):
        _token(read_format, f"{label}.readFormats[{index}]")
    if write_format not in read_formats:
        _fail(f"{label}.readFormats must contain writeFormat")
    _positive_integer(checkpoint["maxBytes"], f"{label}.maxBytes")


def _canonical(value: object) -> str:
    if value is None:
        return "null"
    if value is True:
        return "true"
    if value is False:
        return "false"
    if isinstance(value, int):
        if abs(value) > _SAFE_INTEGER:
            _fail("canonical JSON integer exceeds the safe range")
        return str(value)
    if isinstance(value, float):
        _fail("canonical JSON floats are outside schema 1")
    if isinstance(value, str):
        return json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    if isinstance(value, list):
        return "[" + ",".join(_canonical(item) for item in value) + "]"
    if isinstance(value, dict):
        if not all(isinstance(key, str) for key in value):
            _fail("canonical JSON object keys must be strings")
        ordered = sorted(value, key=lambda key: key.encode("utf-16be", "surrogatepass"))
        return "{" + ",".join(
            f"{json.dumps(key, ensure_ascii=False)}:{_canonical(value[key])}" for key in ordered
        ) + "}"
    _fail(f"canonical JSON does not support {type(value).__name__}")


def _record(value: object, label: str) -> Mapping[str, object]:
    if not isinstance(value, dict):
        _fail(f"{label} must be an object")
    return value


def _array(value: object, label: str) -> Sequence[object]:
    if not isinstance(value, list):
        _fail(f"{label} must be an array")
    return value


def _string_array(value: object, label: str, *, allow_empty: bool = True) -> list[str]:
    values = _array(value, label)
    if not allow_empty and not values:
        _fail(f"{label} must not be empty")
    if not all(isinstance(item, str) for item in values):
        _fail(f"{label} must contain only strings")
    return list(values)


def _exact_keys(value: Mapping[str, object], expected: set[str], label: str) -> None:
    actual = set(value)
    if actual != expected:
        _fail(f"{label} keys differ: missing={sorted(expected - actual)}, unknown={sorted(actual - expected)}")


def _equal(value: object, expected: object, label: str) -> None:
    if isinstance(value, bool) or value != expected:
        _fail(f"{label} must equal {expected!r}")


def _identity(value: object, label: str) -> str:
    if not isinstance(value, str) or not _IDENTITY.fullmatch(value):
        _fail(f"{label} is not a canonical identity")
    return value


def _token(value: object, label: str) -> str:
    if not isinstance(value, str) or not _TOKEN.fullmatch(value):
        _fail(f"{label} is not a canonical token")
    return value


def _semver(value: object, label: str) -> str:
    if not isinstance(value, str) or not _SEMVER.fullmatch(value):
        _fail(f"{label} is not canonical SemVer without build metadata")
    return value


def _bounded_text(value: object, label: str, minimum: int, maximum: int) -> str:
    if not isinstance(value, str) or not minimum <= len(value) <= maximum:
        _fail(f"{label} length is outside {minimum}..{maximum}")
    return value


def _positive_integer(value: object, label: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or not 1 <= value <= _SAFE_INTEGER:
        _fail(f"{label} must be a positive safe integer")
    return value


def _safe_path(value: object, label: str) -> str:
    if not isinstance(value, str) or not value or value.startswith("/") or "\\" in value:
        _fail(f"{label} is not a safe relative path")
    if "?" in value or "#" in value or "\x00" in value:
        _fail(f"{label} contains a forbidden character")
    if any(part in {"", ".", ".."} for part in value.split("/")):
        _fail(f"{label} contains an unsafe path segment")
    return value


def _sorted_unique(values: Sequence[str], label: str) -> None:
    expected = sorted(set(values), key=lambda item: item.encode("utf-8"))
    if list(values) != expected:
        _fail(f"{label} must be unique and sorted by UTF-8 bytes")


def _fail(message: str) -> NoReturn:
    raise ContractError(message)
