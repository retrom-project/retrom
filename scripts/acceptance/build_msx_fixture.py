"""Generate Retrom's owned MSX cartridge: arrows move, Space confirms, Esc resets.

The ROM contains only this project's Z80 program and padding; BIOS is supplied by
an emulator independently. No third-party ROM bytes are embedded here.
"""
from pathlib import Path
import sys

rom = bytearray(b'AB\x10\x40' + bytes(12))
labels = {}
patches = []
def emit(*values):
    rom.extend(values)
def label(name):
    labels[name] = 0x4000 + len(rom)
def jump(op, name):
    emit(op, 0, 0)
    patches.append((len(rom) - 2, name))
def call(address):
    emit(0xCD, address & 255, address >> 8)
def load_a(address):
    emit(0x3A, address & 255, address >> 8)
def store_a(address):
    emit(0x32, address & 255, address >> 8)
# Standard BIOS entrypoints: CHGMOD, CHGCLR, CLS, POSIT, CHPUT, SNSMAT.
emit(0x3E, 1); call(0x5F)
emit(0x3E, 15); store_a(0xF3E9)
emit(0x3E, 1); store_a(0xF3EA); store_a(0xF3EB); call(0x62)
emit(0x3E, 10); store_a(0xC000)
emit(0x3E, 64); store_a(0xC001)
label('draw')
# MSX2 Technical Handbook, Appendix 1: CLS requires Z set.
# https://konamiman.github.io/MSX2-Technical-Handbook/md/Appendix1.html
emit(0xAF); call(0xC3)
load_a(0xC000); emit(0x67, 0x2E, 10); call(0xC6)
load_a(0xC001); call(0xA2)
label('wait')
emit(0xFB, 0x76, 0x3E, 8); call(0x141); emit(0x47)
emit(0xCB, 0x78); jump(0xCA, 'right')
emit(0xCB, 0x60); jump(0xCA, 'left')
emit(0xCB, 0x40); jump(0xCA, 'confirm')
emit(0x3E, 7); call(0x141); emit(0xCB, 0x57); jump(0xCA, 'cancel')
jump(0xC3, 'wait')
label('right'); load_a(0xC000); emit(0xFE, 28); jump(0xD2, 'wait'); emit(0x3C); jump(0xC3, 'move')
label('left'); load_a(0xC000); emit(0xFE, 2); jump(0xDA, 'wait'); emit(0x3D)
label('move'); store_a(0xC000); jump(0xC3, 'draw')
label('confirm'); emit(0x3E, 35); store_a(0xC001); jump(0xC3, 'draw')
label('cancel'); emit(0x3E, 10); store_a(0xC000); emit(0x3E, 64); store_a(0xC001); jump(0xC3, 'draw')
for offset, name in patches:
    address = labels[name]
    rom[offset:offset + 2] = address.to_bytes(2, 'little')
rom.extend(bytes(16384 - len(rom)))
out = Path(sys.argv[1]); out.parent.mkdir(parents=True, exist_ok=True)
if out.exists() and out.read_bytes() != rom:
    raise ValueError('Refusing to replace a different cartridge')
out.write_bytes(rom)
print(f'Owned MSX cartridge: {len(rom)} bytes')
