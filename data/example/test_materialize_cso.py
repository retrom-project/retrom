#!/usr/bin/env python3
"""Regression tests for the fixture-only CISO v1 materializer."""

from __future__ import annotations

import hashlib
import importlib.util
import struct
import tempfile
import unittest
import zlib
from pathlib import Path
from typing import Any


SCRIPT_PATH = Path(__file__).with_name("materialize-cso.py")
SPEC = importlib.util.spec_from_file_location("retrom_materialize_cso", SCRIPT_PATH)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def raw_deflate(payload: bytes) -> bytes:
    compressor = zlib.compressobj(level=9, wbits=-15)
    return compressor.compress(payload) + compressor.flush()


def build_iso(include_pvd: bool = True) -> bytes:
    payload = bytearray(16 * 2048 + 100)
    for index in range(len(payload)):
        payload[index] = index % 251
    if include_pvd:
        payload[16 * 2048] = 1
        payload[16 * 2048 + 1 : 16 * 2048 + 6] = b"CD001"
    return bytes(payload)


def build_cso(iso: bytes, *, header_size: int = 24, version: int = 1) -> bytes:
    block_size = 2048
    blocks = [iso[offset : offset + block_size] for offset in range(0, len(iso), block_size)]
    index_size = (len(blocks) + 1) * 4
    cursor = 24 + index_size
    index: list[int] = []
    encoded_blocks: list[bytes] = []
    for block_number, block in enumerate(blocks):
        compressed = raw_deflate(block)
        uncompressed = block_number % 3 == 1
        payload = block if uncompressed else compressed
        index.append(cursor | (0x80000000 if uncompressed else 0))
        encoded_blocks.append(payload)
        cursor += len(payload)
    index.append(cursor)
    header = struct.pack("<4sIQIBB2s", b"CISO", header_size, len(iso), block_size, version, 0, b"\0\0")
    return header + struct.pack(f"<{len(index)}I", *index) + b"".join(encoded_blocks)


def sha256(payload: bytes) -> str:
    return hashlib.sha256(payload).hexdigest()


class MaterializeCSOTests(unittest.TestCase):
    def setUp(self) -> None:
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.iso = build_iso()
        self.cso = build_cso(self.iso)
        self.source = self.root / "fixture.cso"
        self.target = self.root / "fixture.iso"
        self.source.write_bytes(self.cso)

    def materialize(self, **overrides: Any) -> bool:
        source_hash = overrides.get("source_hash")
        if source_hash is None:
            source_hash = sha256(self.source.read_bytes())
        return MODULE.materialize(
            overrides.get("source", self.source),
            overrides.get("target", self.target),
            source_hash,
            overrides.get("target_hash", sha256(self.iso)),
            overrides.get("target_size", len(self.iso)),
        )

    def assert_rejected(self) -> None:
        with self.assertRaises(MODULE.MaterializationError):
            self.materialize()
        self.assertFalse(self.target.exists())
        self.assertEqual(list(self.root.glob(".fixture.iso.*.tmp")), [])

    def test_compressed_uncompressed_last_block_and_idempotency(self) -> None:
        self.assertTrue(self.materialize())
        self.assertEqual(self.target.read_bytes(), self.iso)
        self.source.unlink()
        self.assertFalse(self.materialize(source_hash=sha256(self.cso)))

    def test_legacy_zero_header_size(self) -> None:
        self.source.write_bytes(build_cso(self.iso, header_size=0))
        self.assertTrue(self.materialize())

    def test_rejects_magic_version_and_header(self) -> None:
        for name, mutation in (
            ("magic", lambda value: b"NOPE" + value[4:]),
            ("version", lambda value: value[:20] + b"\x02" + value[21:]),
            ("header", lambda value: value[:4] + struct.pack("<I", 25) + value[8:]),
        ):
            with self.subTest(name=name):
                self.target.unlink(missing_ok=True)
                self.source.write_bytes(mutation(self.cso))
                self.assert_rejected()

    def test_rejects_non_monotonic_and_out_of_bounds_index(self) -> None:
        for name, replacement in (("non-monotonic", 1), ("out-of-bounds", len(self.cso) + 1)):
            with self.subTest(name=name):
                self.target.unlink(missing_ok=True)
                damaged = bytearray(self.cso)
                struct.pack_into("<I", damaged, 24 + 4, replacement)
                self.source.write_bytes(damaged)
                self.assert_rejected()

    def test_rejects_damaged_deflate_and_wrong_block_length(self) -> None:
        first_offset = struct.unpack_from("<I", self.cso, 24)[0] & 0x7FFFFFFF
        for name, mutation in (
            ("deflate", lambda value: value.__setitem__(first_offset, value[first_offset] ^ 0xFF)),
            ("length", lambda value: struct.pack_into("<I", value, 28, first_offset + 1)),
        ):
            with self.subTest(name=name):
                self.target.unlink(missing_ok=True)
                damaged = bytearray(self.cso)
                mutation(damaged)
                self.source.write_bytes(damaged)
                self.assert_rejected()

    def test_rejects_source_and_target_hash_mismatch(self) -> None:
        with self.assertRaises(MODULE.MaterializationError):
            self.materialize(source_hash="0" * 64)
        with self.assertRaises(MODULE.MaterializationError):
            self.materialize(target_hash="0" * 64)
        self.assertFalse(self.target.exists())

    def test_rejects_missing_primary_volume_descriptor(self) -> None:
        invalid_iso = build_iso(include_pvd=False)
        self.source.write_bytes(build_cso(invalid_iso))
        with self.assertRaises(MODULE.MaterializationError):
            self.materialize(
                source_hash=sha256(self.source.read_bytes()),
                target_hash=sha256(invalid_iso),
                target_size=len(invalid_iso),
            )
        self.assertFalse(self.target.exists())

    def test_rejects_truncated_source_and_cleans_temporary_file(self) -> None:
        self.source.write_bytes(self.cso[:-1])
        self.assert_rejected()


if __name__ == "__main__":
    unittest.main()
