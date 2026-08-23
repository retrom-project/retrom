import { describe, expect, it } from "vitest";
import { acceptsAuthorityNormalization } from "./authority-state";

function raState(core: number[]) {
  const padded = (core.length + 7) & ~7;
  const state = new Uint8Array(8 + 8 + padded + 8);
  state.set(new TextEncoder().encode("RASTATE")); state[7] = 1;
  state.set(new TextEncoder().encode("MEM "), 8);
  new DataView(state.buffer).setUint32(12, core.length, true);
  state.set(core, 16); state.set(new TextEncoder().encode("END "), 16 + padded);
  return state;
}

function nestopiaState(rootPayload: number[], trackedInput: number[], padding: number[] = []) {
  const root = [0x4e, 0x46, 0x4f, 0, 8, 0, 0, 0, ...rootPayload];
  const core = new Uint8Array(8 + root.length + trackedInput.length + padding.length);
  core.set([0x4e, 0x53, 0x54, 0x1a]);
  new DataView(core.buffer).setUint32(4, root.length, true);
  core.set(root, 8);
  core.set(trackedInput, 8 + root.length);
  core.set(padding, 8 + root.length + trackedInput.length);
  return raState([...core]);
}

function result(recaptured: Uint8Array<ArrayBuffer>, firstCoreMismatch: number, coreExact = false) {
  return {
    recaptured, byteExact: coreExact, coreExact, expectedCoreBytes: 16,
    recapturedCoreBytes: 16, firstCoreMismatch, lastCoreMismatch: firstCoreMismatch,
    coreMismatchCount: firstCoreMismatch < 0 ? 0 : 1,
    coreMismatchRanges: firstCoreMismatch < 0 ? [] : [{ start: firstCoreMismatch, end: firstCoreMismatch + 1 }],
  };
}

describe("authority state normalization", () => {
  const root = [1, 2, 3, 4, 5, 6, 7, 8];
  const expected = nestopiaState(root, [2, 2, 2, 2, 0, 0, 0, 0], [7, 7, 7]);

  it("accepts an exact native recapture for every profile", () => {
    expect(acceptsAuthorityNormalization("fbneo-423-v1", expected, result(expected, -1, true))).toBe(true);
  });

  it("accepts only Nestopia's tracked-input block after its variable-length root state", () => {
    const recaptured = nestopiaState(root, [1, 1, 1, 1, 0, 0, 0, 0], [7, 7, 7]);
    expect(acceptsAuthorityNormalization("nestopia-423-v1", expected, result(recaptured, 24))).toBe(true);
    expect(acceptsAuthorityNormalization("snes9x-423-v1", expected, result(recaptured, 24))).toBe(false);
  });

  it("rejects a root-state, padding, malformed-header, or length difference", () => {
    const rootMismatch = nestopiaState([9, ...root.slice(1)], [1, 1, 1, 1, 0, 0, 0, 0], [7, 7, 7]);
    const paddingMismatch = nestopiaState(root, [1, 1, 1, 1, 0, 0, 0, 0], [8, 7, 7]);
    const malformed = raState([9, ...root, 1, 1, 1, 1, 0, 0, 0, 0, 7, 7, 7]);
    const shorter = nestopiaState(root, [1, 1, 1, 1, 0, 0, 0, 0], [7, 7]);
    expect(acceptsAuthorityNormalization("nestopia-423-v1", expected, result(rootMismatch, 16))).toBe(false);
    expect(acceptsAuthorityNormalization("nestopia-423-v1", expected, result(paddingMismatch, 32))).toBe(false);
    expect(acceptsAuthorityNormalization("nestopia-423-v1", expected, result(malformed, 0))).toBe(false);
    expect(acceptsAuthorityNormalization("nestopia-423-v1", expected, result(shorter, 32))).toBe(false);
  });
});
