import {
  createRuntime,
  type ButterscotchRuntimeConfig,
  type GameRuntime,
} from "@xxxsen/retrom-runtime";

import { retromRuntimePlayerInstance } from "./retrom-runtime-player";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import type { components } from "@/lib/api/generated/schema";

export type ButterscotchLaunchConfig = components["schemas"]["ButterscotchLaunchConfig"];

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
const digestPattern = /^[0-9a-f]{64}$/u;
const configKeys = [
  "adapter", "artifactId", "checkpoint", "contentDigest", "coreId", "coreName", "gameTitle", "launchId",
  "mode", "platformName", "protocolVersion", "purpose", "returnTo", "runtimeFamily", "runtimeVersion",
  "sessionId", "warnings",
] as const;
const adapterKeys = ["adapterId", "adapterKind", "projectIndexUrl", "runtimeBaseUrl"] as const;
const checkpointKeys = ["payloadKind", "payloadUrl"] as const;

export function isButterscotchLaunchConfig(value: unknown): value is ButterscotchLaunchConfig {
  try {validateButterscotchLaunchConfig(value); return true;} catch {return false;}
}

export function validateButterscotchLaunchConfig(value: unknown): asserts value is ButterscotchLaunchConfig {
  if (!recordWithKeys(value, configKeys) || !validProtocol(value) || !validIdentity(value) ||
    !validPresentation(value)) {
    throw new Error("PLAYER_BUTTERSCOTCH_CONFIG_INVALID");
  }
  validateAdapter(value.adapter, value.runtimeVersion);
  validateCheckpoint(value.checkpoint, value.launchId);
}

function validProtocol(value: Record<string, unknown>) {
  return value.runtimeFamily === "BUTTERSCOTCH" && value.protocolVersion === 1 &&
    value.mode === "single" && value.purpose === "PRODUCT";
}

function validIdentity(value: Record<string, unknown>) {
  return value.coreId === "butterscotch" && value.platformName === "GameMaker" && uuid(value.launchId) &&
    value.sessionId === value.launchId && uuid(value.artifactId) &&
    typeof value.contentDigest === "string" && digestPattern.test(value.contentDigest);
}

function validPresentation(value: Record<string, unknown>) {
  return boundedString(value.coreName, 1, 200) && boundedString(value.gameTitle, 1, 500) &&
    boundedString(value.runtimeVersion, 1, 160) && relativeURL(value.returnTo) &&
    Array.isArray(value.warnings) && value.warnings.every((warning) => boundedString(warning, 1, 200));
}

function validateAdapter(value: unknown, runtimeVersion: unknown) {
  if (!boundedString(runtimeVersion, 1, 160) || !recordWithKeys(value, adapterKeys) ||
    value.adapterKind !== "BUTTERSCOTCH_WEB" || value.adapterId !== "butterscotch-web" ||
    value.runtimeBaseUrl !== `/runtime/retrom-runtime/${runtimeVersion}/` || !projectIndexURL(value.projectIndexUrl)) {
    throw new Error("PLAYER_BUTTERSCOTCH_CONFIG_INVALID");
  }
}

function validateCheckpoint(value: unknown, launchId: unknown) {
  if (value === null) {return;}
  if (!uuid(launchId) || !recordWithKeys(value, checkpointKeys) || value.payloadKind !== "RUNTIME_STATE" ||
    value.payloadUrl !== `/runtime/launches/${launchId}/state`) {
    throw new Error("PLAYER_BUTTERSCOTCH_CONFIG_INVALID");
  }
}

function projectIndexURL(value: unknown) {
  return typeof value === "string" && /^\/runtime\/content\/project\/[0-9a-f]{64}\/index\.json$/u.test(value);
}
function recordWithKeys(value: unknown, keys: readonly string[]): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}
function uuid(value: unknown): value is string {return typeof value === "string" && uuidPattern.test(value);}
function boundedString(value: unknown, minimum: number, maximum: number): value is string {
  return typeof value === "string" && value.length >= minimum && value.length <= maximum;
}
function relativeURL(value: unknown): value is string {
  return typeof value === "string" && value.startsWith("/") && !value.startsWith("//") &&
    !value.includes("\\") && !value.includes("#");
}

export function createButterscotchProductRuntime(
  config: ButterscotchLaunchConfig,
  frameWindow: Window,
  restorePayload: Uint8Array | null,
  signal: AbortSignal,
) {
  validateButterscotchLaunchConfig(config);
  return createRuntime(toRuntimeConfig(config), { frameWindow, restorePayload, signal });
}

function toRuntimeConfig(config: ButterscotchLaunchConfig): ButterscotchRuntimeConfig {
  return { sessionId: config.sessionId, contentDigest: config.contentDigest, adapter: config.adapter };
}

export function butterscotchPlayerInstance(runtime: GameRuntime, target: HTMLElement): EmulatorInstance {
  return retromRuntimePlayerInstance(runtime, target, {
    checkpointFormat: "butterscotch-checkpoint-v2",
    payloadKind: "RUNTIME_STATE",
  });
}
