# DOS content cache fixture

Retrom-owned, MIT-licensed x86 real-mode program and data. No third-party DOS,
BIOS, game or binary fragments are included. `build.py` is the sole generation
source; `python3 build.py --check` checks the complete committed ZIP bytes.

`CACHE.COM` draws a blue VGA screen. Every keyboard event opens `LATER.DAT`,
seeks to 4 MiB, checks one byte and advances the screen color. A failed read
draws bright red. The counter lives in emulated memory and must survive an
instant checkpoint. The 8 MiB stored member lets the real DOSBox Pure consumer
distinguish default Range startup from complete preload and read new offsets
after the browser goes offline. ZIP timestamps, order and permissions are fixed.

Product consumer: `scripts/acceptance/content_preload_product.mjs`, through the
real Retrom import, review preview, publish, Launch, input and checkpoint APIs.
