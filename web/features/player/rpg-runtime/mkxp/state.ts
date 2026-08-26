export const mkxpRastateEnvelopeBytes = 24;
const rastateHeader = [0x52, 0x41, 0x53, 0x54, 0x41, 0x54, 0x45, 1] as const;
const memoryTag = [0x4d, 0x45, 0x4d, 0x20] as const;
const endTag = [0x45, 0x4e, 0x44, 0x20] as const;

export function decodeMkxpRastate(state: Uint8Array, expectedCoreSize: number) {
  const endOffset = 16 + expectedCoreSize;
  if (state.byteLength !== expectedCoreSize + mkxpRastateEnvelopeBytes || expectedCoreSize < 8) {
    throw new Error("RPG_CHECKPOINT_CREATE_FAILED");
  }
  const view = new DataView(state.buffer, state.byteOffset, state.byteLength);
  if (!matchesBytes(state, 0, rastateHeader) || !matchesBytes(state, 8, memoryTag) ||
    view.getUint32(12, true) !== expectedCoreSize || !matchesBytes(state, endOffset, endTag) ||
    view.getUint32(endOffset + 4, true) !== 0) {
    throw new Error("RPG_CHECKPOINT_CREATE_FAILED");
  }
  const core = state.slice(16, endOffset);
  validateMkxpCoreState(core, expectedCoreSize);
  return core;
}

export function encodeMkxpRastate(core: Uint8Array, expectedCoreSize: number) {
  validateMkxpCoreState(core, expectedCoreSize);
  const state = new Uint8Array(expectedCoreSize + mkxpRastateEnvelopeBytes);
  const endOffset = 16 + expectedCoreSize;
  const view = new DataView(state.buffer);
  state.set(rastateHeader, 0);
  state.set(memoryTag, 8);
  view.setUint32(12, expectedCoreSize, true);
  state.set(core, 16);
  state.set(endTag, endOffset);
  view.setUint32(endOffset + 4, 0, true);
  return state;
}

function validateMkxpCoreState(core: Uint8Array, expectedCoreSize: number) {
  if (core.byteLength !== expectedCoreSize || expectedCoreSize < 8 ||
    !matchesBytes(core, 0, [0x6d, 0x6b, 0x78, 0x70]) ||
    new DataView(core.buffer, core.byteOffset, core.byteLength).getUint32(4, true) !== 1) {
    throw new Error("RPG_CHECKPOINT_CREATE_FAILED");
  }
}

function matchesBytes(bytes: Uint8Array, offset: number, expected: readonly number[]) {
  return expected.every((value, index) => bytes[offset + index] === value);
}
