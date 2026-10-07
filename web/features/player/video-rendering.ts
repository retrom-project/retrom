import type {RuntimeVideoModeV1} from "./runtime/contract";
export type VideoRenderingMode = RuntimeVideoModeV1;
export const videoRenderingModeOptions = [
  { value: "sharp-bilinear", label: "清晰增强" },
  { value: "pixel", label: "锐利像素" },
  { value: "adaptive-sharpen", label: "增强锐化" },
  { value: "smooth", label: "平滑增强" },
  { value: "original", label: "原始画面" },
] as const;
