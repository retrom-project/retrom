#!/usr/bin/env python3
"""Build Retrom's original DOS cache/input/checkpoint fixture without an assembler."""
import argparse
import io
import struct
import zipfile
from pathlib import Path


def program():
    code, labels, fixups = bytearray(), {}, []

    def emit(hex_bytes):
        code.extend(bytes.fromhex(hex_bytes))

    def address(prefix, label):
        emit(prefix)
        fixups.append((len(code), label, 2))
        emit("00 00")

    def branch(opcode, label):
        emit(opcode)
        fixups.append((len(code), label, 1))
        emit("00")

    emit("b8 13 00 cd 10 b8 00 a0 8e c0 fc b0 01")  # mode 13h; ES=VRAM; blue
    labels["draw"] = len(code)
    emit("31 ff b9 00 fa f3 aa")  # fill 64000 pixels with AL
    emit("31 c0 cd 16")  # wait for a real keyboard event
    address("ba", "name")
    emit("b8 00 3d cd 21")  # open LATER.DAT read-only
    branch("72", "error")
    emit("89 c3 b8 00 42 b9 40 00 31 d2 cd 21")  # seek to 4 MiB
    branch("72", "error")
    address("ba", "buffer")
    emit("b9 01 00 b4 3f cd 21")  # read exactly one byte
    branch("72", "error")
    emit("b4 3e cd 21")  # close
    address("80 3e", "buffer")
    emit("42")  # expected original fixture data
    branch("75", "error")
    address("fe 06", "counter")
    address("a0", "counter")
    emit("24 0f")  # next color proves input and preserved state
    branch("eb", "draw")
    labels["error"] = len(code)
    emit("b0 0c")  # bright red on file failure
    branch("eb", "draw")
    for name, value in (("name", b"LATER.DAT\0"), ("buffer", b"\0"), ("counter", b"\1")):
        labels[name] = len(code)
        code.extend(value)
    for offset, label, width in fixups:
        value = labels[label] + 0x100 if width == 2 else labels[label] - offset - 1
        code[offset:offset + width] = struct.pack("<H" if width == 2 else "b", value)
    return bytes(code)


def archive():
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_STORED) as target:
        for name, data in (("CACHE.COM", program()), ("LATER.DAT", b"B" * (8 * 1024 * 1024))):
            info = zipfile.ZipInfo(name, (2026, 1, 1, 0, 0, 0))
            info.create_system = 3
            info.external_attr = 0o100644 << 16
            target.writestr(info, data)
    return output.getvalue()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    arguments = parser.parse_args()
    path = Path(__file__).with_name("dos-cache.zip")
    if arguments.check:
        if path.read_bytes() != archive():
            raise SystemExit("DOS cache fixture drifted; run build.py")
    else:
        path.write_bytes(archive())
