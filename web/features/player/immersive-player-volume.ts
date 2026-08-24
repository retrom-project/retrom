import type { ImmersiveAudioPreferences } from "@/features/immersive/immersive-audio-preferences";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";

type InitialPlayerVolume = {
  volume: number;
  muted: boolean;
  lastAudibleVolume: number | null;
};

export function applyInitialPlayerVolume(
  instance: EmulatorInstance,
  preferences: ImmersiveAudioPreferences | null,
): InitialPlayerVolume {
  if (preferences === null) {
    const volume = clampVolume(instance.volume, 0.5);
    return {
      volume,
      muted: instance.muted === true || volume === 0,
      lastAudibleVolume: volume > 0 ? volume : null,
    };
  }
  const volume = clampVolume(preferences.gameVolume, 1);
  const muted = preferences.gameMuted || volume === 0;
  instance.volume = volume;
  instance.muted = muted;
  instance.setVolume?.(muted ? 0 : volume);
  return { volume, muted, lastAudibleVolume: volume > 0 ? volume : null };
}

function clampVolume(value: unknown, fallback: number) {
  if (typeof value !== "number" || !Number.isFinite(value)) {return fallback;}
  return Math.min(1, Math.max(0, value));
}
