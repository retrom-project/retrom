#!/usr/bin/env python3
"""Build Retrom's project-owned MAME 2003 and FBNeo smoke fixtures."""

from __future__ import annotations

import argparse
import binascii
import hashlib
import io
import shutil
import tempfile
import zipfile
from dataclasses import dataclass
from pathlib import Path
from xml.etree import ElementTree


OUTPUT_ROOT = Path(__file__).resolve().parent
PROGRAM_NAMES = ("pacman.6e", "pacman.6f", "pacman.6h", "pacman.6j")
PARENT_NAMES = (
    "pacman.5e",
    "pacman.5f",
    "82s123.7f",
    "82s126.4a",
    "82s126.1m",
    "82s126.3m",
)
MAME2003_OUTPUT_NAMES = (
    "pacman.zip",
    "puckman.zip",
    "retrombios.zip",
    "mame2003-smoke.xml",
)
FBNEO_OUTPUT_NAMES = (
    "fbneo/pacman.zip",
    "fbneo/puckman.zip",
    "fbneo/retrombios.zip",
    "fbneo/fbneo-smoke.dat",
)
OUTPUT_NAMES = MAME2003_OUTPUT_NAMES + FBNEO_OUTPUT_NAMES
FBNEO_PROGRAM_CRC32 = {
    "pacman.6e": 0xC1E6AB10,
    "pacman.6f": 0x1A6FB2D4,
    "pacman.6h": 0xBCDD1BEB,
    "pacman.6j": 0x817D94E3,
}
FBNEO_GRAPHICS_CRC32 = {
    "pacman.5e": 0x0C944964,
    "pacman.5f": 0x958FEDF9,
}
FBNEO_PARENT_CRC32 = {
    "pm1-1.7f": 0x2FC650BD,
    "pm1-4.4a": 0x3EB3A8E4,
    "pm1-3.1m": 0xA9CC86BF,
    "pm1-2.3m": 0x77245B66,
}
FBNEO_MERGES = {
    "82s123.7f": "pm1-1.7f",
    "82s126.4a": "pm1-4.4a",
    "82s126.1m": "pm1-3.1m",
    "82s126.3m": "pm1-2.3m",
}


@dataclass(frozen=True)
class RelativeFixup:
    operand_offset: int
    label: str


class Z80Program:
    def __init__(self) -> None:
        self.code = bytearray()
        self.labels: dict[str, int] = {}
        self.relative_fixups: list[RelativeFixup] = []

    def emit(self, *values: int) -> None:
        self.code.extend(value & 0xFF for value in values)

    def emit_word(self, opcode: int, value: int) -> None:
        self.emit(opcode, value, value >> 8)

    def label(self, name: str) -> None:
        if name in self.labels:
            raise ValueError(f"duplicate label: {name}")
        self.labels[name] = len(self.code)

    def relative(self, opcode: int, label: str) -> None:
        self.emit(opcode, 0)
        self.relative_fixups.append(RelativeFixup(len(self.code) - 1, label))

    def build(self) -> bytes:
        result = bytearray(self.code)
        for fixup in self.relative_fixups:
            if fixup.label not in self.labels:
                raise ValueError(f"unknown label: {fixup.label}")
            displacement = self.labels[fixup.label] - (fixup.operand_offset + 1)
            if not -128 <= displacement <= 127:
                raise ValueError(f"relative branch is out of range: {fixup.label}")
            result[fixup.operand_offset] = displacement & 0xFF
        return bytes(result)


def build_program() -> bytes:
    program = Z80Program()
    program.emit(0xF3)  # DI
    program.emit_word(0x31, 0x4FF0)  # LD SP,$4ff0
    program.emit(0xAF)  # XOR A
    program.emit_word(0x32, 0x5000)  # interrupt enable = 0
    program.emit_word(0x32, 0x5001)  # sound enable = 0

    program.emit_word(0x21, 0x4000)  # LD HL,video RAM
    program.emit_word(0x01, 0x0400)  # LD BC,1024
    program.emit(0x3E, 0x01)  # LD A,1
    program.label("fill_video")
    program.emit(0x77, 0x3C, 0xE6, 0x03)  # LD (HL),A; INC A; AND 3
    program.relative(0x20, "video_value_ready")  # JR NZ
    program.emit(0x3E, 0x01)
    program.label("video_value_ready")
    program.emit(0x23, 0x0B, 0x78, 0xB1)  # INC HL; DEC BC; LD A,B; OR C
    program.relative(0x20, "fill_video")

    program.emit_word(0x21, 0x4400)  # LD HL,color RAM
    program.emit_word(0x01, 0x0400)
    program.emit(0x3E, 0x01)
    program.label("fill_color")
    program.emit(0x77, 0x23, 0x0B, 0x78, 0xB1)
    program.relative(0x20, "fill_color")

    program.label("animate")
    program.emit_word(0x21, 0x4000)
    program.emit_word(0x01, 0x0400)
    program.label("animate_tiles")
    program.emit(0x34, 0x23, 0x0B, 0x78, 0xB1)  # INC (HL); INC HL; DEC BC; LD A,B; OR C
    program.relative(0x20, "animate_tiles")
    program.emit_word(0x32, 0x50C0)  # watchdog reset
    program.emit_word(0x01, 0xFFFF)
    program.label("delay")
    program.emit(0x0B, 0x78, 0xB1)  # DEC BC; LD A,B; OR C
    program.relative(0x20, "delay")
    program.relative(0x18, "animate")

    image = bytearray(0x4000)
    code = program.build()
    image[: len(code)] = code
    marker = b"RETROM PUBLIC ARCADE SMOKE - ORIGINAL MIT-LICENSED Z80 PROGRAM"
    image[0x3F00 : 0x3F00 + len(marker)] = marker
    return bytes(image)


def build_character_rom() -> bytes:
    image = bytearray(0x1000)
    for character in range(256):
        base = character * 16
        for row in range(8):
            value = (character * 17 + row * 29) & 0xFF
            image[base + row] = value
            image[base + 8 + row] = value ^ 0xFF
    marker = b"RETROM-CHARS"
    image[-len(marker) :] = marker
    return bytes(image)


def build_sprite_rom() -> bytes:
    image = bytearray(0x1000)
    for sprite in range(64):
        base = sprite * 64
        for offset in range(64):
            image[base + offset] = (sprite * 37 + offset * 11) & 0xFF
    marker = b"RETROM-SPRITES"
    image[-len(marker) :] = marker
    return bytes(image)


def build_palette_prom() -> bytes:
    return bytes((0x00, 0x07, 0x38, 0xC0, 0x3F, 0xC7, 0xF8, 0xFF) * 4)


def build_lookup_prom() -> bytes:
    return bytes(index % 4 for index in range(256))


def force_crc32(content: bytes, target: int, patch_offset: int | None = None) -> bytes:
    """Set four generator-controlled bytes so the payload has a driver-locked CRC32."""
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


def deterministic_zip(entries: dict[str, bytes]) -> bytes:
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_STORED) as archive:
        for name, content in entries.items():
            info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_STORED
            info.create_system = 3
            info.external_attr = 0o100644 << 16
            archive.writestr(info, content)
    return output.getvalue()


def rom_attributes(name: str, content: bytes, **extra: str) -> dict[str, str]:
    attributes = {
        "name": name,
        "size": str(len(content)),
        "crc": f"{binascii.crc32(content) & 0xFFFFFFFF:08x}",
        "sha1": hashlib.sha1(content, usedforsecurity=False).hexdigest(),
        "status": "good",
    }
    attributes.update(extra)
    return attributes


def build_mame2003_dat(programs: dict[str, bytes], parent: dict[str, bytes], bios: dict[str, bytes]) -> bytes:
    root = ElementTree.Element("mame", {"build": "retrom-public-arcade-smoke-v1"})
    bios_machine = ElementTree.SubElement(
        root,
        "game",
        {"name": "retrombios", "isbios": "yes", "runnable": "no"},
    )
    ElementTree.SubElement(bios_machine, "description").text = "Retrom public test BIOS archive"
    for name, content in bios.items():
        ElementTree.SubElement(bios_machine, "rom", rom_attributes(name, content, region="cpu1", offset="0"))

    parent_machine = ElementTree.SubElement(
        root,
        "game",
        {"name": "puckman", "romof": "retrombios", "runnable": "yes"},
    )
    ElementTree.SubElement(parent_machine, "description").text = "Retrom public parent fixture"
    for ordinal, (name, content) in enumerate(parent.items()):
        ElementTree.SubElement(
            parent_machine,
            "rom",
            rom_attributes(name, content, region="user1", offset=f"{ordinal:x}"),
        )

    child_machine = ElementTree.SubElement(
        root,
        "game",
        {
            "name": "pacman",
            "cloneof": "puckman",
            "romof": "retrombios",
            "runnable": "yes",
        },
    )
    ElementTree.SubElement(child_machine, "description").text = "Retrom Arcade Smoke"
    for ordinal, (name, content) in enumerate(programs.items()):
        ElementTree.SubElement(
            child_machine,
            "rom",
            rom_attributes(name, content, region="cpu1", offset=f"{ordinal * 0x1000:x}"),
        )
    for ordinal, (name, content) in enumerate(parent.items()):
        ElementTree.SubElement(
            child_machine,
            "rom",
            rom_attributes(name, content, merge=name, region="user1", offset=f"{ordinal:x}"),
        )
    ElementTree.indent(root, space="  ")
    return b'<?xml version="1.0" encoding="UTF-8"?>\n' + ElementTree.tostring(root, encoding="utf-8") + b"\n"


def build_fbneo_dat(
    child: dict[str, bytes],
    parent: dict[str, bytes],
    bios: dict[str, bytes],
) -> bytes:
    root = ElementTree.Element("datafile")
    header = ElementTree.SubElement(root, "header")
    ElementTree.SubElement(header, "name").text = "Retrom public FBNeo smoke"
    ElementTree.SubElement(header, "description").text = "Retrom public FBNeo smoke"
    ElementTree.SubElement(header, "version").text = "1"

    bios_machine = ElementTree.SubElement(
        root,
        "game",
        {"name": "retrombios", "isbios": "yes"},
    )
    ElementTree.SubElement(bios_machine, "description").text = "Retrom public test BIOS archive"
    for name, content in bios.items():
        ElementTree.SubElement(bios_machine, "rom", rom_attributes(name, content))

    parent_machine = ElementTree.SubElement(
        root,
        "game",
        {"name": "puckman", "romof": "retrombios"},
    )
    ElementTree.SubElement(parent_machine, "description").text = "Retrom public FBNeo parent fixture"
    for name, content in parent.items():
        ElementTree.SubElement(parent_machine, "rom", rom_attributes(name, content))

    child_machine = ElementTree.SubElement(
        root,
        "game",
        {"name": "pacman", "cloneof": "puckman", "romof": "retrombios"},
    )
    ElementTree.SubElement(child_machine, "description").text = "Retrom FBNeo Arcade Smoke"
    for name, content in child.items():
        ElementTree.SubElement(child_machine, "rom", rom_attributes(name, content))
    for child_name, parent_name in FBNEO_MERGES.items():
        ElementTree.SubElement(
            child_machine,
            "rom",
            rom_attributes(child_name, parent[parent_name], merge=parent_name),
        )
    ElementTree.indent(root, space="  ")
    return b'<?xml version="1.0" encoding="UTF-8"?>\n' + ElementTree.tostring(root, encoding="utf-8") + b"\n"


def build_outputs() -> dict[str, bytes]:
    program = build_program()
    programs = {
        name: program[index * 0x1000 : (index + 1) * 0x1000]
        for index, name in enumerate(PROGRAM_NAMES)
    }
    parent = {
        "pacman.5e": build_character_rom(),
        "pacman.5f": build_sprite_rom(),
        "82s123.7f": build_palette_prom(),
        "82s126.4a": build_lookup_prom(),
        "82s126.1m": bytes(0x100),
        "82s126.3m": bytes(0x100),
    }
    bios = {"retrom-test-bios.bin": b"RETROM TEST BIOS CONTRACT V1\n" + bytes(229)}
    fbneo_programs = {
        name: force_crc32(content, FBNEO_PROGRAM_CRC32[name], 0x0EFC if index == 3 else None)
        for index, (name, content) in enumerate(programs.items())
    }
    fbneo_graphics = {
        "pacman.5e": force_crc32(build_character_rom(), FBNEO_GRAPHICS_CRC32["pacman.5e"], 0x0FE0),
        "pacman.5f": force_crc32(build_sprite_rom(), FBNEO_GRAPHICS_CRC32["pacman.5f"], 0x0FE0),
    }
    fbneo_child = {**fbneo_programs, **fbneo_graphics}
    parent_sources = {
        "pm1-1.7f": build_palette_prom(),
        "pm1-4.4a": build_lookup_prom(),
        "pm1-3.1m": bytes(0x100),
        "pm1-2.3m": bytes(0x100),
    }
    fbneo_parent = {
        name: force_crc32(content, FBNEO_PARENT_CRC32[name])
        for name, content in parent_sources.items()
    }
    return {
        "pacman.zip": deterministic_zip(programs),
        "puckman.zip": deterministic_zip(parent),
        "retrombios.zip": deterministic_zip(bios),
        "mame2003-smoke.xml": build_mame2003_dat(programs, parent, bios),
        "fbneo/pacman.zip": deterministic_zip(fbneo_child),
        "fbneo/puckman.zip": deterministic_zip(fbneo_parent),
        "fbneo/retrombios.zip": deterministic_zip(bios),
        "fbneo/fbneo-smoke.dat": build_fbneo_dat(fbneo_child, fbneo_parent, bios),
    }


def check_outputs(outputs: dict[str, bytes]) -> None:
    for name in OUTPUT_NAMES:
        path = OUTPUT_ROOT / name
        if not path.is_file() or path.read_bytes() != outputs[name]:
            raise SystemExit(f"public arcade fixture drifted: {name}; run build.py")


def write_outputs(outputs: dict[str, bytes]) -> None:
    with tempfile.TemporaryDirectory(prefix="retrom-arcade-smoke-", dir=OUTPUT_ROOT) as temporary:
        temporary_root = Path(temporary)
        for name in OUTPUT_NAMES:
            path = temporary_root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(outputs[name])
        for name in OUTPUT_NAMES:
            destination = OUTPUT_ROOT / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.move(temporary_root / name, destination)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    arguments = parser.parse_args()
    outputs = build_outputs()
    if arguments.check:
        check_outputs(outputs)
    else:
        write_outputs(outputs)


if __name__ == "__main__":
    main()
