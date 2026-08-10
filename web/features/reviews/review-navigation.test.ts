import { describe, expect, it } from "vitest";
import { adjacentReviewItemId } from "./review-navigation";

describe("adjacentReviewItemId", () => {
  it("prefers the next item and falls back to the previous item", () => {
    expect(adjacentReviewItemId(["one", "two", "three"], "two")).toBe("three");
    expect(adjacentReviewItemId(["one", "two", "three"], "three")).toBe("two");
  });

  it("uses the first filtered item when an edit moved the current item off the loaded page", () => {
    expect(adjacentReviewItemId(["one", "two"], "moved-current")).toBe("one");
  });

  it("returns null only when the filtered queue has no other item", () => {
    expect(adjacentReviewItemId(["only"], "only")).toBeNull();
    expect(adjacentReviewItemId([], "missing")).toBeNull();
  });
});
