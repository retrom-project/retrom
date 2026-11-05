"""One checked-in identity for the paired Providers of a runtime release; go.mod pins the host module."""
from __future__ import annotations

import json
from pathlib import Path
import re
from typing import Any

try:
    from .runtime_provider_bundle import INSTALL_RECORD_KEYS, install_record
    from .runtime_provider_io import _load_json
except ImportError:
    from runtime_provider_bundle import INSTALL_RECORD_KEYS, install_record
    from runtime_provider_io import _load_json

REPOSITORY = "https://github.com/retrom-project/retrom-runtime"
DIGEST = re.compile(r"[0-9a-f]{64}")
TAG = re.compile(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-rc\.[1-9][0-9]*)?")
MANIFEST_KEYS = {"schemaVersion", "repository", "sourceTreeSha256", "release", "providers"}
MODULE = "github.com/retrom-project/retrom-runtime"
PROVIDER_KEYS = INSTALL_RECORD_KEYS | {"chunkManifest", "moduleSha256"}
MAX_METADATA = 1024 * 1024


def digest(value: Any) -> bool:
    return isinstance(value, str) and DIGEST.fullmatch(value) is not None


def release_identity(value: Any) -> bool:
    return isinstance(value, dict) and set(value) == {"repository", "tag", "commit"} and \
        value["repository"] == REPOSITORY and isinstance(value["tag"], str) and \
        TAG.fullmatch(value["tag"]) is not None and isinstance(value["commit"], str) and \
        re.fullmatch(r"[0-9a-f]{40}", value["commit"]) is not None


def validate_manifest(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != MANIFEST_KEYS or type(value["schemaVersion"]) is not int or \
            value["schemaVersion"] != 2 or value["repository"] != REPOSITORY or not digest(value["sourceTreeSha256"]):
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
        install_record(provider)
        chunks = provider["chunkManifest"]
        if not isinstance(chunks, dict) or set(chunks) != {"asset", "sha256", "sizeBytes"} or \
                chunks["asset"] != f'{provider["providerId"]}-chunks-{provider["providerVersion"]}.json' or \
                not digest(chunks["sha256"]) or type(chunks["sizeBytes"]) is not int or \
                not 0 < chunks["sizeBytes"] <= 16 * MAX_METADATA:
            raise ValueError("RUNTIME_INPUT_PROVIDER_INVALID")
    if [provider["providerId"] for provider in providers] != ["emulatorjs", "retrom-runtime"]:
        raise ValueError("RUNTIME_INPUT_PROVIDERS_INVALID")
    versions = {provider["providerVersion"] for provider in providers}
    if len(versions) != 1 or release is not None and versions != {release["tag"][1:]}:
        raise ValueError("RUNTIME_INPUT_VERSION_INVALID")
    return value


def load_manifest(path: Path) -> dict[str, Any]:
    if path.is_symlink() or not path.is_file() or path.stat().st_size > MAX_METADATA:
        raise ValueError("RUNTIME_INPUT_MANIFEST_REQUIRED")
    return validate_manifest(_load_json(path, "RUNTIME_INPUT_MANIFEST_INVALID"))


def manifest_bytes(value: dict[str, Any]) -> bytes:
    return (json.dumps(validate_manifest(value), ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode()


def module_version(go_mod: str) -> str:
    """The runtime host module version that go.mod requires."""
    found = []
    for line in go_mod.splitlines():
        parts = line.split()
        if parts[:1] == [MODULE] and len(parts) >= 2:
            found.append(parts[1])
        elif parts[:2] == ["require", MODULE] and len(parts) >= 3:
            found.append(parts[2])
    if len(found) != 1:
        raise ValueError("RUNTIME_INPUT_MODULE_REQUIRED")
    return found[0]


def check_module_pin(manifest: dict[str, Any], go_mod: str) -> None:
    """A formal Provider pin and the embedded host module come from the same immutable release tag."""
    version = module_version(go_mod)
    if manifest["release"] is not None and version != manifest["release"]["tag"]:
        raise ValueError("RUNTIME_INPUT_MODULE_MISMATCH")
