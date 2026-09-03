import {describe, expect, it} from "vitest";
import {productCheckpointPresentation} from "./player-checkpoint-availability";

describe("productCheckpointPresentation", () => {
  it("moves the product status in both directions as runtime availability changes", () => {
    expect(productCheckpointPresentation(false)).toEqual({
      text: "当前场景暂不可存档",
      tone: "warning",
    });
    expect(productCheckpointPresentation(true)).toEqual({
      text: "可创建存档",
      tone: "synced",
    });
  });
});
