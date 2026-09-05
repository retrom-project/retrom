#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import tempfile
import urllib.request
from pathlib import Path, PurePosixPath
from typing import Any

if __package__:
    from scripts.runtime_provider_bundle import (
        describe_installed_provider,
        install_provider_bundle,
        load_provider_lock,
        validate_provider_lock,
    )
else:
    from runtime_provider_bundle import (
        describe_installed_provider,
        install_provider_bundle,
        load_provider_lock,
        validate_provider_lock,
    )


REPOSITORY = "https://github.com/retrom-project/retrom-runtime"
BUILD_KEYS = {"schemaVersion", "sourceTreeSha256", "providers"}
RELEASE_KEYS = {"schemaVersion", "release", "providers"}
RELEASE_IDENTITY_KEYS = {"repository", "tag", "commit"}
PROVIDER_KEYS = {
    "archive", "bundleDirectory", "bundleSha256", "bundleSizeBytes", "fileCount",
    "manifestSha256", "providerId", "providerVersion", "unpackedSizeBytes",
}
PROVIDER_ID = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$")
SEMVER = re.compile(
    r"^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
    r"(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
    r"(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?$"
)
LOWER_DIGEST = re.compile(r"^[0-9a-f]{64}$")
LOWER_COMMIT = re.compile(r"^[0-9a-f]{40}$")
RELEASE_TAG = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+$")


def prepare_candidate_providers(
    candidate_root: Path,
    installed_root: Path,
    active_path: Path | None = None,
) -> dict[str, Any]:
    """Verify and install local release artifacts without contacting the network."""
    candidate_root = candidate_root.resolve(strict=True)
    metadata = _load_build_metadata(candidate_root / "providers" / "provider-build.json")
    active_providers: list[dict[str, Any]] = []
    for provider in metadata["providers"]:
        archive = _resolve_candidate_archive(candidate_root / "providers", provider["archive"])
        install_provider_bundle(archive, provider, installed_root)
        active_providers.append(describe_installed_provider(provider, installed_root))
    active = {
        "providers": sorted(active_providers, key=lambda item: item["providerId"]),
        "release": None,
        "schemaVersion": 1,
        "source": "candidate",
        "sourceTreeSha256": metadata["sourceTreeSha256"],
    }
    if active_path is not None:
        if active_path.exists():
            existing = _load_json(active_path, "RUNTIME_PROVIDER_ACTIVE_INVALID")
            if existing.get("source") == "production":
                raise ValueError("RUNTIME_PROVIDER_PRODUCTION_FORBIDDEN")
            verify_provider_upgrade(existing, active, [])
        _write_json_atomic(active_path, active)
    return active


def pin_provider_release(release_root: Path, lock_root: Path) -> list[dict[str, Any]]:
    release_root = release_root.resolve(strict=True)
    metadata = _load_release_metadata(release_root / "providers" / "provider-release.json")
    release = metadata["release"]
    locks = []
    for provider in metadata["providers"]:
        archive = _resolve_candidate_archive(release_root / "providers", provider["archive"])
        contents = archive.read_bytes()
        if len(contents) != provider["bundleSizeBytes"] or _digest(contents) != provider["bundleSha256"]:
            raise ValueError("PROVIDER_RELEASE_ARCHIVE_INVALID")
        lock = {
            "bundleSha256": provider["bundleSha256"],
            "bundleSizeBytes": provider["bundleSizeBytes"],
            "bundleUrl": f'{release["repository"]}/releases/download/{release["tag"]}/{archive.name}',
            "commit": release["commit"],
            "fileCount": provider["fileCount"],
            "manifestSha256": provider["manifestSha256"],
            "providerId": provider["providerId"],
            "providerVersion": provider["providerVersion"],
            "repository": release["repository"],
            "schemaVersion": 1,
            "tag": release["tag"],
            "unpackedSizeBytes": provider["unpackedSizeBytes"],
        }
        load_provider_lock_value = _validate_lock_round_trip(lock)
        locks.append(load_provider_lock_value)
    lock_root.mkdir(parents=True, exist_ok=True)
    for lock in locks:
        _write_json_atomic(lock_root / f'{lock["providerId"]}.lock.json', lock)
    return sorted(locks, key=lambda item: item["providerId"])


def prepare_production_providers(
    lock_root: Path,
    cache_root: Path,
    installed_root: Path,
    active_path: Path,
    fetch_bytes=None,
) -> dict[str, Any]:
    if active_path.exists():
        existing = _load_json(active_path, "RUNTIME_PROVIDER_ACTIVE_INVALID")
        if existing.get("source") == "candidate":
            raise ValueError("RUNTIME_PROVIDER_CANDIDATE_FORBIDDEN")
    lock_paths = sorted(lock_root.resolve(strict=True).glob("*.lock.json"))
    if not lock_paths:
        raise ValueError("PROVIDER_LOCK_INVALID")
    locks = [load_provider_lock(path) for path in lock_paths]
    releases = {(lock["repository"], lock["tag"], lock["commit"]) for lock in locks}
    if len(releases) != 1 or len({lock["providerId"] for lock in locks}) != len(locks):
        raise ValueError("PROVIDER_LOCK_INVALID")
    repository, tag, commit = next(iter(releases))
    cache_root.mkdir(parents=True, exist_ok=True)
    active_providers = []
    downloader = fetch_bytes or _fetch_bytes
    for lock in locks:
        cache_path = cache_root / lock["providerId"] / f'{lock["bundleSha256"]}.tar.gz'
        if cache_path.exists():
            _verify_archive_bytes(cache_path.read_bytes(), lock)
        else:
            contents = downloader(lock["bundleUrl"], lock["bundleSizeBytes"])
            _verify_archive_bytes(contents, lock)
            cache_path.parent.mkdir(parents=True, exist_ok=True)
            _write_bytes_atomic(cache_path, contents)
        install_provider_bundle(cache_path, lock, installed_root)
        active_providers.append(describe_installed_provider(lock, installed_root))
    active = {
        "providers": sorted(active_providers, key=lambda item: item["providerId"]),
        "release": {"commit": commit, "repository": repository, "tag": tag},
        "schemaVersion": 1,
        "source": "production",
        "sourceTreeSha256": None,
    }
    if active_path.exists():
        verify_provider_upgrade(existing, active, [])
    _write_json_atomic(active_path, active)
    return active


def check_active_providers(active_path: Path, installed_root: Path, expected_source: str) -> dict[str, Any]:
    active = _load_json(active_path, "RUNTIME_PROVIDER_ACTIVE_INVALID")
    if set(active) != {"providers", "release", "schemaVersion", "source", "sourceTreeSha256"} or \
            active["schemaVersion"] != 1 or active["source"] != expected_source or \
            expected_source not in {"candidate", "production"}:
        raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
    if expected_source == "candidate":
        if active["release"] is not None or not _match(LOWER_DIGEST, active["sourceTreeSha256"]):
            raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
    elif not _valid_release_identity(active["release"]) or active["sourceTreeSha256"] is not None:
        raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
    providers = _active_providers(active)
    for provider in providers.values():
        record = {
            "archive": f'{provider["providerId"]}/{provider["providerId"]}-provider-{provider["providerVersion"]}.tar.gz',
            "bundleDirectory": f'{provider["providerId"]}/{provider["providerId"]}-{provider["providerVersion"]}',
            **{key: provider[key] for key in (
                "bundleSha256", "bundleSizeBytes", "fileCount", "manifestSha256",
                "providerId", "providerVersion", "unpackedSizeBytes",
            )},
        }
        if describe_installed_provider(record, installed_root) != provider:
            raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
    return active


def _load_build_metadata(path: Path) -> dict[str, Any]:
    value = _load_json(path, "PROVIDER_RELEASE_METADATA_INVALID")
    if not isinstance(value, dict) or set(value) != BUILD_KEYS or value["schemaVersion"] != 1 or \
            not isinstance(value["sourceTreeSha256"], str) or not LOWER_DIGEST.fullmatch(value["sourceTreeSha256"]):
        raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
    providers = value["providers"]
    if not isinstance(providers, list) or not providers:
        raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
    identities: set[str] = set()
    for provider in providers:
        if not isinstance(provider, dict) or set(provider) != PROVIDER_KEYS:
            raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
        provider_id = provider["providerId"]
        version = provider["providerVersion"]
        archive = provider["archive"]
        if not isinstance(provider_id, str) or not PROVIDER_ID.fullmatch(provider_id):
            raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
        if provider_id in identities or not isinstance(version, str) or not SEMVER.fullmatch(version):
            raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
        identities.add(provider_id)
        expected_archive = f"{provider_id}/{provider_id}-provider-{version}.tar.gz"
        expected_directory = f"{provider_id}/{provider_id}-{version}"
        if archive != expected_archive or provider["bundleDirectory"] != expected_directory:
            raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
        if not _safe_relative_path(archive):
            raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
        for key in ("bundleSha256", "manifestSha256"):
            if not isinstance(provider[key], str) or not LOWER_DIGEST.fullmatch(provider[key]):
                raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
        for key in ("bundleSizeBytes", "unpackedSizeBytes", "fileCount"):
            if not _positive_safe_integer(provider[key]):
                raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
    return value


def _load_release_metadata(path: Path) -> dict[str, Any]:
    value = _load_json(path, "PROVIDER_RELEASE_METADATA_INVALID")
    if not isinstance(value, dict) or set(value) != RELEASE_KEYS or value["schemaVersion"] != 1 or \
            not _valid_release_identity(value["release"]):
        raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
    _validate_provider_records(value.get("providers"))
    return value


def _validate_provider_records(providers: Any) -> None:
    if not isinstance(providers, list) or not providers:
        raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
    identities: set[str] = set()
    for provider in providers:
        if not isinstance(provider, dict) or set(provider) != PROVIDER_KEYS:
            raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
        provider_id = provider["providerId"]
        version = provider["providerVersion"]
        archive = provider["archive"]
        if not _match(PROVIDER_ID, provider_id) or provider_id in identities or not _match(SEMVER, version):
            raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
        identities.add(provider_id)
        if archive != f"{provider_id}/{provider_id}-provider-{version}.tar.gz" or \
                provider["bundleDirectory"] != f"{provider_id}/{provider_id}-{version}" or \
                not _safe_relative_path(archive):
            raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
        if any(not _match(LOWER_DIGEST, provider[key]) for key in ("bundleSha256", "manifestSha256")) or \
                any(not _positive_safe_integer(provider[key]) for key in (
                    "bundleSizeBytes", "unpackedSizeBytes", "fileCount",
                )):
            raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")


def verify_provider_upgrade(
    current: dict[str, Any],
    candidate: dict[str, Any],
    checkpoint_references: list[dict[str, str]],
) -> None:
    current_by_id = _active_providers(current)
    candidate_by_id = _active_providers(candidate)
    for provider_id, previous in current_by_id.items():
        proposed = candidate_by_id.get(provider_id)
        if proposed is None:
            raise ValueError("RUNTIME_PROVIDER_REFERENCED_TARGET_REMOVED")
        ordering = _compare_semver(proposed["providerVersion"], previous["providerVersion"])
        if ordering < 0:
            raise ValueError("RUNTIME_PROVIDER_DOWNGRADE_FORBIDDEN")
        if ordering == 0 and proposed["bundleSha256"] != previous["bundleSha256"]:
            raise ValueError("RUNTIME_PROVIDER_VERSION_REBUILT")
    for reference in checkpoint_references:
        if not isinstance(reference, dict) or set(reference) != {"format", "providerId", "targetId"}:
            raise ValueError("RUNTIME_PROVIDER_CHECKPOINT_REFERENCE_INVALID")
        provider = candidate_by_id.get(reference["providerId"])
        target = next((item for item in provider["targets"] if item["id"] == reference["targetId"]), None) \
            if provider else None
        checkpoint = target.get("checkpoint") if target else None
        if not checkpoint or reference["format"] not in checkpoint["readFormats"]:
            raise ValueError("RUNTIME_PROVIDER_CHECKPOINT_FORMAT_UNREADABLE")


def _active_providers(value: dict[str, Any]) -> dict[str, dict[str, Any]]:
    providers = value.get("providers") if isinstance(value, dict) else None
    if not isinstance(providers, list) or not providers:
        raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
    result: dict[str, dict[str, Any]] = {}
    for provider in providers:
        if not isinstance(provider, dict) or not _match(PROVIDER_ID, provider.get("providerId")) or \
                not _match(SEMVER, provider.get("providerVersion")) or \
                not _match(LOWER_DIGEST, provider.get("bundleSha256")) or not isinstance(provider.get("targets"), list):
            raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
        if provider["providerId"] in result:
            raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
        result[provider["providerId"]] = provider
    return result


def _compare_semver(left: str, right: str) -> int:
    def parse(value: str) -> tuple[tuple[int, int, int], list[str] | None]:
        match = SEMVER.fullmatch(value)
        if not match:
            raise ValueError("RUNTIME_PROVIDER_ACTIVE_INVALID")
        main, separator, prerelease = value.partition("-")
        return tuple(int(part) for part in main.split(".")), prerelease.split(".") if separator else None

    left_main, left_pre = parse(left)
    right_main, right_pre = parse(right)
    if left_main != right_main:
        return 1 if left_main > right_main else -1
    if left_pre is None or right_pre is None:
        return 0 if left_pre is right_pre else 1 if left_pre is None else -1
    for left_part, right_part in zip(left_pre, right_pre):
        if left_part == right_part:
            continue
        if left_part.isdigit() and right_part.isdigit():
            return 1 if int(left_part) > int(right_part) else -1
        if left_part.isdigit() != right_part.isdigit():
            return -1 if left_part.isdigit() else 1
        return 1 if left_part > right_part else -1
    return (len(left_pre) > len(right_pre)) - (len(left_pre) < len(right_pre))


def _match(pattern: re.Pattern[str], value: Any) -> bool:
    return isinstance(value, str) and pattern.fullmatch(value) is not None


def _resolve_candidate_archive(provider_root: Path, relative: str) -> Path:
    archive = (provider_root / relative).resolve(strict=True)
    try:
        archive.relative_to(provider_root.resolve(strict=True))
    except ValueError as error:
        raise ValueError("PROVIDER_RELEASE_METADATA_INVALID") from error
    if not archive.is_file():
        raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
    return archive


def _load_json(path: Path, code: str) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ValueError(code) from error


def _safe_relative_path(value: Any) -> bool:
    if not isinstance(value, str) or not value or "\\" in value or "\0" in value:
        return False
    path = PurePosixPath(value)
    return not path.is_absolute() and all(part not in {"", ".", ".."} for part in path.parts)


def _positive_safe_integer(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and 0 < value <= 9_007_199_254_740_991


def _valid_release_identity(value: Any) -> bool:
    return isinstance(value, dict) and set(value) == RELEASE_IDENTITY_KEYS and \
        value["repository"] == REPOSITORY and _match(LOWER_COMMIT, value["commit"]) and \
        _match(RELEASE_TAG, value["tag"])


def _validate_lock_round_trip(value: dict[str, Any]) -> dict[str, Any]:
    return validate_provider_lock(value)


def _verify_archive_bytes(contents: Any, lock: dict[str, Any]) -> None:
    if not isinstance(contents, bytes) or len(contents) != lock["bundleSizeBytes"] or \
            _digest(contents) != lock["bundleSha256"]:
        raise ValueError("PROVIDER_BUNDLE_DIGEST_INVALID")


def _fetch_bytes(url: str, maximum: int) -> bytes:
    request = urllib.request.Request(url, headers={"User-Agent": "retrom-runtime-provider"})
    try:
        with urllib.request.urlopen(request, timeout=60) as response:  # noqa: S310 -- HTTPS validated above.
            final_url = response.geturl()
            if not isinstance(final_url, str) or not final_url.startswith("https://"):
                raise ValueError("PROVIDER_DOWNLOAD_INVALID")
            contents = response.read(maximum + 1)
    except (OSError, ValueError) as error:
        raise ValueError("PROVIDER_DOWNLOAD_INVALID") from error
    if not contents or len(contents) > maximum:
        raise ValueError("PROVIDER_DOWNLOAD_INVALID")
    return contents


def _write_json_atomic(path: Path, value: Any) -> None:
    _write_bytes_atomic(path, (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode("utf-8"))


def _write_bytes_atomic(path: Path, contents: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "wb") as output:
            output.write(contents)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
    finally:
        if temporary.exists():
            temporary.unlink()


def _digest(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def main() -> int:
    parser = argparse.ArgumentParser(description="Manage immutable Retrom runtime Provider bundles")
    subcommands = parser.add_subparsers(dest="command", required=True)
    prepare = subcommands.add_parser("prepare-candidate")
    prepare.add_argument("--candidate-root", type=Path, required=True)
    prepare.add_argument("--installed-root", type=Path, required=True)
    prepare.add_argument("--active-path", type=Path, required=True)
    production = subcommands.add_parser("prepare")
    production.add_argument("--lock-root", type=Path, required=True)
    production.add_argument("--cache-root", type=Path, required=True)
    production.add_argument("--installed-root", type=Path, required=True)
    production.add_argument("--active-path", type=Path, required=True)
    check = subcommands.add_parser("check")
    check.add_argument("--active-path", type=Path, required=True)
    check.add_argument("--installed-root", type=Path, required=True)
    check.add_argument("--source", choices=("candidate", "production"), required=True)
    pin = subcommands.add_parser("pin-release")
    pin.add_argument("--release-root", type=Path, required=True)
    pin.add_argument("--lock-root", type=Path, required=True)
    upgrade = subcommands.add_parser("verify-upgrade")
    upgrade.add_argument("--current", type=Path, required=True)
    upgrade.add_argument("--candidate", type=Path, required=True)
    upgrade.add_argument("--checkpoint-references", type=Path, required=True)
    arguments = parser.parse_args()
    if arguments.command == "prepare-candidate":
        result = prepare_candidate_providers(
            arguments.candidate_root,
            arguments.installed_root,
            arguments.active_path,
        )
    elif arguments.command == "prepare":
        result = prepare_production_providers(
            arguments.lock_root, arguments.cache_root, arguments.installed_root, arguments.active_path,
        )
    elif arguments.command == "check":
        result = check_active_providers(arguments.active_path, arguments.installed_root, arguments.source)
    elif arguments.command == "pin-release":
        result = pin_provider_release(arguments.release_root, arguments.lock_root)
    elif arguments.command == "verify-upgrade":
        current = _load_json(arguments.current, "RUNTIME_PROVIDER_ACTIVE_INVALID")
        candidate = _load_json(arguments.candidate, "RUNTIME_PROVIDER_ACTIVE_INVALID")
        references = _load_json(arguments.checkpoint_references, "RUNTIME_PROVIDER_CHECKPOINT_REFERENCE_INVALID")
        if not isinstance(references, list):
            raise ValueError("RUNTIME_PROVIDER_CHECKPOINT_REFERENCE_INVALID")
        verify_provider_upgrade(current, candidate, references)
        result = {"status": "ok"}
    else:
        raise AssertionError(arguments.command)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
