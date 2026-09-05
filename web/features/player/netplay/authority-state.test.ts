import {describe, expect, it, vi} from "vitest";
import {prepareAuthorityTransfer} from "./authority-state";
import {testNetplayPort} from "./netplay-port.test-helper";

describe("opaque authority state normalization", () => {
  it("transfers the Provider recapture fixed point", async () => {
    let current = Uint8Array.of(1);
    const load = vi.fn(async (state: Uint8Array) => {current = state[0] === 1 ? Uint8Array.of(2) : new Uint8Array(state);});
    const bridge = testNetplayPort({captureState: vi.fn(() => current), loadStateAndWait: load});
    const result = await prepareAuthorityTransfer("provider-v1", 1024, bridge, undefined, {epoch: 2, nextFrame: 8});
    expect([...result.state]).toEqual([2]);
    expect(result.recaptureMatched).toBe(true);
    expect(load).toHaveBeenCalledTimes(2);
  });

  it("rejects a state that does not converge after two Provider loads", async () => {
    let current = Uint8Array.of(1);
    const bridge = testNetplayPort({
      captureState: vi.fn(() => current),
      loadStateAndWait: vi.fn(async () => {current = current[0] === 1 ? Uint8Array.of(2) : Uint8Array.of(3);}),
    });
    await expect(prepareAuthorityTransfer("provider-v1", 1024, bridge, undefined, {epoch: 2, nextFrame: 8}))
      .rejects.toThrow("STATE_INVALID");
  });
});
