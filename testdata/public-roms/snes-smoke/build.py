#!/usr/bin/env python3
"""Build Retrom's project-owned deterministic SNES netplay fixture."""

from __future__ import annotations

import argparse
import hashlib
import shutil
import tempfile
from dataclasses import dataclass
from pathlib import Path


OUTPUT_ROOT = Path(__file__).resolve().parent
OUTPUT_NAME = "snes-smoke.sfc"
ROM_SIZE = 32 * 1024
ROM_BASE = 0x8000


@dataclass(frozen=True)
class Fixup:
    offset: int
    label: str
    kind: str


class Assembler:
    def __init__(self) -> None:
        self.code = bytearray()
        self.labels: dict[str, int] = {}
        self.fixups: list[Fixup] = []

    def emit(self, *values: int) -> None:
        self.code.extend(value & 0xFF for value in values)

    def label(self, name: str) -> None:
        if name in self.labels:
            raise ValueError(f"duplicate label: {name}")
        self.labels[name] = len(self.code)

    def relative(self, opcode: int, label: str) -> None:
        self.emit(opcode, 0)
        self.fixups.append(Fixup(len(self.code) - 1, label, "relative"))

    def immediate_label(self, opcode: int, label: str, byte: str) -> None:
        self.emit(opcode, 0)
        self.fixups.append(Fixup(len(self.code) - 1, label, byte))

    def append(self, name: str, value: bytes) -> None:
        self.label(name)
        self.code.extend(value)

    def build(self) -> tuple[bytes, dict[str, int]]:
        result = bytearray(self.code)
        for fixup in self.fixups:
            if fixup.label not in self.labels:
                raise ValueError(f"unknown label: {fixup.label}")
            address = ROM_BASE + self.labels[fixup.label]
            if fixup.kind == "relative":
                displacement = self.labels[fixup.label] - (fixup.offset + 1)
                if not -128 <= displacement <= 127:
                    raise ValueError(f"relative branch is out of range: {fixup.label}")
                result[fixup.offset] = displacement & 0xFF
            elif fixup.kind == "low":
                result[fixup.offset] = address & 0xFF
            elif fixup.kind == "high":
                result[fixup.offset] = address >> 8
            else:
                raise ValueError(f"unknown fixup kind: {fixup.kind}")
        return bytes(result), {
            name: ROM_BASE + offset for name, offset in self.labels.items()
        }


def store_immediate(assembler: Assembler, value: int, address: int) -> None:
    assembler.emit(0xA9, value, 0x8D, address & 0xFF, address >> 8)


def store_zero(assembler: Assembler, address: int) -> None:
    assembler.emit(0x9C, address & 0xFF, address >> 8)


def dma_to_vram(
    assembler: Assembler,
    source_label: str,
    destination: int,
    length: int,
) -> None:
    store_immediate(assembler, destination & 0xFF, 0x2116)
    store_immediate(assembler, destination >> 8, 0x2117)
    store_immediate(assembler, 0x01, 0x4300)
    store_immediate(assembler, 0x18, 0x4301)
    assembler.immediate_label(0xA9, source_label, "low")
    assembler.emit(0x8D, 0x02, 0x43)
    assembler.immediate_label(0xA9, source_label, "high")
    assembler.emit(0x8D, 0x03, 0x43)
    store_zero(assembler, 0x4304)
    store_immediate(assembler, length & 0xFF, 0x4305)
    store_immediate(assembler, length >> 8, 0x4306)
    store_immediate(assembler, 0x01, 0x420B)


def build_tilemap() -> bytes:
    output = bytearray()
    for row in range(32):
        for column in range(32):
            tile = 3 if row < 4 else 1 if column < 16 else 2
            output.extend((tile, 0))
    return bytes(output)


def solid_tile(color: int) -> bytes:
    planes = []
    for plane in range(4):
        planes.append(bytes([0xFF if color & (1 << plane) else 0x00] * 8))
    return b"".join(
        byte for row in range(8) for byte in (planes[0][row : row + 1], planes[1][row : row + 1])
    ) + b"".join(
        byte for row in range(8) for byte in (planes[2][row : row + 1], planes[3][row : row + 1])
    )


def build_tiles() -> bytes:
    return bytes(32) + solid_tile(1) + solid_tile(2) + solid_tile(3)


def emit_reset(assembler: Assembler) -> None:
    assembler.label("reset")
    assembler.emit(0x78, 0x18, 0xFB, 0xD8)  # SEI; CLC; XCE; CLD
    assembler.emit(0xC2, 0x30, 0xA2, 0xFF, 0x1F, 0x9A)  # 16-bit A/X; stack
    assembler.emit(0xA9, 0x00, 0x00, 0x5B)  # direct page = 0
    assembler.emit(0xE2, 0x20)  # 8-bit accumulator, 16-bit index
    store_immediate(assembler, 0x80, 0x2100)  # forced blank
    for address in (0x4200, 0x420C, 0x2105, 0x2107, 0x210D, 0x210E):
        store_zero(assembler, address)
    store_zero(assembler, 0x210D)
    store_zero(assembler, 0x210E)
    store_immediate(assembler, 0x01, 0x210B)  # BG1 tile data at word $1000
    store_immediate(assembler, 0x01, 0x212C)  # BG1 on main screen
    store_immediate(assembler, 0x80, 0x2115)  # increment VRAM after high byte
    dma_to_vram(assembler, "tilemap", 0x0000, 2048)
    dma_to_vram(assembler, "tiles", 0x1000, 128)
    store_zero(assembler, 0x2121)
    for low, high in ((0x00, 0x00), (0x1F, 0x00), (0xE0, 0x03), (0x00, 0x7C)):
        store_immediate(assembler, low, 0x2122)
        store_immediate(assembler, high, 0x2122)
    for address in (0x0000, 0x0001, 0x0002):
        store_zero(assembler, address)
    store_immediate(assembler, 0x0F, 0x2100)  # screen on, full brightness
    store_immediate(assembler, 0x01, 0x4200)  # automatic joypad, no asynchronous NMI
    assembler.relative(0x80, "frame_loop")


def emit_counter_color(assembler: Assembler, cgram_index: int, source: int, phase: int) -> None:
    store_immediate(assembler, cgram_index, 0x2121)
    assembler.emit(
        0xAD, source & 0xFF, source >> 8,  # LDA source
        0x49, phase,                       # EOR phase
        0x8D, 0x22, 0x21,                  # CGRAM low byte
        0x0A, 0x49, phase ^ 0x7C, 0x29, 0x7F,
        0x8D, 0x22, 0x21,                  # CGRAM high byte
    )


def emit_nmi(assembler: Assembler) -> None:
    assembler.label("nmi")
    assembler.emit(0x40)  # RTI; the fixture deliberately uses one busy loop
    assembler.label("irq")
    assembler.emit(0x40)
    assembler.label("frame_loop")
    assembler.label("wait_joypad")
    assembler.emit(0xAD, 0x12, 0x42, 0x29, 0x01)
    assembler.relative(0xD0, "wait_joypad")
    assembler.emit(0xEE, 0x02, 0x00)  # frame counter
    assembler.emit(0xAD, 0x18, 0x42, 0x0D, 0x19, 0x42)
    assembler.relative(0xF0, "p1_idle")
    assembler.emit(0xEE, 0x00, 0x00)
    assembler.label("p1_idle")
    assembler.emit(0xAD, 0x1A, 0x42, 0x0D, 0x1B, 0x42)
    assembler.relative(0xF0, "p2_idle")
    assembler.emit(0xEE, 0x01, 0x00)
    assembler.label("p2_idle")
    emit_counter_color(assembler, 1, 0x0000, 0x16)
    emit_counter_color(assembler, 2, 0x0001, 0x53)
    emit_counter_color(assembler, 3, 0x0002, 0x29)
    assembler.emit(0xA2, 0xFF, 0x1F)  # LDX #$1fff
    assembler.label("frame_delay")
    assembler.emit(0xCA)  # DEX
    assembler.relative(0xD0, "frame_delay")
    assembler.relative(0x80, "frame_loop")


def build_program() -> tuple[bytes, dict[str, int]]:
    assembler = Assembler()
    emit_reset(assembler)
    emit_nmi(assembler)
    assembler.append("tiles", build_tiles())
    assembler.append("tilemap", build_tilemap())
    assembler.append("marker", b"RETROM PUBLIC SNES NETPLAY SMOKE - ORIGINAL MIT-LICENSED ROM")
    return assembler.build()


def build_rom() -> bytes:
    program, labels = build_program()
    if len(program) >= 0x7FC0:
        raise ValueError("SNES fixture program overlaps the LoROM header")
    rom = bytearray(ROM_SIZE)
    rom[: len(program)] = program
    title = b"RETROM SNES SMOKE".ljust(21, b" ")
    rom[0x7FC0 : 0x7FC0 + 21] = title
    rom[0x7FD5 : 0x7FDA] = bytes((0x20, 0x00, 0x05, 0x00, 0x01))
    rom[0x7FDA : 0x7FDC] = bytes((0x33, 0x00))
    for offset in (0x7FE4, 0x7FE6, 0x7FE8, 0x7FEC, 0x7FEE, 0x7FF4, 0x7FF8, 0x7FFE):
        rom[offset : offset + 2] = labels["irq"].to_bytes(2, "little")
    for offset in (0x7FEA, 0x7FFA):
        rom[offset : offset + 2] = labels["nmi"].to_bytes(2, "little")
    rom[0x7FFC : 0x7FFE] = labels["reset"].to_bytes(2, "little")
    rom[0x7FDC : 0x7FE0] = bytes(4)
    checksum = (sum(rom) + 510) & 0xFFFF
    complement = checksum ^ 0xFFFF
    rom[0x7FDC : 0x7FE0] = complement.to_bytes(2, "little") + checksum.to_bytes(2, "little")
    if sum(rom) & 0xFFFF != checksum:
        raise ValueError("SNES checksum construction failed")
    return bytes(rom)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    arguments = parser.parse_args()
    output = build_rom()
    destination = OUTPUT_ROOT / OUTPUT_NAME
    if arguments.check:
        if not destination.is_file() or destination.read_bytes() != output:
            raise SystemExit("public SNES fixture drifted; run build.py")
        print(
            f"snes_smoke_check=passed name={OUTPUT_NAME} size={len(output)} "
            f"sha256={hashlib.sha256(output).hexdigest()}"
        )
        return
    with tempfile.TemporaryDirectory(prefix="retrom-snes-smoke-", dir=OUTPUT_ROOT) as temporary:
        generated = Path(temporary) / OUTPUT_NAME
        generated.write_bytes(output)
        shutil.move(generated, destination)
        destination.chmod(0o644)


if __name__ == "__main__":
    main()
