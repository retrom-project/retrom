#!/usr/bin/env python3
"""Build Retrom's deterministic project-owned NES netplay smoke ROMs."""

from __future__ import annotations

import argparse
import shutil
import tempfile
from dataclasses import dataclass
from pathlib import Path


OUTPUT_ROOT = Path(__file__).resolve().parent
OUTPUTS = {
    "nes-smoke.nes": b"RETROM PUBLIC NES NETPLAY SMOKE - MIT",
    "nestopia-smoke.nes": b"RETROM PUBLIC NESTOPIA NETPLAY SMOKE - MIT",
}
CPU_ORIGIN = 0x8000


@dataclass(frozen=True)
class Fixup:
    offset: int
    label: str
    relative: bool


class Assembler:
    def __init__(self) -> None:
        self.code = bytearray()
        self.labels: dict[str, int] = {}
        self.fixups: list[Fixup] = []

    def emit(self, *values: int) -> None:
        self.code.extend(value & 0xFF for value in values)

    def label(self, name: str) -> None:
        self.labels[name] = CPU_ORIGIN + len(self.code)

    def absolute(self, opcode: int, label: str) -> None:
        self.emit(opcode, 0, 0)
        self.fixups.append(Fixup(len(self.code) - 2, label, False))

    def relative(self, opcode: int, label: str) -> None:
        self.emit(opcode, 0)
        self.fixups.append(Fixup(len(self.code) - 1, label, True))

    def build(self) -> tuple[bytes, dict[str, int]]:
        result = bytearray(self.code)
        for fixup in self.fixups:
            target = self.labels[fixup.label]
            if fixup.relative:
                operand_address = CPU_ORIGIN + fixup.offset
                displacement = target - (operand_address + 1)
                if not -128 <= displacement <= 127:
                    raise ValueError(f"branch out of range: {fixup.label}")
                result[fixup.offset] = displacement & 0xFF
            else:
                result[fixup.offset : fixup.offset + 2] = target.to_bytes(2, "little")
        return bytes(result), self.labels


def build_program(marker: bytes) -> bytes:
    code = Assembler()
    code.label("reset")
    code.emit(0x78, 0xD8)  # SEI; CLD
    code.emit(0xA2, 0x40, 0x8E, 0x17, 0x40)  # LDX #$40; STX $4017
    code.emit(0xA2, 0xFF, 0x9A, 0xE8)  # LDX #$ff; TXS; INX
    code.emit(0x8E, 0x00, 0x20, 0x8E, 0x01, 0x20, 0x8E, 0x10, 0x40)
    code.label("wait_vblank_1")
    code.emit(0x2C, 0x02, 0x20)
    code.relative(0x10, "wait_vblank_1")  # BPL
    code.emit(0xA9, 0x00, 0xA2, 0x00)
    code.label("clear_ram")
    for page in range(8):
        code.emit(0x9D, 0x00, page)
    code.emit(0xE8)
    code.relative(0xD0, "clear_ram")
    code.label("wait_vblank_2")
    code.emit(0x2C, 0x02, 0x20)
    code.relative(0x10, "wait_vblank_2")

    code.emit(0xA9, 0x3F, 0x8D, 0x06, 0x20, 0xA9, 0x00, 0x8D, 0x06, 0x20)
    code.emit(0xA2, 0x00)
    code.label("palette_loop")
    code.absolute(0xBD, "palette")  # LDA palette,X
    code.emit(0x8D, 0x07, 0x20, 0xE8, 0xE0, 0x20)
    code.relative(0xD0, "palette_loop")

    code.emit(0xA9, 0x20, 0x8D, 0x06, 0x20, 0xA9, 0x00, 0x8D, 0x06, 0x20)
    code.emit(0xA9, 0x00, 0xA2, 0x04, 0xA0, 0x00)
    code.label("nametable_page")
    code.emit(0x8D, 0x07, 0x20, 0x88)
    code.relative(0xD0, "nametable_page")
    code.emit(0xCA)
    code.relative(0xD0, "nametable_page")
    code.emit(0xA9, 0x80, 0x8D, 0x00, 0x20, 0xA9, 0x1E, 0x8D, 0x01, 0x20)
    code.label("main")
    code.absolute(0x4C, "main")

    code.label("nmi")
    code.emit(0x48, 0x8A, 0x48, 0x98, 0x48)  # Save A/X/Y
    code.emit(0xE6, 0x00)  # frame counter
    code.emit(0xA9, 0x01, 0x8D, 0x16, 0x40, 0xA9, 0x00, 0x8D, 0x16, 0x40)
    code.emit(0xA2, 0x08, 0xA9, 0x00, 0x85, 0x01, 0x85, 0x02)
    code.label("read_controls")
    code.emit(0xAD, 0x16, 0x40, 0x4A, 0x26, 0x01)
    code.emit(0xAD, 0x17, 0x40, 0x4A, 0x26, 0x02)
    code.emit(0xCA)
    code.relative(0xD0, "read_controls")
    code.emit(0xA5, 0x01)
    code.relative(0xF0, "p1_idle")
    code.emit(0xE6, 0x03)
    code.label("p1_idle")
    code.emit(0xA5, 0x02)
    code.relative(0xF0, "p2_idle")
    code.emit(0xE6, 0x04)
    code.label("p2_idle")

    for address, counter in ((0x2042, 0x03), (0x2052, 0x04), (0x2062, 0x00)):
        code.emit(0xA9, address >> 8, 0x8D, 0x06, 0x20)
        code.emit(0xA9, address & 0xFF, 0x8D, 0x06, 0x20)
        code.emit(0xA5, counter, 0x29, 0x03, 0x18, 0x69, 0x01, 0x8D, 0x07, 0x20)
    code.emit(0xA9, 0x00, 0x8D, 0x05, 0x20, 0x8D, 0x05, 0x20)
    code.emit(0x68, 0xA8, 0x68, 0xAA, 0x68, 0x40)  # Restore Y/X/A; RTI

    code.label("irq")
    code.emit(0x40)
    code.label("palette")
    code.emit(0x0F, 0x30, 0x21, 0x11, 0x0F, 0x16, 0x27, 0x18)
    code.emit(*([0x0F, 0x06, 0x17, 0x28] * 6))

    machine_code, labels = code.build()
    prg = bytearray(0x4000)
    prg[: len(machine_code)] = machine_code
    prg[0x3F00 : 0x3F00 + len(marker)] = marker
    prg[0x3FFA:0x4000] = (
        labels["nmi"].to_bytes(2, "little")
        + labels["reset"].to_bytes(2, "little")
        + labels["irq"].to_bytes(2, "little")
    )
    return bytes(prg)


def build_rom(marker: bytes) -> bytes:
    header = b"NES\x1a" + bytes((1, 1, 0, 0)) + bytes(8)
    chr_rom = bytearray(0x2000)
    patterns = (
        bytes((0xFF,) * 8 + (0x00,) * 8),
        bytes((0x00,) * 8 + (0xFF,) * 8),
        bytes((0xAA, 0x55) * 4 + (0x00,) * 8),
        bytes((0x18, 0x3C, 0x7E, 0xFF, 0xFF, 0x7E, 0x3C, 0x18) + (0x00,) * 8),
    )
    for index, pattern in enumerate(patterns, start=1):
        chr_rom[index * 16 : (index + 1) * 16] = pattern
    return header + build_program(marker) + bytes(chr_rom)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    contents = {name: build_rom(marker) for name, marker in OUTPUTS.items()}
    if args.check:
        for name, content in contents.items():
            destination = OUTPUT_ROOT / name
            if not destination.is_file() or destination.read_bytes() != content:
                raise SystemExit(f"public NES fixture {name} drifted; run build.py")
        return
    with tempfile.TemporaryDirectory(prefix="retrom-nes-smoke-", dir=OUTPUT_ROOT) as temporary:
        for name, content in contents.items():
            generated = Path(temporary) / name
            generated.write_bytes(content)
            shutil.move(generated, OUTPUT_ROOT / name)


if __name__ == "__main__":
    main()
