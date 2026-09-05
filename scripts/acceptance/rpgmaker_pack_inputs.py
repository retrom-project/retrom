#!/usr/bin/env python3
"""Generate Retrom-owned runtime-pack inputs for ACC-RPG-009.

The generated payloads contain no RPG Maker or vendor RTP bytes.  They are
small, deterministic images and text authored by Retrom solely to exercise the
runtime-pack product boundary.  The output directory must be a new absolute
path so an acceptance run can never overwrite operator data.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import shutil
import struct
import subprocess
import sys
import zlib
from pathlib import Path
from zipfile import ZIP_DEFLATED, ZipFile, ZipInfo


SOURCE_NOTE = "Retrom-owned ACC-RPG-009 deterministic fixture; no vendor RTP bytes"
PNG_SIGNATURE = b"\x89PNG\r\n\x1a\n"
ROOT = Path(__file__).resolve().parents[2]
PROJECT_FIXTURES = ROOT / "testdata" / "public-roms" / "rpgmaker-smoke"


def png(marker: str, color: tuple[int, int, int]) -> bytes:
    """Return a valid deterministic 1x1 RGBA PNG with a Retrom text marker."""
    def chunk(kind: bytes, body: bytes) -> bytes:
        checksum = zlib.crc32(kind + body) & 0xFFFFFFFF
        return struct.pack(">I", len(body)) + kind + body + struct.pack(">I", checksum)

    header = struct.pack(">IIBBBBB", 1, 1, 8, 6, 0, 0, 0)
    text = b"Comment\0" + marker.encode("ascii")
    scanline = b"\0" + bytes((*color, 255))
    return PNG_SIGNATURE + chunk(b"IHDR", header) + chunk(b"tEXt", text) + \
        chunk(b"IDAT", zlib.compress(scanline, level=9)) + chunk(b"IEND", b"")


def write_payload(root: Path, entries: dict[str, bytes]) -> None:
    for relative_path, contents in sorted(entries.items()):
        target = root / relative_path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(contents)


def write_zip(target: Path, entries: dict[str, bytes]) -> None:
    with ZipFile(target, "w", compression=ZIP_DEFLATED, compresslevel=9) as archive:
        for relative_path, contents in sorted(entries.items()):
            info = ZipInfo(relative_path, date_time=(1980, 1, 1, 0, 0, 0))
            info.compress_type = ZIP_DEFLATED
            info.create_system = 3
            info.external_attr = 0o100644 << 16
            archive.writestr(info, contents, compress_type=ZIP_DEFLATED, compresslevel=9)


def write_seven_zip(target: Path, entries: dict[str, bytes], seven_zip: str) -> None:
    stage = target.parent / f".{target.stem}-source"
    stage.mkdir()
    write_payload(stage, entries)
    command = [
        seven_zip, "a", "-t7z", "-m0=lzma2", "-mx=5", "-mmt=off",
        "-mtm=off", "-mta=off", "-mtc=off", str(target), *sorted(entries),
    ]
    subprocess.run(command, cwd=stage, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    subprocess.run(
        [seven_zip, "t", str(target)], check=True,
        stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
    )
    shutil.rmtree(stage)


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


def directory_identity(root: Path) -> tuple[int, int, str]:
    files = sorted(path for path in root.rglob("*") if path.is_file())
    digest = hashlib.sha256(b"RETROM_ACC_RPG_009_INPUT_V1\0")
    total = 0
    for path in files:
        relative_path = path.relative_to(root).as_posix().encode("utf-8")
        contents = path.read_bytes()
        total += len(contents)
        digest.update(len(relative_path).to_bytes(4, "big"))
        digest.update(relative_path)
        digest.update(hashlib.sha256(contents).digest())
        digest.update(len(contents).to_bytes(8, "big"))
    return len(files), total, digest.hexdigest()


def input_row(
    identifier: str,
    source: Path,
    source_type: str,
    definition_id: str | None,
    generation: str | None = None,
    declared_name: str | None = None,
) -> dict[str, object]:
    if source_type == "DIRECTORY":
        file_count, size_bytes, digest = directory_identity(source)
    else:
        file_count, size_bytes, digest = 1, source.stat().st_size, sha256(source)
    return {
        "id": identifier,
        "sourcePath": str(source.resolve()),
        "sourceType": source_type,
        "definitionId": definition_id,
        "generation": generation,
        "declaredName": declared_name,
        "sourceNote": SOURCE_NOTE,
        "sourceFileCount": file_count,
        "sourceSizeBytes": size_bytes,
        "sourceSha256": digest,
    }


def prepare_review_project(
    destination: Path,
    source_name: str,
    ini_name: str,
    replacements: dict[str, str],
) -> None:
    shutil.copytree(PROJECT_FIXTURES / source_name, destination)
    ini = destination / ini_name
    contents = ini.read_text(encoding="utf-8")
    for before, after in replacements.items():
        if contents.count(before) != 1:
            raise ValueError(f"review fixture source drift: {source_name}/{ini_name}")
        contents = contents.replace(before, after)
    contents += f"; Retrom acceptance scenario: ACC-RPG-009/{destination.name}\n"
    ini.write_text(contents, encoding="utf-8", newline="\n")


def review_projects(output: Path) -> dict[str, dict[str, str]]:
    root = output / "review-projects"
    specifications = {
        "rpg2000SelfContained": ("rpg2000", "RPG_RT.ini", {}),
        "rpg2000Missing": ("rpg2000", "RPG_RT.ini", {"FullPackageFlag=1": "FullPackageFlag=0"}),
        "rpg2003SelfContained": ("rpg2003", "RPG_RT.ini", {}),
        "rpg2003Missing": ("rpg2003", "RPG_RT.ini", {"FullPackageFlag=1": "FullPackageFlag=0"}),
        "rpgxpNoRtp": ("rpgxp", "Game.ini", {}),
        "rpgxpStandardAmbiguous": ("rpgxp", "Game.ini", {"Title=RETROM RPGXP": "Title=RETROM RPGXP\nRTP1=Standard"}),
        "rpgxpCustom": ("rpgxp", "Game.ini", {"Title=RETROM RPGXP": "Title=RETROM RPGXP\nRTP1=RetromCustomXP"}),
        "rpgvxNoRtp": ("rpgvx", "Game.ini", {}),
        "rpgvxStandardAmbiguous": ("rpgvx", "Game.ini", {"Title=RETROM RPGVX": "Title=RETROM RPGVX\nRTP1=RPGVX"}),
        "rpgvxCustom": ("rpgvx", "Game.ini", {"Title=RETROM RPGVX": "Title=RETROM RPGVX\nRTP1=RetromCustomVX"}),
        "rpgvxaceNoRtp": ("rpgvxace", "Game.ini", {}),
        "rpgvxaceStandardAmbiguous": ("rpgvxace", "Game.ini", {"Title=RETROM RPGVXACE": "Title=RETROM RPGVXACE\nRTP1=RPGVXAce"}),
        "rpgvxaceCustom": ("rpgvxace", "Game.ini", {"Title=RETROM RPGVXACE": "Title=RETROM RPGVXACE\nRTP1=RetromCustomVXAce"}),
    }
    result = {}
    for role, (source_name, ini_name, replacements) in specifications.items():
        destination = root / role
        prepare_review_project(destination, source_name, ini_name, replacements)
        _, _, digest = directory_identity(destination)
        result[role] = {"sourcePath": str(destination.resolve()), "sourceSha256": digest}
    return result


def protected_projects(output: Path) -> dict[str, dict[str, str]]:
    root = output / "protected-projects"
    specifications = {
        "publishedVariant": (
            "rpgxp", {"Title=RETROM RPGXP": "Title=RETROM ACC009 PROTECTED XP\nRTP1=Standard"},
        ),
        "restorableCheckpoint": (
            "rpgvx", {"Title=RETROM RPGVX": "Title=RETROM ACC009 PROTECTED VX\nRTP1=RPGVX"},
        ),
    }
    result = {}
    for role, (source_name, replacements) in specifications.items():
        destination = root / role
        prepare_review_project(destination, source_name, "Game.ini", replacements)
        _, _, digest = directory_identity(destination)
        result[role] = {"sourcePath": str(destination.resolve()), "sourceSha256": digest}
    return result


def generate(output: Path, seven_zip: str) -> dict[str, object]:
    if not output.is_absolute() or output.exists():
        raise ValueError("output must be a new absolute path")
    output.mkdir(parents=True)

    easy_2000 = output / "rpg2000-directory"
    write_payload(easy_2000, {
        "RTP/Backdrop/Bridge.png": png("RETROM ACC-RPG-009 RPG2000 RTP", (24, 88, 160)),
    })
    easy_2003 = output / "rpg2003.zip"
    write_zip(easy_2003, {
        "RPG2003-RTP/Backdrop/Bridge.png": png("RETROM ACC-RPG-009 RPG2003 RTP", (36, 132, 108)),
    })

    rgss_1_v1 = output / "rgss1-standard-v1.7z"
    write_seven_zip(rgss_1_v1, {
        "RGSS1/Graphics/Characters/retrom-standard-v1.png": png(
            "RETROM ACC-RPG-009 RGSS1 STANDARD V1", (48, 112, 224),
        ),
    }, seven_zip)
    rgss_2 = output / "rgss2-rpgvx.zip"
    write_zip(rgss_2, {
        "RGSS2/Graphics/Characters/retrom-rpgvx.png": png(
            "RETROM ACC-RPG-009 RGSS2 RPGVX", (144, 72, 208),
        ),
    })
    rgss_3 = output / "rgss3-rpgvxace.7z"
    write_seven_zip(rgss_3, {
        "RGSS3/Graphics/Characters/retrom-rpgvxace.png": png(
            "RETROM ACC-RPG-009 RGSS3 RPGVXACE", (224, 64, 96),
        ),
    }, seven_zip)
    custom_xp = output / "rgss1-custom-xp.zip"
    write_zip(custom_xp, {
        "RetromCustom/Graphics/Characters/retrom-custom.png": png(
            "RETROM ACC-RPG-009 RGSS1 CUSTOM", (232, 144, 32),
        ),
    })
    rgss_1_v2 = output / "rgss1-standard-v2.7z"
    write_seven_zip(rgss_1_v2, {
        "RGSS1/Graphics/Characters/retrom-standard-v2.png": png(
            "RETROM ACC-RPG-009 RGSS1 STANDARD V2", (24, 160, 216),
        ),
    }, seven_zip)

    rgss_2_v2 = output / "rgss2-rpgvx-v2.7z"
    write_seven_zip(rgss_2_v2, {
        "RGSS2/Graphics/Characters/retrom-rpgvx-v2.png": png(
            "RETROM ACC-RPG-009 RGSS2 RPGVX V2", (176, 88, 224),
        ),
    }, seven_zip)
    custom_vx = output / "rgss2-custom-vx.zip"
    write_zip(custom_vx, {
        "RetromCustom/Graphics/Characters/retrom-custom-vx.png": png(
            "RETROM ACC-RPG-009 RGSS2 CUSTOM", (200, 112, 32),
        ),
    })
    rgss_3_v2 = output / "rgss3-rpgvxace-v2.zip"
    write_zip(rgss_3_v2, {
        "RGSS3/Graphics/Characters/retrom-rpgvxace-v2.png": png(
            "RETROM ACC-RPG-009 RGSS3 RPGVXACE V2", (240, 88, 128),
        ),
    })
    custom_vxace = output / "rgss3-custom-vxace.7z"
    write_seven_zip(custom_vxace, {
        "RetromCustom/Graphics/Characters/retrom-custom-vxace.png": png(
            "RETROM ACC-RPG-009 RGSS3 CUSTOM", (208, 96, 48),
        ),
    }, seven_zip)
    zero_reference = output / "zero-reference.zip"
    write_zip(zero_reference, {
        "RetromZero/Graphics/Characters/retrom-zero.png": png(
            "RETROM ACC-RPG-009 ZERO REFERENCE", (96, 112, 128),
        ),
    })
    protected_xp = output / "protected-rgss1-standard.zip"
    write_zip(protected_xp, {
        "RGSS1/Graphics/Characters/retrom-protected-xp.png": png(
            "RETROM ACC-RPG-009 PROTECTED XP", (32, 72, 176),
        ),
    })
    protected_vx = output / "protected-rgss2-rpgvx.7z"
    write_seven_zip(protected_vx, {
        "RGSS2/Graphics/Characters/retrom-protected-vx.png": png(
            "RETROM ACC-RPG-009 PROTECTED VX", (112, 48, 176),
        ),
    }, seven_zip)

    rows = [
        input_row("rpg2000Rtp", easy_2000, "DIRECTORY", "rpg2000_rtp"),
        input_row("rpg2003Rtp", easy_2003, "FILES", "rpg2003_rtp"),
        input_row("rgss1StandardV1", rgss_1_v1, "FILES", "rgss1_standard"),
        input_row("rgss1StandardV2", rgss_1_v2, "FILES", "rgss1_standard"),
        input_row("rgss1Custom", custom_xp, "FILES", None, "RPGXP", "RetromCustomXP"),
        input_row("rgss2StandardV1", rgss_2, "FILES", "rgss2_rpgvx"),
        input_row("rgss2StandardV2", rgss_2_v2, "FILES", "rgss2_rpgvx"),
        input_row("rgss2Custom", custom_vx, "FILES", None, "RPGVX", "RetromCustomVX"),
        input_row("rgss3StandardV1", rgss_3, "FILES", "rgss3_rpgvxace"),
        input_row("rgss3StandardV2", rgss_3_v2, "FILES", "rgss3_rpgvxace"),
        input_row("rgss3Custom", custom_vxace, "FILES", None, "RPGVXACE", "RetromCustomVXAce"),
        input_row("zeroReference", zero_reference, "FILES", None, "RPGXP", "RetromZeroReference"),
    ]
    protected_rows = {
        "publishedVariant": input_row(
            "protectedPublishedVariant", protected_xp, "FILES", "rgss1_standard",
        ),
        "restorableCheckpoint": input_row(
            "protectedRestorableCheckpoint", protected_vx, "FILES", "rgss2_rpgvx",
        ),
    }
    result = {
        "schemaVersion": 1,
        "license": "MIT",
        "licenseSource": "testdata/public-roms/rpgmaker-smoke/LICENSE",
        "copyright": "Copyright (c) 2026 Retrom contributors",
        "inputs": {str(row.pop("id")): row for row in rows},
        "reviewProjects": review_projects(output),
        "protectedPackInputs": {
            role: {key: value for key, value in row.items() if key != "id"}
            for role, row in protected_rows.items()
        },
        "protectedProjects": protected_projects(output),
    }
    (output / "inputs.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    return result


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--seven-zip", default=shutil.which("7z") or shutil.which("7zz"))
    args = parser.parse_args()
    if not args.seven_zip:
        parser.error("7z or 7zz is required")
    return args


def main() -> int:
    args = parse_args()
    try:
        result = generate(args.output, args.seven_zip)
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        return 1
    print(args.output / "inputs.json")
    print(" ".join(result["inputs"]))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
