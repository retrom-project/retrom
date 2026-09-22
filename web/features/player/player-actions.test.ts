import { describe, expect, it } from "vitest";
import { playerActionPriority } from "./player-actions";

describe("playerActionPriority", () => {
  it("keeps save in the narrow toolbar ahead of disc without dropping overflow actions", () => {
    expect(playerActionPriority({ disc: true, save: true }))
      .toEqual({ primary: "save", overflow: ["disc"] });
    expect(playerActionPriority({ disc: false, save: true }))
      .toEqual({ primary: "save", overflow: [] });
  });
});
