# Retrom public NES netplay smoke ROM

`nes-smoke.nes` is a deterministic, project-owned iNES 1.0 ROM generated entirely by `build.py` and licensed under the adjacent MIT license. It is an NROM/mapper 0 image with one 16 KiB PRG bank and one 8 KiB CHR bank; it contains no third-party game or firmware bytes.

The program initializes all used RAM and PPU state, increments a frame counter from NMI, strobes `$4016`, reads eight P1 bits from `$4016` and eight P2 bits from `$4017`, and maintains independent counters at zero-page `$03` and `$04`. Those counters select tiles in separate nametable positions, so each player's input changes both visible output and FCEUmm savestate/checkpoint bytes. The program does not read clocks, random sources, persistent storage, or uninitialized RAM.

Run `python3 build.py` to regenerate the ROM or `python3 build.py --check` to compare the checked-in bytes. The product consumer is the FCEUmm dual-browser netplay acceptance path; NES/FCEUmm does not require a BIOS, and its Launch config must therefore leave `biosUrl` empty.
