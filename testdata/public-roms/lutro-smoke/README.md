# Lutro save fixture

This project-owned, MIT-licensed cartridge contains only `main.lua`. Right
moves a square; the bottom face button saves the current position through
`lutro.filesystem.write`. A new game instance reads `progress.txt` on load.
The game has no other assets, dependencies, randomness, or time-based state.

Regenerate `lutro-smoke.lutro` with `python3 build.py`. The cartridge is a ZIP
container passed intact to the Lutro core. Its SHA-256 is
`dd41cef55a43a3496e5dbe67dbb7800dd9f8071fdf3159f6ff2309fe4c0f2a87`.
It is used for the real product
upload, preview, launch, native save, and fresh Launch restore acceptance case.

To repeat the full import and review in a persistent PFB, generate an independent
cartridge with a harmless one-line Lua comment, keeping the same basename:

```sh
python3 testdata/public-roms/lutro-smoke/build.py --marker "candidate-19" --output .artifacts/lutro-repeat/lutro-smoke.lutro
```

Pass that absolute output path as `RETROM_LUTRO_CART` and use a new
`RETROM_ACCEPTANCE_CASE_DIR`. The test records the resulting content digest.
