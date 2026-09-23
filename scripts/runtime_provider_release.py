"""Resolve the single checked-in runtime tag to verified installation inputs."""
from __future__ import annotations

import json
import re
from pathlib import Path, PurePosixPath
from typing import Any

if __package__:
    from scripts.runtime_provider_bundle import validate_provider_build_record, validate_provider_lock
    from scripts.runtime_provider_io import _fetch_bytes, _load_json, _write_bytes_atomic, _write_json_atomic
else:
    from runtime_provider_bundle import validate_provider_build_record, validate_provider_lock
    from runtime_provider_io import _fetch_bytes, _load_json, _write_bytes_atomic, _write_json_atomic


REPOSITORY = "https://github.com/retrom-project/retrom-runtime"
PROVIDER_IDS = {"emulatorjs", "retrom-runtime"}
RELEASE_TAG = re.compile(r"^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$")
METADATA_MAX_BYTES = 1024 * 1024


def _validate_tag(tag: Any) -> None:
    if not isinstance(tag, str) or not RELEASE_TAG.fullmatch(tag):
        raise ValueError("PROVIDER_RELEASE_CONFIG_INVALID")


def load_release_config(path: Path) -> dict[str, str]:
    value = _load_json(path, "PROVIDER_RELEASE_CONFIG_INVALID")
    if not isinstance(value, dict) or set(value) != {"tag"}:
        raise ValueError("PROVIDER_RELEASE_CONFIG_INVALID")
    _validate_tag(value["tag"])
    return value


def pin_provider_release(tag: str, release_path: Path, cache_root: Path, fetch_bytes=None) -> dict[str, str]:
    resolve_provider_release(tag, cache_root, fetch_bytes)
    value = {"tag": tag}
    _write_json_atomic(release_path, value)
    return value


def resolve_provider_release(tag: str, cache_root: Path, fetch_bytes=None) -> tuple[dict, list[dict]]:
    """Cache immutable release metadata; retain all byte checks in resolved inputs."""
    _validate_tag(tag)
    descriptor = cache_root / "releases" / tag / "provider-release.json"
    cached = descriptor.exists()
    if cached:
        with descriptor.open("rb") as source:
            contents = source.read(METADATA_MAX_BYTES + 1)
    else:
        contents = (fetch_bytes or _fetch_bytes)(
            f"{REPOSITORY}/releases/download/{tag}/provider-release.json", METADATA_MAX_BYTES,
        )
    metadata = _parse_release_metadata(contents, tag)
    release = metadata["release"]
    locks = [_resolved_provider_lock(release, provider) for provider in metadata["providers"]]
    if not cached:
        _write_bytes_atomic(descriptor, contents)
    return release, sorted(locks, key=lambda lock: lock["providerId"])


def _parse_release_metadata(contents: bytes, tag: str) -> dict[str, Any]:
    try:
        if not isinstance(contents, bytes) or len(contents) > METADATA_MAX_BYTES:
            raise ValueError("descriptor exceeds limit")
        value = json.loads(contents)
        if not isinstance(value, dict) or set(value) != {"schemaVersion", "release", "providers"} or \
                type(value["schemaVersion"]) is not int or value["schemaVersion"] != 1 or \
                not _valid_release_identity(value["release"]) or value["release"]["tag"] != tag:
            raise ValueError("release identity mismatch")
        providers = value["providers"]
        if not isinstance(providers, list) or len(providers) != len(PROVIDER_IDS):
            raise ValueError("incomplete release")
        for provider in providers:
            validate_provider_build_record(provider)
            if provider["providerVersion"] != tag[1:]:
                raise ValueError("provider version mismatch")
        if {provider["providerId"] for provider in providers} != PROVIDER_IDS:
            raise ValueError("provider set mismatch")
        return value
    except (ValueError, UnicodeError) as error:
        raise ValueError("PROVIDER_RELEASE_METADATA_INVALID") from error


def _valid_release_identity(value: Any) -> bool:
    return isinstance(value, dict) and set(value) == {"repository", "tag", "commit"} and \
        value["repository"] == REPOSITORY and isinstance(value["commit"], str) and \
        re.fullmatch(r"[0-9a-f]{40}", value["commit"]) is not None and \
        isinstance(value["tag"], str) and RELEASE_TAG.fullmatch(value["tag"]) is not None


def _resolved_provider_lock(release: dict, provider: dict) -> dict:
    # The shared lock schema describes this derived in-memory installation input,
    # not another checked-in file or a setting the operator needs to maintain.
    return validate_provider_lock({
        **{key: provider[key] for key in (
            "providerId", "providerVersion", "bundleSha256", "bundleSizeBytes",
            "unpackedSizeBytes", "fileCount", "manifestSha256",
        )},
        **release,
        "schemaVersion": 1,
        "bundleUrl": f'{REPOSITORY}/releases/download/{release["tag"]}/{PurePosixPath(provider["archive"]).name}',
    })
