import { describe, expect, it } from "vitest";
import { PLAYER_CONTROLS_REVEAL_EDGE_PX, shouldRevealPlayerControls } from "./player-controls-visibility";

describe("player controls reveal edge", () => {
  it("reveals only at the top edge so mouse-controlled games keep pointer movement", () => {
    expect(shouldRevealPlayerControls(0)).toBe(true);
    expect(shouldRevealPlayerControls(PLAYER_CONTROLS_REVEAL_EDGE_PX)).toBe(true);
    expect(shouldRevealPlayerControls(PLAYER_CONTROLS_REVEAL_EDGE_PX + 1)).toBe(false);
    expect(shouldRevealPlayerControls(450)).toBe(false);
  });
});
