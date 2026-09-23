# Platform core integration (PFB `platform-cores`)

This worktree is an integration candidate. Game & Watch is excluded by request.
Core source, Provider targets, and product bindings are owned by separate repositories;
the PFB catalog at `workspace/manifest.yaml` records the fork dependencies.

| Platform | Fork / core | Provider plan | Selection and acceptance status |
| --- | --- | --- | --- |
| Arduboy | `retrom-project/Ardens` / Ardens | EmulatorJS | Active emulator with native save states; browser candidate built, product launch pending. |
| Atari 8-bit, XEGS | `retrom-project/libretro-atari800` / Atari800 | EmulatorJS | One maintained core covers both machines; separate targets set the model and bundled AltirraOS. Browser candidate built, both machine modes need product validation. |
| Atari ST | `retrom-project/hatariB` / HatariB | EmulatorJS | Hatari-based libretro fork has native state API and built-in EmuTOS. Browser candidate built; product launch pending. |
| BBC Micro | `retrom-project/jsbeeb` / jsbeeb | retrom-runtime | Reproducible browser archive and Provider adapter built. A Chrome core smoke test booted `Welcome.ssd` with three user-supplied ROMs and restored a checkpoint in a new iframe. Retrom review and product launch remain pending. |
| Channel F | `retrom-project/FreeChaF` / FreeChaF | EmulatorJS | Libretro core with state API; browser candidate built. Two 1 KiB BIOS files are required; the Channel F II BIOS is optional. |
| Mega Duck | `retrom-project/SameBoy` / SameDuck | EmulatorJS | SameBoy branch with Mega Duck support and state API; browser candidate built. |
| SAM Coupé | `retrom-project/SamCoupeWeb` / SamCoupeWeb | retrom-runtime | The existing Web build booted the author's downloadable `SafariSam.dsk` in Chrome (screenshot in PFB evidence), but lacks state import/export. A checkpoint and restore implementation is required before declaring support. The existing libretro-simcoupe port describes itself as buggy, with incomplete input and no sound. The checked-in Web data includes machine ROMs, so any candidate must exclude those assets and load user-supplied BIOS. |
| Supervision | `retrom-project/potator` / Potator | EmulatorJS | Libretro core with state API; browser candidate built. |
| Thomson | `retrom-project/theodore` / Theodore | EmulatorJS | Libretro core supports Thomson family media and states; browser candidate built. |
| Sega Model 3 | `retrom-project/Libretro-Supermodel` / Supermodel | EmulatorJS (investigating) | Current GLES3 libretro implementation has a state API, but no established Emscripten build. Browser build and game performance remain unverified. |

The eight built core candidates cover nine requested platforms: Atari800 and XEGS
share one core. Their combined Provider bundle passes package verification, but it
has not been imported into the PFB product. SAM Coupé and Model 3 have no browser
candidate. Do not treat a target declaration or a browser-only smoke test as product
acceptance.

SAM Coupé core-only smoke: `/data/game/testgame/retrom-runtime/samcoupe/SafariSam.dsk`
(SHA-256 `874c06473ea3c64598c2f1c7d725eb6203fb0dc37a02d9027cb626fe84b177ca`),
downloaded from the [author's public page](https://www.martinfitzpatrick.com/safari-sam/). The PFB screenshot is
`.pfb/evidence/samcoupe-core-smoke.png`; it shows the game's introductory screen.

For every platform, the release gate requires a reproducible full core archive,
Provider candidate with fixed source identity, and a real Retrom import, preview,
launch, gamepad, checkpoint, new-instance restore, and screenshot run in Chrome.
Only assets that pass this gate should be pinned as formal releases. Game and BIOS
files used for testing stay in `/data/game` and outside Git.
