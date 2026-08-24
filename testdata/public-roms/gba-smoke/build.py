#!/usr/bin/env python3
"""Build Retrom's project-owned, third-party-free GBA smoke-test ROM."""

from __future__ import annotations

import argparse
import base64
import binascii
import hashlib
import struct
import zlib
from dataclasses import dataclass
from pathlib import Path


OUTPUTS = {
    Path(__file__).with_name("gba-smoke.gba"): (b"RETROM SMOKE", b"RTSM"),
    Path(__file__).with_name("pegasus-smoke.gba"): (b"RETROM PEGAS", b"RTPG"),
    Path(__file__).with_name("emulationstation-smoke.gba"): (
        b"RETROM ESTAT",
        b"RTES",
    ),
}
GAMELIST_OUTPUT = Path(__file__).with_name("gamelist.xml")
ORDINARY_COVER_OUTPUT = Path(__file__).with_name("gba-smoke-cover.png")
ORDINARY_VIDEO_OUTPUT = Path(__file__).with_name("gba-smoke-video.webm")
EMULATIONSTATION_COVER_OUTPUT = Path(__file__).with_name(
    "emulationstation-smoke-cover.png"
)
EMULATIONSTATION_VIDEO_OUTPUT = Path(__file__).with_name(
    "emulationstation-smoke-video.webm"
)
GAMELIST = b"""<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <path>./emulationstation-smoke.gba</path>
    <image>./emulationstation-smoke-cover.png</image>
    <video>./emulationstation-smoke-video.webm</video>
    <name>EmulationStation GBA Smoke</name>
    <desc>Retrom project-owned EmulationStation product E2E fixture</desc>
    <developer>Retrom</developer>
    <publisher>Retrom</publisher>
    <genre>Smoke Test</genre>
    <players>1</players>
  </game>
</gameList>
"""
VIDEO_WEBM_BASE64 = """
GkXfo59ChoEBQveBAULygQRC84EIQoKEd2VibUKHgQJChYECGFOAZwH/////////EU2bdKtNu4tTq4QVSalmU6yBoU27i1OrhBZU
rmtTrIG7TbuMU6uEElTDZ1OsggEI7AEAAAAAAABoAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAVSalmlSrXsYMPQkBNgIRM
YXZmV0GETGF2ZhZUrmvIrgEAAAAAAAA/14EBc8WIAAAAAAAAAAGcgQAitZyDdW5kiIEAhoVWX1ZQOIOBASPjg4QL68IA4JCwgaC6
gXCagQJVsIRVuYEBElTDZ6xzc6ljwItjxYgAAAAAAAAAAWfImEWjh0VOQ09ERVJEh4tMYXZjIGxpYnZweB9DtnVBkOeBAKNBDIEA
AIBwDwCdASqgAHAAAEcIhYWIhYSIAgICvRaF+A/iry7di8gP4A6ID+APYIFXwgOp/9H/5wG8zfwHqARB7Iokx9G7cKVFgqCy+LY0
Ih4jCAKHwE+QKT8fmraOd0N3bQ/pWGmRtD+lYaZGzRapElruO7Kk5ahVH11wJxmhFr4+wFD4Ce+I+yPw/v3ZED/9aHqP94C/eAv/
qjP/ExIdA8XNPb/LdivMikGj+iUKnJY29n746Ojy5JTeftyQ4aWqZoFr0L//55mNJ5m3ZH/7VyCLBdwkDFQUy4ABrXT4BQ0qVAD5
L0EYTfPJO5VNPIXaG9uWzHBODXuIoAZdUdhOKDw7XvWlggrfOY2TBwCjroEAyABxAgABEBAAGAAZ8C/0ADsV4wkAm0JgAa540xc7
AMQIAA3JMAARJCSIAACjmIEBkAARAgABEBAAGAAYWC/0AAiAgQqAAKOYgQJYABECAAEQEAAYABhYL/QACICBCoAAo5iBAyAAEQIA
ARAQABgAGFgv9AAIgIEKgAA=
"""
ROM_SIZE = 1024
CODE_OFFSET = 0xC0
COVER_WIDTH = 70
COVER_HEIGHT = 98

COND_EQ = 0x0
COND_NE = 0x1
COND_CS = 0x2
COND_CC = 0x3
COND_AL = 0xE


def rotate_right(value: int, amount: int) -> int:
    amount %= 32
    return ((value >> amount) | (value << (32 - amount))) & 0xFFFFFFFF


def encode_immediate(value: int) -> tuple[int, int]:
    value &= 0xFFFFFFFF
    for rotation in range(16):
        for immediate in range(256):
            if rotate_right(immediate, rotation * 2) == value:
                return rotation, immediate
    raise ValueError(f"ARM immediate is not encodable: 0x{value:08x}")


def data_processing_immediate(
    opcode: int,
    destination: int,
    value: int,
    *,
    source: int = 0,
    set_flags: bool = False,
    condition: int = COND_AL,
) -> int:
    rotation, immediate = encode_immediate(value)
    return (
        (condition << 28)
        | (1 << 25)
        | (opcode << 21)
        | (int(set_flags) << 20)
        | (source << 16)
        | (destination << 12)
        | (rotation << 8)
        | immediate
    )


@dataclass(frozen=True)
class BranchFixup:
    word_index: int
    label: str
    condition: int


@dataclass(frozen=True)
class LiteralFixup:
    word_index: int
    destination: int
    value: int
    condition: int


class ARMProgram:
    def __init__(self) -> None:
        self.words: list[int] = []
        self.labels: dict[str, int] = {}
        self.branches: list[BranchFixup] = []
        self.literals: list[LiteralFixup] = []

    def emit(self, word: int) -> None:
        self.words.append(word & 0xFFFFFFFF)

    def label(self, name: str) -> None:
        if name in self.labels:
            raise ValueError(f"duplicate label: {name}")
        self.labels[name] = len(self.words)

    def branch(self, label: str, *, condition: int = COND_AL) -> None:
        self.branches.append(BranchFixup(len(self.words), label, condition))
        self.emit(0)

    def load_literal(
        self,
        destination: int,
        value: int,
        *,
        condition: int = COND_AL,
    ) -> None:
        self.literals.append(
            LiteralFixup(len(self.words), destination, value & 0xFFFFFFFF, condition)
        )
        self.emit(0)

    def move_immediate(self, destination: int, value: int) -> None:
        self.emit(data_processing_immediate(13, destination, value))

    def add_immediate(self, destination: int, source: int, value: int) -> None:
        self.emit(
            data_processing_immediate(4, destination, value, source=source)
        )

    def subtract_immediate_with_flags(
        self, destination: int, source: int, value: int
    ) -> None:
        self.emit(
            data_processing_immediate(
                2,
                destination,
                value,
                source=source,
                set_flags=True,
            )
        )

    def compare_immediate(self, source: int, value: int) -> None:
        self.emit(
            data_processing_immediate(
                10,
                0,
                value,
                source=source,
                set_flags=True,
            )
        )

    def test_immediate(self, source: int, value: int) -> None:
        self.emit(
            data_processing_immediate(
                8,
                0,
                value,
                source=source,
                set_flags=True,
            )
        )

    def store_word_postincrement(
        self, source: int, base: int, offset: int = 4
    ) -> None:
        if not 0 <= offset <= 0xFFF:
            raise ValueError("word store offset is out of range")
        self.emit(
            (COND_AL << 28)
            | 0x04800000
            | (base << 16)
            | (source << 12)
            | offset
        )

    def transfer_halfword(
        self,
        *,
        load: bool,
        register: int,
        base: int,
        offset: int = 0,
    ) -> None:
        if not 0 <= offset <= 0xFF:
            raise ValueError("halfword transfer offset is out of range")
        self.emit(
            (COND_AL << 28)
            | 0x01C000B0
            | (int(load) << 20)
            | (base << 16)
            | (register << 12)
            | ((offset & 0xF0) << 4)
            | (offset & 0x0F)
        )

    def build(self) -> bytes:
        words = list(self.words)
        literal_indices: dict[int, int] = {}
        for fixup in self.literals:
            if fixup.value not in literal_indices:
                literal_indices[fixup.value] = len(words)
                words.append(fixup.value)

        for fixup in self.branches:
            if fixup.label not in self.labels:
                raise ValueError(f"unknown branch label: {fixup.label}")
            delta = self.labels[fixup.label] - (fixup.word_index + 2)
            if not -(1 << 23) <= delta < (1 << 23):
                raise ValueError(f"branch target is out of range: {fixup.label}")
            words[fixup.word_index] = (
                (fixup.condition << 28) | 0x0A000000 | (delta & 0x00FFFFFF)
            )

        for fixup in self.literals:
            literal_index = literal_indices[fixup.value]
            offset = (literal_index - (fixup.word_index + 2)) * 4
            if not 0 <= offset <= 0xFFF:
                raise ValueError("literal pool is out of range")
            words[fixup.word_index] = (
                (fixup.condition << 28)
                | 0x059F0000
                | (fixup.destination << 12)
                | offset
            )

        return b"".join(struct.pack("<I", word) for word in words)


def build_program() -> bytes:
    io_base = 0x04000000
    video_ram = 0x06000000
    key_input = 0x04000130
    words_per_half_screen = 9_600
    words_per_animated_band = 1_200

    program = ARMProgram()
    program.load_literal(0, io_base)
    program.load_literal(1, video_ram)
    program.load_literal(2, 0x0403)
    program.transfer_halfword(load=False, register=2, base=0)

    # Mode 3 is a 240x160 15-bit framebuffer. The upper half starts black and
    # the lower half white so screenshots have deterministic contrast.
    program.load_literal(8, video_ram)
    program.load_literal(9, words_per_half_screen)
    program.move_immediate(6, 0)
    program.label("fill_black")
    program.store_word_postincrement(6, 8)
    program.subtract_immediate_with_flags(9, 9, 1)
    program.branch("fill_black", condition=COND_NE)

    program.load_literal(9, words_per_half_screen)
    program.load_literal(6, 0x7FFF7FFF)
    program.label("fill_white")
    program.store_word_postincrement(6, 8)
    program.subtract_immediate_with_flags(9, 9, 1)
    program.branch("fill_white", condition=COND_NE)

    program.move_immediate(4, 0)
    program.load_literal(7, key_input)
    program.label("wait_visible")
    program.transfer_halfword(load=True, register=5, base=0, offset=6)
    program.compare_immediate(5, 160)
    program.branch("wait_visible", condition=COND_CS)
    program.label("wait_vblank")
    program.transfer_halfword(load=True, register=5, base=0, offset=6)
    program.compare_immediate(5, 160)
    program.branch("wait_vblank", condition=COND_CC)

    program.add_immediate(4, 4, 1)
    program.test_immediate(4, 1)
    program.load_literal(6, 0x001F001F, condition=COND_EQ)
    program.load_literal(6, 0x03E003E0, condition=COND_NE)
    program.transfer_halfword(load=True, register=10, base=7)
    program.test_immediate(10, 1)
    program.load_literal(6, 0x7C007C00, condition=COND_EQ)

    # Animate the first ten scanlines red/green on alternating frames. Holding
    # the GBA A button changes the band to blue, providing an input sentinel.
    program.load_literal(8, video_ram)
    program.load_literal(9, words_per_animated_band)
    program.label("paint_band")
    program.store_word_postincrement(6, 8)
    program.subtract_immediate_with_flags(9, 9, 1)
    program.branch("paint_band", condition=COND_NE)
    program.branch("wait_visible")
    return program.build()


def build_rom(title: bytes, product_code: bytes) -> bytes:
    if len(title) != 12 or len(product_code) != 4:
        raise ValueError("GBA fixture title and product code must fill the header")
    header = bytearray(CODE_OFFSET)
    branch_delta = (CODE_OFFSET - 8) // 4
    struct.pack_into("<I", header, 0, 0xEA000000 | branch_delta)
    header[0xA0:0xAC] = title
    header[0xAC:0xB0] = product_code
    header[0xB0:0xB2] = b"00"
    header[0xB2] = 0x96
    header[0xBC] = 0
    header[0xBD] = (-(0x19 + sum(header[0xA0:0xBD]))) & 0xFF

    image = bytes(header) + build_program()
    if len(image) > ROM_SIZE:
        raise ValueError(f"ROM exceeds {ROM_SIZE} bytes: {len(image)}")
    return image.ljust(ROM_SIZE, b"\xFF")


def png_chunk(kind: bytes, payload: bytes) -> bytes:
    checksum = binascii.crc32(kind + payload) & 0xFFFFFFFF
    return struct.pack(">I", len(payload)) + kind + payload + struct.pack(">I", checksum)


def stored_zlib(payload: bytes) -> bytes:
    result = bytearray(b"\x78\x01")
    for offset in range(0, len(payload), 0xFFFF):
        block = payload[offset : offset + 0xFFFF]
        final = offset + len(block) == len(payload)
        result.append(1 if final else 0)
        result.extend(struct.pack("<HH", len(block), 0xFFFF ^ len(block)))
        result.extend(block)
    result.extend(struct.pack(">I", zlib.adler32(payload) & 0xFFFFFFFF))
    return bytes(result)


def build_cover(
    interior: tuple[int, int, int],
    checker_light: tuple[int, int, int],
    checker_dark: tuple[int, int, int],
) -> bytes:
    rows = bytearray()
    for y_coordinate in range(COVER_HEIGHT):
        rows.append(0)
        for x_coordinate in range(COVER_WIDTH):
            if 8 <= x_coordinate < 62 and 10 <= y_coordinate < 88:
                color = interior
            elif (x_coordinate // 7 + y_coordinate // 7) % 2 == 0:
                color = checker_light
            else:
                color = checker_dark
            rows.extend(color)
    header = struct.pack(">IIBBBBB", COVER_WIDTH, COVER_HEIGHT, 8, 2, 0, 0, 0)
    return (
        b"\x89PNG\r\n\x1a\n"
        + png_chunk(b"IHDR", header)
        + png_chunk(b"IDAT", stored_zlib(bytes(rows)))
        + png_chunk(b"IEND", b"")
    )


def build_video(*, distinct_ordinary_identity: bool) -> bytes:
    payload = base64.b64decode("".join(VIDEO_WEBM_BASE64.split()), validate=True)
    if distinct_ordinary_identity:
        # The Segment has an unknown length, so a trailing zero-byte EBML Void
        # is a valid, playback-neutral element and gives the ordinary upload
        # fixture its own deterministic CAS identity.
        return payload + b"\xec\x81\x00"
    return payload


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--check",
        action="store_true",
        help="fail unless the checked-in ROM is byte-identical to generated output",
    )
    arguments = parser.parse_args()

    images = {
        output: build_rom(title, product_code)
        for output, (title, product_code) in OUTPUTS.items()
    }
    media = {
        ORDINARY_COVER_OUTPUT: build_cover(
            (244, 186, 66),
            (37, 99, 235),
            (15, 23, 42),
        ),
        ORDINARY_VIDEO_OUTPUT: build_video(distinct_ordinary_identity=True),
        EMULATIONSTATION_COVER_OUTPUT: build_cover(
            (75, 214, 197),
            (91, 70, 216),
            (16, 24, 39),
        ),
        EMULATIONSTATION_VIDEO_OUTPUT: build_video(
            distinct_ordinary_identity=False
        ),
    }
    if arguments.check:
        for output, image in images.items():
            if not output.exists():
                raise SystemExit(f"public GBA smoke ROM is missing: {output}")
            if output.read_bytes() != image:
                raise SystemExit(
                    f"public GBA smoke ROM drifted: {output.name}; run "
                    "python3 testdata/public-roms/gba-smoke/build.py"
                )
            digest = hashlib.sha256(image).hexdigest()
            print(
                f"gba_smoke_check=passed name={output.name} "
                f"size={len(image)} sha256={digest}"
            )
        if not GAMELIST_OUTPUT.exists():
            raise SystemExit(f"public EmulationStation gamelist is missing: {GAMELIST_OUTPUT}")
        if GAMELIST_OUTPUT.read_bytes() != GAMELIST:
            raise SystemExit(
                "public EmulationStation gamelist drifted; run "
                "python3 testdata/public-roms/gba-smoke/build.py"
            )
        print(
            "emulationstation_gamelist_check=passed "
            f"size={len(GAMELIST)} sha256={hashlib.sha256(GAMELIST).hexdigest()}"
        )
        for output, payload in media.items():
            if not output.exists():
                raise SystemExit(f"public EmulationStation media is missing: {output}")
            if output.read_bytes() != payload:
                raise SystemExit(
                    f"public EmulationStation media drifted: {output.name}; run "
                    "python3 testdata/public-roms/gba-smoke/build.py"
                )
            print(
                f"emulationstation_media_check=passed name={output.name} "
                f"size={len(payload)} sha256={hashlib.sha256(payload).hexdigest()}"
            )
        return 0

    for output, image in images.items():
        output.write_bytes(image)
        digest = hashlib.sha256(image).hexdigest()
        print(
            f"gba_smoke_generated={output} size={len(image)} sha256={digest}"
        )
    GAMELIST_OUTPUT.write_bytes(GAMELIST)
    print(
        f"emulationstation_gamelist_generated={GAMELIST_OUTPUT} "
        f"size={len(GAMELIST)} sha256={hashlib.sha256(GAMELIST).hexdigest()}"
    )
    for output, payload in media.items():
        output.write_bytes(payload)
        print(
            f"emulationstation_media_generated={output} "
            f"size={len(payload)} sha256={hashlib.sha256(payload).hexdigest()}"
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
