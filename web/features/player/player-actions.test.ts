import { describe, expect, it } from "vitest";
import { playerActionPriority } from "./player-actions";

describe("playerActionPriority", () => {
  it("keeps save in the narrow toolbar ahead of disc without dropping overflow actions", () => {
    expect(playerActionPriority({ netplay: true, disc: true, save: true }))
      .toEqual({ primary: "netplay", overflow: ["save", "disc"] });
    expect(playerActionPriority({ netplay: false, disc: true, save: true }))
      .toEqual({ primary: "save", overflow: ["disc"] });
    expect(playerActionPriority({ netplay: false, disc: false, save: true }))
      .toEqual({ primary: "save", overflow: [] });
  });
});
