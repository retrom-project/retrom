import {describe, expect, it, vi} from "vitest";
import {testNetplayPort} from "./netplay-port.test-helper";
import {encodeStateFrame} from "./protocol";
import {digestNetplayState} from "./state-digest";
import {applyTransferredState, pendingStateFromMessage} from "./state-transfer";

const sessionId = "01980000-0000-7000-8000-000000000001";
const transferId = "01980000-0000-7000-8000-000000000002";

describe("opaque netplay state transfer", () => {
  it("validates complete STATE_META", () => {
    const message = {v: 1, type: "STATE_META", sessionId, epoch: 3, seq: 1, transferId,
      nextFrame: 12, byteLength: 3, stateSha256: "1".repeat(64), coreSha256: "1".repeat(64)};
    expect(pendingStateFromMessage(message)).toMatchObject({transferId, nextFrame: 12, byteLength: 3});
    expect(() => pendingStateFromMessage({...message, byteLength: 0})).toThrow("PROTOCOL_VIOLATION");
  });

  it("loads and byte-exactly recaptures Provider-owned opaque state", async () => {
    const authority = Uint8Array.of(7, 8, 9);
    let current = Uint8Array.of(1, 2, 3);
    const digest = await digestNetplayState(authority);
    const load = vi.fn(async (state: Uint8Array) => {current = new Uint8Array(state);});
    const bridge = testNetplayPort({captureState: vi.fn(() => current), loadStateAndWait: load});
    const onStateLoad = vi.fn();
    await expect(applyTransferredState(
      encodeStateFrame(sessionId, transferId, 3, 12, authority),
      {transferId, nextFrame: 12, byteLength: 3, stateSha256: digest, coreSha256: digest},
      sessionId, 3, "provider-opaque-v1", bridge, {onStateLoad},
    )).resolves.toEqual({stateSha256: digest, coreSha256: digest});
    expect(load).toHaveBeenCalledWith(authority, 12);
    expect(onStateLoad).toHaveBeenCalledWith(expect.objectContaining({
      byteExact: true, coreExact: true, changed: true, nativeCompletion: true,
    }));
  });

  it("rejects a Provider recapture that differs from transferred bytes", async () => {
    const authority = Uint8Array.of(7, 8, 9);
    const digest = await digestNetplayState(authority);
    const bridge = testNetplayPort({
      captureState: vi.fn(() => Uint8Array.of(1, 2, 3)), loadStateAndWait: vi.fn(async () => undefined),
    });
    await expect(applyTransferredState(
      encodeStateFrame(sessionId, transferId, 3, 12, authority),
      {transferId, nextFrame: 12, byteLength: 3, stateSha256: digest, coreSha256: digest},
      sessionId, 3, "provider-opaque-v1", bridge,
    )).rejects.toThrow("STATE_INVALID");
  });
});
