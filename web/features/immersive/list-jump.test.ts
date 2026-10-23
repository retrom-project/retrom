import { describe, expect, it } from "vitest";
import { letterAt, letterTarget, pageTarget } from "./list-jump";

const letters = [
  { letter: "A", offset: 0, count: 2 },
  { letter: "C", offset: 2, count: 3 },
  { letter: "M", offset: 5, count: 2 },
  { letter: "#", offset: 7, count: 1 },
];

describe("immersive list jumps", () => {
  it("finds the letter group of a list position", () => {
    expect(letterAt(letters, 0)).toBe("A");
    expect(letterAt(letters, 4)).toBe("C");
    expect(letterAt(letters, 7)).toBe("#");
    expect(letterAt([], 3)).toBe("");
  });

  it("jumps to the next letter and back through letters", () => {
    expect(letterTarget(letters, 0, "next")).toBe(2);
    expect(letterTarget(letters, 3, "next")).toBe(5);
    expect(letterTarget(letters, 7, "next")).toBeNull();
    expect(letterTarget(letters, 6, "previous")).toBe(5);
    expect(letterTarget(letters, 5, "previous")).toBe(2);
    expect(letterTarget(letters, 0, "previous")).toBeNull();
    expect(letterTarget([], 3, "previous")).toBeNull();
  });

  it("pages within the loaded window and loads the next batch at the end", () => {
    expect(pageTarget(1, 5, "next", 8, true)).toEqual({ index: 6, loadMore: false });
    expect(pageTarget(4, 5, "next", 8, true)).toEqual({ index: 7, loadMore: true });
    expect(pageTarget(4, 5, "next", 8, false)).toEqual({ index: 7, loadMore: false });
    expect(pageTarget(3, 5, "previous", 8, true)).toEqual({ index: 0, loadMore: false });
  });
});
