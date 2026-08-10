#!/usr/bin/env python3
"""Verify immutable fixture, BIOS, core, and Arcade DAT relationships."""

from __future__ import annotations

import hashlib
import json
import re
import subprocess
import sys
import xml.etree.ElementTree as ET
import zipfile
import zlib
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
MANIFEST_PATH = Path(__file__).with_name("fixtures.json")
DEPENDENCY_MANIFEST_ROOT = REPO_ROOT / "data/dat/emulatorjs"


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


def verify_materialized_7z(fixture: dict[str, Any], game_path: Path, errors: list[str]) -> None:
    expected = fixture["game"].get("expectedMaterializedMember")
    if not expected:
        return
    try:
        result = subprocess.run(
            ["7z", "x", "-so", "-bd", str(game_path), expected["name"]],
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
    except (OSError, subprocess.CalledProcessError) as error:
        errors.append(f"{fixture['core']}: unable to extract fixed 7z member: {error}")
        return
    payload = result.stdout
    actual_crc32 = f"{zlib.crc32(payload) & 0xFFFFFFFF:08x}"
    actual_sha256 = hashlib.sha256(payload).hexdigest()
    if len(payload) != expected["size"] or actual_crc32 != expected["crc32"] or actual_sha256 != expected["sha256"]:
        errors.append(
            f"{fixture['core']}: materialized 7z member mismatch "
            f"size={len(payload)} crc32={actual_crc32} sha256={actual_sha256}"
        )


def verify_format_variants(fixture: dict[str, Any], errors: list[str]) -> None:
    variants = fixture.get("formatVariants", [])
    if fixture["core"] != "ppsspp":
        if variants:
            errors.append(f"{fixture['core']}: formatVariants is only defined for PPSSPP")
        return
    if fixture["game"].get("formatId") != "cso" or len(variants) != 1 or variants[0].get("formatId") != "iso":
        errors.append("ppsspp: expected one cso source and one iso format variant")
        return
    variant = variants[0]
    materialized = variant.get("materializedFrom", {})
    if materialized.get("format") != "CISO_V1" or materialized.get("sourceSha256") != fixture["game"]["sha256"]:
        errors.append("ppsspp: invalid ISO materializedFrom relationship")
    iso_path = require_file(variant, "ppsspp ISO variant", errors)
    if iso_path.is_file():
        with iso_path.open("rb") as iso:
            iso.seek(16 * 2048 + 1)
            if iso.read(5) != b"CD001":
                errors.append("ppsspp: ISO sector 16 does not contain CD001")


def selected_fixtures(manifest: dict, selectors: set[str]) -> tuple[list[dict], list[dict], set[str]]:
    core_fixtures = manifest.get("fixtures", [])
    multi_disc_fixtures = manifest.get("multiDiscFixtures", [])
    known = {fixture.get("core") for fixture in core_fixtures}
    known.update(fixture.get("id") for fixture in multi_disc_fixtures)
    unknown = selectors - known
    if not selectors:
        return core_fixtures, multi_disc_fixtures, unknown
    return (
        [fixture for fixture in core_fixtures if fixture.get("core") in selectors],
        [fixture for fixture in multi_disc_fixtures if fixture.get("id") in selectors],
        unknown,
    )


def main() -> int:
    manifest = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
    selectors = set(sys.argv[1:])
    core_fixtures, multi_disc_fixtures, unknown_selectors = selected_fixtures(manifest, selectors)
    dependency_manifests = {
        version: json.loads((DEPENDENCY_MANIFEST_ROOT / version / "manifest.json").read_text(encoding="utf-8"))
        for version in ("4.2.3", "4.3.0-pre")
    }
    dependency_manifest = dependency_manifests["4.2.3"]
    errors: list[str] = []
    notes: list[str] = []

    if manifest.get("schemaVersion") != 3:
        errors.append("fixtures.json: expected schemaVersion 3")
    for selector in sorted(unknown_selectors):
        errors.append(f"fixtures.json: unknown fixture selector {selector!r}")

    source = manifest.get("source", {})
    if "host" in source or "root" in source:
        errors.append("fixtures.json: source host/root must be supplied through environment names")

    selected_artifacts = {
        version: {
            artifact["core_id"]: artifact
            for artifact in version_manifest["emulatorjs"]["selected_core_artifacts"]
        }
        for version, version_manifest in dependency_manifests.items()
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
    cores_path = REPO_ROOT / emulatorjs["dataPath"] / "cores/cores.json"
    try:
        core_catalog = json.loads(cores_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        errors.append(f"unable to read EmulatorJS core catalog: {error}")
        core_catalog = []

    seen_games: set[str] = set()
    seen_cores: set[str] = set()
    for fixture in core_fixtures:
        core = fixture["core"]
        if core in seen_cores:
            errors.append(f"fixtures.json: duplicate core {core!r}")
        seen_cores.add(core)
        require_file({"path": fixture["examplePath"]}, f"{core} example", errors)
        emulator_core = fixture.get("emulatorCore", core)
        runtime_version = fixture.get("runtimeVersion", emulatorjs["version"])
        if runtime_version not in dependency_manifests:
            errors.append(f"{core}: unknown runtimeVersion {runtime_version}")
            expected_artifact = None
        else:
            expected_artifact = selected_artifacts[runtime_version].get(core)
        actual_artifact = fixture["coreArtifact"]
        if fixture.get("supportStatus") != "candidate":
            expected_suffix = (expected_artifact or {}).get("path_in_release") or (
                expected_artifact or {}
            ).get("local_path")
            if expected_artifact is None or (
                actual_artifact["size"] != expected_artifact["size_bytes"]
                or actual_artifact["sha256"] != expected_artifact["sha256"]
                or not actual_artifact["path"].endswith(expected_suffix)
            ):
                errors.append(f"{core}: core artifact does not match dependency manifest selection")
        else:
            if not any(record.get("name") == emulator_core for record in core_catalog):
                errors.append(
                    f"{core}: EmulatorJS core {emulator_core!r} is absent from {runtime_version}"
                )
            expected_prefix = f"{emulatorjs['dataPath']}/cores/{emulator_core}-"
            if not actual_artifact["path"].startswith(expected_prefix):
                errors.append(
                    f"{core}: candidate artifact is outside the pinned core path"
                )

        source_relative = fixture["game"].get("sourceRelativePath", "")
        if not source_relative or Path(source_relative).is_absolute() or ".." in Path(source_relative).parts:
            errors.append(f"{core}: unsafe or missing sourceRelativePath")
        game_path = require_file(fixture["game"], f"{core} game", errors)
        verify_materialized_7z(fixture, game_path, errors)
        verify_format_variants(fixture, errors)
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
        for runtime_file in fixture.get("runtimeFiles", []):
            require_file(runtime_file, f"{core} runtime file", errors)

        game_key = str(game_path)
        if fixture.get("datPath") and game_path.is_file():
            notes.append(f"{core}: {verify_arcade(fixture, game_path, errors)}")
        elif game_key not in seen_games:
            notes.append(f"{core} ({runtime_version}): fixture hash and size matched")
        seen_games.add(game_key)

    seen_multidisc_ids: set[str] = set()
    sha256_pattern = re.compile(r"^[0-9a-f]{64}$")
    for fixture in multi_disc_fixtures:
        fixture_id = fixture.get("id", "")
        kind = fixture.get("kind")
        if not fixture_id or fixture_id in seen_multidisc_ids:
            errors.append(f"fixtures.json: duplicate or missing multi-disc fixture ID {fixture_id!r}")
        seen_multidisc_ids.add(fixture_id)
        if kind not in {"RUNTIME_POSITIVE", "CAPABILITY_NEGATIVE"}:
            errors.append(f"{fixture_id}: unsupported multi-disc fixture kind {kind!r}")
        source_playlist = fixture.get("sourcePlaylist", {})
        if not isinstance(source_playlist.get("size"), int) or source_playlist.get("size", 0) <= 0 \
                or not sha256_pattern.fullmatch(source_playlist.get("sha256", "")):
            errors.append(f"{fixture_id}: invalid source playlist evidence")
        discs = fixture.get("discs", [])
        if not 2 <= len(discs) <= 8 or [disc.get("index") for disc in discs] != list(range(len(discs))):
            errors.append(f"{fixture_id}: disc indexes must be contiguous with a count from 2 to 8")
        total_bytes = 0
        for disc in discs:
            size = disc.get("size")
            sha256 = disc.get("sha256", "")
            if not isinstance(size, int) or size <= 0 or not sha256_pattern.fullmatch(sha256):
                errors.append(f"{fixture_id}: invalid disc evidence at index {disc.get('index')!r}")
                continue
            total_bytes += size
            if kind == "RUNTIME_POSITIVE":
                require_file(disc, f"{fixture_id} disc {disc['index'] + 1}", errors)
            elif "localPath" in disc:
                errors.append(f"{fixture_id}: capability-only fixture must not require proprietary local bytes")
        if total_bytes > 1_073_741_824:
            errors.append(f"{fixture_id}: disc bytes exceed the product multi-disc limit")

        if kind == "RUNTIME_POSITIVE":
            core = fixture.get("core")
            runtime_version = fixture.get("runtimeVersion")
            selected = selected_artifacts.get(runtime_version, {}).get(core)
            adapter = dependency_manifests.get(runtime_version, {}).get("emulatorjs", {}).get("player_adapter", {})
            multi_disc = (selected or {}).get("multi_disc")
            supported = (selected or {}).get("supported_content_kinds", [])
            if core != "yabause" or fixture.get("system") != "saturn" \
                    or fixture.get("playerAdapterId") != adapter.get("id") \
                    or "MULTI_DISC_M3U_V1" not in supported or not multi_disc:
                errors.append(f"{fixture_id}: runtime capability does not match the pinned Saturn artifact")
            require_file({"path": fixture.get("examplePath")}, f"{fixture_id} example", errors)
            require_file(fixture.get("runtimePlaylist", {}), f"{fixture_id} canonical playlist", errors)
            notes.append(f"{fixture_id}: registered canonical playlist and {len(discs)} ordered discs")
        else:
            for core in fixture.get("cores", []):
                selected = selected_artifacts.get(fixture.get("runtimeVersion"), {}).get(core, {})
                if "MULTI_DISC_M3U_V1" in selected.get("supported_content_kinds", []) or selected.get("multi_disc"):
                    errors.append(f"{fixture_id}: negative core {core} unexpectedly advertises multi-disc")
            notes.append(f"{fixture_id}: capability remains disabled without requiring proprietary bytes")

    for note in notes:
        print(f"OK  {note}")
    if errors:
        for error in errors:
            print(f"ERR {error}", file=sys.stderr)
        print(f"Fixture verification failed with {len(errors)} error(s).", file=sys.stderr)
        return 1

    runtime_versions = ", ".join(
        sorted(
            {
                fixture.get("runtimeVersion", emulatorjs["version"])
                for fixture in core_fixtures
            }
        )
    )
    print(
        f"Verified {len(core_fixtures)} core fixtures against EmulatorJS "
        f"{runtime_versions or emulatorjs['version']}, {len(multi_disc_fixtures)} multi-disc fixtures, "
        "and pinned DAT files."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
