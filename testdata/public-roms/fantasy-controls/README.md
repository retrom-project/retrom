# Retrom fantasy console controls

These MIT-licensed, project-owned carts contain only the Lua programs in
`build.mjs`. No engine, game, SDK, BIOS or other third-party bytes are embedded.
Regenerate both canonical files with `node build.mjs`.

| File | SHA-256 |
| --- | --- |
| controls.tic | c637c1f3e6f24af56850448fcd6fd6e6c06fa4e40fd735c02582b2cff20fb029 |
| controls.p8 | 9965557a27b806c95174d5aabedb97d166836c26d4ac3372ae2b6bd44c6558f3 |

Left/right move a five-pixel square. Confirm changes palette index 8 to 11.
TIC-80 also saves both position and color in native pmem; FAKE-08 restores both
from its native checkpoint. There is no randomness, animation or audio.
An inert Lua comment supplies the optional per-run import identity.

ACC-TIC-001 and ACC-PICO-001 consume these programs through Retrom upload,
Review Preview, publish, Launch, Player input and save/restore. The generators
and canonical bytes are checked by `fantasy_fixture_test.mjs`; explicit
`fantasy_controls_native.mjs` tests run the actual verified Provider core assets
and check movement, visible confirmation and restoration in a fresh instance.
Native fixture tests do not substitute for the real product cases.
