"""Small original 68000/Z80 programs shared by the CPS fixture builders."""

from __future__ import annotations

from typing import Literal


def _word(value: int) -> bytes:
    return value.to_bytes(2, "big")


def _long(value: int) -> bytes:
    return value.to_bytes(4, "big")


class _Assembler:
    """Minimal 68000 emitter for the project-owned CPS smoke program."""

    def __init__(self) -> None:
        self.code = bytearray()
        self.labels: dict[str, int] = {}
        self.fixups: list[tuple[int, str]] = []

    def emit(self, value: str) -> None:
        self.code += bytes.fromhex(value)

    def word(self, value: int) -> None:
        self.code += _word(value & 0xFFFF)

    def long(self, value: int) -> None:
        self.code += _long(value & 0xFFFFFFFF)

    def label(self, name: str) -> None:
        if name in self.labels:
            raise ValueError(f"duplicate assembler label: {name}")
        self.labels[name] = len(self.code)

    def branch_word(self, opcode: str, label: str) -> None:
        self.emit(opcode)
        self.fixups.append((len(self.code), label))
        self.word(0)

    def finish(self) -> bytes:
        for offset, label in self.fixups:
            target = self.labels.get(label)
            if target is None:
                raise ValueError(f"missing assembler label: {label}")
            # 68000 word branches are relative to the extension word address.
            displacement = target - offset
            if not -0x8000 <= displacement <= 0x7FFF:
                raise ValueError(f"branch displacement is out of range: {label}")
            self.code[offset : offset + 2] = _word(displacement & 0xFFFF)
        return bytes(self.code)


def _move_word_immediate_absolute(assembler: _Assembler, value: int, address: int) -> None:
    assembler.emit("33fc")
    assembler.word(value)
    assembler.long(address)


def _move_long_immediate_absolute(assembler: _Assembler, value: int, address: int) -> None:
    assembler.emit("23fc")
    assembler.long(value)
    assembler.long(address)


def _move_byte_immediate_absolute(assembler: _Assembler, value: int, address: int) -> None:
    assembler.emit("13fc")
    assembler.word(value & 0xFF)
    assembler.long(address)


def _write_cps_register_word(assembler: _Assembler, offset: int, value: int) -> None:
    """Write a big-endian CPS register through the byte-oriented I/O handler."""
    _move_byte_immediate_absolute(assembler, value >> 8, 0x800100 + offset)
    _move_byte_immediate_absolute(assembler, value, 0x800101 + offset)


def _initialize_cps1_objects(assembler: _Assembler) -> None:
    _write_cps_register_word(assembler, 0x00, 0x9000)
    objects = (
        (0x0040, 0x0010, 0x0000, 0xDF01),
        (0x0140, 0x0010, 0x0100, 0xD701),
        (0x0000, 0x0000, 0x0000, 0xFF00),
    )
    for index, entry in enumerate(objects):
        for word_index, value in enumerate(entry):
            _move_word_immediate_absolute(
                assembler,
                value,
                0x900000 + index * 8 + word_index * 2,
            )


def _initialize_cps2_objects(assembler: _Assembler) -> None:
    objects = (
        (0x0000, 0x0000, 0x0000, 0xDF01),
        (0x0100, 0x0000, 0x0100, 0xD701),
        (0x0000, 0x8000, 0x0000, 0x0000),
    )
    for index, entry in enumerate(objects):
        for word_index, value in enumerate(entry):
            _move_word_immediate_absolute(
                assembler,
                value,
                0x700000 + index * 8 + word_index * 2,
            )


def build_68000_program(image_size: int, hardware: Literal["cps1", "cps2"]) -> bytes:
    """Build a deterministic CPS program with visible P1/P2 input feedback."""
    image = bytearray(image_size)
    image[0:4] = _long(0x00FFFFF0)
    image[4:8] = _long(0x00000100)
    assembler = _Assembler()
    assembler.emit("46fc2700")  # move.w #$2700,sr
    work_ram = 0xFF0000 if hardware == "cps1" else 0x660000
    assembler.emit("41f9")  # lea work_ram,a0
    assembler.long(work_ram)
    assembler.emit("429042a8000442a80008")  # clear counters
    _move_long_immediate_absolute(assembler, 0x5254524D, work_ram + 0x10)  # "RTRM"

    palette_control = 0x72 if hardware == "cps1" else 0x70
    _write_cps_register_word(assembler, palette_control, 0x003F)
    _write_cps_register_word(assembler, 0x0A, 0x9200)
    if hardware == "cps1":
        _initialize_cps1_objects(assembler)
    else:
        _initialize_cps2_objects(assembler)

    assembler.emit("343cffff")  # move.w #$ffff,d2 (bright white)
    assembler.branch_word("6100", "set_palette")

    assembler.label("main_loop")
    assembler.emit("52a80008")  # addq.l #1,8(a0), progress counter
    assembler.emit("10390080000146000200007f")
    assembler.branch_word("6700", "p1_done")
    assembler.emit("5290")  # addq.l #1,(a0)
    assembler.emit("343cff00")  # move.w #$ff00,d2 (bright red)
    assembler.branch_word("6100", "set_palette")
    assembler.label("p1_done")
    assembler.emit("10390080000046000200007f")
    assembler.branch_word("6700", "p2_done")
    assembler.emit("52a80004")  # addq.l #1,4(a0)
    assembler.emit("343cf0f0")  # move.w #$f0f0,d2 (bright green)
    assembler.branch_word("6100", "set_palette")
    assembler.label("p2_done")
    assembler.emit("323cffff")  # move.w #$ffff,d1
    assembler.label("delay")
    assembler.branch_word("51c9", "delay")  # dbra d1,delay
    assembler.branch_word("6000", "main_loop")

    assembler.label("set_palette")
    assembler.emit("43f900920000")  # lea $920000,a1
    assembler.emit("363c0bff")  # move.w #$0bff,d3 (3072 palette words)
    assembler.label("palette_loop")
    assembler.emit("32c2")  # move.w d2,(a1)+
    assembler.branch_word("51cb", "palette_loop")
    # Rewriting the palette base copies palette RAM into the renderer.
    _write_cps_register_word(assembler, 0x0A, 0x9200)
    assembler.emit("4e75")  # rts

    code = assembler.finish()
    if 0x100 + len(code) > 0x400:
        raise ValueError("CPS smoke program overlaps its provenance marker")
    image[0x100 : 0x100 + len(code)] = code
    marker = b"RETROM PUBLIC CPS SMOKE - ORIGINAL MIT-LICENSED 68000 PROGRAM"
    image[0x400 : 0x400 + len(marker)] = marker
    return bytes(image)


def build_z80_silence(size: int) -> bytes:
    image = bytearray(size)
    image[:3] = bytes((0xF3, 0x18, 0xFD))  # DI; JR to the JR instruction
    marker = b"RETROM PUBLIC CPS SILENT AUDIO CPU"
    image[0x100 : 0x100 + len(marker)] = marker
    return bytes(image)


def split_cps1_byteswapped(program: bytes) -> tuple[bytes, bytes]:
    if len(program) % 2:
        raise ValueError("CPS1 interleaved program must have an even size")
    # The core maps 68000 words in host order: ROM 0 is loaded at odd bus
    # addresses and therefore carries the logical even byte of every word.
    return program[0::2], program[1::2]


def byteswap_68000_words(program: bytes) -> bytes:
    """Convert logical big-endian 68000 bytes to the core's host-word layout."""
    if len(program) % 2:
        raise ValueError("68000 program must have an even size")
    swapped = bytearray(len(program))
    swapped[0::2] = program[1::2]
    swapped[1::2] = program[0::2]
    return bytes(swapped)
