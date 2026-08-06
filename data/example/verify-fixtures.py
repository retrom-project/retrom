#!/usr/bin/env python3
"""Verify immutable fixture, BIOS, core, and Arcade DAT relationships."""

from __future__ import annotations

import hashlib
import json
import sys
import xml.etree.ElementTree as ET
import zipfile
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
MANIFEST_PATH = Path(__file__).with_name("fixtures.json")
DEPENDENCY_MANIFEST_PATH = REPO_ROOT / "data/dat/emulatorjs/4.2.3/manifest.json"


def digest(path: Path, algorithm: str) -> str:
    checksum = hashlib.new(algorithm)
    with path.open("rb") as file:
        for chunk in iter(lambda: file.read(1024 * 1024), b""):
            checksum.update(chunk)
    return checksum.hexdigest()


def digest_reader(reader: Any, algorithm: str) -> str:
    checksum = hashlib.new(algorithm)
    for chunk in iter(lambda: reader.read(1024 * 1024), b""):
        checksum.update(chunk)
    return checksum.hexdigest()


def require_file(record: dict[str, Any], label: str, errors: list[str]) -> Path:
    relative_path = record.get("localPath") or record.get("path")
    path = REPO_ROOT / relative_path
    if not path.is_file():
        errors.append(f"{label}: missing {relative_path}")
        return path

    expected_size = record.get("size")
    if expected_size is not None and path.stat().st_size != expected_size:
        errors.append(
            f"{label}: size {path.stat().st_size}, expected {expected_size} ({relative_path})"
        )
    for algorithm in ("md5", "sha256"):
        expected = record.get(algorithm)
        if expected:
            actual = digest(path, algorithm)
            if actual.lower() != expected.lower():
                errors.append(
                    f"{label}: {algorithm} {actual}, expected {expected} ({relative_path})"
                )
    return path


def find_dat_game(dat_path: Path, game_name: str) -> ET.Element | None:
    for _event, element in ET.iterparse(dat_path, events=("end",)):
        if element.tag in {"game", "machine"} and element.get("name") == game_name:
            return element
        if element.tag in {"game", "machine"}:
            element.clear()
    return None


def verify_arcade(fixture: dict[str, Any], game_path: Path, errors: list[str]) -> str:
    dat_path = REPO_ROOT / fixture["datPath"]
    if not dat_path.is_file():
        errors.append(f"{fixture['core']}: missing DAT {fixture['datPath']}")
        return "DAT missing"

    game_name = game_path.stem
    game = find_dat_game(dat_path, game_name)
    if game is None:
        errors.append(f"{fixture['core']}: {game_name!r} not present in {fixture['datPath']}")
        return "game absent from DAT"

    dependencies = {
        key: game.get(key)
        for key in ("cloneof", "romof")
        if game.get(key)
    }
    if dependencies:
        errors.append(
            f"{fixture['core']}: smoke fixture unexpectedly depends on {dependencies}; "
            "add and verify the parent/BIOS archive before using it"
        )

    with zipfile.ZipFile(game_path) as archive:
        actual = {
            info.filename: {
                "size": info.file_size,
                "crc": f"{info.CRC & 0xFFFFFFFF:08x}",
            }
            for info in archive.infolist()
            if not info.is_dir()
        }

    required = []
    matched = 0
    for rom in game.findall("rom"):
        name = rom.get("name")
        if not name or rom.get("status") == "nodump":
            continue
        required.append(name)
        entry = actual.get(name)
        if entry is None:
            errors.append(f"{fixture['core']}: missing ROM member {name}")
            continue
        expected_size = rom.get("size")
        expected_crc = (rom.get("crc") or "").lower()
        size_matches = not expected_size or entry["size"] == int(expected_size)
        crc_matches = not expected_crc or entry["crc"] == expected_crc
        if not size_matches:
            errors.append(
                f"{fixture['core']}: {name} size {entry['size']}, expected {expected_size}"
            )
        if not crc_matches:
            errors.append(
                f"{fixture['core']}: {name} CRC {entry['crc']}, expected {expected_crc}"
            )
        if size_matches and crc_matches:
            matched += 1

    extras = sorted(set(actual) - set(required))
    return f"DAT matched {matched}/{len(required)} required ROMs; {len(extras)} extra members"


def main() -> int:
    manifest = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
    dependency_manifest = json.loads(DEPENDENCY_MANIFEST_PATH.read_text(encoding="utf-8"))
    errors: list[str] = []
    notes: list[str] = []

    if manifest.get("schemaVersion") != 2:
        errors.append("fixtures.json: expected schemaVersion 2")

    source = manifest.get("source", {})
    if "host" in source or "root" in source:
        errors.append("fixtures.json: source host/root must be supplied through environment names")

    selected_artifacts = {
        artifact["core_id"]: artifact
        for artifact in dependency_manifest["emulatorjs"]["selected_core_artifacts"]
    }

    emulatorjs = manifest["emulatorjs"]
    release_asset = dependency_manifest["emulatorjs"]["release_asset"]
    if (
        emulatorjs["version"] != dependency_manifest["emulatorjs"]["version"]
        or emulatorjs["releaseUrl"] != release_asset["url"]
        or emulatorjs["archiveSize"] != release_asset["size_bytes"]
        or emulatorjs["archiveSha256"] != release_asset["sha256"]
    ):
        errors.append("fixtures.json: EmulatorJS release does not match dependency manifest")
    require_file(
        {
            "path": emulatorjs["archivePath"],
            "size": emulatorjs["archiveSize"],
            "sha256": emulatorjs["archiveSha256"],
        },
        "EmulatorJS archive",
        errors,
    )

    seen_games: set[str] = set()
    for fixture in manifest["fixtures"]:
        core = fixture["core"]
        expected_artifact = selected_artifacts.get(core)
        actual_artifact = fixture["coreArtifact"]
        expected_suffix = (expected_artifact or {}).get("path_in_release") or (
            expected_artifact or {}
        ).get("local_path")
        if expected_artifact is None or (
            actual_artifact["size"] != expected_artifact["size_bytes"]
            or actual_artifact["sha256"] != expected_artifact["sha256"]
            or not actual_artifact["path"].endswith(expected_suffix)
        ):
            errors.append(f"{core}: core artifact does not match dependency manifest selection")

        source_relative = fixture["game"].get("sourceRelativePath", "")
        if not source_relative or Path(source_relative).is_absolute() or ".." in Path(source_relative).parts:
            errors.append(f"{core}: unsafe or missing sourceRelativePath")
        game_path = require_file(fixture["game"], f"{core} game", errors)
        source_archive_path = fixture["game"].get("sourceArchiveLocalPath")
        if source_archive_path:
            archive_path = require_file(
                {
                    "path": source_archive_path,
                    "size": fixture["game"].get("sourceArchiveSize"),
                    "sha256": fixture["game"].get("sourceArchiveSha256"),
                },
                f"{core} source archive",
                errors,
            )
            member = fixture["game"].get("extractedMember")
            if archive_path.is_file() and game_path.is_file() and member:
                try:
                    with zipfile.ZipFile(archive_path) as archive, archive.open(member) as content:
                        extracted_sha256 = digest_reader(content, "sha256")
                    if extracted_sha256 != fixture["game"]["sha256"]:
                        errors.append(
                            f"{core}: extracted member {member!r} hashes to {extracted_sha256}, "
                            f"expected {fixture['game']['sha256']}"
                        )
                except (KeyError, zipfile.BadZipFile) as error:
                    errors.append(f"{core}: unable to verify extracted member {member!r}: {error}")
        for bios in fixture["bios"]:
            bios_source = bios.get("sourceRelativePath", "")
            if not bios_source or Path(bios_source).is_absolute() or ".." in Path(bios_source).parts:
                errors.append(f"{core}: unsafe or missing BIOS sourceRelativePath")
            require_file(bios, f"{core} BIOS {bios['filename']}", errors)
        require_file(fixture["coreArtifact"], f"{core} core artifact", errors)

        game_key = str(game_path)
        if fixture.get("datPath") and game_path.is_file():
            notes.append(f"{core}: {verify_arcade(fixture, game_path, errors)}")
        elif game_key not in seen_games:
            notes.append(f"{core}: fixture hash and size matched")
        seen_games.add(game_key)

    for note in notes:
        print(f"OK  {note}")
    if errors:
        for error in errors:
            print(f"ERR {error}", file=sys.stderr)
        print(f"Fixture verification failed with {len(errors)} error(s).", file=sys.stderr)
        return 1

    print(
        f"Verified {len(manifest['fixtures'])} core fixtures against EmulatorJS "
        f"{emulatorjs['version']} and pinned DAT files."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
