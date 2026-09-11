import { describe, expect, it } from "vitest";
import { constrainNavigation } from "./use-floating-navigation";

describe("floating favorite navigation bounds", () => {
  it("keeps a moved window in the viewport and recovers after resizing", () => {
    const size = { width: 250, height: 400 };
    expect(constrainNavigation({ x: 300, y: 80 }, size, { width: 1280, height: 800 })).toEqual({ x: 300, y: 80 });
    expect(constrainNavigation({ x: -100, y: -100 }, size, { width: 1280, height: 800 })).toEqual({ x: 8, y: 8 });
    expect(constrainNavigation({ x: 2000, y: 1000 }, size, { width: 390, height: 844 })).toEqual({ x: 132, y: 436 });
    expect(constrainNavigation({ x: 300, y: 800 }, { width: 250, height: 600 }, { width: 390, height: 500 })).toEqual({ x: 132, y: 8 });
  });
});
