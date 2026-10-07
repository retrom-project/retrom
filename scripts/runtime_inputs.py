"""Materialize the one pinned runtime input set atomically, for local and CI use."""
from __future__ import annotations

import hashlib
import fcntl
from pathlib import Path
import shutil
import subprocess
import tempfile
import stat
import tarfile
from typing import Any

try:
    from .runtime_input_archive import archive_integrity, unpack_tool
    from .runtime_input_manifest import artifact_records, build_record, digest, load_manifest, manifest_bytes
    from .runtime_input_transport import artifact, transport_base
    from .runtime_provider_bundle import describe_installed_provider, install_provider_bundle
    from .runtime_provider_io import _fetch_bytes, _load_json, _write_json_atomic
except ImportError:
    from runtime_input_archive import archive_integrity, unpack_tool
    from runtime_input_manifest import artifact_records, build_record, digest, load_manifest, manifest_bytes
    from runtime_input_transport import artifact, transport_base
    from runtime_provider_bundle import describe_installed_provider, install_provider_bundle
    from runtime_provider_io import _fetch_bytes, _load_json, _write_json_atomic


def file_digest(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def regular_files(root: Path) -> dict[str, str]:
    files: dict[str, str] = {}
    if not root.is_dir() or root.is_symlink():
        raise ValueError("RUNTIME_INPUT_DIRECTORY_REQUIRED")
    for path in sorted(root.rglob("*")):
        if path.is_symlink() or not (path.is_dir() or path.is_file()):
            raise ValueError("RUNTIME_INPUT_SYMLINK_OR_SPECIAL_FILE")
        if path.is_file():
            files[path.relative_to(root).as_posix()] = file_digest(path)
    return files


def verify_tool(tool: Path, manifest: dict[str, Any]) -> None:
    regular_files(tool)
    metadata = _load_json(tool / "host-tool.json", "RUNTIME_INPUT_TOOL_METADATA_INVALID")
    if not isinstance(metadata, dict) or set(metadata) != {"schemaVersion", "version", "sourceTreeSha256"} or \
            metadata["schemaVersion"] != 1 or metadata["version"] != manifest["providers"][0]["providerVersion"] or \
            metadata["sourceTreeSha256"] != manifest["sourceTreeSha256"]:
        raise ValueError("RUNTIME_INPUT_SOURCE_MISMATCH")
    package = _load_json(tool / "package.json", "RUNTIME_INPUT_PACKAGE_INVALID")
    if not isinstance(package, dict) or not package.get("dependencies") or not (tool / "node_modules").is_dir():
        raise ValueError("RUNTIME_INPUT_TOOL_DEPENDENCIES_MISSING")
    for path in ("scripts/runtime-cli.mjs", "dist/runtime/index.js"):
        if not (tool / path).is_file():
            raise ValueError("RUNTIME_INPUT_TOOL_ENTRY_MISSING")


def _archive_integrity(archive: Path) -> bytes:
    with tarfile.open(archive, "r|gz") as source:
        for member in source:
            if member.name == "integrity.json":
                if not member.isfile() or member.size > 16 * 1024 * 1024:
                    raise ValueError("RUNTIME_INPUT_PROVIDER_INTEGRITY_INVALID")
                stream = source.extractfile(member)
                if stream is None:
                    raise ValueError("RUNTIME_INPUT_PROVIDER_INTEGRITY_INVALID")
                with stream:
                    return stream.read()
    raise ValueError("RUNTIME_INPUT_PROVIDER_INTEGRITY_INVALID")


def verify_provider(root: Path, record: dict[str, Any], manifest: dict[str, Any], archive: Path) -> dict[str, Any]:
    installed = root / "installed" / record["providerId"] / record["bundleSha256"]
    if (installed / "integrity.json").is_symlink() or (installed / "integrity.json").read_bytes() != _archive_integrity(archive):
        raise ValueError("RUNTIME_INPUT_PROVIDER_ARCHIVE_MISMATCH")
    provider = describe_installed_provider(build_record(record), root / "installed")
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


def verify_roots(tool: Path, providers: Path, manifest: dict[str, Any], *, archives: Path, node: str | None = None) -> None:
    if providers.is_symlink() or not providers.is_dir() or any(path.is_symlink() or not (path.is_file() or path.is_dir())
                                                            for path in providers.rglob("*")):
        raise ValueError("RUNTIME_INPUT_SYMLINK_OR_SPECIAL_FILE")
    for record in artifact_records(manifest):
        path = archives / record["archive"]
        if path.is_symlink() or not path.is_file() or path.stat().st_size != record["sizeBytes"] or file_digest(path) != record["sha256"]:
            raise ValueError("RUNTIME_INPUT_ARTIFACT_DIGEST_INVALID")
    contents = {name: {"sha256": digest, "sizeBytes": (tool / name).stat().st_size,
                       "executable": bool((tool / name).stat().st_mode & (stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH))}
                for name, digest in regular_files(tool).items()}
    if archive_integrity(archives / manifest["tool"]["archive"]) != contents:
        raise ValueError("RUNTIME_INPUT_TOOL_ARCHIVE_MISMATCH")
    verify_tool(tool, manifest)
    described = [verify_provider(providers, record, manifest, archives / Path(record["archive"]).name) for record in manifest["providers"]]
    if _load_json(providers / "active.json", "RUNTIME_PROVIDER_ACTIVE_INVALID") != active_value(described, manifest):
        raise ValueError("RUNTIME_INPUT_ACTIVE_MISMATCH")
    if node:
        subprocess.run([node, str(tool / "scripts/runtime-cli.mjs"), "catalog"], input="{}", text=True,
                       check=True, stdout=subprocess.DEVNULL, timeout=60)


def verify_prepared(root: Path, manifest: dict[str, Any], *, node: str | None = None) -> None:
    if load_manifest(root / "runtime-inputs.json") != manifest:
        raise ValueError("RUNTIME_INPUT_PREPARED_PIN_MISMATCH")
    verify_roots(root / "tool", root / "providers", manifest,
                 archives=root / "archives", node=node)


def prepare(manifest_path: Path, output_root: Path, cache_root: Path, *, archive_root: Path | None = None,
            base_url: str = "", node: str | None = None, fetch=_fetch_bytes) -> Path:
    manifest = load_manifest(manifest_path)
    base = transport_base(manifest, base_url)
    identity = hashlib.sha256(manifest_bytes(manifest)).hexdigest()
    output_root.mkdir(parents=True, exist_ok=True)
    destination = output_root / identity
    with (output_root / ("." + identity + ".lock")).open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        try:
            return _prepare_locked(manifest, destination, output_root, cache_root, archive_root, base, node, fetch)
        finally:
            fcntl.flock(lock, fcntl.LOCK_UN)


def _prepare_locked(manifest, destination, output_root, cache_root, archive_root, base, node, fetch):
    if destination.exists():
        verify_prepared(destination, manifest, node=node)
        return destination
    staging = Path(tempfile.mkdtemp(prefix=".prepare-", dir=output_root))
    try:
        archives = staging / "archives"
        archives.mkdir()
        for record in artifact_records(manifest):
            cached = artifact(record, cache_root, archive_root, base, fetch)
            shutil.copyfile(cached, archives / record["archive"])
        tool = staging / "tool"
        tool.mkdir()
        unpack_tool(archives / manifest["tool"]["archive"], tool)
        verify_tool(tool, manifest)
        providers = staging / "providers"
        for record in manifest["providers"]:
            install_provider_bundle(archives / Path(record["archive"]).name, build_record(record), providers / "installed")
        described = [verify_provider(providers, record, manifest, archives / Path(record["archive"]).name) for record in manifest["providers"]]
        _write_json_atomic(providers / "active.json", active_value(described, manifest))
        _write_json_atomic(staging / "runtime-inputs.json", manifest)
        verify_prepared(staging, manifest, node=node)
        staging.chmod(0o755)
        staging.rename(destination)
        return destination
    finally:
        if staging.exists():
            shutil.rmtree(staging)
