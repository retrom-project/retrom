import test from "node:test";
import assert from "node:assert/strict";
import {build} from "../../../testdata/public-roms/wasm4-controls/build.mjs";
import {wasm4BoundaryCart} from "../wasm4_boundary_cart.mjs";

for (const size of [65536, 65537]) test(`WASM-4 size ${size} preserves valid instructions and actual input response`, async () => {
  const original = Buffer.from(build()), bytes = wasm4BoundaryCart(original, size);
  assert.equal(bytes.length, size); assert.deepEqual(bytes.subarray(0, original.length), original);
  const memory = new WebAssembly.Memory({initial: 1, maximum: 1}); let rectangle;
  const {instance} = await WebAssembly.instantiate(bytes, {env: {memory, rect: (...args) => {rectangle = args;}}});
  instance.exports.update(); assert.deepEqual(rectangle, [20, 20, 10, 10]);
  new Uint8Array(memory.buffer)[0x16] = 32; instance.exports.update(); assert.deepEqual(rectangle, [21, 20, 10, 10]);
  new Uint8Array(memory.buffer)[0x16] = 1; instance.exports.update(); assert.deepEqual(rectangle, [21, 40, 10, 10]);
});
test("boundary fixture refuses invalid Wasm and unsupported sizes", () => {
  assert.throws(() => wasm4BoundaryCart(Buffer.from([1, 2]), 65536), /SOURCE_INVALID/u);
  assert.throws(() => wasm4BoundaryCart(Buffer.from(build()), 65535), /SIZE_INVALID/u);
});
