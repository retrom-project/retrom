"""Validate and materialize tag-pinned RPG runtime release assets."""

from __future__ import annotations

import hashlib
import json
import os
import re
import stat
import tempfile
import urllib.request
from pathlib import Path, PurePosixPath
from typing import Any
from urllib.parse import urljoin, urlparse


OBSERVED_FILENAME = ".release-assets-observed.json"
OBSERVED_SCHEMA_VERSION = 1
COMMIT = re.compile(r"^[0-9a-f]{40}$")
TAG = re.compile(r"^retrom-web-[0-9A-Za-z.-]+-r[1-9][0-9]*$")
HEX_DIGEST = re.compile(r"^[0-9a-f]{64}$")
ALLOWED_RELEASES = {
    "easyrpg": ("https://github.com/xxxsen/Player", "easyrpg-save-v1"),
    "easyrpg-r3": ("https://github.com/xxxsen/Player", "easyrpg-save-v1"),
    "mkxp": (
        "https://github.com/xxxsen/mkxp-z-libretro-emscripten",
        "mkxp-state-v1",
    ),
}
MKXP_SOURCE_COMMITS = {
    "mkxp-z": "f2efc98a344c505a66820e06d6508092719b8dd2",
    "retroarch": "69a4f0ea1e8aaf442ae4858f2e7f2b31a1776576",
}


class ReleaseAssetError(RuntimeError):
    """A stable fail-closed release-asset error."""


class HTTPSRedirectHandler(urllib.request.HTTPRedirectHandler):
    def __init__(self) -> None:
        self.redirects = 0

    def redirect_request(
        self,
        request: Any,
        file_pointer: Any,
        code: int,
        message: str,
        headers: Any,
        new_url: str,
    ) -> Any:
        self.redirects += 1
        resolved = urljoin(request.full_url, new_url)
        if (
            self.redirects > 3
            or urlparse(request.full_url).scheme != "https"
            or urlparse(resolved).scheme != "https"
        ):
            raise ReleaseAssetError("RPG_RUNTIME_RELEASE_REDIRECT_INVALID")
        return super().redirect_request(
            request, file_pointer, code, message, headers, resolved
        )


def safe_path(value: object) -> PurePosixPath:
    if not isinstance(value, str):
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_PATH_INVALID")
    path = PurePosixPath(value)
    if path.is_absolute() or str(path) != value or any(
        part in ("", ".", "..") for part in path.parts
    ):
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_PATH_INVALID")
    return path


def validate(releases: object) -> dict[str, dict[str, Any]]:
    if not isinstance(releases, list) or len(releases) != len(ALLOWED_RELEASES):
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_DECLARATION_INVALID")
    result: dict[str, dict[str, Any]] = {}
    runtime_paths: set[str] = set()
    for release in releases:
        validate_release(release, result, runtime_paths)
    if set(result) != set(ALLOWED_RELEASES):
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_DECLARATION_INVALID")
    return result


def validate_release(
    release: object,
    releases: dict[str, dict[str, Any]],
    runtime_paths: set[str],
) -> None:
    required = {
        "id", "repository", "tag", "tag_commit", "adapter_abi",
        "metadata_asset", "assets", "binary_association",
    }
    if not isinstance(release, dict) or set(release) != required:
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_DECLARATION_INVALID")
    release_id = release.get("id")
    expected = ALLOWED_RELEASES.get(release_id)
    if (
        expected is None
        or release_id in releases
        or release.get("repository") != expected[0]
        or release.get("adapter_abi") != expected[1]
        or release.get("binary_association") != "TAGGED_RELEASE_COMPATIBLE"
        or not isinstance(release.get("tag"), str)
        or TAG.fullmatch(release["tag"]) is None
        or not isinstance(release.get("tag_commit"), str)
        or COMMIT.fullmatch(release["tag_commit"]) is None
    ):
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_DECLARATION_INVALID")
    validate_metadata(release)
    assets = release.get("assets")
    if not isinstance(assets, list) or len(assets) != 2:
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_DECLARATION_INVALID")
    filenames: set[str] = set()
    for asset in assets:
        validate_asset(release, asset, filenames, runtime_paths)
    releases[release_id] = release


def validate_metadata(release: dict[str, Any]) -> None:
    metadata = release.get("metadata_asset")
    if not isinstance(metadata, dict) or set(metadata) != {
        "filename", "url", "max_size_bytes"
    }:
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_DECLARATION_INVALID")
    if (
        metadata.get("filename") != "retrom-runtime-release.json"
        or metadata.get("url") != release_url(release, metadata["filename"])
        or metadata.get("max_size_bytes") != 65536
    ):
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_DECLARATION_INVALID")


def validate_asset(
    release: dict[str, Any],
    asset: object,
    filenames: set[str],
    runtime_paths: set[str],
) -> None:
    if not isinstance(asset, dict) or set(asset) != {
        "filename", "url", "path_in_release", "role", "max_size_bytes"
    }:
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_DECLARATION_INVALID")
    filename = asset.get("filename")
    runtime_path = str(safe_path(asset.get("path_in_release")))
    role = asset.get("role")
    if (
        not isinstance(filename, str)
        or filename != PurePosixPath(filename).name
        or filename in filenames
        or runtime_path in runtime_paths
        or asset.get("url") != release_url(release, filename)
        or role not in {"runtime_js", "runtime_wasm"}
        or not isinstance(asset.get("max_size_bytes"), int)
        or asset["max_size_bytes"] < 1
        or asset["max_size_bytes"] > 128 * 1024 * 1024
        or "sha256" in asset
        or "size_bytes" in asset
    ):
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_DECLARATION_INVALID")
    filenames.add(filename)
    runtime_paths.add(runtime_path)


def release_url(release: dict[str, Any], filename: str) -> str:
    return f"{release['repository']}/releases/download/{release['tag']}/{filename}"


def materialize(releases: object, runtime_root: Path, offline: bool) -> None:
    declarations = validate(releases)
    observed = load_observed(runtime_root)
    updated: dict[str, Any] = {"schema_version": OBSERVED_SCHEMA_VERSION, "releases": {}}
    for release_id, release in declarations.items():
        prior = observed.get("releases", {}).get(release_id)
        updated["releases"][release_id] = materialize_release(
            release, runtime_root, prior, offline
        )
    atomic_write(
        runtime_root / OBSERVED_FILENAME,
        json.dumps(updated, sort_keys=True, separators=(",", ":")).encode() + b"\n",
    )


def verify(releases: object, runtime_root: Path) -> dict[str, tuple[int, str, str]]:
    declarations = validate(releases)
    observed = load_observed(runtime_root)
    if observed.get("schema_version") != OBSERVED_SCHEMA_VERSION:
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_OBSERVED_INVALID")
    result: dict[str, tuple[int, str, str]] = {}
    for release_id, release in declarations.items():
        record = observed.get("releases", {}).get(release_id)
        validate_observed_identity(release, record)
        assets = record.get("assets") if isinstance(record, dict) else None
        if not isinstance(assets, dict):
            raise ReleaseAssetError("RPG_RUNTIME_RELEASE_OBSERVED_INVALID")
        for asset in release["assets"]:
            path = asset["path_in_release"]
            item = assets.get(asset["filename"])
            size, digest = verify_observed_file(runtime_root / path, item, asset)
            result[path] = (size, digest, asset["role"])
    return result


def materialize_release(
    release: dict[str, Any],
    runtime_root: Path,
    prior: object,
    offline: bool,
) -> dict[str, Any]:
    try:
        validate_observed_identity(release, prior)
        assets = prior["assets"]
        for asset in release["assets"]:
            verify_observed_file(
                runtime_root / asset["path_in_release"],
                assets.get(asset["filename"]),
                asset,
            )
        return prior
    except (ReleaseAssetError, KeyError, TypeError):
        if offline:
            first = release["assets"][0]["path_in_release"]
            raise ReleaseAssetError(f"RPG_RUNTIME_RELEASE_ASSET_REQUIRED:{first}")
    metadata = download_bytes(release["metadata_asset"])
    validate_release_metadata(release, metadata)
    records: dict[str, dict[str, Any]] = {}
    downloads: list[tuple[dict[str, Any], bytes]] = []
    for asset in release["assets"]:
        contents = download_bytes(asset)
        downloads.append((asset, contents))
        records[asset["filename"]] = observed_file(contents)
    for asset, contents in downloads:
        atomic_write(runtime_root / asset["path_in_release"], contents)
    return observed_identity(release) | {"assets": records}


def validate_release_metadata(release: dict[str, Any], contents: bytes) -> None:
    try:
        value = json.loads(contents)
    except json.JSONDecodeError as exc:
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_METADATA_INVALID") from exc
    expected_keys = {
        "adapterAbi", "assets", "commit", "digestPolicy", "repository",
        "schemaVersion", "tag",
    }
    if release["id"] == "mkxp":
        expected_keys.add("sourceCommits")
    if not isinstance(value, dict) or set(value) != expected_keys:
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_METADATA_INVALID")
    assets = value.get("assets")
    declared = {asset["filename"] for asset in release["assets"]}
    metadata_names = validate_metadata_assets(assets)
    if (
        value.get("schemaVersion") != 1
        or value.get("repository") != release["repository"]
        or value.get("tag") != release["tag"]
        or value.get("commit") != release["tag_commit"]
        or value.get("adapterAbi") != release["adapter_abi"]
        or value.get("digestPolicy") != "OBSERVED_CACHE_INTEGRITY_ONLY"
        or metadata_names != declared
        or (
            release["id"] == "mkxp"
            and value.get("sourceCommits") != MKXP_SOURCE_COMMITS
        )
    ):
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_METADATA_INVALID")


def validate_metadata_assets(assets: object) -> set[str]:
    if not isinstance(assets, list) or len(assets) != 2:
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_METADATA_INVALID")
    names: set[str] = set()
    for item in assets:
        if (
            not isinstance(item, dict)
            or set(item) != {"filename", "observedSha256", "sizeBytes"}
            or not isinstance(item.get("filename"), str)
            or item["filename"] in names
            or not isinstance(item.get("sizeBytes"), int)
            or item["sizeBytes"] < 1
            or not isinstance(item.get("observedSha256"), str)
            or HEX_DIGEST.fullmatch(item["observedSha256"]) is None
        ):
            raise ReleaseAssetError("RPG_RUNTIME_RELEASE_METADATA_INVALID")
        names.add(item["filename"])
    return names


def download_bytes(asset: dict[str, Any]) -> bytes:
    request = urllib.request.Request(
        asset["url"], headers={"User-Agent": "retrom-rpg-runtime-release/1"}
    )
    opener = urllib.request.build_opener(HTTPSRedirectHandler())
    maximum = asset["max_size_bytes"]
    try:
        with opener.open(request, timeout=60) as response:
            declared = response.headers.get("Content-Length")
            if declared is not None and (
                not declared.isdecimal() or int(declared) < 1 or int(declared) > maximum
            ):
                raise ReleaseAssetError("RPG_RUNTIME_RELEASE_ASSET_SIZE_INVALID")
            contents = response.read(maximum + 1)
    except OSError as exc:
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_ASSET_DOWNLOAD_FAILED") from exc
    if not contents or len(contents) > maximum:
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_ASSET_SIZE_INVALID")
    return contents


def observed_file(contents: bytes) -> dict[str, Any]:
    return {
        "observed_sha256": hashlib.sha256(contents).hexdigest(),
        "observed_size_bytes": len(contents),
    }


def observed_identity(release: dict[str, Any]) -> dict[str, Any]:
    return {
        "repository": release["repository"],
        "tag": release["tag"],
        "tag_commit": release["tag_commit"],
        "adapter_abi": release["adapter_abi"],
    }


def validate_observed_identity(release: dict[str, Any], record: object) -> None:
    if not isinstance(record, dict) or any(
        record.get(key) != value for key, value in observed_identity(release).items()
    ):
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_OBSERVED_INVALID")


def verify_observed_file(
    target: Path,
    item: object,
    declaration: dict[str, Any],
) -> tuple[int, str]:
    if (
        not isinstance(item, dict)
        or set(item) != {"observed_size_bytes", "observed_sha256"}
        or not isinstance(item.get("observed_size_bytes"), int)
        or item["observed_size_bytes"] < 1
        or item["observed_size_bytes"] > declaration["max_size_bytes"]
        or not isinstance(item.get("observed_sha256"), str)
        or HEX_DIGEST.fullmatch(item["observed_sha256"]) is None
    ):
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_OBSERVED_INVALID")
    try:
        info = target.lstat()
        contents = target.read_bytes()
    except OSError as exc:
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_ASSET_MISSING") from exc
    if (
        not stat.S_ISREG(info.st_mode)
        or len(contents) != item["observed_size_bytes"]
        or hashlib.sha256(contents).hexdigest() != item["observed_sha256"]
    ):
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_ASSET_MISMATCH")
    return len(contents), item["observed_sha256"]


def load_observed(runtime_root: Path) -> dict[str, Any]:
    target = runtime_root / OBSERVED_FILENAME
    if not target.exists():
        return {}
    try:
        if target.is_symlink() or not target.is_file():
            raise ReleaseAssetError("RPG_RUNTIME_RELEASE_OBSERVED_INVALID")
        value = json.loads(target.read_bytes())
    except (OSError, json.JSONDecodeError) as exc:
        raise ReleaseAssetError("RPG_RUNTIME_RELEASE_OBSERVED_INVALID") from exc
    return value if isinstance(value, dict) else {}


def atomic_write(target: Path, contents: bytes) -> None:
    target.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{target.name}-", dir=target.parent)
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "wb") as output:
            output.write(contents)
            output.flush()
            os.fsync(output.fileno())
        os.chmod(temporary, 0o644)
        os.replace(temporary, target)
    finally:
        temporary.unlink(missing_ok=True)
