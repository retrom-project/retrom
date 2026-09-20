"""Derive local acceptance ISO/CSO inputs without modifying source game sectors."""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import struct
import uuid
import zlib

SECTOR = 2048


def source_size(path: Path) -> tuple[int, int]:
    size = path.stat().st_size
    if size < 17 * SECTOR or size % SECTOR or size + SECTOR >= 2**31:
        raise ValueError("PSP_RUN_ISO_SIZE_INVALID")
    with path.open("rb") as stream:
        stream.seek(16 * SECTOR)
        descriptor = stream.read(SECTOR)
    little = struct.unpack_from("<I", descriptor, 80)[0]
    big = struct.unpack_from(">I", descriptor, 84)[0]
    if descriptor[:7] != b"\x01CD001\x01" or little != big or not 0 < little * SECTOR + SECTOR < 2**31:
        raise ValueError("PSP_RUN_ISO_DESCRIPTOR_INVALID")
    return size, max(size, little * SECTOR)


def sectors(path: Path, marker: bytes, padding: int):
    with path.open("rb") as stream:
        while data := stream.read(SECTOR):
            yield data
    for _ in range(padding):
        yield bytes(SECTOR)
    # ISO volume descriptors retain their original logical sector count. This
    # trailing provenance sector is outside the game volume and never replaces it.
    yield marker.ljust(SECTOR, b"\0")


def write_cso(stream, blocks, logical_size: int) -> None:
    count = logical_size // SECTOR
    stream.write(struct.pack("<4sIQIBB2x", b"CISO", 24, logical_size, SECTOR, 1, 0))
    stream.write(bytes((count + 1) * 4))
    indexes = []
    for block in blocks:
        compressor = zlib.compressobj(level=1, wbits=-15)
        compressed = compressor.compress(block) + compressor.flush()
        plain = len(compressed) >= len(block)
        indexes.append(stream.tell() | (0x80000000 if plain else 0))
        stream.write(block if plain else compressed)
    indexes.append(stream.tell())
    if len(indexes) != count + 1 or indexes[-1] >= 2**31:
        raise ValueError("PSP_RUN_CSO_INDEX_INVALID")
    stream.seek(24)
    stream.write(struct.pack(f"<{len(indexes)}I", *indexes))


def digest(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def create_run_disc(source: Path, destination: Path, output_format: str, run_id: str) -> dict:
    if output_format not in {"iso", "cso"} or str(uuid.UUID(run_id)) != run_id:
        raise ValueError("PSP_RUN_ARGUMENT_INVALID")
    size, volume_size = source_size(source)
    marker = ("RETROM_ACCEPTANCE_RUN:" + run_id).encode("ascii")
    with destination.open("xb") as stream:
        blocks = sectors(source, marker, (volume_size - size) // SECTOR)
        if output_format == "cso":
            write_cso(stream, blocks, volume_size + SECTOR)
        else:
            for block in blocks:
                stream.write(block)
    return {"schemaVersion": 1, "recipe": "ISO_TRAILING_PROVENANCE_SECTOR_V1",
            "format": output_format.upper(), "runId": run_id,
            "sourceSizeBytes": size, "sourceSha256": digest(source),
            "outputSizeBytes": destination.stat().st_size, "outputSha256": digest(destination),
            "unchangedSourceSectors": size // SECTOR, "addedLogicalSectors": 1, "paddedEmptySectors": (volume_size - size) // SECTOR}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("destination", type=Path)
    parser.add_argument("--format", choices=("iso", "cso"), required=True)
    args = parser.parse_args()
    receipt = create_run_disc(args.source, args.destination, args.format, str(uuid.uuid4()))
    args.destination.with_suffix(args.destination.suffix + ".source.json").write_text(
        json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(receipt))


if __name__ == "__main__":
    main()
