import { userStorageKey } from "@/features/auth/storage";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";

export const videoRenderingModeOptions = [
  { value: "clear", label: "清晰增强" },
  { value: "pixel", label: "锐利像素" },
  { value: "sharpen", label: "增强锐化" },
  { value: "smooth", label: "平滑增强" },
  { value: "original", label: "原始画面" },
] as const;

export type VideoRenderingMode = typeof videoRenderingModeOptions[number]["value"];

const modeValues = new Set<VideoRenderingMode>(videoRenderingModeOptions.map((option) => option.value));
const defaultMode: VideoRenderingMode = "pixel";
const preferenceChangedEvent = "retrom:video-rendering-mode-changed";

const modeConfiguration: Record<VideoRenderingMode, { shader: string; imageRendering: "auto" | "pixelated" }> = {
  clear: { shader: "retrom-sharp-bilinear", imageRendering: "pixelated" },
  pixel: { shader: "disabled", imageRendering: "pixelated" },
  sharpen: { shader: "retrom-adaptive-sharpen", imageRendering: "auto" },
  smooth: { shader: "sabr", imageRendering: "auto" },
  original: { shader: "disabled", imageRendering: "auto" },
};

function preferenceKey(userId: string | null | undefined) {
  return userStorageKey(userId, "player", "video-rendering-mode");
}

export function isVideoRenderingMode(value: unknown): value is VideoRenderingMode {
  return typeof value === "string" && modeValues.has(value as VideoRenderingMode);
}

export function readVideoRenderingMode(userId: string | null | undefined): VideoRenderingMode {
  try {
    const key = preferenceKey(userId);
    const stored = key ? window.localStorage.getItem(key) : null;
    return isVideoRenderingMode(stored) ? stored : defaultMode;
  } catch {
    return defaultMode;
  }
}

export function writeVideoRenderingMode(userId: string | null | undefined, mode: VideoRenderingMode) {
  try {
    const key = preferenceKey(userId);
    if (key) {
      window.localStorage.setItem(key, mode);
      window.dispatchEvent(new Event(preferenceChangedEvent));
    }
  } catch {
    // Rendering remains available with the sharp-pixel default when storage is blocked.
  }
}

export function subscribeVideoRenderingMode(onStoreChange: () => void) {
  window.addEventListener("storage", onStoreChange);
  window.addEventListener(preferenceChangedEvent, onStoreChange);
  return () => {
    window.removeEventListener("storage", onStoreChange);
    window.removeEventListener(preferenceChangedEvent, onStoreChange);
  };
}

export function applyVideoRenderingMode(
  emulator: EmulatorInstance | undefined,
  canvas: HTMLCanvasElement | null,
  mode: VideoRenderingMode,
) {
  const configuration = modeConfiguration[mode];
  canvas?.style.setProperty("image-rendering", configuration.imageRendering, "important");
  if (emulator?.changeSettingOption) {
    emulator.changeSettingOption("shader", configuration.shader);
    return true;
  }
  if (emulator?.enableShader) {
    emulator.enableShader(configuration.shader);
    return true;
  }
  return false;
}
