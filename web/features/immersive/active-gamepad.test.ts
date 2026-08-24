import { afterEach, describe, expect, it } from "vitest";
import {
  consumeImmersivePlayerReturn,
  getActiveImmersiveGamepadIndex,
  markImmersivePlayerReturn,
  setActiveImmersiveGamepadIndex,
} from "./active-gamepad";

describe("active immersive gamepad claim", () => {
  afterEach(() => {
    setActiveImmersiveGamepadIndex(null);
    consumeImmersivePlayerReturn();
  });

  it("keeps the claimed index only in module memory", () => {
    const localCount = localStorage.length;
    const sessionCount = sessionStorage.length;
    expect(getActiveImmersiveGamepadIndex()).toBeNull();
    setActiveImmersiveGamepadIndex(3);
    expect(getActiveImmersiveGamepadIndex()).toBe(3);
    expect(localStorage.length).toBe(localCount);
    expect(sessionStorage.length).toBe(sessionCount);
  });

  it("releases the claim", () => {
    setActiveImmersiveGamepadIndex(1);
    setActiveImmersiveGamepadIndex(null);
    expect(getActiveImmersiveGamepadIndex()).toBeNull();
  });

  it("consumes a player-return handoff exactly once", () => {
    expect(consumeImmersivePlayerReturn()).toBe(false);
    markImmersivePlayerReturn();
    expect(consumeImmersivePlayerReturn()).toBe(true);
    expect(consumeImmersivePlayerReturn()).toBe(false);
  });
});
