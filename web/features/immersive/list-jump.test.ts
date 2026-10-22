import { describe, expect, it } from "vitest";
import { groupTarget, pageTarget, titleGroup } from "./list-jump";

const titles = ["  apple", "Avalon", "超级马里奥1", "超级马里奥2", "超时空", "魔界村", "魔法门", "龙战士"];

describe("immersive list jumps", () => {
  it("groups titles by the same first character the server sorts by", () => {
    expect(titleGroup("  apple")).toBe("A");
    expect(titleGroup("超级马里奥1")).toBe("超");
    expect(titleGroup("   ")).toBe("");
  });

  it("jumps to the start of the next group and back through groups", () => {
    expect(groupTarget(titles, 0, "next", false)).toEqual({ index: 2, loadMore: false });
    expect(groupTarget(titles, 2, "next", false)).toEqual({ index: 5, loadMore: false });
    expect(groupTarget(titles, 6, "previous", false)).toEqual({ index: 5, loadMore: false });
    expect(groupTarget(titles, 5, "previous", false)).toEqual({ index: 2, loadMore: false });
    expect(groupTarget(titles, 0, "previous", false)).toEqual({ index: 0, loadMore: false });
  });

  it("asks for more titles when the next group is beyond the loaded batch", () => {
    expect(groupTarget(titles, 7, "next", true)).toEqual({ index: 7, loadMore: true });
    expect(groupTarget(titles, 7, "next", false)).toEqual({ index: 7, loadMore: false });
  });

  it("pages within the loaded titles and loads the next batch at the end", () => {
    expect(pageTarget(1, 5, "next", 8, true)).toEqual({ index: 6, loadMore: false });
    expect(pageTarget(4, 5, "next", 8, true)).toEqual({ index: 7, loadMore: true });
    expect(pageTarget(4, 5, "next", 8, false)).toEqual({ index: 7, loadMore: false });
    expect(pageTarget(3, 5, "previous", 8, true)).toEqual({ index: 0, loadMore: false });
  });
});
