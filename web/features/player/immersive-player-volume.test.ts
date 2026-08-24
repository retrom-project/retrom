import { describe, expect, it, vi } from "vitest";
import type { ImmersiveAudioPreferences } from "@/features/immersive/immersive-audio-preferences";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import { applyInitialPlayerVolume } from "./immersive-player-volume";

const preferences = (overrides: Partial<ImmersiveAudioPreferences> = {}): ImmersiveAudioPreferences => ({
  bgmVolume: 0.4,
  bgmMuted: false,
  gameVolume: 1,
  gameMuted: false,
  ...overrides,
});

describe("applyInitialPlayerVolume", () => {
  it("leaves the standard and netplay runtime volume untouched", () => {
    const setVolume = vi.fn();
    const instance = { on: () => undefined, volume: 0.72, muted: false, setVolume } satisfies EmulatorInstance;
    expect(applyInitialPlayerVolume(instance, null)).toEqual({
      volume: 0.72,
      muted: false,
      lastAudibleVolume: 0.72,
    });
    expect(setVolume).not.toHaveBeenCalled();
    expect(instance).toMatchObject({ volume: 0.72, muted: false });
  });

  it("applies the immersive game volume to EmulatorJS", () => {
    const setVolume = vi.fn();
    const instance = { on: () => undefined, volume: 0.5, muted: false, setVolume } satisfies EmulatorInstance;
    expect(applyInitialPlayerVolume(instance, preferences({ gameVolume: 0.35 }))).toEqual({
      volume: 0.35,
      muted: false,
      lastAudibleVolume: 0.35,
    });
    expect(setVolume).toHaveBeenCalledWith(0.35);
    expect(instance).toMatchObject({ volume: 0.35, muted: false });
  });

  it("mutes the runtime without losing the preferred restore volume", () => {
    const setVolume = vi.fn();
    const instance = { on: () => undefined, volume: 0.5, muted: false, setVolume } satisfies EmulatorInstance;
    expect(applyInitialPlayerVolume(instance, preferences({ gameVolume: 0.65, gameMuted: true }))).toEqual({
      volume: 0.65,
      muted: true,
      lastAudibleVolume: 0.65,
    });
    expect(setVolume).toHaveBeenCalledWith(0);
    expect(instance).toMatchObject({ volume: 0.65, muted: true });
  });
});
