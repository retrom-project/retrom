import test from "node:test";
import {fantasyBoundaryCart} from "./fantasy_run_cart.mjs";
import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {readFile} from "node:fs/promises";
import {join} from "node:path";
import {fileURLToPath, pathToFileURL} from "node:url";

const provider = process.env.RETROM_FANTASY_PROVIDER_ROOT;
assert.ok(provider, "FANTASY_NATIVE_VERIFIED_PROVIDER_REQUIRED");
const fixture = process.env.RETROM_FANTASY_FIXTURE_ROOT ?? fileURLToPath(new URL("../../testdata/public-roms/fantasy-controls", import.meta.url));
const integrity = JSON.parse(await readFile(join(provider, "integrity.json"), "utf8"));
const sha = bytes => createHash("sha256").update(bytes).digest("hex");
async function asset(core, extension) {
  const path = `assets/${core}/${core}-retrom.${extension}`, bytes = await readFile(join(provider, path));
  const expected = integrity.files.find(row => row.path === path); assert.ok(expected);
  assert.equal(bytes.length, expected.sizeBytes); assert.equal(sha(bytes), expected.sha256); return {path, bytes, sha256: sha(bytes)};
}
function withBytes(module, bytes, consume) {
  const pointer = module._malloc(bytes.length); assert.ok(pointer);
  try {module.HEAPU8.set(bytes, pointer); return consume(pointer, bytes.length);} finally {module._free(pointer);}
}
function square(module, core) {
  const width = core === "tic80" ? 240 : 128, pointer = module._retrom_pixels();
  const pixels = module.HEAPU8.subarray(pointer, pointer + width * (core === "tic80" ? 136 : 128) * 4);
  for (let x = 0; x < width; x++) {
    const at = (20 * width + x) * 4;
    if ([0, 1, 2].some(channel => pixels[at + channel] !== pixels[channel])) {
      return {x, color: [...pixels.subarray(at, at + 3)]};
    }
  }
  throw new Error("FANTASY_NATIVE_SQUARE_MISSING");
}
for (const [core, extension, right, left, confirm] of [["tic80", "tic", 8, 4, 16], ["fake08", "p8", 128, 64, 1]]) {
  for (const maximum of [false, true]) test(`[HP-05] CORE/${core} ${maximum ? "4 MiB boundary" : "canonical"} cart displays confirmation and restores direction plus color in a new instance`, {timeout: 30000}, async context => {
    const js = await asset(core, "mjs"), wasm = await asset(core, "wasm");
    const create = (await import(pathToFileURL(join(provider, js.path)))).default;
    const originalCart = await readFile(join(fixture, `controls.${extension}`));
    const cart = maximum ? fantasyBoundaryCart(core, originalCart, 4194304) : originalCart;
    const instances = [];
    const load = async () => {
      const module = await create({wasmBinary: wasm.bytes, print: () => {}, printErr: () => {}}); instances.push(module);
      assert.equal(module._retrom_abi(), 1);
      assert.equal(withBytes(module, cart, (pointer, length) => module._retrom_load(pointer, length)), 1); return module;
    };
    try {
      if (maximum) {
        const rejected = await load(), oversized = fantasyBoundaryCart(core, originalCart, 4194305);
        assert.equal(withBytes(rejected, oversized, (at, size) => rejected._retrom_load(at, size)), 0);
      }
      const original = await load(); assert.equal(original._retrom_step(0), 1);
      const initial = square(original, core); assert.equal(initial.x, 20);
      for (let frame = 0; frame < 5; frame++) assert.equal(original._retrom_step(right), 1);
      const moved = square(original, core); assert.ok(moved.x > initial.x); assert.deepEqual(moved.color, initial.color);
      assert.equal(original._retrom_step(confirm), 1); assert.equal(original._retrom_step(0), 1);
      const confirmed = square(original, core); assert.equal(confirmed.x, moved.x);
      assert.notDeepEqual(confirmed.color, initial.color, "FANTASY_CONFIRM_NOT_VISIBLE");
      const pointer = original._retrom_state(), size = original._retrom_state_size(); assert.ok(pointer && size > 0);
      const saved = original.HEAPU8.slice(pointer, pointer + size), restored = await load();
      assert.equal(withBytes(restored, saved, (at, length) => restored._retrom_restore(at, length)), 1);
      assert.equal(restored._retrom_step(0), 1); const restoredState = square(restored, core); assert.deepEqual(restoredState, confirmed);
      assert.equal(restored._retrom_step(left), 1); assert.ok(square(restored, core).x < confirmed.x);
      context.diagnostic(JSON.stringify({core, cartSha256: sha(cart), moduleSha256: js.sha256, wasmSha256: wasm.sha256,
        initial, moved, confirmed, restored: restoredState, stateSizeBytes: size}));
    } finally {for (const module of instances) module._retrom_stop();}
  });
}
