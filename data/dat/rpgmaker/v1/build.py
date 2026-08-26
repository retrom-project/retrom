#!/usr/bin/env python3
"""Materialize and byte-verify the pinned Retrom RPG Maker runtime."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import stat
import sys
import tarfile
import tempfile
import urllib.request
from pathlib import Path, PurePosixPath
from typing import Any
from urllib.parse import urljoin, urlparse

import release_assets
import reproduce
import source_offer


DAT_ROOT = Path(__file__).resolve().parent
REPOSITORY_ROOT = DAT_ROOT.parents[3]
DEFAULT_RUNTIME_ROOT = REPOSITORY_ROOT / "data/runtime/rpgmaker/v1"
DEFAULT_SOURCE_CACHE = REPOSITORY_ROOT / ".cache/dependencies/rpgmaker/v1"
HEX_64 = re.compile(r"^[0-9a-f]{64}$")
COMMIT_40 = re.compile(r"^[0-9a-f]{40}$")
SOURCE_BUILD_GUIDE = "data/dat/rpgmaker/v1/REPRODUCING.md"
EXPECTED_FIXED_RUNTIME_FILES = {
    "f2efc98-v5/retrom_position.rb": (1487, "524a0b3210f33f5a01220134548eb2947678ca5ef7a75b5caf3cfea2804fd5fa", "adapter_bridge"),
    "v3/native-bridge.js": (17278, "69016c7d295705836032c84ba86cdd6dd0bf5839f5e34f8c1800ae0f5fd34adc", "adapter_bridge"),
}
EXPECTED_RELEASE_RUNTIME_FILES = {
    "0.8.1.1-v4/easyrpg-player.js": ("easyrpg", "easyrpg-player.js", "runtime_js"),
    "0.8.1.1-v4/easyrpg-player.wasm": ("easyrpg", "easyrpg-player.wasm", "runtime_wasm"),
    "0.8.1.1-v5/easyrpg-player.js": ("easyrpg-r3", "easyrpg-player.js", "runtime_js"),
    "0.8.1.1-v5/easyrpg-player.wasm": ("easyrpg-r3", "easyrpg-player.wasm", "runtime_wasm"),
    "f2efc98-v5/mkxp-z_libretro.js": ("mkxp", "mkxp-z_libretro.js", "runtime_js"),
    "f2efc98-v5/mkxp-z_libretro.wasm": ("mkxp", "mkxp-z_libretro.wasm", "runtime_wasm"),
}
EXPECTED_ROUTES = {
    "RPG2000_EASYRPG_0811_V4": ("rpgmaker_2000", "RPG2000", "EASYRPG_WEB", "easyrpg-web-v1", "0.8.1.1-v4", True),
    "RPG2003_EASYRPG_0811_V4": ("rpgmaker_2003", "RPG2003", "EASYRPG_WEB", "easyrpg-web-v1", "0.8.1.1-v4", True),
    "RPG2000_EASYRPG_0811_V5": ("rpgmaker_2000", "RPG2000", "EASYRPG_WEB", "easyrpg-web-v1", "0.8.1.1-v5", False),
    "RPG2003_EASYRPG_0811_V5": ("rpgmaker_2003", "RPG2003", "EASYRPG_WEB", "easyrpg-web-v1", "0.8.1.1-v5", False),
    "RPGXP_MKXPZ_F2EFC98_V5": ("rpgmaker_xp", "RPGXP", "MKXP_LIBRETRO_WEB", "mkxp-z-libretro-v4", "f2efc98-v5", True),
    "RPGVX_MKXPZ_F2EFC98_V5": ("rpgmaker_vx", "RPGVX", "MKXP_LIBRETRO_WEB", "mkxp-z-libretro-v4", "f2efc98-v5", True),
    "RPGVXACE_MKXPZ_F2EFC98_V5": ("rpgmaker_vx_ace", "RPGVXACE", "MKXP_LIBRETRO_WEB", "mkxp-z-libretro-v4", "f2efc98-v5", True),
    "RPGMV_NATIVE_V3": ("rpgmaker_mv", "RPGMV", "NATIVE_WEB", "rpg-native-web-v1", "v3", True),
    "RPGMZ_NATIVE_V3": ("rpgmaker_mz", "RPGMZ", "NATIVE_WEB", "rpg-native-web-v1", "v3", True),
}

EXPECTED_SOURCE_COMPONENTS = {
    "easyrpg-player", "liblcf", "easyrpg-buildscripts", "mkxp-z", "retroarch",
    "mkxp-z-libretro-emscripten", "player-retrom-release-r2", "player-retrom-release-r3",
    "mkxp-retrom-release",
}


class BuildError(RuntimeError):
    """A stable fail-closed RPG runtime materialization error."""


def digest(contents: bytes) -> str:
    return hashlib.sha256(contents).hexdigest()


def load_manifest() -> dict[str, Any]:
    manifest = json.loads((DAT_ROOT / "manifest.json").read_bytes())
    if manifest.get("schema_version") != 1 or manifest.get("runtime_id") != "rpgmaker-v1":
        raise BuildError("RPG_RUNTIME_MANIFEST_INVALID")
    return manifest


def safe_path(value: object) -> PurePosixPath:
    if not isinstance(value, str):
        raise BuildError("RPG_RUNTIME_PATH_INVALID")
    result = PurePosixPath(value)
    if result.is_absolute() or str(result) != value or any(part in ("", ".", "..") for part in result.parts):
        raise BuildError("RPG_RUNTIME_PATH_INVALID")
    return result


def verify_file(target: Path, size: object, sha256: object) -> None:
    if not isinstance(size, int) or size <= 0 or not isinstance(sha256, str) or len(sha256) != 64:
        raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")
    try:
        info = target.lstat()
        if not stat.S_ISREG(info.st_mode):
            raise BuildError(f"RPG_RUNTIME_FILE_TYPE_INVALID:{target.name}")
        contents = target.read_bytes()
    except OSError as exc:
        raise BuildError(f"RPG_RUNTIME_FILE_MISSING:{target.name}") from exc
    if len(contents) != size or digest(contents) != sha256:
        raise BuildError(f"RPG_RUNTIME_FILE_MISMATCH:{target.name}")


def validate_small_inputs(manifest: dict[str, Any]) -> None:
    if set(manifest) != {
        "schema_version", "runtime_id", "runtime_files", "runtime_releases",
        "artifacts", "source_archives", "build",
    }:
        raise BuildError("RPG_RUNTIME_MANIFEST_INVALID")
    build = manifest.get("build")
    build_keys = {
        "recipe_path", "easyrpg_emscripten_version", "mkxp_emscripten_version",
        "wasi_sdk_version", "binaryen_version", "easyrpg_patch_path",
        "easyrpg_patch_sha256", "mkxp_bridge_path", "mkxp_bridge_sha256",
        "native_bridge_v3_path", "native_bridge_v3_sha256",
    }
    if not isinstance(build, dict) or set(build) != build_keys:
        raise BuildError("RPG_RUNTIME_BUILD_DECLARATION_INVALID")
    if (
        build.get("recipe_path") != "build.py"
        or build.get("easyrpg_emscripten_version") != "3.1.74"
        or build.get("mkxp_emscripten_version") != "4.0.8"
        or build.get("wasi_sdk_version") != "30"
        or build.get("binaryen_version") != "126"
    ):
        raise BuildError("RPG_RUNTIME_BUILD_DECLARATION_INVALID")
    for path_key, digest_key in (
        ("easyrpg_patch_path", "easyrpg_patch_sha256"),
        ("mkxp_bridge_path", "mkxp_bridge_sha256"),
        ("native_bridge_v3_path", "native_bridge_v3_sha256"),
    ):
        relative = safe_path(build.get(path_key))
        expected = build.get(digest_key)
        contents = (DAT_ROOT / relative).read_bytes()
        if not isinstance(expected, str) or HEX_64.fullmatch(expected) is None or digest(contents) != expected:
            raise BuildError("RPG_RUNTIME_BUILD_INPUT_MISMATCH")
    recipe = DAT_ROOT / safe_path(build.get("recipe_path"))
    if recipe.resolve() != Path(__file__).resolve():
        raise BuildError("RPG_RUNTIME_BUILD_DECLARATION_INVALID")
    if not (DAT_ROOT / SOURCE_BUILD_GUIDE.split("/")[-1]).is_file():
        raise BuildError("RPG_RUNTIME_REPRODUCTION_GUIDE_MISSING")
    files = manifest.get("runtime_files")
    releases = manifest.get("runtime_releases")
    artifacts = manifest.get("artifacts")
    sources = manifest.get("source_archives")
    if (
        not isinstance(files, list)
        or not isinstance(releases, list)
        or not isinstance(artifacts, list)
        or len(artifacts) != len(EXPECTED_ROUTES)
        or not isinstance(sources, list)
    ):
        raise BuildError("RPG_RUNTIME_MANIFEST_INVALID")
    release_index = release_assets.validate(releases)
    validate_runtime_file_declarations(files, release_index)
    validate_artifacts(artifacts, files)
    validate_sources(sources)


def validate_runtime_file_declarations(
    files: list[dict[str, Any]],
    releases: dict[str, dict[str, Any]],
) -> None:
    seen: set[str] = set()
    for item in files:
        if not isinstance(item, dict):
            raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")
        relative = str(safe_path(item.get("path_in_release")))
        if relative in seen:
            raise BuildError("RPG_RUNTIME_PATH_DUPLICATE")
        seen.add(relative)
        if relative in EXPECTED_FIXED_RUNTIME_FILES:
            validate_fixed_runtime_file(item, relative)
        elif relative in EXPECTED_RELEASE_RUNTIME_FILES:
            validate_release_runtime_file(item, relative, releases)
        else:
            raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")
    expected = set(EXPECTED_FIXED_RUNTIME_FILES) | set(EXPECTED_RELEASE_RUNTIME_FILES)
    if seen != expected:
        raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")


def validate_fixed_runtime_file(item: dict[str, Any], relative: str) -> None:
    if set(item) != {"path_in_release", "size_bytes", "sha256", "role"}:
        raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")
    actual = (item.get("size_bytes"), item.get("sha256"), item.get("role"))
    if actual != EXPECTED_FIXED_RUNTIME_FILES[relative]:
        raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")


def validate_release_runtime_file(
    item: dict[str, Any],
    relative: str,
    releases: dict[str, dict[str, Any]],
) -> None:
    if set(item) != {"path_in_release", "release_id", "asset_filename", "role"}:
        raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")
    actual = (item.get("release_id"), item.get("asset_filename"), item.get("role"))
    if actual != EXPECTED_RELEASE_RUNTIME_FILES[relative]:
        raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")
    release = releases.get(item["release_id"])
    matches = [
        asset for asset in release["assets"]
        if asset["filename"] == item["asset_filename"]
        and asset["path_in_release"] == relative
        and asset["role"] == item["role"]
    ]
    if len(matches) != 1:
        raise BuildError("RPG_RUNTIME_FILE_DECLARATION_INVALID")


def validate_artifacts(artifacts: list[dict[str, Any]], files: list[dict[str, Any]]) -> None:
    file_index = {item["path_in_release"]: item for item in files}
    seen: set[str] = set()
    selected_by_core: dict[str, int] = {}
    required = {
        "core_id", "generation", "route_key", "runtime_adapter_kind", "runtime_version",
        "adapter_id", "adapter_abi", "entry_path", "file_paths", "requires_threads", "save_payload_kind",
        "save_max_bytes", "selected_for_new_bindings", "available_for_launch", "compatibility",
    }
    for artifact in artifacts:
        if not isinstance(artifact, dict) or set(artifact) != required:
            raise BuildError("RPG_RUNTIME_ARTIFACT_DECLARATION_INVALID")
        core_id = artifact.get("core_id")
        route_key = artifact.get("route_key")
        route = EXPECTED_ROUTES.get(route_key)
        actual_route = (
            core_id, artifact.get("generation"), artifact.get("runtime_adapter_kind"),
            artifact.get("adapter_id"), artifact.get("runtime_version"),
            artifact.get("selected_for_new_bindings"),
        )
        if route is None or actual_route != route or route_key in seen:
            raise BuildError("RPG_RUNTIME_ARTIFACT_ROUTE_INVALID")
        seen.add(route_key)
        selected_by_core[core_id] = selected_by_core.get(core_id, 0) + int(
            artifact.get("selected_for_new_bindings") is True
        )
        paths = artifact.get("file_paths")
        if not isinstance(paths, list) or not paths or len(paths) != len(set(paths)):
            raise BuildError("RPG_RUNTIME_ARTIFACT_FILES_INVALID")
        try:
            declared = [file_index[path] for path in paths]
        except (KeyError, AttributeError) as exc:
            raise BuildError("RPG_RUNTIME_ARTIFACT_FILES_INVALID") from exc
        entry_relative = f"{artifact['runtime_version']}/{artifact['entry_path']}"
        entry = file_index.get(entry_relative)
        if (
            entry is None
            or entry not in declared
            or artifact.get("available_for_launch") is not True
        ):
            raise BuildError("RPG_RUNTIME_ARTIFACT_BYTES_INVALID")
    expected_cores = {route[0] for route in EXPECTED_ROUTES.values()}
    if seen != set(EXPECTED_ROUTES) or selected_by_core != {core: 1 for core in expected_cores}:
        raise BuildError("RPG_RUNTIME_ARTIFACT_ROUTE_INVALID")


def validate_sources(sources: list[dict[str, Any]]) -> None:
    required = {"component_id", "repository", "commit", "archive_url", "size_bytes", "sha256", "license_path"}
    seen: set[str] = set()
    for source in sources:
        if not isinstance(source, dict) or set(source) != required:
            raise BuildError("RPG_RUNTIME_SOURCE_DECLARATION_INVALID")
        component = source.get("component_id")
        commit = source.get("commit")
        archive_url = source.get("archive_url")
        if (
            component not in EXPECTED_SOURCE_COMPONENTS
            or component in seen
            or not isinstance(commit, str)
            or COMMIT_40.fullmatch(commit) is None
            or not isinstance(archive_url, str)
            or urlparse(archive_url).scheme != "https"
            or urlparse(archive_url).hostname != "codeload.github.com"
            or not archive_url.endswith(f"/tar.gz/{commit}")
            or not isinstance(source.get("size_bytes"), int)
            or source["size_bytes"] <= 0
            or not isinstance(source.get("sha256"), str)
            or HEX_64.fullmatch(source["sha256"]) is None
        ):
            raise BuildError("RPG_RUNTIME_SOURCE_DECLARATION_INVALID")
        safe_path(source.get("license_path"))
        seen.add(component)
    if seen != EXPECTED_SOURCE_COMPONENTS:
        raise BuildError("RPG_RUNTIME_SOURCE_CLOSURE_INCOMPLETE")


def materialize_bridges(manifest: dict[str, Any], runtime_root: Path) -> None:
    build = manifest["build"]
    native_v3 = (DAT_ROOT / safe_path(build["native_bridge_v3_path"])).read_bytes()
    copies = {
        "f2efc98-v5/retrom_position.rb": DAT_ROOT / safe_path(build["mkxp_bridge_path"]),
        "v3/native-bridge.js": native_v3,
    }
    for relative, source in copies.items():
        target = runtime_root / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        if isinstance(source, Path) and source.is_relative_to(runtime_root) and not source.is_file():
            continue
        contents = source.read_bytes() if isinstance(source, Path) else source
        if target.exists() and target.read_bytes() == contents:
            continue
        publish_file(target, contents)


class PinnedHTTPSRedirectHandler(urllib.request.HTTPRedirectHandler):
    def __init__(self) -> None:
        self.redirects = 0

    def redirect_request(self, request: Any, file_pointer: Any, code: int, message: str, headers: Any, new_url: str) -> Any:
        self.redirects += 1
        resolved = urljoin(request.full_url, new_url)
        if self.redirects > 3 or urlparse(request.full_url).scheme != "https" or urlparse(resolved).scheme != "https":
            raise BuildError("RPG_RUNTIME_SOURCE_REDIRECT_INVALID")
        return super().redirect_request(request, file_pointer, code, message, headers, resolved)


def publish_file(target: Path, contents: bytes) -> None:
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


def download_source(source: dict[str, Any], target: Path) -> None:
    expected_size = source["size_bytes"]
    request = urllib.request.Request(source["archive_url"], headers={"User-Agent": "retrom-rpg-runtime-builder/1"})
    opener = urllib.request.build_opener(PinnedHTTPSRedirectHandler())
    target.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{source['component_id']}-", dir=target.parent)
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "wb") as output, opener.open(request, timeout=60) as response:
            final_url = response.geturl()
            if urlparse(final_url).scheme != "https":
                raise BuildError("RPG_RUNTIME_SOURCE_REDIRECT_INVALID")
            declared = response.headers.get("Content-Length")
            if declared is not None and (not declared.isdecimal() or int(declared) != expected_size):
                raise BuildError(f"RPG_RUNTIME_SOURCE_SIZE_MISMATCH:{source['component_id']}")
            remaining = expected_size
            while remaining:
                chunk = response.read(min(1024 * 1024, remaining))
                if not chunk:
                    break
                output.write(chunk)
                remaining -= len(chunk)
            if remaining or response.read(1):
                raise BuildError(f"RPG_RUNTIME_SOURCE_SIZE_MISMATCH:{source['component_id']}")
            output.flush()
            os.fsync(output.fileno())
        verify_file(temporary, expected_size, source["sha256"])
        os.chmod(temporary, 0o600)
        os.replace(temporary, target)
    finally:
        temporary.unlink(missing_ok=True)


def download_sources(manifest: dict[str, Any], source_cache: Path) -> None:
    source_cache.mkdir(parents=True, exist_ok=True)
    for source in manifest["source_archives"]:
        component = source["component_id"]
        target = source_cache / f"{component}.tar.gz"
        if target.exists():
            verify_file(target, source["size_bytes"], source["sha256"])
            continue
        download_source(source, target)


def prepare_source_offer_inputs(manifest: dict[str, Any], source_cache: Path) -> None:
    """Materialize every primary and locked input required by the source offer."""
    download_sources(manifest, source_cache)
    reproduce.prepare_inputs(reproduce.load_lock(), source_cache, offline=False)


def verify_runtime(manifest: dict[str, Any], runtime_root: Path) -> None:
    for item in manifest["runtime_files"]:
        if "release_id" in item:
            continue
        relative = safe_path(item["path_in_release"])
        verify_file(runtime_root / relative, item["size_bytes"], item["sha256"])
    try:
        observed = release_assets.verify(manifest["runtime_releases"], runtime_root)
    except release_assets.ReleaseAssetError as error:
        raise BuildError(str(error)) from error
    expected = set(EXPECTED_RELEASE_RUNTIME_FILES)
    if set(observed) != expected:
        raise BuildError("RPG_RUNTIME_RELEASE_ASSET_SET_INVALID")
    for version in ("0.8.1.1-v4", "0.8.1.1-v5"):
        verify_easyrpg_restore_order((runtime_root / version / "easyrpg-player.js").read_bytes())


def verify_easyrpg_restore_order(contents: bytes) -> None:
    markers = (
        b'addRunDependency(retromDependency)',
        b'FS.syncfs(true,function(err)',
        b'for(const entry of Module.retromRestoreFiles||[])',
        b'Module.retromFileSystemReady=true',
        b'removeRunDependency(retromDependency)',
    )
    if any(contents.count(marker) != 1 for marker in markers):
        raise BuildError("RPG_RUNTIME_RESTORE_ORDER_INVALID")
    if list(map(contents.index, markers)) != sorted(map(contents.index, markers)):
        raise BuildError("RPG_RUNTIME_RESTORE_ORDER_INVALID")
    for message in (
        b'retrom filesystem initialization failed',
        b'retrom filesystem payload initialization failed',
    ):
        if contents.count(message) != 1:
            raise BuildError("RPG_RUNTIME_RESTORE_ORDER_INVALID")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("data-check", "prepare", "deps-check"))
    parser.add_argument("--runtime-root", type=Path, default=DEFAULT_RUNTIME_ROOT)
    parser.add_argument("--source-cache", type=Path, default=DEFAULT_SOURCE_CACHE)
    parser.add_argument("--offline", action="store_true")
    args = parser.parse_args()
    manifest = load_manifest()
    validate_small_inputs(manifest)
    if args.action == "data-check":
        return 0
    if args.action == "prepare":
        release_assets.materialize(
            manifest["runtime_releases"], args.runtime_root, offline=args.offline
        )
    materialize_bridges(manifest, args.runtime_root)
    verify_runtime(manifest, args.runtime_root)
    if args.action == "prepare" and not args.offline:
        prepare_source_offer_inputs(manifest, args.source_cache)
        source_offer.materialize(manifest, args.source_cache, args.runtime_root)
    if args.action in ("prepare", "deps-check"):
        source_offer.verify(manifest, args.source_cache, args.runtime_root)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (
        BuildError, OSError, KeyError, json.JSONDecodeError, tarfile.TarError,
        source_offer.SourceOfferError, release_assets.ReleaseAssetError,
        reproduce.ReproductionError,
    ) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1) from error
