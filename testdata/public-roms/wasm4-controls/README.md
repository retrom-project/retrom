# Retrom WASM-4 controls fixture

This MIT-licensed, project-owned cartridge contains only the WebAssembly program
in `build.mjs`. It imports the WASM-4 memory and rectangle drawing API; no engine,
SDK, game, font, BIOS or other third-party bytes are embedded.

Left/right move a 10×10 square. Confirm moves it to row 40. The position lives in
linear memory, so the real runtime checkpoint restores its visible position.
There is no randomness, animation, disk access, clock dependency or audio.

Regenerate with `node build.mjs`. The fixed SHA-256 is
`c19447a62cb51bbe9b91e3ef3002c598972bd20bfb85f96668e5c05e85e256cd`.
`node --test scripts/acceptance/tests/wasm4_fixture_test.mjs` checks the committed
bytes and executes the program. ACC-WASM4-001 consumes it through real Retrom
upload, review, launch, content and Player routes, alongside an operator cart.
