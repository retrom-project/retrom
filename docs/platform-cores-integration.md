# Platform core integration (PFB `platform-cores`)

Game & Watch is excluded by request. This PFB contains ten forked cores for eleven
platform entries; Atari 8-bit and XEGS share the Atari800 implementation. Core
sources live under `retrom-project`, Provider adapters and archives under
`retrom-runtime`, and platform bindings in Retrom's PFB catalog. The root
workspace bootstrap and baseline checkouts are unchanged.

| Platform | Fork / implementation | Provider target | Selection and browser result |
| --- | --- | --- | --- |
| Arduboy | `retrom-project/Ardens` | EmulatorJS `ardens` | Native state support; Arduboy Golf uploads, previews, launches, accepts input, saves, and restores in a new Launch. |
| Atari 8-bit | `retrom-project/libretro-atari800` | EmulatorJS `atari800` | Maintained Atari800 core; ATR boot, input, save, and new Launch restore passed. |
| XEGS | `retrom-project/libretro-atari800` | EmulatorJS `atari800-xegs` | Reuses Atari800 with a separate XEGS model and bundled AltirraOS; cartridge boot, input, save, and restore passed. |
| Atari ST | `retrom-project/hatariB` | EmulatorJS `hatarib` | Hatari-based core with native state support and built-in EmuTOS; `1943` boot, input, save, and restore passed. |
| BBC Micro | `retrom-project/jsbeeb` | retrom-runtime `bbc-jsbeeb` | Mature browser emulator; `Welcome.ssd` with three supplied ROMs boots, accepts input, saves, and restores. Fork archive avoids Google Analytics and remote font requests. |
| Channel F | `retrom-project/FreeChaF` | EmulatorJS `freechaf` | Libretro state API; `lights.bin` with two supplied BIOS files boots, accepts input, saves, and restores. |
| Mega Duck | `retrom-project/SameBoy` / SameDuck | EmulatorJS `sameduck` | SameBoy lineage with Mega Duck support and native state API; `MaxPirate.bin` boot, input, save, and restore passed. |
| SAM Coupé | `retrom-project/SamCoupeWeb` | retrom-runtime `samcoupe` | Browser-capable SimCoupe lineage; the alternate libretro port documents incomplete input and sound. `SafariSam.dsk` and a SAMDOS disk boot and accept input. A BASIC file write, exit-time disk checkpoint, and new Launch disk restore passed. This is a game-data save, not an instant state snapshot. |
| Supervision | `retrom-project/potator` | EmulatorJS `potator` | Libretro state API; `Balloon Fight` boot, input, save, and restore passed. |
| Thomson | `retrom-project/theodore` | EmulatorJS `theodore` | Thomson family support and native state API; MO5 tape and `Bomb Jacques` gameplay respond to input, save, and restore. |
| Sega Model 3 | `retrom-project/Libretro-Supermodel` | EmulatorJS `supermodel` | Supermodel lineage with native serialization; `daytona2.zip` reaches course selection, responds to steering, saves, and restores in a new Launch. Sustained racing performance is not characterized. |

## Published source and product validation

Each fork's `retrom/<baseline>` maintenance branch owns its build and fixed
`retrom-core-<baseline>-r1` Release. The runtime's `provider-sources.json` and
EmulatorJS source catalog pin the release commit, asset names, sizes, hashes,
and adapter ABI. Retrom pins the resulting runtime Provider release in
`data/runtime-providers/release.json`. Neither runtime nor Retrom compiles a
core during Provider installation.

The `platform-cores` PFB exercised all eleven entries through authenticated
import review, preview, product Launch, direction and confirmation input,
checkpoint, a distinct Launch restore, and input after restore. Browser input
tests checked rendered responses as well as delivery events. In particular,
the BBC Micro BASIC test visibly moved `POSITION` from 10 to 16 and back to 10
with keyboard and gamepad input; Model 3 course selection responded to steering;
and the Xbox 360 gamepad started and moved Mega Duck gameplay while another
connected pad occupied browser index 0. Product receipts and screenshots are
kept in the PFB's ignored `.pfb/evidence/` directory, separate from source
control. Game and BIOS bytes remain under `/data/game`, outside Git.

SAM Coupé exposes a native game-data save rather than an instant state. A game
must write its disk before **存档并退出** can export the modified disk; a new
Launch imports that disk before boot, and an in-game load resumes play. The
normal instant-save action remains unavailable for this Target. The adapter
compares durable disk bytes after SimCoupe flushes them and preserves the WebGL
canvas for Provider screenshots. On Provider `v0.48.2`, a new Launch loaded the
saved disk with `LOAD CHR$ 82`; `LIST` displayed `10 PRINT 1`, and `RUN` printed
`1`.
