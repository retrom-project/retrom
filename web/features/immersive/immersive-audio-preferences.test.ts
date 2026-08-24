import { afterEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_IMMERSIVE_AUDIO_PREFERENCES,
  IMMERSIVE_AUDIO_PREFERENCES_STORAGE_KEY,
  getImmersiveAudioPreferences,
  parseImmersiveAudioPreferences,
  saveImmersiveAudioPreferences,
} from "./immersive-audio-preferences";

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("immersive audio preferences", () => {
  it("strictly accepts the complete v1 payload", () => {
    const expected = { bgmVolume: 0.35, bgmMuted: true, gameVolume: 0.8, gameMuted: false };
    expect(parseImmersiveAudioPreferences(JSON.stringify(expected))).toEqual(expected);
  });

  it.each([
    null,
    "not-json",
    "null",
    "[]",
    JSON.stringify({ bgmVolume: 0.4, bgmMuted: false, gameVolume: 1 }),
    JSON.stringify({ bgmVolume: 0.4, bgmMuted: false, gameVolume: 1, gameMuted: false, future: true }),
    JSON.stringify({ bgmVolume: -0.1, bgmMuted: false, gameVolume: 1, gameMuted: false }),
    JSON.stringify({ bgmVolume: 0.4, bgmMuted: "false", gameVolume: 1, gameMuted: false }),
  ])("rejects malformed or unknown payload %s", (raw) => {
    expect(parseImmersiveAudioPreferences(raw)).toBeNull();
  });

  it("falls back safely and persists the exact four-field payload", () => {
    expect(getImmersiveAudioPreferences()).toEqual(DEFAULT_IMMERSIVE_AUDIO_PREFERENCES);
    const preferences = { bgmVolume: 0.2, bgmMuted: true, gameVolume: 0.7, gameMuted: true };
    saveImmersiveAudioPreferences(preferences);
    expect(localStorage.getItem(IMMERSIVE_AUDIO_PREFERENCES_STORAGE_KEY)).toBe(JSON.stringify(preferences));
    expect(getImmersiveAudioPreferences()).toEqual(preferences);
  });

  it("keeps defaults available when localStorage access is denied", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {throw new DOMException("denied");});
    expect(getImmersiveAudioPreferences()).toEqual(DEFAULT_IMMERSIVE_AUDIO_PREFERENCES);
  });
});
