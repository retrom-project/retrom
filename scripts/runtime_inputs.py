"""Materialize the one pinned Provider input set atomically, for local and CI use."""
from __future__ import annotations

import hashlib
import fcntl
from pathlib import Path
import shutil
import tempfile
from typing import Any

try:
    from .runtime_input_manifest import digest, load_manifest, manifest_bytes
    from .runtime_input_transport import transport_base
    from .runtime_provider_bundle import describe_installed_provider
    from .runtime_provider_io import _load_json, _write_json_atomic
    from .runtime_provider_sync import sync_providers
except ImportError:
    from runtime_input_manifest import digest, load_manifest, manifest_bytes
    from runtime_input_transport import transport_base
    from runtime_provider_bundle import describe_installed_provider
    from runtime_provider_io import _load_json, _write_json_atomic
    from runtime_provider_sync import sync_providers


def verify_provider(root: Path, record: dict[str, Any], manifest: dict[str, Any]) -> dict[str, Any]:
    if (root / "installed" / record["providerId"] / record["bundleSha256"] / "integrity.json").is_symlink():
        raise ValueError("RUNTIME_INPUT_SYMLINK_OR_SPECIAL_FILE")
    # bundleSha256 is the digest of integrity.json; this rehashes every installed file.
    provider = describe_installed_provider(record, root / "installed")
    if provider["moduleSha256"] != record["moduleSha256"]:
        raise ValueError("RUNTIME_INPUT_MODULE_MISMATCH")
    installed = root / "installed" / provider["installationPath"]
    provenance = _load_json(installed / "provenance.json", "RUNTIME_INPUT_PROVENANCE_INVALID")
    if not isinstance(provenance, dict) or provenance.get("runtimeSourceTreeSha256") != manifest["sourceTreeSha256"]:
        raise ValueError("RUNTIME_INPUT_SOURCE_MISMATCH")
    fingerprints = _load_json(installed / "runtime-fingerprints.json", "RUNTIME_INPUT_FINGERPRINTS_INVALID")
    targets = {target["id"] for target in provider["targets"]}
    if not isinstance(fingerprints, dict) or fingerprints.get("schemaVersion") != 1 or \
            fingerprints.get("providerId") != provider["providerId"] or not isinstance(fingerprints.get("targets"), dict) or \
            set(fingerprints["targets"]) != targets or any(not isinstance(value, dict) or not digest(value.get("fingerprint"))
                                                         for value in fingerprints["targets"].values()):
        raise ValueError("RUNTIME_INPUT_FINGERPRINTS_INVALID")
    return provider


def active_value(providers: list[dict[str, Any]], manifest: dict[str, Any]) -> dict[str, Any]:
    return {"schemaVersion": 1, "providers": providers, "source": "candidate" if manifest["release"] is None else "production",
            "release": manifest["release"], "sourceTreeSha256": manifest["sourceTreeSha256"] if manifest["release"] is None else None}


def verify_providers(providers: Path, manifest: dict[str, Any]) -> None:
    if providers.is_symlink() or not providers.is_dir() or any(path.is_symlink() or not (path.is_file() or path.is_dir())
                                                            for path in providers.rglob("*")):
        raise ValueError("RUNTIME_INPUT_SYMLINK_OR_SPECIAL_FILE")
    described = [verify_provider(providers, record, manifest) for record in manifest["providers"]]
    if _load_json(providers / "active.json", "RUNTIME_PROVIDER_ACTIVE_INVALID") != active_value(described, manifest):
        raise ValueError("RUNTIME_INPUT_ACTIVE_MISMATCH")


def verify_prepared(root: Path, manifest: dict[str, Any]) -> None:
    if load_manifest(root / "runtime-inputs.json") != manifest:
        raise ValueError("RUNTIME_INPUT_PREPARED_PIN_MISMATCH")
    verify_providers(root / "providers", manifest)


def prepare(manifest_path: Path, output_root: Path, cache_root: Path, *, archive_root: Path | None = None,
            base_url: str = "") -> Path:
    manifest = load_manifest(manifest_path)
    base = transport_base(manifest, base_url)
    identity = hashlib.sha256(manifest_bytes(manifest)).hexdigest()
    output_root.mkdir(parents=True, exist_ok=True)
    destination = output_root / identity
    with (output_root / ("." + identity + ".lock")).open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        try:
            return _prepare_locked(manifest, destination, output_root, cache_root, archive_root, base)
        finally:
            fcntl.flock(lock, fcntl.LOCK_UN)


def _prepare_locked(manifest, destination, output_root, cache_root, archive_root, base):
    if destination.exists():
        verify_prepared(destination, manifest)
        return destination
    staging = Path(tempfile.mkdtemp(prefix=".prepare-", dir=output_root))
    try:
        _write_json_atomic(staging / "runtime-inputs.json", manifest)
        # Providers come only from content-addressed chunks: the shared cache serves unchanged
        # chunks of every earlier version, so a new pin downloads only what changed.
        sync_providers(staging / "runtime-inputs.json", staging / "providers", cache=cache_root / "chunks",
                       source=str(archive_root) if archive_root is not None else base)
        verify_prepared(staging, manifest)
        staging.chmod(0o755)
        staging.rename(destination)
        return destination
    finally:
        if staging.exists():
            shutil.rmtree(staging)
