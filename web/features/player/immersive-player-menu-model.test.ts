import { describe, expect, it } from "vitest";
import { moveImmersiveMenuSelection, selectableImmersiveMenuItem } from "./immersive-player-menu-model";

describe("immersive player menu selection", () => {
  it("includes the cursor in visible order while skipping unsupported operations", () => {
    expect(moveImmersiveMenuSelection(0, "right", true, true)).toBe(3);
    expect(moveImmersiveMenuSelection(3, "right", true, true)).toBe(1);
    expect(moveImmersiveMenuSelection(3, "right", false, true)).toBe(2);
    expect(moveImmersiveMenuSelection(1, "left", true, true)).toBe(3);
    expect(selectableImmersiveMenuItem(3, true, false)).toBe(false);
    expect(selectableImmersiveMenuItem(3, false, true)).toBe(true);
  });
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
  it("includes game editing only when the runtime offers it", () => {
    expect(moveImmersiveMenuSelection(1, "right", true, false, true)).toBe(4);
    expect(moveImmersiveMenuSelection(4, "right", true, false, true)).toBe(2);
    expect(moveImmersiveMenuSelection(0, "right", false, false, true)).toBe(4);
    expect(selectableImmersiveMenuItem(4, true, false, false)).toBe(false);
    expect(selectableImmersiveMenuItem(4, true, false, true)).toBe(true);
  });
  it("keeps cursor and game editing as separate actions", () => {
    expect(moveImmersiveMenuSelection(0, "right", true, true, true)).toBe(3);
    expect(moveImmersiveMenuSelection(1, "right", true, true, true)).toBe(4);
    expect(moveImmersiveMenuSelection(4, "right", true, true, true)).toBe(2);
    expect(selectableImmersiveMenuItem(3, true, true, true)).toBe(true);
    expect(selectableImmersiveMenuItem(4, true, true, true)).toBe(true);
  });
});
