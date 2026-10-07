"""One checked-in identity for the paired offline runtime tool and Providers."""
from __future__ import annotations

import json
from pathlib import Path, PurePosixPath
import re
from typing import Any

try:
    from .runtime_provider_bundle import BUILD_RECORD_KEYS, validate_provider_build_record
    from .runtime_provider_io import _load_json
except ImportError:
    from runtime_provider_bundle import BUILD_RECORD_KEYS, validate_provider_build_record
    from runtime_provider_io import _load_json

REPOSITORY = "https://github.com/retrom-project/retrom-runtime"
DIGEST = re.compile(r"[0-9a-f]{64}")
TAG = re.compile(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-rc\.[1-9][0-9]*)?")
MANIFEST_KEYS = {"schemaVersion", "repository", "sourceTreeSha256", "release", "tool", "providers"}
PROVIDER_KEYS = BUILD_RECORD_KEYS | {"moduleSha256"}
MAX_METADATA = 1024 * 1024


def digest(value: Any) -> bool:
    return isinstance(value, str) and DIGEST.fullmatch(value) is not None


def release_identity(value: Any) -> bool:
    return isinstance(value, dict) and set(value) == {"repository", "tag", "commit"} and \
        value["repository"] == REPOSITORY and isinstance(value["tag"], str) and \
        TAG.fullmatch(value["tag"]) is not None and isinstance(value["commit"], str) and \
        re.fullmatch(r"[0-9a-f]{40}", value["commit"]) is not None


def build_record(provider: dict[str, Any]) -> dict[str, Any]:
    return {key: provider[key] for key in BUILD_RECORD_KEYS}


def validate_manifest(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != MANIFEST_KEYS or type(value["schemaVersion"]) is not int or \
            value["schemaVersion"] != 1 or value["repository"] != REPOSITORY or not digest(value["sourceTreeSha256"]):
        raise ValueError("RUNTIME_INPUT_MANIFEST_INVALID")
    release = value["release"]
    if release is not None and not release_identity(release):
        raise ValueError("RUNTIME_INPUT_RELEASE_INVALID")
    providers = value["providers"]
    if not isinstance(providers, list) or len(providers) != 2:
        raise ValueError("RUNTIME_INPUT_PROVIDERS_INVALID")
    for provider in providers:
        if not isinstance(provider, dict) or set(provider) != PROVIDER_KEYS or not digest(provider["moduleSha256"]):
            raise ValueError("RUNTIME_INPUT_PROVIDER_INVALID")
        validate_provider_build_record(build_record(provider))
    if [provider["providerId"] for provider in providers] != ["emulatorjs", "retrom-runtime"]:
        raise ValueError("RUNTIME_INPUT_PROVIDERS_INVALID")
    versions = {provider["providerVersion"] for provider in providers}
    if len(versions) != 1 or release is not None and versions != {release["tag"][1:]}:
        raise ValueError("RUNTIME_INPUT_VERSION_INVALID")
    tool = value["tool"]
    if not isinstance(tool, dict) or set(tool) != {"archive", "sizeBytes", "sha256"} or \
            tool["archive"] != f"retrom-runtime-host-tool-{next(iter(versions))}.tar.gz" or \
            type(tool["sizeBytes"]) is not int or not 0 < tool["sizeBytes"] <= 8 * 1024**3 or not digest(tool["sha256"]):
        raise ValueError("RUNTIME_INPUT_TOOL_INVALID")
    return value


def load_manifest(path: Path) -> dict[str, Any]:
    if path.is_symlink() or not path.is_file() or path.stat().st_size > MAX_METADATA:
        raise ValueError("RUNTIME_INPUT_MANIFEST_REQUIRED")
    return validate_manifest(_load_json(path, "RUNTIME_INPUT_MANIFEST_INVALID"))


def manifest_bytes(value: dict[str, Any]) -> bytes:
    return (json.dumps(validate_manifest(value), ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode()


def artifact_records(value: dict[str, Any]) -> list[dict[str, Any]]:
    return [value["tool"], *({"archive": PurePosixPath(provider["archive"]).name,
                              "sizeBytes": provider["bundleSizeBytes"], "sha256": provider["bundleSha256"]}
                             for provider in value["providers"])]
