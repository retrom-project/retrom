#!/usr/bin/env python3
"""Deterministically materialize the pinned PPSSPP CISO v1 fixture as ISO."""

from __future__ import annotations

import argparse
import hashlib
import os
import struct
import tempfile
import zlib
from pathlib import Path


HEADER_SIZE = 24
ISO_SECTOR_SIZE = 2048
PVD_OFFSET = 16 * ISO_SECTOR_SIZE
MAX_FIXTURE_BYTES = 64 * 1024 * 1024


class MaterializationError(ValueError):
    """The source or derived fixture violates the closed CISO contract."""


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def valid_iso(path: Path, expected_size: int, expected_sha256: str) -> bool:
    if not path.is_file() or path.stat().st_size != expected_size:
        return False
    if file_sha256(path) != expected_sha256.lower():
        return False
    with path.open("rb") as source:
        source.seek(PVD_OFFSET + 1)
        return source.read(5) == b"CD001"


def _parse_header(source: object, source_size: int) -> tuple[int, int, int, int]:
    header = source.read(HEADER_SIZE)
    if len(header) != HEADER_SIZE:
        raise MaterializationError("truncated CISO header")
    magic, header_size, total_bytes, block_size, version, align, reserved = struct.unpack(
        "<4sIQIBB2s", header
    )
    if magic != b"CISO" or header_size not in (0, HEADER_SIZE) or version != 1:
        raise MaterializationError("unsupported CISO header")
    if reserved != b"\0\0" or total_bytes <= 0 or total_bytes > MAX_FIXTURE_BYTES:
        raise MaterializationError("invalid CISO header values")
    if block_size != ISO_SECTOR_SIZE or align > 31:
        raise MaterializationError("unsupported CISO block geometry")
    block_count = (total_bytes + block_size - 1) // block_size
    index_offset = HEADER_SIZE if header_size == 0 else header_size
    index_bytes = (block_count + 1) * 4
    if index_offset + index_bytes > source_size:
        raise MaterializationError("truncated CISO index")
    return total_bytes, block_size, block_count, index_offset


def _read_index(source: object, count: int, index_offset: int, align: int, source_size: int) -> list[int]:
    source.seek(index_offset)
    encoded = source.read(count * 4)
    if len(encoded) != count * 4:
        raise MaterializationError("truncated CISO index")
    values = list(struct.unpack(f"<{count}I", encoded))
    offsets = [(value & 0x7FFFFFFF) << align for value in values]
    minimum_payload_offset = index_offset + count * 4
    if offsets[0] < minimum_payload_offset or offsets[-1] != source_size:
        raise MaterializationError("CISO index is outside the source payload")
    if any(left > right for left, right in zip(offsets, offsets[1:])):
        raise MaterializationError("CISO index is not monotonic")
    return values


def materialize(
    source_path: Path,
    target_path: Path,
    expected_source_sha256: str,
    expected_target_sha256: str,
    expected_target_size: int,
) -> bool:
    """Materialize once; return False when an already valid target is reused."""

    source_path = Path(source_path)
    target_path = Path(target_path)
    expected_source_sha256 = expected_source_sha256.lower()
    expected_target_sha256 = expected_target_sha256.lower()
    if valid_iso(target_path, expected_target_size, expected_target_sha256):
        return False
    if not source_path.is_file() or file_sha256(source_path) != expected_source_sha256:
        raise MaterializationError("CISO source SHA-256 mismatch")

    source_size = source_path.stat().st_size
    if source_size < HEADER_SIZE or source_size > MAX_FIXTURE_BYTES:
        raise MaterializationError("CISO source size is outside the fixture boundary")
    target_path.parent.mkdir(parents=True, exist_ok=True)
    temporary_name: str | None = None
    try:
        with source_path.open("rb") as source:
            total_bytes, block_size, block_count, index_offset = _parse_header(source, source_size)
            source.seek(21)
            align_raw = source.read(1)
            if len(align_raw) != 1:
                raise MaterializationError("truncated CISO alignment")
            align = align_raw[0]
            index = _read_index(source, block_count + 1, index_offset, align, source_size)
            output_hash = hashlib.sha256()
            written = 0
            with tempfile.NamedTemporaryFile(
                mode="w+b",
                dir=target_path.parent,
                prefix=f".{target_path.name}.",
                suffix=".tmp",
                delete=False,
            ) as destination:
                temporary_name = destination.name
                for block_number in range(block_count):
                    encoded_start = index[block_number]
                    encoded_end = index[block_number + 1]
                    start = (encoded_start & 0x7FFFFFFF) << align
                    end = (encoded_end & 0x7FFFFFFF) << align
                    expected_length = min(block_size, total_bytes - written)
                    if end <= start or end > source_size:
                        raise MaterializationError("CISO block offset is invalid")
                    source.seek(start)
                    payload = source.read(end - start)
                    if len(payload) != end - start:
                        raise MaterializationError("truncated CISO block")
                    if encoded_start & 0x80000000:
                        decoded = payload
                    else:
                        decoder = zlib.decompressobj(wbits=-15)
                        try:
                            decoded = decoder.decompress(payload, expected_length + 1) + decoder.flush()
                        except zlib.error as error:
                            raise MaterializationError("damaged CISO DEFLATE block") from error
                        if not decoder.eof or decoder.unused_data or decoder.unconsumed_tail:
                            raise MaterializationError("CISO DEFLATE block has trailing data")
                    if len(decoded) != expected_length:
                        raise MaterializationError("CISO block decoded to the wrong length")
                    destination.write(decoded)
                    output_hash.update(decoded)
                    written += len(decoded)
                if written != total_bytes or written != expected_target_size:
                    raise MaterializationError("CISO output size mismatch")
                destination.flush()
                os.fsync(destination.fileno())
                destination.seek(PVD_OFFSET + 1)
                if destination.read(5) != b"CD001":
                    raise MaterializationError("derived ISO is missing the CD001 descriptor")
                if output_hash.hexdigest() != expected_target_sha256:
                    raise MaterializationError("derived ISO SHA-256 mismatch")
        os.replace(temporary_name, target_path)
        temporary_name = None
        return True
    except (OSError, struct.error) as error:
        if isinstance(error, MaterializationError):
            raise
        raise MaterializationError("unable to materialize CISO fixture") from error
    finally:
        if temporary_name is not None:
            try:
                os.unlink(temporary_name)
            except FileNotFoundError:
                pass


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("target", type=Path)
    parser.add_argument("--source-sha256", required=True)
    parser.add_argument("--target-sha256", required=True)
    parser.add_argument("--target-size", required=True, type=int)
    arguments = parser.parse_args()
    materialize(
        arguments.source,
        arguments.target,
        arguments.source_sha256,
        arguments.target_sha256,
        arguments.target_size,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
