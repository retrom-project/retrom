import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {readFileSync} from "node:fs";
import test from "node:test";
import {build} from "../../../testdata/public-roms/wasm4-controls/build.mjs";

test("owned WASM-4 bytes are deterministic and movement/confirmation live in checkpoint memory", async () => {
  const bytes = build();
  assert.equal(createHash("sha256").update(bytes).digest("hex"), "c19447a62cb51bbe9b91e3ef3002c598972bd20bfb85f96668e5c05e85e256cd");
  assert.deepEqual(bytes, new Uint8Array(readFileSync(new URL("../../../testdata/public-roms/wasm4-controls/controls.wasm", import.meta.url))));
  const memory = new WebAssembly.Memory({initial: 1, maximum: 1});
  let rect;
  const {instance} = await WebAssembly.instantiate(bytes, {env: {memory, rect: (...values) => {rect = values;}}});
  new Uint8Array(memory.buffer).fill(0, 0xa0, 0x19a0); // Real runtime clears its framebuffer before each update.
  instance.exports.update(); assert.deepEqual(rect, [20, 20, 10, 10]);
  const ram = new Uint8Array(memory.buffer); ram[0x16] = 32; instance.exports.update();
  assert.deepEqual(rect, [21, 20, 10, 10]);
  ram[0x16] = 1; instance.exports.update(); assert.deepEqual(rect, [21, 40, 10, 10]);
  const saved = ram.slice(); ram[0x16] = 16; instance.exports.update(); assert.deepEqual(rect, [20, 40, 10, 10]);
  ram.set(saved); ram[0x16] = 0; instance.exports.update(); assert.deepEqual(rect, [21, 40, 10, 10]);
});

import {runCart, withWasm4RunCart} from "../wasm4_run_cart.mjs";
test("repeatable cart import preserves source bytes and records both actual digests", async () => {
  const source = new URL("../../../testdata/public-roms/wasm4-controls/controls.wasm", import.meta.url);
  const original = readFileSync(source), sha = bytes => createHash("sha256").update(bytes).digest("hex");
  const receipt = await withWasm4RunCart(source, async (filename, bytes, receipt) => {
    assert.deepEqual(readFileSync(filename), bytes);
    assert.deepEqual(bytes.subarray(0, original.length), original);
    assert.equal(receipt.sourceSha256, sha(original)); assert.equal(receipt.outputSha256, sha(bytes));
    assert.equal(receipt.outputSizeBytes, bytes.length); assert.equal(receipt.sourceSizeBytes, original.length);
    return receipt;
  });
  assert.deepEqual(readFileSync(source), original); assert.notEqual(receipt.outputSha256, receipt.sourceSha256);
});
test("per-run custom metadata preserves the canonical fixture instructions and initial memory", async () => {
  const original = build(), wrapped = runCart(Buffer.from(original), "01234567-1234-1234-1234-0123456789ab");
  assert.deepEqual(new Uint8Array(wrapped.subarray(0, original.length)), original);
  const memory = new WebAssembly.Memory({initial: 1, maximum: 1}); let drawn;
  const {instance} = await WebAssembly.instantiate(wrapped, {env: {memory, rect: (...args) => {drawn = args;}}});
  instance.exports.update(); assert.deepEqual(drawn, [20, 20, 10, 10]);
  assert.notDeepEqual(wrapped, runCart(Buffer.from(original), "01234567-1234-1234-1234-0123456789ac"));
});
