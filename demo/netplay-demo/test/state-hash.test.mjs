import assert from "node:assert/strict";
import test from "node:test";
import { coreStateBytes } from "../src/state-hash.js";

test("extracts the deterministic core payload from a RASTATE1 container", () => {
  const state = new Uint8Array(8 + 8 + 8 + 8);
  state.set(new TextEncoder().encode("RASTATE"));
  state[7] = 1;
  state.set(new TextEncoder().encode("MEM "), 8);
  new DataView(state.buffer).setUint32(12, 3, true);
  state.set([9, 8, 7], 16);
  state.set(new TextEncoder().encode("END "), 24);
  assert.deepEqual(coreStateBytes(state), new Uint8Array([9, 8, 7]));
});

test("passes through raw libretro states and rejects malformed RASTATE", () => {
  assert.deepEqual(coreStateBytes(new Uint8Array([1, 2, 3])), new Uint8Array([1, 2, 3]));
  const malformed = new Uint8Array(new TextEncoder().encode("RASTATE2"));
  assert.throws(() => coreStateBytes(malformed), /version/);
});
