import { describe, expect, it } from "vitest";
import { playerActionPriority } from "./player-actions";

describe("playerActionPriority", () => {
  it("prioritizes netplay, then disc, then save without dropping overflow actions", () => {
    expect(playerActionPriority({ netplay: true, disc: true, save: true }))
      .toEqual({ primary: "netplay", overflow: ["disc", "save"] });
    expect(playerActionPriority({ netplay: false, disc: true, save: true }))
      .toEqual({ primary: "disc", overflow: ["save"] });
    expect(playerActionPriority({ netplay: false, disc: false, save: true }))
      .toEqual({ primary: "save", overflow: [] });
  });
});
