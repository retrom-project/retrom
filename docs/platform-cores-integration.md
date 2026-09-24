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
| Thomson | `retrom-project/theodore` | EmulatorJS `theodore` | Thomson family support and native state API; MO5 tape boots, accepts input, saves, and restores. A long tape load and gameplay beyond the boot screen have not been established. |
| Sega Model 3 | `retrom-project/Libretro-Supermodel` | EmulatorJS `supermodel` | Supermodel lineage with native serialization; `daytona2.zip` reaches a rendered boot frame, accepts gamepad events, saves, and restores in a new Launch. Full gameplay and performance need separate visual review. |

## Development candidate

PFB ID: `platform-co-29b3e5616fcb`.
URL: `http://platform-co-29b3e5616fcb.localhost:3000`.
The imported Provider candidate is `0.48.1-dev.15`. The EmulatorJS archive SHA-256
is `59358c452fd50e95c9ebde5a16cec6f660a770ed2b74347f52a1a139839668ee`;
the retrom-runtime archive SHA-256 is
`cd282c7907fc14dec1af5942191a41c6a708a186c99918281227f69542885300`.
`make pfb-verify PFB=platform-cores` passed; its read-only evidence is under
`.pfb/evidence/20260924T045330Z/` in this Retrom worktree. This is a development
candidate, not a published release.

The authenticated product run uploads a ROM, creates an import review, opens
review preview, approves the game, launches it, sends virtual standard gamepad
input, captures a checkpoint, and restores it in a distinct Launch. The JSON
receipts and screenshots are under `.pfb/evidence/platform-core-product/<case>/`.
Use `arduboy-golf`, `atari800`, `xegs`, `atarist-1943`, `bbc`, `channelf`,
`megaduck`, `samcoupe-basic`, `supervision`, `thomson-mo5`, and `model3` for passing cases.
The test runner is `.pfb/evidence/platform-core-product-runner.mjs` and uses the
PFB's `test` account via environment variables. These automated passes prove the
API path and rendered nonblank frames; screenshots still require visual review
before release. Model 3's receipt has no browser errors or failed requests.

SAM Coupé's `SafariSam.dsk` source is the
[author's public download](https://www.martinfitzpatrick.com/safari-sam/). The
SAMDOS test disk comes from the [Outwrite author's page](https://www.intensity.org.uk/samcoupe/download.php);
its test copy changes the `AUTOWRITE!` autorun name to `ZUTOWRITE!` so BASIC is
available after boot. That one-byte derivative and its source metadata live only
in `/data/game/testgame/retrom-runtime/samcoupe/`. The `samcoupe-basic` product
case records a BASIC `SAVE CHR$ 82` write. Its disk fingerprint changed from
`854a23ac` to `98a2599`; the native-save flow staged the content in the browser,
uploaded a 215,221-byte checkpoint on exit, and a new Launch restored the same
`98a2599` fingerprint. SimCoupe clears its dirty flag when the motor stops and
flushes the disk, so the adapter also compares durable disk bytes and reports
their content revision. The normal toolbar's instant-save button remains
disabled because SAM requires an in-game write followed by **存档并退出**. The
earlier `samcoupe` and `samcoupe-outwrite` runs did not write a disk; their
disabled native-save controls were expected.
The SAM canvas now preserves its WebGL frame for Provider screenshots. The
unmodified browser probe at `.pfb/evidence/samcoupe-capture-raw.png` reads BASIC
text directly from the canvas instead of a black frame.

Game and BIOS bytes used in these runs remain under `/data/game`, outside Git.
Formal publication still requires review of the evidence, especially keyboard
computer workflows, Model 3 gameplay, and Thomson tape loading.
