import type { ImmersiveAudioPreferences } from "./immersive-audio-preferences";

export const immersiveSystemMenuItems = [
  "bgm-volume",
  "bgm-muted",
  "game-volume",
  "game-muted",
  "fullscreen",
  "exit",
] as const;

export type ImmersiveSystemMenuItem = typeof immersiveSystemMenuItems[number];
export type MenuAdjustment = "left" | "right" | "confirm";

export function moveSystemMenuSelection(current: number, direction: "up" | "down") {
  const offset = direction === "up" ? -1 : 1;
  return (current + offset + immersiveSystemMenuItems.length) % immersiveSystemMenuItems.length;
}

function adjustedVolume(value: number, direction: Exclude<MenuAdjustment, "confirm">) {
  const delta = direction === "left" ? -0.1 : 0.1;
  return Math.round(Math.min(1, Math.max(0, value + delta)) * 10) / 10;
}

export function adjustSystemMenuPreference(
  preferences: ImmersiveAudioPreferences,
  item: ImmersiveSystemMenuItem,
  adjustment: MenuAdjustment,
): ImmersiveAudioPreferences {
  if (item === "bgm-volume" && adjustment !== "confirm") {
    return { ...preferences, bgmVolume: adjustedVolume(preferences.bgmVolume, adjustment) };
  }
  if (item === "game-volume" && adjustment !== "confirm") {
    return { ...preferences, gameVolume: adjustedVolume(preferences.gameVolume, adjustment) };
  }
  if (item === "bgm-muted") {
    const bgmMuted = adjustment === "confirm" ? !preferences.bgmMuted : adjustment === "left";
    return bgmMuted === preferences.bgmMuted ? preferences : { ...preferences, bgmMuted };
  }
  if (item === "game-muted") {
    const gameMuted = adjustment === "confirm" ? !preferences.gameMuted : adjustment === "left";
    return gameMuted === preferences.gameMuted ? preferences : { ...preferences, gameMuted };
  }
  return preferences;
}
