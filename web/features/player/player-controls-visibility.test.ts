import { describe, expect, it } from "vitest";
import {
  PLAYER_CONTROLS_REVEAL_EDGE_PX,
  shouldAutoHidePlayerControls,
  shouldRevealPlayerControls,
  shouldRevealPlayerControlsForKey,
} from "./player-controls-visibility";

describe("player controls reveal edge", () => {
  it("reveals only at the top edge so mouse-controlled games keep pointer movement", () => {
    expect(shouldRevealPlayerControls(0)).toBe(true);
    expect(shouldRevealPlayerControls(PLAYER_CONTROLS_REVEAL_EDGE_PX)).toBe(true);
    expect(shouldRevealPlayerControls(PLAYER_CONTROLS_REVEAL_EDGE_PX + 1)).toBe(false);
    expect(shouldRevealPlayerControls(450)).toBe(false);
  });

  it("hides immediately after entering the running state unless controls are pinned or the game is paused", () => {
    expect(shouldAutoHidePlayerControls("loading", false, false)).toBe(false);
    expect(shouldAutoHidePlayerControls("running", false, false)).toBe(true);
    expect(shouldAutoHidePlayerControls("running", true, false)).toBe(false);
    expect(shouldAutoHidePlayerControls("running", false, true)).toBe(false);
    expect(shouldAutoHidePlayerControls("error", false, false)).toBe(false);
  });

  it("does not reveal for gameplay keys and preserves Tab navigation", () => {
    for (const key of ["w", "a", "s", "d", "j", "k", "ArrowUp", "ArrowDown", "1", "5", "p"]) {
      expect(shouldRevealPlayerControlsForKey(key)).toBe(false);
    }
    expect(shouldRevealPlayerControlsForKey("Tab")).toBe(true);
  });
});
