import {describe, expect, it, vi} from "vitest";

import type {RuntimeNetplayPortV1} from "./contract";
import {RuntimeNetplayPortAdapter} from "./netplay-port-adapter";

describe("RuntimeNetplayPortAdapter", () => {
  it("maps canonical four-player controls only through RuntimeNetplayPortV1", async () => {
    const port = fixturePort();
    const adapter = new RuntimeNetplayPortAdapter(port);
    const players = [
      Array(24).fill(1), Array(24).fill(2), Array(24).fill(3), Array(24).fill(4),
    ] as const;

    await adapter.runFrame(players, 7, true);
    expect(port.runFrame).toHaveBeenCalledWith(new Int16Array([
      ...players[0], ...players[1], ...players[2], ...players[3],
    ]), 7, true);
    await expect(adapter.captureState(7)).resolves.toEqual(new Uint8Array([1, 2, 3]));
    await expect(adapter.pauseAtBoundary()).resolves.toBe(8);
    expect(adapter.sampleLocalControls()).toEqual(Array(24).fill(0));
    await adapter.close();
    await adapter.close();
    expect(port.close).toHaveBeenCalledOnce();
  });

  it("fails closed on malformed ports, controls, frames and results", async () => {
    expect(() => new RuntimeNetplayPortAdapter({...fixturePort(), controlCount: 12}))
      .toThrow("PLAYER_RUNTIME_CONTRACT_INVALID");
    const port = fixturePort();
    const adapter = new RuntimeNetplayPortAdapter(port);
    await expect(adapter.runFrame([[], [], [], []], 0, false))
      .rejects.toThrow("PLAYER_RUNTIME_CONTRACT_INVALID");
    port.captureState.mockResolvedValueOnce(new Uint8Array());
    await expect(adapter.captureState(0)).rejects.toThrow("PLAYER_RUNTIME_CONTRACT_INVALID");
    port.pauseAtBoundary.mockResolvedValueOnce(-1);
    await expect(adapter.pauseAtBoundary()).rejects.toThrow("PLAYER_RUNTIME_CONTRACT_INVALID");
  });
});

function fixturePort() {
  return {
    captureState: vi.fn(async () => new Uint8Array([1, 2, 3])),
    close: vi.fn(async () => undefined), controlCount: 24,
    loadStateAndWait: vi.fn(async () => undefined),
    pauseAtBoundary: vi.fn(async () => 8), resetLocalControls: vi.fn(),
    runFrame: vi.fn(async () => undefined),
    sampleLocalControls: vi.fn(() => new Int16Array(24)),
  } satisfies RuntimeNetplayPortV1;
}
