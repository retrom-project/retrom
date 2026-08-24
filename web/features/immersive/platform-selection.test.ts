import { describe, expect, it } from "vitest";
import { wrapPlatformIndex } from "./platform-selection";

describe("immersive platform carousel selection", () => {
  it("wraps both ends of the carousel", () => {
    expect(wrapPlatformIndex(0, "left", 4)).toBe(3);
    expect(wrapPlatformIndex(3, "right", 4)).toBe(0);
    expect(wrapPlatformIndex(1, "right", 4)).toBe(2);
  });

  it("keeps a single platform selected and handles an empty list", () => {
    expect(wrapPlatformIndex(0, "left", 1)).toBe(0);
    expect(wrapPlatformIndex(0, "right", 0)).toBe(-1);
  });
});
