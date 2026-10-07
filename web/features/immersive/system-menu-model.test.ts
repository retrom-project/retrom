import { describe, expect, it } from "vitest";
import { adjustSystemMenuPreference, immersiveSystemMenuItems, moveSystemMenuSelection } from "./system-menu-model";

const preferences = { bgmVolume: 0.4, bgmMuted: false, gameVolume: 1, gameMuted: false } as const;

describe("immersive system menu model", () => {
  it("wraps vertical selection at both ends", () => {
    expect(moveSystemMenuSelection(0, "up")).toBe(immersiveSystemMenuItems.length - 1);
    expect(moveSystemMenuSelection(immersiveSystemMenuItems.length - 1, "down")).toBe(0);
  });

  it("adjusts volume by ten percent and clamps at the boundaries", () => {
    expect(adjustSystemMenuPreference(preferences, "bgm-volume", "left").bgmVolume).toBe(0.3);
    expect(adjustSystemMenuPreference(preferences, "game-volume", "right").gameVolume).toBe(1);
    expect(adjustSystemMenuPreference({ ...preferences, bgmVolume: 0 }, "bgm-volume", "left").bgmVolume).toBe(0);
  });

  it("uses stable left/right mute values and toggles with confirm", () => {
    expect(adjustSystemMenuPreference(preferences, "bgm-muted", "left").bgmMuted).toBe(true);
    expect(adjustSystemMenuPreference(preferences, "bgm-muted", "right")).toBe(preferences);
    expect(adjustSystemMenuPreference(preferences, "game-muted", "confirm").gameMuted).toBe(true);
  });

  it("leaves action-only menu rows unchanged", () => {
    expect(adjustSystemMenuPreference(preferences, "fullscreen", "confirm")).toBe(preferences);
    expect(adjustSystemMenuPreference(preferences, "exit", "right")).toBe(preferences);
  });
});
