import { afterEach, describe, expect, it } from "vitest";
import { getActiveImmersiveGamepadIndex, setActiveImmersiveGamepadIndex } from "./active-gamepad";

describe("active immersive gamepad claim", () => {
  afterEach(() => setActiveImmersiveGamepadIndex(null));

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
});
