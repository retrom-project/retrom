import { describe, expect, it, vi } from "vitest";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import { setEmulatorPaused } from "./pause-control";

describe("player pause control", () => {
  it("stops and resumes the emulator loop while projecting heartbeat state", () => {
    const toggleMainLoop = vi.fn();
    const emulator: EmulatorInstance = { on: vi.fn(), gameManager: { toggleMainLoop } };
    expect(setEmulatorPaused(emulator, true)).toBe(true);
    expect(toggleMainLoop).toHaveBeenLastCalledWith(false);
    expect(emulator.paused).toBe(true);
    expect(setEmulatorPaused(emulator, false)).toBe(true);
    expect(toggleMainLoop).toHaveBeenLastCalledWith(true);
    expect(emulator.paused).toBe(false);
  });

  it("does not claim success before the runtime loop is available", () => {
    expect(setEmulatorPaused(undefined, true)).toBe(false);
    expect(setEmulatorPaused({ on: vi.fn() }, true)).toBe(false);
  });
});
