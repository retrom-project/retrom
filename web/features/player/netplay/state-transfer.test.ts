import { describe, expect, it, vi } from "vitest";
import {
  coreStateBytes,
  digestHex,
  transferCoreStateBytes,
  transferStateBytes,
} from "./ejs-netplay-4.2.3-v1";
import { encodeStateFrame } from "./protocol";
import { applyTransferredState, pendingStateFromMessage } from "./state-transfer";

const sessionID = "01980000-0000-7000-8000-000000000001";
const transferID = "01980000-0000-7000-8000-000000000002";

function raState(trackedInput: number[]) {
  const root = [0x4e, 0x46, 0x4f, 0, 8, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8];
  const core = new Uint8Array(8 + root.length + trackedInput.length + 3);
  core.set([0x4e, 0x53, 0x54, 0x1a]);
  new DataView(core.buffer).setUint32(4, root.length, true);
  core.set(root, 8);
  core.set(trackedInput, 8 + root.length);
  core.set([7, 7, 7], 8 + root.length + trackedInput.length);
  const padded = (core.length + 7) & ~7;
  const state = new Uint8Array(8 + 8 + padded + 8);
  state.set(new TextEncoder().encode("RASTATE"));
  state[7] = 1;
  state.set(new TextEncoder().encode("MEM "), 8);
  new DataView(state.buffer).setUint32(12, core.length, true);
  state.set(core, 16);
  state.set(new TextEncoder().encode("END "), 16 + padded);
  return state;
}

async function pendingFor(state: Uint8Array, profileID: string) {
  return {
    transferId: transferID,
    nextFrame: 12,
    byteLength: state.byteLength,
    stateSha256: await digestHex(state),
    coreSha256: await digestHex(transferCoreStateBytes(coreStateBytes(state), profileID)),
  };
}

function bridgeRecapturing(state: Uint8Array<ArrayBuffer>) {
  return {
    captureState: vi.fn(() => state),
    loadStateForTransfer: vi.fn(async () => ({
      recaptured: state,
      byteExact: false,
      coreExact: false,
      expectedCoreBytes: coreStateBytes(state).byteLength,
      recapturedCoreBytes: coreStateBytes(state).byteLength,
      firstCoreMismatch: 24,
      lastCoreMismatch: 27,
      coreMismatchCount: 4,
      coreMismatchRanges: [{ start: 24, end: 28 }],
    })),
  };
}

function exactBridge(before: Uint8Array<ArrayBuffer>, authority: Uint8Array<ArrayBuffer>) {
  return {
    captureState: vi.fn(() => before),
    loadStateForTransfer: vi.fn(async () => ({
      recaptured: authority,
      byteExact: true,
      coreExact: true,
      expectedCoreBytes: coreStateBytes(authority).byteLength,
      recapturedCoreBytes: coreStateBytes(authority).byteLength,
      firstCoreMismatch: -1,
      lastCoreMismatch: -1,
      coreMismatchCount: 0,
      coreMismatchRanges: [],
    })),
  };
}

describe("netplay state transfer", () => {
  it("validates a complete STATE_META before creating pending state", () => {
    const message = {
      v: 1, type: "STATE_META", sessionId: sessionID, epoch: 3, seq: 1,
      transferId: transferID, nextFrame: 12, byteLength: 32,
      stateSha256: "1".repeat(64), coreSha256: "2".repeat(64),
    };
    expect(pendingStateFromMessage(message)).toEqual({
      transferId: transferID, nextFrame: 12, byteLength: 32,
      stateSha256: "1".repeat(64), coreSha256: "2".repeat(64),
    });
    expect(() => pendingStateFromMessage({ ...message, byteLength: 0 }))
      .toThrow("PROTOCOL_VIOLATION");
  });

  it("accepts a Nestopia recapture differing only in its unrestorable tracked input", async () => {
    const wireState = transferStateBytes(
      raState([2, 2, 2, 2, 0, 0, 0, 0]), "nestopia-423-v1",
    );
    const recaptured = raState([1, 1, 1, 1, 0, 0, 0, 0]);
    const pending = await pendingFor(wireState, "nestopia-423-v1");
    const bridge = bridgeRecapturing(recaptured);
    const onStateLoad = vi.fn();
    const result = await applyTransferredState(
      encodeStateFrame(sessionID, transferID, 3, 12, wireState),
      pending,
      sessionID,
      3,
      "nestopia-423-v1",
      bridge,
      { onStateLoad },
    );
    expect(result).toEqual({ stateSha256: pending.stateSha256, coreSha256: pending.coreSha256 });
    expect(onStateLoad).toHaveBeenCalledWith(expect.objectContaining({ coreExact: true, byteExact: false }));
  });

  it("rejects the same recapture difference for another profile", async () => {
    const wireState = raState([2, 2, 2, 2, 0, 0, 0, 0]);
    const recaptured = raState([1, 1, 1, 1, 0, 0, 0, 0]);
    const pending = await pendingFor(wireState, "snes9x-423-v1");
    await expect(applyTransferredState(
      encodeStateFrame(sessionID, transferID, 3, 12, wireState),
      pending,
      sessionID,
      3,
      "snes9x-423-v1",
      bridgeRecapturing(recaptured),
    )).rejects.toThrow("STATE_INVALID");
  });

  it.each([
    { name: "already converged", beforeInput: [2, 2, 2, 2, 0, 0, 0, 0], changed: false },
    { name: "different before load", beforeInput: [1, 1, 1, 1, 0, 0, 0, 0], changed: true },
  ])("records complete SNES no-op evidence when the target is $name", async ({ beforeInput, changed }) => {
    const authority = raState([2, 2, 2, 2, 0, 0, 0, 0]);
    const pending = await pendingFor(authority, "snes9x-423-v1");
    const bridge = exactBridge(raState(beforeInput), authority);
    const onStateLoad = vi.fn();
    await applyTransferredState(
      encodeStateFrame(sessionID, transferID, 3, 12, authority),
      pending,
      sessionID,
      3,
      "snes9x-423-v1",
      bridge,
      { onStateLoad },
    );
    expect(bridge.loadStateForTransfer).toHaveBeenCalledOnce();
    expect(onStateLoad).toHaveBeenCalledWith({
      epoch: 3, nextFrame: 12, byteLength: authority.byteLength,
      stateDigest: pending.stateSha256, coreDigest: pending.coreSha256,
      changed, nativeCompletion: true, byteExact: true, coreExact: true,
      expectedCoreBytes: coreStateBytes(authority).byteLength,
      recapturedCoreBytes: coreStateBytes(authority).byteLength,
      firstCoreMismatch: -1,
    });
  });
});
