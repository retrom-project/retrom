"""Materialize and verify the RPG runtime corresponding-source offer."""

from __future__ import annotations

import hashlib
import os
import shutil
import tarfile
import tempfile
import zipfile
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Any

import reproduce


DAT_ROOT = Path(__file__).resolve().parent
OFFER_ASSOCIATIONS = {
    "RUNTIME_SOURCE", "DERIVED_RUNTIME_DATA", "TOOLCHAIN_RUNTIME_SOURCE",
    "BUILD_RECIPE_SOURCE", "BUILD_TOOL_SOURCE", "BUILD_TOOL_BINARY", "LICENSE_TEXT",
}
RECIPE_FILES = (
    "build.py", "build-inputs.lock", "manifest.json", "REPRODUCING.md",
    "release_assets.py", "reproduce.py", "source_offer.py",
    "builder/common.sh", "builder/deterministic-zip.sh", "builder/easyrpg.sh",
    "builder/mkxp.sh", "patches/easyrpg-retrom-bridge.patch",
    "patches/easyrpg-fixed-parallel.patch", "patches/mkxp-deterministic-build.patch",
    "patches/mkxp-openal-cmake-compat.patch",
)


class SourceOfferError(RuntimeError):
    """A stable fail-closed source-offer failure."""


@dataclass(frozen=True)
class OfferInput:
    filename: str
    size_bytes: int
    sha256: str
    url: str
    license_spec: str
    association: str
    cache_path: Path


def digest(path: Path) -> str:
    checksum = hashlib.sha256()
    with path.open("rb") as source:
        while chunk := source.read(1024 * 1024):
            checksum.update(chunk)
    return checksum.hexdigest()


PRIMARY_ASSOCIATIONS = {
    "easyrpg-player": "RUNTIME_SOURCE",
    "liblcf": "RUNTIME_SOURCE",
    "easyrpg-buildscripts": "BUILD_RECIPE_SOURCE",
    "mkxp-z": "RUNTIME_SOURCE",
    "retroarch": "RUNTIME_SOURCE",
    "mkxp-z-libretro-emscripten": "HISTORICAL_BUILD_RECIPE_REFERENCE",
    "player-retrom-release-r2": "RUNTIME_SOURCE",
    "player-retrom-release-r3": "RUNTIME_SOURCE",
    "mkxp-retrom-release": "BUILD_RECIPE_SOURCE",
}


def primary_inputs(manifest: dict[str, Any], source_cache: Path) -> list[OfferInput]:
    return [
        OfferInput(
            f"{source['component_id']}.tar.gz", source["size_bytes"], source["sha256"],
            source["archive_url"], source["license_path"],
            PRIMARY_ASSOCIATIONS[source["component_id"]],
            source_cache / f"{source['component_id']}.tar.gz",
        )
        for source in manifest["source_archives"]
    ]


def locked_offer_inputs(source_cache: Path) -> list[OfferInput]:
    locked = reproduce.load_lock()
    return [
        OfferInput(
            item.filename, item.size_bytes, item.sha256, item.url, item.license_spec,
            item.association, source_cache / "locked-sources" / item.filename,
        )
        for item in locked.values()
        if item.association in OFFER_ASSOCIATIONS
    ]


def verify_offer_input(item: OfferInput) -> None:
    if (
        item.cache_path.is_symlink()
        or not item.cache_path.is_file()
        or item.cache_path.stat().st_size != item.size_bytes
        or digest(item.cache_path) != item.sha256
    ):
        raise SourceOfferError(f"RPG_RUNTIME_SOURCE_OFFER_INPUT_INVALID:{item.filename}")


def archive_member(path: Path, declared: str) -> bytes:
    if zipfile.is_zipfile(path):
        with zipfile.ZipFile(path) as bundle:
            matches = [name for name in bundle.namelist() if stripped_name(name) == declared]
            if len(matches) != 1:
                raise SourceOfferError(f"RPG_RUNTIME_LICENSE_MISSING:{path.name}:{declared}")
            return bundle.read(matches[0])
    with tarfile.open(path) as bundle:
        matches = [
            member for member in bundle.getmembers()
            if member.isfile() and stripped_name(member.name) == declared
        ]
        if len(matches) != 1:
            raise SourceOfferError(f"RPG_RUNTIME_LICENSE_MISSING:{path.name}:{declared}")
        extracted = bundle.extractfile(matches[0])
        if extracted is None:
            raise SourceOfferError(f"RPG_RUNTIME_LICENSE_MISSING:{path.name}:{declared}")
        return extracted.read()


def stripped_name(name: str) -> str:
    parts = PurePosixPath(name).parts
    return PurePosixPath(*parts[1:]).as_posix() if len(parts) >= 2 else ""


def license_contents(
    item: OfferInput, declaration: str, all_inputs: dict[str, OfferInput]
) -> tuple[str, bytes]:
    source = item
    license_path = declaration
    if declaration.startswith("@"):
        source_name, license_path = declaration[1:].split(":", 1)
        source = all_inputs[source_name]
    if license_path == "SELF":
        return f"{source.filename}:SELF", source.cache_path.read_bytes()
    if license_path == "PER_FILE_LICENSE_HEADERS":
        return (
            f"{source.filename}:PER_FILE_LICENSE_HEADERS",
            b"License grants and copyright notices are retained in each source file of the exact corresponding-source archive.\n",
        )
    return f"{source.filename}:{license_path}", archive_member(source.cache_path, license_path)


def has_reproducible_outputs(manifest: dict[str, Any]) -> bool:
    outputs = manifest.get("build", {}).get("reproducible_outputs")
    return isinstance(outputs, dict) and set(outputs) == {"easyrpg", "mkxp"}


def has_tagged_releases(manifest: dict[str, Any]) -> bool:
    releases = manifest.get("runtime_releases")
    return isinstance(releases, list) and {
        release.get("id") for release in releases if isinstance(release, dict)
    } == {"easyrpg", "easyrpg-r3", "mkxp"}


def binary_association(item: OfferInput, manifest: dict[str, Any] | None = None) -> str:
    if item.association == "HISTORICAL_BUILD_RECIPE_REFERENCE":
        return "HISTORICAL_BUILD_RECIPE_REFERENCE"
    if item.association == "LICENSE_TEXT":
        return "LICENSE_REFERENCE_ONLY"
    if item.association in {"BUILD_RECIPE_SOURCE", "BUILD_TOOL_SOURCE", "BUILD_TOOL_BINARY"}:
        return "BUILD_PROCESS_ONLY"
    if manifest is not None and has_tagged_releases(manifest):
        return "TAGGED_RELEASE_COMPATIBLE"
    if manifest is not None and has_reproducible_outputs(manifest):
        return "EXACT_REPRODUCIBLE_BUILD"
    return "SOURCE_INPUT_NOT_REPRODUCED"


def binary_targets(item: OfferInput, manifest: dict[str, Any]) -> str:
    targets = manifest.get("build", {}).get("reproducible_outputs")
    if isinstance(targets, dict):
        easy = targets["easyrpg"]["runtime_version"]
        mkxp = targets["mkxp"]["runtime_version"]
    elif has_tagged_releases(manifest):
        releases = {release["id"]: release for release in manifest["runtime_releases"]}
        easy = ";".join(
            releases[release_id]["assets"][0]["path_in_release"].split("/", 1)[0]
            for release_id in ("easyrpg", "easyrpg-r3")
        )
        mkxp = releases["mkxp"]["assets"][0]["path_in_release"].split("/", 1)[0]
    else:
        easy = "0.8.1.1-v4"
        mkxp = "f2efc98-v5"
    if item.association == "HISTORICAL_BUILD_RECIPE_REFERENCE":
        return "historical-routes-only"
    if item.filename.startswith("easy-") or "3.1.74" in item.filename:
        return easy
    if (
        item.filename.startswith("mkxp-")
        or item.filename == "boost-license-1.90.0.txt"
        or "4.0.8" in item.filename
        or "wasi-sdk-30" in item.filename
        or "binaryen-126" in item.filename
    ):
        return mkxp
    if item.filename in {
        "easyrpg-player.tar.gz", "liblcf.tar.gz", "easyrpg-buildscripts.tar.gz",
        "player-retrom-release-r2.tar.gz", "player-retrom-release-r3.tar.gz",
    }:
        return easy
    if item.filename in {"mkxp-z.tar.gz", "retroarch.tar.gz", "mkxp-retrom-release.tar.gz"}:
        return mkxp
    if item.filename.startswith("tool-"):
        return f"{easy};{mkxp}"
    raise SourceOfferError(f"RPG_RUNTIME_SOURCE_TARGET_UNDECLARED:{item.filename}")


def notice_block(
    item: OfferInput, all_inputs: dict[str, OfferInput], manifest: dict[str, Any]
) -> tuple[bytes, dict[str, bytes]]:
    licenses: dict[str, bytes] = {}
    references: list[str] = []
    for declaration in item.license_spec.split(";"):
        reference, contents = license_contents(item, declaration, all_inputs)
        target = reference.replace("/", "_").replace(":", "--")
        existing = licenses.setdefault(target, contents)
        if existing != contents:
            raise SourceOfferError("RPG_RUNTIME_LICENSE_TARGET_COLLISION")
        references.append(target)
    header = (
        f"===== {item.filename} =====\n"
        f"source-input: {item.url}\n"
        f"sha256: {item.sha256}\n"
        f"binary-association: {binary_association(item, manifest)}\n"
        f"binary-targets: {binary_targets(item, manifest)}\n"
        f"source-association: {item.association}\n"
        f"license-files: {';'.join(references)}\n"
    ).encode("utf-8")
    bodies = [licenses[reference] + (b"" if licenses[reference].endswith(b"\n") else b"\n") for reference in references]
    return header + b"".join(bodies), licenses


def atomic_copy(source: Path, target: Path, mode: int = 0o644) -> None:
    target.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{target.name}-", dir=target.parent)
    os.close(descriptor)
    temporary = Path(temporary_name)
    try:
        shutil.copyfile(source, temporary)
        os.chmod(temporary, mode)
        os.replace(temporary, target)
    finally:
        temporary.unlink(missing_ok=True)


def atomic_write(contents: bytes, target: Path) -> None:
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


def source_offer_model(manifest: dict[str, Any], source_cache: Path) -> tuple[list[OfferInput], dict[str, bytes], bytes]:
    inputs = primary_inputs(manifest, source_cache) + locked_offer_inputs(source_cache)
    all_locked = {
        item.filename: OfferInput(
            item.filename, item.size_bytes, item.sha256, item.url, item.license_spec,
            item.association, source_cache / "locked-sources" / item.filename,
        )
        for item in reproduce.load_lock().values()
    }
    all_inputs = {**all_locked, **{item.filename: item for item in inputs}}
    notices: list[bytes] = []
    licenses: dict[str, bytes] = {}
    for item in inputs:
        verify_offer_input(item)
        block, item_licenses = notice_block(item, all_inputs, manifest)
        notices.append(block)
        for target, contents in item_licenses.items():
            existing = licenses.setdefault(target, contents)
            if existing != contents:
                raise SourceOfferError("RPG_RUNTIME_LICENSE_TARGET_COLLISION")
    return inputs, licenses, b"\n".join(notices)


def materialize(manifest: dict[str, Any], source_cache: Path, runtime_root: Path) -> None:
    inputs, licenses, notices = source_offer_model(manifest, source_cache)
    prune_directory(
        runtime_root / "corresponding-source",
        {item.filename for item in inputs} | {"retrom-build-recipe"},
    )
    prune_directory(runtime_root / "licenses", set(licenses))
    for item in inputs:
        atomic_copy(item.cache_path, runtime_root / "corresponding-source" / item.filename)
    for name, contents in licenses.items():
        atomic_write(contents, runtime_root / "licenses" / name)
    for relative in RECIPE_FILES:
        source = DAT_ROOT / relative
        mode = 0o755 if source.stat().st_mode & 0o111 else 0o644
        atomic_copy(
            source,
            runtime_root / "corresponding-source/retrom-build-recipe" / relative,
            mode,
        )
    atomic_write(notices, runtime_root / "THIRD_PARTY_NOTICES")


def prune_directory(directory: Path, expected: set[str]) -> None:
    directory.mkdir(parents=True, exist_ok=True)
    for entry in directory.iterdir():
        if entry.name in expected:
            continue
        if entry.is_dir() and not entry.is_symlink():
            shutil.rmtree(entry)
        else:
            entry.unlink()


def verify(manifest: dict[str, Any], source_cache: Path, runtime_root: Path) -> None:
    inputs, licenses, notices = source_offer_model(manifest, source_cache)
    for item in inputs:
        target = runtime_root / "corresponding-source" / item.filename
        if target.is_symlink() or not target.is_file() or target.stat().st_size != item.size_bytes or digest(target) != item.sha256:
            raise SourceOfferError(f"RPG_RUNTIME_SOURCE_OFFER_INVALID:{item.filename}")
    for name, contents in licenses.items():
        target = runtime_root / "licenses" / name
        if target.is_symlink() or not target.is_file() or target.read_bytes() != contents:
            raise SourceOfferError(f"RPG_RUNTIME_LICENSE_MISMATCH:{name}")
    for relative in RECIPE_FILES:
        target = runtime_root / "corresponding-source/retrom-build-recipe" / relative
        source = DAT_ROOT / relative
        expected_mode = 0o755 if source.stat().st_mode & 0o111 else 0o644
        if (
            target.is_symlink()
            or not target.is_file()
            or target.read_bytes() != source.read_bytes()
            or target.stat().st_mode & 0o777 != expected_mode
        ):
            raise SourceOfferError(f"RPG_RUNTIME_BUILD_RECIPE_OFFER_INVALID:{relative}")
    if (runtime_root / "THIRD_PARTY_NOTICES").read_bytes() != notices:
        raise SourceOfferError("RPG_RUNTIME_NOTICE_MISMATCH")
    expected_sources = {item.filename for item in inputs} | {"retrom-build-recipe"}
    expected_licenses = set(licenses)
    actual_sources = {path.name for path in (runtime_root / "corresponding-source").iterdir()}
    actual_licenses = {path.name for path in (runtime_root / "licenses").iterdir()}
    if actual_sources != expected_sources or actual_licenses != expected_licenses:
        raise SourceOfferError("RPG_RUNTIME_SOURCE_OFFER_INVALID")
    recipe_root = runtime_root / "corresponding-source/retrom-build-recipe"
    actual_recipe = {
        path.relative_to(recipe_root).as_posix() for path in recipe_root.rglob("*") if path.is_file()
    }
    if actual_recipe != set(RECIPE_FILES):
        raise SourceOfferError("RPG_RUNTIME_BUILD_RECIPE_OFFER_INVALID")
