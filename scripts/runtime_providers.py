#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import re
from pathlib import Path, PurePosixPath
from typing import Any

from runtime_provider_bundle import install_provider_bundle


REPOSITORY = "https://github.com/retrom-project/retrom-runtime"
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
) -> dict[str, Any]:
    """Verify and install local release artifacts without contacting the network."""
    candidate_root = candidate_root.resolve(strict=True)
    metadata = _load_release_metadata(candidate_root / "providers" / "provider-release.json")
    release = metadata["release"]
    active_providers: list[dict[str, Any]] = []
    for provider in metadata["providers"]:
        lock = _candidate_lock(provider, commit=release["commit"], tag=release["tag"])
        archive = _resolve_candidate_archive(candidate_root / "providers", provider["archive"])
        installed = install_provider_bundle(archive, lock, installed_root)
        manifest = _load_json(installed / "provider.json", "PROVIDER_MANIFEST_INVALID")
        active_providers.append({
            "bundleSha256": lock["bundleSha256"],
            "clientModulePath": manifest["clientModulePath"],
            "manifestSha256": lock["manifestSha256"],
            "providerApiVersion": manifest["providerApiVersion"],
            "providerId": lock["providerId"],
            "providerVersion": lock["providerVersion"],
            "targets": [target["id"] for target in manifest["targets"]],
        })
    return {
        "providers": sorted(active_providers, key=lambda item: item["providerId"]),
        "release": dict(release),
        "schemaVersion": 1,
        "source": "candidate",
    }


def _load_release_metadata(path: Path) -> dict[str, Any]:
    value = _load_json(path, "PROVIDER_RELEASE_METADATA_INVALID")
    if not isinstance(value, dict) or set(value) != RELEASE_KEYS or value["schemaVersion"] != 1:
        raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
    providers = value["providers"]
    release = value["release"]
    if not isinstance(release, dict) or set(release) != RELEASE_IDENTITY_KEYS or \
        release["repository"] != REPOSITORY or not isinstance(release["commit"], str) or \
        not LOWER_COMMIT.fullmatch(release["commit"]) or not isinstance(release["tag"], str) or \
        not RELEASE_TAG.fullmatch(release["tag"]):
        raise ValueError("PROVIDER_RELEASE_METADATA_INVALID")
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


def _candidate_lock(provider: dict[str, Any], *, commit: str, tag: str) -> dict[str, Any]:
    return {
        "bundleSha256": provider["bundleSha256"],
        "bundleSizeBytes": provider["bundleSizeBytes"],
        "bundleUrl": f"{REPOSITORY}/releases/download/{tag}/{PurePosixPath(provider['archive']).name}",
        "commit": commit,
        "fileCount": provider["fileCount"],
        "manifestSha256": provider["manifestSha256"],
        "providerId": provider["providerId"],
        "providerVersion": provider["providerVersion"],
        "repository": REPOSITORY,
        "schemaVersion": 1,
        "tag": tag,
        "unpackedSizeBytes": provider["unpackedSizeBytes"],
    }


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


def main() -> int:
    parser = argparse.ArgumentParser(description="Manage immutable Retrom runtime Provider bundles")
    subcommands = parser.add_subparsers(dest="command", required=True)
    prepare = subcommands.add_parser("prepare-candidate")
    prepare.add_argument("--candidate-root", type=Path, required=True)
    prepare.add_argument("--installed-root", type=Path, required=True)
    arguments = parser.parse_args()
    if arguments.command == "prepare-candidate":
        result = prepare_candidate_providers(
            arguments.candidate_root,
            arguments.installed_root,
        )
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return 0
    raise AssertionError(arguments.command)


if __name__ == "__main__":
    raise SystemExit(main())
