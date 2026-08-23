# Retrom SNES netplay smoke ROM

`snes-smoke.sfc` is a project-owned, MIT-licensed 32 KiB SNES LoROM generated
deterministically by `build.py`. It contains no Nintendo logo, commercial game,
SDK output, BIOS, music, image, or third-party program bytes.

The ROM initializes BG1, VRAM, CGRAM, automatic joypad polling, and all fixture
WRAM explicitly. A deterministic busy loop changes both CGRAM bytes in the top band, while the
left and right halves change independently and visibly when P1 or P2 input is pressed. WRAM `$0000`,
`$0001`, and `$0002` contain the P1, P2, and frame counters captured by native
SNES9x state serialization. Audio remains muted and the fixture uses no SRAM,
RTC, random input, network, or persistent files.

Generate with `python3 build.py`; verify committed bytes with
`python3 build.py --check`. The output is consumed only by Retrom's real import,
Launch, content endpoint, Player, and dual-browser SNES9x acceptance path.
