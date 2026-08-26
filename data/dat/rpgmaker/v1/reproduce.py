#!/usr/bin/env python3
"""Double-clean, fixed-image RPG Maker runtime reproduction."""

from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
import zipfile
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from urllib.parse import urlparse


DAT_ROOT = Path(__file__).resolve().parent
REPOSITORY_ROOT = DAT_ROOT.parents[3]
LOCK_PATH = DAT_ROOT / "build-inputs.lock"
DEFAULT_SOURCE_CACHE = REPOSITORY_ROOT / ".cache/dependencies/rpgmaker/v1"
DEFAULT_WORK_ROOT = REPOSITORY_ROOT / ".cache/tmp"
DEFAULT_RUNTIME_ROOT = REPOSITORY_ROOT / "data/runtime/rpgmaker/v1"
SHA256 = re.compile(r"^[0-9a-f]{64}$")
CONTAINER_ID = re.compile(r"^[0-9a-f]{64}$")
DOWNLOAD_ATTEMPTS = 3
TRANSIENT_HTTP_STATUS = frozenset({429, 502, 503, 504})
IMAGES = {
    "easyrpg": "emscripten/emsdk@sha256:af45409f3199d88db4b1b03af0098532c8fb33a375ac257463eeb0a622870d06",
    "mkxp": "emscripten/emsdk@sha256:92c97951b9a6835cb5da9592e9d95226f67e09ecd01a541d817a5b4801f235a4",
}
OUTPUT_NAMES = {
    "easyrpg": ("easyrpg-player.js", "easyrpg-player.wasm"),
    "mkxp": ("mkxp-z_libretro.js", "mkxp-z_libretro.wasm"),
}
RECIPE_FILES = (
    "build.py", "reproduce.py", "source_offer.py",
    "builder/common.sh", "builder/deterministic-zip.sh", "builder/easyrpg.sh",
    "builder/mkxp.sh", "patches/easyrpg-retrom-bridge.patch",
    "patches/easyrpg-fixed-parallel.patch", "patches/mkxp-deterministic-build.patch",
    "patches/mkxp-openal-cmake-compat.patch",
)


class ReproductionError(RuntimeError):
    """A stable fail-closed reproduction failure."""


@dataclass(frozen=True)
class LockedInput:
    filename: str
    size_bytes: int
    sha256: str
    url: str
    license_spec: str
    association: str


def file_digest(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


def verify_file(path: Path, item: LockedInput) -> None:
    if path.is_symlink() or not path.is_file():
        raise ReproductionError(f"RPG_RUNTIME_BUILD_INPUT_MISSING:{item.filename}")
    if path.stat().st_size != item.size_bytes or file_digest(path) != item.sha256:
        raise ReproductionError(f"RPG_RUNTIME_BUILD_INPUT_MISMATCH:{item.filename}")


def load_lock() -> dict[str, LockedInput]:
    lines = LOCK_PATH.read_text(encoding="utf-8").splitlines()
    if lines[:2] != [
        "# schema=retrom-rpg-runtime-build-inputs-v1",
        "# filename|size_bytes|sha256|immutable_url|license_spec|association",
    ]:
        raise ReproductionError("RPG_RUNTIME_BUILD_INPUT_LOCK_INVALID")
    result: dict[str, LockedInput] = {}
    for line in lines[2:]:
        if not line or line.startswith("#"):
            raise ReproductionError("RPG_RUNTIME_BUILD_INPUT_LOCK_INVALID")
        fields = line.split("|")
        if len(fields) != 6:
            raise ReproductionError("RPG_RUNTIME_BUILD_INPUT_LOCK_INVALID")
        filename, size, sha256, url, license_spec, association = fields
        parsed = urlparse(url)
        if (
            filename != Path(filename).name
            or filename in result
            or not size.isdecimal()
            or int(size) <= 0
            or SHA256.fullmatch(sha256) is None
            or parsed.scheme != "https"
            or not parsed.hostname
            or any(marker in url.lower() for marker in ("/latest/", "lastsuccessfulbuild", "github.io"))
            or not license_spec
            or association not in {
                "RUNTIME_SOURCE", "DERIVED_RUNTIME_DATA", "BUILD_TOOL_SOURCE",
                "BUILD_TOOL_BINARY", "LICENSE_TEXT", "TOOLCHAIN_RUNTIME_SOURCE",
                "BUILD_RECIPE_SOURCE",
            }
        ):
            raise ReproductionError("RPG_RUNTIME_BUILD_INPUT_LOCK_INVALID")
        result[filename] = LockedInput(filename, int(size), sha256, url, license_spec, association)
    validate_license_references(result)
    return result


def load_reproduction_declaration() -> dict[str, object]:
    manifest = json.loads((DAT_ROOT / "manifest.json").read_text(encoding="utf-8"))
    declaration = manifest.get("build", {}).get("reproduction")
    if not isinstance(declaration, dict) or set(declaration) != {
        "images", "recipe_sha256", "source_lock_path", "source_lock_sha256",
    }:
        raise ReproductionError("RPG_RUNTIME_REPRODUCTION_DECLARATION_INVALID")
    if declaration.get("images") != IMAGES or declaration.get("source_lock_path") != "build-inputs.lock":
        raise ReproductionError("RPG_RUNTIME_REPRODUCTION_DECLARATION_INVALID")
    lock_digest = declaration.get("source_lock_sha256")
    if not isinstance(lock_digest, str) or lock_digest != file_digest(LOCK_PATH):
        raise ReproductionError("RPG_RUNTIME_REPRODUCTION_RECIPE_MISMATCH:build-inputs.lock")
    recipes = declaration.get("recipe_sha256")
    if not isinstance(recipes, dict) or set(recipes) != set(RECIPE_FILES):
        raise ReproductionError("RPG_RUNTIME_REPRODUCTION_DECLARATION_INVALID")
    for relative in RECIPE_FILES:
        expected = recipes.get(relative)
        if not isinstance(expected, str) or SHA256.fullmatch(expected) is None:
            raise ReproductionError("RPG_RUNTIME_REPRODUCTION_DECLARATION_INVALID")
        if file_digest(DAT_ROOT / relative) != expected:
            raise ReproductionError(f"RPG_RUNTIME_REPRODUCTION_RECIPE_MISMATCH:{relative}")
    outputs = manifest.get("build", {}).get("reproducible_outputs")
    if not isinstance(outputs, dict) or set(outputs) != set(OUTPUT_NAMES):
        raise ReproductionError("RPG_RUNTIME_REPRODUCIBLE_OUTPUT_UNDECLARED")
    return manifest


def validate_license_references(items: dict[str, LockedInput]) -> None:
    for item in items.values():
        for declaration in item.license_spec.split(";"):
            if declaration.startswith("@"):
                reference = declaration[1:].split(":", 1)
                if len(reference) != 2 or reference[0] not in items or not reference[1]:
                    raise ReproductionError("RPG_RUNTIME_BUILD_INPUT_LICENSE_INVALID")
            elif declaration not in ("SELF", "PER_FILE_LICENSE_HEADERS"):
                path = PurePosixPath(declaration)
                if path.is_absolute() or ".." in path.parts:
                    raise ReproductionError("RPG_RUNTIME_BUILD_INPUT_LICENSE_INVALID")


def download_input_once(item: LockedInput, destination: Path) -> None:
    request = urllib.request.Request(item.url, headers={"User-Agent": "retrom-rpg-reproducer/1"})
    temporary = destination.with_name(f".{destination.name}.download")
    temporary.unlink(missing_ok=True)
    try:
        with urllib.request.urlopen(request, timeout=120) as response, temporary.open("wb") as output:
            if urlparse(response.geturl()).scheme != "https":
                raise ReproductionError(f"RPG_RUNTIME_BUILD_INPUT_REDIRECT_INVALID:{item.filename}")
            remaining = item.size_bytes
            while remaining:
                chunk = response.read(min(1024 * 1024, remaining))
                if not chunk:
                    break
                output.write(chunk)
                remaining -= len(chunk)
            if remaining or response.read(1):
                raise ReproductionError(f"RPG_RUNTIME_BUILD_INPUT_SIZE_MISMATCH:{item.filename}")
        verify_file(temporary, item)
        os.chmod(temporary, 0o600)
        os.replace(temporary, destination)
    finally:
        temporary.unlink(missing_ok=True)


def download_input(item: LockedInput, destination: Path) -> None:
    for attempt in range(1, DOWNLOAD_ATTEMPTS + 1):
        try:
            download_input_once(item, destination)
            return
        except urllib.error.HTTPError as error:
            retry = error.code in TRANSIENT_HTTP_STATUS and attempt < DOWNLOAD_ATTEMPTS
            if not retry:
                raise ReproductionError(
                    f"RPG_RUNTIME_BUILD_INPUT_DOWNLOAD_FAILED:{item.filename}"
                ) from error
        except (urllib.error.URLError, TimeoutError) as error:
            if attempt == DOWNLOAD_ATTEMPTS:
                raise ReproductionError(
                    f"RPG_RUNTIME_BUILD_INPUT_DOWNLOAD_FAILED:{item.filename}"
                ) from error
        time.sleep(attempt)


def prepare_inputs(items: dict[str, LockedInput], source_cache: Path, offline: bool) -> None:
    locked = source_cache / "locked-sources"
    locked.mkdir(parents=True, exist_ok=True)
    for item in items.values():
        target = locked / item.filename
        if target.exists():
            verify_file(target, item)
        elif offline:
            raise ReproductionError(f"RPG_RUNTIME_BUILD_INPUT_MISSING:{item.filename}")
        else:
            download_input(item, target)
    validate_license_contents(items, locked)


def prepare_primary_sources(source_cache: Path, offline: bool) -> None:
    manifest = json.loads((DAT_ROOT / "manifest.json").read_text(encoding="utf-8"))
    source_cache.mkdir(parents=True, exist_ok=True)
    for source in manifest.get("source_archives", []):
        component = source.get("component_id")
        item = LockedInput(
            f"{component}.tar.gz", source.get("size_bytes"), source.get("sha256"),
            source.get("archive_url"), source.get("license_path"), "RUNTIME_SOURCE",
        )
        if (
            not isinstance(component, str)
            or not isinstance(item.size_bytes, int)
            or not isinstance(item.sha256, str)
            or SHA256.fullmatch(item.sha256) is None
            or not isinstance(item.url, str)
            or urlparse(item.url).scheme != "https"
        ):
            raise ReproductionError("RPG_RUNTIME_PRIMARY_SOURCE_INVALID")
        target = source_cache / item.filename
        if target.exists():
            verify_file(target, item)
        elif offline:
            raise ReproductionError(f"RPG_RUNTIME_BUILD_INPUT_MISSING:{item.filename}")
        else:
            download_input(item, target)


def archive_names(path: Path) -> set[str]:
    if zipfile.is_zipfile(path):
        with zipfile.ZipFile(path) as bundle:
            names = bundle.namelist()
    else:
        with tarfile.open(path) as bundle:
            names = [member.name for member in bundle.getmembers() if member.isfile()]
    result: set[str] = set()
    for name in names:
        parts = PurePosixPath(name).parts
        if len(parts) >= 2:
            result.add(PurePosixPath(*parts[1:]).as_posix())
    return result


def validate_license_contents(items: dict[str, LockedInput], locked: Path) -> None:
    name_cache: dict[str, set[str]] = {}
    for item in items.values():
        for declaration in item.license_spec.split(";"):
            source_name = item.filename
            license_path = declaration
            if declaration.startswith("@"):
                source_name, license_path = declaration[1:].split(":", 1)
            if license_path in ("SELF", "PER_FILE_LICENSE_HEADERS"):
                continue
            names = name_cache.setdefault(source_name, archive_names(locked / source_name))
            if license_path not in names:
                raise ReproductionError(
                    f"RPG_RUNTIME_BUILD_INPUT_LICENSE_MISSING:{source_name}:{license_path}"
                )


def cleanup_build_container(cidfile: Path) -> None:
    """Stop and remove only the container recorded for one clean build."""
    if not cidfile.exists():
        return
    cleanup_error: OSError | None = None
    try:
        container_id = cidfile.read_text(encoding="ascii").strip()
        if CONTAINER_ID.fullmatch(container_id) is None:
            raise ReproductionError("RPG_RUNTIME_BUILD_CONTAINER_ID_INVALID")
        for command in (
            ["docker", "stop", "--time", "10", container_id],
            ["docker", "rm", "--force", container_id],
        ):
            try:
                subprocess.run(
                    command, check=False, stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                )
            except OSError as error:
                cleanup_error = error
        if cleanup_error is not None:
            raise ReproductionError("RPG_RUNTIME_BUILD_CONTAINER_CLEANUP_FAILED") from cleanup_error
    finally:
        cidfile.unlink(missing_ok=True)


def run_clean_build(runtime: str, source_cache: Path, work_root: Path) -> tuple[Path, tempfile.TemporaryDirectory[str]]:
    temporary = tempfile.TemporaryDirectory(prefix=f"retrom-rpg-{runtime}-", dir=work_root)
    root = Path(temporary.name)
    work = root / "work"
    output = root / "output"
    cidfile = root / "container.cid"
    container_name = f"{root.name}-container"
    work.mkdir()
    output.mkdir()
    command = [
        "docker", "run", "--rm", "--network", "none", "--platform", "linux/amd64",
        "--hostname", "retrom-rpg-build", "--user", f"{os.getuid()}:{os.getgid()}",
        "--name", container_name, "--cidfile", str(cidfile),
        "-v", f"{DAT_ROOT}:/recipe:ro",
        "-v", f"{source_cache}:/inputs:ro",
        "-v", f"{work}:/work",
        "-v", f"{output}:/output",
        IMAGES[runtime], f"/recipe/builder/{runtime}.sh",
    ]
    try:
        try:
            subprocess.run(command, check=True)
        finally:
            cleanup_build_container(cidfile)
    except KeyboardInterrupt:
        temporary.cleanup()
        raise
    except (OSError, subprocess.CalledProcessError) as error:
        temporary.cleanup()
        raise ReproductionError(f"RPG_RUNTIME_CLEAN_BUILD_FAILED:{runtime}") from error
    except ReproductionError:
        temporary.cleanup()
        raise
    return output, temporary


def compare_outputs(runtime: str, left: Path, right: Path) -> dict[str, dict[str, int | str]]:
    evidence: dict[str, dict[str, int | str]] = {}
    for name in OUTPUT_NAMES[runtime]:
        first = left / name
        second = right / name
        if first.is_symlink() or second.is_symlink() or not first.is_file() or not second.is_file():
            raise ReproductionError(f"RPG_RUNTIME_BUILD_OUTPUT_MISSING:{runtime}:{name}")
        first_hash = file_digest(first)
        if first.stat().st_size != second.stat().st_size or first_hash != file_digest(second):
            raise ReproductionError(f"RPG_RUNTIME_BUILD_NOT_REPRODUCIBLE:{runtime}:{name}")
        evidence[name] = {"sizeBytes": first.stat().st_size, "sha256": first_hash}
    return evidence


def manifest_target(runtime: str) -> tuple[str, dict[str, tuple[int, str]]]:
    manifest = load_reproduction_declaration()
    declaration = manifest.get("build", {}).get("reproducible_outputs", {}).get(runtime)
    if not isinstance(declaration, dict) or set(declaration) != {"runtime_version", "files"}:
        raise ReproductionError(f"RPG_RUNTIME_REPRODUCIBLE_OUTPUT_UNDECLARED:{runtime}")
    files = declaration["files"]
    if not isinstance(files, dict) or set(files) != set(OUTPUT_NAMES[runtime]):
        raise ReproductionError(f"RPG_RUNTIME_REPRODUCIBLE_OUTPUT_UNDECLARED:{runtime}")
    expected: dict[str, tuple[int, str]] = {}
    for name, metadata in files.items():
        if not isinstance(metadata, dict) or set(metadata) != {"size_bytes", "sha256"}:
            raise ReproductionError(f"RPG_RUNTIME_REPRODUCIBLE_OUTPUT_UNDECLARED:{runtime}")
        size = metadata["size_bytes"]
        sha256 = metadata["sha256"]
        if not isinstance(size, int) or size <= 0 or not isinstance(sha256, str) or SHA256.fullmatch(sha256) is None:
            raise ReproductionError(f"RPG_RUNTIME_REPRODUCIBLE_OUTPUT_UNDECLARED:{runtime}")
        expected[name] = (size, sha256)
    version = declaration["runtime_version"]
    if not isinstance(version, str) or not version or Path(version).name != version:
        raise ReproductionError(f"RPG_RUNTIME_REPRODUCIBLE_OUTPUT_UNDECLARED:{runtime}")
    return version, expected


def publish_outputs(runtime: str, output: Path, runtime_root: Path, evidence: dict[str, dict[str, int | str]]) -> None:
    version, expected = manifest_target(runtime)
    for name, actual in evidence.items():
        if (actual["sizeBytes"], actual["sha256"]) != expected[name]:
            raise ReproductionError(f"RPG_RUNTIME_REPRODUCED_BYTES_UNDECLARED:{runtime}:{name}")
    target = runtime_root / version
    target.mkdir(parents=True, exist_ok=True)
    for name in OUTPUT_NAMES[runtime]:
        temporary = target / f".{name}.reproduced"
        shutil.copyfile(output / name, temporary)
        os.chmod(temporary, 0o644)
        os.replace(temporary, target / name)


def reproduce(runtime: str, source_cache: Path, work_root: Path, runtime_root: Path) -> dict[str, object]:
    left, left_context = run_clean_build(runtime, source_cache, work_root)
    try:
        right, right_context = run_clean_build(runtime, source_cache, work_root)
        try:
            evidence = compare_outputs(runtime, left, right)
            publish_outputs(runtime, left, runtime_root, evidence)
            return {"runtime": runtime, "image": IMAGES[runtime], "outputs": evidence}
        finally:
            right_context.cleanup()
    finally:
        left_context.cleanup()


def main(arguments: list[str] | None = None) -> int:
    del arguments
    raise ReproductionError(
        "RPG_RUNTIME_LOCAL_BUILD_UNSUPPORTED:use tagged fork release workflow"
    )


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (ReproductionError, OSError, ValueError, json.JSONDecodeError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1) from error
