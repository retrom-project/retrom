# Retrom public Arcade smoke fixture

This directory contains a deterministic, project-owned MAME 2003 test program. It is built entirely from the adjacent Python source and contains no bytes from Pac-Man, PuckMan, their BIOSes, or any other third-party game.

The fixture deliberately targets the public memory map and file layout of the MAME 2003 `pacman` driver. The generated Z80 program fills video and color RAM with animated test tiles. The generated resources are split across:

- `pacman.zip`: the four test program ROMs;
- `puckman.zip`: generated character, sprite, palette, lookup, and silent sound data used as a Parent archive;
- `retrombios.zip`: a generated BIOS-role archive used to verify Retrom and EmulatorJS dependency delivery; the MAME driver does not execute this test BIOS;
- `mame2003-smoke.xml`: a small DAT that locks the generated names, sizes, CRC32 values, SHA-1 values, Parent relation, and BIOS/base relation.

Run `python3 build.py` to regenerate the files or `python3 build.py --check` to verify the checked-in bytes. The fixture is licensed under the adjacent MIT license.
