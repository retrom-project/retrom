import { describe, expect, it } from "vitest";
import { moveImmersiveMenuSelection, selectableImmersiveMenuItem } from "./immersive-player-menu-model";

describe("immersive player menu selection", () => {
  it("cycles through cancel, save, and exit in both directions", () => {
    expect(moveImmersiveMenuSelection(0, "right", true)).toBe(1);
    expect(moveImmersiveMenuSelection(1, "right", true)).toBe(2);
    expect(moveImmersiveMenuSelection(2, "right", true)).toBe(0);
    expect(moveImmersiveMenuSelection(0, "left", true)).toBe(2);
  });

  it("skips unavailable save without blocking cancel or exit", () => {
    expect(moveImmersiveMenuSelection(0, "right", false)).toBe(2);
    expect(moveImmersiveMenuSelection(2, "right", false)).toBe(0);
    expect(moveImmersiveMenuSelection(0, "left", false)).toBe(2);
    expect(selectableImmersiveMenuItem(1, false)).toBe(false);
  });
});
