import {
  createRuntime,
  type GameRuntime,
  type TyranoScriptRuntimeConfig,
} from "@xxxsen/retrom-runtime";

import { retromRuntimePlayerInstance } from "./retrom-runtime-player";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import type { components } from "@/lib/api/generated/schema";

export type TyranoScriptLaunchConfig = components["schemas"]["TyranoScriptLaunchConfig"];

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
const digestPattern = /^[0-9a-f]{64}$/u;
const ticketPattern = /^[A-Za-z0-9_-]{43,128}$/u;
const configKeys = [
  "adapter", "artifactId", "checkpoint", "contentDigest", "coreId", "coreName", "gameTitle", "launchId",
  "mode", "platformName", "protocolVersion", "purpose", "returnTo", "runtimeFamily", "runtimeVersion",
  "sessionId", "warnings",
] as const;
const adapterKeys = [
  "adapterId", "adapterKind", "bootstrapTicket", "cleanupUrl", "entryUrl", "uniqueOrigin",
] as const;
const checkpointKeys = ["payloadKind", "payloadUrl"] as const;

export function isTyranoScriptLaunchConfig(value: unknown): value is TyranoScriptLaunchConfig {
  try {validateTyranoScriptLaunchConfig(value); return true;} catch {return false;}
}

export function validateTyranoScriptLaunchConfig(value: unknown): asserts value is TyranoScriptLaunchConfig {
  if (!recordWithKeys(value, configKeys) || !validProtocol(value) || !validIdentity(value) ||
    !validPresentation(value)) {
    throw new Error("PLAYER_TYRANOSCRIPT_CONFIG_INVALID");
  }
  validateAdapter(value.adapter);
  validateCheckpoint(value.checkpoint, value.launchId);
}

function validProtocol(value: Record<string, unknown>) {
  return value.runtimeFamily === "TYRANOSCRIPT" && value.protocolVersion === 1 &&
    value.mode === "single" && value.purpose === "PRODUCT";
}

function validIdentity(value: Record<string, unknown>) {
  return value.coreId === "tyranoscript" && value.platformName === "TyranoScript" && uuid(value.launchId) &&
    value.sessionId === value.launchId && uuid(value.artifactId) &&
    typeof value.contentDigest === "string" && digestPattern.test(value.contentDigest);
}

function validPresentation(value: Record<string, unknown>) {
  return boundedString(value.coreName, 1, 200) && boundedString(value.gameTitle, 1, 500) &&
    boundedString(value.runtimeVersion, 1, 160) && relativeURL(value.returnTo) &&
    Array.isArray(value.warnings) && value.warnings.length <= 16 &&
    value.warnings.every((warning) => boundedString(warning, 1, 200));
}

function validateAdapter(value: unknown) {
  if (!recordWithKeys(value, adapterKeys) || value.adapterKind !== "TYRANOSCRIPT_WEB" ||
    value.adapterId !== "tyranoscript-web" || typeof value.bootstrapTicket !== "string" ||
    !ticketPattern.test(value.bootstrapTicket) || !validOrigin(value.uniqueOrigin) ||
    value.entryUrl !== `${value.uniqueOrigin}/__retrom/tyranoscript/bootstrap` ||
    value.cleanupUrl !== `${value.uniqueOrigin}/__retrom/tyranoscript/cleanup`) {
    throw new Error("PLAYER_TYRANOSCRIPT_CONFIG_INVALID");
  }
}

function validateCheckpoint(value: unknown, launchId: unknown) {
  if (value === null) {return;}
  if (!uuid(launchId) || !recordWithKeys(value, checkpointKeys) || value.payloadKind !== "RUNTIME_STATE" ||
    value.payloadUrl !== `/runtime/launches/${launchId}/state`) {
    throw new Error("PLAYER_TYRANOSCRIPT_CONFIG_INVALID");
  }
}

function validOrigin(value: unknown) {
  if (typeof value !== "string") {return false;}
  try {
    const url = new URL(value);
    return (url.protocol === "https:" || url.protocol === "http:") && url.origin === value &&
      !url.username && !url.password;
  } catch {return false;}
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

export function createTyranoScriptProductRuntime(
  config: TyranoScriptLaunchConfig,
  frame: HTMLIFrameElement,
  frameWindow: Window,
  restorePayload: Uint8Array | null,
  signal: AbortSignal,
) {
  validateTyranoScriptLaunchConfig(config);
  return createRuntime(toRuntimeConfig(config), {frame, frameWindow, restorePayload, signal});
}

function toRuntimeConfig(config: TyranoScriptLaunchConfig): TyranoScriptRuntimeConfig {
  return {sessionId: config.sessionId, contentDigest: config.contentDigest, adapter: config.adapter};
}

export function tyranoScriptPlayerInstance(runtime: GameRuntime, target: HTMLElement): EmulatorInstance {
  return retromRuntimePlayerInstance(runtime, target, {
    checkpointFormat: "tyranoscript-snapshot-v1",
    payloadKind: "RUNTIME_STATE",
  });
}
