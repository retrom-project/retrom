import {
  createKirikiriRuntime,
  type KirikiriRuntime,
  type KirikiriRuntimeConfig,
} from "@xxxsen/retrom-runtime";

import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import type { components } from "@/lib/api/generated/schema";

export type KiriKiriLaunchConfig = components["schemas"]["KiriKiriLaunchConfig"];

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const configKeys = [
  "adapter", "artifactId", "checkpoint", "coreId", "coreName", "gameTitle", "launchId", "mode",
  "platformName", "protocolVersion", "purpose", "returnTo", "runtimeFamily", "runtimeVersion", "sessionId",
  "warnings",
] as const;
const adapterKeys = [
  "adapterId", "adapterKind", "checkpointSlot", "projectIndexUrl", "runtimeBaseUrl", "startupXp3Path",
] as const;
const checkpointKeys = ["payloadKind", "payloadUrl"] as const;

export function isKiriKiriLaunchConfig(value: unknown): value is KiriKiriLaunchConfig {
  try {
    validateKiriKiriLaunchConfig(value);
    return true;
  } catch {
    return false;
  }
}

export function validateKiriKiriLaunchConfig(value: unknown): asserts value is KiriKiriLaunchConfig {
  if (!recordWithKeys(value, configKeys) || !validScalarConfig(value)) {
    throw new Error("PLAYER_KIRIKIRI_CONFIG_INVALID");
  }
  validateAdapter(value.adapter, value.launchId, value.runtimeVersion);
  validateCheckpoint(value.checkpoint, value.launchId);
}

function validScalarConfig(value: Record<string, unknown>) {
  const validIdentity = value.runtimeFamily === "KIRIKIRI" && value.protocolVersion === 1 &&
    value.mode === "single" && value.purpose === "PRODUCT" && value.coreId === "kirikiri2" &&
    value.platformName === "KiriKiri" && uuid(value.launchId) && value.sessionId === value.launchId &&
    uuid(value.artifactId);
  const validDisplay = boundedString(value.coreName, 1, 200) && boundedString(value.gameTitle, 1, 500) &&
    boundedString(value.runtimeVersion, 1, 160) && relativeURL(value.returnTo);
  const validWarnings = Array.isArray(value.warnings) &&
    value.warnings.every((warning) => boundedString(warning, 1, 200));
  return validIdentity && validDisplay && validWarnings;
}

function validateAdapter(value: unknown, launchId: unknown, runtimeVersion: unknown) {
  if (!uuid(launchId) || !boundedString(runtimeVersion, 1, 160) || !recordWithKeys(value, adapterKeys) ||
    value.adapterKind !== "KIRIKIRI2_WEB" || value.adapterId !== "kirikiri2-web" ||
    value.checkpointSlot !== 1999 || value.runtimeBaseUrl !== `/runtime/retrom-runtime/${runtimeVersion}/` ||
    value.projectIndexUrl !== `/runtime/projects/${launchId}/index.json` ||
    value.startupXp3Path !== null && !validXp3Path(value.startupXp3Path)) {
    throw new Error("PLAYER_KIRIKIRI_CONFIG_INVALID");
  }
}

function validateCheckpoint(value: unknown, launchId: unknown) {
  if (value === null) {return;}
  if (!uuid(launchId) || !recordWithKeys(value, checkpointKeys) ||
    value.payloadKind !== "KIRIKIRI_SAVE_BUNDLE_V1" ||
    value.payloadUrl !== `/runtime/launches/${launchId}/state`) {
    throw new Error("PLAYER_KIRIKIRI_CONFIG_INVALID");
  }
}

function validXp3Path(value: unknown): value is string {
  return boundedString(value, 1, 1024) && value.toLowerCase().endsWith(".xp3") &&
    !value.startsWith("/") && !value.includes("\\") &&
    value.split("/").every((part) => part !== "" && part !== "." && part !== "..");
}

function recordWithKeys(value: unknown, keys: readonly string[]): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}

function uuid(value: unknown): value is string {
  return typeof value === "string" && uuidPattern.test(value);
}

function boundedString(value: unknown, minimum: number, maximum: number): value is string {
  return typeof value === "string" && value.length >= minimum && value.length <= maximum;
}

function relativeURL(value: unknown): value is string {
  return typeof value === "string" && value.startsWith("/") && !value.startsWith("//") &&
    !value.includes("\\") && !value.includes("#");
}

export async function mountKiriKiriProductRuntime(
  config: KiriKiriLaunchConfig,
  target: HTMLElement,
  frameWindow: Window,
  restorePayload: Uint8Array | null,
  signal: AbortSignal,
) {
  validateKiriKiriLaunchConfig(config);
  const runtime = createKirikiriRuntime(toRuntimeConfig(config), { frameWindow, restorePayload, signal });
  try {
    await runtime.mount(target);
    return { runtime, instance: kirikiriPlayerInstance(runtime, target) };
  } catch (error) {
    await runtime.exit();
    throw error;
  }
}

function toRuntimeConfig(config: KiriKiriLaunchConfig): KirikiriRuntimeConfig {
  return { sessionId: config.sessionId, adapter: config.adapter };
}

export function kirikiriPlayerInstance(runtime: KirikiriRuntime, target: HTMLElement): EmulatorInstance {
  const instance: EmulatorInstance = {
    paused: false,
    on: () => undefined,
    takeScreenshot: async () => ({ blob: await runtime.screenshot(), format: "png" }),
    gameManager: {
      savePayloadKind: "KIRIKIRI_SAVE_BUNDLE_V1",
      getCheckpointAvailability: () => runtime.getCheckpointAvailability(),
      getStateAsync: async () => {
        const checkpoint = await runtime.checkpoint();
        if (checkpoint.payloadKind !== "KIRIKIRI_SAVE_BUNDLE_V1" || !checkpoint.bytes.byteLength) {
          throw new Error("PLAYER_STATE_UNAVAILABLE");
        }
        return checkpoint.bytes;
      },
      getVideoDimensions: (dimension) => videoDimension(target, dimension),
      toggleMainLoop: (running) => {void (running ? runtime.resume() : runtime.pause());},
    },
  };
  instance.canvas = target.querySelector("canvas") ?? undefined;
  return instance;
}

function videoDimension(target: HTMLElement, dimension: "aspect" | "width" | "height") {
  const canvas = target.querySelector("canvas");
  if (!canvas || !canvas.width || !canvas.height) {return undefined;}
  if (dimension === "width") {return canvas.width;}
  if (dimension === "height") {return canvas.height;}
  return canvas.width / canvas.height;
}
