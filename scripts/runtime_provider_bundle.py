from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import tarfile
import tempfile
from pathlib import Path, PurePosixPath
from typing import Any

from runtime_provider_contract import validate_provider_manifest


LOCK_KEYS = {
    "schemaVersion", "providerId", "providerVersion", "repository", "tag", "commit",
    "bundleUrl", "bundleSha256", "bundleSizeBytes", "unpackedSizeBytes", "fileCount",
    "manifestSha256",
}
BUILD_RECORD_KEYS = {
    "archive", "bundleDirectory", "bundleSha256", "bundleSizeBytes", "fileCount",
    "manifestSha256", "providerId", "providerVersion", "unpackedSizeBytes",
}
INTEGRITY_KEYS = {"schemaVersion", "files"}
INTEGRITY_FILE_KEYS = {"path", "sizeBytes", "sha256", "mediaType"}
MEDIA_TYPES = {
    "text/javascript; charset=utf-8", "text/css; charset=utf-8",
    "text/plain; charset=utf-8", "application/json; charset=utf-8",
    "application/wasm", "application/octet-stream", "application/zip",
    "application/x-7z-compressed", "image/png", "image/jpeg", "image/gif",
    "image/webp", "image/svg+xml", "image/x-icon", "audio/ogg", "audio/mpeg",
    "audio/wav", "font/woff", "font/woff2",
}
SEMVER = re.compile(r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$")
LOWER_DIGEST = re.compile(r"^[0-9a-f]{64}$")
LOWER_COMMIT = re.compile(r"^[0-9a-f]{40}$")
PROVIDER_ID = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$")
RELEASE_TAG = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+$")
MAX_SAFE_INTEGER = 9_007_199_254_740_991
SUPPORTED_PROVIDER_API_VERSION = 1


def validate_provider_lock(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != LOCK_KEYS:
        _lock_invalid()
    if value["schemaVersion"] != 1 or not _match(PROVIDER_ID, value["providerId"]):
        _lock_invalid()
    if not _match(SEMVER, value["providerVersion"]) or not _match(RELEASE_TAG, value["tag"]):
        _lock_invalid()
    if not _match(LOWER_COMMIT, value["commit"]) or not _match(LOWER_DIGEST, value["bundleSha256"]):
        _lock_invalid()
    if not _match(LOWER_DIGEST, value["manifestSha256"]):
        _lock_invalid()
    if not _bounded_integer(value["bundleSizeBytes"], minimum=1, maximum=MAX_SAFE_INTEGER):
        _lock_invalid()
    if not _bounded_integer(value["unpackedSizeBytes"], minimum=1, maximum=MAX_SAFE_INTEGER):
        _lock_invalid()
    if not _bounded_integer(value["fileCount"], minimum=3, maximum=100_000):
        _lock_invalid()
    if not _https_url(value["repository"]) or not _https_url(value["bundleUrl"]):
        _lock_invalid()
    if not value["bundleUrl"].endswith(f"/{value['providerId']}-provider-{value['providerVersion']}.tar.gz"):
        _lock_invalid()
    return value


def validate_provider_build_record(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != BUILD_RECORD_KEYS:
        _lock_invalid()
    if not _match(PROVIDER_ID, value["providerId"]) or not _match(SEMVER, value["providerVersion"]):
        _lock_invalid()
    expected_name = f'{value["providerId"]}-provider-{value["providerVersion"]}.tar.gz'
    if value["archive"] != f'{value["providerId"]}/{expected_name}' or \
            value["bundleDirectory"] != f'{value["providerId"]}/{value["providerId"]}-{value["providerVersion"]}':
        _lock_invalid()
    if not _match(LOWER_DIGEST, value["bundleSha256"]) or not _match(LOWER_DIGEST, value["manifestSha256"]):
        _lock_invalid()
    if not _bounded_integer(value["bundleSizeBytes"], minimum=1, maximum=MAX_SAFE_INTEGER) or \
            not _bounded_integer(value["unpackedSizeBytes"], minimum=1, maximum=MAX_SAFE_INTEGER) or \
            not _bounded_integer(value["fileCount"], minimum=3, maximum=100_000):
        _lock_invalid()
    return value


def install_provider_bundle(archive: Path, lock_value: Any, installed_root: Path) -> Path:
    lock = _validate_install_record(lock_value)
    archive = archive.resolve(strict=True)
    installed_root = installed_root.resolve()
    archive_bytes = archive.read_bytes()
    if len(archive_bytes) != lock["bundleSizeBytes"] or _digest(archive_bytes) != lock["bundleSha256"]:
        raise ValueError("PROVIDER_BUNDLE_DIGEST_INVALID")
    destination = installed_root / lock["providerId"] / lock["bundleSha256"]
    if destination.exists():
        _verify_existing(destination, lock)
        _verify_extracted(destination, lock, allow_proof=True)
        return destination
    destination.parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=f".{lock['providerId']}-", dir=destination.parent))
    try:
        _extract_closed_archive(archive, staging, lock)
        _verify_extracted(staging, lock)
        _write_json(staging / ".installation.json", _installation_proof(lock))
        try:
            os.replace(staging, destination)
        except FileExistsError:
            _verify_existing(destination, lock)
        return destination
    finally:
        if staging.exists():
            shutil.rmtree(staging)


def load_provider_lock(path: Path) -> dict[str, Any]:
    try:
        return validate_provider_lock(json.loads(path.read_text(encoding="utf-8")))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ValueError("PROVIDER_LOCK_INVALID") from error


def check_installed_provider(lock_value: Any, installed_root: Path) -> Path:
    lock = _validate_install_record(lock_value)
    destination = installed_root.resolve() / lock["providerId"] / lock["bundleSha256"]
    _verify_existing(destination, lock)
    _verify_extracted(destination, lock, allow_proof=True)
    return destination


def describe_installed_provider(lock_value: Any, installed_root: Path) -> dict[str, Any]:
    lock = _validate_install_record(lock_value)
    destination = check_installed_provider(lock, installed_root)
    manifest_bytes = (destination / "provider.json").read_bytes()
    integrity = json.loads((destination / "integrity.json").read_text(encoding="utf-8"))
    manifest = json.loads(manifest_bytes)
    integrity_by_path = {entry["path"]: entry for entry in integrity["files"]}
    targets = []
    for target in manifest["targets"]:
        assets = [{
            "path": path,
            "sha256": integrity_by_path[path]["sha256"],
            "sizeBytes": integrity_by_path[path]["sizeBytes"],
        } for path in target["assetPaths"]]
        targets.append({
            "checkpoint": target["checkpoint"],
            "gameCompatibilityLine": target["gameCompatibilityLine"],
            "id": target["id"],
            "netplayCompatibilityLine": target["netplayCompatibilityLine"],
            "targetContractSha256": _digest(_canonical_json({
                "assets": assets, "schemaVersion": 1, "target": target,
            })),
        })
    module_path = manifest["clientModulePath"]
    return {
        "bundleSha256": lock["bundleSha256"],
        "bundleSizeBytes": lock["bundleSizeBytes"],
        "clientModulePath": module_path,
        "fileCount": lock["fileCount"],
        "installationPath": f'{lock["providerId"]}/{lock["bundleSha256"]}',
        "manifestSha256": lock["manifestSha256"],
        "moduleSha256": integrity_by_path[module_path]["sha256"],
        "providerApiVersion": manifest["providerApiVersion"],
        "providerId": lock["providerId"],
        "providerVersion": lock["providerVersion"],
        "targets": targets,
        "unpackedSizeBytes": lock["unpackedSizeBytes"],
    }


def _extract_closed_archive(archive: Path, staging: Path, lock: dict[str, Any]) -> None:
    count = 0
    size = 0
    names: set[str] = set()
    try:
        with tarfile.open(archive, "r:gz") as source:
            for member in source:
                if not member.isfile() or member.islnk() or member.issym() or not _safe_path(member.name):
                    raise ValueError("PROVIDER_BUNDLE_UNSAFE")
                if member.name in names or member.size < 0:
                    raise ValueError("PROVIDER_BUNDLE_UNSAFE")
                names.add(member.name)
                count += 1
                size += member.size
                if count > lock["fileCount"] or size > lock["unpackedSizeBytes"]:
                    raise ValueError("PROVIDER_BUNDLE_LIMIT_EXCEEDED")
                contents = source.extractfile(member)
                if contents is None:
                    raise ValueError("PROVIDER_BUNDLE_UNSAFE")
                payload = contents.read(member.size + 1)
                if len(payload) != member.size:
                    raise ValueError("PROVIDER_BUNDLE_UNSAFE")
                target = staging / member.name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(payload)
        if count != lock["fileCount"] or size != lock["unpackedSizeBytes"]:
            raise ValueError("PROVIDER_BUNDLE_LIMIT_INVALID")
    except (tarfile.TarError, OSError) as error:
        raise ValueError("PROVIDER_BUNDLE_UNSAFE") from error


def _verify_extracted(root: Path, lock: dict[str, Any], allow_proof: bool = False) -> None:
    files = _collect_files(root, allow_proof)
    integrity_bytes = files.get("integrity.json")
    manifest_bytes = files.get("provider.json")
    if integrity_bytes is None or manifest_bytes is None:
        _integrity_invalid()
    try:
        integrity = json.loads(integrity_bytes)
        manifest_value = json.loads(manifest_bytes)
    except (UnicodeError, json.JSONDecodeError) as error:
        raise ValueError("PROVIDER_INTEGRITY_INVALID") from error
    if not isinstance(integrity, dict) or set(integrity) != INTEGRITY_KEYS or integrity["schemaVersion"] != 1:
        _integrity_invalid()
    entries = integrity["files"]
    if not isinstance(entries, list):
        _integrity_invalid()
    expected_paths: list[str] = []
    for entry in entries:
        if not isinstance(entry, dict) or set(entry) != INTEGRITY_FILE_KEYS:
            _integrity_invalid()
        path = entry["path"]
        contents = files.get(path) if isinstance(path, str) else None
        if not _safe_path(path) or path == "integrity.json" or contents is None:
            _integrity_invalid()
        if not _nonnegative_integer(entry["sizeBytes"]) or entry["sizeBytes"] != len(contents):
            _integrity_invalid()
        if not _match(LOWER_DIGEST, entry["sha256"]) or entry["sha256"] != _digest(contents):
            _integrity_invalid()
        if entry["mediaType"] not in MEDIA_TYPES or entry["mediaType"] != _media_type(path):
            _integrity_invalid()
        expected_paths.append(path)
    if expected_paths != sorted(expected_paths, key=lambda item: item.encode()) or len(set(expected_paths)) != len(expected_paths):
        _integrity_invalid()
    actual_paths = sorted((path for path in files if path != "integrity.json"), key=lambda item: item.encode())
    if expected_paths != actual_paths:
        _integrity_invalid()
    validate_provider_manifest(manifest_value)
    if manifest_value["providerApiVersion"] != SUPPORTED_PROVIDER_API_VERSION:
        raise ValueError("RUNTIME_PROVIDER_API_UNSUPPORTED")
    declared_assets = {
        path
        for target in manifest_value["targets"]
        for path in target["assetPaths"]
    }
    bundled_assets = {path for path in files if path.startswith("assets/")}
    if declared_assets != bundled_assets or manifest_value["clientModulePath"] not in files or \
            "provenance.json" not in files or not any(path.startswith("licenses/") for path in files):
        raise ValueError("PROVIDER_MANIFEST_ASSET_CLOSURE_INVALID")
    try:
        provenance = json.loads(files["provenance.json"])
    except (UnicodeError, json.JSONDecodeError) as error:
        raise ValueError("PROVIDER_PROVENANCE_INVALID") from error
    if not isinstance(provenance, dict) or provenance.get("schemaVersion") != 1:
        raise ValueError("PROVIDER_PROVENANCE_INVALID")
    module_entry = next((entry for entry in entries if entry["path"] == manifest_value["clientModulePath"]), None)
    if module_entry is None or module_entry["mediaType"] != "text/javascript; charset=utf-8":
        raise ValueError("PROVIDER_MODULE_INVALID")
    if manifest_value["providerId"] != lock["providerId"] or manifest_value["providerVersion"] != lock["providerVersion"]:
        raise ValueError("PROVIDER_MANIFEST_IDENTITY_INVALID")
    if _digest(manifest_bytes) != lock["manifestSha256"]:
        raise ValueError("PROVIDER_MANIFEST_DIGEST_INVALID")


def _collect_files(root: Path, allow_proof: bool) -> dict[str, bytes]:
    result: dict[str, bytes] = {}
    for path in root.rglob("*"):
        if path.is_symlink() or not (path.is_dir() or path.is_file()):
            raise ValueError("PROVIDER_BUNDLE_UNSAFE")
        if not path.is_file():
            continue
        relative = path.relative_to(root).as_posix()
        if allow_proof and relative == ".installation.json":
            continue
        if not _safe_path(relative) or relative in result:
            raise ValueError("PROVIDER_BUNDLE_UNSAFE")
        result[relative] = path.read_bytes()
    return result


def _verify_existing(destination: Path, lock: dict[str, Any]) -> None:
    try:
        proof = json.loads((destination / ".installation.json").read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ValueError("PROVIDER_INSTALLATION_INVALID") from error
    if proof != _installation_proof(lock):
        raise ValueError("PROVIDER_INSTALLATION_INVALID")


def _validate_install_record(value: Any) -> dict[str, Any]:
    if isinstance(value, dict) and set(value) == LOCK_KEYS:
        return validate_provider_lock(value)
    return validate_provider_build_record(value)


def _installation_proof(lock: dict[str, Any]) -> dict[str, Any]:
    return {
        "bundleSha256": lock["bundleSha256"],
        "fileCount": lock["fileCount"],
        "manifestSha256": lock["manifestSha256"],
        "providerId": lock["providerId"],
        "providerVersion": lock["providerVersion"],
        "schemaVersion": 1,
        "unpackedSizeBytes": lock["unpackedSizeBytes"],
    }


def _media_type(path: str) -> str:
    if path.endswith((".js", ".mjs")):
        return "text/javascript; charset=utf-8"
    if path.endswith(".css"):
        return "text/css; charset=utf-8"
    if path.endswith(".json"):
        return "application/json; charset=utf-8"
    if path.endswith(".wasm"):
        return "application/wasm"
    if path.endswith(".zip"):
        return "application/zip"
    if path.endswith(".7z"):
        return "application/x-7z-compressed"
    if path.endswith(".png"):
        return "image/png"
    if path.endswith((".jpg", ".jpeg")):
        return "image/jpeg"
    if path.endswith(".gif"):
        return "image/gif"
    if path.endswith(".webp"):
        return "image/webp"
    if path.endswith(".svg"):
        return "image/svg+xml"
    if path.endswith(".ico"):
        return "image/x-icon"
    if path.endswith(".ogg"):
        return "audio/ogg"
    if path.endswith(".mp3"):
        return "audio/mpeg"
    if path.endswith(".wav"):
        return "audio/wav"
    if path.endswith(".woff"):
        return "font/woff"
    if path.endswith(".woff2"):
        return "font/woff2"
    if path.endswith((".md", ".rb", ".txt")) or PurePosixPath(path).name in {"LICENSE", "COPYING"}:
        return "text/plain; charset=utf-8"
    return "application/octet-stream"


def _safe_path(value: Any) -> bool:
    if not isinstance(value, str) or not value or "\\" in value or "\0" in value or "?" in value or "#" in value:
        return False
    path = PurePosixPath(value)
    return not path.is_absolute() and all(part not in {"", ".", ".."} for part in path.parts)


def _https_url(value: Any) -> bool:
    return isinstance(value, str) and value.startswith("https://") and len(value) > 8


def _positive_integer(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and value > 0


def _bounded_integer(value: Any, *, minimum: int, maximum: int) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and minimum <= value <= maximum


def _nonnegative_integer(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and value >= 0


def _match(pattern: re.Pattern[str], value: Any) -> bool:
    return isinstance(value, str) and pattern.fullmatch(value) is not None


def _write_json(path: Path, value: Any) -> None:
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def _digest(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def _canonical_json(value: Any) -> bytes:
    def encode(item: Any) -> str:
        if item is None:
            return "null"
        if isinstance(item, bool):
            return "true" if item else "false"
        if isinstance(item, str):
            return json.dumps(item, ensure_ascii=False, separators=(",", ":"))
        if isinstance(item, int) and not isinstance(item, bool) and abs(item) <= MAX_SAFE_INTEGER:
            return str(item)
        if isinstance(item, list):
            return "[" + ",".join(encode(entry) for entry in item) + "]"
        if isinstance(item, dict):
            keys = sorted(item, key=lambda key: key.encode("utf-16-be", "surrogatepass"))
            return "{" + ",".join(f"{encode(key)}:{encode(item[key])}" for key in keys) + "}"
        raise ValueError("PROVIDER_CANONICAL_JSON_INVALID")
    return encode(value).encode("utf-8")


def _lock_invalid() -> None:
    raise ValueError("PROVIDER_LOCK_INVALID")


def _integrity_invalid() -> None:
    raise ValueError("PROVIDER_INTEGRITY_INVALID")
