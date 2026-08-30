import {
  createRuntime,
  type GameRuntime,
  type OnsRuntimeConfig,
} from "@xxxsen/retrom-runtime";

import { retromRuntimePlayerInstance } from "./retrom-runtime-player";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import type { components } from "@/lib/api/generated/schema";

export type OnsLaunchConfig = components["schemas"]["OnsLaunchConfig"];

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const configKeys = [
  "adapter", "artifactId", "checkpoint", "coreId", "coreName", "gameTitle", "launchId", "mode",
  "platformName", "protocolVersion", "purpose", "returnTo", "runtimeFamily", "runtimeVersion", "sessionId",
  "warnings",
] as const;
const adapterKeys = [
  "adapterId", "adapterKind", "checkpointSlot", "projectIndexUrl", "runtimeBaseUrl", "scriptEncoding",
] as const;
const checkpointKeys = ["payloadKind", "payloadUrl"] as const;

export function isOnsLaunchConfig(value: unknown): value is OnsLaunchConfig {
  try {
    validateOnsLaunchConfig(value);
    return true;
  } catch {
    return false;
  }
}

export function validateOnsLaunchConfig(value: unknown): asserts value is OnsLaunchConfig {
  if (!recordWithKeys(value, configKeys) || !validOnsScalarConfig(value)) {
    throw new Error("PLAYER_ONS_CONFIG_INVALID");
  }
  validateAdapter(value.adapter, value.launchId, value.runtimeVersion);
  validateCheckpoint(value.checkpoint, value.launchId);
}

function validOnsScalarConfig(value: Record<string, unknown>) {
  const validIdentity = value.runtimeFamily === "ONS" && value.protocolVersion === 1 &&
    value.mode === "single" && value.purpose === "PRODUCT" && value.coreId === "onscripter_yuri" &&
    value.platformName === "ONScripter" && uuid(value.launchId) && value.sessionId === value.launchId &&
    uuid(value.artifactId);
  const validDisplay = boundedString(value.coreName, 1, 160) && boundedString(value.gameTitle, 1, 500) &&
    boundedString(value.runtimeVersion, 1, 80) && relativeURL(value.returnTo);
  const validWarnings = Array.isArray(value.warnings) &&
    value.warnings.every((warning) => boundedString(warning, 1, 120));
  return validIdentity && validDisplay && validWarnings;
}

function validateAdapter(value: unknown, launchId: unknown, runtimeVersion: unknown) {
  if (!uuid(launchId) || !boundedString(runtimeVersion, 1, 80) || !recordWithKeys(value, adapterKeys) ||
    value.adapterKind !== "ONS_YURI_WEB" ||
    value.adapterId !== "ons-yuri-web" || value.checkpointSlot !== 999 ||
    value.runtimeBaseUrl !== `/runtime/retrom-runtime/${runtimeVersion}/` ||
    !projectIndexURL(value.projectIndexUrl) ||
    value.scriptEncoding !== "gbk" && value.scriptEncoding !== "sjis" && value.scriptEncoding !== "utf8") {
    throw new Error("PLAYER_ONS_CONFIG_INVALID");
  }
}

function projectIndexURL(value: unknown) {
  return typeof value === "string" && /^\/runtime\/content\/project\/[0-9a-f]{64}\/index\.json$/u.test(value);
}

function validateCheckpoint(value: unknown, launchId: unknown) {
  if (value === null) {return;}
  if (!uuid(launchId) || !recordWithKeys(value, checkpointKeys) || value.payloadKind !== "ONS_SAVE_BUNDLE_V1" ||
    value.payloadUrl !== `/runtime/launches/${launchId}/state`) {
    throw new Error("PLAYER_ONS_CONFIG_INVALID");
  }
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

export function createOnsProductRuntime(
  config: OnsLaunchConfig,
  frameWindow: Window,
  restorePayload: Uint8Array | null,
  signal: AbortSignal,
) {
  validateOnsLaunchConfig(config);
  return createRuntime(toRuntimeConfig(config), { frameWindow, restorePayload, signal });
}

export async function mountOnsProductRuntime(
  config: OnsLaunchConfig,
  target: HTMLElement,
  frameWindow: Window,
  restorePayload: Uint8Array | null,
  signal: AbortSignal,
) {
  const runtime = createOnsProductRuntime(config, frameWindow, restorePayload, signal);
  try {
    await runtime.mount(target);
    return { runtime, instance: onsPlayerInstance(runtime, target) };
  } catch (error) {
    await runtime.exit();
    throw error;
  }
}

function toRuntimeConfig(config: OnsLaunchConfig): OnsRuntimeConfig {
  return { sessionId: config.sessionId, adapter: config.adapter };
}

export function onsPlayerInstance(runtime: GameRuntime, target: HTMLElement): EmulatorInstance {
  return retromRuntimePlayerInstance(runtime, target, {
    checkpointFormat: "ons-save-bundle-v1",
    payloadKind: "ONS_SAVE_BUNDLE_V1",
  });
}
