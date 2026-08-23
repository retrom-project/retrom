"""CRC32 patching for project-owned arcade fixture payloads."""

from __future__ import annotations

import binascii


def force_crc32(content: bytes, target: int, patch_offset: int | None = None) -> bytes:
    """Set four generator-owned bytes so the complete payload has ``target`` CRC32."""
    if len(content) < 4:
        raise ValueError("CRC32 correction requires at least four bytes")
    if patch_offset is None:
        patch_offset = len(content) - 4
    if patch_offset < 0 or patch_offset + 4 > len(content):
        raise ValueError("CRC32 correction offset is outside the payload")

    baseline = bytearray(content)
    baseline[patch_offset : patch_offset + 4] = bytes(4)
    baseline_crc = binascii.crc32(baseline) & 0xFFFFFFFF
    effects: list[int] = []
    for bit in range(32):
        candidate = bytearray(baseline)
        candidate[patch_offset + bit // 8] ^= 1 << (bit % 8)
        effects.append((binascii.crc32(candidate) & 0xFFFFFFFF) ^ baseline_crc)

    desired = target ^ baseline_crc
    rows: list[int] = []
    for output_bit in range(32):
        coefficients = sum(
            ((effect >> output_bit) & 1) << input_bit
            for input_bit, effect in enumerate(effects)
        )
        rows.append(coefficients | (((desired >> output_bit) & 1) << 32))

    pivot_row_for_column: dict[int, int] = {}
    next_row = 0
    for column in range(32):
        pivot = next(
            (row for row in range(next_row, 32) if rows[row] & (1 << column)),
            None,
        )
        if pivot is None:
            continue
        rows[next_row], rows[pivot] = rows[pivot], rows[next_row]
        for row in range(32):
            if row != next_row and rows[row] & (1 << column):
                rows[row] ^= rows[next_row]
        pivot_row_for_column[column] = next_row
        next_row += 1
    if next_row != 32:
        raise ValueError("CRC32 correction matrix is not invertible")

    correction = 0
    for column, row in pivot_row_for_column.items():
        correction |= ((rows[row] >> 32) & 1) << column
    baseline[patch_offset : patch_offset + 4] = correction.to_bytes(4, "little")
    result = bytes(baseline)
    if binascii.crc32(result) & 0xFFFFFFFF != target:
        raise ValueError("CRC32 correction failed")
    return result
