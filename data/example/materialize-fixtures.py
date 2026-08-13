#!/usr/bin/env python3
"""Materialize operator-authorized formal fixtures inside data/example."""

from __future__ import annotations

import hashlib
import json
import os
import argparse
import sys
import tempfile
import zipfile
from pathlib import Path, PurePosixPath
from typing import BinaryIO, Iterable


REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT_DIR = Path(__file__).resolve().parent
MANIFEST_PATH = SCRIPT_DIR / "fixtures.json"
LOCAL_FIXTURE_ROOT = SCRIPT_DIR / "local-fixtures"


def safe_relative(value: str, label: str) -> PurePosixPath:
    path = PurePosixPath(value)
    if not value or path.is_absolute() or any(part in {"", ".", ".."} for part in path.parts):
        raise ValueError(f"{label} must be a safe relative path")
    return path


def source_path(source_root: Path, relative: str) -> Path:
    path = (source_root / safe_relative(relative, "sourceRelativePath")).resolve(strict=True)
    try:
        path.relative_to(source_root)
    except ValueError as error:
        raise ValueError("sourceRelativePath escapes the fixture root") from error
    if not path.is_file():
        raise ValueError(f"candidate source is not a regular file: {relative}")
    return path


def target_path(repo_root: Path, relative: str) -> Path:
    path = safe_relative(relative, "candidate target")
    required_prefix = PurePosixPath("data/example/local-fixtures")
    try:
        path.relative_to(required_prefix)
    except ValueError as error:
        raise ValueError("fixture targets must remain under data/example/local-fixtures") from error
    destination = repo_root.joinpath(*path.parts)
    local_root = (repo_root / required_prefix).resolve()
    resolved = destination.resolve(strict=False)
    try:
        resolved.relative_to(local_root)
    except ValueError as error:
        raise ValueError("fixture target escapes data/example/local-fixtures") from error
    return destination


def verified_write(
    chunks: Iterable[bytes], destination: Path, expected_size: int, expected_sha256: str
) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    checksum = hashlib.sha256()
    size = 0
    temporary_name: str | None = None
    try:
        with tempfile.NamedTemporaryFile(
            dir=destination.parent, prefix=f".{destination.name}.", delete=False
        ) as temporary:
            temporary_name = temporary.name
            for chunk in chunks:
                temporary.write(chunk)
                checksum.update(chunk)
                size += len(chunk)
        if size != expected_size or checksum.hexdigest() != expected_sha256:
            raise ValueError(
                f"fixture digest mismatch for {destination.name}: "
                f"size={size} sha256={checksum.hexdigest()}"
            )
        os.chmod(temporary_name, 0o644)
        os.replace(temporary_name, destination)
        temporary_name = None
    finally:
        if temporary_name is not None:
            Path(temporary_name).unlink(missing_ok=True)


def file_chunks(file: BinaryIO) -> Iterable[bytes]:
    while chunk := file.read(1024 * 1024):
        yield chunk


def copy_verified(
    source: Path, destination: Path, expected_size: int, expected_sha256: str
) -> None:
    with source.open("rb") as source_file:
        verified_write(file_chunks(source_file), destination, expected_size, expected_sha256)


def materialize_game(fixture: dict, source_root: Path, repo_root: Path) -> list[Path]:
    game = fixture["game"]
    source = source_path(source_root, game["sourceRelativePath"])
    game_target = target_path(repo_root, game["localPath"])
    written: list[Path] = []

    archive_relative = game.get("sourceArchiveLocalPath")
    if archive_relative:
        if game.get("singleMemberArchive") is not True:
            raise ValueError(f"{fixture['core']}: candidate archive must declare singleMemberArchive")
        archive_target = target_path(repo_root, archive_relative)
        copy_verified(
            source,
            archive_target,
            game["sourceArchiveSize"],
            game["sourceArchiveSha256"],
        )
        written.append(archive_target)
        with zipfile.ZipFile(source) as archive:
            members = [member for member in archive.infolist() if not member.is_dir()]
            if len(members) != 1:
                raise ValueError(
                    f"{fixture['core']}: expected one archive member, found {len(members)}"
                )
            with archive.open(members[0]) as content:
                verified_write(
                    file_chunks(content), game_target, game["size"], game["sha256"]
                )
    else:
        copy_verified(source, game_target, game["size"], game["sha256"])
    written.append(game_target)
    return written


def materialize_bios(fixture: dict, source_root: Path, repo_root: Path) -> list[Path]:
    written: list[Path] = []
    for bios in fixture.get("bios", []):
        source = source_path(source_root, bios["sourceRelativePath"])
        destination = target_path(repo_root, bios["localPath"])
        copy_verified(source, destination, bios["size"], bios["sha256"])
        written.append(destination)
    return written


def materialize_parent(fixture: dict, source_root: Path, repo_root: Path) -> list[Path]:
    parent = fixture.get("parent")
    if not parent:
        return []
    source = source_path(source_root, parent["sourceRelativePath"])
    destination = target_path(repo_root, parent["localPath"])
    copy_verified(source, destination, parent["size"], parent["sha256"])
    return [destination]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source-root", type=Path, required=True)
    parser.add_argument("cores", nargs="*")
    args = parser.parse_args()
    manifest = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
    fixtures = [
        fixture
        for fixture in [*manifest.get("fixtures", []), *manifest.get("netplayFixtures", [])]
        if fixture["game"]["localPath"].startswith("data/example/local-fixtures/")
    ]
    requested = set(args.cores)
    selector_for = lambda fixture: fixture.get("id", fixture["core"])
    known = {selector_for(fixture) for fixture in fixtures}
    unknown = requested - known
    if unknown:
        raise ValueError(f"unknown local fixture core(s): {', '.join(sorted(unknown))}")
    selected = [
        fixture for fixture in fixtures if not requested or selector_for(fixture) in requested
    ]
    if not selected:
        raise ValueError("no local fixtures selected")

    source_root = args.source_root
    if not source_root.is_absolute():
        raise ValueError("--source-root must be an absolute path")
    source_root = source_root.resolve(strict=True)

    written: list[Path] = []
    for fixture in selected:
        written.extend(materialize_game(fixture, source_root, REPO_ROOT))
        written.extend(materialize_parent(fixture, source_root, REPO_ROOT))
        written.extend(materialize_bios(fixture, source_root, REPO_ROOT))
        print(f"OK  {selector_for(fixture)}: materialized verified fixture")
    print(f"Materialized {len(selected)} core fixture(s), {len(written)} file(s).")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, KeyError, ValueError, zipfile.BadZipFile) as error:
        print(f"ERR {error}", file=sys.stderr)
        raise SystemExit(1) from error
