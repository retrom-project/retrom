export type ImmersiveAudioPreferences = Readonly<{
  bgmVolume: number;
  bgmMuted: boolean;
  gameVolume: number;
  gameMuted: boolean;
}>;

export const IMMERSIVE_AUDIO_PREFERENCES_STORAGE_KEY = "retrom:immersive:audio-preferences:v1";

export const DEFAULT_IMMERSIVE_AUDIO_PREFERENCES: ImmersiveAudioPreferences = Object.freeze({
  bgmVolume: 0.4,
  bgmMuted: false,
  gameVolume: 1,
  gameMuted: false,
});

const preferenceKeys = ["bgmVolume", "bgmMuted", "gameVolume", "gameMuted"] as const;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isVolume(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 && value <= 1;
}

export function parseImmersiveAudioPreferences(raw: string | null): ImmersiveAudioPreferences | null {
  if (raw === null) {return null;}
  try {
    const value: unknown = JSON.parse(raw);
    if (!isRecord(value)) {return null;}
    const keys = Object.keys(value).sort();
    if (keys.length !== preferenceKeys.length || !preferenceKeys.every((key) => keys.includes(key))) {return null;}
    if (!isVolume(value.bgmVolume) || !isVolume(value.gameVolume)) {return null;}
    if (typeof value.bgmMuted !== "boolean" || typeof value.gameMuted !== "boolean") {return null;}
    return {
      bgmVolume: value.bgmVolume,
      bgmMuted: value.bgmMuted,
      gameVolume: value.gameVolume,
      gameMuted: value.gameMuted,
    };
  } catch {
    return null;
  }
}

export function getImmersiveAudioPreferences(): ImmersiveAudioPreferences {
  if (typeof window === "undefined") {return DEFAULT_IMMERSIVE_AUDIO_PREFERENCES;}
  try {
    return parseImmersiveAudioPreferences(window.localStorage.getItem(IMMERSIVE_AUDIO_PREFERENCES_STORAGE_KEY))
      ?? DEFAULT_IMMERSIVE_AUDIO_PREFERENCES;
  } catch {
    return DEFAULT_IMMERSIVE_AUDIO_PREFERENCES;
  }
}

export function saveImmersiveAudioPreferences(preferences: ImmersiveAudioPreferences) {
  if (typeof window === "undefined") {return;}
  try {
    window.localStorage.setItem(IMMERSIVE_AUDIO_PREFERENCES_STORAGE_KEY, JSON.stringify(preferences));
  } catch {
    // The in-memory preference remains usable when browser storage is unavailable.
  }
}
