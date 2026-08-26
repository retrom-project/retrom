import { describe, expect, it } from "vitest";
import { decodeMkxpRastate, encodeMkxpRastate } from "./state";

const coreSize = 16;

describe("mkxp RASTATE1 boundary codec", () => {
  it("round-trips exact raw mkxp core bytes through the deterministic envelope", () => {
    const core = coreState();
    const state = encodeMkxpRastate(core, coreSize);

    expect(state.byteLength).toBe(coreSize + 24);
    expect(state.slice(0, 16)).toEqual(Uint8Array.of(
      0x52, 0x41, 0x53, 0x54, 0x41, 0x54, 0x45, 1,
      0x4d, 0x45, 0x4d, 0x20, coreSize, 0, 0, 0,
    ));
    expect(state.slice(16 + coreSize)).toEqual(Uint8Array.of(0x45, 0x4e, 0x44, 0x20, 0, 0, 0, 0));
    expect(decodeMkxpRastate(state, coreSize)).toEqual(core);
  });

  it.each([
    ["RASTATE magic", 0, 0],
    ["RASTATE version", 7, 2],
    ["MEM tag", 8, 0],
    ["MEM size", 12, coreSize - 1],
    ["END tag", 16 + coreSize, 0],
    ["END size", 20 + coreSize, 1],
  ])("rejects an invalid %s", (_name, offset, value) => {
    const state = encodeMkxpRastate(coreState(), coreSize);
    state[offset] = value;

    expect(() => decodeMkxpRastate(state, coreSize)).toThrow("RPG_CHECKPOINT_CREATE_FAILED");
  });

  it("rejects trailing envelope bytes", () => {
    const state = encodeMkxpRastate(coreState(), coreSize);
    const trailing = new Uint8Array(state.byteLength + 1);
    trailing.set(state);

    expect(() => decodeMkxpRastate(trailing, coreSize)).toThrow("RPG_CHECKPOINT_CREATE_FAILED");
  });

  it.each([
    ["raw magic", 0, 0],
    ["raw version", 4, 2],
  ])("rejects an invalid %s in both directions", (_name, offset, value) => {
    const core = coreState();
    core[offset] = value;

    expect(() => encodeMkxpRastate(core, coreSize)).toThrow("RPG_CHECKPOINT_CREATE_FAILED");
    const state = encodeMkxpRastate(coreState(), coreSize);
    state[16 + offset] = value;
    expect(() => decodeMkxpRastate(state, coreSize)).toThrow("RPG_CHECKPOINT_CREATE_FAILED");
  });
});

function coreState() {
  const core = new Uint8Array(coreSize);
  core.set([0x6d, 0x6b, 0x78, 0x70, 1, 0, 0, 0]);
  core.set([8, 9, 10, 11, 12, 13, 14, 15], 8);
  return core;
}
