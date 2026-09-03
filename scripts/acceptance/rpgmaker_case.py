#!/usr/bin/env python3
"""Fail-closed launcher and evidence validator for ACC-RPG product cases."""

from __future__ import annotations

import hashlib
import json
import os
import re
import struct
import subprocess
import sys
import unicodedata
import zlib
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import urlparse


ROOT = Path(__file__).resolve().parents[2]
UUID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")
SHA256 = re.compile(r"^[0-9a-f]{64}$")
GATES = (
    "RUNTIME_READY", "ENGINE_PROFILE", "FRAMES_300", "INPUT", "AUDIO",
    "INITIAL_POSITION_RECORDED", "SAVE_POINT_RECORDED", "CHECKPOINT_CREATED",
    "POST_SAVE_STATE_DIVERGED", "ORIGINAL_LAUNCH_ENDED", "RESTORE_STARTED",
    "RESTORE_POSITION_VERIFIED", "RESTORE_SCREENSHOT", "RESTORE_INPUT",
)
ENGINE_PROFILES = {
    "RPG2000": "rpg2k", "RPG2003": "rpg2k3", "RPGXP": "rgss1", "RPGVX": "rgss2",
    "RPGVXACE": "rgss3", "RPGMV": "RPGMV", "RPGMZ": "RPGMZ",
}
MARKERS = {
    "RPG2000": ("RETROM RPG2000", (6, 25, 19)),
    "RPG2003": ("RETROM RPG2003", (33, 21, 1)),
    "RPGXP": ("RETROM RPGXP", (59, 130, 246)),
    "RPGVX": ("RETROM RPGVX", (168, 85, 247)),
    "RPGVXACE": ("RETROM RPGVXACE", (244, 63, 94)),
    "RPGMV": ("RETROM RPGMV", (64, 208, 255)),
    "RPGMZ": ("RETROM RPGMZ", None),
}
LCF_SOURCE_ACCENTS = {
    "RPG2000": (45, 180, 138),
    "RPG2003": (245, 158, 11),
}
MZ_LICENSE_BASES = {
    "OPEN_SOURCE_LICENSE", "AUTHOR_PERMISSION", "OPERATOR_OWNED_PROJECT",
    "RPG_MAKER_MZ_OFFICIAL_SAMPLE_MODIFICATION_TERMS",
}
MZ_SOURCE_SIZE_BYTES = 98_413_632
MZ_SCENE_EXCLUSION = {"x": 24, "y": 24, "width": 360, "height": 72}
XP_STATE_BYTES = 268_435_456
RPG_SAVE_REQUEST_LIMIT_BYTES = 283_115_520
USER_CORE_ID = "rpgmaker"
SECURITY_MATRIX_PATH = ROOT / "testdata/public-roms/rpgmaker-smoke/negative-matrix/matrix.json"


@dataclass(frozen=True)
class GenerationCase:
    core_id: str
    generation: str
    evidence_generation: str | None
    confidence: str
    target_id: str
    fixture_directory: str | None


GENERATION_CASES = {
    "ACC-RPG-002": GenerationCase(USER_CORE_ID, "RPG2000", "RPG2000", "MATCHED", "rpgmaker-2000", "rpg2000"),
    "ACC-RPG-003": GenerationCase(USER_CORE_ID, "RPG2003", "RPG2003", "MATCHED", "rpgmaker-2003", "rpg2003"),
    "ACC-RPG-004": GenerationCase(USER_CORE_ID, "RPGXP", "RPGXP", "MATCHED", "rpgmaker-xp", "rpgxp"),
    "ACC-RPG-005": GenerationCase(USER_CORE_ID, "RPGVX", "RPGVX", "MATCHED", "rpgmaker-vx", "rpgvx"),
    "ACC-RPG-006": GenerationCase(USER_CORE_ID, "RPGVXACE", "RPGVXACE", "MATCHED", "rpgmaker-vx-ace", "rpgvxace"),
    "ACC-RPG-007": GenerationCase(USER_CORE_ID, "RPGMV", "RPGMV", "MATCHED", "rpgmaker-mv", "rpgmv"),
    "ACC-RPG-008": GenerationCase(USER_CORE_ID, "RPGMZ", "RPGMZ", "MATCHED", "rpgmaker-mz", None),
}
PACK_CASE = "ACC-RPG-009"
COMPATIBILITY_CASE = "ACC-RPG-012"
SECURITY_CASES = {"ACC-RPG-010", "ACC-RPG-011"}
DEFERRED_CASES = {
    COMPATIBILITY_CASE: "RPG_SECOND_RUNTIME_RELEASE_REQUIRED",
}
MINIMAL_CLOSURE_CASES = {PACK_CASE, "ACC-RPG-010", "ACC-RPG-011"}
ISOLATION_TARGETS = {
    "RPGMV": "rpgmaker-mv",
    "RPGMZ": "rpgmaker-mz",
}
SECURITY_TARGETS = {
    "RPG2000": "rpgmaker-2000", "RPG2003": "rpgmaker-2003", "RPGXP": "rpgmaker-xp",
    "RPGVX": "rpgmaker-vx", "RPGVXACE": "rpgmaker-vx-ace", "RPGMV": "rpgmaker-mv",
    "RPGMZ": "rpgmaker-mz",
}
SECURITY_UNSAFE = {
    "dual-root": (False, 409, "RPG_PROJECT_ROOT_AMBIGUOUS"),
    "multi-generation": (False, 409, "RPG_GENERATION_AMBIGUOUS"),
    "rgss-conflict": (False, 422, "RPG_RGSS_GENERATION_CONFLICT"),
    "lcf-truncated": (False, 422, "RPG_LCF_INVALID"),
    "case-collision": (False, 422, "RPG_PATH_COLLISION"),
    "nfkc-collision": (False, 422, "RPG_PATH_COLLISION"),
    "gencache-collision": (False, 409, "IMPORT_INPUT_INVALID"),
    "traversal": (False, 409, "IMPORT_INPUT_INVALID"),
    "symlink": (False, 409, "IMPORT_INPUT_INVALID"),
    "bomb": (False, 413, "ARCHIVE_LIMIT_EXCEEDED"),
    "external": (False, 422, "RPG_NATIVE_DEPENDENCY_UNSUPPORTED"),
    "referenced-native": (False, 422, "RPG_NATIVE_DEPENDENCY_UNSUPPORTED"),
    "opaque-native": (True, 202, None),
}
PACK_SOURCE_NOTE = "Retrom-owned ACC-RPG-009 deterministic fixture; no vendor RTP bytes"
PACK_UPLOAD_ROLES = {
    "rpg2000Rtp": ("RPG2000_RTP", None, None),
    "rpg2003Rtp": ("RPG2003_RTP", None, None),
    "rgss1StandardV1": ("RGSS1_RTP_STANDARD", None, None),
    "rgss1StandardV2": ("RGSS1_RTP_STANDARD", None, None),
    "rgss1Custom": ("RGSS_CUSTOM_RTP", "RPGXP", "RetromCustomXP"),
    "rgss2StandardV1": ("RGSS2_RTP_RPGVX", None, None),
    "rgss2StandardV2": ("RGSS2_RTP_RPGVX", None, None),
    "rgss2Custom": ("RGSS_CUSTOM_RTP", "RPGVX", "RetromCustomVX"),
    "rgss3StandardV1": ("RGSS3_RTP_RPGVXAce", None, None),
    "rgss3StandardV2": ("RGSS3_RTP_RPGVXAce", None, None),
    "rgss3Custom": ("RGSS_CUSTOM_RTP", "RPGVXACE", "RetromCustomVXAce"),
    "zeroReference": ("RGSS_CUSTOM_RTP", "RPGXP", "RetromZeroReference"),
}
PACK_REVIEW_ROLES = {
    "rpg2000SelfContained", "rpg2000Missing", "rpg2003SelfContained", "rpg2003Missing",
    "rpgxpNoRtp", "rpgxpStandardAmbiguous", "rpgxpCustom",
    "rpgvxNoRtp", "rpgvxStandardAmbiguous", "rpgvxCustom",
    "rpgvxaceNoRtp", "rpgvxaceStandardAmbiguous", "rpgvxaceCustom",
}


class ContractError(RuntimeError):
    """A product observation failed the formal acceptance contract."""


def length_prefixed(value: str) -> bytes:
    encoded = value.encode("utf-8")
    return len(encoded).to_bytes(4, "big") + encoded


def project_digest(root: Path) -> tuple[str, int, int]:
    """Compute RETROM_FILESET_V1 for a clean, already-selected project root."""
    if not root.is_dir():
        raise ContractError("RPG_ACCEPTANCE_FIXTURE_ROOT_NOT_DIRECTORY")
    entries = list(root.rglob("*"))
    if any(path.is_symlink() for path in entries):
        raise ContractError("RPG_ACCEPTANCE_FIXTURE_SYMLINK_FORBIDDEN")
    files = [path for path in entries if path.is_file()]
    if not files or len(files) > 10_000:
        raise ContractError("RPG_ACCEPTANCE_FIXTURE_FILE_COUNT_INVALID")
    normalized_files = sorted(
        (unicodedata.normalize("NFC", path.relative_to(root).as_posix()), path) for path in files
    )
    digest = hashlib.sha256(b"RETROM_FILESET_V1\0")
    total = 0
    logical_names: set[str] = set()
    for logical_name, path in normalized_files:
        if logical_name in logical_names:
            raise ContractError("RPG_ACCEPTANCE_FIXTURE_PATH_COLLISION")
        logical_names.add(logical_name)
        size = path.stat().st_size
        blob_digest = hashlib.sha256()
        with path.open("rb") as source:
            while chunk := source.read(1024 * 1024):
                blob_digest.update(chunk)
        total += size
        digest.update(length_prefixed("PROJECT_FILE"))
        digest.update(length_prefixed(logical_name))
        digest.update(blob_digest.digest())
        digest.update(size.to_bytes(8, "big"))
        digest.update(b"\0")
    return digest.hexdigest(), len(files), total


def web_engine_version(root: Path, generation: str) -> str | None:
    relative = {"RPGMV": "js/rpg_core.js", "RPGMZ": "js/rmmz_core.js"}.get(generation)
    if relative is None:
        return None
    path = root / relative
    if not path.is_file() or path.is_symlink() or path.stat().st_size > 4 * 1024 * 1024:
        raise ContractError("RPG_ACCEPTANCE_WEB_ENGINE_SOURCE_INVALID")
    source = path.read_text(encoding="utf-8")
    matches = re.findall(r"Utils\.RPGMAKER_VERSION\s*=\s*['\"]([^'\"]{1,64})['\"]\s*;", source)
    if len(matches) != 1 or not re.fullmatch(r"[0-9]+(?:\.[0-9A-Za-z-]+){1,3}", matches[0]):
        raise ContractError("RPG_ACCEPTANCE_WEB_ENGINE_VERSION_INVALID")
    return matches[0]


def require_web_marker(root: Path, marker: str) -> None:
    inspected = 0
    marker_bytes = marker.encode("utf-8")
    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.suffix.lower() not in {".html", ".js", ".json"}:
            continue
        size = path.stat().st_size
        if size > 8 * 1024 * 1024 or inspected + size > 64 * 1024 * 1024:
            raise ContractError("RPG_ACCEPTANCE_WEB_MARKER_SCAN_LIMIT")
        inspected += size
        if marker_bytes in path.read_bytes():
            return
    raise ContractError("RPG_ACCEPTANCE_WEB_MARKER_MISSING")


def fixture_spec_path(spec: GenerationCase) -> Path:
    base = ROOT / "testdata/public-roms/rpgmaker-smoke/fixture-spec"
    if spec.generation == "RPG2000":
        return base / "rpg2000.json"
    if spec.generation == "RPG2003":
        return base / "rpg2003.json"
    if spec.generation in {"RPGXP", "RPGVX", "RPGVXACE"}:
        return base / "rgss.json"
    if spec.generation == "RPGMV":
        return base / "mv.json"
    raise ContractError("RPG_ACCEPTANCE_FIXTURE_SPEC_MISSING")


def public_fixture_marker(spec: GenerationCase) -> tuple[str, list[int], str]:
    path = fixture_spec_path(spec)
    contents = path.read_bytes()
    document = json.loads(contents)
    rows = document if isinstance(document, list) else [document]
    matches = [row for row in rows if isinstance(row, dict) and row.get("generation") == spec.generation]
    if len(matches) != 1 or matches[0].get("directory") != spec.fixture_directory:
        raise ContractError("RPG_ACCEPTANCE_FIXTURE_SPEC_INVALID")
    marker, source_rgb = matches[0].get("marker"), matches[0].get("accentRgb")
    expected_marker, expected_rgb = MARKERS[spec.generation]
    if marker != expected_marker or not isinstance(source_rgb, list) or len(source_rgb) != 3 or any(
        not isinstance(channel, int) or isinstance(channel, bool) or not 0 <= channel <= 255
        for channel in source_rgb
    ):
        raise ContractError("RPG_ACCEPTANCE_FIXTURE_SPEC_INVALID")
    if spec.generation in LCF_SOURCE_ACCENTS:
        if tuple(source_rgb) != LCF_SOURCE_ACCENTS[spec.generation]:
            raise ContractError("RPG_ACCEPTANCE_FIXTURE_SPEC_INVALID")
        rendered_rgb = expected_rgb
    else:
        rendered_rgb = tuple(source_rgb)
    if rendered_rgb is None or rendered_rgb != expected_rgb:
        raise ContractError("RPG_ACCEPTANCE_FIXTURE_SPEC_INVALID")
    return marker, list(rendered_rgb), hashlib.sha256(contents).hexdigest()


def read_json_file(path_value: str, label: str) -> dict[str, Any]:
    path = Path(path_value)
    if not path.is_absolute() or not path.is_file() or path.is_symlink() or path.stat().st_size > 64 * 1024:
        raise ContractError(f"RPG_ACCEPTANCE_{label}_INVALID")
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ContractError(f"RPG_ACCEPTANCE_{label}_INVALID")
    return value


def generation_input_provenance(
    case_id: str,
    spec: GenerationCase,
    root: Path,
    digest: str,
    file_count: int,
    total_bytes: int,
) -> dict[str, Any]:
    engine_version = web_engine_version(root, spec.generation)
    if case_id != "ACC-RPG-008":
        marker, marker_rgb, source_sha256 = public_fixture_marker(spec)
        return {
            "schemaVersion": 1, "kind": "RETROM_OWNED_PUBLIC_FIXTURE",
            "projectFingerprint": digest, "fileCount": file_count, "totalBytes": total_bytes,
            "marker": marker, "markerRgb": marker_rgb, "engineVersion": engine_version,
            "licenseBasis": "RETROM_MIT", "licenseUrl": None, "sourceUrl": None,
            "sourceVersion": "fixture-manifest-v1", "sourceSha256": source_sha256,
        }
    supplied = read_json_file(os.environ["RPG_MZ_SMOKE_PROVENANCE"], "MZ_PROVENANCE")
    expected_keys = {
        "schemaVersion", "kind", "licenseBasis", "licenseUrl", "sourceUrl", "sourceVersion",
        "sourceSha256", "marker", "markerRgb", "transformation",
    }
    if set(supplied) != expected_keys or supplied.get("schemaVersion") != 1 or \
            supplied.get("kind") != "LICENSED_EXTERNAL_WEB_DEPLOYMENT":
        raise ContractError("RPG_ACCEPTANCE_MZ_PROVENANCE_INVALID")
    require_web_marker(root, "RETROM RPGMZ")
    validate_mz_transformation(supplied.get("transformation"), digest, file_count, total_bytes)
    return {
        **supplied, "projectFingerprint": digest, "fileCount": file_count,
        "totalBytes": total_bytes, "engineVersion": engine_version,
    }


def paeth(left: int, above: int, upper_left: int) -> int:
    estimate = left + above - upper_left
    distances = abs(estimate - left), abs(estimate - above), abs(estimate - upper_left)
    if distances[0] <= distances[1] and distances[0] <= distances[2]:
        return left
    return above if distances[1] <= distances[2] else upper_left


def decode_png_pixels(contents: bytes) -> tuple[int, int, int, bytes]:
    if len(contents) < 33 or contents[:8] != b"\x89PNG\r\n\x1a\n":
        raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
    offset, width, height, color_type = 8, 0, 0, -1
    compressed = bytearray()
    seen_header = False
    while offset < len(contents):
        if offset + 12 > len(contents):
            raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
        length = struct.unpack(">I", contents[offset:offset + 4])[0]
        kind = contents[offset + 4:offset + 8]
        end = offset + 12 + length
        if end > len(contents):
            raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
        data = contents[offset + 8:offset + 8 + length]
        expected_crc = struct.unpack(">I", contents[offset + 8 + length:end])[0]
        if zlib.crc32(kind + data) & 0xFFFFFFFF != expected_crc:
            raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
        if offset == 8 and kind != b"IHDR":
            raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
        if kind == b"IHDR":
            if seen_header or length != 13:
                raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
            width, height, depth, color_type, compression, filtering, interlace = struct.unpack(">IIBBBBB", data)
            if not 320 <= width <= 4096 or not 180 <= height <= 4096 or depth != 8 or \
                    color_type not in {2, 6} or compression != 0 or filtering != 0 or interlace != 0:
                raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
            seen_header = True
        elif kind == b"IDAT":
            if not seen_header:
                raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
            compressed.extend(data)
        elif kind == b"IEND":
            if length != 0 or end != len(contents):
                raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
            offset = end
            break
        offset = end
    if offset != len(contents) or not compressed:
        raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
    channels = 3 if color_type == 2 else 4
    stride = width * channels
    expected_size = (stride + 1) * height
    inflater = zlib.decompressobj()
    filtered = inflater.decompress(bytes(compressed), expected_size + 1)
    if len(filtered) > expected_size or inflater.unconsumed_tail:
        raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
    filtered += inflater.flush()
    if len(filtered) != expected_size or not inflater.eof or inflater.unused_data:
        raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
    pixels = bytearray(width * height * channels)
    previous = bytearray(stride)
    source_offset, target_offset = 0, 0
    for _ in range(height):
        filter_kind = filtered[source_offset]
        source_offset += 1
        if filter_kind > 4:
            raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
        current = bytearray(stride)
        for index, value in enumerate(filtered[source_offset:source_offset + stride]):
            left = current[index - channels] if index >= channels else 0
            above = previous[index]
            upper_left = previous[index - channels] if index >= channels else 0
            predictor = (0, left, above, (left + above) // 2, paeth(left, above, upper_left))[filter_kind]
            current[index] = (value + predictor) & 0xFF
        pixels[target_offset:target_offset + stride] = current
        previous = current
        source_offset += stride
        target_offset += stride
    return width, height, channels, bytes(pixels)


def png_visual_evidence(
    path: Path,
    logical_path: str,
    marker: str,
    rgb: list[int],
    scene_exclusion: dict[str, int] | None = None,
) -> dict[str, Any]:
    if not path.is_file() or path.is_symlink() or path.stat().st_size > 16 * 1024 * 1024:
        raise ContractError("RPG_ACCEPTANCE_RESTORE_SCREENSHOT_PNG_INVALID")
    contents = path.read_bytes()
    width, height, channels, pixels = decode_png_pixels(contents)
    opaque, non_black, marker_pixels, scene_non_black = 0, 0, 0, 0
    buckets: set[tuple[int, int, int]] = set()
    scene_buckets: set[tuple[int, int, int]] = set()
    for pixel_index, offset in enumerate(range(0, len(pixels), channels)):
        red, green, blue = pixels[offset:offset + 3]
        alpha = pixels[offset + 3] if channels == 4 else 255
        if alpha >= 240:
            opaque += 1
            bucket = (red >> 4, green >> 4, blue >> 4)
            buckets.add(bucket)
            if max(red, green, blue) >= 16:
                non_black += 1
                x, y = pixel_index % width, pixel_index // width
                excluded = scene_exclusion is not None and \
                    scene_exclusion["x"] <= x < scene_exclusion["x"] + scene_exclusion["width"] and \
                    scene_exclusion["y"] <= y < scene_exclusion["y"] + scene_exclusion["height"]
                if not excluded:
                    scene_non_black += 1
                    scene_buckets.add(bucket)
            if max(abs(red - rgb[0]), abs(green - rgb[1]), abs(blue - rgb[2])) <= 20:
                marker_pixels += 1
    evidence = {
        "screenshot": logical_path, "sha256": hashlib.sha256(contents).hexdigest(),
        "width": width, "height": height, "opaquePixels": opaque,
        "nonBlackPixels": non_black, "distinctColorBuckets": len(buckets),
        "marker": marker, "markerRgb": rgb, "markerPixelCount": marker_pixels,
    }
    if scene_exclusion is not None:
        evidence.update({
            "sceneExclusion": dict(scene_exclusion),
            "sceneNonBlackPixels": scene_non_black,
            "sceneDistinctColorBuckets": len(scene_buckets),
        })
    return evidence


def require_position(value: Any, label: str) -> dict[str, int]:
    keys = {"mapId", "playerX", "playerY", "fixtureState"}
    if not isinstance(value, dict) or set(value) != keys or any(
        not isinstance(value[key], int) or isinstance(value[key], bool) for key in keys
    ):
        raise ContractError(f"RPG_ACCEPTANCE_{label}_POSITION_INVALID")
    return value


def require_equal(actual: Any, expected: Any, label: str) -> None:
    if actual != expected:
        raise ContractError(f"RPG_ACCEPTANCE_{label}_MISMATCH")


def valid_https_url(value: Any) -> bool:
    if not isinstance(value, str):
        return False
    parsed = urlparse(value)
    return parsed.scheme == "https" and bool(parsed.netloc) and not parsed.username and \
        not parsed.password and not parsed.fragment


def contains_secret_or_path(value: Any) -> bool:
    forbidden_keys = {
        "sourcepath", "hostpath", "password", "csrftoken", "capability", "cookie",
        "bootstrapticket", "authorization",
    }
    stack = [value]
    while stack:
        current = stack.pop()
        if isinstance(current, dict):
            if any(str(key).lower() in forbidden_keys for key in current):
                return True
            stack.extend(current.values())
        elif isinstance(current, list):
            stack.extend(current)
        elif isinstance(current, str) and (
            current.startswith(("/", "~/", "file://")) or re.match(r"^[A-Za-z]:[\\/]", current)
        ):
            return True
    return False


def validate_input_transcript(value: Any, spec: GenerationCase, target_contract: str) -> None:
    if not isinstance(value, dict) or set(value) != {"transportScheme", "upload", "import"} or \
            value.get("transportScheme") not in {"HTTPS", "HTTP_LOCALHOST"} or contains_secret_or_path(value):
        raise ContractError("RPG_ACCEPTANCE_INPUT_TRANSCRIPT_INVALID")
    upload, imported = value.get("upload"), value.get("import")
    upload_keys = {
        "uploadId", "state", "purpose", "sourceType", "fileCount", "totalBytes",
        "receivedBytes", "finalizationNo",
    }
    import_keys = {
        "importJobId", "uploadId", "state", "payloadState", "platformId", "defaultCoreId",
        "providerId", "targetId", "targetContractSha256", "counts", "createdAtMs", "updatedAtMs",
    }
    if not isinstance(upload, dict) or set(upload) != upload_keys or \
            not isinstance(imported, dict) or set(imported) != import_keys:
        raise ContractError("RPG_ACCEPTANCE_INPUT_TRANSCRIPT_INVALID")
    if not UUID.fullmatch(str(upload.get("uploadId"))) or upload.get("state") != "COMPLETE" or \
            upload.get("purpose") != "RPG_MAKER_PROJECT" or upload.get("sourceType") != "DIRECTORY" or \
            not isinstance(upload.get("fileCount"), int) or upload["fileCount"] < 1 or \
            not isinstance(upload.get("totalBytes"), int) or upload["totalBytes"] < 1 or \
            upload.get("receivedBytes") != upload["totalBytes"] or \
            not isinstance(upload.get("finalizationNo"), int) or upload["finalizationNo"] < 1:
        raise ContractError("RPG_ACCEPTANCE_INPUT_TRANSCRIPT_INVALID")
    counts = imported.get("counts")
    expected_count_keys = {
        "total", "queued", "running", "reviewPending", "published", "discarded", "failed",
        "cancelled", "unresolvedRejectedFiles",
    }
    if not UUID.fullmatch(str(imported.get("importJobId"))) or \
            imported.get("uploadId") != upload["uploadId"] or imported.get("state") != "COMPLETED" or \
            imported.get("payloadState") not in {"RETAINED", "RELEASING", "RELEASED"} or \
            imported.get("platformId") != "rpgmaker" or imported.get("defaultCoreId") != USER_CORE_ID or \
            imported.get("providerId") != "retrom-runtime" or imported.get("targetId") != spec.target_id or \
            imported.get("targetContractSha256") != target_contract or not isinstance(counts, dict) or \
            set(counts) != expected_count_keys or counts.get("total") != 1 or counts.get("published") != 1 or \
            any(counts.get(key) != 0 for key in expected_count_keys - {"total", "published"}):
        raise ContractError("RPG_ACCEPTANCE_INPUT_TRANSCRIPT_INVALID")
    created, updated = imported.get("createdAtMs"), imported.get("updatedAtMs")
    if not isinstance(created, int) or not isinstance(updated, int) or created < 0 or updated < created:
        raise ContractError("RPG_ACCEPTANCE_INPUT_TRANSCRIPT_INVALID")


def validate_input_provenance(value: Any, spec: GenerationCase, digest: str) -> tuple[str, list[int]]:
    error_code = (
        "RPG_ACCEPTANCE_MZ_INPUT_PROVENANCE_INVALID"
        if spec.generation == "RPGMZ" else "RPG_ACCEPTANCE_INPUT_PROVENANCE_INVALID"
    )
    keys = {
        "schemaVersion", "kind", "projectFingerprint", "fileCount", "totalBytes", "marker",
        "markerRgb", "engineVersion", "licenseBasis", "licenseUrl", "sourceUrl", "sourceVersion",
        "sourceSha256",
    }
    if spec.generation == "RPGMZ":
        keys.add("transformation")
    if not isinstance(value, dict) or set(value) != keys or value.get("schemaVersion") != 1 or \
            value.get("projectFingerprint") != digest or not SHA256.fullmatch(str(value.get("sourceSha256"))) or \
            not isinstance(value.get("fileCount"), int) or value["fileCount"] < 1 or \
            not isinstance(value.get("totalBytes"), int) or value["totalBytes"] < 1:
        raise ContractError(error_code)
    marker, expected_rgb = MARKERS[spec.generation]
    rgb = value.get("markerRgb")
    if value.get("marker") != marker or not isinstance(rgb, list) or len(rgb) != 3 or any(
        not isinstance(channel, int) or isinstance(channel, bool) or not 0 <= channel <= 255 for channel in rgb
    ) or expected_rgb is not None and tuple(rgb) != expected_rgb:
        raise ContractError(error_code)
    if spec.generation == "RPGMZ":
        if value.get("kind") != "LICENSED_EXTERNAL_WEB_DEPLOYMENT" or \
                value.get("licenseBasis") not in MZ_LICENSE_BASES or \
                not valid_https_url(value.get("licenseUrl")) or not valid_https_url(value.get("sourceUrl")) or \
                not isinstance(value.get("sourceVersion"), str) or not 1 <= len(value["sourceVersion"]) <= 120 or \
                not isinstance(value.get("engineVersion"), str) or \
                not re.fullmatch(r"[0-9]+(?:\.[0-9A-Za-z-]+){1,3}", value["engineVersion"]):
            raise ContractError(error_code)
        validate_mz_transformation(value.get("transformation"), digest, value["fileCount"], value["totalBytes"])
    elif value.get("kind") != "RETROM_OWNED_PUBLIC_FIXTURE" or \
            value.get("licenseBasis") != "RETROM_MIT" or value.get("licenseUrl") is not None or \
            value.get("sourceUrl") is not None or value.get("sourceVersion") != "fixture-manifest-v1":
        raise ContractError(error_code)
    return marker, rgb


def validate_mz_transformation(value: Any, digest: str, file_count: int, total_bytes: int) -> None:
    keys = {
        "schemaVersion", "recipe", "tool", "sourceSizeBytes", "removedEntries", "injectedFiles",
        "outputProjectFingerprint", "outputFileCount", "outputTotalBytes",
    }
    if not isinstance(value, dict) or set(value) != keys or value.get("schemaVersion") != 1 or \
            value.get("recipe") != "RETROM_MZ_MINIMAL_V3" or \
            value.get("tool") != "scripts/acceptance/rpgmaker_mz_prepare.py" or \
            value.get("sourceSizeBytes") != MZ_SOURCE_SIZE_BYTES or \
            value.get("outputProjectFingerprint") != digest or value.get("outputFileCount") != file_count or \
            value.get("outputTotalBytes") != total_bytes:
        raise ContractError("RPG_ACCEPTANCE_MZ_TRANSFORMATION_INVALID")
    removed = value.get("removedEntries")
    injected = value.get("injectedFiles")
    if not isinstance(removed, list) or len(removed) != 10 or not isinstance(injected, list) or len(injected) != 2:
        raise ContractError("RPG_ACCEPTANCE_MZ_TRANSFORMATION_INVALID")
    removed_names = {item.get("logicalName") for item in removed if isinstance(item, dict)}
    expected_saves = {"save/config.rmmzsave", "save/global.rmmzsave"} | {
        f"save/file{index}.rmmzsave" for index in range(7)
    }
    root_documents = {
        name for name in removed_names if isinstance(name, str) and "/" not in name and name.lower().endswith(".pdf")
    }
    if expected_saves | root_documents != removed_names or len(root_documents) != 1 or any(
        not isinstance(item, dict) or set(item) != {"logicalName", "reason", "sizeBytes", "sha256"} or
        not isinstance(item.get("sizeBytes"), int) or item["sizeBytes"] < 1 or
        not SHA256.fullmatch(str(item.get("sha256"))) or
        item.get("reason") != (
            "ROOT_DOCUMENTATION_EXCLUDED" if item.get("logicalName") in root_documents else "PACKAGED_SAVE_EXCLUDED"
        ) for item in removed
    ):
        raise ContractError("RPG_ACCEPTANCE_MZ_TRANSFORMATION_INVALID")
    if {item.get("logicalName") for item in injected if isinstance(item, dict)} != {
        "js/plugins.js", "js/plugins/RetromMinimalAcceptance.js",
    } or any(
        not isinstance(item, dict) or set(item) != {"logicalName", "sizeBytes", "sha256"} or
        not isinstance(item.get("sizeBytes"), int) or item["sizeBytes"] < 1 or
        not SHA256.fullmatch(str(item.get("sha256"))) for item in injected
    ):
        raise ContractError("RPG_ACCEPTANCE_MZ_TRANSFORMATION_INVALID")


def validate_restore_visual(value: Any, marker: str, rgb: list[int], require_scene: bool = False) -> None:
    keys = {
        "screenshot", "sha256", "width", "height", "opaquePixels", "nonBlackPixels",
        "distinctColorBuckets", "marker", "markerRgb", "markerPixelCount",
    }
    if require_scene:
        keys.update({"sceneExclusion", "sceneNonBlackPixels", "sceneDistinctColorBuckets"})
    if not isinstance(value, dict) or set(value) != keys or value.get("marker") != marker or \
            value.get("markerRgb") != rgb or not SHA256.fullmatch(str(value.get("sha256"))):
        raise ContractError("RPG_ACCEPTANCE_RESTORE_VISUAL_INVALID")
    screenshot = value.get("screenshot")
    width, height = value.get("width"), value.get("height")
    opaque, non_black = value.get("opaquePixels"), value.get("nonBlackPixels")
    if not isinstance(screenshot, str) or not screenshot.startswith("screenshots/") or \
            screenshot.startswith(("/", "screenshots/../")) or not screenshot.endswith(".png") or \
            not isinstance(width, int) or width < 320 or not isinstance(height, int) or height < 180 or \
            not isinstance(opaque, int) or opaque < width * height // 2 or \
            not isinstance(non_black, int) or non_black < width * height // 200 or \
            not isinstance(value.get("distinctColorBuckets"), int) or value["distinctColorBuckets"] < 3 or \
            not isinstance(value.get("markerPixelCount"), int) or value["markerPixelCount"] < 16:
        raise ContractError("RPG_ACCEPTANCE_RESTORE_VISUAL_INVALID")
    if require_scene and (
        value.get("sceneExclusion") != MZ_SCENE_EXCLUSION or
        not isinstance(value.get("sceneNonBlackPixels"), int) or
        value["sceneNonBlackPixels"] < max(2_048, width * height // 200) or
        not isinstance(value.get("sceneDistinctColorBuckets"), int) or
        value["sceneDistinctColorBuckets"] < 16
    ):
        raise ContractError("RPG_ACCEPTANCE_RESTORE_VISUAL_INVALID")


def gate_durations(gates: list[dict[str, Any]]) -> list[dict[str, int | str]]:
    result: list[dict[str, int | str]] = []
    first_begun: int | None = None
    previous_completed: int | None = None
    for gate in gates:
        begun, completed = gate.get("begunAtMs"), gate.get("completedAtMs")
        if not isinstance(begun, int) or isinstance(begun, bool) or \
                not isinstance(completed, int) or isinstance(completed, bool) or \
                begun < 0 or completed < begun or completed - begun > 300_000 or \
                previous_completed is not None and begun < previous_completed:
            raise ContractError("RPG_ACCEPTANCE_GATE_DURATION_INVALID")
        if first_begun is None:
            first_begun = begun
        previous_completed = completed
        result.append({"gate": str(gate.get("gate")), "durationMs": completed - begun})
    if first_begun is None or previous_completed is None or previous_completed - first_begun > 300_000:
        raise ContractError("RPG_ACCEPTANCE_GATE_DURATION_INVALID")
    return result


def validate_runtime_environment(value: Any, spec: GenerationCase, gates: list[dict[str, Any]]) -> None:
    keys = {"chromeVersion", "engineVersion", "engineProfile", "gateDurationsMs"}
    if not isinstance(value, dict) or set(value) != keys or \
            not re.fullmatch(r"(?:(?:Headless)?Chrome/)?[0-9]+(?:\.[0-9]+){3}",
                             str(value.get("chromeVersion"))) or \
            value.get("engineProfile") != ENGINE_PROFILES[spec.generation] or \
            value.get("gateDurationsMs") != gate_durations(gates):
        raise ContractError("RPG_ACCEPTANCE_RUNTIME_ENVIRONMENT_INVALID")
    if spec.generation in {"RPGMV", "RPGMZ"} and not isinstance(value.get("engineVersion"), str):
        raise ContractError("RPG_ACCEPTANCE_RUNTIME_ENVIRONMENT_INVALID")


def valid_compact_mkxp_checkpoint_size(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and 24 < value < XP_STATE_BYTES


def validate_xp_runtime_trace(value: Any, checkpoint: dict[str, Any], config: dict[str, Any]) -> None:
    if not isinstance(value, dict) or set(value) != {
        "schemaVersion", "checkpointUpload", "oversizeRejection", "threadCapabilityRejections",
    } or value.get("schemaVersion") != 1 or config.get("checkpointMaxBytes") != XP_STATE_BYTES or \
            config.get("checkpointFormat") != "mkxp-state-compact-v1":
        raise ContractError("RPG_ACCEPTANCE_XP_RUNTIME_TRACE_INVALID")
    upload, oversize, rejections = (
        value.get("checkpointUpload"), value.get("oversizeRejection"), value.get("threadCapabilityRejections"),
    )
    upload_length = upload.get("requestContentLengthBytes") if isinstance(upload, dict) else None
    checkpoint_size = checkpoint.get("sizeBytes")
    if not isinstance(upload, dict) or set(upload) != {
        "requestPayloadBytes", "requestContentLengthBytes", "responseStatus", "sha256",
        "startedAtMs", "finishedAtMs",
    } or not valid_compact_mkxp_checkpoint_size(checkpoint_size) or \
            upload.get("requestPayloadBytes") != checkpoint_size or not isinstance(upload_length, int) or \
            isinstance(upload_length, bool) or not checkpoint_size < upload_length <= RPG_SAVE_REQUEST_LIMIT_BYTES or \
            upload.get("responseStatus") != 201 or upload.get("sha256") != checkpoint.get("sha256") or \
            not valid_trace_times(upload):
        raise ContractError("RPG_ACCEPTANCE_XP_RUNTIME_TRACE_INVALID")
    if not isinstance(oversize, dict) or set(oversize) != {
        "declaredContentLengthBytes", "responseStatus", "errorCode", "startedAtMs", "finishedAtMs",
    } or oversize.get("declaredContentLengthBytes") != RPG_SAVE_REQUEST_LIMIT_BYTES + 1 or \
            oversize.get("responseStatus") != 413 or oversize.get("errorCode") != "REQUEST_TOO_LARGE" or \
            not valid_trace_times(oversize):
        raise ContractError("RPG_ACCEPTANCE_XP_RUNTIME_TRACE_INVALID")
    if not isinstance(rejections, list) or len(rejections) != 2 or \
            [item.get("phase") for item in rejections if isinstance(item, dict)] != ["VALIDATION", "RESTORE"]:
        raise ContractError("RPG_ACCEPTANCE_XP_RUNTIME_TRACE_INVALID")
    attempt_ids: set[str] = set()
    for item in rejections:
        keys = {
            "attemptId", "phase", "capabilities", "responseStatus", "errorCode",
            "launchCredentialIssued", "projectPayloadRequestCount",
        }
        capabilities = item.get("capabilities")
        if set(item) != keys or not UUID.fullmatch(str(item.get("attemptId"))) or \
                not isinstance(capabilities, dict) or set(capabilities) != {
                    "secureContext", "crossOriginIsolated", "sharedArrayBuffer",
                } or any(value is not False for value in capabilities.values()) or item.get("responseStatus") != 409 or \
                item.get("errorCode") != "RPG_RUNTIME_ROUTE_UNAVAILABLE" or \
                item.get("launchCredentialIssued") is not False or item.get("projectPayloadRequestCount") != 0:
            raise ContractError("RPG_ACCEPTANCE_XP_RUNTIME_TRACE_INVALID")
        attempt_ids.add(item["attemptId"])
    if len(attempt_ids) != 2:
        raise ContractError("RPG_ACCEPTANCE_XP_RUNTIME_TRACE_INVALID")


def valid_trace_times(value: dict[str, Any]) -> bool:
    started, finished = value.get("startedAtMs"), value.get("finishedAtMs")
    return isinstance(started, int) and isinstance(finished, int) and 0 <= started <= finished and \
        finished - started <= 300_000


def validate_origin_inventory(value: Any, product_launch_id: str) -> None:
    if not isinstance(value, dict) or set(value) != {"appOrigin", "runtimeOrigin", "unexpectedOrigins"} or \
            value.get("unexpectedOrigins") != []:
        raise ContractError("RPG_ACCEPTANCE_ORIGIN_INVENTORY_INVALID")
    app, runtime = value.get("appOrigin"), value.get("runtimeOrigin")
    app_keys = {
        "origin", "documentResponses", "scriptResponses", "projectResourceResponses",
        "domProjectResourceReferences", "cacheProjectResourceEntries",
    }
    runtime_keys = {"origin", "documentResponses", "scriptResponses", "projectResourceResponses"}
    if not isinstance(app, dict) or set(app) != app_keys or not isinstance(runtime, dict) or \
            set(runtime) != runtime_keys:
        raise ContractError("RPG_ACCEPTANCE_ORIGIN_INVENTORY_INVALID")
    count_keys = app_keys - {"origin"}
    runtime_count_keys = runtime_keys - {"origin"}
    if any(not isinstance(app.get(key), int) or isinstance(app.get(key), bool) for key in count_keys) or \
            any(not isinstance(runtime.get(key), int) or isinstance(runtime.get(key), bool)
                for key in runtime_count_keys):
        raise ContractError("RPG_ACCEPTANCE_ORIGIN_INVENTORY_INVALID")
    try:
        app_url, runtime_url = urlparse(app["origin"]), urlparse(runtime["origin"])
    except (TypeError, ValueError):
        raise ContractError("RPG_ACCEPTANCE_ORIGIN_INVENTORY_INVALID") from None
    if app_url.scheme not in {"http", "https"} or runtime_url.scheme not in {"http", "https"} or \
            not app_url.netloc or not runtime_url.netloc or app_url.netloc == runtime_url.netloc or \
            (app_url.scheme != "https" and not is_local_acceptance_hostname(app_url.hostname)) or \
            (runtime_url.scheme != "https" and not is_local_acceptance_hostname(runtime_url.hostname)) or \
            product_launch_id not in str(runtime_url.hostname) or \
            app.get("documentResponses", 0) < 1 or app.get("scriptResponses", 0) < 1 or \
            any(app.get(key) != 0 for key in (
                "projectResourceResponses", "domProjectResourceReferences", "cacheProjectResourceEntries",
            )) or runtime.get("documentResponses", 0) < 1 or runtime.get("scriptResponses", 0) < 1 or \
            runtime.get("projectResourceResponses", 0) < 1:
        raise ContractError("RPG_ACCEPTANCE_ORIGIN_INVENTORY_INVALID")


def is_local_acceptance_hostname(hostname: str | None) -> bool:
    return hostname in {"localhost", "127.0.0.1"} or bool(hostname and hostname.endswith(".localhost"))


def validate_runtime_loading(
    value: Any, spec: GenerationCase, product_launch_id: str, input_file_count: int,
) -> None:
    keys = {"schemaVersion", "cacheLaunchId", "sameProjectContentIdentity", "firstVisible", "cacheLaunchVisible"}
    if not isinstance(value, dict) or set(value) != keys or value.get("schemaVersion") != 1:
        raise ContractError("RPG_ACCEPTANCE_RUNTIME_LOADING_INVALID")
    cache_launch_id = value.get("cacheLaunchId")
    if not UUID.fullmatch(str(cache_launch_id)) or cache_launch_id == product_launch_id:
        raise ContractError("RPG_ACCEPTANCE_RUNTIME_LOADING_INVALID")
    first = require_loading_snapshot(value.get("firstVisible"))
    cached = require_loading_snapshot(value.get("cacheLaunchVisible"))
    if spec.generation in {"RPGMV", "RPGMZ"}:
        if value.get("sameProjectContentIdentity") is not None or any(
            snapshot["projectContentIdentityCount"] != 0 or
            snapshot["nativeProjectResponseCount"] < 1 or
            snapshot["nativeProjectResponseCount"] >= input_file_count
            for snapshot in (first, cached)
        ):
            raise ContractError("RPG_ACCEPTANCE_RUNTIME_LOADING_INVALID")
        return
    if value.get("sameProjectContentIdentity") is not True or any(
        snapshot["projectContentIdentityCount"] != 1 or snapshot["nativeProjectResponseCount"] != 0 or
        snapshot["declaredProjectBytes"] < 1 or snapshot["declaredProjectFileCount"] < 1 or
        snapshot["requestedProjectBytes"] < 1 or
        snapshot["requestedProjectBytes"] >= snapshot["declaredProjectBytes"] or
        snapshot["requestedProjectFileCount"] < 1 or
        snapshot["requestedProjectFileCount"] > snapshot["declaredProjectFileCount"] or
        snapshot["runtimeAssetRequestCount"] < 2
        for snapshot in (first, cached)
    ) or cached["runtimeAssetCacheHitCount"] < 1:
        raise ContractError("RPG_ACCEPTANCE_RUNTIME_LOADING_INVALID")
    if spec.generation in {"RPGXP", "RPGVX", "RPGVXACE"} and any(
        snapshot["declaredLargeFileCount"] < 1 or snapshot["fullProjectFileResponseCount"] != 0 or
        snapshot["rangeProjectFileResponseCount"] < 1 or snapshot["requestedLargeFileCount"] < 1
        for snapshot in (first, cached)
    ):
        raise ContractError("RPG_ACCEPTANCE_RUNTIME_LOADING_INVALID")


def require_loading_snapshot(value: Any) -> dict[str, int]:
    keys = {
        "declaredLargeFileCount", "declaredProjectBytes", "declaredProjectFileCount", "fullProjectFileResponseCount",
        "nativeProjectResponseCount", "projectContentIdentityCount", "rangeProjectFileResponseCount",
        "requestedLargeFileCount", "requestedProjectBytes", "requestedProjectFileCount", "runtimeAssetCacheHitCount",
        "runtimeAssetRequestCount", "runtimeAssetTransferredBytes",
    }
    if not isinstance(value, dict) or set(value) != keys or any(
        not isinstance(value.get(key), int) or isinstance(value.get(key), bool) or value[key] < 0 for key in keys
    ):
        raise ContractError("RPG_ACCEPTANCE_RUNTIME_LOADING_INVALID")
    return value


def validate_generation_evidence(
    payload: dict[str, Any], spec: GenerationCase, expected_project_digest: str,
) -> None:
    if payload.get("schemaVersion") != 1 or payload.get("status") != "PASS":
        raise ContractError("RPG_ACCEPTANCE_PRODUCT_EVIDENCE_HEADER_INVALID")
    review = payload.get("review")
    validation = payload.get("validation")
    product = payload.get("productLaunch")
    if not all(isinstance(value, dict) for value in (review, validation, product)):
        raise ContractError("RPG_ACCEPTANCE_PRODUCT_EVIDENCE_INVALID")
    assert isinstance(review, dict) and isinstance(validation, dict) and isinstance(product, dict)
    target = validation.get("routeEvidence")
    if not isinstance(target, dict):
        raise ContractError("RPG_ACCEPTANCE_TARGET_EVIDENCE_MISSING")
    expected_target = {
        "providerId": "retrom-runtime", "targetId": spec.target_id, "generation": spec.generation,
        "evidenceGeneration": spec.evidence_generation, "evidenceConfidence": spec.confidence,
        "gameCompatibilityLine": f"{spec.target_id}-v1", "projectFingerprint": expected_project_digest,
    }
    for key, expected in expected_target.items():
        require_equal(target.get(key), expected, f"TARGET_{key.upper()}")
    if not UUID.fullmatch(str(target.get("effectiveSourceSnapshotId", ""))):
        raise ContractError("RPG_ACCEPTANCE_TARGET_SOURCE_SNAPSHOT_INVALID")
    for key in ("targetContractSha256", "dependencySnapshotSha256", "projectFingerprint"):
        if not SHA256.fullmatch(str(target.get(key, ""))):
            raise ContractError(f"RPG_ACCEPTANCE_TARGET_{key.upper()}_INVALID")
    rpg_review = review.get("rpgMaker")
    if not isinstance(rpg_review, dict):
        raise ContractError("RPG_ACCEPTANCE_REVIEW_RPG_MISSING")
    require_equal(rpg_review.get("selectedCoreId"), USER_CORE_ID, "REVIEW_CORE")
    require_equal(rpg_review.get("generation"), spec.generation, "REVIEW_GENERATION")
    require_equal(rpg_review.get("evidenceGeneration"), spec.evidence_generation, "REVIEW_EVIDENCE_GENERATION")
    require_equal(rpg_review.get("evidenceConfidence"), spec.confidence, "REVIEW_EVIDENCE_CONFIDENCE")
    require_equal(rpg_review.get("runtimeValidationCurrent"), True, "REVIEW_VALIDATION_CURRENT")
    require_equal(review.get("itemId"), validation.get("importItemId"), "REVIEW_VALIDATION_RELATION")
    require_equal(validation.get("state"), "PASSED", "VALIDATION_STATE")
    decision = validation.get("decision")
    if not isinstance(decision, dict) or decision.get("decision") != "PASS":
        raise ContractError("RPG_ACCEPTANCE_REVIEWER_DECISION_NOT_PASS")
    gates = validation.get("machineGates")
    if not isinstance(gates, list) or [item.get("gate") for item in gates if isinstance(item, dict)] != list(GATES):
        raise ContractError("RPG_ACCEPTANCE_GATE_ORDER_INVALID")
    if any(item.get("status") != "PASSED" for item in gates):
        raise ContractError("RPG_ACCEPTANCE_GATE_NOT_PASSED")
    engine_gate = next(item for item in gates if item["gate"] == "ENGINE_PROFILE")
    engine_evidence = engine_gate.get("evidence")
    if not isinstance(engine_evidence, dict) or engine_evidence.get("generation") != spec.generation or \
            engine_evidence.get("engineProfile") != ENGINE_PROFILES[spec.generation]:
        raise ContractError("RPG_ACCEPTANCE_ENGINE_PROFILE_INVALID")
    frame_gate = next(item for item in gates if item["gate"] == "FRAMES_300")
    if not isinstance(frame_gate.get("evidence"), dict) or frame_gate["evidence"].get("continuousFrames", 0) < 300:
        raise ContractError("RPG_ACCEPTANCE_FRAME_GATE_INVALID")
    validate_checkpoint(validation, gates)
    config = product.get("config")
    if not isinstance(config, dict):
        raise ContractError("RPG_ACCEPTANCE_PRODUCT_CONFIG_MISSING")
    for key, expected in {
        "purpose": "PRODUCT", "providerId": "retrom-runtime", "targetId": spec.target_id,
        "targetContractSha256": target["targetContractSha256"],
        "gameCompatibilityLine": target["gameCompatibilityLine"],
    }.items():
        require_equal(config.get(key), expected, f"PRODUCT_{key.upper()}")
    launch_id = str(product.get("launchId", ""))
    if not UUID.fullmatch(launch_id) or launch_id in {validation.get("launchId"), validation.get("restoreLaunchId")}:
        raise ContractError("RPG_ACCEPTANCE_PRODUCT_LAUNCH_ID_INVALID")
    require_equal(product.get("playerRunning"), True, "PLAYER_RUNNING")
    validate_input_transcript(payload.get("inputTranscript"), spec, target["targetContractSha256"])
    marker, marker_rgb = validate_input_provenance(
        payload.get("inputProvenance"), spec, expected_project_digest,
    )
    require_equal(
        payload["inputTranscript"]["upload"]["fileCount"],
        payload["inputProvenance"]["fileCount"],
        "INPUT_FILE_COUNT",
    )
    require_equal(
        payload["inputTranscript"]["upload"]["totalBytes"],
        payload["inputProvenance"]["totalBytes"],
        "INPUT_TOTAL_BYTES",
    )
    validate_runtime_loading(
        payload.get("loading"), spec, launch_id, payload["inputProvenance"]["fileCount"],
    )
    validate_restore_visual(
        payload.get("restoreVisualEvidence"), marker, marker_rgb, spec.generation == "RPGMZ",
    )
    screenshots = payload.get("screenshots")
    if not isinstance(screenshots, list) or len(screenshots) != 2 or \
            payload["restoreVisualEvidence"]["screenshot"] not in screenshots or any(
                not isinstance(path, str) or not path.startswith("screenshots/") or path.startswith("screenshots/../")
                for path in screenshots
            ):
        raise ContractError("RPG_ACCEPTANCE_PRODUCT_SCREENSHOTS_INVALID")
    validate_runtime_environment(payload.get("runtimeEnvironment"), spec, gates)
    if spec.generation in {"RPGXP", "RPGVX", "RPGVXACE"}:
        if config.get("checkpointMaxBytes") != XP_STATE_BYTES or \
                config.get("checkpointFormat") != "mkxp-state-compact-v1" or \
                not valid_compact_mkxp_checkpoint_size(
                    validation["checkpointRoundTrip"].get("sizeBytes"),
                ):
            raise ContractError("RPG_ACCEPTANCE_MKXP_RUNTIME_EVIDENCE_INVALID")
    if spec.generation == "RPGXP":
        if "xpRuntimeTrace" in payload:
            validate_xp_runtime_trace(payload["xpRuntimeTrace"], validation["checkpointRoundTrip"], config)
    if spec.generation in {"RPGMV", "RPGMZ"}:
        validate_origin_inventory(payload.get("originInventory"), launch_id)
    if spec.generation == "RPGMZ":
        require_equal(
            payload["runtimeEnvironment"].get("engineVersion"),
            payload["inputProvenance"].get("engineVersion"),
            "MZ_ENGINE_VERSION",
        )


def validate_checkpoint(validation: dict[str, Any], gates: list[dict[str, Any]] | None = None) -> None:
    original = validation.get("launchId")
    restore = validation.get("restoreLaunchId")
    if not UUID.fullmatch(str(original)) or not UUID.fullmatch(str(restore)) or original == restore:
        raise ContractError("RPG_ACCEPTANCE_CROSS_LAUNCH_INVALID")
    round_trip = validation.get("checkpointRoundTrip")
    if not isinstance(round_trip, dict):
        raise ContractError("RPG_ACCEPTANCE_CHECKPOINT_MISSING")
    for key in ("created", "originalLaunchEnded", "restoreStarted", "positionVerified", "restoreInputVerified"):
        require_equal(round_trip.get(key), True, f"CHECKPOINT_{key.upper()}")
    require_equal(round_trip.get("originalLaunchId"), original, "CHECKPOINT_ORIGINAL_LAUNCH")
    require_equal(round_trip.get("restoreLaunchId"), restore, "CHECKPOINT_RESTORE_LAUNCH")
    initial = require_position(round_trip.get("initialPosition"), "INITIAL")
    saved = require_position(round_trip.get("savedPosition"), "SAVED")
    diverged = require_position(round_trip.get("divergedPosition"), "DIVERGED")
    restored = require_position(round_trip.get("restoredPosition"), "RESTORED")
    restore_input = require_position(round_trip.get("restoreInputPosition"), "RESTORE_INPUT")
    if saved == initial or saved == diverged or restored != saved or restore_input == restored:
        raise ContractError("RPG_ACCEPTANCE_POSITION_ROUND_TRIP_INVALID")
    if (initial["fixtureState"], saved["fixtureState"], diverged["fixtureState"]) != (0, 1, 2) or \
            restored["fixtureState"] != 1:
        raise ContractError("RPG_ACCEPTANCE_FIXTURE_STATE_SEQUENCE_INVALID")
    if gates is not None:
        expected_positions = {
            "INITIAL_POSITION_RECORDED": initial,
            "SAVE_POINT_RECORDED": saved,
            "POST_SAVE_STATE_DIVERGED": diverged,
            "RESTORE_POSITION_VERIFIED": restored,
            "RESTORE_INPUT": restore_input,
        }
        for gate in gates:
            expected = expected_positions.get(str(gate.get("gate")))
            if expected is not None and gate.get("evidence") != expected:
                raise ContractError("RPG_ACCEPTANCE_GATE_POSITION_EVIDENCE_INVALID")
    if not SHA256.fullmatch(str(round_trip.get("sha256", ""))) or not round_trip.get("screenshotUrl"):
        raise ContractError("RPG_ACCEPTANCE_CHECKPOINT_PAYLOAD_EVIDENCE_INVALID")


def required_environment(case_id: str) -> list[str]:
    common = ["RETROM_ACCEPTANCE_BASE_URL", "RETROM_ACCEPTANCE_USERNAME", "RETROM_ACCEPTANCE_PASSWORD"]
    browser_cases = {"ACC-RPG-001", PACK_CASE, *SECURITY_CASES, *GENERATION_CASES}
    if case_id in browser_cases:
        common.append("RETROM_CHROME_EXECUTABLE")
    if case_id in GENERATION_CASES:
        prefix = case_id.replace("-", "_")
        common.extend(f"RETROM_{prefix}_{suffix}" for suffix in ("IMPORT_ITEM_ID", "VALIDATION_ID", "GAME_ID"))
    if case_id == "ACC-RPG-008":
        common.extend(("RPG_MZ_SMOKE_ROOT", "RPG_MZ_SMOKE_PROVENANCE"))
    if case_id == PACK_CASE:
        common.extend((
            "RETROM_ACC_RPG_009_PLAN", "RETROM_ACC_RPG_009_DATABASE",
            "RETROM_ACC_RPG_009_PROVISION_EVIDENCE",
        ))
    return common



def pack_plan(path: Path) -> dict[str, Any]:
    if not path.is_absolute() or not path.is_file() or path.is_symlink():
        raise ContractError("RPG_ACCEPTANCE_PACK_PLAN_INVALID")
    plan = json.loads(path.read_text(encoding="utf-8"))
    expected_keys = {"schemaVersion", "uploads", "reviewIds", "protectedReferences"}
    if not isinstance(plan, dict) or set(plan) != expected_keys or plan.get("schemaVersion") != 2:
        raise ContractError("RPG_ACCEPTANCE_PACK_PLAN_SCHEMA_INVALID")
    uploads = plan.get("uploads")
    if not isinstance(uploads, dict) or set(uploads) != set(PACK_UPLOAD_ROLES):
        raise ContractError("RPG_ACCEPTANCE_PACK_UPLOAD_MATRIX_INCOMPLETE")
    source_types, suffixes = set(), set()
    upload_keys = {
        "sourcePath", "sourceType", "kind", "generation", "declaredName", "sourceNote",
        "sourceFileCount", "sourceSizeBytes", "sourceSha256",
    }
    for role, expected_identity in PACK_UPLOAD_ROLES.items():
        upload = uploads[role]
        if not isinstance(upload, dict) or set(upload) != upload_keys:
            raise ContractError("RPG_ACCEPTANCE_PACK_UPLOAD_SCHEMA_INVALID")
        source = Path(str(upload["sourcePath"]))
        if not source.is_absolute() or not source.exists() or source.is_symlink():
            raise ContractError("RPG_ACCEPTANCE_PACK_SOURCE_INVALID")
        if upload["sourceType"] not in {"DIRECTORY", "FILES"}:
            raise ContractError("RPG_ACCEPTANCE_PACK_SOURCE_TYPE_INVALID")
        if upload["sourceType"] == "DIRECTORY" and not source.is_dir() or \
                upload["sourceType"] == "FILES" and not source.is_file():
            raise ContractError("RPG_ACCEPTANCE_PACK_SOURCE_TYPE_INVALID")
        if (upload["kind"], upload["generation"], upload["declaredName"]) != expected_identity:
            raise ContractError("RPG_ACCEPTANCE_PACK_UPLOAD_ROLE_INVALID")
        note = upload["sourceNote"]
        if note != PACK_SOURCE_NOTE:
            raise ContractError("RPG_ACCEPTANCE_PACK_SOURCE_NOTE_INVALID")
        if pack_source_identity(source, upload["sourceType"]) != (
            upload["sourceFileCount"], upload["sourceSizeBytes"], upload["sourceSha256"],
        ):
            raise ContractError("RPG_ACCEPTANCE_PACK_SOURCE_IDENTITY_INVALID")
        source_types.add(upload["sourceType"])
        suffixes.add(source.suffix.lower())
    if source_types != {"DIRECTORY", "FILES"} or not {".zip", ".7z"} <= suffixes:
        raise ContractError("RPG_ACCEPTANCE_PACK_INPUT_COVERAGE_INCOMPLETE")
    review_ids = plan.get("reviewIds")
    if not isinstance(review_ids, dict) or set(review_ids) != PACK_REVIEW_ROLES or \
            len(set(review_ids.values())) != len(PACK_REVIEW_ROLES) or \
            any(not UUID.fullmatch(str(value)) for value in review_ids.values()):
        raise ContractError("RPG_ACCEPTANCE_PACK_REVIEW_IDS_INVALID")
    protected = plan.get("protectedReferences")
    if not isinstance(protected, dict) or set(protected) != {"publishedVariant", "restorableCheckpoint"}:
        raise ContractError("RPG_ACCEPTANCE_PACK_PROTECTED_REFERENCES_INVALID")
    expected_reference_keys = {
        "publishedVariant": {"installationId", "gameId"},
        "restorableCheckpoint": {"installationId", "gameId", "saveStateId"},
    }
    for role, keys in expected_reference_keys.items():
        reference = protected[role]
        if not isinstance(reference, dict) or set(reference) != keys or \
                any(not UUID.fullmatch(str(value)) for value in reference.values()):
            raise ContractError("RPG_ACCEPTANCE_PACK_PROTECTED_REFERENCES_INVALID")
    if protected["publishedVariant"]["installationId"] == protected["restorableCheckpoint"]["installationId"]:
        raise ContractError("RPG_ACCEPTANCE_PACK_PROTECTED_REFERENCES_INVALID")
    return plan


def pack_source_identity(source: Path, source_type: str) -> tuple[int, int, str]:
    if source_type == "FILES":
        contents = source.read_bytes()
        return 1, len(contents), hashlib.sha256(contents).hexdigest()
    entries = list(source.rglob("*"))
    if any(path.is_symlink() for path in entries):
        raise ContractError("RPG_ACCEPTANCE_PACK_SOURCE_SYMLINK")
    files = sorted(path for path in entries if path.is_file())
    digest = hashlib.sha256(b"RETROM_ACC_RPG_009_INPUT_V1\0")
    total = 0
    for file in files:
        name = file.relative_to(source).as_posix().encode("utf-8")
        contents = file.read_bytes()
        total += len(contents)
        digest.update(len(name).to_bytes(4, "big"))
        digest.update(name)
        digest.update(hashlib.sha256(contents).digest())
        digest.update(len(contents).to_bytes(8, "big"))
    return len(files), total, digest.hexdigest()


def pack_database(path: Path) -> Path:
    if not path.is_absolute() or not path.is_file() or path.is_symlink():
        raise ContractError("RPG_ACCEPTANCE_PACK_DATABASE_INVALID")
    return path


def inspect_pack_evidence(plan: Path, database: Path, evidence: Path) -> dict[str, Any]:
    completed = subprocess.run(
        [
            sys.executable, str(ROOT / "scripts" / "acceptance" / "rpgmaker_pack_inspect.py"),
            "--database", str(database), "--plan", str(plan), "--evidence", str(evidence),
        ],
        cwd=ROOT, text=True, check=False, capture_output=True, timeout=60,
    )
    if completed.returncode != 0:
        raise ContractError("RPG_ACCEPTANCE_PACK_DATABASE_INSPECT_FAILED")
    try:
        result = json.loads(completed.stdout)
    except json.JSONDecodeError as error:
        raise ContractError("RPG_ACCEPTANCE_PACK_DATABASE_INSPECT_INVALID") from error
    if not isinstance(result, dict):
        raise ContractError("RPG_ACCEPTANCE_PACK_DATABASE_INSPECT_INVALID")
    return result


def valid_pack_job(value: Any, kind: str) -> bool:
    events = value.get("events") if isinstance(value, dict) else None
    required_events = {"SUCCEEDED"} if kind == "UPLOAD_FINALIZE" else {"QUEUED", "STARTED", "SUCCEEDED"}
    return isinstance(value, dict) and isinstance(events, list) and value.get("kind") == kind and \
        value.get("state") == "SUCCEEDED" and UUID.fullmatch(str(value.get("jobId"))) is not None and \
        required_events <= set(events)


def validate_pack_upload_evidence(payload: dict[str, Any]) -> None:
    uploads, installations = payload.get("uploads"), payload.get("installations")
    if not isinstance(uploads, dict) or set(uploads) != set(PACK_UPLOAD_ROLES) or \
            not isinstance(installations, dict) or set(installations) != set(PACK_UPLOAD_ROLES):
        raise ContractError("RPG_ACCEPTANCE_PACK_INSTALL_EVIDENCE_INCOMPLETE")
    for role, item in uploads.items():
        catalog = installations[role]
        if not isinstance(item, dict) or item.get("role") != role or \
                not UUID.fullmatch(str(item.get("uploadId"))) or \
                not UUID.fullmatch(str(item.get("installationId"))) or \
                not UUID.fullmatch(str(item.get("jobId"))) or \
                item.get("jobId") != item.get("validationJob", {}).get("jobId") or \
                not valid_pack_job(item.get("finalizeJob"), "UPLOAD_FINALIZE") or \
                not valid_pack_job(item.get("validationJob"), "RUNTIME_ASSET_PACK_VALIDATE"):
            raise ContractError("RPG_ACCEPTANCE_PACK_INSTALL_IDS_INVALID")
        if not isinstance(catalog, dict) or catalog.get("installationId") != item.get("installationId") or \
                catalog.get("status") != "READY" or not SHA256.fullmatch(str(catalog.get("filesDigest", ""))) or \
                not SHA256.fullmatch(str(catalog.get("bundleSha256", ""))):
            raise ContractError("RPG_ACCEPTANCE_PACK_READY_EVIDENCE_INVALID")


def validate_pack_review_evidence(payload: dict[str, Any]) -> None:
    reviews = payload.get("reviews")
    if not isinstance(reviews, dict) or set(reviews) != {"published", "matcherRejections"}:
        raise ContractError("RPG_ACCEPTANCE_PACK_REVIEW_EVIDENCE_INCOMPLETE")
    published = reviews["published"]
    published_roles = {
        "rpg2000SelfContained", "rpg2003SelfContained", "rpgxpNoRtp", "rpgvxNoRtp", "rpgvxaceNoRtp",
    }
    if not isinstance(published, list) or any(not isinstance(item, dict) for item in published):
        raise ContractError("RPG_ACCEPTANCE_PACK_REVIEW_EVIDENCE_INCOMPLETE")
    if {item.get("role") for item in published} != published_roles or any(
        item.get("status") != 201 or not UUID.fullmatch(str(item.get("itemId"))) or
        not UUID.fullmatch(str(item.get("gameId"))) or not UUID.fullmatch(str(item.get("validationId")))
        for item in published
    ):
        raise ContractError("RPG_ACCEPTANCE_PACK_REVIEW_EVIDENCE_INCOMPLETE")
    outcomes = reviews["matcherRejections"]
    expected = {
        "MISSING": {"rpg2000Missing", "rpg2003Missing", "rpgxpCustom", "rpgvxCustom", "rpgvxaceCustom"},
        "SELECTED": {"rpg2000Missing", "rpg2003Missing", "rpgxpCustom", "rpgvxCustom", "rpgvxaceCustom"},
        "AMBIGUOUS": {"rpgxpStandardAmbiguous", "rpgvxStandardAmbiguous", "rpgvxaceStandardAmbiguous"},
    }
    if not isinstance(outcomes, list) or any(not isinstance(item, dict) for item in outcomes):
        raise ContractError("RPG_ACCEPTANCE_PACK_MATCHER_EVIDENCE_INCOMPLETE")
    for matcher, roles in expected.items():
        matching = [item for item in outcomes if item.get("matcher") == matcher]
        if {item.get("role") for item in matching} != roles or len(matching) != len(roles):
            raise ContractError("RPG_ACCEPTANCE_PACK_MATCHER_EVIDENCE_INCOMPLETE")
        if any(item.get("publish", {}).get("status") != 409 or
               item.get("publish", {}).get("code") != "REVIEW_VALIDATION_STALE" for item in matching):
            raise ContractError("RPG_ACCEPTANCE_PACK_PUBLISH_REJECTION_INVALID")
        if matcher == "MISSING" and any(
            item.get("patchStatus") != 422 or item.get("patchCode") != "REVIEW_DRAFT_INVALID" for item in matching
        ):
            raise ContractError("RPG_ACCEPTANCE_PACK_MATCHER_REJECTION_INVALID")
        if matcher == "AMBIGUOUS" and any(
            item.get("rejectionStatus") != 422 or item.get("rejectionCode") != "REVIEW_DRAFT_INVALID"
            or item.get("patchStatus") != 200 for item in matching
        ):
            raise ContractError("RPG_ACCEPTANCE_PACK_MATCHER_REJECTION_INVALID")
        if matcher == "SELECTED" and any(item.get("patchStatus") != 200 for item in matching):
            raise ContractError("RPG_ACCEPTANCE_PACK_EXPLICIT_SELECTION_INVALID")


def validate_pack_database_evidence(payload: dict[str, Any]) -> None:
    database = payload.get("databaseEvidence")
    if not isinstance(database, dict):
        raise ContractError("RPG_ACCEPTANCE_PACK_DATABASE_EVIDENCE_INCOMPLETE")
    uploads = database.get("uploads")
    published = database.get("publishedReviews")
    selected = database.get("selectedReviews")
    if database.get("schemaVersion") != 1 or not isinstance(uploads, dict) or \
            set(uploads) != set(PACK_UPLOAD_ROLES) or not isinstance(published, list) or len(published) != 5 or \
            not isinstance(selected, list) or len(selected) != 8:
        raise ContractError("RPG_ACCEPTANCE_PACK_DATABASE_EVIDENCE_INCOMPLETE")
    for role, item in uploads.items():
        observed = payload["uploads"][role]
        if not isinstance(item, dict) or item.get("uploadId") != observed.get("uploadId") or \
                item.get("installationId") != observed.get("installationId") or \
                not UUID.fullmatch(str(item.get("consumptionId"))) or item.get("sessionState") != "COMPLETE" or \
                not valid_pack_job(item.get("finalizeJob"), "UPLOAD_FINALIZE") or \
                not valid_pack_job(item.get("validationJob"), "RUNTIME_ASSET_PACK_VALIDATE") or \
                not SHA256.fullmatch(str(item.get("validationJob", {}).get("inputDigest", ""))):
            raise ContractError("RPG_ACCEPTANCE_PACK_DATABASE_UPLOAD_EVIDENCE_INVALID")
        released = item.get("consumptionReleasedAtMs")
        reason = item.get("consumptionReleaseReason")
        if role == "zeroReference" and (not isinstance(released, int) or reason != "UPLOAD_CONSUMED") or \
                role != "zeroReference" and (released is not None or reason is not None):
            raise ContractError("RPG_ACCEPTANCE_PACK_DATABASE_CONSUMPTION_EVIDENCE_INVALID")
    protected = database.get("protectedReferences")
    if not isinstance(protected, dict) or set(protected) != {"publishedVariant", "restorableCheckpoint"} or \
            protected["publishedVariant"].get("definitionId") != "rgss1_standard" or \
            protected["restorableCheckpoint"].get("definitionId") != "rgss2_rpgvx" or \
            protected["publishedVariant"].get("availableForLaunch") is not True or \
            protected["restorableCheckpoint"].get("availableForLaunch") is not True or any(
                item.get("providerId") != "retrom-runtime" or item.get("targetId") != target_id or
                not SHA256.fullmatch(str(item.get("targetContractSha256", "")))
                for item, target_id in (
                    (protected["publishedVariant"], "rpgmaker-xp"),
                    (protected["restorableCheckpoint"], "rpgmaker-vx"),
                )
            ):
        raise ContractError("RPG_ACCEPTANCE_PACK_PROTECTED_REFERENCE_EVIDENCE_INVALID")
    for role, item in protected.items():
        planned = payload["protectedReferences"][role]
        if item.get("installationId") != planned.get("installationId") or item.get("gameId") != planned.get("gameId") or \
                role == "restorableCheckpoint" and item.get("saveStateId") != planned.get("saveStateId"):
            raise ContractError("RPG_ACCEPTANCE_PACK_PROTECTED_REFERENCE_RELATION_INVALID")
    expected_published = {
        (item["role"], item["itemId"], item["gameId"]) for item in payload["reviews"]["published"]
    }
    if any(not isinstance(item, dict) for item in published) or {
        (item.get("role"), item.get("itemId"), item.get("gameId")) for item in published
    } != expected_published:
        raise ContractError("RPG_ACCEPTANCE_PACK_DATABASE_PUBLISHED_EVIDENCE_INVALID")
    expected_selected = {
        (item["role"], item["itemId"], item["installationId"])
        for item in payload["reviews"]["matcherRejections"] if item.get("matcher") in {"SELECTED", "AMBIGUOUS"}
    }
    if any(not isinstance(item, dict) for item in selected) or {
        (item.get("role"), item.get("itemId"), item.get("installationId")) for item in selected
    } != expected_selected:
        raise ContractError("RPG_ACCEPTANCE_PACK_DATABASE_SELECTION_EVIDENCE_INVALID")
    released = database.get("zeroReferenceRelease")
    if not isinstance(released, dict) or released.get("releaseReason") != "UPLOAD_CONSUMED" or \
            released.get("purgedFileCount") != released.get("uploadFileCount") or \
            released.get("completionAuditCount") != 1 or \
            released.get("gcScheduledAtMs", 0) <= released.get("gcFirstUnreferencedAtMs", 0) or \
            released.get("consumptionId") != uploads["zeroReference"].get("consumptionId") or \
            released.get("bundleSha256") != payload["installations"]["zeroReference"].get("bundleSha256") or \
            not SHA256.fullmatch(str(released.get("job", {}).get("inputDigest", ""))) or \
            not valid_pack_job(released.get("job"), "PAYLOAD_RELEASE"):
        raise ContractError("RPG_ACCEPTANCE_PACK_PAYLOAD_RELEASE_EVIDENCE_INVALID")


def validate_pack_evidence(payload: dict[str, Any]) -> None:
    expected_keys = {
        "schemaVersion", "caseId", "status", "installations", "reviews", "protectedReferences",
        "protectedDeletes", "zeroReferenceDelete", "uploads", "screenshots", "databaseEvidence",
    }
    if set(payload) != expected_keys or payload.get("schemaVersion") != 1 or \
            payload.get("caseId") != PACK_CASE or payload.get("status") != "PASS":
        raise ContractError("RPG_ACCEPTANCE_PACK_EVIDENCE_HEADER_INVALID")
    validate_pack_upload_evidence(payload)
    validate_pack_review_evidence(payload)
    validate_pack_database_evidence(payload)
    protected = payload.get("protectedDeletes")
    deleted = payload.get("zeroReferenceDelete")
    if not isinstance(protected, list) or any(not isinstance(item, dict) for item in protected):
        raise ContractError("RPG_ACCEPTANCE_PACK_PROTECTED_DELETE_EVIDENCE_INVALID")
    if {item.get("role") for item in protected} != {
        "publishedVariant", "restorableCheckpoint",
    } or any(item.get("status") != 409 or item.get("code") != "RPG_RUNTIME_PACK_IN_USE" for item in protected):
        raise ContractError("RPG_ACCEPTANCE_PACK_PROTECTED_DELETE_EVIDENCE_INVALID")
    if not isinstance(deleted, dict) or deleted.get("staleStatus") != 412 or deleted.get("currentStatus") != 204 or \
            deleted.get("finalStatus") != "DELETED" or not deleted.get("deletedAtMs"):
        raise ContractError("RPG_ACCEPTANCE_PACK_ZERO_DELETE_EVIDENCE_INVALID")
    if payload.get("screenshots") != [
        "screenshots/rpgmaker-pack-catalog.png", "screenshots/rpgmaker-pack-review-binding.png",
    ]:
        raise ContractError("RPG_ACCEPTANCE_PACK_SCREENSHOT_EVIDENCE_INVALID")
    forbidden = {"sourcePath", "password", "csrfToken", "capability", "cookie"}
    stack: list[Any] = [payload]
    while stack:
        value = stack.pop()
        if isinstance(value, dict):
            if forbidden.intersection(value):
                raise ContractError("RPG_ACCEPTANCE_PACK_EVIDENCE_CONTAINS_SECRET_OR_PATH")
            stack.extend(value.values())
        elif isinstance(value, list):
            stack.extend(value)



def validate_security_evidence(payload: dict[str, Any], case_id: str) -> None:
    if payload.get("schemaVersion") != 1 or payload.get("caseId") != case_id or payload.get("status") != "PASS":
        raise ContractError("RPG_ACCEPTANCE_SECURITY_EVIDENCE_HEADER_INVALID")
    if case_id == "ACC-RPG-010":
        expected_keys = {
            "schemaVersion", "caseId", "status", "detectorMatrix", "unsafe",
            "nestedArchives", "opaqueNative", "screenshots",
        }
        if set(payload) != expected_keys:
            raise ContractError("RPG_ACCEPTANCE_SECURITY_EVIDENCE_HEADER_INVALID")
        validate_detector_matrix(payload.get("detectorMatrix"))
        validate_unsafe_security(payload.get("unsafe"))
        validate_nested_security(payload.get("nestedArchives"))
        validate_opaque_native_security(payload.get("opaqueNative"))
        if payload.get("screenshots") != ["screenshots/acc-rpg-010-opaque-native.png"]:
            raise ContractError("RPG_ACCEPTANCE_SECURITY_SCREENSHOT_INVALID")
        return
    if set(payload) != {"schemaVersion", "caseId", "status", "harnesses", "screenshots"}:
        raise ContractError("RPG_ACCEPTANCE_ISOLATION_EVIDENCE_HEADER_INVALID")
    harnesses = payload.get("harnesses")
    if not isinstance(harnesses, list) or len(harnesses) != 2 or \
            any(not isinstance(item, dict) for item in harnesses) or \
            {item.get("generation") for item in harnesses} != {"RPGMV", "RPGMZ"}:
        raise ContractError("RPG_ACCEPTANCE_ISOLATION_HARNESS_INCOMPLETE")
    for harness in harnesses:
        validate_isolation_harness(harness)
    launch_ids = [harness[key] for harness in harnesses for key in ("originalLaunchId", "restoreLaunchId")]
    if len(set(launch_ids)) != len(launch_ids):
        raise ContractError("RPG_ACCEPTANCE_ISOLATION_HARNESS_INCOMPLETE")
    expected_screenshots = [
        f"screenshots/acc-rpg-011-{generation.lower()}{suffix}.png"
        for generation in ("RPGMV", "RPGMZ") for suffix in ("", "-restore")
    ]
    if payload.get("screenshots") != expected_screenshots:
        raise ContractError("RPG_ACCEPTANCE_ISOLATION_SCREENSHOT_INVALID")


def validate_detector_matrix(value: Any) -> None:
    expected = {
        "boundary": "GO_DETECTOR_UNIT",
        "testName": "TestPublicWrongCoreMatrixHasFortyTwoMismatches",
        "combinationCount": 42,
        "expectedCode": "RPG_SELECTED_CORE_MISMATCH",
        "matrixSha256": hashlib.sha256(SECURITY_MATRIX_PATH.read_bytes()).hexdigest(),
        "log": "detector-matrix.log",
    }
    if not isinstance(value, dict) or value != expected:
        raise ContractError("RPG_ACCEPTANCE_DETECTOR_MATRIX_INVALID")


def validate_unsafe_security(value: Any) -> None:
    keys = {"name", "accepted", "status", "code"}
    if not isinstance(value, list) or len(value) != len(SECURITY_UNSAFE) or any(
        not isinstance(item, dict) or set(item) != keys for item in value
    ):
        raise ContractError("RPG_ACCEPTANCE_UNSAFE_MATRIX_INVALID")
    actual = {item["name"]: item for item in value}
    if set(actual) != set(SECURITY_UNSAFE) or len(actual) != len(value):
        raise ContractError("RPG_ACCEPTANCE_UNSAFE_MATRIX_INVALID")
    for name, expected in SECURITY_UNSAFE.items():
        item = actual[name]
        if (item["accepted"], item["status"], item["code"]) != expected:
            raise ContractError("RPG_ACCEPTANCE_UNSAFE_MATRIX_INVALID")


def validate_nested_security(value: Any) -> None:
    row_keys = {
        "generation", "format", "detection", "sidecar", "sha256", "sizeBytes", "filesDigest",
        "postInspectionFilesDigest", "nestedEntryCount", "importJobId", "importItemId",
        "contentIdentityDigest", "validationId", "launchId", "providerId", "targetId",
        "targetContractSha256", "projection", "launchFinished",
    }
    expected = {
        (generation, archive_format, detection)
        for generation in SECURITY_TARGETS
        for archive_format in ("7Z", "GZIP", "RAR", "TAR", "ZIP")
        for detection in ("extension", "magic")
    }
    if not isinstance(value, list) or len(value) != 70 or any(
        not isinstance(item, dict) or set(item) != row_keys for item in value
    ):
        raise ContractError("RPG_ACCEPTANCE_NESTED_ARCHIVE_EVIDENCE_INVALID")
    actual = {(item["generation"], item["format"], item["detection"]): item for item in value}
    if set(actual) != expected or len(actual) != len(value):
        raise ContractError("RPG_ACCEPTANCE_NESTED_ARCHIVE_EVIDENCE_INVALID")
    identifier_fields = ("importJobId", "importItemId", "validationId", "launchId")
    for field in identifier_fields:
        identifiers = [str(item.get(field, "")) for item in value]
        if any(not UUID.fullmatch(identifier) for identifier in identifiers) or len(set(identifiers)) != len(identifiers):
            raise ContractError("RPG_ACCEPTANCE_NESTED_ARCHIVE_EVIDENCE_INVALID")
    for item in value:
        if not valid_nested_security_row(item):
            raise ContractError("RPG_ACCEPTANCE_NESTED_ARCHIVE_EVIDENCE_INVALID")


def valid_nested_security_row(item: dict[str, Any]) -> bool:
    generation = item["generation"]
    target_id = SECURITY_TARGETS[generation]
    logical_name = item.get("sidecar")
    if not isinstance(logical_name, str) or not logical_name.startswith("RetromNested/") or \
            logical_name.count("/") != 1 or logical_name.endswith("/") or ".." in logical_name or \
            not SHA256.fullmatch(str(item.get("sha256", ""))) or \
            not isinstance(item.get("sizeBytes"), int) or isinstance(item.get("sizeBytes"), bool) or \
            item["sizeBytes"] <= 0 or not SHA256.fullmatch(str(item.get("filesDigest", ""))) or \
            item.get("postInspectionFilesDigest") != item.get("filesDigest") or item.get("nestedEntryCount") != 0 or \
            not SHA256.fullmatch(str(item.get("contentIdentityDigest", ""))) or \
            item.get("providerId") != "retrom-runtime" or item.get("targetId") != target_id or \
            not SHA256.fullmatch(str(item.get("targetContractSha256", ""))) or item.get("launchFinished") is not True:
        return False
    projection = item.get("projection")
    keys = {"kind", "status", "logicalName", "sha256", "sizeBytes", "containerSha256", "exactMember"}
    if not isinstance(projection, dict) or set(projection) != keys or projection.get("logicalName") != logical_name:
        return False
    if generation in {"RPGMV", "RPGMZ"}:
        return projection == {
            "kind": "NATIVE_WEB_DENIED", "status": 404, "logicalName": logical_name,
            "sha256": None, "sizeBytes": None, "containerSha256": None, "exactMember": False,
        }
    expected_kind = "EASYRPG_PROJECT_FILE" if generation in {"RPG2000", "RPG2003"} else "MKXP_ARCHIVE_MEMBER"
    return projection.get("kind") == expected_kind and projection.get("status") == 200 and \
        projection.get("sha256") == item["sha256"] and projection.get("sizeBytes") == item["sizeBytes"] and \
        SHA256.fullmatch(str(projection.get("containerSha256", ""))) is not None and \
        projection.get("exactMember") is True


def validate_opaque_native_security(value: Any) -> None:
    keys = {
        "importItemId", "generation", "filesDigest", "sourceFiles", "runtimeProjection",
        "launchId", "runtimeOrigin", "launchFinished",
    }
    names = {"Game.exe", "nw.dll", "plugin.node", "launcher.bat"}
    source = value.get("sourceFiles") if isinstance(value, dict) else None
    runtime = value.get("runtimeProjection") if isinstance(value, dict) else None
    if not isinstance(value, dict) or set(value) != keys or value.get("generation") != "RPGMZ" or \
            not UUID.fullmatch(str(value.get("importItemId", ""))) or \
            not UUID.fullmatch(str(value.get("launchId", ""))) or \
            not SHA256.fullmatch(str(value.get("filesDigest", ""))) or \
            value.get("launchFinished") is not True or \
            not isinstance(source, list) or len(source) != 4 or {item.get("name") for item in source} != names or \
            any(set(item) != {"name", "sha256", "sizeBytes"} or
                not SHA256.fullmatch(str(item.get("sha256", ""))) or
                not isinstance(item.get("sizeBytes"), int) or item["sizeBytes"] <= 0 for item in source) or \
            not isinstance(runtime, list) or len(runtime) != 4 or {item.get("name") for item in runtime} != names or \
            any(set(item) != {"name", "status"} or item.get("status") != 404 for item in runtime):
        raise ContractError("RPG_ACCEPTANCE_OPAQUE_NATIVE_EVIDENCE_INVALID")
    origin = urlparse(str(value.get("runtimeOrigin", "")))
    if origin.scheme not in {"http", "https"} or origin.username or origin.password or origin.path or \
            origin.query or origin.fragment or not (origin.hostname or "").startswith(f"{value['launchId']}."):
        raise ContractError("RPG_ACCEPTANCE_OPAQUE_NATIVE_EVIDENCE_INVALID")


def validate_isolation_harness(harness: dict[str, Any]) -> None:
    keys = {
        "generation", "importItemId", "validationId", "originalLaunchId", "restoreLaunchId",
        "runtimeOrigin", "config", "originalScreenshot", "restoreScreenshot", "csp", "probes",
        "securityRequests", "bootstrap", "machineGates", "checkpointRoundTrip",
    }
    generation = harness.get("generation")
    if set(harness) != keys or generation not in ISOLATION_TARGETS or any(
        not UUID.fullmatch(str(harness.get(key)))
        for key in ("importItemId", "validationId", "originalLaunchId", "restoreLaunchId")
    ):
        raise ContractError("RPG_ACCEPTANCE_ISOLATION_HARNESS_INVALID")
    validate_isolation_runtime(harness, str(generation))
    validate_isolation_browser_boundary(harness)
    bootstrap = harness.get("bootstrap")
    gates = harness.get("machineGates")
    if not isinstance(bootstrap, dict) or set(bootstrap) != {
        "authenticatedReloadStatus", "replayStatus", "appHostEntryStatus", "runtimeApiStatus",
        "confusedHostStatus", "inactiveBootstrapStatus",
    } or bootstrap.get("authenticatedReloadStatus") != 303 or bootstrap.get("replayStatus") != 410 or \
            bootstrap.get("appHostEntryStatus") != 404 or bootstrap.get("runtimeApiStatus") != 404 or \
            bootstrap.get("confusedHostStatus") != 404 or bootstrap.get("inactiveBootstrapStatus") != 410:
        raise ContractError("RPG_ACCEPTANCE_ISOLATION_BOOTSTRAP_INVALID")
    if not isinstance(gates, list) or [gate.get("gate") for gate in gates] != list(GATES) or \
            any(gate.get("status") != "PASSED" for gate in gates):
        raise ContractError("RPG_ACCEPTANCE_ISOLATION_GATES_INVALID")
    validate_checkpoint({
        "launchId": harness.get("originalLaunchId"),
        "restoreLaunchId": harness.get("restoreLaunchId"),
        "checkpointRoundTrip": harness.get("checkpointRoundTrip"),
    }, gates)


def validate_isolation_runtime(harness: dict[str, Any], generation: str) -> None:
    target_id = ISOLATION_TARGETS[generation]
    config = harness.get("config")
    expected_config = {
        "providerId": "retrom-runtime", "targetId": target_id,
    }
    if not isinstance(config, dict) or set(config) != {*expected_config, "targetContractSha256"} or any(
        config.get(key) != value for key, value in expected_config.items()
    ) or not SHA256.fullmatch(str(config.get("targetContractSha256"))):
        raise ContractError("RPG_ACCEPTANCE_ISOLATION_TARGET_INVALID")
    origin = urlparse(str(harness.get("runtimeOrigin", "")))
    hostname = origin.hostname or ""
    if origin.scheme not in {"http", "https"} or origin.username or origin.password or \
            origin.path or origin.query or origin.fragment or \
            not hostname.startswith(f"{harness['originalLaunchId']}.") or \
            (origin.scheme == "http" and not hostname.endswith("localhost")):
        raise ContractError("RPG_ACCEPTANCE_ISOLATION_ORIGIN_INVALID")
    expected_prefix = f"screenshots/acc-rpg-011-{generation.lower()}"
    if harness.get("originalScreenshot") != f"{expected_prefix}.png" or \
            harness.get("restoreScreenshot") != f"{expected_prefix}-restore.png":
        raise ContractError("RPG_ACCEPTANCE_ISOLATION_SCREENSHOT_INVALID")


def validate_isolation_browser_boundary(harness: dict[str, Any]) -> None:
    csp = harness.get("csp")
    probes = harness.get("probes")
    requests = harness.get("securityRequests")
    if not isinstance(csp, str) or "base-uri 'self'" not in csp or "worker-src 'self' blob:" not in csp or \
            "connect-src 'self'" not in csp:
        raise ContractError("RPG_ACCEPTANCE_ISOLATION_CSP_INVALID")
    expected_probes = {
        "parentDom": "blocked", "appCookie": "none", "topNavigation": "blocked", "popup": "blocked",
        "form": {"attempted", "blocked"}, "externalFetch": "blocked", "nonAllowlistApi": "404",
        "serviceWorker": "blocked", "complete": "true",
    }
    if not isinstance(probes, dict) or set(probes) != set(expected_probes) or any(
        probes.get(key) not in expected if isinstance(expected, set) else probes.get(key) != expected
        for key, expected in expected_probes.items()
    ):
        raise ContractError("RPG_ACCEPTANCE_ISOLATION_PROBE_INVALID")
    if not isinstance(requests, list) or not requests or any(
        not isinstance(item, dict) or set(item) != {"urlKind", "status"} or
        item.get("urlKind") not in {"external", "nonAllowlistApi"} or not isinstance(item.get("status"), int) or
        (item.get("urlKind") == "external" and item.get("status") != 0)
        for item in requests
    ) or not any(item.get("urlKind") == "nonAllowlistApi" and item.get("status") == 404 for item in requests):
        raise ContractError("RPG_ACCEPTANCE_ISOLATION_NETWORK_INVALID")


def write_blocked(case_dir: Path, case_id: str, missing: list[str], reason: str) -> int:
    payload = {"schemaVersion": 1, "caseId": case_id, "status": "BLOCKED", "reason": reason, "missingInputs": missing}
    (case_dir / "rpgmaker-product.json").write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(reason)
    if missing:
        print("missing_inputs=" + ",".join(missing))
    return 3


def run_detector_matrix(case_dir: Path) -> dict[str, Any]:
    log_path = case_dir / "detector-matrix.log"
    command = [
        "go", "test", "./internal/rpgmaker/detector",
        "-run", "^TestPublicWrongCoreMatrixHasFortyTwoMismatches$", "-count=1",
    ]
    try:
        with log_path.open("wb") as log:
            completed = subprocess.run(
                command, cwd=ROOT, env=os.environ.copy(), stdout=log,
                stderr=subprocess.STDOUT, check=False, timeout=60,
            )
    except subprocess.TimeoutExpired as error:
        raise ContractError("RPG_ACCEPTANCE_DETECTOR_MATRIX_TIMEOUT") from error
    if completed.returncode != 0:
        raise ContractError("RPG_ACCEPTANCE_DETECTOR_MATRIX_FAILED")
    return {
        "boundary": "GO_DETECTOR_UNIT",
        "testName": "TestPublicWrongCoreMatrixHasFortyTwoMismatches",
        "combinationCount": 42,
        "expectedCode": "RPG_SELECTED_CORE_MISMATCH",
        "matrixSha256": hashlib.sha256(SECURITY_MATRIX_PATH.read_bytes()).hexdigest(),
        "log": "detector-matrix.log",
    }


def minimal_generation_closure_ready(case_dir: Path) -> bool:
    cases_root = case_dir.parent
    for case_id in GENERATION_CASES:
        result_path = cases_root / case_id.lower() / "result.json"
        try:
            result = json.loads(result_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            return False
        if not isinstance(result, dict) or result.get("caseId") != case_id or \
                result.get("status") != "PASS" or result.get("gitDirty") is not False:
            return False
        product = result.get("productEvidence")
        if not isinstance(product, dict) or product.get("caseId") != case_id or product.get("status") != "PASS":
            return False
    return True


def run(case_id: str, case_dir: Path) -> int:
    case_dir.mkdir(parents=True, exist_ok=True)
    (case_dir / "screenshots").mkdir(exist_ok=True)
    if case_id in MINIMAL_CLOSURE_CASES and not minimal_generation_closure_ready(case_dir):
        return write_blocked(case_dir, case_id, [], "RPG_SEVEN_CORE_MINIMAL_CLOSURE_REQUIRED")
    if case_id in DEFERRED_CASES:
        return write_blocked(case_dir, case_id, [], DEFERRED_CASES[case_id])
    missing = [name for name in required_environment(case_id) if not os.environ.get(name)]
    if missing:
        return write_blocked(case_dir, case_id, missing, "缺少实际 Retrom 产品验收输入")
    detector_matrix = run_detector_matrix(case_dir) if case_id == "ACC-RPG-010" else None
    expected_digest = ""
    fixture_root: Path | None = None
    input_provenance: dict[str, Any] | None = None
    spec = GENERATION_CASES.get(case_id)
    if spec:
        fixture_root = (Path(os.environ["RPG_MZ_SMOKE_ROOT"]) if case_id == "ACC-RPG-008" else
                        ROOT / "testdata" / "public-roms" / "rpgmaker-smoke" / str(spec.fixture_directory))
        if case_id == "ACC-RPG-008" and not fixture_root.is_absolute():
            raise ContractError("RPG_ACCEPTANCE_MZ_ROOT_MUST_BE_ABSOLUTE")
        expected_digest, file_count, total_bytes = project_digest(fixture_root)
        input_provenance = generation_input_provenance(
            case_id, spec, fixture_root, expected_digest, file_count, total_bytes,
        )
        print(f"fixture_files={file_count} fixture_bytes={total_bytes} fixture_digest={expected_digest}")
        prefix = case_id.replace("-", "_")
        for suffix in ("IMPORT_ITEM_ID", "VALIDATION_ID", "GAME_ID"):
            value = os.environ[f"RETROM_{prefix}_{suffix}"]
            if not UUID.fullmatch(value):
                raise ContractError(f"RPG_ACCEPTANCE_{suffix}_INVALID")
    if case_id == PACK_CASE:
        pack_plan(Path(os.environ["RETROM_ACC_RPG_009_PLAN"]))
        pack_database(Path(os.environ["RETROM_ACC_RPG_009_DATABASE"]))
    environment = os.environ.copy()
    environment.update({
        "RETROM_RPG_CASE_ID": case_id,
        "RETROM_RPG_CASE_DIR": str(case_dir),
        "RETROM_RPG_EXPECTED_PROJECT_DIGEST": expected_digest,
    })
    browser_driver = {
        PACK_CASE: "rpgmaker_pack.mjs",
        **{case: "rpgmaker_security.mjs" for case in SECURITY_CASES},
    }.get(case_id, "rpgmaker_browser.mjs")
    completed = subprocess.run(
        [str(ROOT / ".cache" / "tools" / "node-v24.18.0-linux-x64" / "bin" / "node"),
         str(ROOT / "scripts" / "acceptance" / browser_driver)],
        cwd=ROOT, env=environment, text=True, check=False,
    )
    if completed.returncode != 0:
        return completed.returncode
    payload = json.loads((case_dir / "rpgmaker-product.json").read_text(encoding="utf-8"))
    if detector_matrix is not None:
        payload["detectorMatrix"] = detector_matrix
        (case_dir / "rpgmaker-product.json").write_text(
            json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8",
        )
    if case_id == PACK_CASE:
        evidence_path = case_dir / "rpgmaker-product.json"
        payload["databaseEvidence"] = inspect_pack_evidence(
            Path(os.environ["RETROM_ACC_RPG_009_PLAN"]),
            Path(os.environ["RETROM_ACC_RPG_009_DATABASE"]),
            evidence_path,
        )
        payload["status"] = "PASS"
        evidence_path.write_text(
            json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8",
        )
    if spec:
        assert input_provenance is not None
        restored_logical = f"screenshots/{case_id.lower()}-restored-marker.png"
        marker = str(input_provenance["marker"])
        marker_rgb = input_provenance["markerRgb"]
        payload["inputProvenance"] = input_provenance
        payload["restoreVisualEvidence"] = png_visual_evidence(
            case_dir / restored_logical, restored_logical, marker, marker_rgb,
            MZ_SCENE_EXCLUSION if case_id == "ACC-RPG-008" else None,
        )
        runtime_environment = payload.get("runtimeEnvironment")
        if not isinstance(runtime_environment, dict):
            raise ContractError("RPG_ACCEPTANCE_RUNTIME_ENVIRONMENT_INVALID")
        runtime_environment.update({
            "engineVersion": input_provenance["engineVersion"],
            "engineProfile": ENGINE_PROFILES[spec.generation],
            "gateDurationsMs": gate_durations(payload["validation"]["machineGates"]),
        })
        if case_id == "ACC-RPG-004" and os.environ.get("RETROM_ACC_RPG_004_TRACE"):
            payload["xpRuntimeTrace"] = read_json_file(
                os.environ["RETROM_ACC_RPG_004_TRACE"], "XP_TRACE",
            )
        (case_dir / "rpgmaker-product.json").write_text(
            json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8",
        )
        validate_generation_evidence(payload, spec, expected_digest)
    elif case_id == PACK_CASE:
        validate_pack_evidence(payload)
    elif case_id in SECURITY_CASES:
        validate_security_evidence(payload, case_id)
    elif payload.get("status") != "PASS":
        raise ContractError("RPG_ACCEPTANCE_CATALOG_NOT_PASS")
    return 0


def main() -> int:
    if len(sys.argv) != 2 or sys.argv[1] not in {
        "ACC-RPG-001", PACK_CASE, COMPATIBILITY_CASE, *GENERATION_CASES, *SECURITY_CASES, *DEFERRED_CASES,
    }:
        print("usage: rpgmaker_case.py ACC-RPG-001..012", file=sys.stderr)
        return 2
    case_dir_value = os.environ.get("RETROM_ACCEPTANCE_CASE_DIR")
    if not case_dir_value:
        print("RETROM_ACCEPTANCE_CASE_DIR is required", file=sys.stderr)
        return 2
    try:
        return run(sys.argv[1], Path(case_dir_value))
    except (ContractError, OSError, ValueError, json.JSONDecodeError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
